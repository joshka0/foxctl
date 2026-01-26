// Package consoleapp provides the LLM runner for console sessions.
package consoleapp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/engine"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

// Runner executes LLM requests for console sessions.
type Runner struct {
	engine engine.AgentEngine
	tools  []engine.ToolDef
}

// RunnerConfig configures the console runner.
type RunnerConfig struct {
	// Engine is the LLM engine to use.
	Engine engine.AgentEngine

	// Tools are the available tool definitions.
	Tools []engine.ToolDef
}

// NewRunner creates a new console runner.
func NewRunner(cfg RunnerConfig) *Runner {
	return &Runner{
		engine: cfg.Engine,
		tools:  cfg.Tools,
	}
}

// Run implements consolews.Runner.
func (r *Runner) Run(ctx context.Context, session *consolews.Session, userMessage string, correlationID string) error {
	start := time.Now()

	// Build engine input from session history
	input := r.buildInput(session, userMessage)

	// Stream callback for tool calls and partial responses
	streamCallback := &StreamCallback{
		session:       session,
		correlationID: correlationID,
	}

	// Run the engine
	output, err := r.engine.Run(ctx, input)
	if err != nil {
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
		return fmt.Errorf("engine error: %s", output.Error)
	}

	if output.StopReason == engine.StopReasonCancelled {
		return ctx.Err()
	}

	// Add assistant response to session history
	if output.AssistantText != "" {
		session.AddMessage(consolews.Message{
			Role:    "assistant",
			Content: output.AssistantText,
		})
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
func (r *Runner) buildInput(session *consolews.Session, userMessage string) engine.EngineInput {
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
	session       *consolews.Session
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
