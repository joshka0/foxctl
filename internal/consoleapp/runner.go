// Package consoleapp provides the console application/runtime layer, including
// turn execution and stream handling for console sessions.
package consoleapp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	consolepkg "github.com/jkatigb/agentctl/internal/console"
	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runtime/observability"
)

// Runner executes LLM requests for console sessions.
type Runner struct {
	baseConfig engine.LLMChatConfig
	tools      []engine.ToolDef
}

// RunnerConfig configures the console runner.
type RunnerConfig struct {
	// BaseConfig is used to create an LLMChatEngine per turn.
	// Per-turn overrides (e.g. provider/model) are applied from session metadata.
	BaseConfig engine.LLMChatConfig

	// Tools are the available tool definitions.
	Tools []engine.ToolDef
}

// NewRunner creates a new console runner.
func NewRunner(cfg RunnerConfig) *Runner {
	return &Runner{
		baseConfig: cfg.BaseConfig,
		tools:      cfg.Tools,
	}
}

// NewDefaultRunnerFactory builds the default console runner factory used by
// the web transport.
func NewDefaultRunnerFactory(ctx context.Context) consolepkg.RunnerFactory {
	config.LoadDotEnv()

	baseCfg := engine.LLMChatConfig{
		MaxIterations: 20,
		Temperature:   0.0,
		MaxTokens:     4096,
	}

	return func(session consolepkg.SessionHandle) consolepkg.Runner {
		_, err := engine.NewLLMChatEngine(baseCfg)
		if err != nil {
			observability.Emit(ctx, observability.NewEvent("console.runner_factory_invalid").
				WithComponent("console").
				WithSession(session.ID(), "").
				Error(err, 0))
			return nil
		}

		return NewRunner(RunnerConfig{
			BaseConfig: baseCfg,
			Tools:      nil,
		})
	}
}

// Run implements the console session runner contract.
func (r *Runner) Run(ctx context.Context, session consolepkg.SessionHandle, userMessage string, correlationID string) error {
	start := time.Now()

	// Create a new engine each turn so we can apply per-turn overrides.
	engineCfg := r.baseConfig
	meta := session.InFlightMetadata(correlationID)
	if meta != nil {
		if provider, ok := meta["llm_provider"].(string); ok && strings.TrimSpace(provider) != "" {
			engineCfg.Provider = strings.TrimSpace(provider)
			// Force NewLLMChatEngine to resolve the correct key for the overridden provider.
			engineCfg.APIKey = ""
		}
		if model, ok := meta["llm_model"].(string); ok && strings.TrimSpace(model) != "" {
			engineCfg.Model = strings.TrimSpace(model)
		}
	}

	llmEngine, err := engine.NewLLMChatEngine(engineCfg)
	if err != nil {
		return fmt.Errorf("create engine: %w", err)
	}

	// Build engine input from session history
	input := r.buildInput(session, userMessage)

	// Log run start
	observability.Emit(ctx, observability.NewEvent("console.run_start").
		WithComponent("console").
		WithSession(session.ID(), "").
		WithData("correlation_id", correlationID).
		WithData("message_count", len(input.Messages)).
		WithData("user_message_len", len(userMessage)).
		Success(0))

	// Stream callback for tool calls and partial responses
	streamCallback := &StreamCallback{
		session:       session,
		correlationID: correlationID,
	}

	// Run the engine
	output, err := llmEngine.Run(ctx, input)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("console.engine_error").
			WithComponent("console").
			WithSession(session.ID(), "").
			WithData("correlation_id", correlationID).
			WithData("error", err.Error()).
			Error(err, time.Since(start)))
		return fmt.Errorf("engine run: %w", err)
	}

	// Emit tool call events
	for i, tc := range output.ToolCalls {
		var result string
		if i < len(output.ToolResults) {
			result = output.ToolResults[i].Content
		}
		streamCallback.EmitToolCall(tc, result)
	}

	// Check for errors
	if output.StopReason == engine.StopReasonError {
		observability.Emit(ctx, observability.NewEvent("console.stop_reason_error").
			WithComponent("console").
			WithSession(session.ID(), "").
			WithData("correlation_id", correlationID).
			WithData("error", output.Error).
			Error(nil, time.Since(start)))
		return fmt.Errorf("engine error: %s", output.Error)
	}

	if output.StopReason == engine.StopReasonCancelled {
		observability.Emit(ctx, observability.NewEvent("console.stop_reason_cancelled").
			WithComponent("console").
			WithSession(session.ID(), "").
			WithData("correlation_id", correlationID).
			Canceled(time.Since(start)))
		return ctx.Err()
	}

	// Add assistant response to session history with tool calls and injected context metadata
	if output.AssistantText != "" || len(output.ToolCalls) > 0 {
		msg := consolepkg.Message{
			Role:    "assistant",
			Content: output.AssistantText,
		}

		// Build metadata
		metadata := make(map[string]any)

		// Add tool calls to metadata if present
		if len(output.ToolCalls) > 0 {
			const (
				maxResultSize    = 64 * 1024 // 64KB threshold for truncation
				truncatedSummary = 2 * 1024  // 2KB summary for truncated results
			)
			toolCallsData := make([]map[string]any, 0, len(output.ToolCalls))
			for i, tc := range output.ToolCalls {
				tcData := map[string]any{
					"id":        tc.ID,
					"name":      tc.Name,
					"arguments": json.RawMessage(tc.Arguments),
				}
				// Add result if available, with size-based truncation
				if i < len(output.ToolResults) {
					result := output.ToolResults[i].Content
					originalSize := len(result)
					if originalSize > maxResultSize {
						// Truncate large results and add metadata
						tcData["result"] = result[:truncatedSummary] + fmt.Sprintf("\n\n[truncated - %d bytes total]", originalSize)
						tcData["result_truncated"] = true
						tcData["result_original_size"] = originalSize
					} else {
						tcData["result"] = result
					}
					tcData["is_error"] = output.ToolResults[i].IsError
				}
				toolCallsData = append(toolCallsData, tcData)
			}
			metadata["tool_calls"] = toolCallsData
		}

		// Add injected contexts to metadata if present
		if len(output.InjectedContexts) > 0 {
			injectedData := make([]map[string]any, 0, len(output.InjectedContexts))
			for _, ic := range output.InjectedContexts {
				injectedData = append(injectedData, map[string]any{
					"tool_call_id": ic.ToolCallID,
					"source":       ic.Source,
					"content":      ic.Content,
				})
			}
			metadata["injected_contexts"] = injectedData
		}

		if len(metadata) > 0 {
			msg.Metadata = metadata
		}

		session.AddMessage(msg)
	}

	// Emit final reply
	streamCallback.EmitReply(output.AssistantText)

	observability.Emit(ctx, observability.NewEvent("console.run_complete").
		WithComponent("console").
		WithSession(session.ID(), "").
		WithData("correlation_id", correlationID).
		WithData("stop_reason", string(output.StopReason)).
		WithData("tool_calls", len(output.ToolCalls)).
		WithData("input_tokens", output.Tokens.InputTokens).
		WithData("output_tokens", output.Tokens.OutputTokens).
		Success(time.Since(start)))

	return nil
}

