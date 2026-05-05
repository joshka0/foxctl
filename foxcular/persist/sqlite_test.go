package persist_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxcular"
	"github.com/joshka0/foxcular/persist"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func tmpDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test.db")
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-003: SQLite persistence stores queryable events
// ---------------------------------------------------------------------------

func TestSQLiteStoresQueryableEvents(t *testing.T) {
	path := tmpDB(t)
	sink, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}

	events := []*foxcular.Event{
		{
			Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
			TraceID:   "trace-001",
			SpanID:    "span-001",
			Operation: "http.request",
			Name:      "GET /api",
			Status:    foxcular.StatusOK,
			Duration:  100 * time.Millisecond,
			Message:   "success",
			Data: map[string]any{
				"status_code": float64(200),
				"items":       []any{float64(1), float64(2)},
			},
		},
		{
			Timestamp:    time.Date(2026, 5, 5, 12, 1, 0, 0, time.UTC),
			TraceID:      "trace-002",
			SpanID:       "span-002",
			ParentID:     "span-001",
			Operation:    "db.query",
			Status:       foxcular.StatusError,
			Duration:     250 * time.Millisecond,
			ErrorType:    "timeout",
			ErrorCode:    "ERR_TIMEOUT",
			ErrorMessage: "context deadline exceeded",
			Data: map[string]any{
				"query": "SELECT * FROM users",
			},
		},
	}

	for _, e := range events {
		if err := sink.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen and query.
	sink2, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = sink2.Close() }()

	readback, err := sink2.QueryAllEvents()
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(readback) != 2 {
		t.Fatalf("expected 2 events, got %d", len(readback))
	}

	// Verify first event.
	got := readback[0]
	if got.Operation != "http.request" {
		t.Errorf("operation = %q, want http.request", got.Operation)
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
	if got.Data["status_code"].(float64) != 200 {
		t.Errorf("data[status_code] = %v, want 200", got.Data["status_code"])
	}

	// Verify second event (error event).
	got2 := readback[1]
	if got2.Operation != "db.query" {
		t.Errorf("operation = %q, want db.query", got2.Operation)
	}
	if got2.ParentID != "span-001" {
		t.Errorf("parent_id = %q, want span-001", got2.ParentID)
	}
	if got2.Status != foxcular.StatusError {
		t.Errorf("status = %q, want error", got2.Status)
	}
	if got2.ErrorType != "timeout" {
		t.Errorf("error_type = %q, want timeout", got2.ErrorType)
	}
	if got2.ErrorMessage != "context deadline exceeded" {
		t.Errorf("error_message = %q, want 'context deadline exceeded'", got2.ErrorMessage)
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-003: SQLite custom data fields preserved
// ---------------------------------------------------------------------------

func TestSQLiteCustomDataPreserved(t *testing.T) {
	path := tmpDB(t)
	sink, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = sink.Close() }()

	e := &foxcular.Event{
		Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
		TraceID:   "t1",
		SpanID:    "s1",
		Operation: "test.custom",
		Status:    foxcular.StatusOK,
		Data: map[string]any{
			"nested": map[string]any{
				"key": "value",
				"num": float64(42),
			},
			"items": []any{float64(1), "two", true},
		},
	}
	if err := sink.Send(context.Background(), e); err != nil {
		t.Fatalf("send: %v", err)
	}

	readback, err := sink.QueryAllEvents()
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(readback) != 1 {
		t.Fatalf("expected 1 event, got %d", len(readback))
	}

	got := readback[0]
	nested, ok := got.Data["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested is not a map")
	}
	if nested["key"] != "value" {
		t.Errorf("nested[key] = %v, want value", nested["key"])
	}
	if nested["num"].(float64) != 42 {
		t.Errorf("nested[num] = %v, want 42", nested["num"])
	}
	items, ok := got.Data["items"].([]any)
	if !ok {
		t.Fatal("items is not a slice")
	}
	if len(items) != 3 {
		t.Errorf("items length = %d, want 3", len(items))
	}
}

// ---------------------------------------------------------------------------
// VAL-PERSIST-004: SQLite Flush/Close makes data durable
// ---------------------------------------------------------------------------

func TestSQLiteFlushCloseReopenDurable(t *testing.T) {
	path := tmpDB(t)

	// Write events.
	sink, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := range 5 {
		e := &foxcular.Event{
			Timestamp: time.Date(2026, 5, 5, 12, 0, i, 0, time.UTC),
			TraceID:   "trace-001",
			SpanID:    "span-001",
			Operation: "op",
			Status:    foxcular.StatusOK,
			Data:      map[string]any{"i": float64(i)},
		}
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

	// Reopen and verify.
	sink2, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = sink2.Close() }()

	readback, err := sink2.QueryAllEvents()
	if err != nil {
		t.Fatalf("query: %v", err)
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
// VAL-PERSIST-005: SQLite failures follow documented semantics
// ---------------------------------------------------------------------------

func TestSQLiteFailureSemantics(t *testing.T) {
	t.Run("nil event is safe", func(t *testing.T) {
		sink, err := persist.NewSQLiteSink(tmpDB(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer func() { _ = sink.Close() }()
		if err := sink.Send(context.Background(), nil); err != nil {
			t.Errorf("nil event should not error, got: %v", err)
		}
	})

	t.Run("close idempotent", func(t *testing.T) {
		sink, err := persist.NewSQLiteSink(tmpDB(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		for range 3 {
			if err := sink.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
		}
	})

	t.Run("invalid path", func(t *testing.T) {
		// SQLite can create the file but not in a nonexistent deeply nested dir.
		_, err := persist.NewSQLiteSink("/nonexistent/deep/dir/test.db")
		if err == nil {
			t.Error("expected error for invalid path")
		}
	})

	t.Run("forced event flag preserved", func(t *testing.T) {
		sink, err := persist.NewSQLiteSink(tmpDB(t))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer func() { _ = sink.Close() }()

		e := &foxcular.Event{
			Timestamp: time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC),
			TraceID:   "t1",
			SpanID:    "s1",
			Operation: "forced.op",
			Status:    foxcular.StatusOK,
			Forced:    true,
		}
		if err := sink.Send(context.Background(), e); err != nil {
			t.Fatalf("send: %v", err)
		}

		readback, err := sink.QueryAllEvents()
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(readback) != 1 {
			t.Fatalf("expected 1, got %d", len(readback))
		}
		if !readback[0].Forced {
			t.Error("forced flag not preserved")
		}
	})
}

// ---------------------------------------------------------------------------
// SQLite query with SQL suffix
// ---------------------------------------------------------------------------

func TestSQLiteQueryWithSuffix(t *testing.T) {
	path := tmpDB(t)
	sink, err := persist.NewSQLiteSink(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() { _ = sink.Close() }()

	for i := range 10 {
		e := &foxcular.Event{
			Timestamp: time.Date(2026, 5, 5, 12, 0, i, 0, time.UTC),
			TraceID:   "trace-001",
			SpanID:    "span-001",
			Operation: "op",
			Status:    foxcular.StatusOK,
			Data:      map[string]any{"i": float64(i)},
		}
		if err := sink.Send(context.Background(), e); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	rows, err := sink.QueryEvents("WHERE id > 5 ORDER BY id ASC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 rows with id > 5, got %d", count)
	}
}
