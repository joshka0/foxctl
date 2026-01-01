package actor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents"
	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/actor/memory"
	agenttools "github.com/jkatigb/agentctl/internal/agent/tools"
	agenttypes "github.com/jkatigb/agentctl/internal/agent/types"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/storage"
)

// DspyActor wraps a dspy-go ReActAgent as a reactive actor.
//
// It provides message-driven execution where each agent.ask or agent.cmd
// message triggers a dspy-go execution turn. The actor integrates with
// ShortTermMemory for progressive context management.
//
// See docs/designs/reactive-actor-system.md for the full design.
type DspyActor struct {
	*BaseActor

	// dspyAgent is the underlying dspy-go agent
	dspyAgent agents.Agent

	// toolsRegistry holds available tools
	toolsRegistry *agenttools.Registry

	// llm is the language model for the agent
	llm core.LLM

	// agentConfig holds agent-specific configuration
	agentConfig agenttypes.AgentConfig

	// shortTermMem manages progressive context (always enabled)
	shortTermMem *memory.ShortTermMemory

	// logger for structured logging
	logger zerolog.Logger

	// replySender is injected by supervisor for sending replies
	sendReply func(ctx context.Context, msg *Message) error
}

// DspyActorConfig holds configuration for creating a DspyActor.
type DspyActorConfig struct {
	// ActorConfig is the base actor configuration
	ActorConfig Config

	// AgentConfig is the dspy-go agent configuration
	AgentConfig agenttypes.AgentConfig

	// LLMProvider is the LLM provider (gemini, openai, anthropic, groq, openrouter)
	LLMProvider string

	// LLMModel is the model name
	LLMModel string

	// LLMAPIKey is the API key for the LLM provider
	LLMAPIKey string

	// WorkspaceRoot is the workspace directory
	WorkspaceRoot string

	// DB is the database connection for memory
	DB *sql.DB

	// OpenMemoryStore provides access to named memory for retrieval tools
	OpenMemoryStore func(context.Context) (storage.MemoryStore, error)

	// TrajectoryStorageRoot enables agent tool call capture when set
	TrajectoryStorageRoot string

	// Logger for structured logging
	Logger zerolog.Logger
}

// DspyActorOption configures a DspyActor.
type DspyActorOption func(*DspyActor)

// WithDspyLogger sets the logger for the DspyActor.
func WithDspyLogger(logger zerolog.Logger) DspyActorOption {
	return func(a *DspyActor) {
		a.logger = logger
	}
}

// NewDspyActor creates a new DspyActor with the given configuration.
func NewDspyActor(cfg DspyActorConfig, opts ...DspyActorOption) (*DspyActor, error) {
	// Create base actor with lifecycle hooks
	baseActor := NewBaseActor(cfg.ActorConfig)

	actor := &DspyActor{
		BaseActor:   baseActor,
		agentConfig: cfg.AgentConfig,
		logger:      cfg.Logger,
	}

	// Apply options
	for _, opt := range opts {
		opt(actor)
	}

	// Set up lifecycle hooks
	baseActor.onStart = actor.onStart
	baseActor.onStop = actor.onStop

	// Initialize components
	if err := actor.initializeLLM(cfg); err != nil {
		return nil, fmt.Errorf("initialize LLM: %w", err)
	}

	if err := actor.initializeTools(cfg); err != nil {
		return nil, fmt.Errorf("initialize tools: %w", err)
	}

	if err := actor.initializeDspyAgent(cfg); err != nil {
		return nil, fmt.Errorf("initialize dspy agent: %w", err)
	}

	// Initialize short-term memory if DB provided
	if cfg.DB != nil {
		if err := actor.initializeMemory(cfg); err != nil {
			return nil, fmt.Errorf("initialize memory: %w", err)
		}
	}

	// Register message handlers
	actor.RegisterHandler("agent.ask", actor.handleAsk)
	actor.RegisterHandler("agent.cmd", actor.handleCmd)
	actor.RegisterHandler("agent.event", actor.handleEvent)

	return actor, nil
}

