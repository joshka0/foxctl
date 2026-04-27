package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	llmproviders "github.com/joshka0/foxctl/internal/providers/llm"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

// HookContext provides context for hook dispatch from LLMChatEngine.
type HookContext struct {
	SessionID     string
	ActorID       string
	WorkspaceID   string
	WorkspaceRoot string
}

// LLMChatEngine implements AgentEngine using OpenAI-compatible chat completions.
// Supports OpenRouter, Groq, OpenAI, and other compatible providers.
type LLMChatEngine struct {
	config        LLMChatConfig
	client        *http.Client
	bedrockClient *BedrockClient // Non-nil when provider is "bedrock"
	toolRunner    *ToolRunner
	rlmExecutor   *RLMToolExecutor // For tracking RLM context queries
	hookContext   HookContext      // Context for hook dispatch
}

// LLMChatConfig configures the LLM chat engine.
type LLMChatConfig struct {
	// Provider is the LLM provider: "openrouter", "groq", "openai",
	// "openai_compat", "lmstudio", or "bedrock".
	Provider string

	// APIKey is the API key for the provider.
	APIKey string

	// BaseURL is the API base URL. Auto-detected if empty.
	BaseURL string

	// AuthMode controls authentication: auto, none, bearer, header.
	AuthMode string

	// AuthHeader is used when AuthMode=header.
	AuthHeader string

	// AuthPrefix is prepended to APIKey for bearer/header auth.
	AuthPrefix string

	// Model is the model name. Auto-detected if empty.
	Model string

	// MaxIterations limits the tool call loop.
	MaxIterations int

	// MaxContextTokens limits context size before stopping. When prompt tokens
	// exceed this limit, the engine stops with StopReasonContextBudget.
	// Set to 0 to disable (default).
	MaxContextTokens int

	// Timeout is the HTTP request timeout.
	Timeout time.Duration

	// Temperature controls randomness (0-1).
	Temperature float64

	// MaxTokens limits the response length.
	MaxTokens int

	// ResponseFormat enforces structured outputs when supported by the provider.
	ResponseFormat json.RawMessage

	// ExtraBody is merged into the OpenAI-compatible request body after the
	// standard fields are built. Use it for provider/model-specific controls.
	ExtraBody map[string]any

	// ToolChoice forwards an OpenAI-compatible tool_choice override when tools
	// are present, for example `"auto"` or `"required"`.
	ToolChoice json.RawMessage

	// ParseReasoningToolCalls accepts LMStudio/local-model tool-call markup in
	// reasoning_content when a provider does not populate tool_calls.
	ParseReasoningToolCalls bool

	// UseReasoningContentAsText lets internal structured-output calls recover
	// content from reasoning_content when local backends leave content empty.
	// Keep this disabled for user-facing final answers.
	UseReasoningContentAsText bool

	// PassthroughToolCalls returns model-requested tool calls without executing
	// them inside this engine. This lets an outer runtime own tool policy,
	// execution, and event recording.
	PassthroughToolCalls bool

	// HookDispatcher for pre/post tool use hooks (optional).
	HookDispatcher hooks.Dispatcher

	// ActionExecutor processes hook output actions. Optional - actions are
	// skipped if nil.
	ActionExecutor hooks.ActionExecutor

	// StatelessMode enables RLM (Recursive Language Model) mode.
	// In this mode:
	// - Each turn only includes system prompt + current user message
	// - No conversation history is appended
	// - Context is queried via rlm_context_* tools
	// - Ideal for mobile/companion apps with predictable latency
	StatelessMode bool

	// RLMSystemPromptSuffix is appended to the system prompt in StatelessMode.
	// Use this to include RLM-specific instructions for context querying.
	RLMSystemPromptSuffix string

	// RequireContextQuery blocks completion if no context was queried in StatelessMode.
	// Useful for enforcing context-aware responses.
	RequireContextQuery bool

	// SynthesisReserve is the number of iterations to reserve for text-only
	// synthesis at the end of the tool-call loop. Tools are stripped when
	// iteration reaches MaxIterations - SynthesisReserve. Default: 2.
	SynthesisReserve int
}

// DefaultLLMChatConfig returns sensible defaults.
func DefaultLLMChatConfig() LLMChatConfig {
	return LLMChatConfig{
		MaxIterations: 50,
		Timeout:       120 * time.Second,
		Temperature:   0.0,
		MaxTokens:     8192,
	}
}

// NewLLMChatEngine creates a new LLM chat engine.
// Auto-detects provider from environment if not specified.
//
// Index:
// - Purpose: Initialize an OpenAI-compatible chat engine with provider defaults
// - Flow: resolve API key/provider → set base URL/model → apply defaults → create client
// - SideEffects: reads environment variables
// - FailureModes: missing API key, provider resolution errors
// - Related: DefaultLLMChatConfig, apiKeyForProvider, detectProvider
// - Keywords: llm_chat, provider, api_key, base_url, model
func NewLLMChatEngine(cfg LLMChatConfig) (*LLMChatEngine, error) {
	resolvedCfg, err := resolveLLMChatConfig(cfg)
	if err != nil {
		return nil, err
	}

	engine := &LLMChatEngine{
		config: resolvedCfg,
		client: &http.Client{Timeout: resolvedCfg.Timeout},
	}

	if err := engine.initBedrockClient(); err != nil {
		return nil, err
	}

	return engine, nil
}

