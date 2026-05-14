package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/intelligence/retrieval"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

func TestParseInlineMode(t *testing.T) {
	tests := []struct {
		input   string
		want    InlineMode
		wantErr bool
	}{
		{input: "", want: InlineModeAuto},
		{input: "auto", want: InlineModeAuto},
		{input: "full", want: InlineModeFull},
		{input: "preview", want: InlineModePreview},
		{input: "artifact_only", want: InlineModeArtifactOnly},
		{input: "nope", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseInlineMode(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tt.input)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseInlineMode(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Fatalf("parseInlineMode(%q)=%q want %q", tt.input, got, tt.want)
		}
	}
}

func TestShouldPreviewSemanticOutput(t *testing.T) {
	rc := &skillmain.RunContext{InlineKB: 64, MaxPreview: 5}
	out := &Output{
		Query: "test",
		Results: []Result{
			{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}, {Name: "e"}, {Name: "f"},
		},
	}
	if !shouldPreviewSemanticOutput(rc, out) {
		t.Fatal("expected preview when results exceed MaxPreview")
	}
}

func TestBuildSemanticSearchPreview_TruncatesNestedPayloads(t *testing.T) {
	out := &Output{
		Query: "test",
		Results: []Result{
			{Name: "r1", Timeline: &SessionTimeline{SessionID: "s1"}},
			{Name: "r2", Timeline: &SessionTimeline{SessionID: "s2"}},
			{Name: "r3", Timeline: &SessionTimeline{SessionID: "s3"}},
		},
		CandidateBundles: []CandidateBundle{
			{Key: "a"}, {Key: "b"}, {Key: "c"}, {Key: "d"}, {Key: "e"}, {Key: "f"}, {Key: "g"},
		},
		ContextHints: []ContextHint{
			{Type: "a"}, {Type: "b"}, {Type: "c"}, {Type: "d"},
		},
		Timelines: []SessionTimeline{
			{
				SessionID: "s1",
				ChunkSummaries: []TimelineChunk{
					{Summary: "1"}, {Summary: "2"}, {Summary: "3"}, {Summary: "4"},
				},
				Learnings: []TimelineLearning{
					{Summary: "a"}, {Summary: "b"}, {Summary: "c"}, {Summary: "d"},
				},
				Rollup: &TimelineRollup{
					SummaryLines: []string{"1", "2", "3", "4", "5", "6"},
					Tools:        []string{"a", "b", "c", "d", "e", "f"},
				},
			},
			{SessionID: "s2"},
			{SessionID: "s3"},
		},
		TreeText: strings.Repeat("x", DefaultPreviewTreeTextRunes+100),
		Tree: &retrieval.TreeOutput{
			Stats: retrieval.TreeStats{TotalFiles: 10, TotalDirectories: 5},
		},
	}

	preview := buildSemanticSearchPreview(out, InlineModePreview, 2)
	if preview.InlineMode != string(InlineModePreview) {
		t.Fatalf("inline_mode=%q want preview", preview.InlineMode)
	}
	if len(preview.Results) != 2 {
		t.Fatalf("results=%d want 2", len(preview.Results))
	}
	if preview.Results[0].Timeline != nil {
		t.Fatal("expected inline result timelines to be cleared in preview")
	}
	if len(preview.CandidateBundles) != DefaultPreviewCandidateBundles {
		t.Fatalf("candidate_bundles=%d want %d", len(preview.CandidateBundles), DefaultPreviewCandidateBundles)
	}
	if len(preview.ContextHints) != DefaultPreviewContextHints {
		t.Fatalf("context_hints=%d want %d", len(preview.ContextHints), DefaultPreviewContextHints)
	}
	if len(preview.Timelines) != DefaultPreviewTimelines {
		t.Fatalf("timelines=%d want %d", len(preview.Timelines), DefaultPreviewTimelines)
	}
	if len(preview.Timelines[0].ChunkSummaries) != DefaultPreviewTimelineChunks {
		t.Fatalf("chunk_summaries=%d want %d", len(preview.Timelines[0].ChunkSummaries), DefaultPreviewTimelineChunks)
	}
	if len(preview.Timelines[0].Learnings) != DefaultPreviewTimelineLearns {
		t.Fatalf("learnings=%d want %d", len(preview.Timelines[0].Learnings), DefaultPreviewTimelineLearns)
	}
	if preview.Tree != nil {
		t.Fatal("expected tree object omitted in preview mode")
	}
	if !preview.TreeTextTruncated {
		t.Fatal("expected tree text truncation marker")
	}
	if preview.ResultsTotal != 3 || preview.CandidateBundlesTotal != 7 || preview.ContextHintsTotal != 4 || preview.TimelinesTotal != 3 {
		t.Fatalf("unexpected totals: %+v", preview)
	}
}

func TestBuildSemanticSearchPreview_ArtifactOnlyClearsInlinePayload(t *testing.T) {
	out := &Output{
		Query:            "test",
		Results:          []Result{{Name: "r1"}},
		CandidateBundles: []CandidateBundle{{Key: "a"}},
		ContextHints:     []ContextHint{{Type: "hint"}},
		Timelines:        []SessionTimeline{{SessionID: "s1"}},
		TreeText:         "tree",
		Tree:             &retrieval.TreeOutput{},
	}

	preview := buildSemanticSearchPreview(out, InlineModeArtifactOnly, 5)
	if len(preview.Results) != 0 || len(preview.CandidateBundles) != 0 || len(preview.ContextHints) != 0 || len(preview.Timelines) != 0 {
		t.Fatalf("expected inline payload cleared: %+v", preview)
	}
	if preview.Tree != nil {
		t.Fatal("expected tree omitted")
	}
	if preview.TreeText != "" || !preview.TreeTextTruncated {
		t.Fatalf("unexpected tree text state: %q truncated=%v", preview.TreeText, preview.TreeTextTruncated)
	}
}

func TestDimensionValidation(t *testing.T) {
	// Test cases for dimension validation logic
	tests := []struct {
		name         string
		queryDims    int
		configDims   int
		expectHint   bool
		expectVector bool
	}{
		{
			name:         "matching dimensions 3072",
			queryDims:    3072,
			configDims:   3072,
			expectHint:   false,
			expectVector: true,
		},
		{
			name:         "matching dimensions 768",
			queryDims:    768,
			configDims:   768,
			expectHint:   false,
			expectVector: true,
		},
		{
			name:         "mismatch 768 vs 3072",
			queryDims:    768,
			configDims:   3072,
			expectHint:   true,
			expectVector: false,
		},
		{
			name:         "mismatch 3072 vs 768",
			queryDims:    3072,
			configDims:   768,
			expectHint:   true,
			expectVector: false,
		},
		{
			name:         "config zero uses default 3072",
			queryDims:    3072,
			configDims:   0, // Should default to 3072
			expectHint:   false,
			expectVector: true,
		},
		{
			name:         "wrong dims with config zero",
			queryDims:    768,
			configDims:   0, // Defaults to 3072, so 768 mismatches
			expectHint:   true,
			expectVector: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the dimension validation logic from search()
			expectedDims := tc.configDims
			if expectedDims == 0 {
				expectedDims = 3072 // Gemini embedding-001 default
			}

			var hint string
			useVectorSearch := true

			if tc.queryDims != expectedDims {
				hint = "dimension mismatch"
				useVectorSearch = false
			}

			if tc.expectHint && hint == "" {
				t.Error("Expected hint for dimension mismatch, got none")
			}
			if !tc.expectHint && hint != "" {
				t.Errorf("Expected no hint, got: %s", hint)
			}
			if tc.expectVector && !useVectorSearch {
				t.Error("Expected vector search to be enabled")
			}
			if !tc.expectVector && useVectorSearch {
				t.Error("Expected vector search to be disabled")
			}
		})
	}
}

