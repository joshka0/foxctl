package observability

import (
	"context"
	"os"
	"time"

	"github.com/oklog/ulid/v2"
)

// version is set at build time via -ldflags.
var version = "dev"

// EventBuilder provides a fluent API for constructing WideEvents.
// It accumulates context throughout an operation's lifecycle and
// produces a single comprehensive event on completion.
//
// Usage:
//
//	event := observability.NewEvent("skill.run").
//	    WithTraceID(traceID).
//	    WithCommand("code/snippet_extract").
//	    WithWorkspace(wsID).
//	    EnrichFromEnv()
//
//	// ... perform operation ...
//
//	if err != nil {
//	    observability.Emit(ctx, event.Error(err, time.Since(start)))
//	} else {
//	    observability.Emit(ctx, event.WithData("files", count).Success(time.Since(start)))
//	}
type EventBuilder struct {
	event     *WideEvent
	startTime time.Time
	persist   *persistConfig // Optional persistence configuration
}

// NewEvent creates a new EventBuilder for the specified operation.
// It initializes the event with a new SpanID and current timestamp.
//
// Index:
// - Purpose: Initialize a WideEvent builder with base metadata and timing
// - Flow: allocate event → set identifiers → initialize data map → start timer
// - SideEffects: reads clock for timestamps
// - Related: EventBuilder.Success, EventBuilder.Error, EventBuilder.Build
// - Keywords: wide_event, span_id, trace_id, event_builder, observability
func NewEvent(operation string) *EventBuilder {
	return &EventBuilder{
		event: &WideEvent{
			Ts:        time.Now().UTC(),
			SpanID:    ulid.Make().String(),
			Service:   "agentctl",
			Version:   version,
			Operation: operation,
			Data:      make(map[string]any),
		},
		startTime: time.Now(),
	}
}

// WithTraceID sets the trace ID for correlating events across an operation.
// If empty, the builder will generate one on Build/Success/Error.
func (b *EventBuilder) WithTraceID(id string) *EventBuilder {
	b.event.TraceID = id
	return b
}

// WithParentID sets the parent span ID for nested operations.
func (b *EventBuilder) WithParentID(id string) *EventBuilder {
	b.event.ParentID = id
	return b
}

// WithComponent sets the component that generated the event.
func (b *EventBuilder) WithComponent(component string) *EventBuilder {
	b.event.Component = component
	return b
}

// WithCommand sets the command/skill/hook name.
func (b *EventBuilder) WithCommand(cmd string) *EventBuilder {
	b.event.Command = cmd
	return b
}

// WithSubtype sets additional operation classification.
func (b *EventBuilder) WithSubtype(subtype string) *EventBuilder {
	b.event.Subtype = subtype
	return b
}

// WithSession sets session and agent IDs for business context.
func (b *EventBuilder) WithSession(sessionID, agentID string) *EventBuilder {
	b.event.SessionID = sessionID
	b.event.AgentID = agentID
	return b
}

// WithWorkspace sets the logical workspace ID.
func (b *EventBuilder) WithWorkspace(id string) *EventBuilder {
	b.event.WorkspaceID = id
	return b
}

// WithJobID sets the background job ID.
func (b *EventBuilder) WithJobID(id string) *EventBuilder {
	b.event.JobID = id
	return b
}

// WithData adds a key-value pair to the domain-specific data.
// Values should be simple types (strings, numbers, booleans, maps, slices).
// Never include raw content, secrets, or PII.
func (b *EventBuilder) WithData(key string, value any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any)
	}
	b.event.Data[key] = value
	return b
}

// WithDataMap merges a map of key-value pairs into domain-specific data.
func (b *EventBuilder) WithDataMap(data map[string]any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any)
	}
	for k, v := range data {
		b.event.Data[k] = v
	}
	return b
}

// WithInputArtifact sets the CAS digest of the input payload.
// This enables full request replay from observability events.
func (b *EventBuilder) WithInputArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("input_artifact", digest)
	}
	return b
}

// WithResultArtifact sets the CAS digest of the output/result payload.
// This enables full response replay from observability events.
func (b *EventBuilder) WithResultArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("result_artifact", digest)
	}
	return b
}

