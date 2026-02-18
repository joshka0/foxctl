package ask

import "time"

// Request is the canonical input for v2 ask orchestration.
type Request struct {
	RequestID string `json:"request_id,omitempty"`
	AskID     string `json:"ask_id,omitempty"`

	AgentID   string `json:"agent_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	Kind           string        `json:"kind,omitempty"`
	Question       string        `json:"question"`
	ConversationID string        `json:"conversation_id,omitempty"`
	CallerNS       string        `json:"caller_ns,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`
}

// Message is the normalized ask message that gets dispatched to transport.
type Message struct {
	AskID          string `json:"ask_id"`
	RequestID      string `json:"request_id,omitempty"`
	Kind           string `json:"kind"`
	Question       string `json:"question"`
	ConversationID string `json:"conversation_id,omitempty"`
	FromNS         string `json:"from_ns"`
	ToNS           string `json:"to_ns"`
	TTLMS          int64  `json:"ttl_ms,omitempty"`
}

// Response is the canonical ask service output.
type Response struct {
	AskID     string `json:"ask_id"`
	MessageID string `json:"message_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
}
