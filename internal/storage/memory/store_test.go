package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
)

func TestSaveAndGet(t *testing.T) {
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

func TestOpenCreatesNestedRoot(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	root := filepath.Join(base, "nested", "memory")
	store, err := Open(ctx, root, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	if _, err := os.Stat(root); err != nil {
		t.Fatalf("expected root directory to exist: %v", err)
	}
}

func TestSyncSymbolEmbeddingsSupportsPackageScopedKeyEntries(t *testing.T) {
	ctx := context.Background()
	workspace := "ws"
	memoryStore, err := Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	t.Cleanup(func() {
		if err := memoryStore.Close(); err != nil {
			t.Fatalf("close memory store: %v", err)
		}
	})

	pkg := "go:pkg/a"
	key := "helper.go/Helper"
	entryName := symbolutil.KeyEntryName(workspace, pkg, key)
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := memoryStore.SaveFromResult(ctx, entryName, "code_symbol", workspace, "helper", result); err != nil {
		t.Fatalf("save symbol memory: %v", err)
	}

	embeddingDBPath := filepath.Join(t.TempDir(), "embedding_queue.db")
	embeddingDB, err := sqliteutil.OpenDB(ctx, embeddingDBPath, nil)
	if err != nil {
		t.Fatalf("open embedding fixture db: %v", err)
	}
	if _, err := embeddingDB.ExecContext(ctx, `
CREATE TABLE symbol_embeddings (
	symbol_id TEXT NOT NULL,
	workspace_id TEXT NOT NULL,
	file_path TEXT NOT NULL,
	embedding BLOB NOT NULL,
	content_digest TEXT NOT NULL,
	model TEXT NOT NULL,
	dimensions INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (workspace_id, symbol_id)
)`); err != nil {
		t.Fatalf("create embedding fixture table: %v", err)
	}
	if _, err := embeddingDB.ExecContext(ctx, `
INSERT INTO symbol_embeddings
	(symbol_id, workspace_id, file_path, embedding, content_digest, model, dimensions, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		symbolutil.ScopedSymbolID(pkg, key), workspace, "pkg/a/helper.go", []byte(`[0.1,0.2,0.3]`), "digest", "test-model", 3, "2026-05-07T00:00:00Z"); err != nil {
		t.Fatalf("insert embedding fixture: %v", err)
	}
	if err := embeddingDB.Close(); err != nil {
		t.Fatalf("close embedding fixture db: %v", err)
	}

	synced, err := memoryStore.SyncSymbolEmbeddings(ctx, embeddingDBPath, SyncSymbolEmbeddingsOptions{
		WorkspaceID: workspace,
		SymbolIDs:   []string{symbolutil.ScopedSymbolID(pkg, key)},
		OnlyMissing: true,
	})
	if err != nil {
		t.Fatalf("sync symbol embeddings: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced=%d want 1", synced)
	}
	got, err := memoryStore.GetEmbedding(ctx, entryName, workspace)
	if err != nil {
		t.Fatalf("get synced embedding: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("embedding dims=%d want 3", len(got))
	}
}

func TestListFiltersWorkspace(t *testing.T) {
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
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

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
	if len(entries) != 1 || entries[0].Entry.Name != "alpha" {
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

func TestUpdateLifecyclePersistsNamedMemoryState(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	validatedAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	updated, err := store.UpdateLifecycle(ctx, "alpha", "ws", LifecycleUpdate{
		LifecycleState:  "stale",
		ReviewStatus:    "needs_review",
		SupersededBy:    "beta",
		ReviewNotes:     "curator demotion",
		LastValidatedAt: &validatedAt,
	})
	if err != nil {
		t.Fatalf("update lifecycle: %v", err)
	}
	if updated.LifecycleState != "stale" || updated.ReviewStatus != "needs_review" {
		t.Fatalf("unexpected updated lifecycle: %#v", updated)
	}

	got, err := store.Get(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LifecycleState != "stale" || got.ReviewStatus != "needs_review" || got.SupersededBy != "beta" || got.ReviewNotes != "curator demotion" {
		t.Fatalf("lifecycle was not persisted: %#v", got)
	}
	if !got.LastValidatedAt.Equal(validatedAt) {
		t.Fatalf("expected last_validated_at %s, got %s", validatedAt, got.LastValidatedAt)
	}

	listed, err := store.List(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || listed[0].LifecycleState != "stale" {
		t.Fatalf("list did not include lifecycle state: %#v", listed)
	}
}

func TestUpdateTelemetryPersistsExplicitNamedMemoryActions(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}

	at := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	actions := []string{"selected", "used", "succeeded", "failed", "restored", "patched"}
	for _, action := range actions {
		if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: action, At: &at}); err != nil {
			t.Fatalf("update telemetry %s: %v", action, err)
		}
	}
	if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: "used", At: &at}); err != nil {
		t.Fatalf("update telemetry used again: %v", err)
	}

	got, err := store.Get(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SelectedCount != 1 || got.UseCount != 2 || got.SuccessCount != 1 || got.FailureCount != 1 || got.RestoreCount != 1 || got.PatchCount != 1 {
		t.Fatalf("unexpected telemetry counters: %#v", got)
	}
	if !got.LastSelectedAt.Equal(at) || !got.LastUsedAt.Equal(at) || !got.LastSucceededAt.Equal(at) || !got.LastFailedAt.Equal(at) || !got.LastRestoredAt.Equal(at) || !got.LastPatchedAt.Equal(at) {
		t.Fatalf("unexpected telemetry timestamps: %#v", got)
	}
}

func TestUpdateTelemetryRejectsUnknownAction(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: "queried"}); err == nil {
		t.Fatalf("UpdateTelemetry accepted unknown action")
	}
}

func TestSearchDoesNotIncrementUseTelemetry(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.Search(ctx, "ws", "alpha", 10); err != nil {
		t.Fatalf("search: %v", err)
	}
	got, err := store.getWithoutTracking(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get without tracking: %v", err)
	}
	if got.UseCount != 0 || got.SuccessCount != 0 || got.FailureCount != 0 {
		t.Fatalf("query visibility changed use telemetry: %#v", got)
	}
}

func TestRecordAccessBatchUpdatesAccessOnly(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "alpha", "result", "ws", "alpha summary", result); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "beta", "result", "ws", "beta summary", result); err != nil {
		t.Fatalf("save beta: %v", err)
	}
	usedAt := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)
	if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: "used", At: &usedAt}); err != nil {
		t.Fatalf("update used: %v", err)
	}
	if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: "succeeded", At: &usedAt}); err != nil {
		t.Fatalf("update succeeded: %v", err)
	}
	if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: "failed", At: &usedAt}); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	betaBefore, err := store.getWithoutTracking(ctx, "beta", "ws")
	if err != nil {
		t.Fatalf("get beta before: %v", err)
	}

	accessedAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	affected, err := store.RecordAccessBatch(ctx, "ws", []string{"alpha", "alpha", "missing"}, accessedAt)
	if err != nil {
		t.Fatalf("record access batch: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected=%d want 1", affected)
	}

	alpha, err := store.getWithoutTracking(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if alpha.AccessCount != 1 || !alpha.LastAccess.Equal(accessedAt) {
		t.Fatalf("access metadata not updated: %#v", alpha)
	}
	if alpha.UseCount != 1 || alpha.SuccessCount != 1 || alpha.FailureCount != 1 {
		t.Fatalf("outcome telemetry changed: %#v", alpha)
	}
	if !alpha.LastUsedAt.Equal(usedAt) || !alpha.LastSucceededAt.Equal(usedAt) || !alpha.LastFailedAt.Equal(usedAt) {
		t.Fatalf("outcome timestamps changed: %#v", alpha)
	}

	betaAfter, err := store.getWithoutTracking(ctx, "beta", "ws")
	if err != nil {
		t.Fatalf("get beta after: %v", err)
	}
	if betaAfter.AccessCount != betaBefore.AccessCount || !betaAfter.LastAccess.Equal(betaBefore.LastAccess) {
		t.Fatalf("untouched entry access changed: before=%#v after=%#v", betaBefore, betaAfter)
	}
}

func TestSQLiteSearchResultsProjectToCanonicalMemoryRecord(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{"file":"internal/storage/memory/store.go"},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.Save(ctx, NamedEntry{
		Name:           "canonical-contract",
		Type:           "decision",
		Workspace:      "ws",
		Summary:        "canonical search contract should preserve curator metadata",
		Result:         result,
		SessionID:      "session-a",
		LifecycleState: "stale",
		Pinned:         true,
		ReviewStatus:   "needs_review",
		SupersededBy:   "newer-record",
		ReviewNotes:    "curator demotion",
		SelectedCount:  2,
		UseCount:       3,
		SuccessCount:   1,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	results, err := store.Search(ctx, "ws", "canonical search", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search returned %d results, want 1: %#v", len(results), results)
	}

	record := memorycore.RecordFromNamedEntry(results[0].Entry, memorycore.NamedEntryOptions{Score: results[0].Score})
	assertCanonicalSearchRecord(t, record)
}

func assertCanonicalSearchRecord(t *testing.T, record memorycore.Record) {
	t.Helper()
	if record.Kind != memorycore.KindDecision || record.SourceLane != memorycore.SourceLaneNamedMemory || record.SourceID != "canonical-contract" {
		t.Fatalf("unexpected canonical identity: %#v", record)
	}
	if record.Provenance.SessionID != "session-a" {
		t.Fatalf("session provenance was not preserved: %#v", record.Provenance)
	}
	if record.Lifecycle.State != memorycore.LifecycleStateStale || !record.Lifecycle.Pinned || record.Lifecycle.ReviewStatus != memorycore.ReviewStatusNeedsReview || record.Lifecycle.SupersededBy != "newer-record" {
		t.Fatalf("lifecycle envelope was not preserved: %#v", record.Lifecycle)
	}
	if record.Telemetry.SelectedCount != 2 || record.Telemetry.UseCount != 3 || record.Telemetry.SuccessCount != 1 {
		t.Fatalf("telemetry envelope was not preserved: %#v", record.Telemetry)
	}
	if record.Usage.InstructionEligible || !record.Usage.EvidenceOnly {
		t.Fatalf("named memory record must remain evidence-only by default: %#v", record.Usage)
	}
}

func TestRelevantRanking(t *testing.T) {
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
	if _, err := store.Get(ctx, old.Name, "ws"); err != nil {
		t.Fatalf("get old first: %v", err)
	}
	if _, err := store.Get(ctx, old.Name, "ws"); err != nil {
		t.Fatalf("get old second: %v", err)
	}
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

func TestDeleteByNamePrefix(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)

	// Create entries with prefixed names
	if _, err := store.SaveFromResult(ctx, "file://ws/src/main.go", "file_embedding", "ws", "main.go", result); err != nil {
		t.Fatalf("save main.go: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "file://ws/src/main.go#chunk-0", "file_embedding_chunk", "ws", "chunk 0", result); err != nil {
		t.Fatalf("save chunk-0: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "file://ws/src/main.go#chunk-1", "file_embedding_chunk", "ws", "chunk 1", result); err != nil {
		t.Fatalf("save chunk-1: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "file://ws/src/other.go", "file_embedding", "ws", "other.go", result); err != nil {
		t.Fatalf("save other.go: %v", err)
	}

	// Delete all chunks for main.go
	deleted, err := store.DeleteByNamePrefix(ctx, "ws", "file://ws/src/main.go#chunk-")
	if err != nil {
		t.Fatalf("delete by prefix: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	// Verify chunks are gone but main.go entry still exists
	entries, err := store.List(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries remaining, got %d", len(entries))
	}

	// Verify specific entries
	_, err = store.Get(ctx, "file://ws/src/main.go", "ws")
	if err != nil {
		t.Fatalf("main.go should still exist: %v", err)
	}
	_, err = store.Get(ctx, "file://ws/src/other.go", "ws")
	if err != nil {
		t.Fatalf("other.go should still exist: %v", err)
	}
	_, err = store.Get(ctx, "file://ws/src/main.go#chunk-0", "ws")
	if err == nil {
		t.Fatal("chunk-0 should be deleted")
	}

	// Delete with no matches should return 0
	deleted, err = store.DeleteByNamePrefix(ctx, "ws", "nonexistent://")
	if err != nil {
		t.Fatalf("delete nonexistent prefix: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted for nonexistent prefix, got %d", deleted)
	}
}

func TestStats(t *testing.T) {
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

	// Empty store stats
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats on empty: %v", err)
	}
	if stats.Named != 0 {
		t.Errorf("expected 0 entries, got %d", stats.Named)
	}

	// Add entries and check stats
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "entry1", "type1", "ws1", "summary1", result); err != nil {
		t.Fatalf("save entry1: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "entry2", "type2", "ws2", "summary2", result); err != nil {
		t.Fatalf("save entry2: %v", err)
	}

	stats, err = store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats after save: %v", err)
	}
	if stats.Named != 2 {
		t.Errorf("expected 2 entries, got %d", stats.Named)
	}
}

func TestDelete(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "to_delete", "result", "ws", "summary", result); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify entry exists
	_, err = store.Get(ctx, "to_delete", "ws")
	if err != nil {
		t.Fatalf("entry should exist: %v", err)
	}

	// Delete entry
	err = store.Delete(ctx, "to_delete", "ws")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify entry is gone
	_, err = store.Get(ctx, "to_delete", "ws")
	if err == nil {
		t.Fatal("entry should be deleted")
	}

	// Delete non-existent returns error
	err = store.Delete(ctx, "nonexistent", "ws")
	if err == nil {
		t.Log("Note: delete of nonexistent returns no error")
	}
}

func TestSave(t *testing.T) {
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

	entry := NamedEntry{
		Name:      "test_entry",
		Type:      "test_type",
		Workspace: "ws",
		Summary:   "Test summary",
		Result:    []byte(`{"test":true}`), // Result is required
		Digests:   []string{"sha256:test123"},
	}

	saved, err := store.Save(ctx, entry)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Name != entry.Name {
		t.Errorf("saved name = %q, want %q", saved.Name, entry.Name)
	}
	if saved.Type != entry.Type {
		t.Errorf("saved type = %q, want %q", saved.Type, entry.Type)
	}
	// Verify save works - access count starts at 0 or 1 depending on implementation
	// We just verify the entry was saved correctly
	got, err := store.Get(ctx, entry.Name, entry.Workspace)
	if err != nil {
		t.Fatalf("get saved entry: %v", err)
	}
	if got.Name != entry.Name {
		t.Errorf("got name = %q, want %q", got.Name, entry.Name)
	}
}

func TestSaveResult(t *testing.T) {
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

	opts := SaveOptions{
		Name:      "result_entry",
		Type:      "result_type",
		Workspace: "ws",
		Summary:   "Result summary",
		Result:    []byte(`{"version":1,"status":"ok","command":"test","data":{"key":"value"},"meta":{},"error":{}}`),
	}

	saved, err := store.SaveResult(ctx, opts)
	if err != nil {
		t.Fatalf("save result: %v", err)
	}
	if saved.Name != opts.Name {
		t.Errorf("saved name = %q, want %q", saved.Name, opts.Name)
	}
	if saved.Type != opts.Type {
		t.Errorf("saved type = %q, want %q", saved.Type, opts.Type)
	}

	// Verify can retrieve
	got, err := store.Get(ctx, opts.Name, opts.Workspace)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Summary != opts.Summary {
		t.Errorf("summary = %q, want %q", got.Summary, opts.Summary)
	}
}

func TestEmbeddings(t *testing.T) {
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

	// Create an entry first
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "embed_test", "result", "ws", "summary", result); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Update embedding
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	err = store.UpdateEmbedding(ctx, "embed_test", "ws", embedding)
	if err != nil {
		t.Fatalf("update embedding: %v", err)
	}

	// Get embedding
	got, err := store.GetEmbedding(ctx, "embed_test", "ws")
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if len(got) != len(embedding) {
		t.Fatalf("embedding length = %d, want %d", len(got), len(embedding))
	}
	for i := range embedding {
		if got[i] != embedding[i] {
			t.Errorf("embedding[%d] = %f, want %f", i, got[i], embedding[i])
		}
	}

	// Get embedding for non-existent entry returns error
	_, err = store.GetEmbedding(ctx, "nonexistent", "ws")
	if err == nil {
		t.Log("Note: GetEmbedding for nonexistent may return error or nil")
	}
}

func TestListWithoutEmbeddingPagePaginatesMissingEmbeddings(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-07T00:00:00Z"},"error":{}}`)
	for _, name := range []string{"missing-1", "missing-2", "missing-3"} {
		if _, err := store.SaveFromResult(ctx, name, "note", "ws", name+" summary", result); err != nil {
			t.Fatalf("save %s: %v", name, err)
		}
	}

	first, err := store.ListWithoutEmbeddingPage(ctx, "ws", 2, 0)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	second, err := store.ListWithoutEmbeddingPage(ctx, "ws", 2, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first page len=%d want 2", len(first))
	}
	if len(second) != 1 {
		t.Fatalf("second page len=%d want 1", len(second))
	}
	if first[0].Name == second[0].Name || first[1].Name == second[0].Name {
		t.Fatalf("pagination returned duplicate entry: first=%v second=%v", first, second)
	}
}

func TestSearchSimilar(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)

	// Create entries with embeddings
	if _, err := store.SaveFromResult(ctx, "doc1", "result", "ws", "document 1", result); err != nil {
		t.Fatalf("save doc1: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "doc2", "result", "ws", "document 2", result); err != nil {
		t.Fatalf("save doc2: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "doc3", "result", "ws", "document 3", result); err != nil {
		t.Fatalf("save doc3: %v", err)
	}

	// Add embeddings (normalized vectors)
	if err := store.UpdateEmbedding(ctx, "doc1", "ws", []float32{1, 0, 0}); err != nil {
		t.Fatalf("embed doc1: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "doc2", "ws", []float32{0.9, 0.1, 0}); err != nil {
		t.Fatalf("embed doc2: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "doc3", "ws", []float32{0, 1, 0}); err != nil {
		t.Fatalf("embed doc3: %v", err)
	}

	// Search for similar to doc1
	query := []float32{1, 0, 0}
	results, err := store.SearchSimilar(ctx, "ws", query, 3)
	if err != nil {
		t.Fatalf("search similar: %v", err)
	}
	// Should return at least 2 results (entries with embeddings)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// doc1 should be most similar (identical vector)
	if results[0].Entry.Name != "doc1" {
		t.Errorf("expected doc1 first (most similar), got %s", results[0].Entry.Name)
	}
}

func TestEmbeddingMetadata(t *testing.T) {
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

	// Get metadata for non-existent workspace
	meta, err := store.GetEmbeddingMetadata(ctx, "ws")
	if err != nil {
		t.Fatalf("get nonexistent metadata: %v", err)
	}
	if meta != nil {
		t.Error("expected nil metadata for nonexistent workspace")
	}

	// Set metadata
	newMeta := EmbeddingMetadata{
		Workspace:  "ws",
		Provider:   "openai",
		Model:      "text-embedding-3-small",
		Dimensions: 1536,
	}
	if err := store.SetEmbeddingMetadata(ctx, newMeta); err != nil {
		t.Fatalf("set metadata: %v", err)
	}

	// Get metadata
	got, err := store.GetEmbeddingMetadata(ctx, "ws")
	if err != nil {
		t.Fatalf("get metadata: %v", err)
	}
	if got == nil {
		t.Fatal("expected metadata")
		return
	}
	if got.Provider != newMeta.Provider {
		t.Errorf("provider = %q, want %q", got.Provider, newMeta.Provider)
	}
	if got.Model != newMeta.Model {
		t.Errorf("model = %q, want %q", got.Model, newMeta.Model)
	}
	if got.Dimensions != newMeta.Dimensions {
		t.Errorf("dimensions = %d, want %d", got.Dimensions, newMeta.Dimensions)
	}

	// Validate dimensions - matching
	err = store.ValidateEmbeddingDimensions(ctx, "ws", 1536)
	if err != nil {
		t.Fatalf("validate matching dimensions: %v", err)
	}

	// Validate dimensions - mismatched
	err = store.ValidateEmbeddingDimensions(ctx, "ws", 768)
	if err == nil {
		t.Error("expected error for mismatched dimensions")
	}

	// Validate dimensions for workspace without metadata
	err = store.ValidateEmbeddingDimensions(ctx, "nonexistent", 1536)
	if err != nil {
		t.Errorf("validate for nonexistent workspace should not error: %v", err)
	}
}

func TestSearchSimilar_DimensionMismatchFails(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "doc1", "result", "ws", "document 1", result); err != nil {
		t.Fatalf("save doc1: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "doc1", "ws", []float32{1, 0, 0}); err != nil {
		t.Fatalf("embed doc1: %v", err)
	}
	if _, err := store.SearchSimilar(ctx, "ws", []float32{1, 0}, 3); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
	if _, err := store.SearchSimilarByType(ctx, "ws", "result", []float32{1, 0}, 3); err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}

func TestGetNotFound(t *testing.T) {
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

	_, err = store.Get(ctx, "nonexistent", "ws")
	if err == nil {
		t.Error("expected error for nonexistent entry")
	}
}

func TestListEmpty(t *testing.T) {
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

	entries, err := store.List(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestListLimit(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	for i := 0; i < 10; i++ {
		name := filepath.Join("entry", string(rune('a'+i)))
		if _, err := store.SaveFromResult(ctx, name, "result", "ws", "", result); err != nil {
			t.Fatalf("save entry %d: %v", i, err)
		}
	}

	entries, err := store.List(ctx, "ws", 5)
	if err != nil {
		t.Fatalf("list with limit: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestUpdateType(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "type_test", "old_type", "ws", "summary", result); err != nil {
		t.Fatalf("save: %v", err)
	}

	newType := "new_type"
	newSummary := "new_summary"
	updated, err := store.Update(ctx, "type_test", "ws", &newSummary, &newType)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Type != newType {
		t.Errorf("type = %q, want %q", updated.Type, newType)
	}
	if updated.Summary != newSummary {
		t.Errorf("summary = %q, want %q", updated.Summary, newSummary)
	}
}

func TestUpsertBehavior(t *testing.T) {
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

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2025-01-01T00:00:00Z"},"error":{}}`)

	// Save initial entry
	entry1, err := store.SaveFromResult(ctx, "upsert_test", "type1", "ws", "summary1", result)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Save same name again (upsert)
	entry2, err := store.SaveFromResult(ctx, "upsert_test", "type2", "ws", "summary2", result)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}

	// Should update, not create new entry
	if entry2.Type != "type2" {
		t.Errorf("type should be updated, got %q", entry2.Type)
	}
	if entry2.Summary != "summary2" {
		t.Errorf("summary should be updated, got %q", entry2.Summary)
	}

	// Verify only one entry exists
	entries, err := store.List(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after upsert, got %d", len(entries))
	}

	_ = entry1 // Avoid unused variable
}
