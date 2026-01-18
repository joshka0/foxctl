// Package claudejsonl provides types and parsing for Claude Code's JSONL session format.
//
// Claude Code stores conversation history in JSONL files located at:
// ~/.claude/projects/<workspace-hash>/<session-id>.jsonl
//
// Each line contains a JSON message with nested content structures.
package claudejsonl

import "encoding/json"

// Message represents a message in the Claude Code JSONL format.
// This is the top-level structure for each line in the JSONL file.
type Message struct {
	Type             string           `json:"type"`
	Subtype          string           `json:"subtype,omitempty"`
	Role             string           `json:"role,omitempty"`
	Message          json.RawMessage  `json:"message,omitempty"`
	Content          json.RawMessage  `json:"content,omitempty"`
	ToolUse          *ToolUseInfo     `json:"tool_use,omitempty"`
	ToolResult       *ToolResultInfo  `json:"tool_result,omitempty"`
	Timestamp        string           `json:"timestamp,omitempty"`
	CompactMetadata  *CompactMetadata `json:"compactMetadata,omitempty"`
	IsCompactSummary bool             `json:"isCompactSummary,omitempty"` // True for compact continuation messages
}

// MessagePayload represents the nested message content in JSONL.
// Found inside Message.Message field.
type MessagePayload struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// NestedMessage represents the nested message structure in Claude JSONL.
// The .message field contains {role, content} where content is either
// a string (user messages) or an array of content blocks (assistant messages).
type NestedMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock represents a content block in assistant messages.
// Types include: "text", "tool_use", "thinking"
type ContentBlock struct {
	Type     string `json:"type"` // "text", "tool_use", "thinking"
	Text     string `json:"text,omitempty"`
	Thinking string `json:"thinking,omitempty"`
	Name     string `json:"name,omitempty"` // tool name for tool_use
	ID       string `json:"id,omitempty"`   // tool_use_id for tool_use
}

// UserContentBlock represents a content block in user messages.
// User messages can contain text blocks and tool_result blocks.
type UserContentBlock struct {
	Type      string `json:"type"` // "text", "tool_result"
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	// Content field intentionally omitted - contains file contents, command output, etc.
}

// ToolUseInfo represents tool use in a message.
type ToolUseInfo struct {
	Name  string          `json:"name"`
	ID    string          `json:"id,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// ToolResultInfo represents a tool result.
type ToolResultInfo struct {
	ToolUseID string `json:"tool_use_id,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Content   string `json:"content,omitempty"`
}

// CompactMetadata contains metadata about a compaction event.
type CompactMetadata struct {
	Trigger   string `json:"trigger"`   // "auto" or "manual"
	PreTokens int    `json:"preTokens"` // tokens before compaction
}

// ChunkType represents the classified type of a chunk.
type ChunkType string

const (
	ChunkTypeUserRequest       ChunkType = "user_request"
	ChunkTypeAssistantResponse ChunkType = "assistant_response"
	ChunkTypeToolUse           ChunkType = "tool_use"
	ChunkTypeToolOutput        ChunkType = "tool_output"
	ChunkTypeError             ChunkType = "error"
	ChunkTypeCompactBoundary   ChunkType = "compact_boundary"
	ChunkTypeOther             ChunkType = "other"
)
