package memory

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/symbolutil"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"

	_ "modernc.org/sqlite"
)

func TestTursoStoreCloudOpenWhenConfigured(t *testing.T) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")
	if url == "" || token == "" {
		t.Skip("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN not set")
	}

	store, err := OpenTurso(context.Background(), dbdriver.TursoConfig{
		URL:              url,
		AuthToken:        token,
		VectorDimensions: 4,
	})
	if err != nil {
		t.Fatalf("OpenTurso() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if store.vectorDimension != 4 {
		t.Fatalf("vectorDimension = %d, want 4", store.vectorDimension)
	}
}

func TestTursoMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if err := migrateTursoWithDimensions(ctx, db, 4); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrateTursoWithDimensions(ctx, db, 4); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestTursoMigrationReturnsIndexCreationError(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE idx_named_memory_lifecycle (id TEXT)`); err != nil {
		t.Fatal(err)
	}

	err = migrateTursoWithDimensions(ctx, db, 4)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if !strings.Contains(err.Error(), "idx_named_memory_lifecycle") {
		t.Fatalf("error=%q want idx_named_memory_lifecycle context", err)
	}
}

func TestTursoLocalStorePreservesLifecycleAndTelemetryAcrossReadPaths(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "alpha",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "alpha summary",
		Result:    result,
		SessionID: "session-a",
	}); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "beta",
		Type:      "gotcha",
		Workspace: "ws",
		Summary:   "beta summary",
		Result:    result,
		SessionID: "session-b",
	}); err != nil {
		t.Fatalf("save beta: %v", err)
	}

	validatedAt := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	if _, err := store.UpdateLifecycle(ctx, "alpha", "ws", LifecycleUpdate{
		LifecycleState:  "stale",
		ReviewStatus:    "needs_review",
		SupersededBy:    "beta",
		ReviewNotes:     "curator demotion",
		LastValidatedAt: &validatedAt,
	}); err != nil {
		t.Fatalf("update lifecycle: %v", err)
	}

	telemetryAt := time.Date(2026, 5, 4, 12, 30, 0, 0, time.UTC)
	for _, action := range []string{"selected", "used", "succeeded", "failed", "restored", "patched", "used"} {
		if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: action, At: &telemetryAt}); err != nil {
			t.Fatalf("update telemetry %s: %v", action, err)
		}
	}

	got, err := store.Get(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	assertTursoLifecycleTelemetry(t, got, validatedAt, telemetryAt)

	listed, err := store.List(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	assertEntryFromReadPath(t, "List", listed, validatedAt, telemetryAt)

	filtered, total, err := store.ListFiltered(ctx, "ws", ListFilter{Types: []string{"decision"}, SessionID: "session-a"}, 10, 0)
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if total != 1 {
		t.Fatalf("ListFiltered total = %d, want 1", total)
	}
	assertEntryFromReadPath(t, "ListFiltered", filtered, validatedAt, telemetryAt)

	relevant, err := store.Relevant(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("relevant: %v", err)
	}
	if len(relevant) == 0 {
		t.Fatalf("Relevant returned no entries")
	}
	assertTursoLifecycleTelemetry(t, relevant[0].Entry, validatedAt, telemetryAt)
}

func TestTursoLocalStoreRejectsInvalidMemoryLifecycleUpdateWithoutMutatingEntry(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "alpha",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "alpha summary",
		Result:    result,
	}); err != nil {
		t.Fatalf("save alpha: %v", err)
	}

	_, err := store.UpdateLifecycle(ctx, "alpha", "ws", LifecycleUpdate{
		LifecycleState: "trusted",
		ReviewStatus:   "reviewed",
		SupersededBy:   "beta",
		ReviewNotes:    "should not persist",
	})
	if err == nil {
		t.Fatal("expected invalid lifecycle state error")
	}
	if !strings.Contains(err.Error(), "invalid memory lifecycle state") {
		t.Fatalf("error=%v want invalid memory lifecycle state", err)
	}

	got, err := store.Get(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if got.LifecycleState != "active" || got.ReviewStatus != "unreviewed" || got.SupersededBy != "" || got.ReviewNotes != "" {
		t.Fatalf("invalid lifecycle update mutated entry: %#v", got)
	}
}

func TestTursoLocalStoreRejectsInvalidMemoryReviewUpdateWithoutMutatingEntry(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "alpha",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "alpha summary",
		Result:    result,
	}); err != nil {
		t.Fatalf("save alpha: %v", err)
	}

	_, err := store.UpdateLifecycle(ctx, "alpha", "ws", LifecycleUpdate{
		LifecycleState: "stale",
		ReviewStatus:   "trusted",
		SupersededBy:   "beta",
		ReviewNotes:    "should not persist",
	})
	if err == nil {
		t.Fatal("expected invalid review status error")
	}
	if !strings.Contains(err.Error(), "invalid memory review status") {
		t.Fatalf("error=%v want invalid memory review status", err)
	}

	got, err := store.Get(ctx, "alpha", "ws")
	if err != nil {
		t.Fatalf("get alpha: %v", err)
	}
	if got.LifecycleState != "active" || got.ReviewStatus != "unreviewed" || got.SupersededBy != "" || got.ReviewNotes != "" {
		t.Fatalf("invalid review update mutated entry: %#v", got)
	}
}

func TestTursoLocalSaveRejectsGeneratedUnknownMemoryLifecycleStates(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	rejectsUnknownLifecycleState := func(raw string) bool {
		_, err := store.Save(ctx, NamedEntry{
			Name:           "generated-invalid-lifecycle",
			Type:           "result",
			Workspace:      "ws",
			Summary:        "invalid lifecycle should fail closed",
			Result:         []byte(`{"ok":true}`),
			LifecycleState: "unknown:" + raw,
		})
		entries, listErr := store.List(ctx, "ws", 10)
		return err != nil &&
			strings.Contains(err.Error(), "invalid memory lifecycle state") &&
			listErr == nil &&
			len(entries) == 0
	}

	if err := quick.Check(rejectsUnknownLifecycleState, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown lifecycle state was accepted: %v", err)
	}
}

func TestTursoLocalSaveRejectsGeneratedUnknownMemoryReviewStatuses(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	rejectsUnknownReviewStatus := func(raw string) bool {
		_, err := store.Save(ctx, NamedEntry{
			Name:         "generated-invalid-review",
			Type:         "result",
			Workspace:    "ws",
			Summary:      "invalid review should fail closed",
			Result:       []byte(`{"ok":true}`),
			ReviewStatus: "unknown:" + raw,
		})
		entries, listErr := store.List(ctx, "ws", 10)
		return err != nil &&
			strings.Contains(err.Error(), "invalid memory review status") &&
			listErr == nil &&
			len(entries) == 0
	}

	if err := quick.Check(rejectsUnknownReviewStatus, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("generated unknown review status was accepted: %v", err)
	}
}

func TestTursoLocalReadsRejectCorruptPersistedLifecycleMetadata(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		column      string
		value       string
		wantMessage string
	}{
		{
			name:        "lifecycle state",
			column:      "lifecycle_state",
			value:       "trusted",
			wantMessage: "invalid memory lifecycle state",
		},
		{
			name:        "review status",
			column:      "review_status",
			value:       "trusted",
			wantMessage: "invalid memory review status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openLocalTursoOrSkip(t, ctx)
			defer func() { _ = store.Close() }()

			result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
			if _, err := store.SaveResult(ctx, SaveOptions{
				Name:      "alpha",
				Type:      "decision",
				Workspace: "ws",
				Summary:   "alpha summary",
				Result:    result,
			}); err != nil {
				t.Fatalf("save alpha: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE named_memory SET "+tt.column+" = ? WHERE name = ? AND workspace = ?", tt.value, "alpha", "ws"); err != nil {
				t.Fatalf("corrupt %s: %v", tt.column, err)
			}

			if _, err := store.Get(ctx, "alpha", "ws"); err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("Get() error=%v, want %s", err, tt.wantMessage)
			}
			if _, err := store.List(ctx, "ws", 10); err == nil || !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("List() error=%v, want %s", err, tt.wantMessage)
			}
		})
	}
}

func TestTursoLocalSaveRejectsNegativeMemoryTelemetryCounters(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	_, err := store.Save(ctx, NamedEntry{
		Name:          "negative-telemetry",
		Type:          "decision",
		Workspace:     "ws",
		Summary:       "negative telemetry should fail before write",
		Result:        []byte(`{"ok":true}`),
		SelectedCount: -1,
	})
	if err == nil || !strings.Contains(err.Error(), "selected_count") || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("Save() error=%v, want selected_count non-negative error", err)
	}
}

func TestTursoLocalReadsRejectNegativePersistedTelemetryCounters(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{
		"selected_count",
		"use_count",
		"success_count",
		"failure_count",
		"patch_count",
		"restore_count",
	} {
		t.Run(column, func(t *testing.T) {
			store := openLocalTursoOrSkip(t, ctx)
			defer func() { _ = store.Close() }()

			result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
			if _, err := store.SaveResult(ctx, SaveOptions{
				Name:      "alpha",
				Type:      "decision",
				Workspace: "ws",
				Summary:   "alpha summary",
				Result:    result,
			}); err != nil {
				t.Fatalf("save alpha: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, "UPDATE named_memory SET "+column+" = ? WHERE name = ? AND workspace = ?", -1, "alpha", "ws"); err != nil {
				t.Fatalf("corrupt %s: %v", column, err)
			}

			if _, err := store.Get(ctx, "alpha", "ws"); err == nil || !strings.Contains(err.Error(), column) {
				t.Fatalf("Get() error=%v, want it to name %s", err, column)
			}
			if _, err := store.List(ctx, "ws", 10); err == nil || !strings.Contains(err.Error(), column) {
				t.Fatalf("List() error=%v, want it to name %s", err, column)
			}
		})
	}
}

func TestTursoLocalRecordAccessBatchUpdatesAccessOnly(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "alpha",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "alpha summary",
		Result:    result,
	}); err != nil {
		t.Fatalf("save alpha: %v", err)
	}
	if _, err := store.SaveResult(ctx, SaveOptions{
		Name:      "beta",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "beta summary",
		Result:    result,
	}); err != nil {
		t.Fatalf("save beta: %v", err)
	}
	usedAt := time.Date(2026, 5, 3, 9, 0, 0, 0, time.UTC)
	for _, action := range []string{"used", "succeeded", "failed"} {
		if _, err := store.UpdateTelemetry(ctx, "alpha", "ws", TelemetryUpdate{Action: action, At: &usedAt}); err != nil {
			t.Fatalf("update telemetry %s: %v", action, err)
		}
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

func TestTursoLocalSearchResultsProjectToCanonicalMemoryRecord(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
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

func TestTursoLocalSearchIgnoresStopWordOnlyQuery(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "common-token-memory", "semantic_fact", "ws", "Works with existing recall records.", result); err != nil {
		t.Fatalf("save memory: %v", err)
	}

	results, err := store.Search(ctx, "ws", "with", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search returned %d results for stop-word-only query, want 0: %#v", len(results), results)
	}
}

func TestTursoLocalSearchUsesAtomicTextEntitiesAndKeywords(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "plain-memory", "semantic_fact", "ws", "plain summary", result); err != nil {
		t.Fatalf("save plain memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "atomic-memory", "semantic_fact", "ws", "generic summary", result); err != nil {
		t.Fatalf("save atomic memory: %v", err)
	}
	if err := store.UpdateAtomic(
		ctx, "atomic-memory", "ws",
		"Use the local embedder for repository retrieval checks.",
		[]string{"RepositoryMemory", "Embedder"},
		[]string{"retrieval", "reranker"},
	); err != nil {
		t.Fatalf("update atomic: %v", err)
	}

	results, err := store.Search(ctx, "ws", "embedder reranker", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search returned %d results, want 1: %#v", len(results), results)
	}
	entry := results[0].Entry
	if entry.Name != "atomic-memory" {
		t.Fatalf("Search returned %q, want atomic-memory", entry.Name)
	}
	if entry.AtomicText == "" || !containsString(entry.Entities, "Embedder") || !containsString(entry.Keywords, "reranker") {
		t.Fatalf("atomic fields were not projected in search result: %#v", entry)
	}
}

func TestTursoLocalSearchRejectsCorruptAtomicJSON(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "corrupt-atomic", "semantic_fact", "ws", "search summary", result); err != nil {
		t.Fatalf("save corrupt atomic: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE named_memory SET entities = ? WHERE name = ? AND workspace = ?`, `["search"`, "corrupt-atomic", "ws"); err != nil {
		t.Fatalf("corrupt entities: %v", err)
	}

	if _, err := store.Search(ctx, "ws", "search", 10); err == nil || !strings.Contains(err.Error(), "scan entities") {
		t.Fatalf("Search() error=%v, want scan entities error", err)
	}
}

