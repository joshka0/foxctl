// Package engine defines the AgentEngine interface for LLM-driven agent execution.
//
// The engine abstraction enables swapping LLM backends (DSPy, Claude API, etc.)
// while providing canonical hook integration points and tool execution.
package engine

import (
	"context"
	"encoding/json"
)

// AgentEngine executes a single agent turn with tool calls.
//
// Contract:
//   - Run executes until the LLM returns a final response (no tool calls)
//   - Context cancellation triggers graceful abort (StopReasonCancelled)
//   - Tool results > maxResultBytes are CAS-offloaded with artifact hint
type AgentEngine interface {
	// Run executes a single turn, returning when:
	// - LLM returns final response (no tool calls)
	// - Context is cancelled (preemption)
	// - Error occurs
	Run(ctx context.Context, input EngineInput) (EngineOutput, error)
}

// EngineInput is the input for a single engine turn.
type EngineInput struct {
	// Messages is the conversation history.
	Messages []Message `json:"messages"`

	// Tools are the available tool definitions.
	Tools []ToolDef `json:"tools,omitempty"`

	// SystemPrompt is the system instructions.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Workspace is the workspace root path.
	Workspace string `json:"workspace,omitempty"`

	// SessionID identifies the session.
	SessionID string `json:"session_id,omitempty"`

	// ActorID identifies the actor.
	ActorID string `json:"actor_id,omitempty"`

	// TurnID identifies this specific turn.
	TurnID string `json:"turn_id,omitempty"`

	// MaxTokens limits the response length.
	MaxTokens int `json:"max_tokens,omitempty"`

	// Temperature controls randomness (0-1).
	Temperature float64 `json:"temperature,omitempty"`
}

// EngineOutput is the result of an engine turn.
type EngineOutput struct {
	// AssistantText is the final response text.
	AssistantText string `json:"assistant_text,omitempty"`

	// ToolCalls are the tool calls made during the turn.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolResults are the results from tool execution.
	ToolResults []ToolResult `json:"tool_results,omitempty"`

	// StopReason indicates why the turn ended.
	StopReason StopReason `json:"stop_reason"`

	// Tokens is the token consumption.
	Tokens TokenUsage `json:"tokens,omitempty"`

	// Error contains error details if StopReason is Error.
	Error string `json:"error,omitempty"`
}

// StopReason indicates why an engine turn ended.
type StopReason string

const (
	// StopReasonEndTurn means the LLM returned a final response.
	StopReasonEndTurn StopReason = "end_turn"

	// StopReasonCancelled means the context was cancelled (preemption).
	StopReasonCancelled StopReason = "cancelled"

	// StopReasonError means an error occurred.
	StopReasonError StopReason = "error"

	// StopReasonMaxTokens means the token limit was reached.
	StopReasonMaxTokens StopReason = "max_tokens"

	// StopReasonMaxIterations means the iteration limit was reached.
	StopReasonMaxIterations StopReason = "max_iterations"
)

// Message is a conversation message.
type Message struct {
	// Role is the message role: "user", "assistant", "system", "tool".
	Role string `json:"role"`

	// Content is the message text content.
	Content string `json:"content,omitempty"`

	// ToolCalls are tool calls in assistant messages.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolCallID is the tool call ID for tool result messages.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// Name is the tool name for tool result messages.
	Name string `json:"name,omitempty"`
}

// Role constants.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ToolDef is a tool definition.
type ToolDef struct {
	// Name is the canonical tool name (e.g., "fs.read_file").
	Name string `json:"name"`

	// Description describes what the tool does.
	Description string `json:"description"`

	// Parameters is the JSON Schema for the tool's parameters.
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall represents a tool call from the LLM.
type ToolCall struct {
	// ID is the unique identifier for this tool call.
	ID string `json:"id"`

	// Name is the canonical tool name.
	Name string `json:"name"`

	// Arguments is the JSON arguments for the tool.
	Arguments json.RawMessage `json:"arguments"`
}

// ToolResult is the result of a tool execution.
type ToolResult struct {
	// ToolCallID is the ID of the tool call this result is for.
	ToolCallID string `json:"tool_call_id"`

	// Content is the result content.
	Content string `json:"content"`

	// IsError indicates if the result is an error.
	IsError bool `json:"is_error,omitempty"`

	// CASDigest is the CAS digest if the content was offloaded.
	CASDigest string `json:"cas_digest,omitempty"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	// InputTokens is the number of input tokens.
	InputTokens int `json:"input_tokens,omitempty"`

	// OutputTokens is the number of output tokens.
	OutputTokens int `json:"output_tokens,omitempty"`

	// TotalTokens is the total tokens (input + output).
	TotalTokens int `json:"total_tokens,omitempty"`
}

// Add updates the token usage with additional counts.
func (t *TokenUsage) Add(input, output int) {
	t.InputTokens += input
	t.OutputTokens += output
	t.TotalTokens = t.InputTokens + t.OutputTokens
}

// NewUserMessage creates a user message.
func NewUserMessage(content string) Message {
	return Message{Role: RoleUser, Content: content}
}

// NewAssistantMessage creates an assistant message.
func NewAssistantMessage(content string) Message {
	return Message{Role: RoleAssistant, Content: content}
}

// NewAssistantToolCallMessage creates an assistant message with tool calls.
func NewAssistantToolCallMessage(toolCalls []ToolCall) Message {
	return Message{Role: RoleAssistant, ToolCalls: toolCalls}
}

// NewSystemMessage creates a system message.
func NewSystemMessage(content string) Message {
	return Message{Role: RoleSystem, Content: content}
}

// NewToolResultMessage creates a tool result message.
func NewToolResultMessage(toolCallID, name, content string, isError bool) Message {
	return Message{
		Role:       RoleTool,
		ToolCallID: toolCallID,
		Name:       name,
		Content:    content,
	}
}

// EngineOption configures an engine.
type EngineOption func(*EngineConfig)

// EngineConfig holds engine configuration.
type EngineConfig struct {
	// MaxIterations limits the tool call loop.
	MaxIterations int

	// MaxResultBytes limits tool result size before CAS offload.
	MaxResultBytes int

	// Temperature for LLM calls.
	Temperature float64

	// MaxTokens for LLM response.
	MaxTokens int
}

// DefaultEngineConfig returns sensible defaults.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		MaxIterations:  50,
		MaxResultBytes: 100 * 1024, // 100KB
		Temperature:    0.0,
		MaxTokens:      8192,
	}
}

// WithMaxIterations sets the maximum tool call iterations.
func WithMaxIterations(n int) EngineOption {
	return func(c *EngineConfig) {
		c.MaxIterations = n
	}
}

// WithMaxResultBytes sets the maximum tool result size before CAS offload.
func WithMaxResultBytes(n int) EngineOption {
	return func(c *EngineConfig) {
		c.MaxResultBytes = n
	}
}

// WithTemperature sets the LLM temperature.
func WithTemperature(t float64) EngineOption {
	return func(c *EngineConfig) {
		c.Temperature = t
	}
}

// WithMaxTokens sets the maximum response tokens.
func WithMaxTokens(n int) EngineOption {
	return func(c *EngineConfig) {
		c.MaxTokens = n
	}
}
