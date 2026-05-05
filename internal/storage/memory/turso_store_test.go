//go:build cgo && !race

package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
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

func TestLibSQLLocalStorePreservesLifecycleAndTelemetryAcrossReadPaths(t *testing.T) {
	ctx := context.Background()
	store := openLocalLibSQLOrSkip(t, ctx)
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
	assertLibSQLLifecycleTelemetry(t, got, validatedAt, telemetryAt)

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
	assertLibSQLLifecycleTelemetry(t, relevant[0].Entry, validatedAt, telemetryAt)
}

func TestLibSQLLocalSearchResultsProjectToCanonicalMemoryRecord(t *testing.T) {
	ctx := context.Background()
	store := openLocalLibSQLOrSkip(t, ctx)
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

func TestLibSQLLocalVectorSearchResultsProjectToCanonicalMemoryRecord(t *testing.T) {
	ctx := context.Background()
	store := openLocalLibSQLOrSkip(t, ctx)
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
		if isUnavailableLocalLibSQLError(err) {
			t.Skipf("local libsql vector write unavailable: %v", err)
		}
		t.Fatalf("save with embedding: %v", err)
	}

	assertVectorSearchRecord := func(name string, results []ScoredEntry, err error) {
		t.Helper()
		if err != nil {
			if isUnavailableLocalLibSQLError(err) {
				t.Skipf("%s unavailable with local libsql vector support: %v", name, err)
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

func TestLibSQLLocalStoreListWithoutEmbeddingReturnsSavedMemoryFields(t *testing.T) {
	ctx := context.Background()
	store := openLocalLibSQLOrSkip(t, ctx)
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

func openLocalLibSQLOrSkip(t *testing.T, ctx context.Context) *TursoStore {
	t.Helper()

	store, err := OpenLibSQL(ctx, dbdriver.LibSQLConfig{
		Path:             filepath.Join(t.TempDir(), "memory.libsql"),
		VectorDimensions: 4,
	})
	if err != nil {
		if isUnavailableLocalLibSQLError(err) {
			t.Skipf("local libsql vector support unavailable: %v", err)
		}
		t.Fatalf("OpenLibSQL() error = %v", err)
	}
	return store
}

func isUnavailableLocalLibSQLError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "requires cgo") ||
		strings.Contains(msg, "f32_blob") ||
		strings.Contains(msg, "vector") ||
		strings.Contains(msg, "sqlite-vec")
}

func assertEntryFromReadPath(t *testing.T, path string, entries []NamedEntry, validatedAt, telemetryAt time.Time) {
	t.Helper()
	for _, entry := range entries {
		if entry.Name == "alpha" {
			assertLibSQLLifecycleTelemetry(t, entry, validatedAt, telemetryAt)
			return
		}
	}
	t.Fatalf("%s did not return alpha: %#v", path, entries)
}

func assertLibSQLLifecycleTelemetry(t *testing.T, entry NamedEntry, validatedAt, telemetryAt time.Time) {
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
