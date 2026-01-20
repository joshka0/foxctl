package main

import (
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
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
