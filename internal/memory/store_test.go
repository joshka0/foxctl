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