func resolveLLMChatConfig(cfg LLMChatConfig) (LLMChatConfig, error) {
	cfg.Provider = normalizeEngineProvider(cfg.Provider)
	if cfg.Provider == "" && strings.TrimSpace(cfg.BaseURL) != "" {
		cfg.Provider = "openai_compat"
	}

	if err := resolveLLMChatCredentials(&cfg); err != nil {
		return LLMChatConfig{}, err
	}
	applyLLMChatDefaults(&cfg)
	if authRequiresCredential(cfg.AuthMode) && cfg.APIKey == "" {
		return LLMChatConfig{}, fmt.Errorf("auth mode %q requires an API key", cfg.AuthMode)
	}
	return cfg, nil
}

func resolveLLMChatCredentials(cfg *LLMChatConfig) error {
	if authRequiresCredential(cfg.AuthMode) && cfg.APIKey == "" && cfg.Provider != "" {
		cfg.APIKey = apiKeyForProvider(cfg.Provider)
		if cfg.APIKey == "" {
			return fmt.Errorf("no API key configured for provider %q (set the appropriate env var or use auth_mode=none)", cfg.Provider)
		}
	}
	if cfg.APIKey == "" && cfg.Provider == "" {
		cfg.APIKey, cfg.Provider = detectProvider()
	}
	return nil
}

func applyLLMChatDefaults(cfg *LLMChatConfig) {
	if cfg.BaseURL == "" {
		cfg.BaseURL = baseURLForProvider(cfg.Provider)
	}
	if cfg.Model == "" {
		cfg.Model = llmproviders.DefaultModelForProvider(cfg.Provider)
	}
	if cfg.AuthMode == "" {
		cfg.AuthMode = defaultAuthModeForProvider(cfg.Provider, cfg.APIKey)
	}
	if cfg.AuthHeader == "" {
		cfg.AuthHeader = defaultAuthHeader(cfg.AuthMode)
	}
	if cfg.AuthPrefix == "" && cfg.AuthMode == "bearer" {
		cfg.AuthPrefix = "Bearer "
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 50
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}
	if cfg.SynthesisReserve <= 0 {
		cfg.SynthesisReserve = 2
	}
}

func (e *LLMChatEngine) initBedrockClient() error {
	if e.config.Provider != "bedrock" {
		return nil
	}
	region := resolveBedrockRegion(e.config)
	if region == "" {
		return fmt.Errorf("bedrock provider requires BEDROCK_REGION or AWS_DEFAULT_REGION")
	}
	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	bc, err := NewBedrockClient(initCtx, region)
	if err != nil {
		return fmt.Errorf("bedrock client: %w", err)
	}
	e.bedrockClient = bc
	return nil
}

func resolveBedrockRegion(cfg LLMChatConfig) string {
	if cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	if region := os.Getenv("BEDROCK_REGION"); region != "" {
		return region
	}
	return os.Getenv("AWS_DEFAULT_REGION")
}

// SetToolRunner sets the tool runner for executing tools.
func (e *LLMChatEngine) SetToolRunner(runner *ToolRunner) {
	e.toolRunner = runner
}

// SetRLMExecutor sets the RLM tool executor for context query tracking.
// Required for StatelessMode with RequireContextQuery enabled.
func (e *LLMChatEngine) SetRLMExecutor(executor *RLMToolExecutor) {
	e.rlmExecutor = executor
}

// SetHookContext sets the hook context for dispatch.
func (e *LLMChatEngine) SetHookContext(ctx HookContext) {
	e.hookContext = ctx
}

// Config returns the resolved LLMChatConfig for this engine.
// Useful for callers that need the provider-resolved APIKey, BaseURL, and Model
// after auto-detection has run.
func (e *LLMChatEngine) Config() LLMChatConfig {
	return e.config
}

// IsStatelessMode returns true if the engine is in RLM stateless mode.
func (e *LLMChatEngine) IsStatelessMode() bool {
	return e.config.StatelessMode
}