// initializeLLM sets up the language model.
func (a *DspyActor) initializeLLM(cfg DspyActorConfig) error {
	llms.EnsureFactory()

	// Resolve provider: config → env → default
	provider := cfg.LLMProvider
	if provider == "" {
		provider = os.Getenv("AGENTCTL_LLM_PROVIDER")
	}
	if provider == "" {
		provider = "gemini"
	}

	// Resolve model: config → env → provider default
	model := cfg.LLMModel
	if model == "" {
		model = os.Getenv("AGENTCTL_LLM_MODEL")
	}
	if model == "" {
		model = defaultModelForProvider(provider)
	}

	// Resolve API key: config → env
	apiKey := cfg.LLMAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("AGENTCTL_LLM_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("LLM API key not configured for provider %q", provider)
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
		config := core.ProviderConfig{Name: "anthropic", APIKey: apiKey}
		llm, err = llms.NewAnthropicLLMFromConfig(context.Background(), config, core.ModelID(model))
	case "groq":
		llm, err = llms.NewOpenAICompatible("groq", core.ModelID(model),
			"https://api.groq.com/openai/v1", llms.WithAPIKey(apiKey))
	case "openrouter":
		llm, err = llms.NewOpenAICompatible("openrouter", core.ModelID(model),
			"https://openrouter.ai/api/v1", llms.WithAPIKey(apiKey))
	default:
		return fmt.Errorf("unsupported LLM provider: %q", provider)
	}
	if err != nil {
		return fmt.Errorf("create %s LLM: %w", provider, err)
	}

	a.llm = llm
	return nil
}

// initializeTools sets up the tools registry.
func (a *DspyActor) initializeTools(cfg DspyActorConfig) error {
	toolsCfg := agenttools.Config{
		WorkspaceRoot:         cfg.WorkspaceRoot,
		WorkspaceID:           cfg.AgentConfig.WorkspaceID,
		ActorID:               cfg.AgentConfig.ActorID,
		TaskID:                cfg.AgentConfig.TaskID,
		EpicID:                cfg.AgentConfig.EpicID,
		AgentRole:             string(cfg.AgentConfig.Role),
		TrajectoryStorageRoot: cfg.TrajectoryStorageRoot,
	}

	if cfg.OpenMemoryStore != nil {
		toolsCfg.OpenMemoryStore = cfg.OpenMemoryStore
	}

	registry, err := agenttools.NewRegistry(toolsCfg, nil)
	if err != nil {
		return fmt.Errorf("create tools registry: %w", err)
	}

	a.toolsRegistry = registry
	return nil
}

// initializeDspyAgent creates the dspy-go ReActAgent.
func (a *DspyActor) initializeDspyAgent(cfg DspyActorConfig) error {
	// Create ReActAgent with options
	maxIterations := cfg.AgentConfig.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	timeout := cfg.AgentConfig.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	opts := []react.Option{
		react.WithMaxIterations(maxIterations),
		react.WithTimeout(timeout),
	}

	agentID := fmt.Sprintf("%s:%s", cfg.AgentConfig.Role, cfg.AgentConfig.ActorID)
	agentName := fmt.Sprintf("%s Agent", cfg.AgentConfig.Role)

	agent := react.NewReActAgent(agentID, agentName, opts...)

	// Build signature based on role
	signature := buildAgentSignature(cfg.AgentConfig.Role)

	// Initialize agent with LLM and signature
	if err := agent.Initialize(a.llm, *signature); err != nil {
		return fmt.Errorf("initialize agent: %w", err)
	}

	// Register tools
	for _, tool := range a.toolsRegistry.List() {
		if err := agent.RegisterTool(tool); err != nil {
			return fmt.Errorf("register tool %s: %w", tool.Name(), err)
		}
	}

	a.dspyAgent = agent
	return nil
}

// initializeMemory sets up short-term memory.
func (a *DspyActor) initializeMemory(cfg DspyActorConfig) error {
	mem, err := memory.New(context.Background(), cfg.DB)
	if err != nil {
		return fmt.Errorf("create short-term memory: %w", err)
	}

	a.shortTermMem = mem
	return nil
}

