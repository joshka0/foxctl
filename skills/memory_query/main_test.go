package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage"
	contextstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func readWideEvents(t *testing.T, dir string) []observability.WideEvent {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "events", observability.WideEventFileName+".ndjson"))
	require.NoError(t, err)
	lines := bytes.Split(bytes.TrimSpace(body), []byte("\n"))
	events := make([]observability.WideEvent, 0, len(lines))
	for _, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event observability.WideEvent
		require.NoError(t, json.Unmarshal(line, &event))
		events = append(events, event)
	}
	return events
}

func openMemoryStore(t *testing.T, rc *skillmain.RunContext) *memory.Store {
	t.Helper()
	store, err := memory.Open(context.Background(), rc.Config.Storage.Root, rc.Config.Paths.CAS)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func openContextStore(t *testing.T, rc *skillmain.RunContext) contextstore.Store {
	t.Helper()
	store, err := contextstore.Open(context.Background(), rc.Config.Storage.Root)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedMemory(t *testing.T, store *memory.Store, workspace string, opts *seedOpts) storage.NamedEntry {
	t.Helper()
	if opts == nil {
		opts = &seedOpts{}
	}
	if opts.Name == "" {
		opts.Name = "test-record"
	}
	if opts.Type == "" {
		opts.Type = "semantic_fact"
	}
	if opts.Summary == "" {
		opts.Summary = "A test record entry"
	}

	entry := storage.NamedEntry{
		Name:      opts.Name,
		Type:      opts.Type,
		Workspace: workspace,
		Summary:   opts.Summary,
		SessionID: opts.SessionID,
	}

	// Result is required (NOT NULL constraint) - always provide a value
	if opts.Result != nil {
		resultJSON, err := json.Marshal(opts.Result)
		require.NoError(t, err)
		entry.Result = resultJSON
	} else {
		entry.Result = []byte("{}")
	}

	saved, err := store.Save(context.Background(), entry)
	require.NoError(t, err)
	return saved
}

type seedOpts struct {
	Name      string
	Type      string
	Summary   string
	SessionID string
	Result    map[string]any
}

func seedContextClaim(t *testing.T, store contextstore.Store, workspace string, claim contextengine.MemoryClaim) contextengine.MemoryClaim {
	t.Helper()
	if claim.ID == "" {
		claim.ID = "claim-test"
	}
	if claim.WorkspaceID == "" {
		claim.WorkspaceID = workspace
	}
	if claim.Status == "" {
		claim.Status = contextengine.ClaimStatusCurrent
	}
	if claim.ClaimType == "" {
		claim.ClaimType = "semantic_fact"
	}
	if claim.Summary == "" {
		claim.Summary = "Context claim record"
	}
	saved, err := store.UpsertClaim(context.Background(), claim)
	require.NoError(t, err)
	return saved
}

// Tests for validation

func TestMemoryQuery_MissingAllCriteria(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of query, file, kinds, or lifecycle_states must be provided")
}

func TestMemoryQuery_InvalidLifecycleState(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		LifecycleStates: "active,not-a-state",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory lifecycle state")
}

func TestMemoryQuery_QueryOnly(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "auth-bug",
		Type:    "semantic_fact",
		Summary: "Authentication tokens expire too quickly",
	})

	in := Input{
		Query: "authentication",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.NotNil(t, data["records"])
	assert.NotNil(t, data["pagination"])
	assert.NotNil(t, data["stats"])
}

func TestMemoryQueryEmitsWideEventTelemetry(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	obsDir := t.TempDir()
	observability.SetObsDirForTesting(obsDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "telemetry-decision",
		Type:    "decision",
		Summary: "Telemetry should record memory query counts",
	})

	err := run(context.Background(), rc, Input{Query: "telemetry"})
	require.NoError(t, err)

	var found *observability.WideEvent
	for _, event := range readWideEvents(t, obsDir) {
		if event.Operation == observability.OpMemoryQuery {
			found = &event
			break
		}
	}
	require.NotNil(t, found, "memory.query event not emitted")
	require.Equal(t, observability.ComponentSkill, found.Component)
	require.Equal(t, "memory/query", found.Command)
	require.Equal(t, rc.Workspace, found.WorkspaceID)
	require.Equal(t, observability.StatusOK, found.Status)
	require.Equal(t, true, found.Data["query_present"])
	require.Equal(t, float64(1), found.Data["records_returned"])
	require.NotEmpty(t, found.Data["search_method"])
}

