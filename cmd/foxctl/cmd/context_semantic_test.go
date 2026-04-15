package cmd

import (
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestObsidianSemanticEnabled(t *testing.T) {
	t.Run("explicit true env enables semantic retrieval", func(t *testing.T) {
		t.Setenv("FOXCTL_OBSIDIAN_SEMANTIC_ENABLED", "true")
		if !obsidianSemanticEnabled(config.Config{}) {
			t.Fatalf("expected semantic retrieval to be enabled")
		}
	})

	t.Run("explicit false env disables semantic retrieval even when endpoint exists", func(t *testing.T) {
		t.Setenv("FOXCTL_OBSIDIAN_SEMANTIC_ENABLED", "false")
		t.Setenv("FOXCTL_OPENAI_COMPAT_BASE_URL", "http://127.0.0.1:1234/v1")
		if obsidianSemanticEnabled(config.Config{}) {
			t.Fatalf("expected semantic retrieval to be disabled")
		}
	})

	t.Run("openai-compatible endpoint config enables semantic retrieval by default", func(t *testing.T) {
		t.Setenv("FOXCTL_OPENAI_COMPAT_BASE_URL", "http://127.0.0.1:1234/v1")
		t.Setenv("FOXCTL_OPENAI_COMPAT_EMBEDDING_MODEL", "text-embedding-embeddinggemma-300m-qat")
		if !obsidianSemanticEnabled(config.Config{}) {
			t.Fatalf("expected semantic retrieval to be enabled when openai-compatible endpoint is configured")
		}
	})

	t.Run("embedding provider config enables semantic retrieval by default", func(t *testing.T) {
		cfg := config.Config{
			Embedding: config.EmbeddingSettings{
				Provider: "openai_compat",
			},
		}
		if !obsidianSemanticEnabled(cfg) {
			t.Fatalf("expected semantic retrieval to be enabled when embedding provider is configured")
		}
	})
}