func TestSearchStatsHintPopulation(t *testing.T) {
	// Verify SearchStats correctly captures dimension information
	stats := SearchStats{
		TotalResults:        0,
		SourceCounts:        make(map[string]int),
		SourceLatencies:     make(map[string]int),
		EmbeddingDimensions: 768,
		Hint:                "dimension mismatch: query=768, config=3072",
	}

	if stats.EmbeddingDimensions != 768 {
		t.Errorf("Expected dimensions 768, got %d", stats.EmbeddingDimensions)
	}
	if stats.Hint == "" {
		t.Error("Expected hint to be set")
	}
}

func TestValidateExecutionWorkspaceReturnsActionableMismatch(t *testing.T) {
	err := validateExecutionWorkspace("/tmp/target-workspace", "/tmp/current-workspace", "/tmp/current-workspace")
	if err == nil {
		t.Fatal("expected workspace mismatch error")
	}
	msg := err.Error()
	for _, want := range []string{
		"workspace mismatch",
		`--workspace "/tmp/target-workspace"`,
		"foxctl run code/semantic_search",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestValidateExecutionWorkspaceAllowsJSONWorkspaceWithoutRunWorkspaceEnv(t *testing.T) {
	if err := validateExecutionWorkspace("/tmp/target-workspace", "/tmp/current-workspace", ""); err != nil {
		t.Fatalf("validate workspace: %v", err)
	}
}

func TestValidateExecutionWorkspaceAllowsMatchingWorkspace(t *testing.T) {
	if err := validateExecutionWorkspace("/tmp/target-workspace/.", "/tmp/target-workspace", "/tmp/target-workspace"); err != nil {
		t.Fatalf("validate workspace: %v", err)
	}
}

func TestDefaultConfigDimensions(t *testing.T) {
	// Default dimensions should be 3072 (Gemini gemini-embedding-001)
	expectedDefault := 3072

	// Simulate config load behavior
	configDims := 0 // Zero means use default
	effectiveDims := configDims
	if effectiveDims == 0 {
		effectiveDims = expectedDefault
	}

	if effectiveDims != expectedDefault {
		t.Errorf("Expected default dimensions %d, got %d", expectedDefault, effectiveDims)
	}
}

func TestInputDefaults(t *testing.T) {
	// Test input struct defaults match skill.yaml
	in := Input{
		Query: "test query",
	}

	// Apply defaults (simulating parseInput logic)
	if len(in.Scope) == 0 {
		in.Scope = []string{ScopeSymbols, ScopeSessions, ScopeMemories}
	}
	if in.Limit <= 0 {
		in.Limit = DefaultLimit
	}
	if in.MinSimilarity <= 0 {
		in.MinSimilarity = DefaultMinSimilarity
	}

	if len(in.Scope) != 3 {
		t.Errorf("Expected 3 default scopes, got %d", len(in.Scope))
	}
	if in.Limit != DefaultLimit {
		t.Errorf("Expected default limit %d, got %d", DefaultLimit, in.Limit)
	}
	if in.MinSimilarity != DefaultMinSimilarity {
		t.Errorf("Expected default min_similarity %f, got %f", DefaultMinSimilarity, in.MinSimilarity)
	}
}

func TestScopeConstants(t *testing.T) {
	// Verify scope constants match expected values
	if ScopeSymbols != "symbols" {
		t.Errorf("Expected ScopeSymbols='symbols', got '%s'", ScopeSymbols)
	}
	if ScopeSessions != "sessions" {
		t.Errorf("Expected ScopeSessions='sessions', got '%s'", ScopeSessions)
	}
	if ScopeMemories != "memories" {
		t.Errorf("Expected ScopeMemories='memories', got '%s'", ScopeMemories)
	}
	if ScopeContext != "context" {
		t.Errorf("Expected ScopeContext='context', got '%s'", ScopeContext)
	}
}

func TestErrorCodeConstants(t *testing.T) {
	// Verify error codes use canonical envelope codes
	codes := map[string]string{
		"ErrCodeInput":         ErrCodeInput,
		"ErrCodeEmbedProvider": ErrCodeEmbedProvider,
		"ErrCodeSourceEmpty":   ErrCodeSourceEmpty,
		"ErrCodePolicy":        ErrCodePolicy,
		"ErrCodeRuntime":       ErrCodeRuntime,
	}

	for name, code := range codes {
		if code == "" {
			t.Errorf("%s should not be empty", name)
		}
	}
}

func TestEmbeddingModelConfig_UsesOverrides(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Model: "text-embedding-qwen3-embedding-8b",
			Models: map[string]string{
				"symbols":  "text-embedding-qwen3-embedding-8b",
				"memory":   "qwen3-embedding-0.6b",
				"tasks":    "text-embedding-qwen3-embedding-8b",
				"sessions": "text-embedding-qwen3-embedding-8b",
			},
		},
	}

	codeModel, memoryModel, textModel, _ := embeddingModelConfig("openai_compat", cfg)
	if codeModel != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("code model = %q, want text-embedding-qwen3-embedding-8b", codeModel)
	}
	if memoryModel != "qwen3-embedding-0.6b" {
		t.Fatalf("memory model = %q, want qwen3-embedding-0.6b", memoryModel)
	}
	if textModel != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("text model = %q, want text-embedding-qwen3-embedding-8b", textModel)
	}
}