func TestTursoLocalVectorSearchResultsProjectToCanonicalMemoryRecord(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	embedding := []float32{1, 0, 0, 0}
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T12:00:00Z"},"error":{}}`)
	if _, err := store.SaveWithEmbedding(ctx, NamedEntry{
		Name:           "canonical-contract",
		Type:           "decision",
		Workspace:      "ws",
		Summary:        "canonical vector contract should preserve curator metadata",
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
	}, embedding, "test-model"); err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso vector write unavailable: %v", err)
		}
		t.Fatalf("save with embedding: %v", err)
	}

	assertVectorSearchRecord := func(name string, results []ScoredEntry, err error) {
		t.Helper()
		if err != nil {
			if isUnavailableLocalTursoError(err) {
				t.Skipf("%s unavailable with local turso vector support: %v", name, err)
			}
			t.Fatalf("%s: %v", name, err)
		}
		if len(results) != 1 {
			t.Fatalf("%s returned %d results, want 1: %#v", name, len(results), results)
		}
		record := memorycore.RecordFromNamedEntry(results[0].Entry, memorycore.NamedEntryOptions{Score: results[0].Score})
		assertCanonicalSearchRecord(t, record)
	}

	results, err := store.SearchSimilar(ctx, "ws", embedding, 10)
	assertVectorSearchRecord("SearchSimilar", results, err)

	results, err = store.SearchSimilarByType(ctx, "ws", "decision", embedding, 10)
	assertVectorSearchRecord("SearchSimilarByType", results, err)

	results, err = store.SearchSimilarGlobal(ctx, embedding, 10)
	assertVectorSearchRecord("SearchSimilarGlobal", results, err)

	results, err = store.SearchSimilarMultiWorkspace(ctx, []string{"ws"}, embedding, 10)
	assertVectorSearchRecord("SearchSimilarMultiWorkspace", results, err)
}