// Run implements AgentEngine.
//
// Index:
// - Purpose: Execute a single agent turn with tool calls, hooks, and LLM responses
// - Flow: build messages → loop LLM calls → dispatch hooks → run tools → append results → finalize output
// - SideEffects: network calls to LLM; tool execution; hook dispatch; observability emits
// - FailureModes: iteration limit, context cancellation, LLM errors, tool execution errors
// - Observability: emits OpAgentIteration and llm.no_choices events
// - Related: callLLM, dispatchPreToolUse, dispatchPostToolUse, ToolRunner.Execute
// - Keywords: agent_run, tool_calls, hook_dispatch, iterations, stop_reason
func (e *LLMChatEngine) Run(ctx context.Context, input EngineInput) (EngineOutput, error) {
	if os.Getenv("FOXCTL_DEBUG_CONTEXT_QUERY") == "1" {
		fmt.Fprintf(os.Stderr, "[CTX-POLICY] stateless=%t require_context_query=%t max_iterations=%d provider=%s model=%s\n",
			e.config.StatelessMode, e.config.RequireContextQuery, e.config.MaxIterations, e.config.Provider, e.config.Model)
	}

	// Reset RLM query counter at start of turn
	if e.rlmExecutor != nil {
		e.rlmExecutor.ResetQueryCount()
	}

	// Build initial messages
	messages := e.buildMessages(input)
	tools := e.buildTools(input.Tools)

	var output EngineOutput
	iteration := 0

	for {
		iterStart := time.Now()

		iteration++
		if e.finishIfIterationExceeded(ctx, iteration, messages, &output) {
			return output, nil
		}

		if e.finishIfRunCanceled(ctx, &output) {
			return output, nil
		}

		resp, err := e.callLLM(ctx, messages, tools)
		iterDuration := time.Since(iterStart)
		if e.finishIfRunCallFailed(ctx, err, &output) {
			return output, nil
		}

		if !e.recordLLMResponse(ctx, resp, messages, iteration, iterDuration, &output) {
			return output, nil
		}
		choice := resp.Choices[0]
		if e.config.ParseReasoningToolCalls && len(choice.Message.ToolCalls) == 0 && len(tools) > 0 {
			choice.Message.ToolCalls = parseReasoningContentToolCalls(choice.Message.ReasoningContent)
		}

		output.Iterations = append(output.Iterations, IterationUsage{
			Iteration:        iteration,
			MessageCount:     len(messages),
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.PromptTokens + resp.Usage.CompletionTokens,
			FinishReason:     choice.FinishReason,
			ToolCalls:        len(choice.Message.ToolCalls),
			ToolNames:        oaiToolCallNames(choice.Message.ToolCalls),
		})

		if len(choice.Message.ToolCalls) > 0 {
			if e.config.PassthroughToolCalls {
				for _, tc := range choice.Message.ToolCalls {
					output.ToolCalls = append(output.ToolCalls, ToolCall{
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: json.RawMessage(tc.Function.Arguments),
					})
				}
				output.AssistantText = strings.TrimSpace(resolveAssistantContent(choice.Message))
				output.StopReason = StopReasonEndTurn
				return output, nil
			}
			var stop bool
			messages, tools, stop = e.handleToolCallIteration(ctx, choice.Message, choice.Message.ToolCalls, messages, tools, iteration, &output)
			if stop {
				return output, nil
			}
			continue
		}

		if e.enforceRequiredContextQuery(choice.Message, &messages, &output) {
			continue
		}

		e.finalizeAssistantChoice(ctx, choice.Message, choice.FinishReason, messages, &output)
		return output, nil
	}
}

func (e *LLMChatEngine) finishIfIterationExceeded(ctx context.Context, iteration int, messages []oaiMessage, output *EngineOutput) bool {
	if iteration <= e.config.MaxIterations {
		return false
	}
	if output.AssistantText == "" {
		finalizeMessages := append(messages, oaiMessage{
			Role:    "user",
			Content: boundedFinalizePrompt("Your tool budget is exhausted.", output.ToolCalls),
		})
		if finalResp, err := e.callLLM(ctx, finalizeMessages, nil); err == nil && len(finalResp.Choices) > 0 {
			output.AssistantText = e.resolveAssistantContent(finalResp.Choices[0].Message)
			e.recordFinalizeUsage(output, finalResp, "max_iterations_finalize")
			fmt.Fprintf(os.Stderr, "[CONTEXT] finalize: prompt_tokens=%d completion_tokens=%d\n", finalResp.Usage.PromptTokens, finalResp.Usage.CompletionTokens)
		}
	}
	if output.StopReason != StopReasonError {
		output.StopReason = StopReasonMaxIterations
	}
	return true
}

func (e *LLMChatEngine) finishIfRunCanceled(ctx context.Context, output *EngineOutput) bool {
	if ctx.Err() == nil {
		return false
	}
	output.StopReason = StopReasonCancelled
	output.Error = ctx.Err().Error()
	return true
}

func (e *LLMChatEngine) finishIfRunCallFailed(ctx context.Context, err error, output *EngineOutput) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		output.StopReason = StopReasonCancelled
		output.Error = ctx.Err().Error()
		return true
	}
	output.StopReason = StopReasonError
	output.Error = err.Error()
	return true
}

func (e *LLMChatEngine) recordLLMResponse(ctx context.Context, resp *oaiResponse, messages []oaiMessage, iteration int, iterDuration time.Duration, output *EngineOutput) bool {
	logLLMIteration(ctx, e, resp, messages, iteration, iterDuration)
	output.Tokens.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	if e.finishIfContextBudgetExceeded(ctx, resp, iterDuration, output) {
		return false
	}
	if len(resp.Choices) == 0 {
		output.StopReason = StopReasonError
		output.Error = "no response from LLM"
		return false
	}
	return true
}

func logLLMIteration(ctx context.Context, e *LLMChatEngine, resp *oaiResponse, messages []oaiMessage, iteration int, iterDuration time.Duration) {
	if len(resp.Choices) == 0 {
		observability.Emit(ctx, observability.NewEvent("llm.no_choices").
			WithComponent(observability.ComponentAgent).
			WithData("message", "LLM returned no choices").
			Error(nil, 0))
		return
	}
	finishReason := resp.Choices[0].FinishReason
	promptTokens := resp.Usage.PromptTokens
	completionTokens := resp.Usage.CompletionTokens
	totalTokens := promptTokens + completionTokens
	fmt.Fprintf(os.Stderr, "[CONTEXT] iter=%d msgs=%d prompt_tokens=%d completion_tokens=%d total=%d finish=%s tool_calls=%d\n",
		iteration, len(messages), promptTokens, completionTokens, totalTokens, finishReason, len(resp.Choices[0].Message.ToolCalls))
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
		WithComponent(observability.ComponentAgent).
		WithSession(e.hookContext.SessionID, e.hookContext.ActorID).
		WithWorkspace(e.hookContext.WorkspaceID).
		WithData("iteration", iteration).
		WithData("message_count", len(messages)).
		WithData("prompt_tokens", promptTokens).
		WithData("completion_tokens", completionTokens).
		WithData("total_tokens", totalTokens).
		WithData("finish_reason", finishReason).
		WithData("tool_calls", len(resp.Choices[0].Message.ToolCalls)).
		WithData("provider", e.config.Provider).
		WithData("model", e.config.Model).
		Success(iterDuration))
}

