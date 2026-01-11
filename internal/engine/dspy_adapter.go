package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/XiaoConstantine/dspy-go/pkg/agents/react"
	"github.com/XiaoConstantine/dspy-go/pkg/core"
	"github.com/XiaoConstantine/dspy-go/pkg/llms"
	dstools "github.com/XiaoConstantine/dspy-go/pkg/tools"
	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/rs/zerolog"
)

// DSPyAdapter wraps a dspy-go agent as an AgentEngine.
//
// This adapter provides the bridge between the engine interface and
// the dspy-go ReActAgent implementation.
type DSPyAdapter struct {
	// reactAgent is the underlying dspy-go ReActAgent.
	// We store the concrete type to access RegisterTool.
	reactAgent *react.ReActAgent

	// llm is the language model.
	llm core.LLM

	// toolRegistry holds available tools.
	toolRegistry *dstools.InMemoryToolRegistry

	// config holds adapter configuration.
	config DSPyAdapterConfig

	// logger for structured logging.
	logger zerolog.Logger
}

// DSPyAdapterConfig configures the DSPy adapter.
type DSPyAdapterConfig struct {
	// LLMProvider is the LLM provider (gemini, openai, anthropic, groq, openrouter).
	LLMProvider string

	// LLMModel is the model name.
	LLMModel string

	// LLMAPIKey is the API key.
	LLMAPIKey string

	// MaxIterations limits the tool call loop.
	MaxIterations int

	// Timeout is the maximum execution time.
	Timeout time.Duration

	// AgentID identifies the agent.
	AgentID string

	// AgentName is the display name.
	AgentName string

	// SystemPrompt is the default system prompt.
	SystemPrompt string

	// Logger for structured logging.
	Logger zerolog.Logger
}

// DefaultDSPyAdapterConfig returns sensible defaults.
func DefaultDSPyAdapterConfig() DSPyAdapterConfig {
	return DSPyAdapterConfig{
		LLMProvider:   "gemini",
		LLMModel:      "gemini-2.0-flash",
		MaxIterations: 50,
		Timeout:       30 * time.Minute,
		AgentID:       "engine-agent",
		AgentName:     "Engine Agent",
	}
}

// NewDSPyAdapter creates a new DSPy adapter.
func NewDSPyAdapter(cfg DSPyAdapterConfig) (*DSPyAdapter, error) {
	// Ensure LLM factory is initialized
	llms.EnsureFactory()

	// Create LLM
	llm, err := createLLM(cfg)
	if err != nil {
		return nil, fmt.Errorf("create LLM: %w", err)
	}

	// Create adapter with empty tool registry
	adapter := &DSPyAdapter{
		llm:          llm,
		toolRegistry: dstools.NewInMemoryToolRegistry(),
		config:       cfg,
		logger:       cfg.Logger,
	}

	// Initialize the agent
	if err := adapter.initializeAgent(); err != nil {
		return nil, fmt.Errorf("initialize agent: %w", err)
	}

	return adapter, nil
}

// createLLM creates the LLM based on provider configuration.
func createLLM(cfg DSPyAdapterConfig) (core.LLM, error) {
	if cfg.LLMAPIKey == "" {
		return nil, fmt.Errorf("LLM API key not configured for provider %q", cfg.LLMProvider)
	}

	switch cfg.LLMProvider {
	case "gemini", "":
		return llms.NewGeminiLLM(cfg.LLMAPIKey, core.ModelID(cfg.LLMModel))
	case "openai":
		return llms.NewOpenAILLM(core.ModelID(cfg.LLMModel), llms.WithAPIKey(cfg.LLMAPIKey))
	case "anthropic":
		config := core.ProviderConfig{Name: "anthropic", APIKey: cfg.LLMAPIKey}
		return llms.NewAnthropicLLMFromConfig(context.Background(), config, core.ModelID(cfg.LLMModel))
	case "groq":
		return llms.NewOpenAICompatible("groq", core.ModelID(cfg.LLMModel),
			"https://api.groq.com/openai/v1", llms.WithAPIKey(cfg.LLMAPIKey))
	case "openrouter":
		return llms.NewOpenAICompatible("openrouter", core.ModelID(cfg.LLMModel),
			"https://openrouter.ai/api/v1", llms.WithAPIKey(cfg.LLMAPIKey))
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %q", cfg.LLMProvider)
	}
}

// initializeAgent creates and initializes the dspy-go agent.
func (a *DSPyAdapter) initializeAgent() error {
	maxIterations := a.config.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 50
	}

	timeout := a.config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}

	opts := []react.Option{
		react.WithMaxIterations(maxIterations),
		react.WithTimeout(timeout),
	}

	agent := react.NewReActAgent(a.config.AgentID, a.config.AgentName, opts...)

	// Build signature with system prompt
	instruction := a.config.SystemPrompt
	if instruction == "" {
		instruction = "You are a helpful agent. Complete the given task using available tools."
	}

	sig := core.NewSignature(
		[]core.InputField{
			{Field: core.NewField("task", core.WithDescription("The task or message to process"))},
		},
		[]core.OutputField{
			{Field: core.NewField("result", core.WithDescription("The final result or response"))},
		},
	).WithInstruction(instruction)

	if err := agent.Initialize(a.llm, sig); err != nil {
		return fmt.Errorf("initialize agent: %w", err)
	}

	a.reactAgent = agent
	return nil
}