func TestTursoSaveWithEmbeddingRejectsInvalidMemoryLifecycleMetadataBeforeVectorWrite(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	_, err := store.SaveWithEmbedding(ctx, NamedEntry{
		Name:           "invalid-vector-lifecycle",
		Type:           "decision",
		Workspace:      "ws",
		Summary:        "invalid vector lifecycle should fail before write",
		Result:         []byte(`{"ok":true}`),
		LifecycleState: "trusted",
	}, []float32{1, 0, 0, 0}, "test-model")
	if err == nil {
		t.Fatal("expected invalid lifecycle state error")
	}
	if !strings.Contains(err.Error(), "invalid memory lifecycle state") {
		t.Fatalf("error=%v want invalid memory lifecycle state", err)
	}
}

func TestTursoLocalStoreSearchSimilar4096Dimensions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "memory-4096.turso")
	store, err := OpenTurso(ctx, dbdriver.TursoConfig{
		Path:               dbPath,
		ReplicaPath:        dbPath,
		EnableVectorSearch: true,
		VectorDimensions:   4096,
	})
	if err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso unavailable: %v", err)
		}
		t.Fatalf("OpenTurso() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	embedding := make([]float32, 4096)
	for i := range embedding {
		embedding[i] = (float32(i%17) - 8) / 17
	}
	_, err = store.SaveWithEmbedding(ctx, NamedEntry{
		Name:      "qwen-sized",
		Type:      "decision",
		Workspace: "ws",
		Summary:   "4096 dimension vector smoke",
		Result:    []byte(`{"ok":true}`),
	}, embedding, "text-embedding-qwen3-embedding-8b")
	if err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso vector write unavailable: %v", err)
		}
		t.Fatalf("SaveWithEmbedding: %v", err)
	}

	results, err := store.SearchSimilar(ctx, "ws", embedding, 10)
	if err != nil {
		t.Fatalf("SearchSimilar: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchSimilar returned %d results, want 1", len(results))
	}
	if results[0].Entry.Name != "qwen-sized" {
		t.Fatalf("result name = %q, want qwen-sized", results[0].Entry.Name)
	}
}

