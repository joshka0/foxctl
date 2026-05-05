package foxcular_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// traceIDKey is a test-local context key type to avoid SA1029.
type traceIDKey struct{}

// stubClock returns fixed times in sequence.
type stubClock struct {
	times []time.Time
	idx   int
}

func (c *stubClock) Now() time.Time {
	if c.idx < len(c.times) {
		t := c.times[c.idx]
		c.idx++
		return t
	}
	return time.Now().UTC()
}

// stubIDs returns predetermined IDs in sequence.
type stubIDs struct {
	ids []string
	idx int
}

func (s *stubIDs) NewID() string {
	if s.idx < len(s.ids) {
		id := s.ids[s.idx]
		s.idx++
		return id
	}
	return fmt.Sprintf("gen-%d", s.idx)
}

// captureDrain stores all received events.
type captureDrain struct {
	events []*foxcular.Event
	mu     sync.Mutex
}

func (d *captureDrain) Send(_ context.Context, e *foxcular.Event) error {
	d.mu.Lock()
	d.events = append(d.events, e.Clone())
	d.mu.Unlock()
	return nil
}

func (d *captureDrain) Flush(_ context.Context) error { return nil }
func (d *captureDrain) Close() error                  { return nil }

func (d *captureDrain) Events() []*foxcular.Event {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]*foxcular.Event, len(d.events))
	copy(cp, d.events)
	return cp
}

func (d *captureDrain) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.events)
}

// failingDrain always returns an error on Send.
type failingDrain struct {
	err error
}

func (d *failingDrain) Send(_ context.Context, _ *foxcular.Event) error { return d.err }
func (d *failingDrain) Flush(_ context.Context) error                   { return nil }
func (d *failingDrain) Close() error                                    { return nil }

// countingDrain counts Close and Flush calls.
type countingDrain struct {
	sendCount  atomic.Int64
	flushCount atomic.Int64
	closeCount atomic.Int64
	err        error
}

func (d *countingDrain) Send(_ context.Context, _ *foxcular.Event) error {
	d.sendCount.Add(1)
	return d.err
}

func (d *countingDrain) Flush(_ context.Context) error {
	d.flushCount.Add(1)
	return d.err
}

func (d *countingDrain) Close() error {
	d.closeCount.Add(1)
	return nil
}

// blockingDrain blocks on Send until unblocked or context is done.
type blockingDrain struct {
	unblock chan struct{}
}

func (d *blockingDrain) Send(ctx context.Context, _ *foxcular.Event) error {
	select {
	case <-d.unblock:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (d *blockingDrain) Flush(_ context.Context) error { return nil }
func (d *blockingDrain) Close() error                  { return nil }

// newTestClient creates a client with fixed clock/IDs and a capture drain.
func newTestClient() (*foxcular.Client, *captureDrain, *stubClock, *stubIDs) {
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts, ts.Add(50 * time.Millisecond)}}
	ids := &stubIDs{ids: []string{"trace-001", "span-001", "span-002", "span-003", "trace-002", "span-child-001"}}
	drain := &captureDrain{}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)
	return client, drain, clock, ids
}

// ---------------------------------------------------------------------------
// VAL-CORE-001: Event construction preserves foxcular event shape
// ---------------------------------------------------------------------------

