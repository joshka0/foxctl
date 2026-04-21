package run

import "encoding/json"

// ToolCall is a model-requested tool invocation inside an iteration.
type ToolCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// TurnInput is the canonical request-scoped input passed into the v2 runner.
type TurnInput struct {
	RunID         string `json:"run_id"`
	TurnID        string `json:"turn_id,omitempty"`
	Command       string `json:"command,omitempty"`
	Mode          string `json:"mode,omitempty"`
	Prompt        string `json:"prompt,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
}

// StageFailure records non-fatal stage degradation details.
type StageFailure struct {
	Stage     string `json:"stage"`
	Kind      string `json:"kind,omitempty"`
	Message   string `json:"message"`
	Fatal     bool   `json:"fatal,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

// TurnOutput captures deterministic runner results, including degraded state.
type TurnOutput struct {
	TurnID        string         `json:"turn_id"`
	Summary       string         `json:"summary,omitempty"`
	Iterations    int            `json:"iterations"`
	ToolCalls     int            `json:"tool_calls"`
	Degraded      bool           `json:"degraded,omitempty"`
	StageFailures []StageFailure `json:"stage_failures,omitempty"`
}