func (e *LLMChatEngine) finishIfContextBudgetExceeded(ctx context.Context, resp *oaiResponse, iterDuration time.Duration, output *EngineOutput) bool {
	if e.config.MaxContextTokens <= 0 || resp.Usage.PromptTokens <= e.config.MaxContextTokens {
		return false
	}
	fmt.Fprintf(os.Stderr, "[CONTEXT] budget exceeded: %d > %d limit, stopping\n", resp.Usage.PromptTokens, e.config.MaxContextTokens)
	observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
		WithComponent(observability.ComponentAgent).
		WithSession(e.hookContext.SessionID, e.hookContext.ActorID).
		WithData("iteration", len(output.Iterations)+1).
		WithData("budget_exceeded", true).
		WithData("prompt_tokens", resp.Usage.PromptTokens).
		WithData("budget_limit", e.config.MaxContextTokens).
		Canceled(iterDuration))
	output.StopReason = StopReasonContextBudget
	if len(resp.Choices) > 0 {
		output.AssistantText = e.resolveAssistantContent(resp.Choices[0].Message)
	}
	return true
}

func (e *LLMChatEngine) handleToolCallIteration(ctx context.Context, choiceMessage oaiMessage, toolCalls []oaiToolCall, messages []oaiMessage, tools []oaiTool, iteration int, output *EngineOutput) ([]oaiMessage, []oaiTool, bool) {
	messages = append(messages, choiceMessage)
	totalToolResultChars := 0
	for _, tc := range toolCalls {
		stop, resultChars := e.executeToolCallIteration(ctx, tc, &messages, output)
		totalToolResultChars += resultChars
		if stop {
			return messages, tools, true
		}
	}
	if len(output.Iterations) > 0 {
		idx := len(output.Iterations) - 1
		output.Iterations[idx].ToolResultChars = totalToolResultChars
		output.Iterations[idx].ToolResultTokenEstimate = estimateEngineTokens(totalToolResultChars)
	}
	if e.shouldEnterSynthesisPhase(iteration) {
		tools = nil
		messages = append(messages, oaiMessage{
			Role:    "user",
			Content: boundedFinalizePrompt("SYNTHESIS PHASE: Your tool budget is ending.", output.ToolCalls),
		})
	}
	return messages, tools, false
}

func (e *LLMChatEngine) executeToolCallIteration(ctx context.Context, tc oaiToolCall, messages *[]oaiMessage, output *EngineOutput) (bool, int) {
	toolCall := ToolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: json.RawMessage(tc.Function.Arguments)}
	output.ToolCalls = append(output.ToolCalls, toolCall)
	if result, finalText, handled := maybeHandleFinalAnswerJSONTool(toolCall); handled {
		output.ToolResults = append(output.ToolResults, result)
		output.AssistantText = finalText
		output.StopReason = StopReasonEndTurn
		return true, 0
	}
	result := e.executeSingleToolCall(ctx, tc, toolCall, output)
	output.ToolResults = append(output.ToolResults, result)
	*messages = append(*messages, oaiMessage{Role: "tool", ToolCallID: tc.ID, Content: result.Content})
	return false, len(result.Content)
}

func (e *LLMChatEngine) executeSingleToolCall(ctx context.Context, tc oaiToolCall, toolCall ToolCall, output *EngineOutput) ToolResult {
	start := time.Now()
	preOutput, err := e.dispatchPreToolUse(ctx, toolCall)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("hook.pre_tool_use_error").
			WithComponent(observability.ComponentAgent).
			WithData("tool", toolCall.Name).
			Error(err, 0))
	}
	result := e.resolveToolCallResult(ctx, tc, toolCall, preOutput)
	postOutput := e.dispatchPostToolUse(ctx, toolCall, result, time.Since(start).Milliseconds())
	e.applyPostToolActions(ctx, toolCall, postOutput, &result, output)
	return result
}

func (e *LLMChatEngine) resolveToolCallResult(ctx context.Context, tc oaiToolCall, toolCall ToolCall, preOutput hooks.Output) ToolResult {
	if preOutput.Decision.IsBlocking() {
		return ToolResult{ToolCallID: tc.ID, Content: fmt.Sprintf("Blocked by hook: %s", preOutput.Reason), IsError: true}
	}
	execArgs := toolCall.Arguments
	if len(preOutput.UpdatedToolInput) > 0 {
		execArgs = preOutput.UpdatedToolInput
	}
	if e.toolRunner != nil {
		execCall := ToolCall{ID: toolCall.ID, Name: toolCall.Name, Arguments: execArgs}
		result, _ := e.toolRunner.Execute(ctx, execCall)
		return result
	}
	return ToolResult{ToolCallID: tc.ID, Content: fmt.Sprintf("Tool %q not available", tc.Function.Name), IsError: true}
}

