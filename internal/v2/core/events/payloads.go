package events

import "encoding/json"

// ErrorPayload captures structured error details for failure events.
type ErrorPayload struct {
	Kind         string         `json:"kind"`
	Message      string         `json:"message"`
	Cause        string         `json:"cause,omitempty"`
	Fatal        bool           `json:"fatal,omitempty"`
	Retryable    bool           `json:"retryable,omitempty"`
	HTTPStatus   int            `json:"http_status,omitempty"`
	EnvelopeCode string         `json:"envelope_code,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// RunStartedPayload captures run-start context.
type RunStartedPayload struct {
	Mode   string `json:"mode,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// RunCompletedPayload captures run-completion context.
type RunCompletedPayload struct {
	Summary string `json:"summary,omitempty"`
}

// ToolInvokedPayload captures tool invocation context.
type ToolInvokedPayload struct {
	Name           string `json:"name"`
	IterationIndex int    `json:"iteration_index,omitempty"`
}

// ToolRespondedPayload captures tool result context.
type ToolRespondedPayload struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// TurnRecordedPayload captures turn persistence context.
type TurnRecordedPayload struct {
	TurnID     string `json:"turn_id"`
	Iterations int    `json:"iterations,omitempty"`
	ToolCalls  int    `json:"tool_calls,omitempty"`
}

// MarshalPayload encodes payload values as raw JSON bytes for event storage.
func MarshalPayload(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// MustMarshalPayload encodes payload values and panics on error.
func MustMarshalPayload(v any) json.RawMessage {
	payload, err := MarshalPayload(v)
	if err != nil {
		panic(err)
	}
	return payload
}