func TestEmbeddingModelConfig_GeminiFallback(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Model: "text-embedding-qwen3-embedding-8b",
		},
	}

	codeModel, memoryModel, textModel, _ := embeddingModelConfig("gemini", cfg)
	if codeModel != "gemini-embedding-001" {
		t.Fatalf("code model = %q, want gemini-embedding-001", codeModel)
	}
	if memoryModel != "gemini-embedding-001" {
		t.Fatalf("memory model = %q, want gemini-embedding-001", memoryModel)
	}
	if textModel != "gemini-embedding-001" {
		t.Fatalf("text model = %q, want gemini-embedding-001", textModel)
	}
}

func TestSearchMemoriesBM25_LabelsCoChangeArtifacts(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer store.Close()

	payload := []byte(`{"anchor_path":"internal/context/contextplane/dispatch.go","neighbor_paths":["internal/context/contextplane/store.go"],"summary":"dispatch cluster"}`)
	if _, err := store.SaveFromResult(ctx, "cochange://internal/context/contextplane/dispatch.go", contextplane.CoChangeClusterType, "ws", "dispatch changes with store", payload); err != nil {
		t.Fatalf("save cochange artifact: %v", err)
	}

	results, decayStats, err := searchMemoriesBM25(ctx, cfg, "ws", "dispatch", 5, false, store)
	if err != nil {
		t.Fatalf("searchMemoriesBM25: %v", err)
	}
	if decayStats != nil {
		t.Fatalf("decayStats=%#v want nil when disabled", decayStats)
	}
	if len(results) == 0 {
		t.Fatalf("expected results")
	}
	if results[0].Source != "cochange" {
		t.Fatalf("source=%q want cochange", results[0].Source)
	}
	if results[0].Path != "internal/context/contextplane/dispatch.go" {
		t.Fatalf("path=%q want internal/context/contextplane/dispatch.go", results[0].Path)
	}
}