func (e *LLMChatEngine) applyPostToolActions(ctx context.Context, toolCall ToolCall, postOutput hooks.Output, result *ToolResult, output *EngineOutput) {
	if e.config.ActionExecutor == nil || len(postOutput.Actions) == 0 {
		return
	}
	hookInput := hooks.Input{
		Event:         hooks.EventPostToolUse,
		ToolName:      toolCall.Name,
		SessionID:     e.hookContext.SessionID,
		ActorID:       e.hookContext.ActorID,
		WorkspaceID:   e.hookContext.WorkspaceID,
		WorkspaceRoot: e.hookContext.WorkspaceRoot,
	}
	injectedCtx, _ := e.config.ActionExecutor.Execute(ctx, postOutput.Actions, hookInput)
	if injectedCtx == "" {
		return
	}
	result.Content = result.Content + "\n\n---\n" + injectedCtx
	output.InjectedContexts = append(output.InjectedContexts, InjectedContext{
		ToolCallID: toolCall.ID,
		Source:     "PostToolUse:" + toolCall.Name,
		Content:    injectedCtx,
	})
}

func (e *LLMChatEngine) shouldEnterSynthesisPhase(iteration int) bool {
	return e.config.SynthesisReserve > 0 &&
		e.config.MaxIterations > e.config.SynthesisReserve &&
		iteration == e.config.MaxIterations-e.config.SynthesisReserve
}

func (e *LLMChatEngine) enforceRequiredContextQuery(choiceMessage oaiMessage, messages *[]oaiMessage, output *EngineOutput) bool {
	if os.Getenv("FOXCTL_DEBUG_CONTEXT_QUERY") == "1" {
		qc := -1
		if e.rlmExecutor != nil {
			qc = e.rlmExecutor.QueryCount()
		}
		fmt.Fprintf(os.Stderr, "[CTX-POLICY] pre-check stateless=%t require=%t query_count=%d\n", e.config.StatelessMode, e.config.RequireContextQuery, qc)
	}
	if !(e.config.StatelessMode && e.config.RequireContextQuery) {
		return false
	}
	if e.rlmExecutor != nil && e.rlmExecutor.QueryCount() > 0 {
		if os.Getenv("FOXCTL_DEBUG_CONTEXT_QUERY") == "1" {
			fmt.Fprintf(os.Stderr, "[CTX-POLICY] satisfied context query count=%d\n", e.rlmExecutor.QueryCount())
		}
		return false
	}
	if os.Getenv("FOXCTL_DEBUG_CONTEXT_QUERY") == "1" {
		fmt.Fprintf(os.Stderr, "[CTX-POLICY] unsatisfied context query: nudging model and continuing\n")
	}
	assistantText := e.resolveAssistantContent(choiceMessage)
	*messages = append(*messages,
		oaiMessage{Role: "assistant", Content: assistantText},
		oaiMessage{Role: "user", Content: "You MUST use the rlm_context_query tool to retrieve conversation context BEFORE responding. Please query context first, then respond."},
	)
	return true
}

func (e *LLMChatEngine) finalizeAssistantChoice(ctx context.Context, choiceMessage oaiMessage, finishReason string, messages []oaiMessage, output *EngineOutput) {
	output.AssistantText = e.resolveAssistantContent(choiceMessage)
	output.StopReason = mapFinishReason(finishReason)
	if strings.TrimSpace(output.AssistantText) != "" {
		return
	}
	finalPrompt := boundedFinalizePrompt("You returned an empty response.", nil)
	if len(output.ToolCalls) > 0 {
		finalPrompt = boundedFinalizePrompt("You stopped without producing a text response.", output.ToolCalls)
	}
	finalizeMessages := append(messages,
		oaiMessage{Role: "assistant", Content: ""},
		oaiMessage{Role: "user", Content: finalPrompt},
	)
	if finalResp, finalErr := e.callLLM(ctx, finalizeMessages, nil); finalErr == nil && len(finalResp.Choices) > 0 {
		output.AssistantText = strings.TrimSpace(e.resolveAssistantContent(finalResp.Choices[0].Message))
		e.recordFinalizeUsage(output, finalResp, "empty_response_finalize")
		fmt.Fprintf(os.Stderr, "[CONTEXT] finalize (early stop): prompt_tokens=%d completion_tokens=%d\n", finalResp.Usage.PromptTokens, finalResp.Usage.CompletionTokens)
	}
}

func (e *LLMChatEngine) recordFinalizeUsage(output *EngineOutput, resp *oaiResponse, finishReason string) {
	if output == nil || resp == nil {
		return
	}
	output.Tokens.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	output.Iterations = append(output.Iterations, IterationUsage{
		Iteration:        len(output.Iterations) + 1,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.PromptTokens + resp.Usage.CompletionTokens,
		FinishReason:     finishReason,
	})
	if e.config.MaxTokens > 0 && resp.Usage.CompletionTokens > e.config.MaxTokens {
		finalText := ""
		if len(resp.Choices) > 0 {
			finalText = strings.TrimSpace(e.resolveAssistantContent(resp.Choices[0].Message))
		}
		if finalText != "" && len(finalText) <= compactFinalizeVisibleCharLimit(e.config.MaxTokens) {
			return
		}
		output.StopReason = StopReasonError
		output.Error = fmt.Sprintf("model completion exceeded configured max tokens during finalize: completion_tokens=%d max_tokens=%d", resp.Usage.CompletionTokens, e.config.MaxTokens)
	}
}

