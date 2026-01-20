// Package runtime provides the agent runtime using agentctl's LLMChatEngine.
package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/hooks"
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

	// MailboxStore provides access to inter-agent mailbox messaging.
	// When nil, mailbox tools are not available.
	MailboxStore MailboxStore

	// BoardStore provides access to workspace blackboard coordination.
	// When nil, blackboard tools are not available.
	BoardStore BoardStore
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
	ID         string
	Config     types.AgentConfig
	Status     types.AgentStatus
	Engine     *engine.LLMChatEngine
	Tools      []engine.ToolDef
	StartedAt  time.Time
	EndedAt    *time.Time
	Iterations int
	Summary    string
	Error      string
	ToolCalls  []types.ToolCall
	Children   []string // IDs of spawned child sessions
	cancel     context.CancelFunc
	mu         sync.RWMutex
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
		dbSession := storage.Session{
			ID:            sessionID,
			WorkspacePath: r.config.WorkspaceRoot,
			AgentID:       cfg.ActorID,
			AgentType:     string(cfg.Role),
			Status:        storage.SessionStatusRunning,
			StartedAt:     session.StartedAt,
		}
		if _, err := r.config.SessionStore.Save(ctx, dbSession); err != nil {
			// Log but don't fail - in-memory tracking is sufficient
			// The session will just not be visible in agentctl session list
			_ = err // TODO: Add logging
		}
	}

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
		model = defaultModelForProvider(provider)
	}

	// Create LLMChatEngine config - it will auto-detect provider/key from env if not specified
	engineCfg := engine.LLMChatConfig{
		Provider:       provider,
		Model:          model,
		MaxIterations:  cfg.MaxIterations,
		Timeout:        cfg.Timeout,
		HookDispatcher: r.config.HookDispatcher,
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
		Workspace:   r.config.WorkspaceRoot,
		WorkspaceID: cfg.WorkspaceID,
		SessionID:   sessionID,
		ActorID:     cfg.ActorID,
	}
	toolRunner := engine.NewToolRunner(executor, r.config.HookDispatcher, runnerCfg)
	llmEngine.SetToolRunner(toolRunner)

	return llmEngine, toolDefs, nil
}

