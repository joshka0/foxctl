// Package runtime provides the dspy-go agent runtime wrapper for agentctl.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	"github.com/oklog/ulid/v2"

	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/agentprompt"
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

	traceID := traceIDFromContext(ctx)
	if traceID == "" {
		traceID = ulid.Make().String()
	}

	// Create tools registry with telemetry recorder
	recorder := &sessionRecorder{sessionID: sessionID}
	toolsCfg := agenttools.Config{
		WorkspaceRoot:         r.config.WorkspaceRoot,
		WorkspaceID:           cfg.WorkspaceID,
		SessionID:             sessionID,
		ActorID:               cfg.ActorID,
		TaskID:                cfg.TaskID,
		EpicID:                cfg.EpicID,
		Depth:                 cfg.Depth,
		MaxDepth:              cfg.MaxDepth,
		LocalMaxDepth:         cfg.LocalMaxDepth,
		AgentRole:             string(cfg.Role),
		TraceID:               traceID,
		TrajectoryStorageRoot: r.config.TrajectoryStorageRoot,
		HookDispatcher:        r.config.HookDispatcher,
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

	// Resolve provider: agent → runtime → default
	provider := cfg.LLMProvider
	if provider == "" {
		provider = r.config.LLMProvider
	}
	if provider == "" {
		provider = "gemini" // Default provider
	}

	// Resolve model: agent → runtime → provider-specific default
	model := cfg.LLMModel
	if model == "" {
		model = r.config.LLMModel
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
	case "anthropic":
		// For Claude models via Anthropic API
		config := core.ProviderConfig{Name: "anthropic", APIKey: apiKey}
		llm, err = llms.NewAnthropicLLMFromConfig(context.Background(), config, core.ModelID(model))
	case "groq":
		// GROQ uses OpenAI-compatible API
		llm, err = llms.NewOpenAICompatible("groq", core.ModelID(model),
			"https://api.groq.com/openai/v1", llms.WithAPIKey(apiKey))
	case "openrouter":
		// OpenRouter provides access to multiple models via OpenAI-compatible API
		llm, err = llms.NewOpenAICompatible("openrouter", core.ModelID(model),
			"https://openrouter.ai/api/v1", llms.WithAPIKey(apiKey))
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q (supported: gemini, openai, anthropic, groq, openrouter)", provider)
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
	return agentprompt.BuildSignature(cfg.Role)
}

// defaultModelForProvider returns the default model for a given LLM provider.
func defaultModelForProvider(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4.1-mini"
	case "gemini", "":
		return "gemini-2.0-flash"
	case "anthropic":
		return "claude-haiku-4-5"
	case "groq":
		return "llama-3.1-70b-versatile"
	case "openrouter":
		return "anthropic/claude-haiku-4-5" // OpenRouter uses provider/model format
	default:
		return "gemini-2.0-flash"
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
	ctx, cancel := context.WithTimeout(ctx, session.Config.Timeout)
	defer cancel()

	// Build the task prompt based on config
	taskPrompt := buildTaskPrompt(session.Config)
	var result string

	for {
		// Run the agent using Execute
		input := map[string]any{
			"task": taskPrompt,
		}
		resultMap, err := session.Agent.Execute(ctx, input)

		// Extract result string
		if resultMap != nil {
			if r, ok := resultMap["result"].(string); ok {
				result = r
			} else if r, ok := resultMap["output"].(string); ok {
				result = r
			} else {
				result = fmt.Sprintf("%v", resultMap)
			}
		}

		if err != nil {
			session.mu.Lock()
			defer session.mu.Unlock()

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
				session.Error = err.Error()
			}
			return
		}

		stopResult := r.dispatchStopRequested(ctx, session, taskPrompt, result)
		if stopResult.Blocked {
			continuation := buildStopContinuation(result, stopResult.Output.Context)
			if continuation == "" {
				session.mu.Lock()
				defer session.mu.Unlock()

				now := time.Now()
				session.EndedAt = &now
				session.Status = types.StatusError
				session.Error = fmt.Sprintf("stop blocked without continuation: %s", stopResult.Output.Reason)
				return
			}
			taskPrompt = continuation
			continue
		}

		break
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	now := time.Now()
	session.EndedAt = &now

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