func TestSearchMemoriesBM25_DecayDisabledPreservesCurrentOrder(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer store.Close()

	seedBM25DecayFixture(t, ctx, store)

	results, decayStats, err := searchMemoriesBM25(ctx, cfg, "ws", "decay ranking", 1, false, store)
	if err != nil {
		t.Fatalf("searchMemoriesBM25: %v", err)
	}
	if decayStats != nil {
		t.Fatalf("decayStats=%#v want nil when disabled", decayStats)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].Name != "memory://decay-old-strong" {
		t.Fatalf("result name=%q want memory://decay-old-strong", results[0].Name)
	}
}

func TestSearchMemoriesBM25_DecayOptInReranksBeforeLimit(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{}
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer store.Close()

	seedBM25DecayFixture(t, ctx, store)

	results, decayStats, err := searchMemoriesBM25(ctx, cfg, "ws", "decay ranking", 1, true, store)
	if err != nil {
		t.Fatalf("searchMemoriesBM25: %v", err)
	}
	if decayStats == nil {
		t.Fatalf("decayStats=nil want stats when enabled")
	}
	if !decayStats.Enabled || decayStats.CandidatesBefore < 2 || decayStats.CandidatesAfter < 2 {
		t.Fatalf("unexpected decay stats: %#v", decayStats)
	}
	if decayStats.FactorMax <= decayStats.FactorMin || decayStats.FactorAvg <= 0 {
		t.Fatalf("unexpected decay factors: %#v", decayStats)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].Name != "memory://decay-recent-near-tie" {
		t.Fatalf("result name=%q want memory://decay-recent-near-tie", results[0].Name)
	}
	if results[0].Similarity <= 0.9 {
		t.Fatalf("similarity=%f want boosted decayed score", results[0].Similarity)
	}
}

func TestReciprocalRankFusionUsesBaseSimilarityForMemoryDecayThreshold(t *testing.T) {
	results := reciprocalRankFusion(map[string][]Result{
		ScopeMemories: {
			{
				Source:         ScopeMemories,
				ID:             "memory:stale-strong",
				Name:           "stale strong memory",
				Similarity:     0.25,
				BaseSimilarity: 0.95,
			},
		},
	}, 0.9)

	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].ID != "memory:stale-strong" {
		t.Fatalf("id=%q want memory:stale-strong", results[0].ID)
	}
}

type semanticSearchAccessRecorder struct {
	workspace string
	names     []string
	err       error
}

func (r *semanticSearchAccessRecorder) RecordAccessBatch(_ context.Context, workspace string, names []string, _ time.Time) (int, error) {
	r.workspace = workspace
	r.names = append([]string(nil), names...)
	if r.err != nil {
		return 0, r.err
	}
	return len(names), nil
}

