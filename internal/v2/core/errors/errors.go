package errors

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/events"
)

// ErrorKind classifies v2 runtime failures.
type ErrorKind string

const (
	ErrNotFound        ErrorKind = "not_found"
	ErrPolicyViolation ErrorKind = "policy_violation"
	ErrTimeout         ErrorKind = "timeout"
	ErrToolFailed      ErrorKind = "tool_failed"
	ErrStageFailed     ErrorKind = "stage_failed"
	ErrInternal        ErrorKind = "internal"
	ErrValidation      ErrorKind = "validation"
	ErrDependency      ErrorKind = "dependency"
)

// EventContext identifies event-stream metadata attached to generated error events.
type EventContext struct {
	StreamID      string
	StreamType    events.StreamType
	Command       string
	CorrelationID string
	CausationID   string
	ActorID       string
	RequestID     string
	Now           func() time.Time
}

// V2Error is the canonical v2 error contract used by service/runtime layers.
type V2Error struct {
	Kind      ErrorKind
	Message   string
	Cause     error
	Fatal     bool
	Retryable bool
	Details   map[string]any
}

// Error returns the human-readable error message.
func (e *V2Error) Error() string {
	if e == nil {
		return string(ErrInternal)
	}

	base := strings.TrimSpace(e.Message)
	if base == "" {
		base = string(e.Kind)
	}
	if base == "" {
		base = string(ErrInternal)
	}

	if e.Cause == nil {
		return base
	}
	return fmt.Sprintf("%s: %v", base, e.Cause)
}

// IsFatal reports whether this error should terminate the turn.
func (e *V2Error) IsFatal() bool {
	if e == nil {
		return true
	}
	return e.Fatal
}

// HTTPStatus maps v2 error kinds to deterministic HTTP status codes.
func (e *V2Error) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}

	switch e.Kind {
	case ErrValidation:
		return http.StatusBadRequest
	case ErrPolicyViolation:
		return http.StatusForbidden
	case ErrNotFound:
		return http.StatusNotFound
	case ErrTimeout:
		return http.StatusRequestTimeout
	case ErrToolFailed:
		return http.StatusBadGateway
	case ErrStageFailed:
		if e.IsFatal() {
			return http.StatusInternalServerError
		}
		// Non-fatal stage failures surface as degraded output rather than terminal HTTP errors.
		return http.StatusOK
	case ErrInternal, ErrDependency:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// EnvelopeCode maps v2 errors to stable v1-compatible envelope error codes.
func (e *V2Error) EnvelopeCode() string {
	if e == nil {
		return "ERUNTIME"
	}

	switch e.Kind {
	case ErrValidation:
		return "EARG"
	case ErrPolicyViolation:
		return "EPOLICY"
	case ErrNotFound:
		return "ENOTFOUND"
	case ErrTimeout:
		return "ETIMEOUT"
	default:
		return "ERUNTIME"
	}
}

// ToEvent renders this error into a typed v2 event payload.
func (e *V2Error) ToEvent(ctx EventContext, evtType events.EventType) events.Event {
	if e == nil {
		e = &V2Error{Kind: ErrInternal, Message: string(ErrInternal), Fatal: true}
	}
	if evtType == "" {
		evtType = events.EventRunFailed
	}
	if ctx.StreamType == "" {
		ctx.StreamType = events.StreamTypeRun
	}
	now := ctx.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	payload := events.ErrorPayload{
		Kind:         string(e.Kind),
		Message:      e.Error(),
		Fatal:        e.IsFatal(),
		Retryable:    e.Retryable,
		Details:      cloneDetails(e.Details),
		HTTPStatus:   e.HTTPStatus(),
		EnvelopeCode: e.EnvelopeCode(),
	}
	if e.Cause != nil {
		payload.Cause = e.Cause.Error()
	}

	rawPayload, err := events.MarshalPayload(payload)
	if err != nil {
		rawPayload = events.MustMarshalPayload(events.ErrorPayload{
			Kind:         string(ErrInternal),
			Message:      "failed to marshal error payload",
			Fatal:        true,
			HTTPStatus:   http.StatusInternalServerError,
			EnvelopeCode: "ERUNTIME",
		})
	}

	return events.Event{
		ID:            buildEventID(ctx, evtType, e.Kind),
		StreamID:      ctx.StreamID,
		StreamType:    ctx.StreamType,
		EventType:     evtType,
		OccurredAt:    now().UTC(),
		CorrelationID: ctx.CorrelationID,
		CausationID:   ctx.CausationID,
		ActorID:       ctx.ActorID,
		RequestID:     ctx.RequestID,
		Command:       ctx.Command,
		Payload:       rawPayload,
	}
}

func buildEventID(ctx EventContext, evtType events.EventType, kind ErrorKind) string {
	seed := strings.TrimSpace(ctx.RequestID)
	if seed == "" {
		seed = strings.TrimSpace(ctx.CorrelationID)
	}
	if seed == "" {
		seed = strings.TrimSpace(ctx.StreamID)
	}
	if seed == "" {
		seed = "event"
	}
	return fmt.Sprintf("%s:%s:%s", seed, evtType, kind)
}

func cloneDetails(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