// buildAgentSignature creates the signature for the agent based on its role.
func buildAgentSignature(role agenttypes.AgentRole) *core.Signature {
	var instruction string
	switch role {
	case agenttypes.RoleCoder:
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

Testing:
- tests.run: Run tests

Workflow: Use code.symbol_search to find relevant symbols, then code.swe_grep to get detailed context.
Apply changes with edit.apply_patch for simple edits.`
	case agenttypes.RolePlanner:
		instruction = `You are a planning agent. You analyze tasks and create structured plans.
Available tools:
- todo.add: Add new tasks
- todo.query: Query existing tasks
- todo.graph_insights: Get task graph analysis
- mail.send: Send messages to other agents

Use these tools to plan and coordinate work.`
	case agenttypes.RoleReviewer:
		instruction = `You are a code review agent. Your job is to understand proposed changes,
evaluate their impact, and suggest improvements. You do not directly apply edits yourself.

Code Search & Retrieval Tools (read/inspect):
- code.symbol_search: Search the symbol index for functions, methods, classes by natural language query
- code.swe_grep: Extract high-signal code snippets from candidate files
- code.search: Search code using ripgrep patterns

File Operations (read-only):
- fs.read_file: Read file contents for review
- fs.list_dir: Inspect project structure

Validation:
- tests.run: Run tests to validate changes

Coordination:
- mail.send: Communicate findings and requests to other agents
- todo.add: Create follow-up tasks from review findings`
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
		return "gpt-4.1-mini" // Also available: gpt-4.1-nano for lighter tasks
	case "gemini", "":
		return "gemini-2.0-flash"
	case "anthropic":
		return "claude-haiku-4-5"
	case "groq":
		return "llama-3.1-70b-versatile"
	case "openrouter":
		return "anthropic/claude-haiku-4-5"
	default:
		return "gemini-2.0-flash"
	}
}

// onStart is called when the actor starts.
func (a *DspyActor) onStart(ctx context.Context) error {
	// Initialize memory state for this actor
	if a.shortTermMem != nil {
		sessionID := ulid.Make().String()
		if err := a.shortTermMem.InitState(ctx, a.ID(), sessionID); err != nil {
			a.logger.Warn().Err(err).Msg("failed to initialize memory state")
		}
	}

	a.logger.Info().
		Str("actor_id", a.ID()).
		Str("namespace", a.Namespace()).
		Str("role", string(a.agentConfig.Role)).
		Msg("DspyActor started")

	return nil
}

// onStop is called when the actor stops.
func (a *DspyActor) onStop(ctx context.Context) error {
	a.logger.Info().
		Str("actor_id", a.ID()).
		Msg("DspyActor stopped")
	return nil
}

// handleAsk processes an agent.ask message.
func (a *DspyActor) handleAsk(ctx context.Context, msg *Message) (*Message, error) {
	// Parse payload envelope
	var env struct {
		Data agentdomain.AskData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal ask payload: %w", err)
	}
	askData := env.Data

	// Build prompt from ask
	prompt := fmt.Sprintf("Question: %s\nContext: %v", askData.Question, askData.Context)

	// Add memory context if available
	if a.shortTermMem != nil {
		memContext, err := a.shortTermMem.GetContext(ctx, a.ID())
		if err == nil && memContext != "" {
			prompt = memContext + "\n\n---\n\n" + prompt
		}
	}

	// Execute dspy-go turn
	input := map[string]any{
		"task": prompt,
	}

	// Apply timeout
	timeout := 10 * time.Minute
	if a.agentConfig.Timeout > 0 {
		timeout = a.agentConfig.Timeout
	}
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	resultMap, err := a.dspyAgent.Execute(turnCtx, input)
	if err != nil {
		return nil, fmt.Errorf("dspy execution failed: %w", err)
	}

	// Extract result
	result := extractResult(resultMap)

	// Record turn in memory
	if a.shortTermMem != nil {
		turn := memory.Turn{
			Role:      "assistant",
			Content:   result,
			Timestamp: time.Now(),
		}
		if err := a.shortTermMem.AppendTurn(ctx, a.ID(), turn); err != nil {
			a.logger.Warn().Err(err).Msg("failed to append turn to memory")
		}
	}

	// Build reply
	replyData := agentdomain.ReplyData{
		AskID:  askData.AskID,
		Answer: map[string]any{"response": result},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		return nil, fmt.Errorf("marshal reply envelope: %w", err)
	}

	return &Message{
		ID:        ulid.Make().String(),
		Subject:   "agent.reply",
		Body:      replyPayload,
		CreatedAt: time.Now(),
	}, nil
}

// handleCmd processes an agent.cmd message.
func (a *DspyActor) handleCmd(ctx context.Context, msg *Message) (*Message, error) {
	var env struct {
		Data agentdomain.CmdData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal cmd payload: %w", err)
	}
	cmdData := env.Data

	switch cmdData.Action {
	case "run_skill":
		return nil, fmt.Errorf("run_skill not yet implemented")
	case "run_turn", "do_work":
		// Execute dspy-go turn with cmdData.Args as context
		prompt := fmt.Sprintf("Command: %s\nArgs: %v", cmdData.Action, cmdData.Args)

		input := map[string]any{
			"task": prompt,
		}

		timeout := 10 * time.Minute
		if a.agentConfig.Timeout > 0 {
			timeout = a.agentConfig.Timeout
		}
		turnCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		_, err := a.dspyAgent.Execute(turnCtx, input)
		if err != nil {
			return nil, fmt.Errorf("dspy execution failed: %w", err)
		}

		return nil, nil // Commands are fire-and-forget
	default:
		return nil, fmt.Errorf("unknown action: %s", cmdData.Action)
	}
}

// handleEvent processes an agent.event message.
func (a *DspyActor) handleEvent(ctx context.Context, msg *Message) (*Message, error) {
	var env struct {
		Data agentdomain.EventData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		return nil, fmt.Errorf("unmarshal event payload: %w", err)
	}
	eventData := env.Data

	// MVP: just log it
	a.logger.Info().
		Str("event_id", eventData.EventID).
		Str("kind", eventData.Kind).
		Int("job_count", eventData.JobCount).
		Msg("received agent event")

	return nil, nil
}

// extractResult extracts the result string from a dspy-go execution result.
func extractResult(resultMap map[string]any) string {
	if resultMap == nil {
		return "Task completed"
	}

	// Priority order: result, answer, output, thought
	if r, ok := resultMap["result"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["answer"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["output"].(string); ok && r != "" {
		return r
	}
	if r, ok := resultMap["thought"].(string); ok && r != "" {
		return r
	}

	// Last resort: format the map but exclude internal ReAct fields
	cleaned := make(map[string]any)
	for k, v := range resultMap {
		if k == "action" || k == "observation" || k == "conversation_context" {
			continue
		}
		cleaned[k] = v
	}
	if len(cleaned) > 0 {
		return fmt.Sprintf("%v", cleaned)
	}

	return "Task completed"
}

// SetReplySender sets the function used to send reply messages.
// Called by the supervisor when registering the actor.
func (a *DspyActor) SetReplySender(fn func(ctx context.Context, msg *Message) error) {
	a.sendReply = fn
	// Also set on base actor
	a.replySender = fn
}

// Agent returns the underlying dspy-go agent for testing.
func (a *DspyActor) Agent() agents.Agent {
	return a.dspyAgent
}

// ToolsRegistry returns the tools registry for testing.
func (a *DspyActor) ToolsRegistry() *agenttools.Registry {
	return a.toolsRegistry
}

// ShortTermMemory returns the short-term memory for testing.
func (a *DspyActor) ShortTermMemory() *memory.ShortTermMemory {
	return a.shortTermMem
}

// Ensure DspyActor implements Actor interface.
var _ Actor = (*DspyActor)(nil)
