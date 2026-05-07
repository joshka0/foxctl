package run

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	// ErrEffectNotFound indicates a model/tool effect journal row does not exist.
	ErrEffectNotFound = errors.New("v2 run: effect not found")
	// ErrEffectConflict indicates a journal key was reused for different effect data.
	ErrEffectConflict = errors.New("v2 run: effect conflict")
	// ErrEffectIncomplete indicates a replay reached a recorded intent without a terminal result.
	ErrEffectIncomplete = errors.New("v2 run: effect incomplete")
)

// EffectKey identifies one model or tool effect inside a turn request.
type EffectKey struct {
	RunID          string `json:"run_id"`
	RequestID      string `json:"request_id"`
	TurnID         string `json:"turn_id"`
	IterationIndex int    `json:"iteration_index"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
}

// ModelEffectStatus is the durable lifecycle for one model call.
type ModelEffectStatus string

const (
	ModelEffectIntent    ModelEffectStatus = "intent"
	ModelEffectSucceeded ModelEffectStatus = "succeeded"
	ModelEffectFailed    ModelEffectStatus = "failed"
)

// IsTerminal reports whether a model effect has a durable result.
func (s ModelEffectStatus) IsTerminal() bool {
	switch s {
	case ModelEffectSucceeded, ModelEffectFailed:
		return true
	default:
		return false
	}
}

// ModelEffectRecord stores a model-call intent and, once known, its terminal result.
type ModelEffectRecord struct {
	EffectKey
	InputJSON    json.RawMessage   `json:"input_json,omitempty"`
	Status       ModelEffectStatus `json:"status"`
	ResponseJSON json.RawMessage   `json:"response_json,omitempty"`
	ErrorMessage string            `json:"error_message,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ToolEffectStatus is the durable lifecycle for one tool call.
type ToolEffectStatus string

const (
	ToolEffectIntent    ToolEffectStatus = "intent"
	ToolEffectSucceeded ToolEffectStatus = "succeeded"
	ToolEffectFailed    ToolEffectStatus = "failed"
)

// IsTerminal reports whether a tool effect has a durable result.
func (s ToolEffectStatus) IsTerminal() bool {
	switch s {
	case ToolEffectSucceeded, ToolEffectFailed:
		return true
	default:
		return false
	}
}

// ToolEffectRecord stores a tool intent and, once known, its terminal result.
type ToolEffectRecord struct {
	EffectKey
	ToolName     string           `json:"tool_name"`
	ArgsJSON     json.RawMessage  `json:"args_json,omitempty"`
	ReplayPolicy string           `json:"replay_policy,omitempty"`
	Status       ToolEffectStatus `json:"status"`
	ResultJSON   json.RawMessage  `json:"result_json,omitempty"`
	ErrorMessage string           `json:"error_message,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}

// EffectJournal persists replayable model and tool effects for durable runner execution.
type EffectJournal interface {
	GetModelEffect(ctx context.Context, key EffectKey) (ModelEffectRecord, error)
	BeginModelEffect(ctx context.Context, record ModelEffectRecord) (ModelEffectRecord, error)
	CompleteModelEffect(ctx context.Context, record ModelEffectRecord) (ModelEffectRecord, error)
	SaveModelEffect(ctx context.Context, record ModelEffectRecord) (ModelEffectRecord, error)
	GetToolEffect(ctx context.Context, key EffectKey) (ToolEffectRecord, error)
	BeginToolEffect(ctx context.Context, record ToolEffectRecord) (ToolEffectRecord, error)
	CompleteToolEffect(ctx context.Context, record ToolEffectRecord) (ToolEffectRecord, error)
}
