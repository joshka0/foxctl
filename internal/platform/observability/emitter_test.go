package observability

import (
	"context"
	"testing"
)

type recordingSink struct {
	events []*Event
}

func (s *recordingSink) Emit(_ context.Context, event *Event) {
	s.events = append(s.events, event)
}

func (s *recordingSink) EmitSync(_ context.Context, event *Event) error {
	s.events = append(s.events, event)
	return nil
}

func TestSinkReceivesEventsAndCanBeRestored(t *testing.T) {
	first := &recordingSink{}
	restoreFirst := SetSink(first)
	t.Cleanup(restoreFirst)

	Emit(context.Background(), NewEvent("first.async").Build())
	if err := EmitSync(context.Background(), NewEvent("first.sync").Build()); err != nil {
		t.Fatalf("EmitSync returned error: %v", err)
	}
	if got := len(first.events); got != 2 {
		t.Fatalf("expected first sink to receive 2 events, got %d", got)
	}

	second := &recordingSink{}
	restoreSecond := SetSink(second)
	Emit(context.Background(), NewEvent("second.async").Build())
	if got := len(first.events); got != 2 {
		t.Fatalf("expected first sink not to receive event while replaced, got %d", got)
	}
	if got := len(second.events); got != 1 {
		t.Fatalf("expected second sink to receive 1 event, got %d", got)
	}

	restoreSecond()
	Emit(context.Background(), NewEvent("first.restored").Build())
	if got := len(first.events); got != 3 {
		t.Fatalf("expected first sink to receive restored event, got %d", got)
	}
}

func TestTraceContextRoundTrip(t *testing.T) {
	ctx := WithTraceID(context.Background(), "trace-123")
	ctx = WithSpanID(ctx, "span-456")

	if got := TraceIDFromContext(ctx); got != "trace-123" {
		t.Fatalf("TraceIDFromContext() = %q, want trace-123", got)
	}
	if got := SpanIDFromContext(ctx); got != "span-456" {
		t.Fatalf("SpanIDFromContext() = %q, want span-456", got)
	}

	event := NewEvent("trace.test").EnrichFromContext(ctx).Build()
	if event.TraceID != "trace-123" {
		t.Fatalf("event trace ID = %q, want trace-123", event.TraceID)
	}
}
