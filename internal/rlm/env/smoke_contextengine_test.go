package env

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite" // register sqlite driver for read-only count queries

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/rlm"
	"github.com/joshka0/foxctl/internal/runtime/hooks/memoryflow"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
)

// TestSmokeContextEngineWiring proves the contextengine SQLite store is
// actually populated when the live RLM/eval pipeline runs. It exercises three
// real production code paths against a temp storage root and asserts the
// expected rows land in contextengine.db:
//
//  1. Bootstrapper.Build() opens the contextengine store on disk.
//  2. ReadOnlyAdapter.ExecuteInternal("retrieve_mixed", ...) records a
//     retrieval episode (and sub-episodes) via the lane services.
//  3. memoryflow.HandleLifecycle for an Edit tool emits a code.changed_dirty
//     event and runs ApplyInvalidation, producing both context_events and
//     staleness_markers rows.
//
// If any of the three deltas are zero, the wiring is broken — not the test.
func TestSmokeContextEngineWiring(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	workspaceRoot := t.TempDir()

	cfg := config.Config{}
	cfg.Storage.Root = storageRoot

	// Disable the embedding/memory-capture side branches in memoryflow so we
	// isolate the contextengine-write code path.
	t.Setenv("FOXCTL_MEMORY_CAPTURE", "0")
	t.Setenv("FOXCTL_MEMORY_EMBED", "0")

	// --- Phase 1: bootstrap opens the contextengine + tasks stores ---
	bs := NewBootstrapper(BootstrapConfig{AppConfig: cfg})
	t.Cleanup(func() { _ = bs.Close() })

	env, err := bs.Build(ctx, rlm.Task{WorkspaceRoot: workspaceRoot, Prompt: "auth handler"})
	if err != nil {
		t.Fatalf("bootstrap build: %v", err)
	}
	if bs.ContextEngineStore() == nil {
		t.Fatal("bootstrap did not open contextengine store; cfg.Storage.Root may be unset or Open failed")
	}

	dbPath := filepath.Join(storageRoot, "contextengine.db")

	// --- Phase 2: tool dispatch should append a retrieval_episodes row ---
	adapter := NewReadOnlyAdapter(cfg, workspaceRoot, "", nil, env)
	adapter.SetContextEngineStore(bs.ContextEngineStore())
	adapter.SetTaskStore(bs.TaskStore())

	beforeEpisodes := countRows(t, dbPath, "retrieval_episodes")
	beforePacks := countRows(t, dbPath, "evidence_packs")
	beforeEvents := countRows(t, dbPath, "context_events")
	beforeStaleness := countRows(t, dbPath, "staleness_markers")
	t.Logf("baseline: episodes=%d packs=%d events=%d staleness=%d", beforeEpisodes, beforePacks, beforeEvents, beforeStaleness)

	// retrieve_mixed exercises the most lanes; even when downstream lookups
	// return zero hits, each lane's wrapper still RecordRetrievalEpisode.
	// We use ExecuteInternal so the model-visible allowlist is bypassed.
	args := json.RawMessage(`{"query":"auth handler","limit":3}`)
	if _, err := adapter.ExecuteInternal(ctx, "retrieve_mixed", args); err != nil {
		t.Fatalf("retrieve_mixed: %v", err)
	}

	// retrieve_mixed fans out to 4 sub-lanes + records its own pack/episode.
	// Each lane must record both an episode and a pack on every exit path
	// (success, empty, error) — the eval harness needs uniform records.
	const wantDelta = 5
	afterEpisodes := countRows(t, dbPath, "retrieval_episodes")
	if got := afterEpisodes - beforeEpisodes; got != wantDelta {
		t.Errorf("retrieval_episodes delta=%d, want=%d (1 mixed + 4 sub-lanes); a sub-lane is short-circuiting recordEpisode", got, wantDelta)
	} else {
		t.Logf("retrieve_mixed recorded %d retrieval_episodes (was %d)", got, beforeEpisodes)
	}

	afterPacks := countRows(t, dbPath, "evidence_packs")
	if got := afterPacks - beforePacks; got != wantDelta {
		t.Errorf("evidence_packs delta=%d, want=%d (1 mixed + 4 sub-lanes); a sub-lane is short-circuiting recordPack", got, wantDelta)
	} else {
		t.Logf("retrieve_mixed persisted %d evidence_packs (was %d)", got, beforePacks)
	}

	// --- Phase 3: memoryflow Edit lifecycle → context_events + staleness_markers ---
	deps := memoryflow.Dependencies{Config: cfg}

	editFile := filepath.Join(workspaceRoot, "test.go")
	req := memoryflow.LifecycleRequest{
		Workspace: workspaceRoot,
		Payload:   memoryflow.LifecyclePayload{ToolName: "Edit"},
	}
	req.Payload.ToolInput.FilePath = editFile
	req.Payload.ToolInput.OldString = "old"
	req.Payload.ToolInput.NewString = "new"

	if _, err := memoryflow.HandleLifecycle(ctx, deps, req); err != nil {
		t.Fatalf("memoryflow lifecycle: %v", err)
	}

	afterEvents := countRows(t, dbPath, "context_events")
	afterStaleness := countRows(t, dbPath, "staleness_markers")

	if afterEvents <= beforeEvents {
		t.Errorf("memoryflow Edit did not append context_events (before=%d, after=%d) — emitDirtyEditEvent may have early-returned",
			beforeEvents, afterEvents)
	} else {
		t.Logf("memoryflow Edit appended %d context_events (was %d)", afterEvents-beforeEvents, beforeEvents)
	}
	if afterStaleness <= beforeStaleness {
		t.Errorf("ApplyInvalidation did not upsert staleness_markers (before=%d, after=%d)",
			beforeStaleness, afterStaleness)
	} else {
		t.Logf("ApplyInvalidation upserted %d staleness_markers (was %d)", afterStaleness-beforeStaleness, beforeStaleness)
	}

	// Cross-check via the store API (workspace-scoped) — catches the
	// "rows written but workspace_id mismatch hides them from queries"
	// silent failure mode.
	store := bs.ContextEngineStore()
	wsEvents, err := store.ListEvents(ctx, ctxengstore.EventFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	wsIDs := make(map[string]int, len(wsEvents))
	for _, e := range wsEvents {
		wsIDs[e.WorkspaceID]++
	}
	t.Logf("ListEvents returned %d rows; workspace_ids=%v", len(wsEvents), wsIDs)
}

// countRows opens dbPath read-only and returns SELECT COUNT(*) FROM <table>.
// Returns 0 if the DB or table does not exist.
func countRows(t *testing.T, dbPath, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		t.Logf("countRows open %s: %v", dbPath, err)
		return 0
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Logf("countRows %s: %v", table, err)
		return 0
	}
	return n
}
