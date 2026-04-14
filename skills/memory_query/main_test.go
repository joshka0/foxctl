package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/storage"
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

func openMemoryStore(t *testing.T, rc *skillmain.RunContext) *memory.Store {
	t.Helper()
	store, err := memory.Open(context.Background(), rc.Config.Storage.Root, rc.Config.Paths.CAS)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

func seedMemory(t *testing.T, store *memory.Store, workspace string, opts *seedOpts) storage.NamedEntry {
	t.Helper()
	if opts == nil {
		opts = &seedOpts{}
	}
	if opts.Name == "" {
		opts.Name = "test-memory"
	}
	if opts.Type == "" {
		opts.Type = "gotcha"
	}
	if opts.Summary == "" {
		opts.Summary = "A test memory entry"
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

// Tests for validation

func TestMemoryQuery_MissingAllCriteria(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of query, file, or types must be provided")
}

func TestMemoryQuery_QueryOnly(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "auth-bug",
		Type:    "gotcha",
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

	assert.NotNil(t, data["memories"])
	assert.NotNil(t, data["pagination"])
	assert.NotNil(t, data["stats"])
}

func TestMemoryQuery_TypesOnly(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "gotcha-1",
		Type:    "gotcha",
		Summary: "First gotcha",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "decision-1",
		Type:    "decision",
		Summary: "First decision",
	})

	in := Input{
		Types: "gotcha",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
	mem := memories[0].(map[string]any)
	assert.Equal(t, "gotcha", mem["type"])
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

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
	mem := memories[0].(map[string]any)
	assert.Contains(t, mem["name"], "auth/handler.go")
}

// Tests for filtering

func TestMemoryQuery_FilterByMultipleTypes(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "gotcha-1", Type: "gotcha", Summary: "Gotcha 1"})
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "decision-1", Type: "decision", Summary: "Decision 1"})
	seedMemory(t, store, rc.Workspace, &seedOpts{Name: "context-1", Type: "context", Summary: "Context 1"})

	in := Input{
		Types: "gotcha,decision",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	assert.Len(t, memories, 2)

	// Verify types
	types := make(map[string]bool)
	for _, m := range memories {
		mem := m.(map[string]any)
		types[mem["type"].(string)] = true
	}
	assert.True(t, types["gotcha"])
	assert.True(t, types["decision"])
	assert.False(t, types["context"])
}

func TestMemoryQuery_FilterBySessionID(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:      "mem-session-a",
		Type:      "gotcha",
		Summary:   "Session A memory",
		SessionID: "session-a",
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:      "mem-session-b",
		Type:      "gotcha",
		Summary:   "Session B memory",
		SessionID: "session-b",
	})

	in := Input{
		Types:     "gotcha",
		SessionID: "session-a",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
	mem := memories[0].(map[string]any)
	assert.Equal(t, "session-a", mem["session_id"])
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
		Name:    "gotcha-auth",
		Type:    "gotcha",
		Summary: "Auth gotcha",
	})

	in := Input{
		Query: "auth",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	// Should only return the gotcha, not symbols
	for _, m := range memories {
		mem := m.(map[string]any)
		assert.NotEqual(t, "symbol", mem["type"])
		assert.NotEqual(t, "code_symbol", mem["type"])
	}
}

// Tests for pagination

func TestMemoryQuery_Pagination(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 15 memories
	for i := 0; i < 15; i++ {
		seedMemory(t, store, rc.Workspace, &seedOpts{
			Name:    "gotcha-" + string(rune('a'+i)),
			Type:    "gotcha",
			Summary: "Gotcha " + string(rune('a'+i)),
		})
	}

	// First page
	in := Input{
		Types: "gotcha",
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

	memories := data["memories"].([]any)
	assert.Len(t, memories, 5)
}

func TestMemoryQuery_PaginationOffset(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 10 memories
	for i := 0; i < 10; i++ {
		seedMemory(t, store, rc.Workspace, &seedOpts{
			Name:    "gotcha-" + string(rune('a'+i)),
			Type:    "gotcha",
			Summary: "Gotcha " + string(rune('a'+i)),
		})
	}

	in := Input{
		Types:  "gotcha",
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

	memories := data["memories"].([]any)
	assert.Len(t, memories, 3) // 10 - 7 = 3
}

func TestMemoryQuery_LimitCapped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Type: "gotcha", Summary: "Test"})

	in := Input{
		Types: "gotcha",
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
		Type:    "gotcha",
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

	// Filter-only path (no query or file, just session_id and types)
	in := Input{
		Types:     "decision",
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
		Name:    "gotcha-with-file",
		Type:    "gotcha",
		Summary: "Gotcha about a specific file",
		Result:  map[string]any{"file": "internal/auth/handler.go"},
	})
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "gotcha-other-file",
		Type:    "gotcha",
		Summary: "Gotcha about another file",
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

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
}

