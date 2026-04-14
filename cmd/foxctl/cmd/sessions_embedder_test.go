package cmd

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/adapters/sourceimport"
)

func TestResolveResynthesizeEmbedder_DisabledReturnsNilEmbedder(t *testing.T) {
	t.Parallel()

	embedder, resolved, err := resolveResynthesizeEmbedder(false, sourceimport.EmbedderConfig{
		Provider: "voyage",
		Model:    "voyage-3.5",
	})
	if err != nil {
		t.Fatalf("resolveResynthesizeEmbedder() error = %v", err)
	}
	if embedder != nil {
		t.Fatal("embedder should be nil when includeEmbedding=false")
	}
	if resolved.Provider != sourceimport.EmbeddingProviderVoyage {
		t.Fatalf("provider=%q want %q", resolved.Provider, sourceimport.EmbeddingProviderVoyage)
	}
	if resolved.Model != "voyage-3.5" {
		t.Fatalf("model=%q want voyage-3.5", resolved.Model)
	}
}

func TestResolveResynthesizeEmbedder_HashDefault(t *testing.T) {
	t.Parallel()

	embedder, resolved, err := resolveResynthesizeEmbedder(true, sourceimport.EmbedderConfig{
		Provider:   "",
		Dimensions: 48,
		Timeout:    3 * time.Second,
	})
	if err != nil {
		t.Fatalf("resolveResynthesizeEmbedder() error = %v", err)
	}
	if embedder == nil {
		t.Fatal("embedder is nil")
	}
	if resolved.Provider != sourceimport.EmbeddingProviderHash {
		t.Fatalf("provider=%q want %q", resolved.Provider, sourceimport.EmbeddingProviderHash)
	}
	if got := sourceimport.DeclaredEmbedderDimensions(embedder); got != 48 {
		t.Fatalf("DeclaredEmbedderDimensions()=%d want 48", got)
	}
}

func TestResolveResynthesizeEmbedder_InvalidProvider(t *testing.T) {
	t.Parallel()

	_, _, err := resolveResynthesizeEmbedder(true, sourceimport.EmbedderConfig{
		Provider: "bad-provider",
	})
	if err == nil {
		t.Fatal("expected invalid provider error")
	}
}