func TestMemoryQuery_KindsOnly(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "semantic_fact-1",
		Type:    "semantic_fact",
		Summary: "First semantic_fact",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "decision-1",
		Type:    "decision",
		Summary: "First decision",
	})

	in := Input{
		Kinds: "semantic_fact",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "semantic_fact", mem["kind"])
}

func TestMemoryQuery_IncludesContextClaimLane(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	memStore := openMemoryStore(t, rc)
	seedMemory(t, memStore, rc.Workspace, &seedOpts{
		Name:    "named-memory-decision",
		Type:    "decision",
		Summary: "Named memory decision",
	})
	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-decision",
		ClaimType: "decision",
		Status:    contextengine.ClaimStatusCurrent,
		Scope: contextengine.ClaimScope{
			SessionID: "session-ctx",
			Path:      "internal/context/contextengine/claims.go",
		},
		Summary: "Context claim decision",
	})

	in := Input{
		Kinds: "decision",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 2)
	lanes := map[string]bool{}
	for _, m := range records {
		mem := m.(map[string]any)
		lanes[mem["source_lane"].(string)] = true
	}
	assert.True(t, lanes["named_memory"])
	assert.True(t, lanes["context_claim"])

	stats := data["stats"].(map[string]any)
	counts := stats["source_counts"].(map[string]any)
	assert.Equal(t, float64(1), counts["named_memory"])
	assert.Equal(t, float64(1), counts["context_claim"])
}

func TestMemoryQuery_FiltersContextClaimsByFile(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-auth",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Scope: contextengine.ClaimScope{
			Path: "internal/auth/handler.go",
		},
		Summary: "Auth handler context claim",
	})
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-router",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Scope: contextengine.ClaimScope{
			Path: "internal/api/router.go",
		},
		Summary: "Router context claim",
	})

	in := Input{
		File: "auth/handler.go",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "claim-auth", mem["source_id"])
	assert.Equal(t, "context_claim", mem["source_lane"])
}

func TestMemoryQuery_ExcludesStaleContextClaimsByDefault(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-current",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Current context claim",
	})
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-stale",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusStale,
		Summary:   "Stale context claim",
	})

	in := Input{
		Kinds: "semantic_fact",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "claim-current", mem["source_id"])

	stats := data["stats"].(map[string]any)
	assert.Equal(t, float64(1), stats["suppressed_by_lifecycle"])
	assert.Equal(t, "active_default_strong_candidate_stale", stats["lifecycle_policy"])
}

func TestMemoryQuery_IncludesExplicitLifecycleStates(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-current",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusCurrent,
		Summary:   "Current context claim",
	})
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-stale",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusStale,
		Summary:   "Stale context claim",
	})

	in := Input{
		LifecycleStates: "stale",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "claim-stale", mem["source_id"])

	stats := data["stats"].(map[string]any)
	assert.Equal(t, "explicit_lifecycle_states", stats["lifecycle_policy"])
	assert.Equal(t, "stale", stats["lifecycle_filter"])
}

func TestMemoryQuery_StrongQueryCanSurfaceStaleEvidence(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	claimStore := openContextStore(t, rc)
	seedContextClaim(t, claimStore, rc.Workspace, contextengine.MemoryClaim{
		ID:        "claim-stale-auth",
		ClaimType: "semantic_fact",
		Status:    contextengine.ClaimStatusStale,
		Summary:   "authentication token expiry",
	})

	in := Input{
		Query: "authentication token expiry",
		Kinds: "semantic_fact",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "claim-stale-auth", mem["source_id"])
	lifecycle := mem["lifecycle"].(map[string]any)
	assert.Equal(t, "stale", lifecycle["state"])
}

