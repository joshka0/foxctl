// Package atcp contains draft Go types for Agent Terminal Coordination Protocol v0.1.
package atcp

import "time"

type Envelope struct {
	Version        string      `json:"v"`
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Timestamp      time.Time   `json:"ts"`
	Source         string      `json:"source,omitempty"`
	Target         string      `json:"target,omitempty"`
	Seq            uint64      `json:"seq,omitempty"`
	CorrelationID  string      `json:"correlation_id,omitempty"`
	IdempotencyKey string      `json:"idempotency_key,omitempty"`
	Body           interface{} `json:"body"`
}

type TerminalKey struct {
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers,omitempty"`
	Repeat    int      `json:"repeat,omitempty"`
}

type TerminalText struct {
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type TerminalSubmit struct {
	Text      string           `json:"text"`
	SubmitKey string           `json:"submit_key,omitempty"` // default: Enter
	Mode      string           `json:"mode,omitempty"`       // typed, paste, paced, literal
	Expect    *ExpectCondition `json:"expect,omitempty"`
	LeaseID   string           `json:"lease_id,omitempty"`
}

type TerminalPaste struct {
	Text        string `json:"text"`
	SubmitAfter bool   `json:"submit_after,omitempty"`
	Bracketed   string `json:"bracketed,omitempty"` // auto, force, off
	LeaseID     string `json:"lease_id,omitempty"`
}

type ExpectCondition struct {
	Kind      string `json:"kind"`             // prompt-or-output, screen-regex, output-regex
	Source    string `json:"source,omitempty"` // screen, output
	Regex     string `json:"regex,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type LeaseAcquire struct {
	Scope   string `json:"scope"`
	Owner   string `json:"owner"`
	TTLMS   int    `json:"ttl_ms"`
	Preempt bool   `json:"preempt,omitempty"`
}

type MessageSend struct {
	MessageID        string          `json:"message_id"`
	CorrelationID    string          `json:"correlation_id,omitempty"`
	ReplyToMessageID string          `json:"reply_to_message_id,omitempty"`
	Topic            string          `json:"topic,omitempty"`
	Priority         string          `json:"priority,omitempty"`
	Content          []ContentBlock  `json:"content"`
	Delivery         *DeliveryPolicy `json:"delivery,omitempty"`
	Receipt          *MessageReceipt `json:"receipt,omitempty"`
	AwaitActivityMS  int             `json:"await_activity_ms,omitempty"`
	AwaitReadyMS     int             `json:"await_ready_ms,omitempty"`
	TerminalPolicy   string          `json:"terminal_policy,omitempty"`
	PolicyTimeoutMS  int             `json:"policy_timeout_ms,omitempty"`
	InterruptKey     string          `json:"interrupt_key,omitempty"`
}

type MessageReceipt struct {
	MessageID        string `json:"message_id"`
	RoomID           string `json:"room_id"`
	Source           string `json:"source,omitempty"`
	CorrelationID    string `json:"correlation_id,omitempty"`
	ReplyToMessageID string `json:"reply_to_message_id,omitempty"`
	ReplyPrefix      string `json:"reply_prefix"`
}

type MessageActivity struct {
	OutputChanged            bool    `json:"output_changed"`
	Completed                bool    `json:"completed"`
	Ready                    bool    `json:"ready"`
	FirstOutputMS            int     `json:"first_output_ms,omitempty"`
	CompletedMS              int     `json:"completed_ms,omitempty"`
	AwaitActivityTimedOut    bool    `json:"await_activity_timed_out,omitempty"`
	AwaitReadyTimedOut       bool    `json:"await_ready_timed_out,omitempty"`
	BaselineSeq              uint64  `json:"baseline_seq"`
	CurrentSeq               uint64  `json:"current_seq"`
	SeqDelta                 uint64  `json:"seq_delta"`
	BaselineOutputBytesTotal int64   `json:"baseline_output_bytes_total"`
	OutputBytesTotal         int64   `json:"output_bytes_total"`
	OutputBytesDelta         int64   `json:"output_bytes_delta"`
	OutputRateBPS            float64 `json:"output_rate_bps"`
}

type TerminalActivity struct {
	SessionID             string  `json:"session_id"`
	Working               bool    `json:"working"`
	OutputChanged         bool    `json:"output_changed"`
	SinceSeq              uint64  `json:"since_seq"`
	CurrentSeq            uint64  `json:"current_seq"`
	SeqDelta              uint64  `json:"seq_delta"`
	SinceOutputBytesTotal int64   `json:"since_output_bytes_total"`
	OutputBytesTotal      int64   `json:"output_bytes_total"`
	OutputBytesDelta      int64   `json:"output_bytes_delta"`
	OutputRateBPS         float64 `json:"output_rate_bps"`
	IdleForMS             int     `json:"idle_for_ms"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type DeliveryPolicy struct {
	Prefer         []string `json:"prefer,omitempty"`          // inbox, native, terminal, overlay
	TerminalPolicy string   `json:"terminal_policy,omitempty"` // safe-prompt-only, queue, overlay, interrupt
	RequiresAck    bool     `json:"requires_ack,omitempty"`
}

type TransactionRun struct {
	Name  string            `json:"name,omitempty"`
	Lease *TransactionLease `json:"lease,omitempty"`
	Steps []TransactionStep `json:"steps"`
}

type TransactionLease struct {
	Scope   string `json:"scope"`
	TTLMS   int    `json:"ttl_ms"`
	Preempt bool   `json:"preempt,omitempty"`
}

type TransactionStep struct {
	Op          string           `json:"op"` // wait, submit, key, paste, text
	Text        string           `json:"text,omitempty"`
	Key         string           `json:"key,omitempty"`
	SubmitKey   string           `json:"submit_key,omitempty"`
	Match       *ExpectCondition `json:"match,omitempty"`
	TimeoutMS   int              `json:"timeout_ms,omitempty"`
	Bracketed   string           `json:"bracketed,omitempty"`
	SubmitAfter bool             `json:"submit_after,omitempty"`
}
