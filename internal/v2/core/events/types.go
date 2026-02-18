package events

import (
	"context"
	"encoding/json"
	"time"
)

// StreamType identifies the logical stream an event belongs to.
type StreamType string

const (
	StreamTypeRun   StreamType = "run"
	StreamTypeAgent StreamType = "agent"
	StreamTypeTurn  StreamType = "turn"
)

// EventType identifies the semantic event name.
type EventType string

const (
	EventRunStarted     EventType = "run.started"
	EventRunCompleted   EventType = "run.completed"
	EventRunFailed      EventType = "run.failed"
	EventToolInvoked    EventType = "tool.invoked"
	EventToolResponded  EventType = "tool.responded"
	EventTurnRecorded   EventType = "turn.recorded"
	EventStageFailed    EventType = "stage.failed"
	EventArtifactFailed EventType = "artifact.failed"
)

// Event is the canonical append-only v2 runtime event envelope.
type Event struct {
	ID            string          `json:"id"`
	StreamID      string          `json:"stream_id"`
	StreamType    StreamType      `json:"stream_type"`
	StreamVersion int64           `json:"stream_version"`
	Sequence      int64           `json:"sequence"`
	EventType     EventType       `json:"event_type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	ActorID       string          `json:"actor_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	Command       string          `json:"command,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// Clone returns a deep copy of the event payload bytes.
func (e Event) Clone() Event {
	out := e
	if len(e.Payload) > 0 {
		out.Payload = append(json.RawMessage(nil), e.Payload...)
	}
	return out
}

// Appender stores events in append-only order.
type Appender interface {
	Append(ctx context.Context, event Event) error
}