func TestMemoryQuery_FileOnly(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "edit:internal/auth/handler.go:fix",
		Type:    "edit",
		Summary: "Fixed auth handler bug",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "edit:internal/api/router.go:fix",
		Type:    "edit",
		Summary: "Fixed router bug",
	})

	in := Input{
		File: "auth/handler.go",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Contains(t, mem["source_id"], "auth/handler.go")
}

// Tests for filtering

func TestMemoryQuery_FilterByMultipleKinds(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "semantic_fact-1", Type: "semantic_fact", Summary: "Semantic fact 1"})
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "decision-1", Type: "decision", Summary: "Decision 1"})
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "context-1", Type: "context", Summary: "Context 1"})

	in := Input{
		Kinds: "semantic_fact,decision",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 2)

	kinds := make(map[string]bool)
	for _, m := range records {
		mem := m.(map[string]any)
		kinds[mem["kind"].(string)] = true
	}
	assert.True(t, kinds["semantic_fact"])
	assert.True(t, kinds["decision"])
}

func TestMemoryQuery_FilterBySessionID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:      "mem-session-a",
		Type:      "semantic_fact",
		Summary:   "Session A record",
		SessionID: "session-a",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:      "mem-session-b",
		Type:      "semantic_fact",
		Summary:   "Session B record",
		SessionID: "session-b",
	})

	in := Input{
		Kinds:     "semantic_fact",
		SessionID: "session-a",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
	mem := records[0].(map[string]any)
	provenance := mem["provenance"].(map[string]any)
	assert.Equal(t, "session-a", provenance["session_id"])
}

func TestMemoryQuery_ExcludesSymbolTypes(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "symbol:func:handleAuth",
		Type:    "symbol",
		Summary: "handleAuth function",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "code_symbol:class:AuthHandler",
		Type:    "code_symbol",
		Summary: "AuthHandler class",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "semantic_fact-auth",
		Type:    "semantic_fact",
		Summary: "Auth semantic_fact",
	})

	in := Input{
		Query: "auth",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	// Should only return the semantic_fact, not symbols
	for _, m := range records {
		mem := m.(map[string]any)
		assert.NotContains(t, mem["source_id"], "symbol:")
		assert.NotContains(t, mem["source_id"], "code_symbol:")
	}
}

// Tests for pagination

func TestMemoryQuery_Pagination(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 15 records
	for i := 0; i < 15; i++ {
		seedMemory(t, store, rc.Workspace, &seedOpts{
			Name:    "semantic_fact-" + string(rune('a'+i)),
			Type:    "semantic_fact",
			Summary: "Semantic fact " + string(rune('a'+i)),
		})
	}

	// First page
	in := Input{
		Kinds: "semantic_fact",
		Limit: 5,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(15), pagination["total"])
	assert.Equal(t, float64(0), pagination["offset"])
	assert.Equal(t, float64(5), pagination["limit"])
	assert.True(t, pagination["has_more"].(bool))

	records := data["records"].([]any)
	assert.Len(t, records, 5)
}

func TestMemoryQuery_PaginationOffset(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 10 records
	for i := 0; i < 10; i++ {
		seedMemory(t, store, rc.Workspace, &seedOpts{
			Name:    "semantic_fact-" + string(rune('a'+i)),
			Type:    "semantic_fact",
			Summary: "Semantic fact " + string(rune('a'+i)),
		})
	}

	in := Input{
		Kinds:  "semantic_fact",
		Limit:  5,
		Offset: 7,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(7), pagination["offset"])
	assert.False(t, pagination["has_more"].(bool))

	records := data["records"].([]any)
	assert.Len(t, records, 3) // 10 - 7 = 3
}

func TestMemoryQuery_LimitCapped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Type: "semantic_fact", Summary: "Test"})

	in := Input{
		Kinds: "semantic_fact",
		Limit: 999, // Should be capped to 100
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(100), pagination["limit"])
}

// Tests for search methods

