package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/observability"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/storage"
)

var traceIDContextKey = struct{ Name string }{Name: "agentctl.trace_id"}

func traceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDContextKey).(string); ok {
		return v
	}
	return ""
}

// Runtime manages agent sessions and lifecycle.
type Runtime struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	config   Config
}

// Config configures the agent runtime.
type Config struct {
	// DefaultMaxIterations is the default limit for ReAct iterations.
	DefaultMaxIterations int

	// DefaultMaxContextTokens is the default context token budget.
	// When exceeded, the engine stops with StopReasonContextBudget.
	// Set to 0 to disable (default).
	DefaultMaxContextTokens int

	// DefaultTimeout is the default session timeout.
	DefaultTimeout time.Duration

	// SessionStore persists agent sessions to database.
	// When nil, sessions are only kept in memory.
	SessionStore storage.SessionStore

	// LLMProvider is the default LLM provider (e.g., "gemini", "openai").
	LLMProvider string

	// LLMModel is the default model name.
	LLMModel string

	// LLMAPIKey is the API key for the LLM provider.
	LLMAPIKey string

	// WorkspaceRoot is the workspace root directory.
	WorkspaceRoot string

	// DefaultMaxDepth is the default max hierarchy depth for spawned agents.
	DefaultMaxDepth int

	// SpawnHandler is called when an agent requests to spawn subagents.
	// If nil, spawn requests return a "pending" status without actual spawning.
	SpawnHandler SpawnHandler

	// MaxConcurrentAgents is the maximum number of concurrent agent sessions.
	// If > 0, Spawn() atomically enforces this limit to avoid TOCTOU races.
	MaxConcurrentAgents int

	// OpenMemoryStore provides access to named memory for retrieval tools like code.symbol_search.
	// When nil, tools requiring named memory return empty results.
	OpenMemoryStore func(context.Context) (storage.MemoryStore, error)

	// TrajectoryStorageRoot enables agent tool call capture when set.
	TrajectoryStorageRoot string

	// HookDispatcher dispatches hook events for tool calls.
	HookDispatcher hooks.Dispatcher

	// ActionExecutor processes hook output actions (run_skill, send_mailbox, etc).
	// When nil, hook actions are ignored.
	ActionExecutor hooks.ActionExecutor

	// MailboxStore provides access to inter-agent mailbox messaging.
	// When nil, mailbox tools are not available.
	MailboxStore MailboxStore

	// BoardStore provides access to workspace blackboard coordination.
	// When nil, blackboard tools are not available.
	BoardStore BoardStore

	// CASStore provides content-addressable storage for turn content.
	// When nil, turn content is not persisted to CAS.
	CASStore storage.CASStore
}

// MailboxStore is the interface for inter-agent messaging.
// Use mailbox.Store from internal/storage/mailbox.
type MailboxStore interface {
	Send(ctx context.Context, msg agent.Message) error
	Ack(ctx context.Context, messageID string) error
	List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error)
}

// BoardStore is the interface for workspace blackboard coordination.
// Use blackboard.BoardStore from internal/storage/blackboard.
type BoardStore interface {
	SendMessage(ctx context.Context, msg *agent.BoardMessage) error
	Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error)
	MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
}

// SpawnHandler processes spawn requests from agents.
type SpawnHandler interface {
	// HandleSpawnRequest processes a spawn request and returns the response.
	HandleSpawnRequest(ctx context.Context, req types.SpawnRequest) (*types.SpawnResponse, error)
}

// Session represents a running agent session.
type Session struct {
	ID           string
	Config       types.AgentConfig
	Status       types.AgentStatus
	Engine       *engine.LLMChatEngine
	Tools        []engine.ToolDef
	StartedAt    time.Time
	EndedAt      *time.Time
	Iterations   int
	Summary      string
	Error        string
	ToolCalls    []types.ToolCall
	Children     []string // IDs of spawned child sessions
	SystemPrompt string   // Role-specific system prompt
	TurnCounter  uint64   // Monotonically increasing turn index
	cancel       context.CancelFunc
	mu           sync.RWMutex
}

// nextTurnIndex atomically increments and returns the next turn index for this session.
func (s *Session) nextTurnIndex() int {
	return int(atomic.AddUint64(&s.TurnCounter, 1) - 1)
}

// NewRuntime creates a new agent runtime.
func NewRuntime(cfg Config) *Runtime {
	if cfg.DefaultMaxIterations <= 0 {
		cfg.DefaultMaxIterations = 10
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.DefaultMaxDepth <= 0 {
		cfg.DefaultMaxDepth = 3 // Default: overseer -> agent -> subagent
	}

	return &Runtime{
		sessions: make(map[string]*Session),
		config:   cfg,
	}
}

// Spawn creates and starts a new agent session.
//
// Index:
// - Purpose: Initialize a session and launch the agent loop
// - Flow: apply defaults → create engine/tools → create session → store session → spawn runSession
// - SideEffects: starts goroutine; may open memory store; engine initialization
// - FailureModes: engine/tool init errors, resource limits exceeded
// - Related: Runtime.runSession, Runtime.createEngine
// - Keywords: agent_spawn, session_id, max_iterations, tools
func (r *Runtime) Spawn(ctx context.Context, cfg types.AgentConfig) (*Session, error) {
	// Generate session ID
	sessionID := ulid.Make().String()

	// Apply defaults
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = r.config.DefaultMaxIterations
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = r.config.DefaultTimeout
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = r.config.LLMProvider
	}
	if cfg.LLMModel == "" {
		cfg.LLMModel = r.config.LLMModel
	}

	// Initialize hierarchy fields if not set
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = r.config.DefaultMaxDepth
	}
	if cfg.LocalMaxDepth <= 0 {
		cfg.LocalMaxDepth = cfg.MaxDepth
	}
	// RootActorID defaults to self if this is the root agent (depth 0)
	if cfg.RootActorID == "" && cfg.Depth == 0 {
		cfg.RootActorID = cfg.ActorID
	}

	// Create LLMChatEngine
	llmEngine, tools, err := r.createEngine(cfg, sessionID)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	// Create session context with cancellation
	sessionCtx, cancel := context.WithCancel(ctx)

	session := &Session{
		ID:        sessionID,
		Config:    cfg,
		Status:    types.StatusRunning,
		Engine:    llmEngine,
		Tools:     tools,
		StartedAt: time.Now(),
		ToolCalls: []types.ToolCall{},
		Children:  []string{},
		cancel:    cancel,
	}

	// Store session (atomic with limit check to avoid TOCTOU race)
	r.mu.Lock()
	if r.config.MaxConcurrentAgents > 0 && len(r.sessions) >= r.config.MaxConcurrentAgents {
		r.mu.Unlock()
		cancel() // Cancel the session context since we won't use it
		return nil, fmt.Errorf("resource_exhausted: max concurrent agents reached (current: %d, max: %d)",
			len(r.sessions), r.config.MaxConcurrentAgents)
	}
	r.sessions[sessionID] = session
	r.mu.Unlock()

	// Persist to database if session store is configured
	if r.config.SessionStore != nil {
		// Compute prompt hash for correlation with wide events
		var promptHash string
		if cfg.Prompt != "" {
			h := sha256.Sum256([]byte(cfg.Prompt))
			promptHash = "sha256:" + hex.EncodeToString(h[:])
		}

		// Resolve provider/model for persistence (same logic as createEngine)
		provider := cfg.LLMProvider
		if provider == "" {
			provider = r.config.LLMProvider
		}
		model := cfg.LLMModel
		if model == "" {
			model = r.config.LLMModel
		}
		// If model still empty, use default for the resolved provider
		if model == "" && provider != "" {
			model = llmproviders.DefaultModelForProvider(provider)
		}

		dbSession := storage.Session{
			ID:            sessionID,
			WorkspacePath: r.config.WorkspaceRoot,
			AgentID:       cfg.ActorID,
			AgentType:     string(cfg.Role),
			Status:        storage.SessionStatusRunning,
			StartedAt:     session.StartedAt,
			Prompt:        cfg.Prompt,
			PromptHash:    promptHash,
			LLMProvider:   provider,
			LLMModel:      model,
		}
		if _, err := r.config.SessionStore.Save(ctx, dbSession); err != nil {
			// Log but don't fail - in-memory tracking is sufficient
			// The session will just not be visible in agentctl session list
			_ = err // TODO: Add logging
		}
	}

	// Emit spawn event for real-time activity tracking
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentSpawn).
		WithComponent(observability.ComponentAgent).
		WithSession(sessionID, cfg.ActorID).
		WithWorkspace(r.config.WorkspaceRoot).
		WithData("role", string(cfg.Role)).
		WithData("depth", cfg.Depth).
		WithData("max_iterations", cfg.MaxIterations).
		Success(0)) // Duration 0 since spawn is instant

	// Start the agent in background
	go r.runSession(sessionCtx, session)

	return session, nil
}

// createEngine creates an LLMChatEngine with tools for the given agent configuration.
func (r *Runtime) createEngine(cfg types.AgentConfig, sessionID string) (*engine.LLMChatEngine, []engine.ToolDef, error) {
	// Resolve provider: agent → runtime → auto-detect from env
	provider := cfg.LLMProvider
	if provider == "" {
		provider = r.config.LLMProvider
	}

	// Resolve model: agent → runtime → provider-specific default
	model := cfg.LLMModel
	if model == "" {
		model = r.config.LLMModel
	}
	if model == "" && provider != "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}

	// Resolve max context tokens: agent → runtime default
	maxContextTokens := cfg.MaxContextTokens
	if maxContextTokens <= 0 {
		maxContextTokens = r.config.DefaultMaxContextTokens
	}

	// Create LLMChatEngine config - it will auto-detect provider/key from env if not specified
	engineCfg := engine.LLMChatConfig{
		Provider:         provider,
		Model:            model,
		MaxIterations:    cfg.MaxIterations,
		MaxContextTokens: maxContextTokens,
		Timeout:          cfg.Timeout,
		HookDispatcher:   r.config.HookDispatcher,
		ActionExecutor:   r.config.ActionExecutor,
	}

	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create LLM engine: %w", err)
	}

	// Set hook context
	llmEngine.SetHookContext(engine.HookContext{
		SessionID:     sessionID,
		ActorID:       cfg.ActorID,
		WorkspaceID:   cfg.WorkspaceID,
		WorkspaceRoot: r.config.WorkspaceRoot,
	})

	// Create tool executor adapter and get tool definitions
	executor, toolDefs := r.createToolExecutor(cfg, sessionID)

	// Create ToolRunner with the executor
	runnerCfg := engine.ToolRunnerConfig{
		Workspace:      r.config.WorkspaceRoot,
		WorkspaceID:    cfg.WorkspaceID,
		SessionID:      sessionID,
		ActorID:        cfg.ActorID,
		ActionExecutor: r.config.ActionExecutor,
	}
	toolRunner := engine.NewToolRunner(executor, r.config.HookDispatcher, runnerCfg)
	llmEngine.SetToolRunner(toolRunner)

	return llmEngine, toolDefs, nil
}

