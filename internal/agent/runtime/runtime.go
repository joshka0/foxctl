package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"github.com/jkatigb/agentctl/internal/agent/toolnames"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
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

func (r *Runtime) workspaceRootForConfig(cfg types.AgentConfig) string {
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		return cfg.WorkspaceRoot
	}
	return r.config.WorkspaceRoot
}

func (r *Runtime) workspaceRootForSession(session *Session) string {
	if session == nil {
		return r.config.WorkspaceRoot
	}
	return r.workspaceRootForConfig(session.Config)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
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

	// LLMBaseURL overrides the base URL for OpenAI-compatible/self-hosted backends.
	LLMBaseURL string

	// LLMAuthMode controls auth mode: auto, none, bearer, header.
	LLMAuthMode string

	// LLMAuthHeader names the auth header when auth mode is header.
	LLMAuthHeader string

	// LLMAuthPrefix prefixes the API key for bearer/header auth.
	LLMAuthPrefix string

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
	Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error
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
	// ConversationHistory accumulates user/assistant messages across engine Run() calls
	// so that follow-up turns (autonomous continuation, mailbox ask) have full context.
	ConversationHistory []engine.Message
	endTickRequested    bool
	cancel              context.CancelFunc
	done                chan struct{} // closed when runSession exits
	mu                  sync.RWMutex
}

// nextTurnIndex atomically increments and returns the next turn index for this session.
func (s *Session) nextTurnIndex() int {
	return int(atomic.AddUint64(&s.TurnCounter, 1) - 1)
}

// buildEngineInput creates an EngineInput that includes the session's accumulated
// conversation history plus a new user prompt. This ensures follow-up engine calls
// (autonomous continuation, mailbox ask) have context from prior turns.
func (r *Runtime) buildEngineInput(session *Session, userPrompt string) engine.EngineInput {
	session.mu.RLock()
	history := make([]engine.Message, len(session.ConversationHistory))
	copy(history, session.ConversationHistory)
	session.mu.RUnlock()

	messages := append(history, engine.Message{Role: engine.RoleUser, Content: userPrompt})
	return engine.EngineInput{
		SystemPrompt: session.SystemPrompt,
		Messages:     messages,
		Tools:        session.Tools,
		Workspace:    r.workspaceRootForSession(session),
		SessionID:    session.ID,
	}
}

// appendToHistory records a user/assistant exchange in the session's conversation history.
func appendToHistory(session *Session, userPrompt, assistantText string) {
	session.mu.Lock()
	session.ConversationHistory = append(session.ConversationHistory,
		engine.Message{Role: engine.RoleUser, Content: userPrompt},
		engine.Message{Role: engine.RoleAssistant, Content: assistantText},
	)
	session.mu.Unlock()
}

