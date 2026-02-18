package run

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrTurnNotFound indicates a referenced turn record does not exist.
	ErrTurnNotFound = errors.New("v2 run: turn not found")
)

// MessageRef references textual content captured in a turn.
type MessageRef struct {
	ID   string `json:"id"`
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}

// TurnRecord is the persisted root lineage record for one completed turn.
type TurnRecord struct {
	ID            string            `json:"id"`
	SessionID     string            `json:"session_id,omitempty"`
	TurnIndex     int               `json:"turn_index,omitempty"`
	TraceID       string            `json:"trace_id,omitempty"`
	RootSpanID    string            `json:"root_span_id,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
	CausationID   string            `json:"causation_id,omitempty"`
	RequestID     string            `json:"request_id,omitempty"`
	ActorID       string            `json:"actor_id,omitempty"`
	Command       string            `json:"command,omitempty"`
	Prompt        string            `json:"prompt,omitempty"`
	Iterations    []IterationRecord `json:"iterations,omitempty"`
	FinalOutput   MessageRef        `json:"final_output,omitempty"`
	CreatedAt     time.Time         `json:"created_at,omitempty"`
	UpdatedAt     time.Time         `json:"updated_at,omitempty"`
}

// Clone returns a deep copy safe for cross-goroutine reads.
func (r TurnRecord) Clone() TurnRecord {
	out := r
	if len(r.Iterations) > 0 {
		out.Iterations = make([]IterationRecord, len(r.Iterations))
		for i := range r.Iterations {
			out.Iterations[i] = r.Iterations[i].Clone()
		}
	}
	return out
}

// TurnRecorder persists canonical turn lineage records.
type TurnRecorder interface {
	SaveTurn(ctx context.Context, turn TurnRecord) error
}

// TurnReader loads canonical turn lineage records.
type TurnReader interface {
	GetTurn(ctx context.Context, turnID string) (TurnRecord, error)
}