// createToolExecutor creates a ToolExecutor adapter for the agent tools registry.
func (r *Runtime) createToolExecutor(cfg types.AgentConfig, sessionID string) (engine.ToolExecutor, []engine.ToolDef) {
	// Build tool definitions based on agent role and available stores
	toolDefs := buildToolDefsForRole(cfg.Role, r.config.MailboxStore != nil, r.config.BoardStore != nil, cfg.SkillsAllow)

	// Create the executor adapter
	executor := &agentToolExecutor{
		workspaceRoot:   r.config.WorkspaceRoot,
		workspaceID:     cfg.WorkspaceID,
		sessionID:       sessionID,
		actorID:         cfg.ActorID,
		depth:           cfg.Depth,
		maxDepth:        cfg.MaxDepth,
		localMaxDepth:   cfg.LocalMaxDepth,
		agentRole:       string(cfg.Role),
		hookDispatcher:  r.config.HookDispatcher,
		openMemoryStore: r.config.OpenMemoryStore,
		mailboxStore:    r.config.MailboxStore,
		boardStore:      r.config.BoardStore,
		toolDefs:        toolDefs,
	}

	// Overseer agents get runtime access for agent management
	if cfg.Role == types.RoleOverseer {
		executor.runtime = r
	}

	return executor, toolDefs
}

// agentToolExecutor implements engine.ToolExecutor for agent tools.
type agentToolExecutor struct {
	workspaceRoot   string
	workspaceID     string
	sessionID       string
	actorID         string
	depth           int
	maxDepth        int
	localMaxDepth   int
	agentRole       string
	hookDispatcher  hooks.Dispatcher
	openMemoryStore func(context.Context) (storage.MemoryStore, error)
	mailboxStore    MailboxStore
	boardStore      BoardStore
	toolDefs        []engine.ToolDef
	// runtime is set for overseer agents to enable agent management
	runtime *Runtime
}

// Execute runs a tool by name with the given arguments.
func (e *agentToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	// Parse args into map
	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}

	// Execute based on tool name (using underscores for Anthropic API compatibility)
	switch name {
	case "fs_read_file":
		return e.executeReadFile(ctx, argsMap)
	case "fs_list_dir":
		return e.executeListDir(ctx, argsMap)
	case "fs_write_file":
		return e.executeWriteFile(ctx, argsMap)
	case "code_search":
		return e.executeCodeSearch(ctx, argsMap)
	case "think":
		return e.executeThink(ctx, argsMap)
	// Mailbox tools
	case "mail_inbox":
		return e.executeMailInbox(ctx, argsMap)
	case "mail_send":
		return e.executeMailSend(ctx, argsMap)
	case "mail_ack":
		return e.executeMailAck(ctx, argsMap)
	// Blackboard tools
	case "bb_inbox":
		return e.executeBBInbox(ctx, argsMap)
	case "bb_post":
		return e.executeBBPost(ctx, argsMap)
	case "bb_mark_read":
		return e.executeBBMarkRead(ctx, argsMap)
	// Overseer context gathering tools
	case "context_search":
		return e.executeContextSearch(ctx, argsMap)
	case "session_timeline":
		return e.executeSessionTimeline(ctx, argsMap)
	case "smart_search":
		return e.executeSmartSearch(ctx, argsMap)
	case "context_grep":
		return e.executeContextGrep(ctx, argsMap)
	case "repo_index_search":
		return e.executeRepoIndexSearch(ctx, argsMap)
	case "repo_index_expand":
		return e.executeRepoIndexExpand(ctx, argsMap)
	case "repo_index_open":
		return e.executeRepoIndexOpen(ctx, argsMap)
	case "repo_index_dag_grep":
		return e.executeRepoIndexDagGrep(ctx, argsMap)
	// Overseer agent management tools
	case "agent_spawn":
		return e.executeAgentSpawn(ctx, argsMap)
	case "agent_list":
		return e.executeAgentList(ctx, argsMap)
	case "agent_status":
		return e.executeAgentStatus(ctx, argsMap)
	case "agent_kill":
		return e.executeAgentKill(ctx, argsMap)
	case "agent_hierarchy":
		return e.executeAgentHierarchy(ctx, argsMap)
	case "agent_wait":
		return e.executeAgentWait(ctx, argsMap)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// List returns all available tool definitions.
func (e *agentToolExecutor) List() []engine.ToolDef {
	return e.toolDefs
}

// Tool execution implementations
func (e *agentToolExecutor) executeReadFile(ctx context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Resolve path relative to workspace
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		if e.workspaceRoot != "" {
			fullPath = e.workspaceRoot + "/" + path
		} else if cwd, err := os.Getwd(); err == nil {
			fullPath = cwd + "/" + path
		}
	}

	data, err := readFileWithLimit(fullPath, 1024*1024) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	return string(data), nil
}

func (e *agentToolExecutor) executeListDir(ctx context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	// Resolve path relative to workspace
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		if e.workspaceRoot != "" {
			fullPath = e.workspaceRoot + "/" + path
		} else {
			// Default to current working directory if no workspace set
			if cwd, err := os.Getwd(); err == nil {
				fullPath = cwd + "/" + path
			}
		}
	}

	entries, err := listDirEntries(fullPath)
	if err != nil {
		return "", fmt.Errorf("list dir: %w", err)
	}

	result, _ := json.Marshal(entries)
	return string(result), nil
}

func (e *agentToolExecutor) executeWriteFile(ctx context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Resolve path relative to workspace
	fullPath := path
	if !strings.HasPrefix(path, "/") {
		if e.workspaceRoot != "" {
			fullPath = e.workspaceRoot + "/" + path
		} else if cwd, err := os.Getwd(); err == nil {
			fullPath = cwd + "/" + path
		}
	}

	if err := writeFileSafe(fullPath, []byte(content)); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
}

func (e *agentToolExecutor) executeCodeSearch(ctx context.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	// Use simple grep for now
	results, err := simpleGrep(e.workspaceRoot, pattern)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}

	return results, nil
}

func (e *agentToolExecutor) executeThink(ctx context.Context, args map[string]any) (string, error) {
	thought, _ := args["thought"].(string)
	// Think is just a reflection tool - return the thought as acknowledgment
	return fmt.Sprintf("Acknowledged: %s", thought), nil
}

// --- Mailbox tool executors ---

func (e *agentToolExecutor) executeMailInbox(ctx context.Context, args map[string]any) (string, error) {
	if e.mailboxStore == nil {
		return "", fmt.Errorf("mailbox not configured")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	// Use actorID as the agent namespace for polling
	agentNS := e.actorID
	if agentNS == "" {
		agentNS = e.sessionID
	}

	messages, err := e.mailboxStore.List(ctx, agentNS, limit)
	if err != nil {
		return "", fmt.Errorf("list inbox: %w", err)
	}

	if len(messages) == 0 {
		return "No messages in inbox.", nil
	}

	// Format messages for the agent
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d message(s):\n\n", len(messages)))
	for _, msg := range messages {
		result.WriteString(fmt.Sprintf("ID: %s\nFrom: %s\nType: %s\n", msg.ID, msg.FromNS, msg.Type))
		result.WriteString(fmt.Sprintf("Content: %s\n---\n", string(msg.Payload)))
	}
	return result.String(), nil
}

func (e *agentToolExecutor) executeMailSend(ctx context.Context, args map[string]any) (string, error) {
	if e.mailboxStore == nil {
		return "", fmt.Errorf("mailbox not configured")
	}

	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)

	if to == "" || subject == "" || body == "" {
		return "", fmt.Errorf("to, subject, and body are required")
	}

	// Build message
	fromNS := e.actorID
	if fromNS == "" {
		fromNS = e.sessionID
	}

	payload := fmt.Sprintf(`{"subject":%q,"body":%q}`, subject, body)

	msg := agent.Message{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		FromNS:    fromNS,
		ToNS:      to,
		Type:      agent.MessageTypeAsk,
		Payload:   json.RawMessage(payload),
		SessionID: e.sessionID,
		Workspace: e.workspaceID,
		Timestamp: time.Now().UnixMilli(),
	}

	if err := e.mailboxStore.Send(ctx, msg); err != nil {
		return "", fmt.Errorf("send message: %w", err)
	}

	return fmt.Sprintf("Message sent to %s (ID: %s)", to, msg.ID), nil
}

func (e *agentToolExecutor) executeMailAck(ctx context.Context, args map[string]any) (string, error) {
	if e.mailboxStore == nil {
		return "", fmt.Errorf("mailbox not configured")
	}

	messageID, _ := args["message_id"].(string)
	if messageID == "" {
		return "", fmt.Errorf("message_id is required")
	}

	if err := e.mailboxStore.Ack(ctx, messageID); err != nil {
		return "", fmt.Errorf("ack message: %w", err)
	}

	return fmt.Sprintf("Acknowledged message %s", messageID), nil
}

// --- Blackboard tool executors ---

