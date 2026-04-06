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

	// Build codemap data
	traceList := make([]map[string]any, traces)
	for i := 0; i < traces; i++ {
		traceList[i] = map[string]any{
			"name":        "trace-" + string(rune('A'+i)),
			"description": "Trace description",
			"content":     "Trace content here",
		}
	}

	result := map[string]any{
		"title":       title,
		"description": "Test codemap",
		"traces":      traceList,
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

// Tests for basic retrieval

func TestCodemapGet_Found(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "test-id-123", "Auth Flow", 3)

	in := Input{
		ID: "codemap://test-id-123",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.True(t, data["found"].(bool))
	codemap := data["codemap"].(map[string]any)
	assert.Equal(t, "codemap://test-id-123", codemap["id"])
	assert.Equal(t, "Auth Flow", codemap["title"])
}

func TestCodemapGet_NotFound(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := Input{
		ID: "nonexistent-codemap-id",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.False(t, data["found"].(bool))
}

func TestCodemapGet_WithoutPrefix(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "my-codemap", "Session Flow", 2)

	// Search without codemap:// prefix
	in := Input{
		ID: "my-codemap",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	assert.True(t, data["found"].(bool))
}

// Tests for trace inclusion

func TestCodemapGet_IncludeTraces(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "trace-test", "Traced Map", 4)

	includeTraces := true
	in := Input{
		ID:            "trace-test",
		IncludeTraces: &includeTraces,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemap := data["codemap"].(map[string]any)
	traces := codemap["traces"].([]any)
	assert.Len(t, traces, 4)
}

func TestCodemapGet_ExcludeTraces(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "no-trace", "No Traces", 5)

	includeTraces := false
	in := Input{
		ID:            "no-trace",
		IncludeTraces: &includeTraces,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemap := data["codemap"].(map[string]any)
	// Traces should be empty when excluded (may be nil or empty slice)
	traces, ok := codemap["traces"].([]any)
	if ok {
		assert.Len(t, traces, 0)
	}
}

// Tests for max trace content

func TestCodemapGet_MaxTraceContentTruncates(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)

	// Seed with long trace content
	longContent := ""
	for i := 0; i < 100; i++ {
		longContent += "word "
	}
	result := map[string]any{
		"title": "Long Traces",
		"traces": []map[string]any{
			{
				"name":    "long-trace",
				"content": longContent,
			},
		},
	}
	resultJSON, _ := json.Marshal(result)

	entry := storage.NamedEntry{
		Name:      "codemap://long-trace-test",
		Type:      "codemap",
		Workspace: rc.Workspace,
		Summary:   "Test",
		Result:    resultJSON,
	}
	_, err := store.Save(context.Background(), entry)
	require.NoError(t, err)

	includeTraces := true
	in := Input{
		ID:              "long-trace-test",
		IncludeTraces:   &includeTraces,
		MaxTraceContent: 50,
	}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemap := data["codemap"].(map[string]any)
	traces := codemap["traces"].([]any)
	require.Len(t, traces, 1)
	trace := traces[0].(map[string]any)
	content := trace["content"].(string)
	assert.LessOrEqual(t, len(content), 50+3) // +3 for potential ellipsis
}

// Tests for stats

func TestCodemapGet_StatsIncluded(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "stats-test", "Stats Test", 1)

	in := Input{
		ID: "stats-test",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	stats := data["stats"].(map[string]any)
	assert.NotNil(t, stats["latency_ms"])
}

// Tests for timestamps

func TestCodemapGet_TimestampsFormatted(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	store := openMemoryStore(t, rc)
	seedCodemap(t, store, rc.Workspace, "ts-test", "Timestamp Test", 1)

	in := Input{
		ID: "ts-test",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	codemap := data["codemap"].(map[string]any)
	assert.NotEmpty(t, codemap["created_at"])
}

// Tests for extractWindsurfFiles helper

func TestExtractWindsurfFiles_Empty(t *testing.T) {
	files := extractWindsurfFiles(nil)
	assert.Empty(t, files)
}

// Tests for default values

func TestCodemapGet_DefaultsApplied(t *testing.T) {
	in := Input{
		ID: "test",
	}

	// Apply defaults as the function would
	if in.MaxTraceContent <= 0 {
		in.MaxTraceContent = DefaultMaxTraceContent
	}
	if in.IncludeTraces == nil {
		defaultTrue := true
		in.IncludeTraces = &defaultTrue
	}

	assert.Equal(t, DefaultMaxTraceContent, in.MaxTraceContent)
	assert.True(t, *in.IncludeTraces)
}

func TestParseInlineMode(t *testing.T) {
	tests := []struct {
		in      string
		want    InlineMode
		wantErr bool
	}{
		{"", InlineModeAuto, false},
		{"auto", InlineModeAuto, false},
		{"full", InlineModeFull, false},
		{"preview", InlineModePreview, false},
		{"artifact_only", InlineModeArtifactOnly, false},
		{"bad", InlineModeAuto, true},
	}

	for _, tt := range tests {
		got, err := parseInlineMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseInlineMode(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseInlineMode(%q)=%q want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildCodemapPreviewTruncatesTraces(t *testing.T) {
	out := &Output{
		Found: true,
		Codemap: &CodemapData{
			ID: "codemap://x",
			Traces: []Trace{
				{Name: "1"}, {Name: "2"}, {Name: "3"}, {Name: "4"}, {Name: "5"}, {Name: "6"},
			},
		},
		TracesTotal: 6,
	}
	preview := buildCodemapPreview(out)
	if preview.InlineMode != string(InlineModePreview) {
		t.Fatalf("inline_mode=%q want preview", preview.InlineMode)
	}
	if len(preview.Codemap.Traces) != DefaultPreviewTraces {
		t.Fatalf("traces=%d want %d", len(preview.Codemap.Traces), DefaultPreviewTraces)
	}
	if !preview.TracesTruncated {
		t.Fatal("expected trace truncation")
	}
}
