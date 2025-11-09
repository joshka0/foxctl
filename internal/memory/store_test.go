package memory

import (
	"context"
	"testing"
)

func TestSaveAndGet(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{"artifact":"sha256:abc"},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "spec", "openapi_spec", "/workspace", "demo", result); err != nil {
		t.Fatalf("save: %v", err)
	}
	entry, err := store.Get(ctx, "spec", "/workspace")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Name != "spec" || entry.Type != "openapi_spec" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
	if len(entry.Digests) != 1 || entry.Digests[0] != "sha256:abc" {
		t.Fatalf("expected digest recorded")
	}
}

func TestListFiltersWorkspace(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "one", "result", "/ws1", "", result); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "two", "result", "/ws2", "", result); err != nil {
		t.Fatalf("save: %v", err)
	}

	entries, err := store.List(ctx, "/ws1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "one" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
}

func TestSearchAndUpdate(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "beta", "result", "ws", "beta summary", result); err != nil {
		t.Fatalf("save beta: %v", err)
	}
	entries, err := store.Search(ctx, "ws", "alpha", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "alpha" {
		t.Fatalf("expected alpha search result, got %#v", entries)
	}
	newSummary := "updated"
	updated, err := store.Update(ctx, "alpha", "ws", &newSummary, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Summary != newSummary {
		t.Fatalf("expected updated summary")
	}
}

func TestRelevantRanking(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	entry, err := store.SaveFromResult(ctx, "fresh", "result", "ws", "fresh", result)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	// simulate older entry with more accesses
	old, err := store.SaveFromResult(ctx, "old", "result", "ws", "old", result)
	if err != nil {
		t.Fatalf("save old: %v", err)
	}
	_, _ = store.Get(ctx, old.Name, "ws")
	_, _ = store.Get(ctx, old.Name, "ws")
	// Manually adjust timestamps to ensure ordering difference
	_, err = store.Update(ctx, entry.Name, "ws", nil, nil)
	if err != nil {
		t.Fatalf("touch fresh: %v", err)
	}
	entries, err := store.Relevant(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("relevant: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 entries, got %d", len(entries))
	}
	if !(entries[0].Score >= entries[1].Score) {
		t.Fatalf("expected sorted scores, got %f < %f", entries[0].Score, entries[1].Score)
	}
}