func TestMemoryQuery_BM25Fallback(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "unique-keyword-xyz",
		Type:    "semantic_fact",
		Summary: "This has a unique keyword xyz for testing BM25",
	})

	in := Input{
		Query: "unique keyword xyz",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	// Without VOYAGE_API_KEY, should fall back to BM25
	// Either "bm25" or "vector" is valid - we just want success
	method := stats["search_method"].(string)
	assert.True(t, method == "bm25" || method == "vector")
}

func TestMemoryQuery_FilterMethod(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:      "filtered-mem",
		Type:      "decision",
		Summary:   "A decision",
		SessionID: "session-xyz",
	})

	// Filter-only path (no query or file, just session_id and kinds)
	in := Input{
		Kinds:     "decision",
		SessionID: "session-xyz",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	assert.Equal(t, "filter", stats["search_method"])
}

// Tests for file association

func TestMemoryQuery_FileAssociatedInResult(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "semantic_fact-with-file",
		Type:    "semantic_fact",
		Summary: "Semantic fact about a specific file",
		Result:  map[string]any{"file": "internal/auth/handler.go"},
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "semantic_fact-other-file",
		Type:    "semantic_fact",
		Summary: "Semantic fact about another file",
		Result:  map[string]any{"file": "internal/api/router.go"},
	})

	in := Input{
		File: "auth/handler.go",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
}

func TestMemoryQuery_FileAssociatedInSummary(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "semantic_fact-summary-file",
		Type:    "semantic_fact",
		Summary: "Bug in internal/auth/handler.go causes token expiry",
	})

	in := Input{
		File: "auth/handler.go",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
}

// Tests for content inclusion

func TestMemoryQuery_IncludeContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "rich-record",
		Type:    "semantic_fact",
		Summary: "Record with rich content about authentication",
		Result: map[string]any{
			"details": "This is the full content",
			"code":    "func main() {}",
		},
	})

	// Use query path (not filter-only) since IncludeContent is implemented there
	in := Input{
		Query:          "authentication",
		IncludeContent: true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.GreaterOrEqual(t, len(records), 1)

	// Find the record we seeded
	var found bool
	for _, m := range records {
		mem := m.(map[string]any)
		if mem["source_id"] == "rich-record" {
			found = true
			require.NotNil(t, mem["content"], "content should be included when IncludeContent=true")
			content := map[string]any{}
			require.NoError(t, json.Unmarshal([]byte(mem["content"].(string)), &content))
			assert.Equal(t, "This is the full content", content["details"])
			assert.Equal(t, "func main() {}", content["code"])
			break
		}
	}
	assert.True(t, found, "should find the seeded record")
}

func TestMemoryQuery_ExcludeContentByDefault(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "record-with-result",
		Type:    "semantic_fact",
		Summary: "Has result data",
		Result:  map[string]any{"secret": "should-not-appear"},
	})

	in := Input{
		Kinds: "semantic_fact",
		// IncludeContent is false by default
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)

	mem := records[0].(map[string]any)
	assert.Nil(t, mem["content"])
}

// Tests for stats

func TestMemoryQuery_StatsIncluded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Type: "semantic_fact", Summary: "Test"})

	in := Input{
		Kinds:     "semantic_fact",
		SessionID: "test-session",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	assert.NotNil(t, stats["total_found"])
	assert.NotNil(t, stats["filtered"])
	assert.NotNil(t, stats["search_method"])
	assert.NotNil(t, stats["latency_ms"])
	assert.Equal(t, "semantic_fact", stats["kinds_filter"])
	assert.Equal(t, "test-session", stats["session_id_filter"])
}

// Tests for workspace scoping

func TestMemoryQuery_WorkspaceScoped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Record in workspace
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "workspace-record",
		Type:    "semantic_fact",
		Summary: "Record in workspace",
	})
	// Record in different workspace
	seedMemory(t, store, "/other/workspace", &seedOpts{
		Name:    "other-record",
		Type:    "semantic_fact",
		Summary: "Record in other workspace",
	})

	in := Input{
		Kinds: "semantic_fact",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 1)
	mem := records[0].(map[string]any)
	assert.Equal(t, "workspace-record", mem["source_id"])
}