// createToolExecutor creates a ToolExecutor adapter for the agent tools registry.
func (r *Runtime) createToolExecutor(cfg types.AgentConfig, sessionID string) (engine.ToolExecutor, []engine.ToolDef) {
	// Build tool definitions based on agent role and available stores
	toolDefs := buildToolDefsForRole(cfg.Role, r.config.MailboxStore != nil, r.config.BoardStore != nil)

	// Create the executor adapter
	executor := &agentToolExecutor{
		workspaceRoot:     r.config.WorkspaceRoot,
		workspaceID:       cfg.WorkspaceID,
		sessionID:         sessionID,
		actorID:           cfg.ActorID,
		depth:             cfg.Depth,
		maxDepth:          cfg.MaxDepth,
		localMaxDepth:     cfg.LocalMaxDepth,
		agentRole:         string(cfg.Role),
		hookDispatcher:    r.config.HookDispatcher,
		openMemoryStore:   r.config.OpenMemoryStore,
		mailboxStore:      r.config.MailboxStore,
		boardStore:        r.config.BoardStore,
		toolDefs:          toolDefs,
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

// buildToolDefsForRole returns tool definitions appropriate for the agent role.
// Tool names use underscores for Anthropic API compatibility (pattern: ^[a-zA-Z0-9_-]{1,128}$).
func buildToolDefsForRole(role types.AgentRole, hasMailbox, hasBoard bool) []engine.ToolDef {
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

	return tools
}

// defaultModelForProvider returns the default model for a given LLM provider.
func defaultModelForProvider(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4.1-mini"
	case "gemini", "":
		return "gemini-2.0-flash"
	case "anthropic":
		return "claude-3-5-haiku-20241022"
	case "groq":
		return "llama-3.1-70b-versatile"
	case "openrouter":
		return "anthropic/claude-3.5-haiku" // OpenRouter alias for latest 3.5 Haiku
	default:
		return "gemini-2.0-flash"
	}
}

// runSession executes the agent session using LLMChatEngine.
func (r *Runtime) runSession(ctx context.Context, session *Session) {
	defer func() {
		if rec := recover(); rec != nil {
			session.mu.Lock()
			session.Status = types.StatusError
			session.Error = fmt.Sprintf("panic: %v", rec)
			now := time.Now()
			session.EndedAt = &now
			session.mu.Unlock()
			r.persistSessionStatus(session)
		}
	}()

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, session.Config.Timeout)
	defer cancel()

	// Build the system prompt and task message
	systemPrompt := agentprompt.Instruction(session.Config.Role)
	taskPrompt := buildTaskPrompt(session.Config)
	var result string

	for {
		// Build input for LLMChatEngine
		engineInput := engine.EngineInput{
			SystemPrompt: systemPrompt,
			Messages: []engine.Message{
				{Role: engine.RoleUser, Content: taskPrompt},
			},
			Tools:     session.Tools,
			Workspace: r.config.WorkspaceRoot,
			SessionID: session.ID,
		}

		// Run the engine
		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			session.mu.Lock()
			now := time.Now()
			session.EndedAt = &now
			session.Status = types.StatusError
			session.Error = err.Error()
			session.mu.Unlock()
			r.persistSessionStatus(session)
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
		session.mu.Unlock()

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

	session.mu.Lock()
	now := time.Now()
	session.EndedAt = &now
	session.Status = types.StatusOK
	session.Summary = result
	session.mu.Unlock()

	r.persistSessionStatus(session)
}

// parseJSONToMap parses JSON bytes into a map.
func parseJSONToMap(data json.RawMessage) map[string]any {
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	return os.WriteFile(path, content, 0644)
}

// buildAgentSignature creates a dspy-go Signature for the agent role.
// This is used by tests to validate agent configuration.
func buildAgentSignature(cfg types.AgentConfig) *core.Signature {
	var instruction string

	switch cfg.Role {
	case types.RoleCoder:
		instruction = `You are a coding agent that implements features and fixes bugs.

Code Search & Retrieval Tools:
- code.symbol_search to find relevant symbols (functions, classes, types)
- code.swe_grep for fast regex search across the codebase
- code.search for semantic code search

File Operations:
- fs.read_file to read file contents
- fs.list_dir to explore directory structure

Edit Tools:
- edit.create_file to create new files
- edit.apply_patch for simple edits
- edit.apply_structured_diff for complex multi-location edits

Testing:
- tests.run to verify changes

Workflow:
1. Use code.symbol_search to find relevant symbols before making changes
2. Read and understand the code before editing
3. Make minimal, focused changes
4. Run tests to verify your changes`

	case types.RolePlanner:
		instruction = `You are a planning agent that orchestrates tasks and coordinates work.

Task Management:
- todo.add to create new tasks
- todo.query to search existing tasks
- todo.graph_insights to analyze task dependencies

Communication:
- mail.send to coordinate with other agents

Focus on high-level orchestration rather than implementation details.`

	case types.RoleReviewer:
		instruction = `You are a code review agent that inspects code quality without modifying it.

Code Search & Retrieval Tools:
- code.symbol_search for finding definitions and usages
- code.swe_grep for pattern-based search
- code.search for semantic search

File Operations:
- fs.read_file to inspect file contents
- fs.list_dir to navigate the codebase

Validation:
- tests.run to verify code behavior

Coordination:
- mail.send to communicate findings
- todo.add to create follow-up tasks

Workflow:
1. Search and inspect relevant code
2. Identify issues and improvements
3. Document findings and create tasks
4. Communicate with the team

IMPORTANT: You do not directly apply edits. Instead, report findings and create tasks for coders.`

	default:
		instruction = `You are a helpful agent that assists with general tasks.`
	}

	return &core.Signature{
		Instruction: instruction,
		Inputs: []core.InputField{
			{Field: core.Field{Name: "task", Description: "The task to perform"}},
		},
		Outputs: []core.OutputField{
			{Field: core.Field{Name: "result", Description: "The result of the task"}},
		},
	}
}

// simpleGrep performs a simple grep search using the system grep command.
func simpleGrep(root, pattern string) (string, error) {
	// Use "--" to prevent pattern from being interpreted as a flag if it starts with "-"
	cmd := exec.Command("grep", "-rn", "--include=*.go", "--include=*.ts", "--include=*.js", "--include=*.py", "--", pattern, root)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
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
