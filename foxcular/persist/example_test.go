package persist_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/persist"
)

// Example_ndjsonSink demonstrates writing events to an NDJSON file.
func Example_ndjsonSink() {
	dir, err := os.MkdirTemp("", "foxcular-example-*")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "events.ndjson")
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("user.signup").
		WithData("email", "alice@example.com").
		Success(context.Background(), 0)

	_ = client.Flush(context.Background())
	_ = client.Close()

	// Read back the events.
	events, err := persist.ReadNDJSON(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	for _, e := range events {
		fmt.Printf("op=%s status=%s\n", e.Operation, e.Status)
	}
	// Output:
	// op=user.signup status=ok
}

// Example_ndjsonRedaction demonstrates that NDJSON files contain redacted data.
func Example_ndjsonRedaction() {
	dir, err := os.MkdirTemp("", "foxcular-example-*")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "events.ndjson")
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("api.call").
		WithData("api_key", "sk-secret-key-12345").
		WithData("password", "hunter2").
		Success(context.Background(), 0)

	_ = client.Flush(context.Background())
	_ = client.Close()

	events, err := persist.ReadNDJSON(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	for _, e := range events {
		fmt.Printf("api_key=%v password=%v\n",
			e.Data["api_key"], e.Data["password"])
	}
	// Output:
	// api_key=[REDACTED] password=[REDACTED]
}

// Example_sqliteSink demonstrates storing events in SQLite.
func Example_sqliteSink() {
	dir, err := os.MkdirTemp("", "foxcular-example-*")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "events.db")
	sink, err := persist.NewSQLiteSink(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	client := foxcular.NewClient(sink, foxcular.WithSampler(foxcular.AlwaysSample{}))

	_ = client.Emit("job.complete").
		WithData("job_id", "job-42").
		WithData("rows", 100).
		Success(context.Background(), 250e6) // 250ms

	_ = client.Flush(context.Background())
	_ = client.Close()

	// Reopen and query.
	sink2, err := persist.NewSQLiteSink(path)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	defer func() { _ = sink2.Close() }()

	events, err := sink2.QueryAllEvents()
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	for _, e := range events {
		fmt.Printf("op=%s status=%s dur=%dms rows=%v\n",
			e.Operation, e.Status, e.Duration.Milliseconds(), e.Data["rows"])
	}
	// Output:
	// op=job.complete status=ok dur=250ms rows=100
}