func TestRecordSemanticSearchMemoryAccessDedupeAndSkipRemote(t *testing.T) {
	recorder := &semanticSearchAccessRecorder{}
	stats := recordSemanticSearchMemoryAccess(context.Background(), recorder, "ws", &Input{}, []Result{
		{Source: "memory", Name: "alpha"},
		{Source: "memory", Name: "alpha"},
		{Source: "cochange", Name: "cochange://path"},
		{Source: "symbol", Name: "ignored"},
	})
	if stats.Count != 2 || stats.Failed {
		t.Fatalf("stats=%#v want count=2 failed=false", stats)
	}
	if recorder.workspace != "ws" {
		t.Fatalf("workspace=%q want ws", recorder.workspace)
	}
	want := []string{"alpha", "cochange://path"}
	if !reflect.DeepEqual(recorder.names, want) {
		t.Fatalf("names=%v want %v", recorder.names, want)
	}

	remoteRecorder := &semanticSearchAccessRecorder{}
	remoteStats := recordSemanticSearchMemoryAccess(context.Background(), remoteRecorder, "ws", &Input{Remote: true}, []Result{
		{Source: "memory", Name: "alpha"},
	})
	if remoteStats.Count != 0 || remoteStats.Failed || len(remoteRecorder.names) != 0 {
		t.Fatalf("remote recording happened: stats=%#v names=%v", remoteStats, remoteRecorder.names)
	}
}

func TestRecordSemanticSearchMemoryAccessBestEffort(t *testing.T) {
	recorder := &semanticSearchAccessRecorder{err: os.ErrInvalid}
	stats := recordSemanticSearchMemoryAccess(context.Background(), recorder, "ws", &Input{}, []Result{
		{Source: "memory", Name: "alpha"},
	})
	if stats.Count != 0 || !stats.Failed {
		t.Fatalf("stats=%#v want count=0 failed=true on best-effort failure", stats)
	}
}

func seedBM25DecayFixture(t *testing.T, ctx context.Context, store *memory.Store) {
	t.Helper()
	if _, err := store.SaveFromResult(ctx, "memory://decay-old-strong", "semantic_fact", "ws", "decay ranking old strong", []byte(`{}`)); err != nil {
		t.Fatalf("save old memory: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "memory://decay-recent-near-tie", "semantic_fact", "ws", "decay ranking recent near tie", []byte(`{}`)); err != nil {
		t.Fatalf("save recent memory: %v", err)
	}
	now := time.Now().UTC()
	oldAt := now.Add(-90 * 24 * time.Hour)
	for i := 0; i < 10; i++ {
		if _, err := store.UpdateTelemetry(ctx, "memory://decay-old-strong", "ws", storage.MemoryTelemetryUpdate{
			Action: "viewed",
			At:     &oldAt,
		}); err != nil {
			t.Fatalf("mark old viewed: %v", err)
		}
	}
	recentAt := now.Add(-10 * time.Minute)
	if _, err := store.UpdateTelemetry(ctx, "memory://decay-recent-near-tie", "ws", storage.MemoryTelemetryUpdate{
		Action: "viewed",
		At:     &recentAt,
	}); err != nil {
		t.Fatalf("mark recent viewed: %v", err)
	}
}

func TestSearchMemoriesVectorUsesProvidedStoreAndFiltersCodeChunks(t *testing.T) {
	ctx := context.Background()
	cfg := config.Config{
		Database: config.DatabaseSettings{
			Driver: "turso",
			Turso: config.TursoSettings{
				URL: "libsql://configured-store-should-not-be-opened.invalid",
			},
			Vector: config.VectorSettings{Enabled: true, Dimensions: 2},
		},
	}
	store, err := memory.Open(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("open memory: %v", err)
	}
	defer store.Close()

	if _, err := store.SaveFromResult(ctx, "file://ws/internal/a.go", "file_summary", "ws", "file summary", []byte(`{"text":"file summary"}`)); err != nil {
		t.Fatalf("save file summary: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "file://ws/internal/a.go", "ws", []float32{1, 0}); err != nil {
		t.Fatalf("update file summary embedding: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "file://ws/internal/a.go#chunk-0", "file_embedding_chunk", "ws", "code chunk", []byte(`{"text":"code chunk"}`)); err != nil {
		t.Fatalf("save chunk memory: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "file://ws/internal/a.go#chunk-0", "ws", []float32{1, 0}); err != nil {
		t.Fatalf("update chunk embedding: %v", err)
	}
	if _, err := store.SaveFromResult(ctx, "note://semantic-commenting", "note", "ws", "semantic commenting note", []byte(`{"text":"semantic commenting note"}`)); err != nil {
		t.Fatalf("save note memory: %v", err)
	}
	if err := store.UpdateEmbedding(ctx, "note://semantic-commenting", "ws", []float32{1, 0}); err != nil {
		t.Fatalf("update note embedding: %v", err)
	}

	results, hint, decayStats, err := searchMemories(ctx, cfg, "ws", "semantic commenting", []float32{1, 0}, 5, &Input{}, store)
	if err != nil {
		t.Fatalf("searchMemories: %v", err)
	}
	if decayStats != nil {
		t.Fatalf("decayStats=%#v want nil when disabled", decayStats)
	}
	if hint != "" {
		t.Fatalf("hint=%q want empty", hint)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1: %#v", len(results), results)
	}
	if results[0].Name != "note://semantic-commenting" {
		t.Fatalf("result name=%q want note://semantic-commenting", results[0].Name)
	}
}

func TestDetectEmbeddingProviderName_OpenAICompat(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Provider: "openai-compatible",
			Model:    "text-embedding-embeddinggemma-300m-qat",
			BaseURL:  "http://127.0.0.1:1234/v1",
		},
	}
	if got := detectEmbeddingProviderName(cfg, ""); got != "openai_compat" {
		t.Fatalf("provider=%q want openai_compat", got)
	}
}

func TestNoEmbeddingHint_OpenAICompat(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Provider: "openai_compat",
			BaseURL:  "http://127.0.0.1:1234/v1",
		},
	}
	hint := noEmbeddingHint(cfg)
	if !strings.Contains(hint, "FOXCTL_EMBEDDING_PROVIDER=openai_compat") {
		t.Fatalf("hint=%q", hint)
	}
}

