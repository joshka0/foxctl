package cas

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStoreGCDryRun(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	putTestObject(t, store, "alpha")
	putTestObject(t, store, "beta")

	result, err := store.GC(ctx, GCOptions{
		DryRun:     true,
		OlderThan:  0,
		KeepPinned: true,
	})
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if result.ObjectsDeleted != 2 {
		t.Fatalf("expected 2 deletions in dry-run, got %d", result.ObjectsDeleted)
	}

	objects, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(objects) != 2 {
		t.Fatalf("dry-run should not delete objects")
	}
}

func TestStoreGCDeletesObjects(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	obj := putTestObject(t, store, "payload")

	result, err := store.GC(ctx, GCOptions{
		OlderThan:  0,
		KeepPinned: true,
	})
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if result.ObjectsDeleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", result.ObjectsDeleted)
	}
	if _, err := store.Head(ctx, obj.Digest); err != ErrNotFound {
		t.Fatalf("object should be removed, got %v", err)
	}
}

func TestStoreGCKeepPinned(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	obj := putTestObject(t, store, "pinned")
	if err := store.Pin(ctx, obj.Digest); err != nil {
		t.Fatalf("pin: %v", err)
	}

	result, err := store.GC(ctx, GCOptions{
		OlderThan:  0,
		KeepPinned: true,
	})
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if result.ObjectsSkipped != 1 {
		t.Fatalf("expected skip count 1, got %d", result.ObjectsSkipped)
	}
	if _, err := store.Head(ctx, obj.Digest); err != nil {
		t.Fatalf("pinned object should remain: %v", err)
	}
}

func TestStoreGCMaxDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		putTestObject(t, store, strings.Repeat("x", i+1))
	}

	result, err := store.GC(ctx, GCOptions{
		OlderThan:  0,
		MaxDelete:  1,
		KeepPinned: true,
	})
	if err != nil {
		t.Fatalf("gc: %v", err)
	}
	if result.ObjectsDeleted != 1 {
		t.Fatalf("expected 1 deletion, got %d", result.ObjectsDeleted)
	}
}

func TestStoreGCCtxCancel(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.GC(ctx, GCOptions{OlderThan: time.Second}); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func newTestStore(t testing.TB) *Store {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func putTestObject(t testing.TB, store *Store, payload string) Object {
	t.Helper()
	obj, err := store.Put(context.Background(), strings.NewReader(payload), "text/plain", nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return obj
}