func TestTursoLocalStoreListWithoutEmbeddingReturnsSavedMemoryFields(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	validatedAt := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-04T13:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, "needs-embedding", "note", "ws", "summary to embed", result); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := store.UpdateLifecycle(ctx, "needs-embedding", "ws", LifecycleUpdate{
		LifecycleState:  "active",
		ReviewStatus:    "reviewed",
		LastValidatedAt: &validatedAt,
	}); err != nil {
		t.Fatalf("update lifecycle: %v", err)
	}

	entries, err := store.ListWithoutEmbedding(ctx, "ws", 10)
	if err != nil {
		t.Fatalf("list without embedding: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListWithoutEmbedding returned %d entries, want 1: %#v", len(entries), entries)
	}
	if entries[0].Name != "needs-embedding" || entries[0].Type != "note" || entries[0].Summary != "summary to embed" {
		t.Fatalf("ListWithoutEmbedding returned wrong entry: %#v", entries[0])
	}
	if entries[0].ReviewStatus != "reviewed" || !entries[0].LastValidatedAt.Equal(validatedAt) {
		t.Fatalf("ListWithoutEmbedding dropped lifecycle fields: %#v", entries[0])
	}

	for _, name := range []string{"needs-embedding-page-2", "needs-embedding-page-3"} {
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
	if len(first) != 2 || len(second) != 1 {
		t.Fatalf("paged lengths = %d/%d, want 2/1", len(first), len(second))
	}
	if first[0].Name == second[0].Name || first[1].Name == second[0].Name {
		t.Fatalf("pagination returned duplicate entry: first=%v second=%v", first, second)
	}
}

