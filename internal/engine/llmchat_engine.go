package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/observability"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
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
	config      LLMChatConfig
	client      *http.Client
	toolRunner  *ToolRunner
	rlmExecutor *RLMToolExecutor // For tracking RLM context queries
	hookContext HookContext      // Context for hook dispatch
}

// LLMChatConfig configures the LLM chat engine.
type LLMChatConfig struct {
	// Provider is the LLM provider: "openrouter", "groq", "openai"
	Provider string

	// APIKey is the API key for the provider.
	APIKey string

	// BaseURL is the API base URL. Auto-detected if empty.
	BaseURL string

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
	// Resolve API key: if provider is specified, get key for that provider
	if cfg.APIKey == "" && cfg.Provider != "" {
		cfg.APIKey = apiKeyForProvider(cfg.Provider)
		// Error if explicit provider but no key found - don't auto-detect
		if cfg.APIKey == "" {
			return nil, fmt.Errorf("no API key configured for provider %q (set the appropriate env var)", cfg.Provider)
		}
	}
	// Fall back to auto-detect only if no provider specified
	if cfg.APIKey == "" && cfg.Provider == "" {
		cfg.APIKey, cfg.Provider = detectProvider()
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured (set CEREBRAS_API_KEY, OPENROUTER_API_KEY, GROQ_API_KEY, or OPENAI_API_KEY)")
	}

	// Set base URL based on provider
	if cfg.BaseURL == "" {
		cfg.BaseURL = baseURLForProvider(cfg.Provider)
	}

	// Set default model based on provider
	if cfg.Model == "" {
		cfg.Model = llmproviders.DefaultModelForProvider(cfg.Provider)
	}

	// Apply defaults
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = 50
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 8192
	}

	engine := &LLMChatEngine{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}

	return engine, nil
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

		// Check iteration limit
		iteration++
		if iteration > e.config.MaxIterations {
			output.StopReason = StopReasonMaxIterations
			output.Error = fmt.Sprintf("exceeded max iterations (%d)", e.config.MaxIterations)
			return output, nil
		}

		// Check context cancellation
		if ctx.Err() != nil {
			output.StopReason = StopReasonCancelled
			output.Error = ctx.Err().Error()
			return output, nil
		}

		// Call LLM
		resp, err := e.callLLM(ctx, messages, tools)
		iterDuration := time.Since(iterStart)
		if err != nil {
			// Check if error is due to context cancellation
			if ctx.Err() != nil {
				output.StopReason = StopReasonCancelled
				output.Error = ctx.Err().Error()
				return output, nil
			}
			output.StopReason = StopReasonError
			output.Error = err.Error()
			return output, nil
		}

		// Log response details with context tracking
		if len(resp.Choices) > 0 {
			finishReason := resp.Choices[0].FinishReason
			promptTokens := resp.Usage.PromptTokens
			completionTokens := resp.Usage.CompletionTokens
			totalTokens := promptTokens + completionTokens

			// Per-iteration context tracking (stderr for visibility)
			fmt.Fprintf(os.Stderr, "[CONTEXT] iter=%d msgs=%d prompt_tokens=%d completion_tokens=%d total=%d finish=%s\n",
				iteration, len(messages), promptTokens, completionTokens, totalTokens, finishReason)

			// Emit structured wide event for observability
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
		} else {
			observability.Emit(ctx, observability.NewEvent("llm.no_choices").
				WithComponent(observability.ComponentAgent).
				WithData("message", "LLM returned no choices").
				Error(nil, 0))
		}

		// Track tokens
		output.Tokens.Add(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

		// Check context budget (stop before next iteration if exceeded)
		if e.config.MaxContextTokens > 0 && resp.Usage.PromptTokens > e.config.MaxContextTokens {
			fmt.Fprintf(os.Stderr, "[CONTEXT] budget exceeded: %d > %d limit, stopping\n",
				resp.Usage.PromptTokens, e.config.MaxContextTokens)

			observability.Emit(ctx, observability.NewEvent(observability.OpAgentIteration).
				WithComponent(observability.ComponentAgent).
				WithSession(e.hookContext.SessionID, e.hookContext.ActorID).
				WithData("iteration", iteration).
				WithData("budget_exceeded", true).
				WithData("prompt_tokens", resp.Usage.PromptTokens).
				WithData("budget_limit", e.config.MaxContextTokens).
				Canceled(iterDuration))

			output.StopReason = StopReasonContextBudget
			output.Error = fmt.Sprintf("context budget exceeded (%d tokens > %d limit)", resp.Usage.PromptTokens, e.config.MaxContextTokens)
			// Still capture any assistant text from this response
			if len(resp.Choices) > 0 && resp.Choices[0].Message.Content != "" {
				output.AssistantText = resp.Choices[0].Message.Content
			}
			return output, nil
		}

		// Check for tool calls
		if len(resp.Choices) == 0 {
			output.StopReason = StopReasonError
			output.Error = "no response from LLM"
			return output, nil
		}

		choice := resp.Choices[0]

		// If tool calls present, execute them
		if len(choice.Message.ToolCalls) > 0 {
			// Add assistant message with tool calls to history
			messages = append(messages, choice.Message)

			// Execute each tool call
			for _, tc := range choice.Message.ToolCalls {
				toolCall := ToolCall{
					ID:        tc.ID,
					Name:      tc.Function.Name,
					Arguments: json.RawMessage(tc.Function.Arguments),
				}
				output.ToolCalls = append(output.ToolCalls, toolCall)

				var result ToolResult
				start := time.Now()

				// 1. Dispatch PreToolUse hook
				preOutput, err := e.dispatchPreToolUse(ctx, toolCall)
				if err != nil {
					observability.Emit(ctx, observability.NewEvent("hook.pre_tool_use_error").
						WithComponent(observability.ComponentAgent).
						WithData("tool", toolCall.Name).
						Error(err, 0))
				}

				// Check if blocked by hook
				if preOutput.Decision.IsBlocking() {
					result = ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Blocked by hook: %s", preOutput.Reason),
						IsError:    true,
					}
				} else {
					// Use updated args if hook modified them
					execArgs := toolCall.Arguments
					if len(preOutput.UpdatedToolInput) > 0 {
						execArgs = preOutput.UpdatedToolInput
					}

					// 2. Execute tool
					if e.toolRunner != nil {
						// Create a modified call with potentially updated args
						execCall := ToolCall{
							ID:        toolCall.ID,
							Name:      toolCall.Name,
							Arguments: execArgs,
						}
						result, _ = e.toolRunner.Execute(ctx, execCall)
					} else {
						result = ToolResult{
							ToolCallID: tc.ID,
							Content:    fmt.Sprintf("Tool %q not available", tc.Function.Name),
							IsError:    true,
						}
					}
				}

				// 3. Dispatch PostToolUse hook
				durationMS := time.Since(start).Milliseconds()
				postOutput := e.dispatchPostToolUse(ctx, toolCall, result, durationMS)

				// 4. Process hook actions
				if e.config.ActionExecutor != nil && len(postOutput.Actions) > 0 {
					hookInput := hooks.Input{
						Event:         hooks.EventPostToolUse,
						ToolName:      toolCall.Name,
						SessionID:     e.hookContext.SessionID,
						ActorID:       e.hookContext.ActorID,
						WorkspaceID:   e.hookContext.WorkspaceID,
						WorkspaceRoot: e.hookContext.WorkspaceRoot,
					}

					injectedCtx, _ := e.config.ActionExecutor.Execute(ctx, postOutput.Actions, hookInput)

					// Append injected context to tool result if present
					if injectedCtx != "" {
						result.Content = result.Content + "\n\n---\n" + injectedCtx

						// Track injected context for visibility
						output.InjectedContexts = append(output.InjectedContexts, InjectedContext{
							ToolCallID: toolCall.ID,
							Source:     "PostToolUse:" + toolCall.Name,
							Content:    injectedCtx,
						})
					}
				}

				output.ToolResults = append(output.ToolResults, result)

				// Add tool result to messages
				messages = append(messages, oaiMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Content:    result.Content,
				})
			}

			// Warn on second-to-last iteration so agent can wrap up
			if iteration == e.config.MaxIterations-1 {
				messages = append(messages, oaiMessage{
					Role:    "user",
					Content: "⚠️ IMPORTANT: This is your FINAL iteration. You must provide your complete response NOW. Do NOT call any more tools - just give your final answer/summary.",
				})
			}

			// Continue the loop
			continue
		}

		// No tool calls - this is the final response
		output.AssistantText = choice.Message.Content
		output.StopReason = mapFinishReason(choice.FinishReason)

		// In StatelessMode with RequireContextQuery, verify context was queried.
		// If the model skipped tool calling, nudge it to query context first.
		if e.config.StatelessMode && e.config.RequireContextQuery {
			if e.rlmExecutor == nil || e.rlmExecutor.QueryCount() == 0 {
				// Add the model's premature response as an assistant message, then
				// inject a user nudge asking it to query context before responding.
				messages = append(messages,
					oaiMessage{Role: "assistant", Content: choice.Message.Content},
					oaiMessage{Role: "user", Content: "You MUST use the rlm_context_query tool to retrieve conversation context BEFORE responding. Please query context first, then respond."},
				)
				continue
			}
		}

		return output, nil
	}
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
	reqBody := oaiRequest{
		Model:       e.config.Model,
		Messages:    messages,
		Temperature: e.config.Temperature,
		MaxTokens:   e.config.MaxTokens,
	}

	if len(tools) > 0 {
		reqBody.Tools = tools
	}
	if len(e.config.ResponseFormat) > 0 {
		reqBody.ResponseFormat = e.config.ResponseFormat
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", e.config.BaseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.config.APIKey)

	// OpenRouter-specific headers
	if e.config.Provider == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://agentctl.dev")
		req.Header.Set("X-Title", "agentctl")
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

	return &oaiResp, nil
}

// OpenAI API types

type oaiRequest struct {
	Model          string          `json:"model"`
	Messages       []oaiMessage    `json:"messages"`
	Tools          []oaiTool       `json:"tools,omitempty"`
	Temperature    float64         `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
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

// Helper functions

// apiKeyForProvider returns the API key for a specific provider.
// Only includes providers that have full support (baseURL, defaultModel, detectProvider).
func apiKeyForProvider(provider string) string {
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
	default:
		return ""
	}
}

func detectProvider() (apiKey, provider string) {
	if key := os.Getenv("CEREBRAS_API_KEY"); key != "" {
		return key, "cerebras"
	}
	if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		return key, "openrouter"
	}
	if key := os.Getenv("GROQ_API_KEY"); key != "" {
		return key, "groq"
	}
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		return key, "openai"
	}
	return "", ""
}

func baseURLForProvider(provider string) string {
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
	default:
		return "https://api.openai.com/v1"
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