// WithStderrArtifact sets the CAS digest of captured stderr.
// This preserves debugging info in CAS for later retrieval.
func (b *EventBuilder) WithStderrArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("stderr_artifact", digest)
	}
	return b
}

// WithTrajectory sets the trajectory ID for turn correlation.
func (b *EventBuilder) WithTrajectory(trajectoryID string) *EventBuilder {
	if trajectoryID != "" {
		return b.WithData("trajectory_id", trajectoryID)
	}
	return b
}

// WithMailbox sets the mailbox message ID for actor coordination.
func (b *EventBuilder) WithMailbox(mailboxMsgID string) *EventBuilder {
	if mailboxMsgID != "" {
		return b.WithData("mailbox_message_id", mailboxMsgID)
	}
	return b
}

// EnrichFromEnv populates business context from environment variables.
// This pulls session, agent, and workspace IDs from standard env vars.
func (b *EventBuilder) EnrichFromEnv() *EventBuilder {
	if b.event.SessionID == "" {
		// Check agentctl-specific first, then fallbacks
		if id := os.Getenv("AGENTCTL_SESSION_ID"); id != "" {
			b.event.SessionID = id
		} else if id := os.Getenv("CLAUDE_SESSION_ID"); id != "" {
			b.event.SessionID = id
		}
	}
	if b.event.AgentID == "" {
		b.event.AgentID = os.Getenv("AGENTCTL_AGENT_ID")
	}
	return b
}

// EnrichFromContext populates trace ID and other context from ctx.
func (b *EventBuilder) EnrichFromContext(ctx context.Context) *EventBuilder {
	if b.event.TraceID == "" {
		if traceID := TraceIDFromContext(ctx); traceID != "" {
			b.event.TraceID = traceID
		}
	}
	return b
}

// Success finalizes the event as successful and returns the WideEvent.
// If TraceID is empty, it generates one.
func (b *EventBuilder) Success(duration time.Duration) *WideEvent {
	b.finalize(StatusOK, duration)
	return b.event
}

// Error finalizes the event as failed and returns the WideEvent.
// It extracts error type and message from the error.
func (b *EventBuilder) Error(err error, duration time.Duration) *WideEvent {
	b.finalize(StatusError, duration)
	if err != nil {
		b.event.ErrorMessage = err.Error()
		// Extract error type from common patterns
		b.event.ErrorType = classifyError(err)
	}
	return b.event
}

// ErrorWithDetails finalizes the event with explicit error details.
func (b *EventBuilder) ErrorWithDetails(errType, errCode, errMsg string, retriable bool, duration time.Duration) *WideEvent {
	b.finalize(StatusError, duration)
	b.event.ErrorType = errType
	b.event.ErrorCode = errCode
	b.event.ErrorMessage = errMsg
	b.event.Retriable = &retriable
	return b.event
}

// Canceled finalizes the event as canceled and returns the WideEvent.
func (b *EventBuilder) Canceled(duration time.Duration) *WideEvent {
	b.finalize(StatusCanceled, duration)
	return b.event
}

// Build returns the current WideEvent without finalizing status.
// Use this for custom status handling.
func (b *EventBuilder) Build() *WideEvent {
	if b.event.TraceID == "" {
		b.event.TraceID = ulid.Make().String()
	}
	return b.event
}

func (b *EventBuilder) finalize(status Status, duration time.Duration) {
	b.event.Status = status
	b.event.DurationMS = duration.Milliseconds()
	if b.event.TraceID == "" {
		b.event.TraceID = ulid.Make().String()
	}
}

// classifyError attempts to categorize an error by examining common patterns.
func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// Check for common error patterns
	switch {
	case contains(msg, "context canceled", "context deadline exceeded"):
		return "timeout"
	case contains(msg, "permission denied", "access denied", "unauthorized"):
		return "permission"
	case contains(msg, "not found", "no such file", "does not exist"):
		return "not_found"
	case contains(msg, "invalid", "malformed", "parse error"):
		return "validation"
	case contains(msg, "connection refused", "network", "dial"):
		return "network"
	default:
		return "internal"
	}
}

func contains(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