func TestEventConstructionPreservesFoxcularEventShape(t *testing.T) {
	client, drain, clock, _ := newTestClient()

	err := client.Emit("http.request").
		WithName("GET /api/users").
		WithData("status_code", 200).
		WithData("user_id", "abc123").
		Success(context.Background(), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	// Verify timestamp
	if !e.Timestamp.Equal(clock.times[0]) {
		t.Errorf("timestamp = %v, want %v", e.Timestamp, clock.times[0])
	}
	// Verify operation
	if e.Operation != "http.request" {
		t.Errorf("operation = %q, want %q", e.Operation, "http.request")
	}
	// Verify name
	if e.Name != "GET /api/users" {
		t.Errorf("name = %q, want %q", e.Name, "GET /api/users")
	}
	// Verify status
	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want %q", e.Status, foxcular.StatusOK)
	}
	// Verify trace/span IDs are set
	if e.TraceID == "" {
		t.Error("trace ID is empty")
	}
	if e.SpanID == "" {
		t.Error("span ID is empty")
	}
	// Verify duration
	if e.Duration != 100*time.Millisecond {
		t.Errorf("duration = %v, want %v", e.Duration, 100*time.Millisecond)
	}
	// Verify data
	if e.Data["status_code"] != 200 {
		t.Errorf("status_code = %v, want 200", e.Data["status_code"])
	}
	if e.Data["user_id"] != "abc123" {
		t.Errorf("user_id = %v, want abc123", e.Data["user_id"])
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-002: Event data accepts heterogeneous values
// ---------------------------------------------------------------------------

func TestEventDataAcceptsHeterogeneousValues(t *testing.T) {
	client, drain, _, _ := newTestClient()

	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	dur := 5 * time.Second

	err := client.Emit("test").
		WithData("string_val", "hello").
		WithData("bool_val", true).
		WithData("int_val", 42).
		WithData("float_val", 3.14).
		WithData("time_val", now).
		WithData("duration_val", dur).
		WithData("nil_val", nil).
		WithData("slice_val", []any{1, "two", true}).
		WithData("map_val", map[string]any{"nested": "value"}).
		WithData("error_val", fmt.Errorf("test error")).
		Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	e := drain.Events()[0]

	// Verify all values are present and not silently dropped
	checks := map[string]any{
		"string_val": "hello",
		"bool_val":   true,
		"int_val":    42,
		"float_val":  3.14,
		"nil_val":    nil,
	}
	for key, want := range checks {
		got := e.Data[key]
		if got != want {
			t.Errorf("data[%q] = %v (%T), want %v (%T)", key, got, got, want, want)
		}
	}

	// Check special types are present
	if _, ok := e.Data["time_val"]; !ok {
		t.Error("time_val was dropped")
	}
	if _, ok := e.Data["duration_val"]; !ok {
		t.Error("duration_val was dropped")
	}
	if _, ok := e.Data["slice_val"]; !ok {
		t.Error("slice_val was dropped")
	}
	if _, ok := e.Data["map_val"]; !ok {
		t.Error("map_val was dropped")
	}
	if _, ok := e.Data["error_val"]; !ok {
		t.Error("error_val was dropped")
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-003: Emitted events are immutable snapshots
// ---------------------------------------------------------------------------

func TestEmittedEventsAreImmutableSnapshots(t *testing.T) {
	client, drain, _, _ := newTestClient()

	// Build with mutable data
	data := map[string]any{"key": "original", "nested": map[string]any{"inner": "value"}}
	err := client.Emit("test").
		WithDataMap(data).
		Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	// Mutate the original maps after emission
	data["key"] = "mutated"
	data["nested"].(map[string]any)["inner"] = "mutated"

	e := drain.Events()[0]

	// Captured event should still have original values
	if e.Data["key"] != "original" {
		t.Errorf("data[key] = %v, want original (snapshot was not immutable)", e.Data["key"])
	}
	nested, ok := e.Data["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested is not a map")
	}
	if nested["inner"] != "value" {
		t.Errorf("nested[inner] = %v, want value (snapshot was not immutable)", nested["inner"])
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-004: Minimal events are valid
// ---------------------------------------------------------------------------

func TestMinimalEventsAreValid(t *testing.T) {
	client, drain, _, _ := newTestClient()

	err := client.Emit("minimal.op").Success(context.Background(), 0)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	e := drain.Events()[0]
	if e.Timestamp.IsZero() {
		t.Error("timestamp should be auto-filled")
	}
	if e.SpanID == "" {
		t.Error("span ID should be auto-generated")
	}
	if e.TraceID == "" {
		t.Error("trace ID should be auto-generated")
	}
	if e.Operation != "minimal.op" {
		t.Errorf("operation = %q, want minimal.op", e.Operation)
	}
	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok", e.Status)
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-005: Error metadata is consistent
// ---------------------------------------------------------------------------

func TestErrorMetadataIsConsistent(t *testing.T) {
	client, drain, _, _ := newTestClient()

	// Test with a sentinel error
	sentinelErr := errors.New("something failed")
	err := client.Emit("test.op").
		WithName("TestOp").
		Error(context.Background(), sentinelErr, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	e := drain.Events()[0]
	if e.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error", e.Status)
	}
	if e.ErrorMessage != "something failed" {
		t.Errorf("error_message = %q, want 'something failed'", e.ErrorMessage)
	}
	if e.ErrorType == "" {
		t.Error("error_type should be populated")
	}
	if e.Duration != 50*time.Millisecond {
		t.Errorf("duration = %v, want 50ms", e.Duration)
	}

	// Test with a custom error type
	customErr := fmt.Errorf("connection refused to host")
	_ = client.Emit("test.op2").Error(context.Background(), customErr, time.Millisecond)
	e2 := drain.Events()[1]
	if e2.ErrorType != "network" {
		t.Errorf("error_type = %q, want network", e2.ErrorType)
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-006: Client emits to configured drain
// ---------------------------------------------------------------------------

func TestClientEmitsToConfiguredDrain(t *testing.T) {
	client, drain, _, _ := newTestClient()

	err := client.Emit("test.op").Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	if drain.Len() != 1 {
		t.Fatalf("expected 1 event in drain, got %d", drain.Len())
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-007: Multiple drains are isolated
// ---------------------------------------------------------------------------

func TestMultipleDrainsAreIsolated(t *testing.T) {
	drain1 := &captureDrain{}
	drain2 := &captureDrain{}
	fanout := foxcular.NewFanoutDrain(drain1, drain2)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(fanout,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)

	err := client.Emit("test.op").
		WithData("shared_key", "sensitive").
		Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	if drain1.Len() != 1 || drain2.Len() != 1 {
		t.Fatalf("expected 1 event in each drain, got %d and %d", drain1.Len(), drain2.Len())
	}

	e1 := drain1.Events()[0]
	e2 := drain2.Events()[0]

	// Both should have the same data
	if e1.Data["shared_key"] != "sensitive" || e2.Data["shared_key"] != "sensitive" {
		t.Error("drains did not receive equivalent data")
	}

	// Mutating one drain's event should not affect the other
	e1.Data["shared_key"] = "mutated"
	if e2.Data["shared_key"] == "mutated" {
		t.Error("mutating one drain's event affected the other drain's snapshot")
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-008: Best-effort drain failure does not crash
// ---------------------------------------------------------------------------

func TestBestEffortDrainFailureDoesNotCrash(t *testing.T) {
	healthy := &captureDrain{}
	failing := &failingDrain{err: errors.New("drain broken")}
	fanout := foxcular.NewFanoutDrain(failing, healthy)

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(fanout,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)

	// EmitEvent is best-effort, should not panic
	client.EmitEvent(context.Background(), &foxcular.Event{
		Timestamp: ts,
		TraceID:   "t1",
		SpanID:    "s1",
		Operation: "test",
		Status:    foxcular.StatusOK,
	})

	// Healthy drain should still receive events
	if healthy.Len() != 1 {
		t.Errorf("healthy drain should have 1 event, got %d", healthy.Len())
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-009: Synchronous emit/flush surfaces errors
// ---------------------------------------------------------------------------

func TestSynchronousEmitSurfacesErrors(t *testing.T) {
	sentinel := errors.New("sentinel drain error")
	drain := &failingDrain{err: sentinel}

	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)

	err := client.Emit("test").Success(context.Background(), time.Millisecond)
	if err == nil {
		t.Fatal("expected error from synchronous emit, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want sentinel", err)
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-010: Flush waits for pending events
// ---------------------------------------------------------------------------

func TestFlushWaitsForPendingEvents(t *testing.T) {
	client, drain, _, _ := newTestClient()

	for i := range 5 {
		err := client.Emit("test.op").
			WithData("i", i).
			Success(context.Background(), time.Millisecond)
		if err != nil {
			t.Fatalf("emit %d failed: %v", i, err)
		}
	}

	if err := client.Flush(context.Background()); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	if drain.Len() != 5 {
		t.Errorf("expected 5 events after flush, got %d", drain.Len())
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-011: Close is safe and idempotent
// ---------------------------------------------------------------------------

func TestCloseIsSafeAndIdempotent(t *testing.T) {
	drain := &countingDrain{}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)

	// Close multiple times
	for range 3 {
		if err := client.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
	}

	if drain.closeCount.Load() != 1 {
		t.Errorf("drain Close called %d times, want 1", drain.closeCount.Load())
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-012: Context cancellation bounds blocking operations
// ---------------------------------------------------------------------------

func TestContextCancellationBoundsBlockingOperations(t *testing.T) {
	drain := &blockingDrain{unblock: make(chan struct{})}
	ts := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	clock := &stubClock{times: []time.Time{ts}}
	ids := &stubIDs{ids: []string{"t1", "s1"}}
	client := foxcular.NewClient(drain,
		foxcular.WithClock(clock),
		foxcular.WithIDGenerator(ids),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := client.EmitEventSync(ctx, &foxcular.Event{
		Timestamp: ts,
		TraceID:   "t1",
		SpanID:    "s1",
		Operation: "test",
		Status:    foxcular.StatusOK,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error from cancelled context")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("operation took %v, should have returned within ~50ms", elapsed)
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-013: StartSpan adds active span to context
// ---------------------------------------------------------------------------

func TestStartSpanAddsActiveSpanToContext(t *testing.T) {
	client, _, _, _ := newTestClient()

	ctx, span := client.StartSpan(context.Background(), "test.op",
		foxcular.WithSpanName("TestOp"),
	)

	retrieved := foxcular.ActiveSpanFromContext(ctx)
	if retrieved == nil {
		t.Fatal("active span not found in context")
	}
	if retrieved.SpanID() != span.SpanID() {
		t.Error("retrieved span does not match created span")
	}
	if span.TraceID() == "" {
		t.Error("trace ID should be set")
	}
	if span.SpanID() == "" {
		t.Error("span ID should be set")
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-014: Span lifecycle emits status and duration
// ---------------------------------------------------------------------------

func TestSpanLifecycleEmitsStatusAndDuration(t *testing.T) {
	client, drain, _, _ := newTestClient()

	ctx, span := client.StartSpan(context.Background(), "test.op")
	span.AddData("items", 42)

	if err := span.End(nil); err != nil {
		t.Fatalf("span end failed: %v", err)
	}

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]

	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok", e.Status)
	}
	if e.Duration <= 0 {
		t.Error("duration should be non-negative")
	}
	if e.Operation != "test.op" {
		t.Errorf("operation = %q, want test.op", e.Operation)
	}
	if e.Data["items"] != 42 {
		t.Errorf("data[items] = %v, want 42", e.Data["items"])
	}

	_ = ctx // context is available for downstream use
}

// ---------------------------------------------------------------------------
// VAL-CORE-015: Ending span with error records failure
// ---------------------------------------------------------------------------

func TestEndingSpanWithErrorRecordsFailure(t *testing.T) {
	client, drain, _, _ := newTestClient()

	_, span := client.StartSpan(context.Background(), "test.op")
	err := span.End(errors.New("operation failed"))
	if err != nil {
		t.Fatalf("span end returned error: %v", err)
	}

	e := drain.Events()[0]
	if e.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error", e.Status)
	}
	if e.ErrorMessage != "operation failed" {
		t.Errorf("error_message = %q, want 'operation failed'", e.ErrorMessage)
	}
	if e.ErrorType == "" {
		t.Error("error_type should be populated")
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-016: Nested spans preserve parent/child correlation
// ---------------------------------------------------------------------------

func TestNestedSpansPreserveParentChildCorrelation(t *testing.T) {
	client, drain, _, ids := newTestClient()
	// IDs: trace-001, span-001 (parent), span-002 (child)
	_ = ids // IDs are consumed by the builder

	ctx, parent := client.StartSpan(context.Background(), "parent.op")

	childCtx, child := client.StartSpan(ctx, "child.op")

	// Child should have same trace ID as parent
	if child.TraceID() != parent.TraceID() {
		t.Errorf("child trace ID %q != parent trace ID %q", child.TraceID(), parent.TraceID())
	}

	// Child should have different span ID
	if child.SpanID() == parent.SpanID() {
		t.Error("child and parent should have different span IDs")
	}

	// End both spans
	_ = child.End(nil)
	_ = parent.End(nil)

	events := drain.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// Find child event (first emitted)
	var childEvent, parentEvent *foxcular.Event
	for _, e := range events {
		if e.Operation == "child.op" {
			childEvent = e
		} else {
			parentEvent = e
		}
	}

	if childEvent == nil || parentEvent == nil {
		t.Fatal("missing child or parent event")
	}

	// Same trace ID
	if childEvent.TraceID != parentEvent.TraceID {
		t.Error("parent and child should share trace ID")
	}

	// Child has parent's span ID as parent_id
	if childEvent.ParentID != parentEvent.SpanID {
		t.Errorf("child parent_id = %q, want parent span_id %q", childEvent.ParentID, parentEvent.SpanID)
	}

	_ = childCtx
}

// ---------------------------------------------------------------------------
// VAL-CORE-017: Events inside spans inherit correlation IDs
// ---------------------------------------------------------------------------

func TestEventsInsideSpansInheritCorrelationIDs(t *testing.T) {
	client, drain, _, _ := newTestClient()

	ctx, span := client.StartSpan(context.Background(), "parent.op")

	// Emit an event within the span context
	err := client.Emit("child.event").
		InheritContext(ctx).
		Success(ctx, time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	events := drain.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.TraceID != span.TraceID() {
		t.Errorf("event trace_id = %q, want span trace_id %q", e.TraceID, span.TraceID())
	}
	if e.ParentID != span.SpanID() {
		t.Errorf("event parent_id = %q, want span span_id %q", e.ParentID, span.SpanID())
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-018: Events outside spans remain valid
// ---------------------------------------------------------------------------

func TestEventsOutsideSpansRemainValid(t *testing.T) {
	client, drain, _, _ := newTestClient()

	// Emit event without any span context
	err := client.Emit("standalone.op").Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit failed: %v", err)
	}

	e := drain.Events()[0]
	if e.TraceID == "" {
		t.Error("trace ID should be auto-generated")
	}
	if e.SpanID == "" {
		t.Error("span ID should be auto-generated")
	}
	if e.ParentID != "" {
		t.Error("parent ID should be empty for standalone event")
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-019: Span End is idempotent
// ---------------------------------------------------------------------------

func TestSpanEndIsIdempotent(t *testing.T) {
	client, drain, _, _ := newTestClient()

	_, span := client.StartSpan(context.Background(), "test.op")

	// End multiple times
	_ = span.End(nil)
	_ = span.End(errors.New("should not emit"))
	_ = span.End(nil)

	// Should only emit 1 event
	if drain.Len() != 1 {
		t.Errorf("expected 1 event after double End, got %d", drain.Len())
	}

	e := drain.Events()[0]
	if e.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok (first End wins)", e.Status)
	}
}

// ---------------------------------------------------------------------------
// VAL-CORE-020: Context propagation survives standard wrapping
// ---------------------------------------------------------------------------

func TestContextPropagationSurvivesStandardWrapping(t *testing.T) {
	client, _, _, _ := newTestClient()

	ctx, span := client.StartSpan(context.Background(), "test.op")
	traceID := span.TraceID()
	spanID := span.SpanID()

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{"WithValue", context.WithValue(ctx, traceIDKey{}, "value")},
		{"WithCancel", func() context.Context {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c
		}()},
		{"WithTimeout", func() context.Context {
			c, cancel := context.WithTimeout(ctx, time.Hour)
			cancel()
			return c
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Trace ID should survive
			if got := foxcular.TraceIDFromContext(tt.ctx); got != traceID {
				t.Errorf("TraceIDFromContext = %q, want %q", got, traceID)
			}
			// Span ID should survive
			if got := foxcular.SpanIDFromContext(tt.ctx); got != spanID {
				t.Errorf("SpanIDFromContext = %q, want %q", got, spanID)
			}
			// Active span should survive
			if got := foxcular.ActiveSpanFromContext(tt.ctx); got == nil || got.SpanID() != spanID {
				t.Error("ActiveSpanFromContext lost the span")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// VAL-REPO-001/002: Repo structure and module path
// ---------------------------------------------------------------------------

func TestModulePathAndEventTypes(t *testing.T) {
	// Verify that the package compiles and types are usable.
	// The go.mod validation is done by the tooling.
	var _ foxcular.Event
	var _ foxcular.Client
	var _ foxcular.Drain
	var _ foxcular.Status
}

// ---------------------------------------------------------------------------
// JSON round-trip for Event
// ---------------------------------------------------------------------------

func TestEventJSONRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	orig := foxcular.Event{
		Timestamp:    ts,
		TraceID:      "trace-123",
		SpanID:       "span-456",
		ParentID:     "parent-789",
		Operation:    "test.op",
		Name:         "TestOp",
		Status:       foxcular.StatusError,
		Duration:     250 * time.Millisecond,
		Message:      "test message",
		ErrorType:    "timeout",
		ErrorCode:    "ERR_TIMEOUT",
		ErrorMessage: "context deadline exceeded",
		Data:         map[string]any{"count": 42, "items": []any{"a", "b"}},
	}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify duration_ms is in JSON
	if !strings.Contains(string(data), `"duration_ms":250`) {
		t.Errorf("JSON missing duration_ms: %s", string(data))
	}

	var decoded foxcular.Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Timestamp.UTC().Format(time.RFC3339Nano) != ts.Format(time.RFC3339Nano) {
		t.Errorf("timestamp mismatch: got %v, want %v", decoded.Timestamp, ts)
	}
	if decoded.Duration != orig.Duration {
		t.Errorf("duration mismatch: got %v, want %v", decoded.Duration, orig.Duration)
	}
	if decoded.TraceID != orig.TraceID {
		t.Errorf("trace_id mismatch: got %q, want %q", decoded.TraceID, orig.TraceID)
	}
	if decoded.Status != orig.Status {
		t.Errorf("status mismatch: got %q, want %q", decoded.Status, orig.Status)
	}
	if decoded.Data["count"].(float64) != 42 {
		t.Errorf("data[count] mismatch: got %v", decoded.Data["count"])
	}
}
