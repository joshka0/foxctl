package codexjsonl

import "encoding/json"

// Message represents a top-level JSONL record.
type Message struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// ResponseItem is the payload for type == "response_item".
// It captures assistant/user messages and tool calls/results.
type ResponseItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   []ContentBlock  `json:"content,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Status    string          `json:"status,omitempty"`
	Summary   string          `json:"summary,omitempty"`
}

// ContentBlock represents a message content block.
// Codex commonly uses input_text (user) and output_text (assistant).
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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