func (e *agentToolExecutor) executeBBInbox(ctx context.Context, args map[string]any) (string, error) {
	if e.boardStore == nil {
		return "", fmt.Errorf("blackboard not configured")
	}

	unreadOnly := true
	if u, ok := args["unread_only"].(bool); ok {
		unreadOnly = u
	}
	kind, _ := args["kind"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	filter := agent.InboxFilter{
		WorkspaceID: e.workspaceID,
		ActorID:     e.actorID,
		OnlyUnread:  unreadOnly,
		Stream:      kind, // kind maps to stream for filtering
		Limit:       limit,
	}

	messages, err := e.boardStore.Inbox(ctx, filter)
	if err != nil {
		return "", fmt.Errorf("query blackboard: %w", err)
	}

	if len(messages) == 0 {
		return "No messages on the blackboard.", nil
	}

	// Format messages for the agent
	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d blackboard message(s):\n\n", len(messages)))
	for _, msg := range messages {
		result.WriteString(fmt.Sprintf("ID: %s\nFrom: %s\nKind: %s\nPriority: %d\nSubject: %s\n",
			msg.ID, msg.Sender, msg.Kind, msg.Priority, msg.Subject))
		result.WriteString(fmt.Sprintf("Body: %s\n---\n", msg.Body))
	}
	return result.String(), nil
}

func (e *agentToolExecutor) executeBBPost(ctx context.Context, args map[string]any) (string, error) {
	if e.boardStore == nil {
		return "", fmt.Errorf("blackboard not configured")
	}

	to, _ := args["to"].(string)
	subject, _ := args["subject"].(string)
	body, _ := args["body"].(string)
	kind, _ := args["kind"].(string)
	priority := 2 // default normal
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	if to == "" || subject == "" || body == "" {
		return "", fmt.Errorf("to, subject, and body are required")
	}

	// Validate and normalize kind
	var msgKind agent.BoardMessageKind
	switch kind {
	case "instruction":
		msgKind = agent.BoardMessageKindInstruction
	case "alert":
		msgKind = agent.BoardMessageKindAlert
	case "review_request":
		msgKind = agent.BoardMessageKindReviewRequest
	case "info", "":
		msgKind = agent.BoardMessageKindInfo
	default:
		// Default to info for unrecognized values
		msgKind = agent.BoardMessageKindInfo
	}

	msg := &agent.BoardMessage{
		ID:          "bb-" + ulid.Make().String(),
		WorkspaceID: e.workspaceID,
		Sender:      e.actorID,
		Recipient:   to,
		Subject:     subject,
		Body:        body,
		Kind:        msgKind,
		Priority:    priority,
		Status:      agent.BoardMessageStatusUnread,
		CreatedAt:   time.Now(),
	}

	if err := e.boardStore.SendMessage(ctx, msg); err != nil {
		return "", fmt.Errorf("post to blackboard: %w", err)
	}

	return fmt.Sprintf("Posted to blackboard (ID: %s)", msg.ID), nil
}

func (e *agentToolExecutor) executeBBMarkRead(ctx context.Context, args map[string]any) (string, error) {
	if e.boardStore == nil {
		return "", fmt.Errorf("blackboard not configured")
	}

	messageIDsRaw, ok := args["message_ids"].([]any)
	if !ok || len(messageIDsRaw) == 0 {
		return "", fmt.Errorf("message_ids is required")
	}

	messageIDs := make([]string, 0, len(messageIDsRaw))
	for _, id := range messageIDsRaw {
		if s, ok := id.(string); ok {
			messageIDs = append(messageIDs, s)
		}
	}

	count, err := e.boardStore.MarkRead(ctx, e.workspaceID, e.actorID, messageIDs)
	if err != nil {
		return "", fmt.Errorf("mark read: %w", err)
	}

	return fmt.Sprintf("Marked %d message(s) as read", count), nil
}

// Overseer agent management tool implementations

func (e *agentToolExecutor) executeAgentSpawn(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent spawning not available (not an overseer)")
	}

	roleStr, _ := args["role"].(string)
	if roleStr == "" {
		return "", fmt.Errorf("role is required")
	}
	task, _ := args["task"].(string)
	if task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Optional LLM configuration
	llmProvider, _ := args["llm_provider"].(string)
	llmModel, _ := args["llm_model"].(string)

	// Start timing for observability
	spawnStart := time.Now()

	// Compute task hash for correlation with session persistence
	taskHashBytes := sha256.Sum256([]byte(task))
	taskHash := "sha256:" + hex.EncodeToString(taskHashBytes[:])

	spawnEvent := observability.NewEvent(observability.OpAgentSpawn).
		WithComponent(observability.ComponentAgent).
		WithSession(e.sessionID, e.actorID).
		WithWorkspace(e.workspaceRoot).
		WithData("role", roleStr).
		WithData("task_len", len(task)).
		WithData("task_hash", taskHash).
		WithData("caller_depth", e.depth).
		WithData("llm_provider", llmProvider).
		WithData("llm_model", llmModel)

	localMaxDepth := e.localMaxDepth
	if lmd, ok := args["local_max_depth"].(float64); ok && int(lmd) > 0 {
		// Can only tighten, not loosen
		if int(lmd) < localMaxDepth {
			localMaxDepth = int(lmd)
		}
	}

	// Build spawn request
	req := types.SpawnRequest{
		CallerActorID:       e.actorID,
		CallerDepth:         e.depth,
		CallerMaxDepth:      e.maxDepth,
		CallerLocalMaxDepth: localMaxDepth,
		EpicID:              "", // TODO: propagate epic ID
		RequestedSubagents: []types.SubagentRequest{
			{
				Role:          types.AgentRole(roleStr),
				Task:          task,
				LocalMaxDepth: localMaxDepth,
				LLMProvider:   llmProvider,
				LLMModel:      llmModel,
			},
		},
	}

	// Use overseer if available
	if e.runtime.config.SpawnHandler != nil {
		resp, err := e.runtime.config.SpawnHandler.HandleSpawnRequest(ctx, req)
		if err != nil {
			// Always record spawn events (bypass sampling) for debugging
			_ = observability.EmitSync(ctx, spawnEvent.
				WithData("error_phase", "handler").
				Error(err, time.Since(spawnStart)))
			return "", fmt.Errorf("spawn request failed: %w", err)
		}

		if len(resp.DeniedAgents) > 0 && len(resp.SpawnedAgents) == 0 {
			// Always record spawn events (bypass sampling) for debugging multi-agent workflows
			_ = observability.EmitSync(ctx, spawnEvent.
				WithData("denied", true).
				WithData("deny_reason", resp.DeniedAgents[0].Reason).
				Success(time.Since(spawnStart)))
			return fmt.Sprintf("Spawn denied: %s", resp.DeniedAgents[0].Reason), nil
		}

		if len(resp.SpawnedAgents) > 0 {
			agent := resp.SpawnedAgents[0]
			// Always record spawn events (bypass sampling) for debugging multi-agent workflows
			_ = observability.EmitSync(ctx, spawnEvent.
				WithData("spawned_session", agent.SessionID).
				WithData("spawned_actor", agent.ActorID).
				WithData("spawned_depth", agent.Depth).
				Success(time.Since(spawnStart)))
			return fmt.Sprintf("Spawned agent: session=%s, actor=%s, depth=%d", agent.SessionID, agent.ActorID, agent.Depth), nil
		}

		// Always record spawn events (bypass sampling) for debugging multi-agent workflows
		_ = observability.EmitSync(ctx, spawnEvent.
			WithData("accepted", resp.Accepted).
			WithData("reason", resp.Reason).
			Success(time.Since(spawnStart)))
		return fmt.Sprintf("Spawn result: accepted=%v, reason=%s", resp.Accepted, resp.Reason), nil
	}

	// Direct spawn (no overseer)
	cfg := types.AgentConfig{
		Role:          types.AgentRole(roleStr),
		ActorID:       fmt.Sprintf("actor:%s:%s", roleStr, ulid.Make().String()),
		Prompt:        task,
		ParentActorID: e.actorID,
		Depth:         e.depth + 1,
		MaxDepth:      e.maxDepth,
		LocalMaxDepth: localMaxDepth,
		LLMProvider:   llmProvider,
		LLMModel:      llmModel,
	}

	session, err := e.runtime.Spawn(ctx, cfg)
	if err != nil {
		// Emit error event for consistency with overseer path
		_ = observability.EmitSync(ctx, spawnEvent.
			WithData("error_phase", "direct_spawn").
			Error(err, time.Since(spawnStart)))
		return "", fmt.Errorf("spawn failed: %w", err)
	}

	// Emit success event for consistency with overseer path
	_ = observability.EmitSync(ctx, spawnEvent.
		WithData("spawned_session", session.ID).
		WithData("spawned_actor", cfg.ActorID).
		WithData("spawned_depth", cfg.Depth).
		Success(time.Since(spawnStart)))

	// Register parent-child relationship for hierarchy tracking
	e.runtime.RegisterChild(e.sessionID, e.actorID, session.ID, cfg.ActorID, cfg.Depth)

	return fmt.Sprintf("Spawned agent: session=%s, actor=%s", session.ID, cfg.ActorID), nil
}

func (e *agentToolExecutor) executeAgentList(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent listing not available")
	}

	sessions := e.runtime.List()
	if len(sessions) == 0 {
		return "No active agent sessions", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Active sessions (%d):\n", len(sessions)))
	for _, s := range sessions {
		sb.WriteString(fmt.Sprintf("- %s: role=%s, status=%s, actor=%s\n",
			s.ID, s.Config.Role, s.Status, s.Config.ActorID))
	}
	return sb.String(), nil
}

