// Package foxcular provides foxcular event observability primitives for Go services.
//
// Foxcular implements the foxcular event model: one rich event per operation captures
// identity, timing, outcome, and domain-specific data. Events are immutable
// snapshots delivered through configured drains.
package foxcular

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Status represents the outcome of an operation.
type Status string

const (
	// StatusOK indicates the operation completed successfully.
	StatusOK Status = "ok"
	// StatusError indicates the operation failed.
	StatusError Status = "error"
	// StatusCanceled indicates the operation was canceled.
	StatusCanceled Status = "canceled"
)

// Event is an immutable foxcular event snapshot. Once created via an EventBuilder
// or returned from a Span, its fields must not be mutated. All Data map values
// are deep-copied at construction time so that later mutations to caller-held
// maps/slices are not reflected in the event.
type Event struct {
	// Timestamp is the event time (UTC).
	Timestamp time.Time `json:"ts"`
	// TraceID correlates all events in a logical operation.
	TraceID string `json:"trace_id"`
	// SpanID is the unique identifier for this specific event/span.
	SpanID string `json:"span_id"`
	// ParentID is the parent span ID for nested operations, empty for roots.
	ParentID string `json:"parent_id,omitempty"`

	// Operation is the name of the operation (e.g., "http.request", "job.run").
	Operation string `json:"operation"`
	// Name is an optional short display name or command name.
	Name string `json:"name,omitempty"`

	// Status is the outcome: StatusOK, StatusError, or StatusCanceled.
	Status Status `json:"status"`
	// Duration is the wall-clock duration of the operation.
	Duration time.Duration `json:"-"`

	// Message is a human-readable message.
	Message string `json:"message,omitempty"`

	// ErrorType categorises the error (e.g., "timeout", "validation").
	ErrorType string `json:"error_type,omitempty"`
	// ErrorCode is a machine-readable error code.
	ErrorCode string `json:"error_code,omitempty"`
	// ErrorMessage is the error message (redacted per policy).
	ErrorMessage string `json:"error_message,omitempty"`

	// Forced indicates the event bypasses sampling and is always delivered.
	Forced bool `json:"forced,omitempty"`

	// Data holds domain-specific key-value pairs. Values must be
	// JSON-serializable. Nested maps and slices are deep-copied.
	Data map[string]any `json:"data,omitempty"`
}

// MarshalJSON implements custom JSON marshaling for Event with duration in ms.
func (e Event) MarshalJSON() ([]byte, error) {
	type Alias Event
	aux := &struct {
		*Alias
		DurationMS int64 `json:"duration_ms"`
	}{
		Alias:      (*Alias)(&e),
		DurationMS: e.Duration.Milliseconds(),
	}
	return json.Marshal(aux)
}

// UnmarshalJSON implements custom JSON unmarshaling for Event with duration from ms.
func (e *Event) UnmarshalJSON(data []byte) error {
	type Alias Event
	aux := &struct {
		*Alias
		DurationMS int64 `json:"duration_ms"`
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	e.Duration = time.Duration(aux.DurationMS) * time.Millisecond
	return nil
}

// Clone returns a deep copy of the event, including a deep copy of the Data map.
func (e *Event) Clone() *Event {
	if e == nil {
		return nil
	}
	clone := *e
	clone.Data = deepCopyMap(e.Data)
	return &clone
}

// String returns a human-readable summary of the event.
func (e *Event) String() string {
	return fmt.Sprintf("Event{op=%s status=%s trace=%s span=%s dur=%s}",
		e.Operation, e.Status, e.TraceID, e.SpanID, e.Duration)
}

// IsError reports whether the event status is error.
func (e *Event) IsError() bool {
	return e.Status == StatusError
}

// IsOK reports whether the event status is ok.
func (e *Event) IsOK() bool {
	return e.Status == StatusOK
}

// deepCopyMap returns a deep copy of a map[string]any, recursively copying
// nested maps and slices.
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = deepCopyValue(v)
	}
	return cp
}

// deepCopySlice returns a deep copy of a []any, recursively.
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	cp := make([]any, len(s))
	for i, v := range s {
		cp[i] = deepCopyValue(v)
	}
	return cp
}

// deepCopyValue returns a deep copy of a value, recursing into maps and slices.
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		return deepCopySlice(val)
	default:
		// Primitive types (string, bool, numbers, nil, time.Time, etc.)
		// are value types or immutable, so return as-is.
		return v
	}
}

// classifyError categorises an error by inspecting its message.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msgLower := strings.ToLower(msg)
	switch {
	case containsAny(msgLower, "context canceled", "context deadline exceeded"):
		return "timeout"
	case containsAny(msgLower, "permission denied", "access denied", "unauthorized"):
		return "permission"
	case containsAny(msgLower, "not found", "no such file", "does not exist"):
		return "not_found"
	case containsAny(msgLower, "invalid", "malformed", "parse error"):
		return "validation"
	case containsAny(msgLower, "connection refused", "network", "dial"):
		return "network"
	default:
		return "internal"
	}
}

// containsAny reports whether s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
