package foxcular

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// IDGenerator abstracts unique ID generation for testability.
type IDGenerator interface {
	NewID() string
}

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// RealClock returns the current time using time.Now.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// ULIDGenerator generates ULIDs.
type ULIDGenerator struct{}

func (ULIDGenerator) NewID() string { return ulid.Make().String() }

// EventBuilder provides a fluent API for constructing immutable Events.
//
// Usage:
//
//	event := foxcular.NewEventBuilder(clock, ids, "http.request").
//	    WithName("GET /api/users").
//	    WithData("user_count", 42).
//	    Success(100 * time.Millisecond)
type EventBuilder struct {
	clock    Clock
	ids      IDGenerator
	event    Event
	start    time.Time
	finished bool
}

// NewEventBuilder creates an EventBuilder for the given operation.
func NewEventBuilder(clock Clock, ids IDGenerator, operation string) *EventBuilder {
	now := clock.Now()
	return &EventBuilder{
		clock: clock,
		ids:   ids,
		start: now,
		event: Event{
			Timestamp: now,
			SpanID:    ids.NewID(),
			Operation: operation,
			Status:    StatusOK,
			Data:      make(map[string]any),
		},
	}
}

// WithTraceID sets the trace ID for correlation.
func (b *EventBuilder) WithTraceID(id string) *EventBuilder {
	b.event.TraceID = id
	return b
}

// WithParentID sets the parent span ID.
func (b *EventBuilder) WithParentID(id string) *EventBuilder {
	b.event.ParentID = id
	return b
}

// WithName sets a short display name or command.
func (b *EventBuilder) WithName(name string) *EventBuilder {
	b.event.Name = name
	return b
}

// WithMessage sets a human-readable message.
func (b *EventBuilder) WithMessage(msg string) *EventBuilder {
	b.event.Message = msg
	return b
}

// WithData adds a key-value pair to the event data.
func (b *EventBuilder) WithData(key string, value any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any)
	}
	b.event.Data[key] = value
	return b
}

// WithDataMap merges a map of key-value pairs into event data.
func (b *EventBuilder) WithDataMap(data map[string]any) *EventBuilder {
	if b.event.Data == nil {
		b.event.Data = make(map[string]any, len(data))
	}
	for k, v := range data {
		b.event.Data[k] = v
	}
	return b
}

// Success finalizes the event as successful with the given duration
// and returns an immutable Event snapshot.
func (b *EventBuilder) Success(duration time.Duration) *Event {
	return b.finalize(StatusOK, duration, nil)
}

// Error finalizes the event as failed with the given duration and error.
func (b *EventBuilder) Error(err error, duration time.Duration) *Event {
	return b.finalize(StatusError, duration, err)
}

// Canceled finalizes the event as canceled.
func (b *EventBuilder) Canceled(duration time.Duration) *Event {
	return b.finalize(StatusCanceled, duration, nil)
}

// Build returns the current event as an immutable snapshot without
// finalizing status. Useful for custom status handling.
func (b *EventBuilder) Build() *Event {
	if b.event.TraceID == "" {
		b.event.TraceID = b.ids.NewID()
	}
	return b.event.Clone()
}

func (b *EventBuilder) finalize(status Status, duration time.Duration, err error) *Event {
	if b.finished {
		return b.event.Clone()
	}
	b.finished = true
	b.event.Status = status
	b.event.Duration = duration
	if b.event.TraceID == "" {
		b.event.TraceID = b.ids.NewID()
	}
	if err != nil {
		b.event.ErrorMessage = err.Error()
		b.event.ErrorType = classifyError(err)
	}
	// Deep-copy the data to guarantee immutability.
	snapshot := b.event.Clone()
	return snapshot
}

// EnsureTraceID ensures the context has a trace ID, generating one if needed.
func EnsureTraceID(ctx context.Context, ids IDGenerator) (context.Context, string) {
	if tid := TraceIDFromContext(ctx); tid != "" {
		return ctx, tid
	}
	tid := ids.NewID()
	return WithTraceID(ctx, tid), tid
}
