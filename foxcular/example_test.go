package foxcular_test

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxcular"
)

// captureSink collects events for inspection in examples.
type captureSink struct {
	events []*foxcular.Event
}

func (s *captureSink) Send(_ context.Context, event *foxcular.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *captureSink) Flush(_ context.Context) error { return nil }
func (s *captureSink) Close() error                  { return nil }

func newCaptureClient(opts ...foxcular.ClientOption) (*foxcular.Client, *captureSink) {
	sink := &captureSink{}
	opts = append([]foxcular.ClientOption{foxcular.WithSampler(foxcular.AlwaysSample{})}, opts...)
	return foxcular.NewClient(sink, opts...), sink
}

// Example_emit demonstrates emitting a basic foxcular event with the client builder API.
func Example_emit() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	err := client.Emit("user.login").
		WithName("alice@example.com").
		WithData("source_ip", "192.168.1.42").
		WithData("user_agent", "Mozilla/5.0").
		Success(context.Background(), 120*time.Millisecond)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s status=%s name=%s dur=%dms\n",
			e.Operation, e.Status, e.Name, e.Duration.Milliseconds())
	}
	// Output:
	// op=user.login status=ok name=alice@example.com dur=120ms
}

// Example_span demonstrates span lifecycle: start a span, add data, end with success.
func Example_span() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	_, span := client.StartSpan(context.Background(), "db.query",
		foxcular.WithSpanName("SELECT users"),
		foxcular.WithSpanData("db.system", "postgres"),
	)
	span.AddData("rows_affected", 42)

	_ = span.End(nil)
	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s status=%s trace_id_set=%v\n",
			e.Operation, e.Status, e.TraceID != "")
	}
	// Output:
	// op=db.query status=ok trace_id_set=true
}

// Example_nestedSpans demonstrates parent-child span correlation.
func Example_nestedSpans() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	ctx, parent := client.StartSpan(context.Background(), "request.handle")
	parent.AddData("handler", "users")

	_, child := client.StartSpan(ctx, "db.query",
		foxcular.WithSpanName("SELECT users"),
	)
	child.AddData("sql", "SELECT * FROM users")

	_ = child.End(nil)
	_ = parent.End(nil)
	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s has_parent=%v\n", e.Operation, e.ParentID != "")
	}
	// Output:
	// op=db.query has_parent=true
	// op=request.handle has_parent=false
}

// Example_errorMetadata demonstrates ending a span with an error.
func Example_errorMetadata() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	_, span := client.StartSpan(context.Background(), "cache.get")
	span.AddData("key", "user:123")

	_ = span.End(fmt.Errorf("connection refused"))
	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s status=%s error_type=%s\n",
			e.Operation, e.Status, e.ErrorType)
	}
	// Output:
	// op=cache.get status=error error_type=network
}

// Example_immutableEvents demonstrates that events are immutable snapshots.
func Example_immutableEvents() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	data := map[string]any{"count": 42}
	err := client.Emit("job.run").
		WithDataMap(data).
		Success(context.Background(), 50*time.Millisecond)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	// Mutate the original data after emission.
	data["count"] = 999

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("count=%v\n", e.Data["count"])
	}
	// Output:
	// count=42
}

// Example_forcedEvents demonstrates that forced events bypass sampling.
func Example_forcedEvents() {
	// NeverSample drops all non-forced events.
	client, sink := newCaptureClient(foxcular.WithSampler(foxcular.NeverSample{}))
	defer func() { _ = client.Close() }()

	// Normal event — dropped by sampler.
	_ = client.Emit("debug.tick").
		Success(context.Background(), 0)

	// Forced event — always delivered.
	_ = client.Emit("security.alert").
		Forced().
		WithData("severity", "critical").
		Success(context.Background(), 0)

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s forced=%v\n", e.Operation, e.Forced)
	}
	// Output:
	// op=security.alert forced=true
}

// Example_redaction demonstrates automatic redaction of sensitive data.
func Example_redaction() {
	client, sink := newCaptureClient()
	defer func() { _ = client.Close() }()

	err := client.Emit("api.call").
		WithData("api_key", "sk-secret-12345").
		WithData("password", "hunter2").
		WithData("email", "user@example.com").
		WithData("safe_field", "visible").
		Success(context.Background(), 10*time.Millisecond)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("api_key=%v password=%v safe_field=%v\n",
			e.Data["api_key"], e.Data["password"], e.Data["safe_field"])
	}
	// Output:
	// api_key=[REDACTED] password=[REDACTED] safe_field=visible
}