func TestTursoLocalSyncSymbolEmbeddingsSupportsPackageScopedKeyEntries(t *testing.T) {
	ctx := context.Background()
	store := openLocalTursoOrSkip(t, ctx)
	defer func() { _ = store.Close() }()

	workspace := "ws"
	pkg := "go:pkg/a"
	key := "helper.go/Helper"
	entryName := symbolutil.KeyEntryName(workspace, pkg, key)
	result := []byte(`{"version":1,"status":"ok","command":"test","data":{},"meta":{"ts":"2026-05-08T00:00:00Z"},"error":{}}`)
	if _, err := store.SaveFromResult(ctx, entryName, "code_symbol", workspace, "helper", result); err != nil {
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
		symbolutil.ScopedSymbolID(pkg, key), workspace, "pkg/a/helper.go", []byte(`[0.1,0.2,0.3,0.4]`), "digest", "test-model", 4, "2026-05-08T00:00:00Z"); err != nil {
		t.Fatalf("insert embedding fixture: %v", err)
	}
	if err := embeddingDB.Close(); err != nil {
		t.Fatalf("close embedding fixture db: %v", err)
	}

	synced, err := store.SyncSymbolEmbeddings(ctx, embeddingDBPath, SyncSymbolEmbeddingsOptions{
		WorkspaceID: workspace,
		SymbolIDs:   []string{symbolutil.ScopedSymbolID(pkg, key)},
		OnlyMissing: true,
	})
	if err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso vector support unavailable: %v", err)
		}
		t.Fatalf("sync symbol embeddings: %v", err)
	}
	if synced != 1 {
		t.Fatalf("synced=%d want 1", synced)
	}
	got, err := store.Get(ctx, entryName, workspace)
	if err != nil {
		t.Fatalf("get synced entry: %v", err)
	}
	if got.Name != entryName {
		t.Fatalf("synced entry name = %q, want %q", got.Name, entryName)
	}

	results, err := store.SearchSimilarByType(ctx, workspace, "code_symbol", []float32{0.1, 0.2, 0.3, 0.4}, 10)
	if err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso vector search unavailable: %v", err)
		}
		t.Fatalf("search similar by type: %v", err)
	}
	if len(results) != 1 || results[0].Entry.Name != entryName {
		t.Fatalf("SearchSimilarByType returned %#v, want %s", results, entryName)
	}
}