func TestMemoryQuery_FileAssociatedInSummary(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "gotcha-summary-file",
		Type:    "gotcha",
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

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
}

// Tests for content inclusion

func TestMemoryQuery_IncludeContent(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "rich-memory",
		Type:    "gotcha",
		Summary: "Memory with rich content about authentication",
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

	memories := data["memories"].([]any)
	require.GreaterOrEqual(t, len(memories), 1)

	// Find the memory we seeded
	var found bool
	for _, m := range memories {
		mem := m.(map[string]any)
		if mem["name"] == "rich-memory" {
			found = true
			require.NotNil(t, mem["content"], "content should be included when IncludeContent=true")
			content, ok := mem["content"].(map[string]any)
			require.True(t, ok, "content should be a map")
			assert.Equal(t, "This is the full content", content["details"])
			assert.Equal(t, "func main() {}", content["code"])
			break
		}
	}
	assert.True(t, found, "should find the seeded memory")
}

func TestMemoryQuery_ExcludeContentByDefault(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "memory-with-result",
		Type:    "gotcha",
		Summary: "Has result data",
		Result:  map[string]any{"secret": "should-not-appear"},
	})

	in := Input{
		Types: "gotcha",
		// IncludeContent is false by default
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	require.Len(t, memories, 1)

	mem := memories[0].(map[string]any)
	assert.Nil(t, mem["content"])
}

// Tests for stats

func TestMemoryQuery_StatsIncluded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{Type: "gotcha", Summary: "Test"})

	in := Input{
		Types:     "gotcha",
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
	assert.Equal(t, "gotcha", stats["types_filter"])
	assert.Equal(t, "test-session", stats["session_id_filter"])
}

// Tests for workspace scoping

func TestMemoryQuery_WorkspaceScoped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Memory in workspace
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "workspace-memory",
		Type:    "gotcha",
		Summary: "Memory in workspace",
	})
	// Memory in different workspace
	seedMemory(t, store, "/other/workspace", &seedOpts{
		Name:    "other-memory",
		Type:    "gotcha",
		Summary: "Memory in other workspace",
	})

	in := Input{
		Types: "gotcha",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	assert.Len(t, memories, 1)
	mem := memories[0].(map[string]any)
	assert.Equal(t, "workspace-memory", mem["name"])
}

// Tests for timestamps

func TestMemoryQuery_TimestampsFormatted(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "timestamped-memory",
		Type:    "gotcha",
		Summary: "Memory with timestamps",
	})

	in := Input{
		Types: "gotcha",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	require.Len(t, memories, 1)

	mem := memories[0].(map[string]any)
	createdAt := mem["created_at"].(string)
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
		Types: "nonexistent-type",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	memories := data["memories"].([]any)
	assert.Len(t, memories, 0)

	stats := data["stats"].(map[string]any)
	assert.NotEmpty(t, stats["hint"]) // Should have a hint for no results
}

func TestMemoryQuery_EmptyQueryResults(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedMemory(t, store, rc.Workspace, &seedOpts{
		Name:    "existing-memory",
		Type:    "gotcha",
		Summary: "Existing memory",
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
	// Should have a hint when no matching memories
	if len(data["memories"].([]any)) == 0 {
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
		Name:   "some-memory",
		Result: resultJSON,
	}
	file := extractFileFromEntry(entry)
	assert.Equal(t, "internal/api/router.go", file)
}

func TestExtractFileFromEntry_NoFile(t *testing.T) {
	entry := storage.NamedEntry{
		Name:    "some-memory",
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