// NewRuntime creates a new agent runtime.
func NewRuntime(cfg Config) *Runtime {
	if cfg.DefaultMaxIterations <= 0 {
		cfg.DefaultMaxIterations = 20
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
	workspaceRoot := r.workspaceRootForConfig(cfg)
	// Generate session ID
	sessionID := ulid.Make().String()

	// Apply defaults
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = r.config.DefaultMaxIterations
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = r.config.DefaultTimeout
	}
	if cfg.Timeout < 0 {
		cfg.Timeout = 0
	}
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = r.config.LLMProvider
	}
	if cfg.LLMModel == "" {
		cfg.LLMModel = r.config.LLMModel
	}
	if cfg.LLMBaseURL == "" {
		cfg.LLMBaseURL = r.config.LLMBaseURL
	}
	if cfg.LLMAuthMode == "" {
		cfg.LLMAuthMode = r.config.LLMAuthMode
	}
	if cfg.LLMAuthHeader == "" {
		cfg.LLMAuthHeader = r.config.LLMAuthHeader
	}
	if cfg.LLMAuthPrefix == "" {
		cfg.LLMAuthPrefix = r.config.LLMAuthPrefix
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
		done:      make(chan struct{}),
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
			WorkspacePath: workspaceRoot,
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
		WithWorkspace(workspaceRoot).
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
	workspaceRoot := r.workspaceRootForConfig(cfg)
	// Resolve provider: agent → runtime → auto-detect from env
	provider := cfg.LLMProvider
	if provider == "" {
		provider = r.config.LLMProvider
	}
	if cfg.ExecMode == agent.ModeTick {
		provider = "lmstudio"
	}

	// Resolve model: agent → runtime → provider-specific default
	model := cfg.LLMModel
	if model == "" {
		model = r.config.LLMModel
	}
	if cfg.ExecMode == agent.ModeTick && model == "" {
		model = llmproviders.DefaultModelForProvider("lmstudio")
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
		APIKey:           firstNonEmpty(cfg.LLMAPIKey, r.config.LLMAPIKey),
		BaseURL:          firstNonEmpty(cfg.LLMBaseURL, r.config.LLMBaseURL),
		AuthMode:         firstNonEmpty(cfg.LLMAuthMode, r.config.LLMAuthMode),
		AuthHeader:       firstNonEmpty(cfg.LLMAuthHeader, r.config.LLMAuthHeader),
		AuthPrefix:       firstNonEmpty(cfg.LLMAuthPrefix, r.config.LLMAuthPrefix),
		Model:            model,
		MaxIterations:    cfg.MaxIterations,
		MaxContextTokens: maxContextTokens,
		Timeout:          cfg.Timeout,
		SynthesisReserve: 2,
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
		WorkspaceRoot: workspaceRoot,
	})

	// Create tool executor adapter and get tool definitions
	executor, toolDefs := r.createToolExecutor(cfg, sessionID)

	// Create ToolRunner with the executor
	runnerCfg := engine.ToolRunnerConfig{
		Workspace:      workspaceRoot,
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
	workspaceRoot := r.workspaceRootForConfig(cfg)
	// Build tool definitions based on agent role and available stores
	toolDefs := buildToolDefsForRole(cfg.Role, r.config.MailboxStore != nil, r.config.BoardStore != nil, cfg.SkillsAllow)

	// Create the executor adapter
	executor := &agentToolExecutor{
		workspaceRoot:   workspaceRoot,
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
		llmProvider:     cfg.LLMProvider,
		llmModel:        cfg.LLMModel,
		endTick: func(ctx context.Context) error {
			return r.requestEndTick(ctx, sessionID)
		},
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
	llmProvider     string // Agent's configured LLM provider
	llmModel        string // Agent's configured LLM model
	endTick         func(context.Context) error
	// runtime is set for overseer agents to enable agent management
	runtime *Runtime
}

// Execute runs a tool by name with the given arguments.
func (e *agentToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	fmt.Fprintf(os.Stderr, "[TOOLEXEC] raw_name=%s raw_args=%s\n", name, strings.TrimSpace(string(args)))
	// Parse args into map
	var argsMap map[string]any
	if len(args) > 0 {
		if err := json.Unmarshal(args, &argsMap); err != nil {
			return "", fmt.Errorf("parse args: %w", err)
		}
	}

	canonicalName, ok := toolnames.CanonicalizeToolName(toolnames.ToolModeRuntime, name)
	if !ok {
		fmt.Fprintf(os.Stderr, "[TOOLEXEC] unknown_tool raw_name=%s\n", name)
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	name = canonicalName
	fmt.Fprintf(os.Stderr, "[TOOLEXEC] canonical_name=%s\n", name)

	// Execute based on tool name (using underscores for Anthropic API compatibility)
	var (
		out string
		err error
	)
	switch name {
	case "fs_read_file":
		out, err = e.executeReadFile(ctx, argsMap)
	case "fs_list_dir":
		out, err = e.executeListDir(ctx, argsMap)
	case "fs_write_file":
		out, err = e.executeWriteFile(ctx, argsMap)
	case "code_search":
		out, err = e.executeCodeSearch(ctx, argsMap)
	case "think":
		out, err = e.executeThink(ctx, argsMap)
	case "end_tick":
		out, err = e.executeEndTick(ctx, argsMap)
	// Mailbox tools
	case "mail_inbox":
		out, err = e.executeMailInbox(ctx, argsMap)
	case "mail_send":
		out, err = e.executeMailSend(ctx, argsMap)
	case "mail_ack":
		out, err = e.executeMailAck(ctx, argsMap)
	// Blackboard tools
	case "bb_inbox":
		out, err = e.executeBBInbox(ctx, argsMap)
	case "bb_post":
		out, err = e.executeBBPost(ctx, argsMap)
	case "bb_mark_read":
		out, err = e.executeBBMarkRead(ctx, argsMap)
	// Overseer context gathering tools
	case "context_search":
		out, err = e.executeContextSearch(ctx, argsMap)
	case "session_timeline":
		out, err = e.executeSessionTimeline(ctx, argsMap)
	case "smart_search":
		out, err = e.executeSmartSearch(ctx, argsMap)
	case "context_grep":
		out, err = e.executeContextGrep(ctx, argsMap)
	case "code_symbols":
		out, err = e.executeCodeSymbols(ctx, argsMap)
	case "memory_query":
		out, err = e.executeMemoryQuery(ctx, argsMap)
	case "session_recall":
		out, err = e.executeSessionRecall(ctx, argsMap)
	case "annotation_recall":
		out, err = e.executeAnnotationRecall(ctx, argsMap)
	case "annotation_list_sessions":
		out, err = e.executeAnnotationListSessions(ctx)
	case "annotation_category_stats":
		out, err = e.executeAnnotationCategoryStats(ctx, argsMap)
	case "repo_index_search":
		out, err = e.executeRepoIndexSearch(ctx, argsMap)
	case "repo_index_expand":
		out, err = e.executeRepoIndexExpand(ctx, argsMap)
	case "repo_index_open":
		out, err = e.executeRepoIndexOpen(ctx, argsMap)
	case "repo_index_dag_grep":
		out, err = e.executeRepoIndexDagGrep(ctx, argsMap)
	case "context_show":
		out, err = e.executeContextShow(ctx, argsMap)
	case "context_retrieve":
		out, err = e.executeContextRetrieve(ctx, argsMap)
	case "obsidian_index_search":
		out, err = e.executeObsidianIndexSearch(ctx, argsMap)
	case "obsidian_read":
		out, err = e.executeObsidianRead(ctx, argsMap)
	case "obsidian_related":
		out, err = e.executeObsidianRelated(ctx, argsMap)
	case "heartwood_state":
		out, err = e.executeHeartwoodState(ctx, argsMap)
	case "heartwood_action":
		out, err = e.executeHeartwoodAction(ctx, argsMap)
	case "context_filter":
		out, err = e.executeContextFilter(ctx, argsMap)
	// Overseer agent management tools
	case "agent_spawn":
		out, err = e.executeAgentSpawn(ctx, argsMap)
	case "agent_list":
		out, err = e.executeAgentList(ctx, argsMap)
	case "agent_status":
		out, err = e.executeAgentStatus(ctx, argsMap)
	case "agent_kill":
		out, err = e.executeAgentKill(ctx, argsMap)
	case "agent_hierarchy":
		out, err = e.executeAgentHierarchy(ctx, argsMap)
	case "agent_wait":
		out, err = e.executeAgentWait(ctx, argsMap)
	default:
		err = fmt.Errorf("unknown tool: %s", name)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[TOOLEXEC] tool=%s err=%v\n", name, err)
		return "", err
	}
	fmt.Fprintf(os.Stderr, "[TOOLEXEC] tool=%s ok output_len=%d\n", name, len(out))
	return out, nil
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

func (e *agentToolExecutor) executeEndTick(ctx context.Context, _ map[string]any) (string, error) {
	if e.endTick == nil {
		return "", fmt.Errorf("end_tick is not available")
	}
	if err := e.endTick(ctx); err != nil {
		return "", err
	}
	return `{"status":"requested","ended":true}`, nil
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
	llmBaseURL, _ := args["llm_base_url"].(string)
	llmAuthMode, _ := args["llm_auth_mode"].(string)
	llmAuthHeader, _ := args["llm_auth_header"].(string)
	llmAuthPrefix, _ := args["llm_auth_prefix"].(string)

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
		WithData("llm_model", llmModel).
		WithData("llm_base_url", llmBaseURL).
		WithData("llm_auth_mode", llmAuthMode)

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
				LLMBaseURL:    llmBaseURL,
				LLMAuthMode:   llmAuthMode,
				LLMAuthHeader: llmAuthHeader,
				LLMAuthPrefix: llmAuthPrefix,
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
	cmd := e.newAgentctlCommand(ctx, "run", "code/semantic_search", "--input", input)

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

	inputMap := map[string]any{
		"question": query,
	}
	maxSnippets := intArg(args, 0, "max_snippets", "limit")
	if maxSnippets > 0 {
		inputMap["limits"] = map[string]any{"max_snippets": maxSnippets}
	}
	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return "", fmt.Errorf("marshal smart_search input: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "code/smart_search", "--input", string(inputBytes))

	return commandOutput(cmd, "smart_search")
}

// executeContextFilter calls context/filter skill for LLM-powered chunk selection
func (e *agentToolExecutor) executeContextFilter(ctx context.Context, args map[string]any) (string, error) {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	inputMap := map[string]any{
		"prompt": prompt,
	}

	// Use agent's configured LLM provider, falling back to LM Studio.
	llmCfg := map[string]any{}
	if e.llmProvider != "" {
		llmCfg["provider"] = e.llmProvider
	} else {
		llmCfg["provider"] = "lmstudio"
	}
	if e.llmModel != "" {
		llmCfg["model"] = e.llmModel
	}
	inputMap["llm"] = llmCfg

	// Pass source through (text or chunks)
	if source, ok := args["source"].(map[string]any); ok {
		inputMap["source"] = source
	} else {
		return "", fmt.Errorf("source is required (object with text or chunks)")
	}

	// Pass budget through if provided
	if budget, ok := args["budget"].(map[string]any); ok {
		inputMap["budget"] = budget
	}

	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return "", fmt.Errorf("marshal context_filter input: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "context/filter", "--input", string(inputBytes))

	return commandOutput(cmd, "context_filter")
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
	cmd := e.newAgentctlCommand(ctx, "run", "code/context_grep", "--input", input)

	return commandOutput(cmd, "context_grep")
}

func (e *agentToolExecutor) executeCodeSymbols(ctx context.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	inputMap := map[string]any{
		"path":         path,
		"include_docs": true,
	}
	if kind, ok := args["kind"].(string); ok && kind != "" {
		inputMap["symbol_type"] = kind
	}

	inputBytes, _ := json.Marshal(inputMap)
	cmd := e.newAgentctlCommand(ctx, "run", "code/symbols", "--input", string(inputBytes))
	return commandOutput(cmd, "code_symbols")
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

	cmd := e.newAgentctlCommand(ctx, "index", "repo", "search", "--workspace", workspace, "--query", query, "--limit", strconv.Itoa(limit))

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

	cmd := e.newAgentctlCommand(ctx, argsList...)

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

	cmd := e.newAgentctlCommand(ctx, "index", "repo", "open", "--workspace", workspace, "--id", id)

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

	cmd := e.newAgentctlCommand(ctx, "run", "code/dag_grep", "--input", string(inputBytes))

	return commandOutput(cmd, "repo_index_dag_grep")
}

func (e *agentToolExecutor) executeContextShow(ctx context.Context, _ map[string]any) (string, error) {
	workspace := strings.TrimSpace(e.workspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	cmd := e.newAgentctlCommand(ctx, "context", "show", "--workspace", workspace)
	return commandOutputData(cmd, "context_show")
}

func (e *agentToolExecutor) executeContextRetrieve(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	vaultPath := stringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	limit := intArg(args, 5, "limit")
	workspace := strings.TrimSpace(e.workspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	cmd := e.newAgentctlCommand(ctx, "context", "retrieve", "--workspace", workspace, "--vault-path", vaultPath, "--query", query, "--limit", strconv.Itoa(limit))
	return commandOutputData(cmd, "context_retrieve")
}

func (e *agentToolExecutor) executeObsidianIndexSearch(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is required")
	}
	vaultPath := stringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	limit := intArg(args, 10, "limit")
	argsList := []string{"obsidian", "index", "search", "--vault-path", vaultPath, "--query", query, "--limit", strconv.Itoa(limit)}
	if boolArg(args, "semantic") {
		argsList = append(argsList, "--semantic")
	}
	cmd := e.newAgentctlCommand(ctx, argsList...)
	return commandOutputData(cmd, "obsidian_index_search")
}

func (e *agentToolExecutor) executeObsidianRead(ctx context.Context, args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	vaultPath := stringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	cmd := e.newAgentctlCommand(ctx, "obsidian", "read", "--vault-path", vaultPath, "--path", path)
	return commandOutputData(cmd, "obsidian_read")
}

func (e *agentToolExecutor) executeObsidianRelated(ctx context.Context, args map[string]any) (string, error) {
	path := stringArg(args, "path", "")
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	vaultPath := stringArg(args, "vault_path", "")
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		vaultPath = strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH"))
	}
	if strings.TrimSpace(vaultPath) == "" {
		return "", fmt.Errorf("vault_path is required")
	}
	limit := intArg(args, 10, "limit")
	cmd := e.newAgentctlCommand(ctx, "obsidian", "related", "--vault-path", vaultPath, "--path", path, "--limit", strconv.Itoa(limit))
	return commandOutputData(cmd, "obsidian_related")
}

func (e *agentToolExecutor) executeHeartwoodState(ctx context.Context, args map[string]any) (string, error) {
	inputBytes, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal heartwood_state args: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "heartwood/state", "--input", string(inputBytes))
	return commandOutput(cmd, "heartwood_state")
}

func (e *agentToolExecutor) executeHeartwoodAction(ctx context.Context, args map[string]any) (string, error) {
	inputBytes, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal heartwood_action args: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "heartwood/action", "--input", string(inputBytes))
	return commandOutput(cmd, "heartwood_action")
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
	cmd := e.newAgentctlCommand(ctx, "run", "code/semantic_search", "--input", input)

	return commandOutput(cmd, "session_timeline")
}

// executeMemoryQuery calls memory/query skill for gotchas, decisions, learnings
func (e *agentToolExecutor) executeMemoryQuery(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, 10, "limit")
	types, _ := args["types"].(string)

	inputMap := map[string]any{
		"query":     query,
		"workspace": e.workspaceRoot,
		"limit":     limit,
	}
	if types != "" {
		inputMap["types"] = types
	}
	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return "", fmt.Errorf("marshal memory_query input: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "memory/query", "--input", string(inputBytes))
	return commandOutput(cmd, "memory_query")
}

// executeSessionRecall calls code/semantic_search with sessions scope
func (e *agentToolExecutor) executeSessionRecall(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, 5, "limit")

	input := fmt.Sprintf(`{"query": %q, "scope": ["sessions"], "limit": %d}`, query, limit)
	cmd := e.newAgentctlCommand(ctx, "run", "code/semantic_search", "--input", input)
	return commandOutput(cmd, "session_recall")
}

// executeAnnotationRecall calls session/recall with annotation_granularity mode
func (e *agentToolExecutor) executeAnnotationRecall(ctx context.Context, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	limit := intArg(args, 10, "limit")

	inputMap := map[string]any{
		"query":                  query,
		"annotation_granularity": true,
		"workspace":              e.workspaceRoot,
		"limit":                  limit,
	}
	if cat, ok := args["filter_category"].(string); ok && cat != "" {
		inputMap["filter_category"] = cat
	}
	if sortBy, ok := args["sort_by"].(string); ok && sortBy != "" {
		inputMap["sort_by"] = sortBy
	}
	if sid, ok := args["session_id"].(string); ok && sid != "" {
		inputMap["session_id"] = sid
	}

	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return "", fmt.Errorf("marshal annotation_recall input: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "session/recall", "--input", string(inputBytes))
	return commandOutput(cmd, "annotation_recall")
}

// executeAnnotationCategoryStats calls session/recall in category_stats mode
func (e *agentToolExecutor) executeAnnotationCategoryStats(ctx context.Context, args map[string]any) (string, error) {
	inputMap := map[string]any{
		"category_stats": true,
		"workspace":      e.workspaceRoot,
	}
	if sid, ok := args["session_id"].(string); ok && sid != "" {
		inputMap["session_id"] = sid
	}

	inputBytes, err := json.Marshal(inputMap)
	if err != nil {
		return "", fmt.Errorf("marshal annotation_category_stats input: %w", err)
	}

	cmd := e.newAgentctlCommand(ctx, "run", "session/recall", "--input", string(inputBytes))
	return commandOutput(cmd, "annotation_category_stats")
}

// executeAnnotationListSessions calls session/recall in list_sessions mode
func (e *agentToolExecutor) executeAnnotationListSessions(ctx context.Context) (string, error) {
	input := fmt.Sprintf(`{"list_sessions": true, "workspace": %q}`, e.workspaceRoot)
	cmd := e.newAgentctlCommand(ctx, "run", "session/recall", "--input", input)
	return commandOutput(cmd, "annotation_list_sessions")
}

func (e *agentToolExecutor) newAgentctlCommand(ctx context.Context, args ...string) *exec.Cmd {
	bin := "agentctl"
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		bin = exe
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = e.workspaceRoot
	cmd.Env = filteredAgentctlEnv(os.Environ())
	return cmd
}

func filteredAgentctlEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, kv := range in {
		switch {
		case strings.HasPrefix(kv, "AGENTCTL_JIDO_"):
			continue
		case strings.HasPrefix(kv, "AGENTCTL_V2_ASK_DISPATCHER="):
			continue
		case strings.HasPrefix(kv, "AGENTCTL_JIDO_SOCKET="):
			continue
		case strings.HasPrefix(kv, "AGENTCTL_JIDO_RPC_PATH="):
			continue
		case strings.HasPrefix(kv, "AGENTCTL_JIDO_RPC_TIMEOUT_MS="):
			continue
		case strings.HasPrefix(kv, "AGENTCTL_JIDO_SIGNAL_SOURCE="):
			continue
		default:
			out = append(out, kv)
		}
	}
	return out
}

func commandOutput(cmd *exec.Cmd, label string) (string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[TOOLCMD] label=%s path=%s args=%q err=%v stderr=%q stdout=%q\n", label, cmd.Path, cmd.Args, err, strings.TrimSpace(stderr.String()), strings.TrimSpace(out))
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

func commandOutputData(cmd *exec.Cmd, label string) (string, error) {
	output, err := commandOutput(cmd, label)
	if err != nil {
		return "", err
	}
	var envelope map[string]any
	if json.Unmarshal([]byte(output), &envelope) != nil {
		return output, nil
	}
	data, ok := envelope["data"]
	if !ok {
		return output, nil
	}
	if summary := summarizeToolData(label, data); summary != "" {
		return summary, nil
	}
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return output, nil
	}
	return string(body), nil
}

func summarizeToolData(label string, data any) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	switch label {
	case "context_show":
		top, ok := m["top_of_mind"].(map[string]any)
		if !ok {
			return ""
		}
		var b strings.Builder
		b.WriteString("Top of mind\n")
		writeKeyValueLine(&b, "Workspace", stringFromMap(top, "workspace_id"))
		writeKeyValueLine(&b, "Objective", stringFromMap(top, "objective"))
		writeKeyValueLine(&b, "Phase", stringFromMap(top, "phase"))
		writeListLine(&b, "Active tasks", stringSliceFromMap(top, "active_task_ids"), 5)
		writeListLine(&b, "Next actions", stringSliceFromMap(top, "next_actions"), 5)
		return strings.TrimSpace(b.String())
	case "context_retrieve":
		result, ok := m["result"].(map[string]any)
		if !ok {
			return ""
		}
		var b strings.Builder
		b.WriteString("Retrieved context\n")
		writeKeyValueLine(&b, "Query", stringFromMap(result, "query"))
		if top, ok := result["top_of_mind"].(map[string]any); ok {
			writeKeyValueLine(&b, "Objective", stringFromMap(top, "objective"))
			writeKeyValueLine(&b, "Phase", stringFromMap(top, "phase"))
		}
		if hits, ok := result["vault_hits"].([]any); ok && len(hits) > 0 {
			b.WriteString("Vault hits:\n")
			for _, item := range hits[:minInt(len(hits), 5)] {
				hit, ok := item.(map[string]any)
				if !ok {
					continue
				}
				title := stringFromMap(hit, "title")
				path := stringFromMap(hit, "path")
				snippet := stringFromMap(hit, "snippet")
				b.WriteString("- " + firstNonEmptyString(title, path) + " [" + path + "]\n")
				if snippet != "" {
					b.WriteString("  " + snippet + "\n")
				}
			}
		}
		return strings.TrimSpace(b.String())
	case "obsidian_index_search":
		var b strings.Builder
		b.WriteString("Vault index search\n")
		if query := stringFromMap(m, "query"); query != "" {
			writeKeyValueLine(&b, "Query", query)
		}
		if hits, ok := m["hits"].([]any); ok {
			if len(hits) == 0 {
				b.WriteString("No hits.\n")
				return strings.TrimSpace(b.String())
			}
			b.WriteString("Hits:\n")
			for _, item := range hits[:minInt(len(hits), 5)] {
				hit, ok := item.(map[string]any)
				if !ok {
					continue
				}
				title := stringFromMap(hit, "title")
				path := stringFromMap(hit, "path")
				snippet := stringFromMap(hit, "snippet")
				b.WriteString("- " + firstNonEmptyString(title, path) + " [" + path + "]\n")
				if snippet != "" {
					b.WriteString("  " + snippet + "\n")
				}
			}
			return strings.TrimSpace(b.String())
		}
	case "obsidian_read":
		if result, ok := m["result"].(map[string]any); ok {
			content := stringFromMap(result, "content")
			if content != "" {
				return content
			}
		}
	case "obsidian_related":
		var b strings.Builder
		b.WriteString("Related notes\n")
		if items, ok := m["results"].([]any); ok {
			if len(items) == 0 {
				b.WriteString("No related notes.\n")
				return strings.TrimSpace(b.String())
			}
			for _, item := range items[:minInt(len(items), 5)] {
				if hit, ok := item.(map[string]any); ok {
					title := stringFromMap(hit, "title")
					path := stringFromMap(hit, "path")
					b.WriteString("- " + firstNonEmptyString(title, path) + " [" + path + "]\n")
				}
			}
			return strings.TrimSpace(b.String())
		}
	}
	return ""
}

func writeKeyValueLine(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	b.WriteString(label + ": " + value + "\n")
}

func writeListLine(b *strings.Builder, label string, items []string, limit int) {
	if len(items) == 0 {
		return
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	b.WriteString(label + ": " + strings.Join(items, ", ") + "\n")
}

func stringFromMap(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func stringSliceFromMap(m map[string]any, key string) []string {
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if v, ok := item.(string); ok && strings.TrimSpace(v) != "" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// buildToolDefsForRole returns tool definitions appropriate for the agent role.
// Tool names use underscores for Anthropic API compatibility (pattern: ^[a-zA-Z0-9_-]{1,128}$).
func buildToolDefsForRole(role types.AgentRole, hasMailbox, hasBoard bool, allowlist []string) []engine.ToolDef {
	// think is available to all agents
	tools := []engine.ToolDef{
		{
			Name:        "think",
			Description: "Record your reasoning or analysis without taking action",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"thought":{"type":"string","description":"Your reasoning or analysis"}},"required":["thought"]}`),
		},
		{
			Name:        "end_tick",
			Description: "Gracefully end the current tick-mode agent loop when no further scheduled work is needed.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		},
	}

	// Scout roles only get their specialized tools — no base file tools
	isScout := role == types.RoleSemanticScout || role == types.RoleDAGScout || role == types.RoleSymbolScout || role == types.RoleAnnotationScout
	if !isScout {
		tools = append(tools,
			engine.ToolDef{
				Name:        "fs_read_file",
				Description: "Read the contents of a file at the given path",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path to read"}},"required":["path"]}`),
			},
			engine.ToolDef{
				Name:        "code_search",
				Description: "Search for patterns in the codebase using regex",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"}},"required":["pattern"]}`),
			},
		)
	}

	// fs_list_dir for roles that benefit from directory browsing (not researcher or scouts — wastes iterations)
	if role != types.RoleResearcher && !isScout {
		tools = append(tools, engine.ToolDef{
			Name:        "fs_list_dir",
			Description: "List files and directories in a directory",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path to list"}}}`),
		})
	}

	// Heartwood tools are available to non-scout agents and can be restricted further by allowlist.
	if !isScout {
		tools = append(tools,
			engine.ToolDef{
				Name:        "heartwood_state",
				Description: "Fetch compact Heartwood participant state through the generated SpacetimeDB client.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"heartwood_root":{"type":"string","description":"Path to the Heartwood repo"},
					"host":{"type":"string","description":"WebSocket host, e.g. ws://127.0.0.1:3001"},
					"db_name":{"type":"string","description":"Heartwood database name"},
					"token":{"type":"string","description":"Optional SpacetimeDB token"},
					"token_path":{"type":"string","description":"Optional token file path"},
					"wait_timeout_ms":{"type":"integer","description":"Connection/subscription timeout in milliseconds"},
					"message_limit":{"type":"integer","description":"Recent message limit"}
				},"required":["host","db_name"]}`),
			},
			engine.ToolDef{
				Name:        "heartwood_action",
				Description: "Execute a whitelisted Heartwood participant action through the generated SpacetimeDB client.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"heartwood_root":{"type":"string","description":"Path to the Heartwood repo"},
					"host":{"type":"string","description":"WebSocket host, e.g. ws://127.0.0.1:3001"},
					"db_name":{"type":"string","description":"Heartwood database name"},
					"token":{"type":"string","description":"Optional SpacetimeDB token"},
					"token_path":{"type":"string","description":"Optional token file path"},
					"wait_timeout_ms":{"type":"integer","description":"Connection timeout in milliseconds"},
					"operation":{"type":"string","description":"Heartwood action name"},
					"args":{"type":"object","description":"Action arguments"}
				},"required":["host","db_name","operation"]}`),
			},
		)
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
					"max_snippets":{"type":"integer","description":"Maximum snippets to return (default 20)"}
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
				Name:        "code_symbols",
				Description: "Extract function/type/method signatures from a file with line numbers. Use this to see what's in a file before reading specific sections.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"path":{"type":"string","description":"File path to extract symbols from"},
					"kind":{"type":"string","description":"Filter by kind: function, method, type, interface, struct, const, var (default: all)"}
				},"required":["path"]}`),
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
			engine.ToolDef{
				Name:        "context_show",
				Description: "Read the current ACA top-of-mind bundle for the workspace.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			engine.ToolDef{
				Name:        "context_retrieve",
				Description: "Blend ACA control-plane state with vault retrieval for a focused question.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Question or topic to retrieve context for"},
					"vault_path":{"type":"string","description":"Vault path (optional if AGENTCTL_ACA_VAULT_PATH or AGENTCTL_OBSIDIAN_VAULT_PATH is set)"},
					"limit":{"type":"integer","description":"Maximum result count (default 5)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "obsidian_index_search",
				Description: "Search the local Obsidian vault index. Supports optional semantic note search.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Vault search query"},
					"vault_path":{"type":"string","description":"Vault path (optional if AGENTCTL_ACA_VAULT_PATH or AGENTCTL_OBSIDIAN_VAULT_PATH is set)"},
					"limit":{"type":"integer","description":"Maximum result count (default 10)"},
					"semantic":{"type":"boolean","description":"Use semantic note search if enabled"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "obsidian_read",
				Description: "Read a note from the Obsidian vault.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"path":{"type":"string","description":"Vault note path"},
					"vault_path":{"type":"string","description":"Vault path (optional if AGENTCTL_ACA_VAULT_PATH or AGENTCTL_OBSIDIAN_VAULT_PATH is set)"}
				},"required":["path"]}`),
			},
			engine.ToolDef{
				Name:        "obsidian_related",
				Description: "List related notes from the Obsidian vault using links, backlinks, aliases, or the local index.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"path":{"type":"string","description":"Vault note path"},
					"vault_path":{"type":"string","description":"Vault path (optional if AGENTCTL_ACA_VAULT_PATH or AGENTCTL_OBSIDIAN_VAULT_PATH is set)"},
					"limit":{"type":"integer","description":"Maximum result count (default 10)"}
				},"required":["path"]}`),
			},
			engine.ToolDef{
				Name:        "memory_query",
				Description: "Search stored memories (gotchas, decisions, learnings) for relevant context about the codebase.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"What to search for"},
					"types":{"type":"string","description":"Filter: gotcha,decision,learning,pattern (default: all)"},
					"limit":{"type":"integer","description":"Max results (default 10)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "session_recall",
				Description: "Search past agent sessions for relevant context, summaries, and findings.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Topic to search past sessions for"},
					"limit":{"type":"integer","description":"Max results (default 5)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "session_timeline",
				Description: "Get past session learnings as a timeline. Shows what work has been done before.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Topic to search past sessions for"},
					"limit":{"type":"integer","description":"Max sessions (default 5)"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "annotation_recall",
				Description: "Search past session turn-level annotations using semantic similarity. Supports category filtering (decision, debug, code_change, refactor, config, test, documentation) and multi-key sorting (similarity, date, recent). Returns detailed annotation matches with content previews.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Natural language query to find relevant annotations"},
					"filter_category":{"type":"string","description":"Filter by annotation category: decision, debug, code_change, refactor, config, test, documentation, discussion"},
					"sort_by":{"type":"string","description":"Comma-separated sort keys: similarity (default), date (oldest first), recent (newest first)"},
					"limit":{"type":"integer","description":"Maximum results (default 10)"},
					"session_id":{"type":"string","description":"Restrict search to a specific session ID"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "context_filter",
				Description: "LLM-powered context filtering: given text chunks and a question, uses an LLM to select the most relevant chunks. Use when you have large text output from tools and need to extract the most relevant parts for your report.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"prompt":{"type":"string","description":"What context to select (e.g., 'Find code related to session spawning')"},
					"source":{"type":"object","properties":{"text":{"type":"string","description":"Raw text to filter"},"chunks":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"text":{"type":"string"}}},"description":"Pre-chunked text segments"}},"description":"Text or chunks to filter"},
					"budget":{"type":"object","properties":{"target_tokens":{"type":"integer","description":"Approximate token budget for selected chunks (default 2000)"},"max_chunks":{"type":"integer","description":"Max chunks to return (default 16)"}},"description":"Size constraints"}
				},"required":["prompt","source"]}`),
			},
		)
	case types.RoleSemanticScout:
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
					"max_snippets":{"type":"integer","description":"Maximum snippets to return (default 20)"}
				},"required":["question"]}`),
			},
			engine.ToolDef{
				Name:        "memory_query",
				Description: "Search stored memories (gotchas, decisions, learnings) for relevant context about the codebase.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"What to search for"},
					"types":{"type":"string","description":"Filter: gotcha,decision,learning,pattern (default: all)"},
					"limit":{"type":"integer","description":"Max results (default 10)"}
				},"required":["query"]}`),
			},
		)
	case types.RoleDAGScout:
		tools = append(tools,
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
	case types.RoleSymbolScout:
		tools = append(tools,
			engine.ToolDef{
				Name:        "code_symbols",
				Description: "Extract function/type/method signatures from a file with line numbers. Use this to see what's in a file before reading specific sections.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"path":{"type":"string","description":"File path to extract symbols from"},
					"kind":{"type":"string","description":"Filter by kind: function, method, type, interface, struct, const, var (default: all)"}
				},"required":["path"]}`),
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
				Name:        "code_search",
				Description: "Search for patterns in the codebase using regex",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string","description":"Regex pattern to search for"}},"required":["pattern"]}`),
			},
		)
	case types.RoleAnnotationScout:
		tools = append(tools,
			engine.ToolDef{
				Name:        "annotation_recall",
				Description: "Search past session annotations using semantic similarity. Supports category filtering and multi-key sorting. Returns matching annotations with similarity scores, content previews, and metadata.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"Natural language query to find relevant annotations"},
					"filter_category":{"type":"string","description":"Filter by annotation category: decision, debug, code_change, refactor, config, test, documentation, discussion"},
					"sort_by":{"type":"string","description":"Comma-separated sort keys: similarity (default), date (oldest first), recent (newest first)"},
					"limit":{"type":"integer","description":"Maximum results (default 10)"},
					"session_id":{"type":"string","description":"Restrict search to a specific session ID"}
				},"required":["query"]}`),
			},
			engine.ToolDef{
				Name:        "annotation_list_sessions",
				Description: "List available sessions that have annotations. Use this to discover which sessions exist before searching.",
				Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
			},
			engine.ToolDef{
				Name:        "annotation_category_stats",
				Description: "Get annotation counts grouped by category. Call this FIRST to see what categories exist and how many annotations each has. Helps you decide which categories to filter on.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"session_id":{"type":"string","description":"Optional: restrict counts to a specific session"}
				}}`),
			},
			engine.ToolDef{
				Name:        "memory_query",
				Description: "Search stored memories (gotchas, decisions, learnings) for relevant context about the codebase.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"query":{"type":"string","description":"What to search for"},
					"types":{"type":"string","description":"Filter: gotcha,decision,learning,pattern (default: all)"},
					"limit":{"type":"integer","description":"Max results (default 10)"}
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
					"max_snippets":{"type":"integer","description":"Maximum snippets to return (default 20)"}
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
			engine.ToolDef{
				Name:        "repo_index_search",
				Description: "Search the repo index for nodes that match a text query. USE THIS for precise structural discovery before spawning DAG-focused subagents.",
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

		// Agent management tools
		tools = append(tools,
			engine.ToolDef{
				Name:        "agent_spawn",
				Description: "Spawn subagents with DETAILED prompts. Include specific file paths, tool instructions, and context from context_search/session_timeline results.",
				Parameters: json.RawMessage(`{"type":"object","properties":{
					"role":{"type":"string","description":"Agent role: coder, researcher, reviewer, planner","enum":["coder","researcher","reviewer","planner"]},
					"task":{"type":"string","description":"DETAILED task with: specific files, which tools to use, what to look for, success criteria"},
					"local_max_depth":{"type":"integer","description":"Maximum depth for this subtree (optional)"},
					"llm_provider":{"type":"string","description":"LLM provider: cerebras, openrouter, groq, gemini, anthropic, openai_compat (optional, inherits from parent)"},
					"llm_model":{"type":"string","description":"Model name (optional, uses provider default)"},
					"llm_base_url":{"type":"string","description":"Custom base URL for OpenAI-compatible/self-hosted backends (optional)"},
					"llm_auth_mode":{"type":"string","description":"Auth mode: auto, none, bearer, header (optional)"},
					"llm_auth_header":{"type":"string","description":"Auth header name when llm_auth_mode=header (optional)"},
					"llm_auth_prefix":{"type":"string","description":"Auth prefix for bearer/header mode, e.g. 'Bearer ' or 'Token ' (optional)"}
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
		allowlist = toolnames.NormalizeAllowlist(toolnames.ToolModeRuntime, allowlist)
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
	workspaceRoot := r.workspaceRootForSession(session)

	defer func() {
		if session.done != nil {
			close(session.done)
		}
	}()

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
				WithWorkspace(workspaceRoot).
				WithData("role", string(session.Config.Role)).
				WithData("iterations", session.Iterations).
				WithData("panic", true).
				Error(fmt.Errorf("panic: %v", rec), time.Since(sessionStart)))
		}
	}()

	// Apply session timeout when configured. A zero timeout means no outer session deadline.
	var cancel context.CancelFunc
	if session.Config.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, session.Config.Timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
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
	session.SystemPrompt = agentprompt.InstructionRuntime(session.Config.Role)
	taskPrompt := buildTaskPrompt(session.Config)
	var result string
	var bestResult string // Track most substantive response for summary
	var engineRetries int

	for {
		// Build input with conversation history
		engineInput := r.buildEngineInput(session, taskPrompt)

		// Persist user turn before running engine
		turnIndex := session.nextTurnIndex()
		_ = r.saveTurn(ctx, session.ID, turnIndex, "user", taskPrompt, nil, 0)

		// Run the engine
		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			// Context errors are not retryable
			if ctx.Err() != nil {
				session.mu.Lock()
				now := time.Now()
				session.EndedAt = &now
				if errors.Is(ctx.Err(), context.Canceled) {
					session.Status = types.StatusCanceled
					session.Error = "session canceled"
				} else {
					session.Status = types.StatusError
					session.Error = err.Error()
				}
				iterations := session.Iterations
				session.mu.Unlock()
				r.persistSessionStatus(session)
				observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
					WithComponent(observability.ComponentAgent).
					WithSession(session.ID, session.Config.ActorID).
					WithWorkspace(workspaceRoot).
					WithData("role", string(session.Config.Role)).
					WithData("iterations", iterations).
					Error(err, time.Since(sessionStart)))
				return
			}

			// Retry once on transient errors
			engineRetries++
			if engineRetries <= 1 {
				observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
					WithComponent(observability.ComponentAgent).
					WithSession(session.ID, session.Config.ActorID).
					WithWorkspace(workspaceRoot).
					WithData("role", string(session.Config.Role)).
					WithData("retry", engineRetries).
					WithData("error", err.Error()).
					Success(0))

				taskPrompt = fmt.Sprintf("The previous engine call failed with error: %s\n\nPlease adjust your approach and try again. If a tool failed, try a different tool or different parameters.", err.Error())
				continue
			}

			// Second failure: die
			session.mu.Lock()
			now := time.Now()
			session.EndedAt = &now
			session.Status = types.StatusError
			session.Error = err.Error()
			iterations := session.Iterations
			session.mu.Unlock()
			r.persistSessionStatus(session)

			observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
				WithComponent(observability.ComponentAgent).
				WithSession(session.ID, session.Config.ActorID).
				WithWorkspace(workspaceRoot).
				WithData("role", string(session.Config.Role)).
				WithData("iterations", iterations).
				WithData("retries_exhausted", true).
				Error(err, time.Since(sessionStart)))
			return
		}

		// Extract result from output
		result = output.AssistantText
		if isSubstantiveResult(result, bestResult) {
			bestResult = result
		}

		// Accumulate conversation history for cross-turn context
		appendToHistory(session, taskPrompt, result)

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
			WithWorkspace(workspaceRoot).
			WithData("role", string(session.Config.Role)).
			WithData("iteration", currentIterations).
			WithData("tool_calls", len(output.ToolCalls)).
			WithData("tool_names", toolNames).
			WithData("tokens_used", output.Tokens.TotalTokens).
			WithData("stop_reason", string(output.StopReason)).
			Success(0))

		// Persist assistant turn after engine run
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", result, output.ToolCalls, output.Tokens.TotalTokens)

		// Check for actual engine errors in output.
		// Note: max_iterations/context_budget may set output.Error informationally
		// and should still complete normally.
		if output.StopReason == engine.StopReasonError {
			// Context errors are not retryable
			if ctx.Err() != nil {
				session.mu.Lock()
				now := time.Now()
				session.EndedAt = &now
				if errors.Is(ctx.Err(), context.Canceled) {
					session.Status = types.StatusCanceled
					session.Error = "session canceled"
				} else {
					session.Status = types.StatusError
					session.Error = "session timeout"
				}
				session.mu.Unlock()
				r.persistSessionStatus(session)
				return
			}

			// Retry once on output errors
			engineRetries++
			if engineRetries <= 1 {
				errMsg := output.Error
				if errMsg == "" {
					errMsg = "engine returned error stop reason"
				}
				taskPrompt = fmt.Sprintf("The previous attempt returned an error: %s\n\nPlease adjust your approach and try again.", errMsg)
				continue
			}

			// Second failure: die
			session.mu.Lock()
			now := time.Now()
			session.EndedAt = &now
			session.Status = types.StatusError
			session.Error = output.Error
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
		result = r.runAutonomousContinuation(ctx, session, result, &bestResult)

	case agent.ModeAutonomousReactive:
		// Run autonomous continuation first, then stay alive in reactive mode.
		result = r.runAutonomousContinuation(ctx, session, result, &bestResult)

		session.mu.Lock()
		session.Status = types.StatusRunning
		session.mu.Unlock()
		r.persistSessionStatus(session)

		r.runMessageLoop(ctx, session, 0)

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

		thinkInterval := scheduledTickInterval(session.Config.ThinkInterval)

		// Enter message loop with proactive think cycles (blocks until context canceled)
		r.runMessageLoop(ctx, session, thinkInterval)

	case agent.ModeTick:
		// Tick mode: stay alive with interval-driven simulation/work cycles + message polling
		session.mu.Lock()
		session.Status = types.StatusRunning
		session.mu.Unlock()
		r.persistSessionStatus(session)

		thinkInterval := scheduledTickInterval(session.Config.ThinkInterval)

		r.runMessageLoop(ctx, session, thinkInterval)

	default:
		// Default (empty or unknown): complete immediately after initial response
	}

	// Check if context was canceled before marking OK
	session.mu.Lock()
	now := time.Now()
	session.EndedAt = &now
	if session.endTickRequested {
		session.Status = types.StatusOK
		session.Error = ""
		if bestResult == "" {
			bestResult = "tick ended"
		}
	} else if ctx.Err() == context.Canceled {
		session.Status = types.StatusCanceled
		session.Error = "session canceled"
	} else if ctx.Err() == context.DeadlineExceeded {
		session.Status = types.StatusError
		session.Error = "session timeout"
	} else {
		session.Status = types.StatusOK
	}
	if bestResult != "" {
		session.Summary = bestResult
	} else {
		session.Summary = result
	}
	totalIterations := session.Iterations
	totalToolCalls := len(session.ToolCalls)
	session.mu.Unlock()

	r.persistSessionStatus(session)

	// Emit completion event for real-time activity tracking
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentComplete).
		WithComponent(observability.ComponentAgent).
		WithSession(session.ID, session.Config.ActorID).
		WithWorkspace(workspaceRoot).
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

func stringArg(args map[string]any, fallback string, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key].(string); ok {
			trimmed := strings.TrimSpace(v)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return fallback
}

func boolArg(args map[string]any, keys ...string) bool {
	for _, key := range keys {
		if v, ok := args[key].(bool); ok {
			return v
		}
	}
	return false
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
		WorkspaceRoot: r.workspaceRootForSession(session),
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
	sessionError := session.Error
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

	// Update status (with error message if present)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if sessionError != "" {
		if err := r.config.SessionStore.SetStatusWithError(ctx, sessionID, status, sessionError); err != nil {
			_ = err // best-effort
			return
		}
	} else {
		if err := r.config.SessionStore.SetStatus(ctx, sessionID, status); err != nil {
			_ = err // best-effort
			return
		}
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
		if err := r.config.SessionStore.SetStatusWithError(ctx, s.ID, storage.SessionStatusError, "session interrupted by daemon restart"); err != nil {
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

// Done returns a channel that is closed when the session completes.
func (s *Session) Done() <-chan struct{} {
	return s.done
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
		WorkspaceRoot:    prevSession.WorkspacePath,
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

// isSubstantiveResult returns true if candidate is a more substantive response than current.
// Filters out control signals (TASK_COMPLETE, etc.) and picks the longer meaningful response.
func isSubstantiveResult(candidate, current string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if containsTaskComplete(candidate) || containsNoWork(candidate) {
		return false
	}
	return len(candidate) > len(current)
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
func (r *Runtime) runAutonomousContinuation(ctx context.Context, session *Session, lastResult string, bestResult *string) string {
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
			WithWorkspace(r.workspaceRootForSession(session)).
			WithData("turn", turn+1).
			WithData("max_turns", maxTurns).
			Success(0))

		// Check if agent already signaled completion
		if containsTaskComplete(result) {
			break
		}

		continuePrompt := r.continuationPrompt(session)

		// Persist user turn before running engine
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", continuePrompt, nil, 0)

		engineInput := r.buildEngineInput(session, continuePrompt)

		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			break
		}

		result = output.AssistantText
		if bestResult != nil && isSubstantiveResult(result, *bestResult) {
			*bestResult = result
		}

		// Accumulate conversation history
		appendToHistory(session, continuePrompt, result)

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
			WithWorkspace(r.workspaceRootForSession(session)).
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

// sessionResearchSummary builds a summary of research done across all turns
// from the session's accumulated tool calls (types.ToolCall format).
func sessionResearchSummary(toolCalls []types.ToolCall) string {
	var files, searches []string
	seen := make(map[string]bool)
	for _, tc := range toolCalls {
		switch tc.ToolName {
		case "fs_read_file":
			if p, _ := tc.Args["path"].(string); p != "" && !seen["f:"+p] {
				seen["f:"+p] = true
				files = append(files, filepath.Base(p))
			}
		case "code_symbols":
			if p, _ := tc.Args["path"].(string); p != "" && !seen["s:"+p] {
				seen["s:"+p] = true
				files = append(files, filepath.Base(p)+" (symbols)")
			}
		case "context_filter":
			if q, _ := tc.Args["prompt"].(string); q != "" {
				searches = append(searches, `context_filter("`+q+`")`)
			}
		case "context_search":
			if q, _ := tc.Args["query"].(string); q != "" {
				searches = append(searches, tc.ToolName+`("`+q+`")`)
			}
		case "smart_search":
			q, _ := tc.Args["question"].(string)
			if q == "" {
				q, _ = tc.Args["query"].(string)
			}
			if q != "" {
				searches = append(searches, tc.ToolName+`("`+q+`")`)
			}
		case "code_search", "context_grep":
			q, _ := tc.Args["pattern"].(string)
			if q == "" {
				q, _ = tc.Args["query"].(string)
			}
			if q != "" {
				searches = append(searches, tc.ToolName+`("`+q+`")`)
			}
		case "repo_index_dag_grep":
			if q, _ := tc.Args["query"].(string); q != "" {
				searches = append(searches, `dag_grep("`+q+`")`)
			}
		}
	}
	var b strings.Builder
	b.WriteString("Files read: ")
	if len(files) > 0 {
		b.WriteString(strings.Join(files, ", "))
	} else {
		b.WriteString("(none)")
	}
	b.WriteString("\nSearches: ")
	if len(searches) > 0 {
		b.WriteString(strings.Join(searches, ", "))
	} else {
		b.WriteString("(none)")
	}
	return b.String()
}

// continuationPrompt returns the prompt used for autonomous continuation turns.
// For researcher agents, it pushes for deeper investigation instead of early completion,
// and includes a summary of research done so far so the model knows what it already covered.
func (r *Runtime) continuationPrompt(session *Session) string {
	if session.Config.Role == types.RoleResearcher {
		session.mu.RLock()
		toolCalls := make([]types.ToolCall, len(session.ToolCalls))
		copy(toolCalls, session.ToolCalls)
		session.mu.RUnlock()

		summary := sessionResearchSummary(toolCalls)

		return "CONTINUATION TURN — if you have NOT yet produced a text report, write it NOW.\n\n" +
			"Prior research this session:\n" + summary + "\n\n" +
			"If you HAVE already produced a report, deepen it:\n" +
			"1. Read files you referenced but haven't read (schemas, configs, tests)\n" +
			"2. Find alternative code paths (code_search for callers you missed)\n" +
			"3. Verify claims by reading actual source — never paraphrase from memory\n" +
			"4. Append a '## Deepening' section with new findings and code snippets\n\n" +
			"Respond with 'TASK_COMPLETE' only if your report is already comprehensive."
	}
	return "Continue working on the previous task if there is more to do. If the task is complete, respond with 'TASK_COMPLETE'. Do not repeat already completed work."
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
	if r.config.MailboxStore == nil && !isTickDrivenMode(session.Config.ExecMode) {
		return
	}

	pollTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()

	var thinkTicker *time.Ticker
	var thinkChan <-chan time.Time
	isTickDriven := isTickDrivenMode(session.Config.ExecMode)

	if isTickDriven && thinkInterval > 0 {
		thinkTicker = time.NewTicker(thinkInterval)
		thinkChan = thinkTicker.C
		defer thinkTicker.Stop()
	}

	// Extract namespace from ActorID (format: actor:<role>:<id>)
	actorNS := session.Config.ActorID

	for {
		if r.endTickRequested(session) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			if r.config.MailboxStore != nil {
				r.processMailboxMessages(ctx, session, actorNS)
			}
		case <-thinkChan:
			r.runScheduledThink(ctx, session)
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

	now := time.Now().Unix()
	for _, msg := range messages {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Skip messages that are not yet visible (nacked with future visibility)
		if msg.VisibleAt > now {
			continue
		}

		// Process message - extract body from Payload
		payloadStr := string(msg.Payload)
		prompt := fmt.Sprintf("You received a message:\nFrom: %s\nPayload: %s\n\nRespond appropriately.", msg.FromNS, payloadStr)

		// Persist user turn before running engine
		_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", prompt, nil, 0)

		engineInput := r.buildEngineInput(session, prompt)

		output, err := session.Engine.Run(ctx, engineInput)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("agent.mailbox_error").
				WithComponent(observability.ComponentAgent).
				WithSession(session.ID, session.Config.ActorID).
				WithWorkspace(r.workspaceRootForSession(session)).
				Error(err, 0))
			// Nack with 30s delay to avoid tight retry loops on persistent errors
			_ = r.config.MailboxStore.Nack(ctx, msg.ID, 30*time.Second)
			continue
		}

		// Accumulate conversation history
		appendToHistory(session, prompt, output.AssistantText)

		if msg.Type == agent.MessageTypeAsk {
			if err := r.sendAskReply(ctx, session, msg, output.AssistantText); err != nil {
				observability.Emit(ctx, observability.NewEvent("agent.mailbox_reply_error").
					WithComponent(observability.ComponentAgent).
					WithSession(session.ID, session.Config.ActorID).
					WithWorkspace(r.workspaceRootForSession(session)).
					Error(err, 0))
			}
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
			WithWorkspace(r.workspaceRootForSession(session)).
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

func (r *Runtime) sendAskReply(ctx context.Context, session *Session, msg agent.Message, response string) error {
	if r.config.MailboxStore == nil {
		return fmt.Errorf("mailbox not configured")
	}

	askID := extractAskID(msg)
	if askID == "" {
		return fmt.Errorf("missing ask correlation")
	}

	replyData := agent.ReplyData{
		AskID:  askID,
		Answer: map[string]any{"response": response},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return fmt.Errorf("marshal reply envelope: %w", err)
	}

	fromNS := msg.ToNS
	if fromNS == "" {
		fromNS = session.Config.ActorID
	}

	replyMsg := agent.Message{
		ID:        ulid.Make().String(),
		FromNS:    fromNS,
		ToNS:      msg.FromNS,
		Type:      agent.MessageTypeReply,
		TTLMS:     300000,
		Headers:   map[string]string{"correlation": askID, "ask_id": askID},
		Payload:   replyPayload,
		VisibleAt: time.Now().Unix(),
		Timestamp: time.Now().Unix(),
	}

	return r.config.MailboxStore.Send(ctx, replyMsg)
}

func extractAskID(msg agent.Message) string {
	if corr := strings.TrimSpace(msg.Headers["correlation"]); corr != "" {
		return corr
	}

	var askEnv struct {
		Data agent.AskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Payload, &askEnv); err == nil {
		if askID := strings.TrimSpace(askEnv.Data.AskID); askID != "" {
			return askID
		}
	}

	var askData agent.AskData
	if err := json.Unmarshal(msg.Payload, &askData); err == nil {
		return strings.TrimSpace(askData.AskID)
	}

	return ""
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
func (r *Runtime) runScheduledThink(ctx context.Context, session *Session) {
	thinkPrompt := scheduledThinkPrompt(session.Config.ExecMode)
	eventName := scheduledThinkEventName(session.Config.ExecMode)
	workspaceRoot := r.workspaceRootForSession(session)

	// Persist user turn before running engine
	_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "user", thinkPrompt, nil, 0)

	engineInput := engine.EngineInput{
		SystemPrompt: session.SystemPrompt,
		Messages: []engine.Message{
			{Role: engine.RoleUser, Content: thinkPrompt},
		},
		Tools:     session.Tools,
		Workspace: workspaceRoot,
		SessionID: session.ID,
	}

	output, err := session.Engine.Run(ctx, engineInput)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent(eventName+"_error").
			WithComponent(observability.ComponentAgent).
			WithSession(session.ID, session.Config.ActorID).
			WithWorkspace(workspaceRoot).
			Error(err, 0))
		return
	}

	// Persist assistant turn after engine run
	_ = r.saveTurn(ctx, session.ID, session.nextTurnIndex(), "assistant", output.AssistantText, output.ToolCalls, output.Tokens.TotalTokens)

	if r.endTickRequested(session) {
		return
	}

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
		WithWorkspace(workspaceRoot).
		WithData("role", string(session.Config.Role)).
		WithData("iteration", currentIterations).
		WithData("tool_calls", len(output.ToolCalls)).
		WithData("tool_names", toolNames).
		WithData("tokens_used", output.Tokens.TotalTokens).
		WithData(eventName, true).
		Success(0))

	observability.Emit(ctx, observability.NewEvent("agent."+eventName+"_work").
		WithComponent(observability.ComponentAgent).
		WithSession(session.ID, session.Config.ActorID).
		WithWorkspace(workspaceRoot).
		Success(0))

	// If work was started and autonomous continuation is enabled
	if session.Config.MaxAutoTurns > 1 {
		r.runAutonomousContinuation(ctx, session, output.AssistantText, nil)
	}
}

func isTickDrivenMode(mode agent.ExecutionMode) bool {
	return mode == agent.ModeProactive || mode == agent.ModeTick
}

func scheduledTickInterval(seconds int) time.Duration {
	interval := time.Duration(seconds) * time.Second
	if interval <= 0 {
		return 60 * time.Second
	}
	return interval
}

func scheduledThinkEventName(mode agent.ExecutionMode) string {
	if mode == agent.ModeTick {
		return "tick_cycle"
	}
	return "proactive_think"
}

func scheduledThinkPrompt(mode agent.ExecutionMode) string {
	if mode == agent.ModeTick {
		return `You are in tick mode. This is one scheduled simulation/work tick.
Advance the current work by one step using the latest context.
If no action is required on this tick, respond with 'NO_WORK_NEEDED'.`
	}
	return `You are in proactive mode. Check if there is any work that needs to be done:
1. Review any pending tasks or todos
2. Check for any issues that need attention
3. Look for opportunities to help

If there is work to do, start working on the highest priority item.
If there is nothing to do, respond with 'NO_WORK_NEEDED'.`
}

func (r *Runtime) requestEndTick(_ context.Context, sessionID string) error {
	r.mu.RLock()
	session, ok := r.sessions[sessionID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	if session.Config.ExecMode != agent.ModeTick {
		return fmt.Errorf("end_tick is only available in tick mode")
	}
	session.mu.Lock()
	session.endTickRequested = true
	session.mu.Unlock()
	return nil
}

func (r *Runtime) endTickRequested(session *Session) bool {
	if session == nil {
		return false
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.endTickRequested
}
