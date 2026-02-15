package repoindex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testNodes returns deterministic nodes where individual words match
// but a multi-word AND query returns 0 results.
// - "hybrid" appears only in node 1's doc
// - "memory" appears only in node 2's doc
// - "pipeline" appears only in node 3's doc
func testNodes(key, pkg string, now time.Time) []Node {
	return []Node{
		{
			ID:        SymbolID(key, pkg, "builder"),
			Kind:      NodeSymbol,
			Pkg:       pkg,
			File:      "builder.go",
			Name:      "builder",
			Doc:       "This is a hybrid system for building context",
			UpdatedAt: now,
		},
		{
			ID:        SymbolID(key, pkg, "store"),
			Kind:      NodeSymbol,
			Pkg:       pkg,
			File:      "store.go",
			Name:      "store",
			Doc:       "This component manages memory and storage",
			UpdatedAt: now,
		},
		{
			ID:        SymbolID(key, pkg, "runner"),
			Kind:      NodeSymbol,
			Pkg:       pkg,
			File:      "pipeline.go",
			Name:      "runner",
			Doc:       "Pipeline for extracting evidence from data",
			UpdatedAt: now,
		},
	}
}

// setupTestStore creates an ephemeral store with deterministic nodes for testing.
func setupTestStore(t *testing.T) (*Store, *QueryEngine) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	key := repoKey(repoRoot)
	pkg := "go:test/pkg"
	now := time.Now().UTC()

	if err := store.ReplaceAll(ctx, testNodes(key, pkg, now), nil); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	qe := NewQueryEngine(store)
	return store, qe
}

// --- Unit Tests: Helper Functions ---

func TestBuildOrFallbackQuery_MultiWord(t *testing.T) {
	got := buildOrFallbackQuery("hybrid memory pipeline")
	want := "hybrid OR memory OR pipeline"
	if got != want {
		t.Errorf("buildOrFallbackQuery(%q) = %q, want %q", "hybrid memory pipeline", got, want)
	}
}

func TestBuildOrFallbackQuery_SkipsOperators(t *testing.T) {
	got := buildOrFallbackQuery("cache AND invalid")
	want := "cache OR invalid"
	if got != want {
		t.Errorf("buildOrFallbackQuery(%q) = %q, want %q", "cache AND invalid", got, want)
	}
}

func TestBuildOrFallbackQuery_SingleWord(t *testing.T) {
	got := buildOrFallbackQuery("hybrid")
	if got != "" {
		t.Errorf("buildOrFallbackQuery(%q) = %q, want empty string", "hybrid", got)
	}
}

func TestBuildFallbackCandidates_Order(t *testing.T) {
	candidates := buildFallbackCandidates("hybrid memory")
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %v", len(candidates), candidates)
	}
	if candidates[0] != "hybrid memory" {
		t.Errorf("candidates[0] = %q, want %q", candidates[0], "hybrid memory")
	}
	if candidates[1] != `"hybrid memory"` {
		t.Errorf("candidates[1] = %q, want %q", candidates[1], `"hybrid memory"`)
	}
	if candidates[2] != "hybrid OR memory" {
		t.Errorf("candidates[2] = %q, want %q", candidates[2], "hybrid OR memory")
	}
}

// --- Unit Tests: Search Integration ---

func TestSearch_ZeroResultMultiWord_UsesOr(t *testing.T) {
	_, qe := setupTestStore(t)
	ctx := context.Background()

	// Multi-word AND query — no single node has all three words.
	// Should fall back to OR and return results.
	results, err := qe.Search(ctx, "hybrid memory pipeline", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected non-empty results from OR fallback, got 0")
	}
}

func TestSearch_ZeroResultSingleWord_NoFallback(t *testing.T) {
	_, qe := setupTestStore(t)
	ctx := context.Background()

	// Single word that doesn't match anything — should return empty, no OR attempted.
	results, err := qe.Search(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching single word, got %d", len(results))
	}
}

func TestSearch_SyntaxError_FallsBackToQuote(t *testing.T) {
	_, qe := setupTestStore(t)
	ctx := context.Background()

	// Malformed FTS5 query with unmatched quote — should fall back to quoted version.
	// The quoted fallback wraps it as `" hybrid"` which should match the word "hybrid" in doc.
	results, err := qe.Search(ctx, `"hybrid`, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected results from quoted fallback, got 0")
	}
}

func TestSearchScored_ZeroResultMultiWord_UsesOr(t *testing.T) {
	_, qe := setupTestStore(t)
	ctx := context.Background()

	results, err := qe.SearchScored(ctx, "hybrid memory pipeline", 10)
	if err != nil {
		t.Fatalf("search scored: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected non-empty scored results from OR fallback, got 0")
	}
}

// --- Benchmarks ---

func BenchmarkSearch_ZeroResultMultiWord_OrFallback(b *testing.B) {
	_, qe := setupBenchStore(b)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = qe.Search(ctx, "hybrid memory pipeline", 10)
	}
}

func BenchmarkSearchScored_ZeroResultMultiWord_OrFallback(b *testing.B) {
	_, qe := setupBenchStore(b)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = qe.SearchScored(ctx, "hybrid memory pipeline", 10)
	}
}

func BenchmarkSearch_SyntaxFallbackPath(b *testing.B) {
	_, qe := setupBenchStore(b)
	ctx := context.Background()

	b.ResetTimer()
	for b.Loop() {
		_, _ = qe.Search(ctx, `"hybrid`, 10)
	}
}

// setupBenchStore is like setupTestStore but for benchmarks.
func setupBenchStore(b *testing.B) (*Store, *QueryEngine) {
	b.Helper()
	ctx := context.Background()
	root := b.TempDir()
	storageRoot := filepath.Join(root, "storage")
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		b.Fatalf("mkdir repo root: %v", err)
	}

	store, err := Open(ctx, storageRoot, repoRoot)
	if err != nil {
		b.Fatalf("open store: %v", err)
	}
	b.Cleanup(func() { store.Close() })

	key := repoKey(repoRoot)
	pkg := "go:bench/pkg"
	now := time.Now().UTC()

	if err := store.ReplaceAll(ctx, testNodes(key, pkg, now), nil); err != nil {
		b.Fatalf("replace all: %v", err)
	}

	qe := NewQueryEngine(store)
	return store, qe
}