func oaiToolCallNames(calls []oaiToolCall) []string {
	names := make([]string, 0, len(calls))
	seen := map[string]struct{}{}
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func estimateEngineTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return chars / 4
}

func boundedFinalizePrompt(reason string, toolCalls []ToolCall) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "Finalize the answer now."
	}
	var b strings.Builder
	b.WriteString(reason)
	b.WriteString("\n\nWrite the final answer now. Keep it bounded and task-shaped:\n")
	b.WriteString("- If the user requested an exact output format, return only that format.\n")
	b.WriteString("- Do not write a complete report, dependency graph, scratch work, tool log, or proof unless explicitly requested.\n")
	b.WriteString("- If blocked, return one short blocker line and the best partial answer.\n")
	b.WriteString("- Otherwise keep the answer under 120 words.\n")
	if len(toolCalls) > 0 {
		b.WriteString("\nResearch summary:\n")
		b.WriteString(buildResearchSummary(toolCalls))
	}
	return b.String()
}

func compactFinalizeVisibleCharLimit(maxTokens int) int {
	if maxTokens <= 0 {
		return 8192
	}
	limit := maxTokens * 4
	if limit < 2048 {
		return 2048
	}
	if limit > 8192 {
		return 8192
	}
	return limit
}

// buildMessages converts EngineInput messages to OpenAI format.
func (e *LLMChatEngine) buildMessages(input EngineInput) []oaiMessage {
	var messages []oaiMessage

	// Build system prompt
	systemPrompt := input.SystemPrompt
	if e.config.StatelessMode && e.config.RLMSystemPromptSuffix != "" {
		if systemPrompt != "" {
			systemPrompt += "\n\n" + e.config.RLMSystemPromptSuffix
		} else {
			systemPrompt = e.config.RLMSystemPromptSuffix
		}
	}

	// Add system prompt if provided
	if systemPrompt != "" {
		messages = append(messages, oaiMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	// In StatelessMode, only include the current user message (last message)
	// No conversation history is accumulated
	if e.config.StatelessMode {
		// Find the last user message
		for i := len(input.Messages) - 1; i >= 0; i-- {
			if input.Messages[i].Role == RoleUser {
				messages = append(messages, oaiMessage{
					Role:    "user",
					Content: input.Messages[i].Content,
				})
				break
			}
		}
		return messages
	}

	// Normal mode: Convert all input messages
	for _, msg := range input.Messages {
		oaiMsg := oaiMessage{
			Role:    msg.Role,
			Content: msg.Content,
		}

		// Handle tool calls in assistant messages
		if len(msg.ToolCalls) > 0 {
			oaiMsg.ToolCalls = make([]oaiToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				oaiMsg.ToolCalls[i] = oaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: oaiFunction{
						Name:      tc.Name,
						Arguments: string(tc.Arguments),
					},
				}
			}
		}

		// Handle tool result messages
		if msg.Role == RoleTool {
			oaiMsg.ToolCallID = msg.ToolCallID
		}

		messages = append(messages, oaiMsg)
	}

	return messages
}

// buildTools converts ToolDef to OpenAI format.
func (e *LLMChatEngine) buildTools(tools []ToolDef) []oaiTool {
	if len(tools) == 0 {
		return nil
	}

	oaiTools := make([]oaiTool, len(tools))
	for i, t := range tools {
		oaiTools[i] = oaiTool{
			Type:     "function",
			Function: oaiToolFunction(t),
		}
	}
	return oaiTools
}

// callLLM makes the API request.
func (e *LLMChatEngine) callLLM(ctx context.Context, messages []oaiMessage, tools []oaiTool) (*oaiResponse, error) {
	// Bedrock uses the AWS SDK Converse API instead of HTTP.
	if e.bedrockClient != nil {
		return e.bedrockClient.Converse(ctx, e.config.Model, messages, tools, e.config.Temperature, e.config.MaxTokens)
	}

	reqBody := oaiRequest{
		Model:       e.config.Model,
		Messages:    messages,
		Temperature: e.config.Temperature,
		MaxTokens:   e.config.MaxTokens,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
		if len(e.config.ToolChoice) > 0 {
			reqBody.ToolChoice = e.config.ToolChoice
		}
	}
	if len(e.config.ResponseFormat) > 0 {
		reqBody.ResponseFormat = e.config.ResponseFormat
	}

	requestBody, err := oaiRequestBodyMap(reqBody, e.config.ExtraBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.config.BaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	applyAuthHeader(req, e.config)

	// OpenRouter-specific headers
	if e.config.Provider == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://foxctl.dev")
		req.Header.Set("X-Title", "foxctl")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if oaiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}

	if os.Getenv("FOXCTL_DEBUG_LLM_EMPTY") == "1" && len(oaiResp.Choices) == 0 {
		raw := string(body)
		if len(raw) > 800 {
			raw = raw[:800] + "...(truncated)"
		}
		fmt.Fprintf(os.Stderr, "[LLM-NO-CHOICES] provider=%s model=%s raw=%s\n", e.config.Provider, e.config.Model, raw)
	}

	if os.Getenv("FOXCTL_DEBUG_LLM_EMPTY") == "1" &&
		len(oaiResp.Choices) > 0 &&
		e.resolveAssistantContent(oaiResp.Choices[0].Message) == "" &&
		len(oaiResp.Choices[0].Message.ToolCalls) == 0 {
		raw := string(body)
		if len(raw) > 800 {
			raw = raw[:800] + "...(truncated)"
		}
		fmt.Fprintf(os.Stderr, "[LLM-EMPTY] provider=%s model=%s raw=%s\n", e.config.Provider, e.config.Model, raw)
	}

	return &oaiResp, nil
}

func oaiRequestBodyMap(req oaiRequest, extra map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" || value == nil {
			continue
		}
		body[key] = value
	}
	return body, nil
}