// Tests for timestamps

func TestMemoryQuery_TimestampsFormatted(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "timestamped-record",
		Type:    "semantic_fact",
		Summary: "Record with timestamps",
	})

	in := Input{
		Kinds: "semantic_fact",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	require.Len(t, records, 1)

	mem := records[0].(map[string]any)
	temporal := mem["temporal"].(map[string]any)
	createdAt := temporal["observed_at"].(string)
	// Should be RFC3339 formatted
	_, err = time.Parse(time.RFC3339, createdAt)
	assert.NoError(t, err)
}

// Tests for empty results

func TestMemoryQuery_EmptyResults(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		Kinds: "adapter_example",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	records := data["records"].([]any)
	assert.Len(t, records, 0)

	stats := data["stats"].(map[string]any)
	assert.NotEmpty(t, stats["hint"]) // Should have a hint for no results
}

func TestMemoryQuery_EmptyQueryResults(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "existing-record",
		Type:    "semantic_fact",
		Summary: "Existing record",
	})

	in := Input{
		Query: "completely-nonexistent-term-xyz123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	// Should have a hint when no matching records
	if len(data["records"].([]any)) == 0 {
		assert.NotEmpty(t, stats["hint"])
	}
}

// Tests for MinSimilarity

func TestMemoryQuery_MinSimilarityDefault(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Just verify defaults are applied - MinSimilarity = 0.3
	in := Input{
		Query: "test",
	}
	normalizeInput(&in, rc)
	assert.Equal(t, DefaultMinSimilarity, in.MinSimilarity)
}

func TestMemoryQuery_MinSimilarityCustom(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		Query:         "test",
		MinSimilarity: 0.8,
	}
	normalizeInput(&in, rc)
	assert.Equal(t, 0.8, in.MinSimilarity)
}

// Tests for file extraction helper

func TestExtractFileFromEntry_EditPrefix(t *testing.T) {
	entry := storage.NamedEntry{
		Name: "edit:internal/auth/handler.go:fix-bug",
	}
	file := extractFileFromEntry(entry)
	assert.Equal(t, "internal/auth/handler.go", file)
}

func TestExtractFileFromEntry_ResultField(t *testing.T) {
	resultJSON, _ := json.Marshal(map[string]any{
		"file": "internal/api/router.go",
	})
	entry := storage.NamedEntry{
		Name:   "some-record",
		Result: resultJSON,
	}
	file := extractFileFromEntry(entry)
	assert.Equal(t, "internal/api/router.go", file)
}

func TestExtractFileFromEntry_NoFile(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "some-record",
		Summary: "No file here",
	}
	file := extractFileFromEntry(entry)
	assert.Empty(t, file)
}

// Tests for isFileAssociated helper

func TestIsFileAssociated_InName(t *testing.T) {
	entry := storage.NamedEntry{
		Name: "edit:internal/auth/handler.go:fix",
	}
	assert.True(t, isFileAssociated(entry, "auth/handler.go"))
}

func TestIsFileAssociated_InSummary(t *testing.T) {
	entry := storage.NamedEntry{
		Summary: "Fixed bug in internal/auth/handler.go",
	}
	assert.True(t, isFileAssociated(entry, "auth/handler.go"))
}

func TestIsFileAssociated_InResult(t *testing.T) {
	resultJSON, _ := json.Marshal(map[string]any{
		"path": "internal/auth/handler.go",
	})
	entry := storage.NamedEntry{
		Result: resultJSON,
	}
	assert.True(t, isFileAssociated(entry, "auth/handler.go"))
}

func TestIsFileAssociated_NoMatch(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "some-entry",
		Summary: "No file reference here",
	}
	assert.False(t, isFileAssociated(entry, "auth/handler.go"))
}

// Tests for normalizePath helper

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"./internal/auth.go", "internal/auth.go"},
		{"/internal/auth.go", "internal/auth.go"},
		{"internal/auth.go", "internal/auth.go"},
		{"./", ""},
		{"/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizePath(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
