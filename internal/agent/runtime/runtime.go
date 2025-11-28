// Package runtime provides the dspy-go agent runtime wrapper for agentctl.
package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
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

	// WorkspaceRoot is the workspace root directory.
	WorkspaceRoot string
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

	// Register all tools from the registry
	for _, tool := range toolsRegistry.List() {
		if err := agent.RegisterTool(tool); err != nil {
			return nil, fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}

	return agent, nil
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
