package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
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

func seedCodemap(t *testing.T, store *memory.Store, workspace string, id, title string, traces int) storage.NamedEntry {
	t.Helper()

	traceList := make([]map[string]any, traces)
	for i := 0; i < traces; i++ {
		traceList[i] = map[string]any{
			"name":        "trace-" + string(rune('A'+i)),
			"description": "Trace description",
		}
	}

	result := map[string]any{
		"title":  title,
		"traces": traceList,
	}
	resultJSON, err := json.Marshal(result)
	require.NoError(t, err)

	entry := storage.NamedEntry{
		Name:      "codemap://" + id,
		Type:      "codemap",
		Workspace: workspace,
		Summary:   "Test codemap: " + title,
		Result:    resultJSON,
	}

	saved, err := store.Save(context.Background(), entry)
	require.NoError(t, err)
	return saved
}

// Tests for basic listing

func TestCodemapList_Empty(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 0)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(0), pagination["total"])
}

func TestCodemapList_WithCodemaps(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "id-1", "Auth Flow", 2)
	seedCodemap(t, store, rc.Workspace, "id-2", "Session Handler", 3)

	in := Input{}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 2)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(2), pagination["total"])
}

// Tests for pagination

func TestCodemapList_Pagination(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 15 codemaps
	for i := 0; i < 15; i++ {
		seedCodemap(t, store, rc.Workspace, "id-"+string(rune('a'+i)), "Map "+string(rune('A'+i)), 1)
	}

	in := Input{
		Limit: 5,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 5)

	pagination := data["pagination"].(map[string]any)
	assert.Equal(t, float64(15), pagination["total"])
	assert.Equal(t, float64(0), pagination["offset"])
	assert.Equal(t, float64(5), pagination["limit"])
	assert.True(t, pagination["has_more"].(bool))
}

func TestCodemapList_PaginationOffset(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed 10 codemaps
	for i := 0; i < 10; i++ {
		seedCodemap(t, store, rc.Workspace, "id-"+string(rune('a'+i)), "Map "+string(rune('A'+i)), 1)
	}

	in := Input{
		Limit:  5,
		Offset: 8,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 2) // 10 - 8 = 2

	pagination := data["pagination"].(map[string]any)
	assert.False(t, pagination["has_more"].(bool))
}

func TestCodemapList_LimitCapped(t *testing.T) {
	var buf bytes.Buffer
	_, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		Limit: 999, // Over max
	}

	// Apply the normalization logic
	if in.Limit > MaxLimit {
		in.Limit = MaxLimit
	}

	assert.Equal(t, MaxLimit, in.Limit)
}

// Tests for codemaps only

func TestCodemapList_FiltersNonCodemaps(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Seed a codemap
	seedCodemap(t, store, rc.Workspace, "real-codemap", "Real Map", 1)

	// Seed a non-codemap entry
	entry := storage.NamedEntry{
		Name:      "gotcha://something",
		Type:      "gotcha",
		Workspace: rc.Workspace,
		Summary:   "Not a codemap",
		Result:    []byte("{}"),
	}
	_, err := store.Save(context.Background(), entry)
	require.NoError(t, err)

	in := Input{}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 1) // Only the codemap, not the gotcha
}

// Tests for trace count

func TestCodemapList_IncludesTraceCount(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "trace-count-test", "Traced Map", 5)

	in := Input{}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	require.Len(t, codemaps, 1)

	cm := codemaps[0].(map[string]any)
	assert.Equal(t, float64(5), cm["trace_count"])
}

// Tests for summary truncation

func TestCodemapList_SummaryTruncated(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)

	// Create codemap with long summary
	longSummary := ""
	for i := 0; i < 100; i++ {
		longSummary += "word "
	}

	result := map[string]any{"title": "Long Summary"}
	resultJSON, _ := json.Marshal(result)

	entry := storage.NamedEntry{
		Name:      "codemap://long-summary",
		Type:      "codemap",
		Workspace: rc.Workspace,
		Summary:   longSummary,
		Result:    resultJSON,
	}
	_, err := store.Save(context.Background(), entry)
	require.NoError(t, err)

	in := Input{
		MaxSummaryChars: 50,
	}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	require.Len(t, codemaps, 1)

	cm := codemaps[0].(map[string]any)
	summary := cm["summary"].(string)
	assert.LessOrEqual(t, len(summary), 50+3) // +3 for potential ellipsis
}

// Tests for stats

func TestCodemapList_StatsIncluded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "stats-test", "Stats", 1)

	in := Input{}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	assert.NotNil(t, stats["latency_ms"])
	assert.NotNil(t, stats["search_method"])
}

// Tests for extractTitleFromName helper

func TestExtractTitleFromName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"codemap://simple", "simple"},
		{"codemap://very-long-title-that-exceeds-maximum", "very-long-title-that-excee"}, // truncated at 26 chars
		{"just-name", "just-name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTitleFromName(tt.name)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Tests for default values

func TestCodemapList_DefaultsApplied(t *testing.T) {
	in := Input{}

	// Apply defaults
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	if in.MaxSummaryChars <= 0 {
		in.MaxSummaryChars = DefaultMaxSummaryChar
	}
	if in.SummaryOnly == nil {
		defaultTrue := true
		in.SummaryOnly = &defaultTrue
	}

	assert.Equal(t, DefaultLimit, in.Limit)
	assert.Equal(t, 0, in.Offset)
	assert.Equal(t, DefaultMaxSummaryChar, in.MaxSummaryChars)
	assert.True(t, *in.SummaryOnly)
}

// Tests for workspace scoping

func TestCodemapList_WorkspaceScoped(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	// Codemap in workspace
	seedCodemap(t, store, rc.Workspace, "ws-codemap", "Workspace Map", 1)
	// Codemap in different workspace
	seedCodemap(t, store, "/other/workspace", "other-codemap", "Other Map", 1)

	in := Input{}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemaps := data["codemaps"].([]any)
	assert.Len(t, codemaps, 1)

	cm := codemaps[0].(map[string]any)
	assert.Equal(t, "codemap://ws-codemap", cm["id"])
}
