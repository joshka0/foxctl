// Package runtime provides the dspy-go agent runtime wrapper for agentctl.
package runtime

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	"github.com/oklog/ulid/v2"

	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
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
	Agent      agents.Agent
	Tools      *agenttools.Registry
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
	if cfg.Timeout.Duration() <= 0 {
		cfg.Timeout = types.Duration(r.config.DefaultTimeout)
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

	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = ulid.Make().String()
	}

	// Create tools registry with telemetry recorder
	recorder := &sessionRecorder{sessionID: sessionID}
	toolsCfg := agenttools.Config{
		WorkspaceRoot:         r.config.WorkspaceRoot,
		WorkspaceID:           cfg.WorkspaceID,
		ActorID:               cfg.ActorID,
		TaskID:                cfg.TaskID,
		EpicID:                cfg.EpicID,
		AgentRole:             string(cfg.Role),
		TraceID:               traceID,
		TrajectoryStorageRoot: r.config.TrajectoryStorageRoot,
		OpenMemoryStore: func(ctx context.Context) (storage.MemoryStore, error) {
			if r.config.OpenMemoryStore == nil {
				return nil, fmt.Errorf("named memory store not configured")
			}
			return r.config.OpenMemoryStore(ctx)
		},
	}
	toolsRegistry, err := agenttools.NewRegistry(toolsCfg, recorder)
	if err != nil {
		return nil, fmt.Errorf("create tools registry: %w", err)
	}

	// Register spawn tool with hierarchy config
	spawnCfg := agenttools.SpawnToolConfig{
		CallerActorID:       cfg.ActorID,
		CallerDepth:         cfg.Depth,
		CallerMaxDepth:      cfg.MaxDepth,
		CallerLocalMaxDepth: cfg.LocalMaxDepth,
		EpicID:              cfg.EpicID,
	}

	// Wire up spawn handler if configured
	if r.config.SpawnHandler != nil {
		spawnCfg.MailSender = func(ctx context.Context, _, _ string, body any) (any, error) {
			req, ok := body.(types.SpawnRequest)
			if !ok {
				return nil, fmt.Errorf("invalid spawn request body type")
			}
			return r.config.SpawnHandler.HandleSpawnRequest(ctx, req)
		}
	}

	if err := toolsRegistry.RegisterSpawnTool(spawnCfg); err != nil {
		return nil, fmt.Errorf("register spawn tool: %w", err)
	}

	// Create the dspy-go agent based on role
	agent, err := r.createAgent(cfg, toolsRegistry)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// Create session context with cancellation
	sessionCtx, cancel := context.WithCancel(ctx)

	session := &Session{
		ID:        sessionID,
		Config:    cfg,
		Status:    types.StatusRunning,
		Agent:     agent,
		Tools:     toolsRegistry,
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

	// Link recorder to session
	recorder.session = session

	// Start the agent in background
	go r.runSession(sessionCtx, session)

	return session, nil
}

// createAgent creates the appropriate dspy-go agent based on role.
func (r *Runtime) createAgent(cfg types.AgentConfig, toolsRegistry *agenttools.Registry) (agents.Agent, error) {
	// Create the ReActAgent with options
	opts := []react.Option{
		react.WithMaxIterations(cfg.MaxIterations),
		react.WithTimeout(cfg.Timeout.Duration()),
	}

	// Create agent ID based on config
	agentID := fmt.Sprintf("%s:%s", cfg.Role, cfg.ActorID)
	agentName := fmt.Sprintf("%s Agent", cfg.Role)

	agent := react.NewReActAgent(agentID, agentName, opts...)

	// Initialize LLM
	llms.EnsureFactory()

	// Determine API key (prefer agent-level, fall back to runtime default)
	apiKey := cfg.LLMAPIKey
	if apiKey == "" {
		apiKey = r.config.LLMAPIKey
	}

	// Resolve provider: agent → runtime → env → default
	provider := cfg.LLMProvider
	if provider == "" {
		provider = r.config.LLMProvider
	}
	if provider == "" {
		provider = os.Getenv("AGENTCTL_LLM_PROVIDER")
	}
	if provider == "" {
		provider = "gemini" // Default provider
	}

	// Resolve model: agent → runtime → env → provider-specific default
	model := cfg.LLMModel
	if model == "" {
		model = r.config.LLMModel
	}
	if model == "" {
		model = os.Getenv("AGENTCTL_LLM_MODEL")
	}
	if model == "" {
		model = defaultModelForProvider(provider)
	}

	if apiKey == "" {
		return nil, fmt.Errorf("LLM API key not configured for provider %q (set AGENTCTL_LLM_API_KEY or pass via config)", provider)
	}

	// Create LLM based on provider
	var llm core.LLM
	var err error
	switch provider {
	case "gemini", "":
		llm, err = llms.NewGeminiLLM(apiKey, core.ModelID(model))
	case "openai":
		llm, err = llms.NewOpenAILLM(core.ModelID(model), llms.WithAPIKey(apiKey))
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: gemini, openai)", provider)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s LLM: %w", provider, err)
	}

	// Create a signature for the agent
	signature := buildAgentSignature(cfg)

	// Initialize the agent with LLM and signature
	if err := agent.Initialize(llm, *signature); err != nil {
		return nil, fmt.Errorf("initialize agent: %w", err)
	}

	// Register all tools from the registry
	for _, tool := range toolsRegistry.List() {
		if err := agent.RegisterTool(tool); err != nil {
			return nil, fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}

	return agent, nil
}

