package run

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrTurnRequestConflict indicates a request id was reused for a different turn.
	ErrTurnRequestConflict = errors.New("v2 run: turn request conflict")
	// ErrTurnRequestNotFound indicates a turn request record does not exist.
	ErrTurnRequestNotFound = errors.New("v2 run: turn request not found")
)

// TurnRequestStatus is the idempotency lifecycle for one run-scoped request.
type TurnRequestStatus string

const (
	TurnRequestRunning   TurnRequestStatus = "running"
	TurnRequestSucceeded TurnRequestStatus = "succeeded"
	TurnRequestFailed    TurnRequestStatus = "failed"
	TurnRequestCanceled  TurnRequestStatus = "canceled"
)

const (
	// Compatibility aliases for callers that prefer explicit type names.
	TurnRequestStatusRunning   = TurnRequestRunning
	TurnRequestStatusSucceeded = TurnRequestSucceeded
	TurnRequestStatusFailed    = TurnRequestFailed
	TurnRequestStatusCancelled = TurnRequestCanceled
)

// IsTerminal reports whether status is a completed turn request state.
func (s TurnRequestStatus) IsTerminal() bool {
	switch s {
	case TurnRequestSucceeded, TurnRequestFailed, TurnRequestCanceled:
		return true
	default:
		return false
	}
}

// TurnRequestRecord is the persisted registry row for one run/request pair.
type TurnRequestRecord struct {
	RunID       string            `json:"run_id"`
	RequestID   string            `json:"request_id"`
	TurnID      string            `json:"turn_id"`
	Status      TurnRequestStatus `json:"status"`
	OutputJSON  json.RawMessage   `json:"output_json,omitempty"`
	ErrorJSON   json.RawMessage   `json:"error_json,omitempty"`
	StartedAt   time.Time         `json:"started_at,omitempty"`
	CompletedAt time.Time         `json:"completed_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

// Clone returns a defensive copy of JSON-bearing request state.
func (r TurnRequestRecord) Clone() TurnRequestRecord {
	out := r
	out.OutputJSON = append(json.RawMessage(nil), r.OutputJSON...)
	out.ErrorJSON = append(json.RawMessage(nil), r.ErrorJSON...)
	return out
}

// TurnRequestRegistry stores run-scoped request idempotency records.
type TurnRequestRegistry interface {
	BeginTurnRequest(ctx context.Context, record TurnRequestRecord) (TurnRequestRecord, bool, error)
	CompleteTurnRequest(ctx context.Context, record TurnRequestRecord) (TurnRequestRecord, error)
	GetTurnRequest(ctx context.Context, runID, requestID string) (TurnRequestRecord, error)
}

// TurnRequestToucher refreshes a running request without changing terminal rows.
type TurnRequestToucher interface {
	TouchTurnRequest(ctx context.Context, runID, requestID, turnID string, now time.Time) (TurnRequestRecord, bool, error)
}

// StaleTurnRequestRecoverer atomically reclaims an orphaned running request.
type StaleTurnRequestRecoverer interface {
	RecoverStaleTurnRequest(ctx context.Context, record TurnRequestRecord, staleBefore time.Time) (TurnRequestRecord, bool, error)
}
