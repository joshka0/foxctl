package cmd

import (
	"context"
	"errors"
	"strings"
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

func TestEvalModesRequireSemanticHealth(t *testing.T) {
	if !evalModesRequireSemanticHealth([]string{"contextwiki_default"}) {
		t.Fatalf("contextwiki_default should require semantic health")
	}
	if !evalModesRequireSemanticHealth([]string{"semantic"}) {
		t.Fatalf("semantic should require semantic health")
	}
	if evalModesRequireSemanticHealth([]string{"repoindex_search", "contextwiki_control_only"}) {
		t.Fatalf("repoindex_search and contextwiki_control_only should not require semantic health")
	}
}

func TestCheckEvalSemanticProviderHealthSurfacesEndpointFailure(t *testing.T) {
	err := checkEvalSemanticProviderHealth(context.Background(), failingEvalSemanticProvider{err: errors.New("dial tcp 127.0.0.1:1234: connect: connection refused")}, nil)
	if err == nil {
		t.Fatalf("expected semantic provider health error")
	}
	for _, want := range []string{"semantic embedder health check failed", "LM Studio/OpenAI-compatible embedding endpoint", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCheckEvalSemanticProviderHealthSurfacesProviderCreationFailure(t *testing.T) {
	err := checkEvalSemanticProviderHealth(context.Background(), nil, errors.New("model is required"))
	if err == nil {
		t.Fatalf("expected semantic provider creation error")
	}
	if !strings.Contains(err.Error(), "semantic embedder unavailable") || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("error=%q", err.Error())
	}
}

type failingEvalSemanticProvider struct {
	err error
}

func (p failingEvalSemanticProvider) Embed(context.Context, string) ([]float32, error) {
	return nil, p.err
}

func (p failingEvalSemanticProvider) EmbedBatch(context.Context, []string) ([][]float32, error) {
	return nil, p.err
}

func (p failingEvalSemanticProvider) Model() string {
	return "test-embedding-model"
}

func (p failingEvalSemanticProvider) Dimensions() int {
	return 0
}