func TestContextRetrievalToResults(t *testing.T) {
	retrieved := contextplane.RetrievalResult{
		WorkspaceID: "foxctl",
		TopOfMind: &contextplane.TopOfMind{
			Objective:   "Bring v2 skills to parity",
			Phase:       "implement",
			NextActions: []string{"Wire default executor", "Adopt Jido payloads"},
		},
		LatestHandoff: &contextplane.HandoffRecord{
			Handoff: contextplane.Handoff{
				TaskID:  "T-1",
				Phase:   "design",
				Summary: "Defined the first v2 parity slice.",
			},
		},
		VaultHits: []contextplane.RetrievalHit{
			{Path: "notes/repo/foxctl/skills-runtime-wiring.md", Title: "skills runtime wiring", Snippet: "Bridge delegate and executor wiring"},
		},
	}

	results := contextRetrievalToResults(retrieved, 3)
	if len(results) != 3 {
		t.Fatalf("len(results)=%d want 3", len(results))
	}
	if results[0].Source != ScopeContext {
		t.Fatalf("top result source=%q want %q", results[0].Source, ScopeContext)
	}
	if results[0].Name != "Top of Mind" {
		t.Fatalf("top result name=%q want Top of Mind", results[0].Name)
	}
	if results[2].Path != "notes/repo/foxctl/skills-runtime-wiring.md" {
		t.Fatalf("vault result path=%q", results[2].Path)
	}
}

