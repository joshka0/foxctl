package memory

import (
	"context"
	"testing"
)

func TestIndexerState_SetAndGet(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	workspaceID := "ws"
	indexerID := "code_symbol_dag"
	headSHA := "abc123"

	if _, err := store.SetLastIndexedHeadSHA(ctx, workspaceID, indexerID, headSHA); err != nil {
		t.Fatalf("set: %v", err)
	}

	state, ok, err := store.GetIndexerState(ctx, workspaceID, indexerID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if state.WorkspaceID != workspaceID {
		t.Fatalf("workspace mismatch: got %q want %q", state.WorkspaceID, workspaceID)
	}
	if state.IndexerID != indexerID {
		t.Fatalf("indexer mismatch: got %q want %q", state.IndexerID, indexerID)
	}
	if state.LastIndexedHeadSHA != headSHA {
		t.Fatalf("head sha mismatch: got %q want %q", state.LastIndexedHeadSHA, headSHA)
	}
	if state.UpdatedAt.IsZero() || state.CreatedAt.IsZero() {
		t.Fatalf("expected timestamps to be set: %#v", state)
	}
}

func TestIndexerState_UpsertUpdatesSHA(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	workspaceID := "ws"
	indexerID := "code_symbol_dag"

	if _, err := store.SetLastIndexedHeadSHA(ctx, workspaceID, indexerID, "abc123"); err != nil {
		t.Fatalf("set(1): %v", err)
	}
	if _, err := store.SetLastIndexedHeadSHA(ctx, workspaceID, indexerID, "def456"); err != nil {
		t.Fatalf("set(2): %v", err)
	}

	state, ok, err := store.GetIndexerState(ctx, workspaceID, indexerID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if state.LastIndexedHeadSHA != "def456" {
		t.Fatalf("expected updated sha, got %q", state.LastIndexedHeadSHA)
	}
}

func TestMigrateWorkspace_MovesIndexerState(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	from := "ws_from"
	to := "ws_to"
	indexerID := "code_symbol_dag"

	if _, err := store.SetLastIndexedHeadSHA(ctx, from, indexerID, "abc123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if _, err := store.MigrateWorkspace(ctx, from, to, false); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, ok, err := store.GetIndexerState(ctx, from, indexerID); err != nil {
		t.Fatalf("get(from): %v", err)
	} else if ok {
		t.Fatalf("expected state to be removed from source workspace")
	}

	state, ok, err := store.GetIndexerState(ctx, to, indexerID)
	if err != nil {
		t.Fatalf("get(to): %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for target workspace")
	}
	if state.LastIndexedHeadSHA != "abc123" {
		t.Fatalf("sha mismatch: got %q want %q", state.LastIndexedHeadSHA, "abc123")
	}
}