func (e *LLMChatEngine) resolveAssistantContent(msg oaiMessage) string {
	return resolveAssistantContentWithReasoning(msg, e.config.UseReasoningContentAsText)
}

func resolveAssistantContent(msg oaiMessage) string {
	return resolveAssistantContentWithReasoning(msg, false)
}

func resolveAssistantContentWithReasoning(msg oaiMessage, useReasoning bool) string {
	if strings.TrimSpace(msg.Content) != "" {
		return msg.Content
	}
	if strings.TrimSpace(msg.OutputText) != "" {
		return msg.OutputText
	}
	if useReasoning && strings.TrimSpace(msg.ReasoningContent) != "" {
		return msg.ReasoningContent
	}
	return ""
}

func parseReasoningContentToolCalls(content string) []oaiToolCall {
	content = strings.TrimSpace(content)
	if content == "" || !strings.Contains(content, "<tool_call>") {
		return nil
	}
	var calls []oaiToolCall
	remaining := content
	for {
		start := strings.Index(remaining, "<function=")
		if start < 0 {
			break
		}
		remaining = remaining[start+len("<function="):]
		nameEnd := strings.Index(remaining, ">")
		if nameEnd < 0 {
			break
		}
		name := strings.TrimSpace(remaining[:nameEnd])
		remaining = remaining[nameEnd+1:]
		closeIdx := strings.Index(remaining, "</function>")
		if closeIdx < 0 {
			break
		}
		args := strings.TrimSpace(remaining[:closeIdx])
		remaining = remaining[closeIdx+len("</function>"):]
		if name == "" || !json.Valid([]byte(args)) {
			continue
		}
		calls = append(calls, oaiToolCall{
			ID:   fmt.Sprintf("reasoning_call_%d", len(calls)+1),
			Type: "function",
			Function: oaiFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}
	return calls
}

// OpenAI API types

type oaiRequest struct {
	Model          string          `json:"model"`
	Messages       []oaiMessage    `json:"messages"`
	Tools          []oaiTool       `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
}

type oaiMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content,omitempty"`
	OutputText       string        `json:"output_text,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
}

type oaiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type oaiResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      oaiMessage `json:"message"`
		FinishReason string     `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Hook dispatch methods

// dispatchPreToolUse dispatches the PreToolUse hook before tool execution.
// Returns the merged output from all matching hooks.
func (e *LLMChatEngine) dispatchPreToolUse(ctx context.Context, call ToolCall) (hooks.Output, error) {
	if e.config.HookDispatcher == nil {
		return hooks.NewApprove("no dispatcher", nil), nil
	}

	input := hooks.Input{
		Event:         hooks.EventPreToolUse,
		ToolName:      call.Name,
		ToolCanonical: call.Name,
		ToolKind:      hooks.ClassifyToolKind(call.Name, call.Name),
		ToolInput:     call.Arguments,
		SessionID:     e.hookContext.SessionID,
		ActorID:       e.hookContext.ActorID,
		WorkspaceID:   e.hookContext.WorkspaceID,
		WorkspaceRoot: e.hookContext.WorkspaceRoot,
	}

	result, err := e.config.HookDispatcher.Dispatch(ctx, input)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("hook.pre_tool_dispatch_failed").
			WithComponent(observability.ComponentAgent).
			WithData("tool", call.Name).
			Error(err, 0))
		return hooks.NewApprove("hook error (fail-open)", nil), nil
	}
	return result.Output, nil
}

// dispatchPostToolUse dispatches the PostToolUse hook after tool execution.
func (e *LLMChatEngine) dispatchPostToolUse(ctx context.Context, call ToolCall, result ToolResult, durationMS int64) hooks.Output {
	if e.config.HookDispatcher == nil {
		return hooks.NewNone()
	}

	// Prepare observation (what goes back to LLM)
	observation, _ := json.Marshal(map[string]any{
		"content":  result.Content,
		"is_error": result.IsError,
	})

	input := hooks.Input{
		Event:           hooks.EventPostToolUse,
		ToolName:        call.Name,
		ToolCanonical:   call.Name,
		ToolKind:        hooks.ClassifyToolKind(call.Name, call.Name),
		ToolInput:       call.Arguments,
		ToolObservation: observation,
		ToolDurationMS:  durationMS,
		SessionID:       e.hookContext.SessionID,
		ActorID:         e.hookContext.ActorID,
		WorkspaceID:     e.hookContext.WorkspaceID,
		WorkspaceRoot:   e.hookContext.WorkspaceRoot,
	}

	if result.IsError {
		input.ToolError = result.Content
	}

	hookResult, err := e.config.HookDispatcher.Dispatch(ctx, input)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("hook.post_tool_dispatch_failed").
			WithComponent(observability.ComponentAgent).
			WithData("tool", call.Name).
			Error(err, 0))
		return hooks.NewNone()
	}
	return hookResult.Output
}