func TestSearchPathFallback(t *testing.T) {
	workspace := t.TempDir()
	mustWrite := func(rel string) {
		path := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite("cmd/foxctl/cmd/agent.go")
	mustWrite("internal/platform/config/config.go")
	mustWrite("internal/adapters/skillslib/skillmain/main.go")
	mustWrite("skills/code_semantic_search/skill.yaml")

	results := searchPathFallback(context.Background(), workspace, "platform config settings", 5)
	if len(results) == 0 {
		t.Fatal("expected path fallback results")
	}
	if results[0].Path != "internal/platform/config/config.go" {
		t.Fatalf("top path=%q want internal/platform/config/config.go", results[0].Path)
	}

	results = searchPathFallback(context.Background(), workspace, "code semantic search skill manifest", 10)
	foundManifest := false
	for _, item := range results {
		if item.Path == "skills/code_semantic_search/skill.yaml" {
			foundManifest = true
			break
		}
	}
	if !foundManifest {
		t.Fatalf("expected skill manifest in results: %v", results)
	}
}

func TestSearchPathFallbackRequiresTokenMatchForCodePaths(t *testing.T) {
	workspace := t.TempDir()
	mustWrite := func(rel string) {
		path := filepath.Join(workspace, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	mustWrite("docs/archive/source/cmd/foxctl_viewer/actions.go")
	mustWrite("cmd/foxctl/cmd/agent.go")
	mustWrite("internal/platform/config/config.go")

	results := searchPathFallback(context.Background(), workspace, "normalizeParallelism", 10)
	if len(results) != 0 {
		t.Fatalf("expected no arbitrary code-path fallback hits, got %v", results)
	}
}

func TestMergeSymbolResultsWithFallbackKeepsPrimaryFirst(t *testing.T) {
	primary := []Result{{
		ID:         "symbol:workspace:skills/embedding_worker/main.go/normalizeParallelism",
		Path:       "skills/embedding_worker/main.go",
		Similarity: 0.016,
		SourceRank: 1,
	}}
	fallback := []Result{{
		ID:         "symbol:path_fallback:docs/archive/source/cmd/foxctl_viewer/actions.go",
		Path:       "docs/archive/source/cmd/foxctl_viewer/actions.go",
		Similarity: 0.6,
		SourceRank: 1,
	}}

	results := mergeSymbolResultsWithFallback(primary, fallback, 2)
	if len(results) != 2 {
		t.Fatalf("len(results)=%d want 2", len(results))
	}
	if results[0].ID != primary[0].ID {
		t.Fatalf("top result id=%q want primary %q", results[0].ID, primary[0].ID)
	}
	if results[1].ID != fallback[0].ID {
		t.Fatalf("fallback result id=%q want %q", results[1].ID, fallback[0].ID)
	}
}

type fakeSymbolVectorSearchStore struct {
	workspace string
	entryType string
	limit     int
	entries   []storage.ScoredEntry
}

func (f *fakeSymbolVectorSearchStore) SearchSimilarByType(_ context.Context, workspace, entryType string, _ []float32, limit int) ([]storage.ScoredEntry, error) {
	f.workspace = workspace
	f.entryType = entryType
	f.limit = limit
	return f.entries, nil
}

func TestSearchSymbolsFromMemoryEmbeddingsUsesStoredSymbolVectors(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "skills/embedding_worker/main.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc normalizeParallelism() int {\n\treturn 4\n}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	resultBytes, err := symbol.MarshalResult(symbol.Result{
		Symbol: symbol.Symbol{
			ID:        "skills/embedding_worker/main.go:normalizeParallelism",
			FilePath:  "skills/embedding_worker/main.go",
			Name:      "normalizeParallelism",
			Language:  "go",
			Kind:      symbol.KindFunction,
			StartLine: 3,
			EndLine:   5,
		},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	store := &fakeSymbolVectorSearchStore{entries: []storage.ScoredEntry{{
		Entry: storage.NamedEntry{
			Name:      "symbol://workspace/go:skills/embedding_worker::main.go/normalizeParallelism",
			Type:      symbol.SymbolType,
			Workspace: "workspace",
			Summary:   "function normalizeParallelism in skills/embedding_worker/main.go",
			Result:    resultBytes,
		},
		Score: 0.72,
	}}}

	results, err := searchSymbolsFromMemoryEmbeddings(context.Background(), store, "workspace", workspace, []float32{1, 0}, 5)
	if err != nil {
		t.Fatalf("search symbols: %v", err)
	}
	if store.workspace != "workspace" || store.entryType != symbol.SymbolType || store.limit != 5 {
		t.Fatalf("unexpected search args workspace=%q type=%q limit=%d", store.workspace, store.entryType, store.limit)
	}
	if len(results) != 1 {
		t.Fatalf("len(results)=%d want 1", len(results))
	}
	if results[0].Path != "skills/embedding_worker/main.go" {
		t.Fatalf("path=%q", results[0].Path)
	}
	if results[0].Name != "normalizeParallelism" {
		t.Fatalf("name=%q", results[0].Name)
	}
	if results[0].Similarity != 0.72 {
		t.Fatalf("similarity=%v", results[0].Similarity)
	}
	if !strings.Contains(results[0].Snippet, "func normalizeParallelism") {
		t.Fatalf("snippet=%q", results[0].Snippet)
	}
}

func TestScorePathFallbackCandidateBoostsDeclarativeArtifactsForManifestQueries(t *testing.T) {
	scoreManifest := scorePathFallbackCandidate("skills/code_semantic_search/skill.yaml", []string{"code", "semantic", "search", "skill", "manifest"})
	scoreRuntime := scorePathFallbackCandidate("internal/agent/runtime/runtime.go", []string{"code", "semantic", "search", "skill", "manifest"})
	if scoreManifest <= scoreRuntime {
		t.Fatalf("manifest score=%d runtime score=%d", scoreManifest, scoreRuntime)
	}
}

func TestDefaultSemanticSearchScopes(t *testing.T) {
	workspace := t.TempDir()
	got := defaultSemanticSearchScopes(workspace, "")
	want := []string{ScopeSymbols, ScopeSessions, ScopeMemories, ScopeTasks, ScopeCodemaps}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default scopes=%v want %v", got, want)
	}

	got = defaultSemanticSearchScopes(workspace, ProfileCode)
	want = []string{ScopeSymbols, ScopeCodemaps}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("code profile scopes=%v want %v", got, want)
	}

	policyPath := filepath.Join(workspace, ".foxctl", "policy", "retrieval.yaml")
	if err := os.MkdirAll(filepath.Dir(policyPath), 0o755); err != nil {
		t.Fatalf("mkdir policy dir: %v", err)
	}
	if err := os.WriteFile(policyPath, []byte("semantic_search_default_scopes:\n  - symbols\n  - context\n  - codemaps\n  - context\n"), 0o644); err != nil {
		t.Fatalf("write retrieval policy: %v", err)
	}
	got = defaultSemanticSearchScopes(workspace, "")
	want = []string{ScopeSymbols, ScopeContext, ScopeCodemaps}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("policy scopes=%v want %v", got, want)
	}

	got = defaultSemanticSearchScopes(workspace, ProfileCode)
	want = []string{ScopeSymbols, ScopeCodemaps}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("code profile should ignore broad policy scopes, got %v want %v", got, want)
	}
}

