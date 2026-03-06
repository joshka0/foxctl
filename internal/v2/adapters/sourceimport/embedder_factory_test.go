package sourceimport

import (
	"context"
	"testing"
	"time"
)

func TestResolveEmbedderConfig_DefaultHash(t *testing.T) {
	t.Parallel()

	got, err := ResolveEmbedderConfig(EmbedderConfig{
		Provider:   "",
		Dimensions: 384,
	})
	if err != nil {
		t.Fatalf("ResolveEmbedderConfig() error = %v", err)
	}
	if got.Provider != EmbeddingProviderHash {
		t.Fatalf("provider=%q want %q", got.Provider, EmbeddingProviderHash)
	}
	if got.Model != hashEmbeddingModel {
		t.Fatalf("model=%q want %q", got.Model, hashEmbeddingModel)
	}
	if got.Dimensions != 384 {
		t.Fatalf("dims=%d want 384", got.Dimensions)
	}
}

func TestResolveEmbedderConfig_LMStudioFromEnv(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"LMSTUDIO_EMBEDDING_MODEL": "nomic-v1",
		"LMSTUDIO_BASE_URL":        "http://127.0.0.1:1234/v1",
		"LMSTUDIO_API_KEY":         "k",
	}
	got, err := ResolveEmbedderConfig(EmbedderConfig{
		Provider: "lmstudio",
		EnvLookup: func(k string) string {
			return env[k]
		},
	})
	if err != nil {
		t.Fatalf("ResolveEmbedderConfig() error = %v", err)
	}
	if got.Model != "nomic-v1" {
		t.Fatalf("model=%q want nomic-v1", got.Model)
	}
	if got.BaseURL != "http://127.0.0.1:1234/v1" {
		t.Fatalf("baseURL=%q want lmstudio env", got.BaseURL)
	}
	if got.APIKey != "k" {
		t.Fatalf("apiKey=%q want k", got.APIKey)
	}
}

func TestResolveEmbedderConfig_VoyageFallbackChain(t *testing.T) {
	t.Parallel()

	env := map[string]string{
		"AGENTCTL_EMBEDDING_MODEL_TEXT": "voyage-text-fallback",
	}
	got, err := ResolveEmbedderConfig(EmbedderConfig{
		Provider: "voyage",
		EnvLookup: func(k string) string {
			return env[k]
		},
	})
	if err != nil {
		t.Fatalf("ResolveEmbedderConfig() error = %v", err)
	}
	if got.Model != "voyage-text-fallback" {
		t.Fatalf("model=%q want voyage-text-fallback", got.Model)
	}
}

func TestResolveEmbedderConfig_InvalidProvider(t *testing.T) {
	t.Parallel()

	_, err := ResolveEmbedderConfig(EmbedderConfig{Provider: "bad"})
	if err == nil {
		t.Fatal("expected invalid provider error")
	}
}

func TestNewEmbedderFromConfig_Hash(t *testing.T) {
	t.Parallel()

	embedder, resolved, err := NewEmbedderFromConfig(EmbedderConfig{
		Provider:   EmbeddingProviderHash,
		Dimensions: 24,
	})
	if err != nil {
		t.Fatalf("NewEmbedderFromConfig() error = %v", err)
	}
	if resolved.Provider != EmbeddingProviderHash {
		t.Fatalf("resolved provider=%q want hash", resolved.Provider)
	}
	if got := DeclaredEmbedderDimensions(embedder); got != 24 {
		t.Fatalf("DeclaredEmbedderDimensions()=%d want 24", got)
	}
}

func TestProbeEmbedderDimensions(t *testing.T) {
	t.Parallel()

	embedder := NewHashEmbedder(32)
	dims, model, err := ProbeEmbedderDimensions(context.Background(), embedder, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeEmbedderDimensions() error = %v", err)
	}
	if dims != 32 {
		t.Fatalf("dims=%d want 32", dims)
	}
	if model != hashEmbeddingModel {
		t.Fatalf("model=%q want %q", model, hashEmbeddingModel)
	}
}