// buildInput builds the engine input from session history.
func (r *Runner) buildInput(session consolepkg.SessionHandle, userMessage string) engine.EngineInput {
	// Convert session messages to engine messages
	history := session.Messages()
	messages := make([]engine.Message, 0, len(history)+1)

	for _, msg := range history {
		messages = append(messages, engine.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Add current user message
	messages = append(messages, engine.NewUserMessage(userMessage))

	return engine.EngineInput{
		Messages:     messages,
		Tools:        r.tools,
		SystemPrompt: session.SystemPrompt(),
		Workspace:    session.Workspace(),
		SessionID:    session.ID(),
	}
}

// StreamCallback handles streaming events from the engine.
type StreamCallback struct {
	session       consolepkg.SessionHandle
	correlationID string
}

// EmitToolCall emits a tool call event.
func (s *StreamCallback) EmitToolCall(tc engine.ToolCall, result string) {
	// Emit tool call event
	s.session.BroadcastEvent(s.correlationID, fmt.Sprintf("Tool: %s", tc.Name), map[string]any{
		"tool":      tc.Name,
		"tool_id":   tc.ID,
		"arguments": json.RawMessage(tc.Arguments),
		"partial":   false,
		"phase":     "call",
	})

	// Emit tool result event
	if result != "" {
		// Truncate long results for display
		displayResult := result
		if len(displayResult) > 1000 {
			displayResult = displayResult[:1000] + "... (truncated)"
		}

		s.session.BroadcastEvent(s.correlationID, displayResult, map[string]any{
			"tool":    tc.Name,
			"tool_id": tc.ID,
			"partial": false,
			"phase":   "result",
		})
	}
}

// EmitPartialText emits a partial text event (for streaming).
func (s *StreamCallback) EmitPartialText(text string) {
	s.session.BroadcastEvent(s.correlationID, text, map[string]any{
		"partial": true,
	})
}

// EmitReply emits the final reply.
func (s *StreamCallback) EmitReply(text string) {
	s.session.BroadcastReply(s.correlationID, text)
}
