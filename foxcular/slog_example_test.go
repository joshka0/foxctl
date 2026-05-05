package foxcular_test

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/joshka0/foxcular"
)

// Example_slogStandalone demonstrates slog integration without an active span.
func Example_slogStandalone() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	handler := foxcular.NewSlogHandler(client, &foxcular.SlogHandlerOptions{
		Operation: "app.log",
	})
	logger := slog.New(handler)

	logger.Info("user signed in", "user_id", 42, "method", "oauth")

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s msg=%s user_id=%v method=%v\n",
			e.Operation, e.Message, e.Data["user_id"], e.Data["method"])
	}
	// Output:
	// op=app.log msg=user signed in user_id=42 method=oauth
}

// Example_slogWithSpan demonstrates slog records attached to an active span.
func Example_slogWithSpan() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	ctx, span := client.StartSpan(context.Background(), "request.handle")
	defer func() { _ = span.End(nil) }()

	logger.InfoContext(ctx, "processing request", "path", "/api/users")

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("op=%s trace_set=%v parent_set=%v\n",
			e.Operation, e.TraceID != "", e.ParentID != "")
	}
	// Output:
	// op=slog trace_set=true parent_set=true
}

// Example_slogRedaction demonstrates that slog attributes are redacted.
func Example_slogRedaction() {
	sink := &captureSink{}
	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))
	defer func() { _ = client.Close() }()

	handler := foxcular.NewSlogHandler(client, nil)
	logger := slog.New(handler)

	logger.Info("login attempt", "token", "bearer abc123secret", "user", "alice")

	_ = client.Flush(context.Background())

	for _, e := range sink.events {
		fmt.Printf("token=%v user=%v\n",
			e.Data["token"], e.Data["user"])
	}
	// Output:
	// token=[REDACTED] user=alice
}
