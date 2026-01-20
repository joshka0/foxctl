package agent

import (
	"encoding/json"
)

// MessageType defines the type of inter-agent message.
type MessageType string

const (
	// MessageTypeAsk represents a request expecting a response.
	MessageTypeAsk MessageType = "agent.ask"
	// MessageTypeReply represents a response to an ask.
	MessageTypeReply MessageType = "agent.reply"
	// MessageTypeCmd represents a command (fire-and-forget).
	MessageTypeCmd MessageType = "agent.cmd"
	// MessageTypeEvent represents a notification.
	MessageTypeEvent MessageType = "agent.event"

	// Console message types for interactive actor consoles.
	// MessageTypeConsoleAsk is a user request from console.
	MessageTypeConsoleAsk MessageType = "console.ask"
	// MessageTypeConsoleReply is the final response to console.
	MessageTypeConsoleReply MessageType = "console.reply"
	// MessageTypeConsoleEvent is a streaming update to console.
	MessageTypeConsoleEvent MessageType = "console.event"
	// MessageTypeConsoleCmd is a control command (e.g., cancel).
	MessageTypeConsoleCmd MessageType = "console.cmd"
)

// Message represents a mailbox message for inter-agent communication.
type Message struct {
	ID        string            `json:"id"`
	FromNS    string            `json:"from_ns"`
	ToNS      string            `json:"to_ns"`
	Type      MessageType       `json:"type"`
	TTLMS     int64             `json:"ttl_ms"`
	Headers   map[string]string `json:"headers,omitempty"`
	Payload   json.RawMessage   `json:"payload"` // Envelope JSON
	VisibleAt int64             `json:"visible_at"`
	Attempt   int               `json:"attempt"`
	Timestamp int64             `json:"ts"`

	// Session context for lineage-scoped reads
	SessionID string `json:"session_id,omitempty"` // Originating session
	Workspace string `json:"workspace,omitempty"`  // Workspace path
	AgentID   string `json:"agent_id,omitempty"`   // Sending agent's ID
}

// AskData represents the data payload for an agent.ask message.
type AskData struct {
	AskID          string         `json:"ask_id"`
	Kind           string         `json:"kind"` // context|secret|approval|toolhint|other
	Question       string         `json:"question"`
	NeedsByMS      int64          `json:"needs_by_ms,omitempty"`
	Context        map[string]any `json:"context,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"` // For memory continuity across messages
}

// ReplyData represents the data payload for an agent.reply message.
type ReplyData struct {
	AskID  string         `json:"ask_id"`
	Answer map[string]any `json:"answer"`
}

// CmdData represents the data payload for an agent.cmd message.
type CmdData struct {
	CmdID  string         `json:"cmd_id"`
	Action string         `json:"action"`
	Skill  string         `json:"skill,omitempty"`
	Args   map[string]any `json:"args,omitempty"`
}

// EventData represents the data payload for an agent.event message.
type EventData struct {
	EventID   string         `json:"event_id"`
	Kind      string         `json:"kind"` // heartbeat|liveness-failed|etc
	JobCount  int            `json:"job_count,omitempty"`
	CacheHits int            `json:"cache_hits,omitempty"`
	Custom    map[string]any `json:"custom,omitempty"`
}

// ConsoleAskData represents user input from console.
type ConsoleAskData struct {
	AskID     string         `json:"ask_id"`
	Prompt    string         `json:"prompt"`
	Context   map[string]any `json:"context,omitempty"`
	ConsoleID string         `json:"console_id,omitempty"`
}

// ConsoleReplyData represents final response to console.
type ConsoleReplyData struct {
	AskID    string         `json:"ask_id"`
	Response string         `json:"response"`
	Status   string         `json:"status"` // ok, error, cancelled
	Metrics  map[string]any `json:"metrics,omitempty"`
}

// ConsoleEventData represents streaming update during execution.
type ConsoleEventData struct {
	AskID     string `json:"ask_id"`
	Kind      string `json:"kind"` // thought, tool_call, tool_result, progress
	Content   string `json:"content"`
	Seq       int    `json:"seq"`
	Iteration int    `json:"iteration,omitempty"`
	ToolName  string `json:"tool_name,omitempty"`
}

// ConsoleCmdData represents control commands.
type ConsoleCmdData struct {
	CmdID  string `json:"cmd_id"`
	Action string `json:"action"` // cancel, pause, resume
	AskID  string `json:"ask_id,omitempty"`
}