// buildAgentSignature creates the signature for the agent based on its role.
func buildAgentSignature(cfg types.AgentConfig) *core.Signature {
	var instruction string
	switch cfg.Role {
	case types.RoleCoder:
		instruction = `You are a coding agent. You have access to file system tools to read and write code.

Code Search & Retrieval Tools:
- code.symbol_search: Search the symbol index for functions, methods, classes by natural language query
- code.swe_grep: Extract high-signal code snippets from candidate files (use after symbol_search)
- code.search: Search code using ripgrep patterns

File Operations:
- fs.read_file: Read file contents
- fs.list_dir: List directory contents

Edit Tools:
- edit.create_file: Create new files
- edit.apply_patch: Modify existing files with simple text replacement
- edit.apply_structured_diff: Apply structured diffs from code/diff skill (for complex multi-hunk changes)

Testing:
- tests.run: Run tests

Workflow: Use code.symbol_search to find relevant symbols, then code.swe_grep to get detailed context.
Apply changes with edit.apply_patch for simple edits or edit.apply_structured_diff for complex refactors.`
	case types.RolePlanner:
		instruction = `You are a planning agent. You analyze tasks and create structured plans.
Available tools:
- todo.add: Add new tasks
- todo.query: Query existing tasks
- todo.graph_insights: Get task graph analysis
- mail.send: Send messages to other agents

Use these tools to plan and coordinate work.`
	case types.RoleReviewer:
		instruction = `You are a code review agent. Your job is to understand proposed changes,
evaluate their impact, and suggest improvements. You do not directly apply edits yourself.

Code Search & Retrieval Tools (read/inspect):
- code.symbol_search: Search the symbol index for functions, methods, classes by natural language query
- code.swe_grep: Extract high-signal code snippets from candidate files (use after symbol_search)
- code.search: Search code using ripgrep patterns

File Operations (read-only):
- fs.read_file: Read file contents for review
- fs.list_dir: Inspect project structure

Validation:
- tests.run: Run tests to validate changes

Coordination:
- mail.send: Communicate findings and requests to other agents
- todo.add: Create follow-up tasks from review findings

Workflow:
1. Use code.symbol_search and code.swe_grep to understand the relevant code paths.
2. Use fs.read_file to inspect surrounding context.
3. Use tests.run to verify behavior and check for regressions.
4. Suggest concrete patches or improvements in your output, but leave edits to Coder.
5. Use mail.send to communicate review feedback or todo.add to track follow-ups.`
	default:
		instruction = `You are a helpful agent. Complete the given task using available tools.`
	}

	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("task", core.WithDescription("The task to be completed by the agent"))},
		},
		[]core.OutputField{
			{Field: core.NewField("result", core.WithDescription("The final result or answer from completing the task"))},
		},
	).WithInstruction(instruction)
	return &sig
}

// defaultModelForProvider returns the default model for a given LLM provider.
func defaultModelForProvider(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4.1-mini"
	case "gemini", "":
		return "gemini-2.5-flash"
	default:
		return "gemini-2.5-flash"
	}
}

// runSession executes the agent session.
func (r *Runtime) runSession(ctx context.Context, session *Session) {
	defer func() {
		if rec := recover(); rec != nil {
			session.mu.Lock()
			session.Status = types.StatusError
			session.Error = fmt.Sprintf("panic: %v", rec)
			now := time.Now()
			session.EndedAt = &now
			session.mu.Unlock()
		}
	}()

	// Apply timeout
	ctx, cancel := context.WithTimeout(ctx, session.Config.Timeout.Duration())
	defer cancel()

	// Build the task prompt based on config
	taskPrompt := buildTaskPrompt(session.Config)

	// Run the agent using Execute
	input := map[string]interface{}{
		"task": taskPrompt,
	}
	resultMap, err := session.Agent.Execute(ctx, input)

	// Extract result string
	var result string
	if resultMap != nil {
		if r, ok := resultMap["result"].(string); ok {
			result = r
		} else if r, ok := resultMap["output"].(string); ok {
			result = r
		} else {
			result = fmt.Sprintf("%v", resultMap)
		}
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	now := time.Now()
	session.EndedAt = &now

	if err != nil {
		if ctx.Err() == context.Canceled {
			session.Status = types.StatusCanceled
			session.Error = "session canceled"
		} else if ctx.Err() == context.DeadlineExceeded {
			session.Status = types.StatusError
			session.Error = "session timeout"
		} else {
			session.Status = types.StatusError
			session.Error = err.Error()
		}
		return
	}

	// Parse result
	session.Status = types.StatusOK
	session.Summary = result
}

// buildTaskPrompt creates the prompt for the agent from config.
func buildTaskPrompt(cfg types.AgentConfig) string {
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
	defer r.mu.Unlock()

	session, ok := r.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Cancel the session
	if session.cancel != nil {
		session.cancel()
	}

	session.mu.Lock()
	if session.Status == types.StatusRunning {
		session.Status = types.StatusCanceled
		now := time.Now()
		session.EndedAt = &now
	}
	session.mu.Unlock()

	return nil
}

// sessionRecorder records tool calls for a session.
type sessionRecorder struct {
	sessionID string
	session   *Session
}

func (r *sessionRecorder) RecordToolCall(call types.ToolCall) {
	if r.session == nil {
		return
	}

	r.session.mu.Lock()
	defer r.session.mu.Unlock()

	r.session.ToolCalls = append(r.session.ToolCalls, call)
	r.session.Iterations = len(r.session.ToolCalls)
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
