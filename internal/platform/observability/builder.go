package observability

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

var version = "dev"

// EventBuilder provides a fluent API for constructing Events.
type EventBuilder struct {
	event *Event
}

// NewEvent creates a new EventBuilder for the specified operation.
func NewEvent(operation string) *EventBuilder {
	now := time.Now()
	return &EventBuilder{
		event: &Event{
			Timestamp: now.UTC(),
			SpanID:    ulid.Make().String(),
			Operation: operation,
			Status:    StatusOK,
			Data: map[string]any{
				DataKeyService: "foxctl",
				DataKeyVersion: version,
			},
		},
	}
}

func (b *EventBuilder) WithTraceID(id string) *EventBuilder {
	b.event.TraceID = id
	return b
}

func (b *EventBuilder) WithParentID(id string) *EventBuilder {
	b.event.ParentID = id
	return b
}

func (b *EventBuilder) WithComponent(component string) *EventBuilder {
	return b.WithData(DataKeyComponent, component)
}

func (b *EventBuilder) WithCommand(cmd string) *EventBuilder {
	b.event.Name = cmd
	return b.WithData(DataKeyCommand, cmd)
}

func (b *EventBuilder) WithSubtype(subtype string) *EventBuilder {
	return b.WithData(DataKeySubtype, subtype)
}

func (b *EventBuilder) WithSession(sessionID, agentID string) *EventBuilder {
	if sessionID != "" {
		b.WithData(DataKeySessionID, sessionID)
	}
	if agentID != "" {
		b.WithData(DataKeyAgentID, agentID)
	}
	return b
}

func (b *EventBuilder) WithWorkspace(id string) *EventBuilder {
	return b.WithData(DataKeyWorkspaceID, id)
}

func (b *EventBuilder) WithJobID(id string) *EventBuilder {
	return b.WithData(DataKeyJobID, id)
}

func (b *EventBuilder) WithData(key string, value any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any)
	}
	b.event.Data[key] = value
	return b
}

func (b *EventBuilder) WithDataMap(data map[string]any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any, len(data))
	}
	for k, v := range data {
		b.event.Data[k] = v
	}
	return b
}

func (b *EventBuilder) WithInputArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("input_artifact", digest)
	}
	return b
}

func (b *EventBuilder) WithResultArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("result_artifact", digest)
	}
	return b
}

func (b *EventBuilder) WithStderrArtifact(digest string) *EventBuilder {
	if digest != "" {
		return b.WithData("stderr_artifact", digest)
	}
	return b
}

func (b *EventBuilder) WithTrajectory(trajectoryID string) *EventBuilder {
	if trajectoryID != "" {
		return b.WithData("trajectory_id", trajectoryID)
	}
	return b
}

func (b *EventBuilder) WithMailbox(mailboxMsgID string) *EventBuilder {
	if mailboxMsgID != "" {
		return b.WithData("mailbox_message_id", mailboxMsgID)
	}
	return b
}

func (b *EventBuilder) EnrichFromEnv() *EventBuilder {
	if EventDataString(b.event, DataKeySessionID) == "" {
		if id := os.Getenv("FOXCTL_SESSION_ID"); id != "" {
			b.WithData(DataKeySessionID, id)
		} else if id := os.Getenv("CLAUDE_SESSION_ID"); id != "" {
			b.WithData(DataKeySessionID, id)
		}
	}
	if EventDataString(b.event, DataKeyAgentID) == "" {
		if id := os.Getenv("FOXCTL_AGENT_ID"); id != "" {
			b.WithData(DataKeyAgentID, id)
		}
	}
	return b
}

func (b *EventBuilder) EnrichFromContext(ctx context.Context) *EventBuilder {
	if b.event.TraceID == "" {
		if traceID := TraceIDFromContext(ctx); traceID != "" {
			b.event.TraceID = traceID
		}
	}
	return b
}

func (b *EventBuilder) Success(duration time.Duration) *Event {
	b.finalize(StatusOK, duration)
	return b.event
}

func (b *EventBuilder) Error(err error, duration time.Duration) *Event {
	b.finalize(StatusError, duration)
	if err != nil {
		b.event.ErrorMessage = err.Error()
		b.event.ErrorType = classifyError(err)
	}
	return b.event
}

func (b *EventBuilder) ErrorWithDetails(errType, errCode, errMsg string, retriable bool, duration time.Duration) *Event {
	b.finalize(StatusError, duration)
	b.event.ErrorType = errType
	b.event.ErrorCode = errCode
	b.event.ErrorMessage = errMsg
	b.WithData(DataKeyRetriable, retriable)
	return b.event
}

func (b *EventBuilder) Canceled(duration time.Duration) *Event {
	b.finalize(StatusCanceled, duration)
	return b.event
}

func (b *EventBuilder) Build() *Event {
	if b.event.TraceID == "" {
		b.event.TraceID = ulid.Make().String()
	}
	return b.event
}

func (b *EventBuilder) finalize(status Status, duration time.Duration) {
	b.event.Status = status
	b.event.Duration = duration
	if b.event.TraceID == "" {
		b.event.TraceID = ulid.Make().String()
	}
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
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
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
