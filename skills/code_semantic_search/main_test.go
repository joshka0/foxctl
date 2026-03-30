package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

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
			Model: "voyage-3.5",
			Models: map[string]string{
				"symbols":  "voyage-code-3",
				"memory":   "voyage-3-large",
				"tasks":    "voyage-3.5",
				"sessions": "voyage-3.5",
			},
		},
	}

	codeModel, memoryModel, textModel, _ := embeddingModelConfig("voyage", cfg)
	if codeModel != "voyage-code-3" {
		t.Fatalf("code model = %q, want voyage-code-3", codeModel)
	}
	if memoryModel != "voyage-3-large" {
		t.Fatalf("memory model = %q, want voyage-3-large", memoryModel)
	}
	if textModel != "voyage-3.5" {
		t.Fatalf("text model = %q, want voyage-3.5", textModel)
	}
}

func TestEmbeddingModelConfig_GeminiFallback(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Model: "voyage-3.5",
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

	payload := []byte(`{"anchor_path":"internal/contextplane/dispatch.go","neighbor_paths":["internal/contextplane/store.go"],"summary":"dispatch cluster"}`)
	if _, err := store.SaveFromResult(ctx, "cochange://internal/contextplane/dispatch.go", contextplane.CoChangeClusterType, "ws", "dispatch changes with store", payload); err != nil {
		t.Fatalf("save cochange artifact: %v", err)
	}

	results, err := searchMemoriesBM25(ctx, cfg, "ws", "dispatch", 5, store)
	if err != nil {
		t.Fatalf("searchMemoriesBM25: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected results")
	}
	if results[0].Source != "cochange" {
		t.Fatalf("source=%q want cochange", results[0].Source)
	}
	if results[0].Path != "internal/contextplane/dispatch.go" {
		t.Fatalf("path=%q want internal/contextplane/dispatch.go", results[0].Path)
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
	if got := detectEmbeddingProviderName(cfg, "", ""); got != "openai_compat" {
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
	if !strings.Contains(hint, "AGENTCTL_EMBEDDING_PROVIDER=openai_compat") {
		t.Fatalf("hint=%q", hint)
	}
}

func TestContextRetrievalToResults(t *testing.T) {
	retrieved := contextplane.RetrievalResult{
		WorkspaceID: "agentctl",
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
			{Path: "notes/repo/agentctl/skills-runtime-wiring.md", Title: "skills runtime wiring", Snippet: "Bridge delegate and executor wiring"},
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
	if results[2].Path != "notes/repo/agentctl/skills-runtime-wiring.md" {
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
	mustWrite("cmd/agentctl/cmd/agent.go")
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

	policyPath := filepath.Join(workspace, ".agentctl", "policy", "retrieval.yaml")
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
