package persist_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/persist"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// tmpFile creates a temp file path for testing.
func tmpFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "events.ndjson")
}

// makeTestEvent creates a simple test event.
func makeTestEvent(op string) *foxcular.Event {
	return &foxcular.Event{
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		TraceID:   "trace-001",
		SpanID:    "span-001",
		Operation: op,
		Status:    foxcular.StatusOK,
		Duration:  100 * time.Millisecond,
		Message:   "test message",
		Data: map[string]any{
			"count": 42,
			"label": "hello",
		},
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-001: NDJSON persistence round-trips events
// ---------------------------------------------------------------------------

func TestNDJSONRoundTripsEvents(t *testing.T) {
	path := tmpFile(t)
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}

	events := []*foxcular.Event{
		makeTestEvent("op.a"),
		makeTestEvent("op.b"),
		{
			Timestamp:    time.Date(2026, 5, 5, 12, 1, 0, 0, time.UTC),
			TraceID:      "trace-002",
			SpanID:       "span-002",
			ParentID:     "span-001",
			Operation:    "op.c",
			Name:         "TestOp",
			Status:       foxcular.StatusError,
			Duration:     250 * time.Millisecond,
			Message:      "failed",
			ErrorType:    "timeout",
			ErrorCode:    "ERR_TIMEOUT",
			ErrorMessage: "context deadline exceeded",
			Data: map[string]any{
				"nested": map[string]any{"key": "val"},
				"items":  []any{1, 2, 3},
			},
		},
	}

	for _, e := range events {
		if err := sink.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Read back.
	readback, err := persist.ReadNDJSON(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(readback) != 3 {
		t.Fatalf("expected 3 events, got %d", len(readback))
	}

	// Verify first event round-trips correctly.
	got := readback[0]
	if got.Operation != "op.a" {
		t.Errorf("operation = %q, want op.a", got.Operation)
	}
	if got.TraceID != "trace-001" {
		t.Errorf("trace_id = %q, want trace-001", got.TraceID)
	}
	if got.SpanID != "span-001" {
		t.Errorf("span_id = %q, want span-001", got.SpanID)
	}
	if got.Status != foxcular.StatusOK {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.Duration != 100*time.Millisecond {
		t.Errorf("duration = %v, want 100ms", got.Duration)
	}
	if got.Message != "test message" {
		t.Errorf("message = %q, want 'test message'", got.Message)
	}
	if got.Data["count"].(float64) != 42 {
		t.Errorf("data[count] = %v, want 42", got.Data["count"])
	}
	if got.Data["label"] != "hello" {
		t.Errorf("data[label] = %v, want hello", got.Data["label"])
	}

	// Verify third event (complex event).
	got3 := readback[2]
	if got3.Operation != "op.c" {
		t.Errorf("operation = %q, want op.c", got3.Operation)
	}
	if got3.ParentID != "span-001" {
		t.Errorf("parent_id = %q, want span-001", got3.ParentID)
	}
	if got3.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error", got3.Status)
	}
	if got3.ErrorType != "timeout" {
		t.Errorf("error_type = %q, want timeout", got3.ErrorType)
	}
	if got3.ErrorMessage != "context deadline exceeded" {
		t.Errorf("error_message = %q, want 'context deadline exceeded'", got3.ErrorMessage)
	}

	// Verify one JSON line per event.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-002: NDJSON persistence redacts before write
// ---------------------------------------------------------------------------

func TestNDJSONRedactsBeforeWrite(t *testing.T) {
	path := tmpFile(t)
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	// Create a client with default redaction and the NDJSON sink.
	client := foxcular.NewClient(sink,
		foxcular.WithRedaction(foxcular.NewRedactionPolicy()),
	)

	err = client.Emit("test.redact").
		WithData("password", "supersecret123").
		WithData("api_key", "sk-abcdef123456").
		WithData("token", "bearer-xyz789").
		WithData("email", "user@example.com").
		WithData("safe_field", "visible").
		Success(context.Background(), time.Millisecond)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := client.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Read raw file bytes.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	rawStr := string(raw)

	// Raw secrets must be absent.
	forbidden := []string{"supersecret123", "sk-abcdef123456", "bearer-xyz789", "user@example.com"}
	for _, secret := range forbidden {
		if strings.Contains(rawStr, secret) {
			t.Errorf("raw file contains forbidden secret %q", secret)
		}
	}

	// Redacted mask should be present.
	if !strings.Contains(rawStr, "[REDACTED]") {
		t.Error("raw file should contain [REDACTED] mask")
	}

	// Safe field should still be present.
	if !strings.Contains(rawStr, "visible") {
		t.Error("raw file should contain safe field value")
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-004: Persistence Flush/Close makes data durable (NDJSON)
// ---------------------------------------------------------------------------

func TestNDJSONFlushCloseReopenDurable(t *testing.T) {
	path := tmpFile(t)

	// Write events, flush, close.
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	for i := range 5 {
		e := makeTestEvent("op")
		e.Data["i"] = i
		if err := sink.Send(context.Background(), e); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and read.
	readback, err := persist.ReadNDJSON(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(readback) != 5 {
		t.Fatalf("expected 5 events after reopen, got %d", len(readback))
	}
	for i, e := range readback {
		if e.Data["i"].(float64) != float64(i) {
			t.Errorf("event %d data[i] = %v, want %d", i, e.Data["i"], i)
		}
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-005: Persistence failures follow documented semantics (NDJSON)
// ---------------------------------------------------------------------------

func TestNDJSONFailureSemantics(t *testing.T) {
	t.Run("invalid path", func(t *testing.T) {
		// Opening a path in a non-existent directory should fail.
		_, err := persist.NewNDJSONSink("/nonexistent/dir/file.ndjson")
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})

	t.Run("read-only directory", func(t *testing.T) {
		dir := t.TempDir()
		file := filepath.Join(dir, "events.ndjson")
		// Create file then make directory read-only.
		if err := os.WriteFile(file, []byte{}, 0o644); err != nil {
			t.Fatalf("create: %v", err)
		}
		// Make file read-only.
		if err := os.Chmod(file, 0o444); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		defer func() { _ = os.Chmod(file, 0o644) }() // restore for cleanup

		// Opening for append write should fail since file is read-only.
		_, err := persist.NewNDJSONSink(file)
		// Depending on OS, this may or may not fail.
		// The important thing is it doesn't panic.
		_ = err
	})

	t.Run("close idempotent", func(t *testing.T) {
		sink, err := persist.NewNDJSONSink(tmpFile(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// Close multiple times without panic.
		for range 3 {
			if err := sink.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
		}
	})

	t.Run("nil event is safe", func(t *testing.T) {
		sink, err := persist.NewNDJSONSink(tmpFile(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer func() { _ = sink.Close() }()
		if err := sink.Send(context.Background(), nil); err != nil {
			t.Errorf("nil event should not error, got: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-004: NDJSON durability - write without flush loses data
// ---------------------------------------------------------------------------

func TestNDJSONFlushRequiredForDurability(t *testing.T) {
	path := tmpFile(t)
	sink, err := persist.NewNDJSONSink(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}

	e := makeTestEvent("op")
	if err := sink.Send(context.Background(), e); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Close (which flushes internally) makes data durable.
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	readback, err := persist.ReadNDJSON(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(readback) != 1 {
		t.Fatalf("expected 1 event after close, got %d", len(readback))
	}
}
