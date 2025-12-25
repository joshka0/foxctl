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
}

// AskData represents the data payload for an agent.ask message.
type AskData struct {
	AskID     string         `json:"ask_id"`
	Kind      string         `json:"kind"` // context|secret|approval|toolhint|other
	Question  string         `json:"question"`
	NeedsByMS int64          `json:"needs_by_ms,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
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
