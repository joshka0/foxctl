package semantic

import (
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestNewEmbedderFromConfigUsesOpenAICompatConfig(t *testing.T) {
	cfg := config.Config{
		Embedding: config.EmbeddingSettings{
			Provider: "lmstudio",
			Model:    "text-embedding-qwen3-embedding-8b",
			BaseURL:  "http://127.0.0.1:1234/v1",
		},
	}

	provider, err := NewProviderForScope(ScopeMemory, cfg)
	if err != nil {
		t.Fatalf("NewProviderForScope: %v", err)
	}
	if provider.Model() != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model = %q, want text-embedding-qwen3-embedding-8b", provider.Model())
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

func TestResolveDimensionsForModelPrefersKnownNonDefaultModel(t *testing.T) {
	got := ResolveDimensionsForModel("text-embedding-qwen3-embedding-8b", 1024)
	if got != 4096 {
		t.Fatalf("dimensions = %d, want 4096", got)
	}
}

func TestResolveDimensionsForModelSupportsQwenThreeSmallEmbedding(t *testing.T) {
	got := ResolveDimensionsForModel("qwen3-embedding-0.6b", 4096)
	if got != 1024 {
		t.Fatalf("dimensions = %d, want 1024", got)
	}
}

func TestResolveDimensionsForModelSupportsQwenThreeFourBEmbedding(t *testing.T) {
	got := ResolveDimensionsForModel("qwen3-embedding-4b", 4096)
	if got != 2560 {
		t.Fatalf("dimensions = %d, want 2560", got)
	}
}

func TestResolveDimensionsForModelUsesConfiguredForUnknownModel(t *testing.T) {
	got := ResolveDimensionsForModel("local-unknown-embedder", 1536)
	if got != 1536 {
		t.Fatalf("dimensions = %d, want configured 1536", got)
	}
}
