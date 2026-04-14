package obsidianindex

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRebuildSearchRelatedAndStats(t *testing.T) {
	ctx := context.Background()
	storageRoot := t.TempDir()
	vaultRoot := fixtureVaultRoot(t)

	store, err := Open(ctx, storageRoot, vaultRoot)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	result, err := store.Rebuild(ctx, vaultRoot)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if result.Notes == 0 || result.Links == 0 || result.Tags == 0 || result.Chunks == 0 || result.RepoPaths == 0 || result.Symbols == 0 {
		t.Fatalf("unexpected build result: %#v", result)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Notes != result.Notes {
		t.Fatalf("stats notes=%d want %d", stats.Notes, result.Notes)
	}
	if stats.Tags != result.Tags || stats.Chunks != result.Chunks || stats.RepoPaths != result.RepoPaths || stats.Symbols != result.Symbols {
		t.Fatalf("stats mismatch: %#v vs %#v", stats, result)
	}

	hits, err := store.SearchNotes(ctx, "evidence-backed", 10)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected search hits")
	}
	if hits[0].Snippet == "" {
		t.Fatalf("expected chunk-backed snippet on first hit")
	}
	tagHits, err := store.SearchNotes(ctx, "handoff", 10)
	if err != nil {
		t.Fatalf("SearchNotes tags: %v", err)
	}
	if len(tagHits) == 0 {
		t.Fatalf("expected tag-aware search hits")
	}
	repoPathHits, err := store.SearchNotes(ctx, "internal/context/contextplane", 10)
	if err != nil {
		t.Fatalf("SearchNotes repo paths: %v", err)
	}
	if len(repoPathHits) == 0 {
		t.Fatalf("expected repo path-aware search hits")
	}
	if repoPathHits[0].Path != "notes/patterns/compact-handoff-pattern.md" {
		t.Fatalf("repo path hit=%q want compact handoff pattern", repoPathHits[0].Path)
	}
	primaryAnchorHits, err := store.SearchNotes(ctx, "store.go", 10)
	if err != nil {
		t.Fatalf("SearchNotes primary anchor: %v", err)
	}
	if len(primaryAnchorHits) == 0 {
		t.Fatalf("expected primary-anchor-aware search hits")
	}
	if primaryAnchorHits[0].PrimaryAnchorPath != "internal/context/contextplane/store.go" {
		t.Fatalf("primary anchor=%q want internal/context/contextplane/store.go", primaryAnchorHits[0].PrimaryAnchorPath)
	}
	if got := primaryAnchorHits[0].AnchorRoles["impl"]; len(got) != 0 {
		t.Fatalf("unexpected impl anchor roles in fixture hit: %v", got)
	}
	repoSymbolHits, err := store.SearchNotes(ctx, "WorkspaceStore", 10)
	if err != nil {
		t.Fatalf("SearchNotes repo symbols: %v", err)
	}
	if len(repoSymbolHits) == 0 {
		t.Fatalf("expected repo symbol-aware search hits")
	}
	if repoSymbolHits[0].Path != "notes/patterns/compact-handoff-pattern.md" {
		t.Fatalf("repo symbol hit=%q want compact handoff pattern", repoSymbolHits[0].Path)
	}

	related, err := store.RelatedNotes(ctx, "notes/moc/foxctl-context.md", 10)
	if err != nil {
		t.Fatalf("RelatedNotes: %v", err)
	}
	if len(related) == 0 {
		t.Fatalf("expected related hits")
	}

	health, err := store.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if len(health.Orphans) == 0 && len(health.DeadEnds) == 0 {
		t.Fatalf("expected some health signal in fixture vault")
	}

	embedded, err := store.EnsureSemanticEmbeddings(ctx, fakeEmbeddingProvider{})
	if err != nil {
		t.Fatalf("EnsureSemanticEmbeddings: %v", err)
	}
	if embedded == 0 {
		t.Fatalf("expected semantic embeddings to be created")
	}
	stats, err = store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after embeddings: %v", err)
	}
	if stats.SemanticEmbeddings == 0 {
		t.Fatalf("expected semantic embedding count in stats")
	}
	semanticHits, err := store.SearchNotesSemantic(ctx, "compact handoff pattern", fakeEmbeddingProvider{}, 10)
	if err != nil {
		t.Fatalf("SearchNotesSemantic: %v", err)
	}
	if len(semanticHits) == 0 {
		t.Fatalf("expected semantic search hits")
	}
	foundCompactPattern := false
	for _, hit := range semanticHits {
		if hit.Path == "notes/patterns/compact-handoff-pattern.md" {
			foundCompactPattern = true
			break
		}
	}
	if !foundCompactPattern {
		t.Fatalf("expected compact handoff pattern in semantic hits, got %#v", semanticHits)
	}
	stats, err = store.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats after semantic search: %v", err)
	}
	if stats.ChunkSemanticEmbeddings == 0 {
		t.Fatalf("expected chunk semantic embedding count in stats")
	}

	if _, err := store.SearchNotesSemantic(ctx, "compact handoff pattern", fakeEmbeddingProviderDifferentDims{}, 10); err == nil {
		t.Fatalf("expected dimension mismatch error")
	}
}

func TestRetryObsidianBusyRetriesLockedErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	attempts := 0
	err := retryObsidianBusy(ctx, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryObsidianBusy() error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts=%d want 3", attempts)
	}
}

func TestRetryObsidianBusyHonorsContextDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := retryObsidianBusy(ctx, func() error {
		return errors.New("SQLITE_BUSY")
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retryObsidianBusy() error = %v want deadline exceeded", err)
	}
}

func fixtureVaultRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "tooling", "tools", "obsidian", "testdata", "vaults", "basic"))
}

type fakeEmbeddingProvider struct{}

func (fakeEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	return fakeEmbeddingForText(text), nil
}

func (fakeEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		out = append(out, fakeEmbeddingForText(text))
	}
	return out, nil
}

func (fakeEmbeddingProvider) Model() string {
	return "fake-obsidian-semantic"
}

func (fakeEmbeddingProvider) Dimensions() int {
	return 3
}

type fakeEmbeddingProviderDifferentDims struct{}

func (fakeEmbeddingProviderDifferentDims) Embed(_ context.Context, text string) ([]float32, error) {
	return fakeEmbeddingForText(text), nil
}

func (fakeEmbeddingProviderDifferentDims) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		out = append(out, fakeEmbeddingForText(text))
	}
	return out, nil
}

func (fakeEmbeddingProviderDifferentDims) Model() string {
	return "fake-obsidian-semantic"
}

func (fakeEmbeddingProviderDifferentDims) Dimensions() int {
	return 4
}

func fakeEmbeddingForText(text string) []float32 {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "compact") && strings.Contains(lower, "pattern"):
		return []float32{1, 0, 0}
	case strings.Contains(lower, "draft") && strings.Contains(lower, "handoff"):
		return []float32{0, 1, 0}
	case strings.Contains(lower, "incident"):
		return []float32{0, 0, 1}
	case strings.Contains(lower, "handoff"):
		return []float32{1, 0.2, 0}
	default:
		return []float32{0.1, 0.1, 0.1}
	}
}
