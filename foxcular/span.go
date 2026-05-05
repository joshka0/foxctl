package foxcular

import (
	"context"
	"errors"
	"time"
)

// Span tracks an operation lifecycle and emits one event on End.
type Span struct {
	client  *Client
	builder *EventBuilder
	ctx     context.Context
	start   time.Time
	ended   bool
}

// SpanOption configures a span.
type SpanOption func(*spanOpts)

type spanOpts struct {
	traceID  string
	parentID string
	name     string
	data     map[string]any
}

// WithSpanTraceID sets a specific trace ID for the span.
func WithSpanTraceID(id string) SpanOption {
	return func(o *spanOpts) { o.traceID = id }
}

// WithSpanParentID sets the parent span ID for nested operations.
func WithSpanParentID(id string) SpanOption {
	return func(o *spanOpts) { o.parentID = id }
}

// WithSpanName sets a short name for the span.
func WithSpanName(name string) SpanOption {
	return func(o *spanOpts) { o.name = name }
}

// WithSpanData adds key-value data to the span event.
func WithSpanData(key string, value any) SpanOption {
	return func(o *spanOpts) {
		if o.data == nil {
			o.data = make(map[string]any)
		}
		o.data[key] = value
	}
}

// WithSpanDataMap merges a map of data into the span event.
func WithSpanDataMap(m map[string]any) SpanOption {
	return func(o *spanOpts) {
		if o.data == nil {
			o.data = make(map[string]any, len(m))
		}
		for k, v := range m {
			o.data[k] = v
		}
	}
}

// newSpan creates a span and returns a derived context containing the span.
func newSpan(ctx context.Context, c *Client, operation string, opts ...SpanOption) (context.Context, *Span) {
	var o spanOpts
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	// Handle trace ID: explicit option > context > generate new.
	if o.traceID != "" {
		ctx = WithTraceID(ctx, o.traceID)
	}
	ctx, traceID := EnsureTraceID(ctx, c.ids)

	b := NewEventBuilder(c.clock, c.ids, operation)
	b.WithTraceID(traceID)

	// Inherit parent span ID from context if not explicitly set.
	if o.parentID != "" {
		b.WithParentID(o.parentID)
	} else if parentSpanID := SpanIDFromContext(ctx); parentSpanID != "" {
		b.WithParentID(parentSpanID)
	}
	if o.name != "" {
		b.WithName(o.name)
	}
	if len(o.data) > 0 {
		b.WithDataMap(o.data)
	}

	// Set the span ID in context so children can find it.
	ctx = WithSpanID(ctx, b.event.SpanID)

	span := &Span{
		client:  c,
		builder: b,
		ctx:     ctx,
		start:   c.clock.Now(),
	}

	// Store active span in context.
	ctx = WithActiveSpan(ctx, span)

	return ctx, span
}

// AddData adds key-value data to the span before it ends.
func (s *Span) AddData(key string, value any) {
	if s.ended {
		return
	}
	s.builder.WithData(key, value)
}

// AddDataMap merges a map of data into the span.
func (s *Span) AddDataMap(m map[string]any) {
	if s.ended {
		return
	}
	s.builder.WithDataMap(m)
}

// TraceID returns the span's trace ID.
func (s *Span) TraceID() string {
	return s.builder.event.TraceID
}

// SpanID returns the span's span ID.
func (s *Span) SpanID() string {
	return s.builder.event.SpanID
}

// Context returns the derived context containing this span.
func (s *Span) Context() context.Context {
	return s.ctx
}

// End finalizes the span and emits the event. If err is nil, status is OK.
// If err wraps context.Canceled, status is Canceled. Otherwise, status is Error.
// Calling End more than once is a no-op.
func (s *Span) End(err error) error {
	if s.ended {
		return nil
	}
	s.ended = true

	dur := s.client.clock.Now().Sub(s.start)
	var event *Event
	switch {
	case err == nil:
		event = s.builder.Success(dur)
	case errors.Is(err, context.Canceled):
		event = s.builder.Canceled(dur)
	default:
		event = s.builder.Error(err, dur)
	}

	return s.client.EmitEventSync(s.ctx, event)
}