func (e *agentToolExecutor) executeAgentStatus(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent status not available")
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	session, ok := e.runtime.Get(sessionID)
	if !ok {
		return fmt.Sprintf("Session not found: %s", sessionID), nil
	}

	session.mu.RLock()
	status := fmt.Sprintf("Session: %s\nRole: %s\nActor: %s\nStatus: %s\nDepth: %d/%d\nStarted: %s",
		session.ID, session.Config.Role, session.Config.ActorID, session.Status,
		session.Config.Depth, session.Config.MaxDepth, session.StartedAt.Format("15:04:05"))
	if session.Error != "" {
		status += fmt.Sprintf("\nError: %s", session.Error)
	}
	if session.Summary != "" {
		// Truncate summary for display
		summary := session.Summary
		if len(summary) > 200 {
			summary = summary[:200] + "..."
		}
		status += fmt.Sprintf("\nSummary: %s", summary)
	}
	session.mu.RUnlock()

	return status, nil
}

func (e *agentToolExecutor) executeAgentKill(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent kill not available")
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}

	if err := e.runtime.Kill(sessionID); err != nil {
		return "", fmt.Errorf("kill failed: %w", err)
	}

	return fmt.Sprintf("Killed session: %s", sessionID), nil
}

func (e *agentToolExecutor) executeAgentHierarchy(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent hierarchy not available")
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		sessionID = e.sessionID // Default to self
	}

	// Get overseer if available
	overseer, ok := e.runtime.config.SpawnHandler.(*Overseer)
	if !ok || overseer == nil {
		return "Hierarchy tracking requires overseer", nil
	}

	node := overseer.GetHierarchy(sessionID)
	if node == nil {
		return fmt.Sprintf("No hierarchy found for session: %s", sessionID), nil
	}

	// Format hierarchy as tree
	var sb strings.Builder
	e.formatHierarchyNode(&sb, node, 0)
	return sb.String(), nil
}

func (e *agentToolExecutor) formatHierarchyNode(sb *strings.Builder, node *HierarchyNode, indent int) {
	prefix := strings.Repeat("  ", indent)
	fmt.Fprintf(sb, "%s- %s (%s): %s\n", prefix, node.ActorID, node.Role, node.Status)
	for _, child := range node.Children {
		e.formatHierarchyNode(sb, child, indent+1)
	}
}

func (e *agentToolExecutor) executeAgentWait(ctx context.Context, args map[string]any) (string, error) {
	if e.runtime == nil {
		return "", fmt.Errorf("agent wait not available")
	}

	timeout := 300 * time.Second
	if ts, ok := args["timeout_seconds"].(float64); ok && ts > 0 {
		timeout = time.Duration(ts) * time.Second
	}

	waitStart := time.Now()
	waitEvent := observability.NewEvent(observability.OpAgentWait).
		WithComponent(observability.ComponentAgent).
		WithSession(e.sessionID, e.actorID).
		WithWorkspace(e.workspaceRoot).
		WithData("timeout_seconds", int(timeout.Seconds()))

	// Get overseer if available
	overseer, ok := e.runtime.config.SpawnHandler.(*Overseer)
	if !ok || overseer == nil {
		observability.Emit(ctx, waitEvent.
			WithData("error_reason", "no_overseer").
			Success(time.Since(waitStart)))
		return "Wait requires overseer for hierarchy tracking", nil
	}

	children := overseer.GetChildren(e.sessionID)
	waitEvent.WithData("children_count", len(children)).
		WithData("children_ids", children)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := overseer.WaitForChildren(ctx, e.sessionID); err != nil {
		if ctx.Err() != nil {
			observability.Emit(ctx, waitEvent.
				WithData("timeout", true).
				Canceled(time.Since(waitStart)))
			return "Timeout waiting for children", nil
		}
		observability.Emit(ctx, waitEvent.Error(err, time.Since(waitStart)))
		return "", fmt.Errorf("wait failed: %w", err)
	}

	observability.Emit(ctx, waitEvent.
		WithData("all_completed", true).
		Success(time.Since(waitStart)))
	return "All children completed", nil
}

// executeContextSearch calls code/semantic_search skill with tree format
func (e *agentToolExecutor) executeContextSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	// Call agentctl skill
	input := fmt.Sprintf(`{"query": %q, "format": "tree", "limit": %d}`, query, limit)
	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/semantic_search", "--input", input)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "context_search")
}

// executeSmartSearch calls code/smart_search skill for all-in-one search + extract
func (e *agentToolExecutor) executeSmartSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		if q, ok := args["question"].(string); ok {
			query = q
		}
	}
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	input := fmt.Sprintf(`{"question": %q, "limit": %d}`, query, limit)
	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/smart_search", "--input", input)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "smart_search")
}

// executeContextGrep calls code/context_grep for pattern search with full function bodies
func (e *agentToolExecutor) executeContextGrep(ctx context.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}

	path := "."
	if p, ok := args["path"].(string); ok && p != "" {
		path = p
	}

	input := fmt.Sprintf(`{"pattern": %q, "path": %q}`, pattern, path)
	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/context_grep", "--input", input)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "context_grep")
}

func (e *agentToolExecutor) executeRepoIndexSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		if q, ok := args["question"].(string); ok {
			query = q
		}
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := intArg(args, 20, "limit")
	workspace := strings.TrimSpace(e.workspaceRoot)
	if workspace == "" {
		workspace = "."
	}

	cmd := exec.CommandContext(ctx, "agentctl", "index", "repo", "search", "--workspace", workspace, "--query", query, "--limit", strconv.Itoa(limit))
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "repo_index_search")
}

func (e *agentToolExecutor) executeRepoIndexExpand(ctx context.Context, args map[string]any) (string, error) {
	seeds := stringSliceArg(args, "seeds", "seed")
	if len(seeds) == 0 {
		return "", fmt.Errorf("seeds are required")
	}

	edgeTypes := stringSliceArg(args, "edge_types", "edges", "edge")
	direction, _ := args["direction"].(string)
	if strings.TrimSpace(direction) == "" {
		direction = "out"
	}

	depth := intArg(args, 1, "depth")
	budget := intArg(args, 50, "budget")
	perNodeCap := intArg(args, 50, "per_node_cap", "per_node")
	workspace := strings.TrimSpace(e.workspaceRoot)
	if workspace == "" {
		workspace = "."
	}

	argsList := []string{"index", "repo", "expand", "--workspace", workspace, "--direction", direction, "--depth", strconv.Itoa(depth), "--budget", strconv.Itoa(budget), "--per-node", strconv.Itoa(perNodeCap)}
	for _, seed := range seeds {
		argsList = append(argsList, "--seed", seed)
	}
	for _, edgeType := range edgeTypes {
		argsList = append(argsList, "--edge", edgeType)
	}

	cmd := exec.CommandContext(ctx, "agentctl", argsList...)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "repo_index_expand")
}

func (e *agentToolExecutor) executeRepoIndexOpen(ctx context.Context, args map[string]any) (string, error) {
	id, _ := args["id"].(string)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("id is required")
	}

	workspace := strings.TrimSpace(e.workspaceRoot)
	if workspace == "" {
		workspace = "."
	}

	cmd := exec.CommandContext(ctx, "agentctl", "index", "repo", "open", "--workspace", workspace, "--id", id)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "repo_index_open")
}

func (e *agentToolExecutor) executeRepoIndexDagGrep(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}

	inputBytes, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal dag_grep args: %w", err)
	}

	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/dag_grep", "--input", string(inputBytes))
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "repo_index_dag_grep")
}

// executeSessionTimeline calls code/semantic_search with sessions scope and timeline format
func (e *agentToolExecutor) executeSessionTimeline(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 5
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}

	// Call semantic_search with sessions scope and timeline=true
	input := fmt.Sprintf(`{"query": %q, "scope": ["sessions"], "timeline": true, "limit": %d}`, query, limit)
	cmd := exec.CommandContext(ctx, "agentctl", "run", "code/semantic_search", "--input", input)
	cmd.Dir = e.workspaceRoot

	return commandOutput(cmd, "session_timeline")
}

func commandOutput(cmd *exec.Cmd, label string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		if strings.TrimSpace(out) != "" {
			// Preserve tool output for soft-failure paths (e.g., no results).
			return out, nil
		}
		errText := strings.TrimSpace(stderr.String())
		if errText != "" {
			return "", fmt.Errorf("%s error: %s", label, errText)
		}
		return "", fmt.Errorf("%s error: %w", label, err)
	}
	return out, nil
}