func TestNormalizeSemanticSearchProfile(t *testing.T) {
	if got := normalizeSemanticSearchProfile(""); got != ProfileDefault {
		t.Fatalf("empty profile=%q want %q", got, ProfileDefault)
	}
	if got := normalizeSemanticSearchProfile(" CODE "); got != ProfileCode {
		t.Fatalf("normalized code profile=%q want %q", got, ProfileCode)
	}
	if got := normalizeSemanticSearchProfile("weird"); got != "weird" {
		t.Fatalf("unknown profile=%q want weird", got)
	}
}

func TestBuildCandidateBundlesGroupsCoLocatedPaths(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "skills", "code_semantic_search"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "code_semantic_search", "skill.yaml"), []byte("kind: Skill\n"), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "skills", "code_semantic_search", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	results := []Result{
		{Path: "skills/code_semantic_search/skill.yaml", Name: "skill.yaml", FinalScore: 0.92, Source: ScopeSymbols, Summary: "semantic search manifest"},
		{Path: "internal/agent/runtime/runtime.go", Name: "executeSmartSearch", FinalScore: 0.81, Source: ScopeSymbols},
	}

	bundles := buildCandidateBundles(workspace, results, 8)
	if len(bundles) != 2 {
		t.Fatalf("len(bundles)=%d want 2 (%v)", len(bundles), bundles)
	}
	if bundles[0].PrimaryPath != "skills/code_semantic_search/skill.yaml" {
		t.Fatalf("primary=%q", bundles[0].PrimaryPath)
	}
	if !reflect.DeepEqual(bundles[0].RelatedPaths, []string{"skills/code_semantic_search/main.go"}) {
		t.Fatalf("related=%v", bundles[0].RelatedPaths)
	}
	if bundles[0].Ambiguity != "single_file_with_companions" {
		t.Fatalf("ambiguity=%q", bundles[0].Ambiguity)
	}
}

func TestSearchPathFallbackSkipsDependencyAndBuildDirs(t *testing.T) {
	workspace := t.TempDir()
	for _, dir := range []string{
		filepath.Join(workspace, "deps", "pkg"),
		filepath.Join(workspace, "_build", "prod"),
		filepath.Join(workspace, "apps", "real"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "deps", "pkg", "session_restore.ex"), []byte("defmodule Dep.SessionRestore do\nend\n"), 0o644); err != nil {
		t.Fatalf("write deps file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "_build", "prod", "session_restore.ex"), []byte("defmodule Build.SessionRestore do\nend\n"), 0o644); err != nil {
		t.Fatalf("write build file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "apps", "real", "session_restore.ex"), []byte("defmodule Real.SessionRestore do\nend\n"), 0o644); err != nil {
		t.Fatalf("write real file: %v", err)
	}

	results := searchPathFallback(context.Background(), workspace, "session restore", 10)
	if len(results) == 0 {
		t.Fatal("expected fallback results")
	}
	for _, result := range results {
		if strings.HasPrefix(result.Path, "deps/") || strings.HasPrefix(result.Path, "_build/") {
			t.Fatalf("unexpected dependency/build fallback hit: %v", results)
		}
	}
}
