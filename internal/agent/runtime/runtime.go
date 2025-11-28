// Package runtime provides the dspy-go agent runtime wrapper for agentctl.
package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	"github.com/oklog/ulid/v2"

	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
)

// Runtime manages agent sessions and lifecycle.
type Runtime struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	config   RuntimeConfig
}

// RuntimeConfig configures the agent runtime.
type RuntimeConfig struct {
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
func NewRuntime(cfg RuntimeConfig) *Runtime {
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

	// Create tools registry with telemetry recorder
	recorder := &sessionRecorder{sessionID: sessionID}
	toolsCfg := agenttools.ToolsConfig{
		WorkspaceRoot: r.config.WorkspaceRoot,
		WorkspaceID:   cfg.WorkspaceID,
		ActorID:       cfg.ActorID,
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
		spawnCfg.MailSender = func(ctx context.Context, to, subject string, body any) (any, error) {
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

	// Store session
	r.mu.Lock()
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
		react.WithTimeout(cfg.Timeout),
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

	// Determine model (prefer agent-level, fall back to runtime default)
	model := cfg.LLMModel
	if model == "" {
		model = r.config.LLMModel
	}
	if model == "" {
		model = "gemini-2.5-flash" // Default to Gemini 2.5 Flash (supported by dspy-go)
	}

	if apiKey == "" {
		return nil, fmt.Errorf("LLM API key not configured (set AGENTCTL_LLM_API_KEY or pass via config)")
	}

	// Create LLM based on provider
	llm, err := llms.NewGeminiLLM(apiKey, core.ModelID(model))
	if err != nil {
		return nil, fmt.Errorf("create LLM: %w", err)
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
Available tools:
- fs.read_file: Read file contents
- fs.list_dir: List directory contents
- edit.create_file: Create new files
- edit.apply_patch: Modify existing files
- code.search: Search code using ripgrep
- tests.run: Run tests

Use these tools to complete coding tasks. Always create or modify files as needed.`
	case types.RolePlanner:
		instruction = `You are a planning agent. You analyze tasks and create structured plans.
Available tools:
- todo.add: Add new tasks
- todo.query: Query existing tasks
- todo.graph_insights: Get task graph analysis
- mail.send: Send messages to other agents

Use these tools to plan and coordinate work.`
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
	ctx, cancel := context.WithTimeout(ctx, session.Config.Timeout)
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