// RegisterTool registers a tool with the adapter's agent.
func (a *DSPyAdapter) RegisterTool(tool core.Tool) error {
	if err := a.toolRegistry.Register(tool); err != nil {
		return err
	}

	// Also register with the agent
	if a.reactAgent != nil {
		return a.reactAgent.RegisterTool(tool)
	}
	return nil
}

// RegisterFuncTool registers a function-based tool.
// The function signature matches dspy-go's ToolFunc type.
func (a *DSPyAdapter) RegisterFuncTool(name, description string, schema models.InputSchema, fn dstools.ToolFunc) error {
	tool := dstools.NewFuncTool(name, description, schema, fn)
	return a.RegisterTool(tool)
}

// Run implements AgentEngine.
func (a *DSPyAdapter) Run(ctx context.Context, input EngineInput) (EngineOutput, error) {
	// Apply timeout if specified
	timeout := a.config.Timeout
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build prompt from messages
	prompt := a.buildPrompt(input)

	// Create input map for dspy-go
	inputMap := map[string]any{
		"task": prompt,
	}

	// Execute the agent
	start := time.Now()
	resultMap, err := a.reactAgent.Execute(ctx, inputMap)

	// Handle context cancellation
	if ctx.Err() != nil {
		return EngineOutput{
			StopReason: StopReasonCancelled,
			Error:      ctx.Err().Error(),
		}, nil
	}

	// Handle other errors
	if err != nil {
		return EngineOutput{
			StopReason: StopReasonError,
			Error:      err.Error(),
		}, nil
	}

	// Extract result
	result := extractResultFromMap(resultMap)

	// Build output
	output := EngineOutput{
		AssistantText: result,
		StopReason:    StopReasonEndTurn,
		Tokens: TokenUsage{
			// Token usage would need to be tracked by the LLM wrapper
			// This is a limitation of dspy-go that we may need to address
		},
	}

	_ = start // Could use for timing metrics

	return output, nil
}

// buildPrompt constructs a prompt from the engine input messages.
func (a *DSPyAdapter) buildPrompt(input EngineInput) string {
	var parts []string

	// Add system prompt if not already in messages
	hasSystem := false
	for _, msg := range input.Messages {
		if msg.Role == RoleSystem {
			hasSystem = true
			break
		}
	}
	if !hasSystem && input.SystemPrompt != "" {
		parts = append(parts, input.SystemPrompt)
	}

	// Process messages
	for _, msg := range input.Messages {
		switch msg.Role {
		case RoleSystem:
			parts = append(parts, msg.Content)
		case RoleUser:
			parts = append(parts, fmt.Sprintf("User: %s", msg.Content))
		case RoleAssistant:
			parts = append(parts, fmt.Sprintf("Assistant: %s", msg.Content))
		case RoleTool:
			parts = append(parts, fmt.Sprintf("Tool (%s): %s", msg.Name, msg.Content))
		}
	}

	return strings.Join(parts, "\n\n")
}

// extractResultFromMap extracts the result string from a dspy-go result map.
func extractResultFromMap(resultMap map[string]any) string {
	// Try common keys
	for _, key := range []string{"result", "response", "answer", "output"} {
		if v, ok := resultMap[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case map[string]any:
				// If it's a nested map, try to extract a string value
				if s, ok := val["text"].(string); ok {
					return s
				}
				if s, ok := val["content"].(string); ok {
					return s
				}
				// Serialize as JSON
				b, _ := json.Marshal(val)
				return string(b)
			default:
				// Serialize as JSON
				b, _ := json.Marshal(val)
				return string(b)
			}
		}
	}

	// If no known key, serialize the whole map
	b, _ := json.Marshal(resultMap)
	return string(b)
}

// DSPyAdapterOption configures a DSPyAdapter.
type DSPyAdapterOption func(*DSPyAdapterConfig)

// WithDSPyLLMProvider sets the LLM provider.
func WithDSPyLLMProvider(provider string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.LLMProvider = provider
	}
}

// WithDSPyLLMModel sets the LLM model.
func WithDSPyLLMModel(model string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.LLMModel = model
	}
}

// WithDSPyLLMAPIKey sets the LLM API key.
func WithDSPyLLMAPIKey(key string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.LLMAPIKey = key
	}
}

// WithDSPyMaxIterations sets the maximum iterations.
func WithDSPyMaxIterations(n int) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.MaxIterations = n
	}
}

// WithDSPyTimeout sets the timeout.
func WithDSPyTimeout(d time.Duration) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.Timeout = d
	}
}

// WithDSPyAgentID sets the agent ID.
func WithDSPyAgentID(id string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.AgentID = id
	}
}

// WithDSPyAgentName sets the agent name.
func WithDSPyAgentName(name string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.AgentName = name
	}
}

// WithDSPySystemPrompt sets the system prompt.
func WithDSPySystemPrompt(prompt string) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.SystemPrompt = prompt
	}
}

// WithDSPyLogger sets the logger.
func WithDSPyLogger(logger zerolog.Logger) DSPyAdapterOption {
	return func(c *DSPyAdapterConfig) {
		c.Logger = logger
	}
}