func TestOpenWithConfigUsesSQLiteStoreByDefault(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "")

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	if _, ok := store.(*Store); !ok {
		t.Fatalf("OpenWithConfig() store type = %T, want *Store", store)
	}
	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if !strings.HasSuffix(stats.Path, "memory.db") {
		t.Fatalf("Stats.Path = %q, want sqlite memory.db path", stats.Path)
	}
}

func openLocalTursoOrSkip(t *testing.T, ctx context.Context) *TursoStore {
	t.Helper()

	store, err := OpenTurso(ctx, dbdriver.TursoConfig{
		Path:             filepath.Join(t.TempDir(), "memory.turso"),
		VectorDimensions: 4,
	})
	if err != nil {
		if isUnavailableLocalTursoError(err) {
			t.Skipf("local turso vector support unavailable: %v", err)
		}
		t.Fatalf("OpenTurso() error = %v", err)
	}
	return store
}

func isUnavailableLocalTursoError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "f32_blob") ||
		strings.Contains(msg, "vector") ||
		strings.Contains(msg, "sqlite-vec")
}

func assertEntryFromReadPath(t *testing.T, path string, entries []NamedEntry, validatedAt, telemetryAt time.Time) {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == "alpha" {
			assertTursoLifecycleTelemetry(t, entry, validatedAt, telemetryAt)
			return
		}
	}
	t.Fatalf("%s did not return alpha: %#v", path, entries)
}

func assertTursoLifecycleTelemetry(t *testing.T, entry NamedEntry, validatedAt, telemetryAt time.Time) {
	t.Helper()
	if entry.Name != "alpha" || entry.Type != "decision" || entry.SessionID != "session-a" {
		t.Fatalf("unexpected entry identity: %#v", entry)
	}
	if entry.LifecycleState != "stale" || entry.ReviewStatus != "needs_review" || entry.SupersededBy != "beta" || entry.ReviewNotes != "curator demotion" {
		t.Fatalf("lifecycle fields not preserved: %#v", entry)
	}
	if !entry.LastValidatedAt.Equal(validatedAt) {
		t.Fatalf("LastValidatedAt = %s, want %s", entry.LastValidatedAt, validatedAt)
	}
	if entry.SelectedCount != 1 || entry.UseCount != 2 || entry.SuccessCount != 1 || entry.FailureCount != 1 || entry.RestoreCount != 1 || entry.PatchCount != 1 {
		t.Fatalf("telemetry counters not preserved: %#v", entry)
	}
	if !entry.LastSelectedAt.Equal(telemetryAt) || !entry.LastUsedAt.Equal(telemetryAt) || !entry.LastSucceededAt.Equal(telemetryAt) || !entry.LastFailedAt.Equal(telemetryAt) || !entry.LastRestoredAt.Equal(telemetryAt) || !entry.LastPatchedAt.Equal(telemetryAt) {
		t.Fatalf("telemetry timestamps not preserved: %#v", entry)
	}
}
