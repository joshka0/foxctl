package semantic

import (
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestNewEmbedderFromConfigUsesConfigVoyageKey(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Provider:     "voyage",
			Model:        "voyage-3.5",
			VoyageAPIKey: "test-voyage-key",
		},
	}

	provider, err := NewProviderForScope(ScopeMemory, cfg)
	if err != nil {
		t.Fatalf("NewProviderForScope: %v", err)
	}
	if provider.Model() != "voyage-3.5" {
		t.Fatalf("model = %q, want voyage-3.5", provider.Model())
	}
}

func TestNewEmbedderFromConfigUsesConfigGeminiKey(t *testing.T) {
	cfg := config.Config{
		LLM: config.LLMSettings{
			GeminiAPIKey: "test-gemini-key",
		},
		Embedding: config.EmbeddingSettings{
			Provider: "gemini",
			Model:    "gemini-embedding-001",
		},
	}

	provider, err := NewProviderForScope(ScopeMemory, cfg)
	if err != nil {
		t.Fatalf("NewProviderForScope: %v", err)
	}
	if provider.Model() != "gemini-embedding-001" {
		t.Fatalf("model = %q, want gemini-embedding-001", provider.Model())
	}
}