// buildToolDefsForRole returns tool definitions appropriate for the agent role.
// Tool names use underscores for Anthropic API compatibility (pattern: ^[a-zA-Z0-9_-]{1,128}$).
func buildToolDefsForRole(role types.AgentRole, hasMailbox, hasBoard bool, allowlist []string) []engine.ToolDef {
	// Base tools available to all agents
	tools := []engine.ToolDef{
		{
			Name:        "fs_read_file",
			Description: "Read the contents of a file at the given path",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path to read"}},"required":["path"]}`),
		},
		{
			Name:        "fs_list_dir",
			Description: "List files and directories in a directory",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path to list"}}}`),
		},
		{
			Name:        "code_search",
			Description: "Search for patterns in the codebase using regex",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"}},"required":["pattern"]}`),
		},
		{
			Name:        "think",
			Description: "Record your reasoning or analysis without taking action",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"thought":{"type":"string","description":"Your reasoning or analysis"}},"required":["thought"]}`),
		},
	}

	// Add role-specific tools
	switch role {
	case types.RoleCoder:
		// Only coder gets write permissions; Reviewer is read-only
		tools = append(tools, engine.ToolDef{
			Name:        "fs_write_file",
			Description: "Write content to a file",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path to write"},"content":{"type":"string","description":"Content to write"}},"required":["path","content"]}`),
		})
	case types.RoleResearcher:
		tools = append(tools,
			engine.ToolDef{
				Name:        "context_search",
				Description: "Search codebase for relevant files and symbols. Returns a tree view of matches with file paths and sizes.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Natural language query describing what to find (e.g., 'hook dispatcher implementation')"},
					"limit":{"type":"integer","description":"Maximum results to return (default 20)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "smart_search",
				Description: "All-in-one search: finds candidate files AND extracts relevant code snippets. Best for getting actual code context quickly.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"question":{"type":"string","description":"Natural language query describing what code to find"},
					"limit":{"type":"integer","description":"Maximum snippets to return (default 10)"}
				},"required":["question"]}`),
			},
			engine.ToolDef{
				Name:        "context_grep",
				Description: "Search with regex pattern, returns full function/block bodies (not just matching lines). Good for finding specific patterns with surrounding context.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"pattern":{"type":"string","description":"Regex pattern to search for"},
					"path":{"type":"string","description":"Path to search in (default: workspace root)"}
				},"required":["pattern"]}`),
			},
			engine.ToolDef{
				Name:        "repo_index_search",
				Description: "Search the repo index for nodes that match a text query.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"FTS query string"},"limit":{"type":"integer","description":"Maximum results","default":20}},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "repo_index_expand",
				Description: "Expand the repo index graph from seed node IDs.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"seeds":{"type":"array","items":{"type":"string"},"description":"Seed node IDs"},"edge_types":{"type":"array","items":{"type":"string"},"description":"Edge types to traverse"},"direction":{"type":"string","enum":["out","in"],"description":"Traversal direction"},"depth":{"type":"integer","description":"Traversal depth","default":1},"budget":{"type":"integer","description":"Max nodes to return","default":50},"per_node_cap":{"type":"integer","description":"Max edges per node per hop","default":50}},"required":["seeds"]}`),
			},
			engine.ToolDef{
				Name:        "repo_index_open",
				Description: "Open a repo index node by ID.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Node ID"}},"required":["id"]}`),
			},
			engine.ToolDef{
				Name:        "repo_index_dag_grep",
				Description: "Search and expand the repo index into a compact explanation subgraph.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Search query"},
					"mode":{"type":"string","enum":["fts","semantic","hybrid"]},
					"k":{"type":"integer","description":"Number of seed nodes (default 10)"},
					"node_kinds":{"type":"array","items":{"type":"string","enum":["symbol","file","package","concept"]}},
					"edge_sets":{"type":"array","items":{"type":"string","enum":["structural","doc","all"]}},
					"edge_types":{"type":"array","items":{"type":"string"}},
					"direction":{"type":"string","enum":["out","in"]},
					"depth":{"type":"integer","description":"Traversal depth"},
					"budget":{"type":"integer","description":"Max nodes to return"},
					"per_node_cap":{"type":"integer","description":"Max edges per node"},
					"include_anchors":{"type":"boolean","description":"Include file/package anchors"},
					"render":{"type":"string","enum":["none","tree","mermaid"]}
				},"required":["query"]}`),
			},
		)
	case types.RoleOverseer:
		// Overseer gets context gathering tools FIRST (for spawn prep)
		tools = append(tools,
			engine.ToolDef{
				Name:        "context_search",
				Description: "Search codebase for relevant files and symbols. Returns a tree view of matches with file paths and sizes. USE THIS BEFORE SPAWNING to gather context for agent prompts.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Natural language query describing what to find (e.g., 'hook dispatcher implementation')"},
					"limit":{"type":"integer","description":"Maximum results to return (default 20)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "smart_search",
				Description: "All-in-one search: finds candidate files AND extracts relevant code snippets. Best for getting actual code context quickly.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"question":{"type":"string","description":"Natural language query describing what code to find"},
					"limit":{"type":"integer","description":"Maximum snippets to return (default 10)"}
				},"required":["question"]}`),
			},
			engine.ToolDef{
				Name:        "context_grep",
				Description: "Search with regex pattern, returns full function/block bodies (not just matching lines). Good for finding specific patterns with surrounding context.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"pattern":{"type":"string","description":"Regex pattern to search for"},
					"path":{"type":"string","description":"Path to search in (default: workspace root)"}
				},"required":["pattern"]}`),
			},
			engine.ToolDef{
				Name:        "session_timeline",
				Description: "Get past session learnings related to a topic. Shows what work has been done before. USE THIS BEFORE SPAWNING to provide agents with relevant history.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Topic to search past sessions for"},
					"limit":{"type":"integer","description":"Maximum sessions to return (default 5)"}
				},"required":["query"]}`),
			},
		)

		// Agent management tools
		tools = append(tools,
			engine.ToolDef{
				Name:        "agent_spawn",
				Description: "Spawn subagents with DETAILED prompts. Include specific file paths, tool instructions, and context from context_search/session_timeline results.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"role":{"type":"string","description":"Agent role: coder, researcher, reviewer, planner","enum":["coder","researcher","reviewer","planner"]},
					"task":{"type":"string","description":"DETAILED task with: specific files, which tools to use, what to look for, success criteria"},
					"local_max_depth":{"type":"integer","description":"Maximum depth for this subtree (optional)"},
					"llm_provider":{"type":"string","description":"LLM provider: cerebras, openrouter, groq, gemini, anthropic (optional, inherits from parent)"},
					"llm_model":{"type":"string","description":"Model name (optional, uses provider default)"}
				},"required":["role","task"]}`),
			},
			engine.ToolDef{
				Name:        "agent_list",
				Description: "List all active agent sessions in the hierarchy",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			engine.ToolDef{
				Name:        "agent_status",
				Description: "Get detailed status of an agent session",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Session ID to query"}},"required":["session_id"]}`),
			},
			engine.ToolDef{
				Name:        "agent_kill",
				Description: "Terminate an agent session",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Session ID to terminate"}},"required":["session_id"]}`),
			},
			engine.ToolDef{
				Name:        "agent_hierarchy",
				Description: "Get the full agent hierarchy tree starting from a session",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Root session ID (optional, defaults to self)"}}}`),
			},
			engine.ToolDef{
				Name:        "agent_wait",
				Description: "Wait for all child agents to complete",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"timeout_seconds":{"type":"integer","description":"Timeout in seconds (default 300)"}}}`),
			},
		)
	}

	// Mailbox tools - available when MailboxStore is configured
	if hasMailbox {
		tools = append(tools,
			engine.ToolDef{
				Name:        "mail_inbox",
				Description: "Check your inbox for messages from other agents, the overseer, or human operators",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","description":"Maximum messages to return (default 10)"}}}`),
			},
			engine.ToolDef{
				Name:        "mail_send",
				Description: "Send a message to another agent, the overseer, or request human review",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"Recipient ID (e.g., 'overseer', 'human', 'agent:coder-1')"},"subject":{"type":"string","description":"Message subject"},"body":{"type":"string","description":"Message body"}},"required":["to","subject","body"]}`),
			},
			engine.ToolDef{
				Name:        "mail_ack",
				Description: "Acknowledge receipt of a message",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"message_id":{"type":"string","description":"ID of the message to acknowledge"}},"required":["message_id"]}`),
			},
		)
	}

	// Blackboard tools - available when BoardStore is configured
	if hasBoard {
		tools = append(tools,
			engine.ToolDef{
				Name:        "bb_inbox",
				Description: "Check the blackboard for coordination messages and work items in this workspace",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"unread_only":{"type":"boolean","description":"Only return unread messages (default true)"},"kind":{"type":"string","description":"Filter by message kind (e.g., 'task', 'info', 'alert')"},"limit":{"type":"integer","description":"Maximum messages to return (default 20)"}}}`),
			},
			engine.ToolDef{
				Name:        "bb_post",
				Description: "Post a message to the workspace blackboard for coordination with other agents",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"Recipient actor ID or 'broadcast' for all"},"subject":{"type":"string","description":"Message subject"},"body":{"type":"string","description":"Message body"},"kind":{"type":"string","description":"Message kind: task, info, alert, status_update"},"priority":{"type":"integer","description":"Priority 1-4 (1=low, 4=urgent)"}},"required":["to","subject","body"]}`),
			},
			engine.ToolDef{
				Name:        "bb_mark_read",
				Description: "Mark blackboard messages as read",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"message_ids":{"type":"array","items":{"type":"string"},"description":"IDs of messages to mark as read"}},"required":["message_ids"]}`),
			},
		)
	}

	if len(allowlist) > 0 {
		tools = filterToolDefs(tools, allowlist)
	}

	return tools
}

func filterToolDefs(toolDefs []engine.ToolDef, allowlist []string) []engine.ToolDef {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, entry := range allowlist {
		trimmed := strings.TrimSpace(entry)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	filtered := make([]engine.ToolDef, 0, len(toolDefs))
	for _, tool := range toolDefs {
		if _, ok := allowed[tool.Name]; ok {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// runSession executes the agent session using LLMChatEngine.
//
// Index:
// - Purpose: Drive the agent loop for a session
// - Flow: build prompts → run engine → record tool calls → persist turns → emit events
// - SideEffects: LLM calls; mailbox sends; turn persistence; observability emits
// - FailureModes: engine errors, context cancellation, persistence errors
// - Observability: emits agent iteration/complete events
// - Related: Runtime.runAutonomousContinuation, Runtime.processMailboxMessages
// - Keywords: agent_session, iterations, tool_calls, tokens_used
func (r *Runtime) runSession(ctx context.Context, session *Session) {
	sessionStart := time.Now()

	defer func() {
		if rec := recover(); rec != nil {
			session.mu.Lock()
			session.Status = types.StatusError
			session.Error = fmt.Sprintf("panic: %v", rec)
			now := time.Now()
			session.EndedAt = &now
			session.mu.Unlock()
			r.persistSessionStatus(session)

			// Emit error completion event
			observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
				WithComponent(observability.ComponentAgent).
				WithSession(session.ID, session.Config.ActorID).
				WithWorkspace(r.config.WorkspaceRoot).
				WithData("role", string(session.Config.Role)).
				WithData("iterations", session.Iterations).
				WithData("panic", true).
				Error(fmt.Errorf("panic: %v", rec), time.Since(sessionStart)))
		}
	}()

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, session.Config.Timeout)
	defer cancel()

	// Check if context is already canceled
	if ctx.Err() != nil {
		session.mu.Lock()
		session.Status = types.StatusCanceled
		session.Error = "context canceled before start"
		now := time.Now()
		session.EndedAt = &now
		session.mu.Unlock()
		r.persistSessionStatus(session)
		return
	}

	// Build the system prompt and task message
	session.SystemPrompt = agentprompt.Instruction(session.Config.Role)
	taskPrompt := buildTaskPrompt(session.Config)
	var result string

	for {
		// Build input for LLMChatEngine
		engineInput := engine.EngineInput{
			SystemPrompt: session.SystemPrompt,
			Messages: []engine.Message{
				{Role: engine.RoleUser, Content: taskPrompt},
			},
			Tools:     session.Tools,
			Workspace: r.config.WorkspaceRoot,
			SessionID: session.ID,
		}

		// Persist user turn before running engine
		turnIndex := session.nextTurnIndex()
		_ = r.saveTurn(ctx, session.ID, turnIndex, "user", taskPrompt, nil, 0)

		// Run the engine
		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			session.mu.Lock()
			now := time.Now()
			session.EndedAt = &now
			session.Status = types.StatusError
			session.Error = err.Error()
			iterations := session.Iterations
			session.mu.Unlock()
			r.persistSessionStatus(session)

			// Emit error completion event
			observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
				WithComponent(observability.ComponentAgent).
				WithSession(session.ID, session.Config.ActorID).
				WithWorkspace(r.config.WorkspaceRoot).
				WithData("role", string(session.Config.Role)).
				WithData("iterations", iterations).
				Error(err, time.Since(sessionStart)))
			return
		}

		// Extract result from output
		result = output.AssistantText

		// Record tool calls
		session.mu.Lock()
		for _, tc := range output.ToolCalls {
			session.ToolCalls = append(session.ToolCalls, types.ToolCall{
				ToolName:  tc.Name,
				Args:      parseJSONToMap(tc.Arguments),
				Timestamp: time.Now(),
			})
		}
		session.Iterations = len(session.ToolCalls)
		currentIterations := session.Iterations
		session.mu.Unlock()

		// Emit iteration event for real-time activity tracking
		toolNames := make([]string, len(output.ToolCalls))
		for i, tc := range output.ToolCalls {
			toolNames[i] = tc.Name
		}
		observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(r.config.WorkspaceRoot).
			WithData("role", string(session.Config.Role)).
			WithData("iteration", currentIterations).
			WithData("tool_calls", len(output.ToolCalls)).
			WithData("tool_names", toolNames).
			WithData("tokens_used", output.Tokens.TotalTokens).
			WithData("stop_reason", string(output.StopReason)).
			Success(0))

		// Persist assistant turn after engine run
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", result, output.ToolCalls, output.Tokens.TotalTokens)

		// Check for errors in output
		if output.StopReason == engine.StopReasonError || output.Error != "" {
			session.mu.Lock()
			now := time.Now()
			session.EndedAt = &now

			if ctx.Err() == context.Canceled {
				session.Status = types.StatusCanceled
				session.Error = "session canceled"
			} else if ctx.Err() == context.DeadlineExceeded {
				session.Status = types.StatusError
				session.Error = "session timeout"
			} else {
				session.Status = types.StatusError
				session.Error = output.Error
			}
			session.mu.Unlock()
			r.persistSessionStatus(session)
			return
		}

		// Check stop hooks
		stopResult := r.dispatchStopRequested(ctx, session, taskPrompt, result)
		if stopResult.Blocked {
			continuation := buildStopContinuation(result, stopResult.Output.Context)
			if continuation == "" {
				session.mu.Lock()
				now := time.Now()
				session.EndedAt = &now
				session.Status = types.StatusError
				session.Error = fmt.Sprintf("stop blocked without continuation: %s", stopResult.Output.Reason)
				session.mu.Unlock()
				r.persistSessionStatus(session)
				return
			}
			taskPrompt = continuation
			continue
		}

		break
	}

	// Handle exec_mode after initial prompt execution
	switch session.Config.ExecMode {
	case agent.ModeAutonomous:
		// Run autonomous continuation loop
		result = r.runAutonomousContinuation(ctx, session, result)

	case agent.ModeReactive:
		// Reactive mode: stay alive polling for messages
		// Update status to running while in message loop
		session.mu.Lock()
		session.Status = types.StatusRunning
		session.mu.Unlock()
		r.persistSessionStatus(session)

		// Enter message loop (blocks until context canceled)
		r.runMessageLoop(ctx, session, 0)

	case agent.ModeProactive:
		// Proactive mode: stay alive with think cycles + message polling
		session.mu.Lock()
		session.Status = types.StatusRunning
		session.mu.Unlock()
		r.persistSessionStatus(session)

		// Default think interval: 60 seconds
		thinkInterval := 60 * time.Second

		// Enter message loop with proactive think cycles (blocks until context canceled)
		r.runMessageLoop(ctx, session, thinkInterval)

	default:
		// Default (empty or unknown): complete immediately after initial response
	}

	// Check if context was canceled before marking OK
	session.mu.Lock()
	now := time.Now()
	session.EndedAt = &now
	if ctx.Err() == context.Canceled {
		session.Status = types.StatusCanceled
		session.Error = "session canceled"
	} else if ctx.Err() == context.DeadlineExceeded {
		session.Status = types.StatusError
		session.Error = "session timeout"
	} else {
		session.Status = types.StatusOK
	}
	session.Summary = result
	totalIterations := session.Iterations
	totalToolCalls := len(session.ToolCalls)
	session.mu.Unlock()

	r.persistSessionStatus(session)

	// Emit completion event for real-time activity tracking
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
		WithComponent(observability.ComponentAgent).
		WithSession(session.ID, session.Config.ActorID).
		WithWorkspace(r.config.WorkspaceRoot).
		WithData("role", string(session.Config.Role)).
		WithData("iterations", totalIterations).
		WithData("tool_calls", totalToolCalls).
		WithData("summary_len", len(result)).
		WithData("exec_mode", string(session.Config.ExecMode)).
		Success(time.Since(sessionStart)))
}

// parseJSONToMap parses JSON bytes into a map.
func parseJSONToMap(data json.RawMessage) map[string]any {
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}

func intArg(args map[string]any, fallback int, keys ...string) int {
	for _, key := range keys {
		if v, ok := args[key].(float64); ok && v > 0 {
			return int(v)
		}
	}
	return fallback
}

func stringSliceArg(args map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := args[key]
		if !ok || value == nil {
			continue
		}
		items := normalizeStringSlice(value)
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func normalizeStringSlice(value any) []string {
	var items []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					items = append(items, trimmed)
				}
			}
		}
	case string:
		for _, part := range strings.Split(v, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				items = append(items, trimmed)
			}
		}
	}
	return items
}

// buildTaskPrompt creates the prompt for the agent from config.
func buildTaskPrompt(cfg types.AgentConfig) string {
	// If a specific prompt is provided, use it directly
	if cfg.Prompt != "" {
		return cfg.Prompt
	}

	// Otherwise, build a generic task prompt
	prompt := fmt.Sprintf("You are a %s agent working in workspace %s.\n\n",
		cfg.Role, cfg.WorkspaceID)

	if cfg.TaskID != "" {
		prompt += fmt.Sprintf("You are working on task: %s\n\n", cfg.TaskID)
	}

	if cfg.EpicID != "" {
		prompt += fmt.Sprintf("This is part of epic: %s\n\n", cfg.EpicID)
	}

	prompt += "Please analyze the workspace and complete your assigned work."

	return prompt
}

func (r *Runtime) dispatchStopRequested(ctx context.Context, session *Session, prompt, assistantText string) hooks.Result {
	if r.config.HookDispatcher == nil {
		return hooks.Result{Output: hooks.NewApprove("no dispatcher", nil)}
	}

	input := hooks.Input{
		Event:         hooks.EventStopRequested,
		Prompt:        prompt,
		AssistantText: assistantText,
		SessionID:     session.ID,
		ActorID:       session.Config.ActorID,
		WorkspaceID:   session.Config.WorkspaceID,
		WorkspaceRoot: r.config.WorkspaceRoot,
		TraceID:       traceIDFromContext(ctx),
	}

	result, err := r.config.HookDispatcher.Dispatch(ctx, input)
	if err != nil {
		return hooks.Result{Output: hooks.NewApprove("dispatch error", nil)}
	}
	return result
}

func buildStopContinuation(result string, context string) string {
	result = strings.TrimSpace(result)
	context = strings.TrimSpace(context)

	if result == "" && context == "" {
		return ""
	}
	if result == "" {
		return context
	}
	if context == "" {
		return fmt.Sprintf("Previous response:\n%s", result)
	}
	return fmt.Sprintf("Previous response:\n%s\n\n%s", result, context)
}

// persistSessionStatus updates session status in the database.
// This is a best-effort operation - failures are logged but don't affect the session.
func (r *Runtime) persistSessionStatus(session *Session) {
	if r.config.SessionStore == nil {
		return
	}

	// Snapshot session fields under lock to avoid data race
	session.mu.RLock()
	sessionID := session.ID
	sessionStatus := session.Status
	sessionEndedAt := session.EndedAt
	sessionSummary := session.Summary
	session.mu.RUnlock()

	// Map agent status to storage status
	var status string
	switch sessionStatus {
	case types.StatusRunning:
		status = storage.SessionStatusRunning
	case types.StatusOK:
		status = storage.SessionStatusOK
	case types.StatusError:
		status = storage.SessionStatusError
	case types.StatusCanceled:
		status = storage.SessionStatusCanceled
	default:
		status = storage.SessionStatusError
	}

	// Update status
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.config.SessionStore.SetStatus(ctx, sessionID, status); err != nil {
		_ = err // TODO: Add logging
		return
	}

	// If session has ended, also update summary
	if sessionEndedAt != nil && sessionSummary != "" {
		_ = r.config.SessionStore.UpdateSummary(ctx, sessionID, sessionSummary,
			nil, nil, nil, nil, nil, nil, "")
	}
}

// RecoverSessions marks stale running sessions as interrupted on daemon restart.
// This should be called during daemon startup to clean up sessions from a previous run.
func (r *Runtime) RecoverSessions(ctx context.Context) error {
	if r.config.SessionStore == nil {
		return nil
	}

	// Query sessions marked as "running" - these were interrupted by daemon shutdown
	opts := storage.SessionListOptions{
		Statuses: []string{storage.SessionStatusRunning},
	}
	staleSessions, err := r.config.SessionStore.List(ctx, opts)
	if err != nil {
		return fmt.Errorf("list stale sessions: %w", err)
	}

	for _, s := range staleSessions {
		// Mark as error with interruption message
		if err := r.config.SessionStore.SetStatus(ctx, s.ID, storage.SessionStatusError); err != nil {
			continue // Best effort
		}
		_ = r.config.SessionStore.UpdateSummary(ctx, s.ID, "Session interrupted by daemon restart",
			nil, nil, nil, nil, nil, nil, "")
	}

	return nil
}

// Get returns a session by ID.
func (r *Runtime) Get(sessionID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.sessions[sessionID]
	return session, ok
}

// GetSpawnHandler returns the configured spawn handler (typically an Overseer).
func (r *Runtime) GetSpawnHandler() SpawnHandler {
	return r.config.SpawnHandler
}

// List returns all active sessions.
func (r *Runtime) List() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sessions := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// RegisterChild records a parent-child relationship between sessions.
// This maintains hierarchy tracking for agents spawned via direct spawn (non-overseer path).
func (r *Runtime) RegisterChild(parentSessionID, parentActorID, childSessionID, childActorID string, childDepth int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Find parent session and add child to its Children list
	if parent, ok := r.sessions[parentSessionID]; ok {
		parent.mu.Lock()
		parent.Children = append(parent.Children, childSessionID)
		parent.mu.Unlock()
	}
}

// FindSessionByActorID returns the session ID for an actor, or empty string if not found.
func (r *Runtime) FindSessionByActorID(actorID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for id, s := range r.sessions {
		if s.Config.ActorID == actorID {
			return id
		}
	}
	return ""
}

// Kill cancels and removes a session.
func (r *Runtime) Kill(sessionID string) error {
	r.mu.Lock()
	session, ok := r.sessions[sessionID]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("session not found: %s", sessionID)
	}
	r.mu.Unlock()

	// Cancel the session
	if session.cancel != nil {
		session.cancel()
	}

	session.mu.Lock()
	wasRunning := session.Status == types.StatusRunning
	if wasRunning {
		session.Status = types.StatusCanceled
		now := time.Now()
		session.EndedAt = &now
	}
	session.mu.Unlock()

	// Persist status change to database
	if wasRunning {
		r.persistSessionStatus(session)
	}

	return nil
}

// GetSession returns the session state.
func (s *Session) GetSession() types.AgentSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return types.AgentSession{
		ID:         s.ID,
		JobID:      "", // TODO: link to jobs store
		Config:     s.Config,
		Status:     s.Status,
		StartedAt:  s.StartedAt,
		EndedAt:    s.EndedAt,
		Iterations: s.Iterations,
		Summary:    s.Summary,
		Error:      s.Error,
	}
}

// GetToolCalls returns a copy of tool calls.
func (s *Session) GetToolCalls() []types.ToolCall {
	s.mu.RLock()
	defer s.mu.RUnlock()

	calls := make([]types.ToolCall, len(s.ToolCalls))
	copy(calls, s.ToolCalls)
	return calls
}

// Helper functions for tool execution

// readFileWithLimit reads a file up to the given size limit.
func readFileWithLimit(path string, maxSize int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check file size
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() > maxSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", stat.Size(), maxSize)
	}

	return io.ReadAll(f)
}

// listDirEntries lists directory entries with basic info.
func listDirEntries(path string) ([]map[string]any, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		entry := map[string]any{
			"name":  e.Name(),
			"isDir": e.IsDir(),
		}
		if err == nil {
			entry["size"] = info.Size()
		}
		result = append(result, entry)
	}
	return result, nil
}

// writeFileSafe writes content to a file, creating parent directories if needed.
func writeFileSafe(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, content, 0o644)
}

// simpleGrep performs a simple grep search using the system grep command.
func simpleGrep(root, pattern string) (string, error) {
	// Use "--" to prevent pattern from being interpreted as a flag if it starts with "-"
	// Add timeout to prevent hanging on large codebases
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "grep", "-rn", "--include=*.go", "--include=*.ts", "--include=*.js", "--include=*.py", "--", pattern, root)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("grep timed out after 30 seconds")
		}
		// grep returns exit code 1 when no matches found - that's not an error
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		return "", fmt.Errorf("grep failed: %s", stderr.String())
	}

	// Limit output to first 50 lines
	lines := strings.Split(stdout.String(), "\n")
	originalCount := len(lines)
	if originalCount > 50 {
		lines = lines[:50]
		lines = append(lines, fmt.Sprintf("... and %d more lines", originalCount-50))
	}

	return strings.Join(lines, "\n"), nil
}

// truncate returns s truncated to maxLen characters with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// saveTurn persists a turn to the session store.
func (r *Runtime) saveTurn(ctx context.Context, sessionID string, turnIndex int, role, content string, toolCalls []engine.ToolCall, tokensUsed int) error {
	if r.config.SessionStore == nil {
		return nil // No store configured, skip persistence
	}

	// Convert engine tool calls to storage format
	storageTCs := make([]storage.ToolCall, len(toolCalls))
	for i, tc := range toolCalls {
		storageTCs[i] = storage.ToolCall{
			Name:    tc.Name,
			Success: true, // Assume success if we got this far
		}
	}

	// Store full content in CAS if available
	var contentCASDigest string
	if r.config.CASStore != nil && content != "" {
		obj, err := r.config.CASStore.Put(ctx, strings.NewReader(content), "text/plain", []string{"agent-turn", sessionID})
		if err == nil {
			contentCASDigest = obj.Digest
		}
		// Don't fail if CAS storage fails - content preview is still saved
	}

	turn := storage.SessionTurn{
		SessionID:        sessionID,
		TurnIndex:        turnIndex,
		Role:             role,
		ContentPreview:   truncate(content, 500),
		ContentCASDigest: contentCASDigest,
		ToolCalls:        storageTCs,
		TokensUsed:       tokensUsed,
		Timestamp:        time.Now(),
	}

	_, err := r.config.SessionStore.SaveTurn(ctx, turn)
	return err
}

// GetTurns retrieves turns for a session.
func (r *Runtime) GetTurns(ctx context.Context, sessionID string) ([]storage.SessionTurn, error) {
	if r.config.SessionStore == nil {
		return nil, fmt.Errorf("no session store configured")
	}

	return r.config.SessionStore.GetTurns(ctx, sessionID, storage.SessionTurnListOptions{
		SessionID: sessionID,
		Limit:     1000, // Get all turns
	})
}

// Resume continues a previous session with an additional prompt.
// It loads the previous turns and builds messages from them.
func (r *Runtime) Resume(ctx context.Context, sessionID string, additionalPrompt string) (*Session, error) {
	// Load previous session from store
	if r.config.SessionStore == nil {
		return nil, fmt.Errorf("no session store configured")
	}

	prevSession, err := r.config.SessionStore.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load session: %w", err)
	}

	// Load turns
	turns, err := r.GetTurns(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("load turns: %w", err)
	}

	// Build the prompt including previous context
	var promptBuilder strings.Builder
	promptBuilder.WriteString("PREVIOUS CONVERSATION:\n\n")

	for _, turn := range turns {
		// Prefer full content from CAS if available
		content := turn.ContentPreview
		if turn.ContentCASDigest != "" && r.config.CASStore != nil {
			if reader, _, err := r.config.CASStore.Get(ctx, turn.ContentCASDigest); err == nil {
				if fullContent, err := io.ReadAll(reader); err == nil {
					content = string(fullContent)
				}
				reader.Close()
			}
			// Fall back to ContentPreview if CAS fetch fails
		}
		promptBuilder.WriteString(fmt.Sprintf("[%s]: %s\n\n", strings.ToUpper(turn.Role), content))
	}

	promptBuilder.WriteString("---\n\nCONTINUATION REQUEST:\n\n")
	promptBuilder.WriteString(additionalPrompt)

	// Create new session config based on previous
	cfg := types.AgentConfig{
		Role:             types.AgentRole(prevSession.AgentType),
		WorkspaceID:      prevSession.WorkspacePath,
		Prompt:           promptBuilder.String(),
		MaxIterations:    10, // Default for continuation
		MaxContextTokens: 50000,
		LLMProvider:      prevSession.LLMProvider,
		LLMModel:         prevSession.LLMModel,
	}

	// Generate new actor ID for continuation
	cfg.ActorID = fmt.Sprintf("actor:%s:%s", cfg.Role, ulid.Make().String())

	// Spawn the continuation session
	newSession, err := r.Spawn(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn continuation: %w", err)
	}

	// Link sessions via edge
	if r.config.SessionStore != nil {
		edge := storage.SessionEdge{
			Workspace:   prevSession.WorkspacePath,
			FromSession: sessionID,
			ToSession:   newSession.ID,
			EdgeType:    "continues",
		}
		_ = r.config.SessionStore.SaveEdge(ctx, edge)
	}

	return newSession, nil
}

// containsTaskComplete reports whether the agent output signals task completion.
// It returns true when the trimmed result equals the token "TASK_COMPLETE" or
// when the trimmed result is a short completion phrase ("Task completed", "Done",
// or "Complete") shorter than 100 characters.
func containsTaskComplete(result string) bool {
	trimmed := strings.TrimSpace(result)
	return len(trimmed) > 0 && (trimmed == "TASK_COMPLETE" ||
		len(trimmed) < 100 && (trimmed == "Task completed" || trimmed == "Done" || trimmed == "Complete"))
}

// containsNoWork reports whether the proactive agent indicated there is no work to do.
// It returns true if the trimmed result equals "NO_WORK_NEEDED" or "No work needed".
func containsNoWork(result string) bool {
	trimmed := strings.TrimSpace(result)
	return trimmed == "NO_WORK_NEEDED" || trimmed == "No work needed"
}

// runAutonomousContinuation allows the agent to continue working across multiple turns
// without needing a new external message. Used for autonomous mode.
//
// Index:
// - Purpose: Continue autonomous turns until completion or max turns
// - Flow: emit turn event → check completion → run engine → persist turns → repeat
// - SideEffects: LLM calls; turn persistence; observability emits
// - FailureModes: engine errors, context cancellation
// - Observability: emits agent.autonomous_turn and iteration events
// - Related: containsTaskComplete, Runtime.runSession
// - Keywords: autonomous_turn, max_auto_turns, continuation
func (r *Runtime) runAutonomousContinuation(ctx context.Context, session *Session, lastResult string) string {
	maxTurns := session.Config.MaxAutoTurns
	if maxTurns <= 1 {
		return lastResult // Autonomous continuation disabled
	}

	result := lastResult

	for turn := 1; turn < maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return result
		default:
		}

		// Emit autonomous turn event
		observability.Emit(ctx, observability.NewEvent("agent.autonomous_turn").
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(r.config.WorkspaceRoot).
			WithData("turn", turn+1).
			WithData("max_turns", maxTurns).
			Success(0))

		// Check if agent already signaled completion
		if containsTaskComplete(result) {
			break
		}

		continuePrompt := "Continue working on the previous task if there is more to do. If the task is complete, respond with 'TASK_COMPLETE'. Do not repeat already completed work."

		// Persist user turn before running engine
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", continuePrompt, nil, 0)

		engineInput := engine.EngineInput{
			SystemPrompt: session.SystemPrompt,
			Messages: []engine.Message{
				{Role: engine.RoleUser, Content: continuePrompt},
			},
			Tools:     session.Tools,
			Workspace: r.config.WorkspaceRoot,
			SessionID: session.ID,
		}

		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			break
		}

		result = output.AssistantText

		// Record tool calls for this continuation turn
		session.mu.Lock()
		for _, tc := range output.ToolCalls {
			session.ToolCalls = append(session.ToolCalls, types.ToolCall{
				ToolName:  tc.Name,
				Args:      parseJSONToMap(tc.Arguments),
				Timestamp: time.Now(),
			})
		}
		session.Iterations = len(session.ToolCalls)
		currentIterations := session.Iterations
		session.mu.Unlock()

		// Emit iteration event for real-time activity tracking
		toolNames := make([]string, len(output.ToolCalls))
		for i, tc := range output.ToolCalls {
			toolNames[i] = tc.Name
		}
		observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(r.config.WorkspaceRoot).
			WithData("role", string(session.Config.Role)).
			WithData("iteration", currentIterations).
			WithData("tool_calls", len(output.ToolCalls)).
			WithData("tool_names", toolNames).
			WithData("tokens_used", output.Tokens.TotalTokens).
			WithData("autonomous_turn", turn+1).
			Success(0))

		// Persist assistant turn after engine run
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", result, output.ToolCalls, output.Tokens.TotalTokens)

		if containsTaskComplete(result) {
			break
		}
	}

	return result
}

// runMessageLoop polls for mailbox messages and processes them.
// Used for reactive and proactive modes to keep agents alive.
//
// Index:
// - Purpose: Poll mailbox and trigger proactive think cycles
// - Flow: poll ticker → process messages → optional think ticker
// - SideEffects: mailbox reads; engine runs; turn persistence
// - FailureModes: mailbox errors, engine errors
// - Related: Runtime.processMailboxMessages, Runtime.runProactiveThink
// - Keywords: mailbox_poll, proactive, think_interval
func (r *Runtime) runMessageLoop(ctx context.Context, session *Session, thinkInterval time.Duration) {
	if r.config.MailboxStore == nil {
		return
	}

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()

	var thinkTicker *time.Ticker
	var thinkChan <-chan time.Time
	isProactive := session.Config.ExecMode == agent.ModeProactive

	if isProactive && thinkInterval > 0 {
		thinkTicker = time.NewTicker(thinkInterval)
		thinkChan = thinkTicker.C
		defer thinkTicker.Stop()
	}

	// Extract namespace from ActorID (format: actor:<role>:<id>)
	actorNS := session.Config.ActorID

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			r.processMailboxMessages(ctx, session, actorNS)
		case <-thinkChan:
			r.runProactiveThink(ctx, session)
		}
	}
}

// processMailboxMessages checks and processes pending mailbox messages.
//
// Index:
// - Purpose: Consume mailbox messages and run agent responses
// - Flow: list messages → build prompt → run engine → record tool calls → emit events
// - SideEffects: LLM calls; observability emits; turn persistence
// - FailureModes: mailbox list errors, engine errors
// - Observability: emits agent.mailbox_error and iteration events
// - Related: Runtime.runMessageLoop
// - Keywords: mailbox, agent.mailbox_error, tool_calls
func (r *Runtime) processMailboxMessages(ctx context.Context, session *Session, actorNS string) {
	if r.config.MailboxStore == nil {
		return
	}

	messages, err := r.config.MailboxStore.List(ctx, actorNS, 10)
	if err != nil {
		return
	}

	for _, msg := range messages {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Process message - extract body from Payload
		payloadStr := string(msg.Payload)
		prompt := fmt.Sprintf("You received a message:\nFrom: %s\nPayload: %s\n\nRespond appropriately.", msg.FromNS, payloadStr)

		// Persist user turn before running engine
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", prompt, nil, 0)

		engineInput := engine.EngineInput{
			SystemPrompt: session.SystemPrompt,
			Messages: []engine.Message{
				{Role: engine.RoleUser, Content: prompt},
			},
			Tools:     session.Tools,
			Workspace: r.config.WorkspaceRoot,
			SessionID: session.ID,
		}

		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("agent.mailbox_error").
				WithComponent(observability.ComponentAgent).
				WithSession(session.ID, session.Config.ActorID).
				WithWorkspace(r.config.WorkspaceRoot).
				Error(err, 0))
			continue
		}

		// Record tool calls
		session.mu.Lock()
		for _, tc := range output.ToolCalls {
			session.ToolCalls = append(session.ToolCalls, types.ToolCall{
				ToolName:  tc.Name,
				Args:      parseJSONToMap(tc.Arguments),
				Timestamp: time.Now(),
			})
		}
		session.Iterations = len(session.ToolCalls)
		currentIterations := session.Iterations
		session.mu.Unlock()

		// Emit iteration event for real-time activity tracking
		toolNames := make([]string, len(output.ToolCalls))
		for i, tc := range output.ToolCalls {
			toolNames[i] = tc.Name
		}
		observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(r.config.WorkspaceRoot).
			WithData("role", string(session.Config.Role)).
			WithData("iteration", currentIterations).
			WithData("tool_calls", len(output.ToolCalls)).
			WithData("tool_names", toolNames).
			WithData("tokens_used", output.Tokens.TotalTokens).
			WithData("message_source", "mailbox").
			Success(0))

		// Persist assistant turn after engine run
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", output.AssistantText, output.ToolCalls, output.Tokens.TotalTokens)

		// Ack the message
		_ = r.config.MailboxStore.Ack(ctx, msg.ID)
	}
}

// runProactiveThink runs a periodic "think" cycle for proactive agents.
//
// Index:
// - Purpose: Trigger proactive agent cycles when idle
// - Flow: build think prompt → run engine → persist turns → emit events
// - SideEffects: LLM calls; turn persistence; observability emits
// - FailureModes: engine errors
// - Observability: emits agent proactive iteration events
// - Related: Runtime.runMessageLoop
// - Keywords: proactive, think_prompt, iterations
func (r *Runtime) runProactiveThink(ctx context.Context, session *Session) {
	thinkPrompt := `You are in proactive mode. Check if there is any work that needs to be done:
1. Review any pending tasks or todos
2. Check for any issues that need attention
3. Look for opportunities to help

If there is work to do, start working on the highest priority item.
If there is nothing to do, respond with 'NO_WORK_NEEDED'.`

	// Persist user turn before running engine
	_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", thinkPrompt, nil, 0)

	engineInput := engine.EngineInput{
		SystemPrompt: session.SystemPrompt,
		Messages: []engine.Message{
			{Role: engine.RoleUser, Content: thinkPrompt},
		},
		Tools:     session.Tools,
		Workspace: r.config.WorkspaceRoot,
		SessionID: session.ID,
	}

	output, err := session.Engine.Run(ctx, engineInput)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("agent.proactive_think_error").
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(r.config.WorkspaceRoot).
			Error(err, 0))
		return
	}

	// Persist assistant turn after engine run
	_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", output.AssistantText, output.ToolCalls, output.Tokens.TotalTokens)

	if containsNoWork(output.AssistantText) {
		return
	}

	// Record tool calls
	session.mu.Lock()
	for _, tc := range output.ToolCalls {
		session.ToolCalls = append(session.ToolCalls, types.ToolCall{
			ToolName:  tc.Name,
			Args:      parseJSONToMap(tc.Arguments),
			Timestamp: time.Now(),
		})
	}
	session.Iterations = len(session.ToolCalls)
	currentIterations := session.Iterations
	session.mu.Unlock()

	// Emit iteration event for real-time activity tracking
	toolNames := make([]string, len(output.ToolCalls))
	for i, tc := range output.ToolCalls {
		toolNames[i] = tc.Name
	}
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
		WithComponent(observability.ComponentAgent).
		WithSession(session.ID, session.Config.ActorID).
		WithWorkspace(r.config.WorkspaceRoot).
		WithData("role", string(session.Config.Role)).
		WithData("iteration", currentIterations).
		WithData("tool_calls", len(output.ToolCalls)).
		WithData("tool_names", toolNames).
		WithData("tokens_used", output.Tokens.TotalTokens).
		WithData("proactive_think", true).
		Success(0))

	// Emit proactive work event
	observability.Emit(ctx, observability.NewEvent("agent.proactive_work").
		WithComponent(observability.ComponentAgent).
		WithSession(session.ID, session.Config.ActorID).
		WithWorkspace(r.config.WorkspaceRoot).
		Success(0))

	// If work was started and autonomous continuation is enabled
	if session.Config.MaxAutoTurns > 1 {
		r.runAutonomousContinuation(ctx, session, output.AssistantText)
	}
}
