package foxcular

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// Client is the primary API for emitting foxcular events. It delivers immutable
// event snapshots to configured drains. Events are redacted before delivery
// and optionally filtered by a sampler.
type Client struct {
	drain   Drain
	clock   Clock
	ids     IDGenerator
	sampler Sampler
	redact  *RedactionPolicy
	closed  atomic.Bool
}

// ClientOption configures a Client.
type ClientOption func(*clientOpts)

type clientOpts struct {
	clock   Clock
	ids     IDGenerator
	drain   Drain
	sampler Sampler
	redact  *RedactionPolicy
}

// WithClock sets a custom clock (for testing).
func WithClock(c Clock) ClientOption {
	return func(o *clientOpts) { o.clock = c }
}

// WithIDGenerator sets a custom ID generator (for testing).
func WithIDGenerator(ids IDGenerator) ClientOption {
	return func(o *clientOpts) { o.ids = ids }
}

// WithSampler sets the event sampler. When set, events that are not forced are
// subject to sampling before delivery. Forced events always bypass sampling.
func WithSampler(s Sampler) ClientOption {
	return func(o *clientOpts) { o.sampler = s }
}

// WithRedaction sets the redaction policy. When set, all events are redacted
// before delivery to any drain.
func WithRedaction(p *RedactionPolicy) ClientOption {
	return func(o *clientOpts) { o.redact = p }
}

// NewClient creates a new Client that delivers events to the given drain.
func NewClient(drain Drain, opts ...ClientOption) *Client {
	o := clientOpts{
		clock:   RealClock{},
		ids:     ULIDGenerator{},
		drain:   drain,
		sampler: AlwaysSample{},
		redact:  NewRedactionPolicy(),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &Client{
		drain:   o.drain,
		clock:   o.clock,
		ids:     o.ids,
		sampler: o.sampler,
		redact:  o.redact,
	}
}

// Emit creates and delivers an event for the given operation using a builder.
// It returns a builder so the caller can add fields and finalize with
// Success/Error/Canceled.
func (c *Client) Emit(operation string) *ClientBuilder {
	return &ClientBuilder{
		client:  c,
		builder: NewEventBuilder(c.clock, c.ids, operation),
	}
}

// EmitEvent delivers a pre-built event snapshot to the drain.
// Returns nil for best-effort delivery. Use EmitSync for error-returning delivery.
// Sampling is applied unless the event is forced. Redaction is always applied.
func (c *Client) EmitEvent(ctx context.Context, event *Event) {
	if c.closed.Load() {
		return
	}
	redacted := c.prepare(event)
	if redacted == nil {
		return // dropped by sampler
	}
	_ = c.drain.Send(ctx, redacted)
}

// EmitEventSync delivers a pre-built event snapshot to the drain and returns
// any drain error. Sampling is applied unless the event is forced. Redaction
// is always applied.
func (c *Client) EmitEventSync(ctx context.Context, event *Event) error {
	if c.closed.Load() {
		return errors.New("foxcular: client is closed")
	}
	redacted := c.prepare(event)
	if redacted == nil {
		return nil // dropped by sampler
	}
	return c.drain.Send(ctx, redacted)
}

// prepare applies sampling and redaction to an event. Returns nil if the event
// should be dropped by the sampler.
func (c *Client) prepare(event *Event) *Event {
	if event == nil {
		return nil
	}

	// Check forced from data flags if Forced field is not already set.
	if !event.Forced {
		event = event.Clone()
		event.Forced = forcedFromData(event.Data)
	}

	// Sampling: forced events bypass sampling.
	if !event.Forced && c.sampler != nil {
		if c.sampler.ShouldSample(event) == Drop {
			return nil
		}
	}

	// Redaction always runs before drain delivery.
	if c.redact != nil && c.redact.Enabled() {
		return c.redact.RedactEvent(event)
	}
	return event
}

// Flush waits for pending events to be delivered.
func (c *Client) Flush(ctx context.Context) error {
	if c.closed.Load() {
		return nil
	}
	return c.drain.Flush(ctx)
}

// Close flushes remaining events and releases resources.
// Safe to call multiple times.
func (c *Client) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil // already closed
	}
	return c.drain.Close()
}

// StartSpan creates a span that tracks an operation lifecycle.
// The returned Span's End method emits a single event.
func (c *Client) StartSpan(ctx context.Context, operation string, opts ...SpanOption) (context.Context, *Span) {
	return newSpan(ctx, c, operation, opts...)
}

// ClientBuilder wraps an EventBuilder and auto-delivers on finalize.
type ClientBuilder struct {
	client  *Client
	builder *EventBuilder
}

func (cb *ClientBuilder) WithTraceID(id string) *ClientBuilder {
	cb.builder.WithTraceID(id)
	return cb
}

func (cb *ClientBuilder) WithParentID(id string) *ClientBuilder {
	cb.builder.WithParentID(id)
	return cb
}

func (cb *ClientBuilder) WithName(name string) *ClientBuilder {
	cb.builder.WithName(name)
	return cb
}

func (cb *ClientBuilder) WithMessage(msg string) *ClientBuilder {
	cb.builder.WithMessage(msg)
	return cb
}

func (cb *ClientBuilder) WithData(key string, value any) *ClientBuilder {
	cb.builder.WithData(key, value)
	return cb
}

func (cb *ClientBuilder) WithDataMap(data map[string]any) *ClientBuilder {
	cb.builder.WithDataMap(data)
	return cb
}

// InheritContext reads trace/span IDs from ctx and applies them.
func (cb *ClientBuilder) InheritContext(ctx context.Context) *ClientBuilder {
	if tid := TraceIDFromContext(ctx); tid != "" && cb.builder.event.TraceID == "" {
		cb.builder.WithTraceID(tid)
	}
	if sid := SpanIDFromContext(ctx); sid != "" && cb.builder.event.ParentID == "" {
		cb.builder.WithParentID(sid)
	}
	return cb
}

// Forced marks the event as forced, bypassing any sampling.
func (cb *ClientBuilder) Forced() *ClientBuilder {
	cb.builder.event.Forced = true
	return cb
}

// Success finalizes and delivers a successful event.
func (cb *ClientBuilder) Success(ctx context.Context, duration time.Duration) error {
	event := cb.builder.Success(duration)
	return cb.client.EmitEventSync(ctx, event)
}

// Error finalizes and delivers a failed event.
func (cb *ClientBuilder) Error(ctx context.Context, err error, duration time.Duration) error {
	event := cb.builder.Error(err, duration)
	return cb.client.EmitEventSync(ctx, event)
}

// Canceled finalizes and delivers a canceled event.
func (cb *ClientBuilder) Canceled(ctx context.Context, duration time.Duration) error {
	event := cb.builder.Canceled(duration)
	return cb.client.EmitEventSync(ctx, event)
}

// Build returns the event without delivering it.
func (cb *ClientBuilder) Build() *Event {
	return cb.builder.Build()
}