// buildResearchSummary extracts a structured summary from accumulated tool calls.
// Used by finalize and synthesis prompts to give the model context about what
// was researched before asking it to write.
func buildResearchSummary(toolCalls []ToolCall) string {
	var files, searches []string
	seen := make(map[string]bool)
	for _, tc := range toolCalls {
		var args map[string]any
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			continue
		}
		switch tc.Name {
		case "fs_read_file":
			if p, _ := args["path"].(string); p != "" && !seen["f:"+p] {
				seen["f:"+p] = true
				files = append(files, filepath.Base(p))
			}
		case "code_symbols":
			if p, _ := args["path"].(string); p != "" && !seen["s:"+p] {
				seen["s:"+p] = true
				files = append(files, filepath.Base(p)+" (symbols)")
			}
		case "context_filter":
			if q, _ := args["prompt"].(string); q != "" {
				searches = append(searches, `context_filter("`+q+`")`)
			}
		case "context_search":
			if q, _ := args["query"].(string); q != "" {
				searches = append(searches, tc.Name+`("`+q+`")`)
			}
		case "smart_search":
			q, _ := args["question"].(string)
			if q == "" {
				q, _ = args["query"].(string)
			}
			if q != "" {
				searches = append(searches, tc.Name+`("`+q+`")`)
			}
		case "code_search", "context_grep":
			q, _ := args["pattern"].(string)
			if q == "" {
				q, _ = args["query"].(string)
			}
			if q != "" {
				searches = append(searches, tc.Name+`("`+q+`")`)
			}
		case "repo_index_dag_grep":
			if q, _ := args["query"].(string); q != "" {
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

// Helper functions

// apiKeyForProvider returns the API key for a specific provider.
// Only includes providers that have full support (baseURL, defaultModel, detectProvider).
func apiKeyForProvider(provider string) string {
	provider = normalizeEngineProvider(provider)
	switch provider {
	case "cerebras":
		return os.Getenv("CEREBRAS_API_KEY")
	case "openrouter":
		return os.Getenv("OPENROUTER_API_KEY")
	case "groq":
		return os.Getenv("GROQ_API_KEY")
	case "openai":
		return os.Getenv("OPENAI_API_KEY")
	case "lmstudio":
		// LM Studio doesn't require a real API key; use a placeholder
		if key := os.Getenv("LMSTUDIO_API_KEY"); key != "" {
			return key
		}
		return "lm-studio"
	case "bedrock":
		// Bedrock uses AWS IAM credentials, not an API key
		return "bedrock-iam"
	default:
		return ""
	}
}

func detectProvider() (apiKey, provider string) {
	// Default to LM Studio when no explicit provider is configured.
	// LM Studio is local and does not require a real API key.
	if key := os.Getenv("LMSTUDIO_API_KEY"); key != "" {
		return key, "lmstudio"
	}
	if os.Getenv("LMSTUDIO_BASE_URL") != "" || os.Getenv("LMSTUDIO_MODEL") != "" {
		return apiKeyForProvider("lmstudio"), "lmstudio"
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return key, "openrouter"
	}
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" {
		return key, "cerebras"
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		return key, "groq"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key, "openai"
	}
	if os.Getenv("BEDROCK_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
		return "bedrock-iam", "bedrock"
	}
	return apiKeyForProvider("lmstudio"), "lmstudio"
}

func baseURLForProvider(provider string) string {
	provider = normalizeEngineProvider(provider)
	switch provider {
	case "cerebras":
		return "https://api.cerebras.ai/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "lmstudio":
		if url := os.Getenv("LMSTUDIO_BASE_URL"); url != "" {
			return url
		}
		return "http://localhost:1234/v1"
	case "bedrock":
		return "" // Uses AWS SDK, not HTTP base URL
	default:
		return "https://api.openai.com/v1"
	}
}

func normalizeEngineProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "openai-compatible", "openai_compat":
		return "openai_compat"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func defaultAuthModeForProvider(provider, apiKey string) string {
	switch normalizeEngineProvider(provider) {
	case "bedrock":
		return "none"
	case "lmstudio", "openai_compat":
		if strings.TrimSpace(apiKey) == "" {
			return "none"
		}
		return "bearer"
	default:
		return "bearer"
	}
}

func defaultAuthHeader(mode string) string {
	if strings.EqualFold(strings.TrimSpace(mode), "header") {
		return "X-API-Key"
	}
	return "Authorization"
}

func authRequiresCredential(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "auto":
		return false
	case "none":
		return false
	default:
		return true
	}
}

func applyAuthHeader(req *http.Request, cfg LLMChatConfig) {
	switch strings.ToLower(strings.TrimSpace(cfg.AuthMode)) {
	case "", "none":
		return
	case "bearer":
		req.Header.Set("Authorization", cfg.AuthPrefix+cfg.APIKey)
	case "header":
		header := cfg.AuthHeader
		if header == "" {
			header = "X-API-Key"
		}
		req.Header.Set(header, cfg.AuthPrefix+cfg.APIKey)
	}
}

func mapFinishReason(reason string) StopReason {
	switch reason {
	case "stop":
		return StopReasonEndTurn
	case "length":
		return StopReasonMaxTokens
	case "tool_calls":
		return StopReasonEndTurn // Will continue in loop
	default:
		return StopReasonEndTurn
	}
}
