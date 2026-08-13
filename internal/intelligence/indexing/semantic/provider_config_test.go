package semantic

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestNewProviderForModel_WorkspaceOpenAICompatOverrideWins(t *testing.T) {
	tmp := t.TempDir()
	oldHome := os.Getenv("HOME")
	t.Setenv("HOME", tmp)
	t.Cleanup(func() {
		if oldHome == "" {
			_ = os.Unsetenv("HOME")
		} else {
			_ = os.Setenv("HOME", oldHome)
		}
	})

	home := filepath.Join(tmp, ".foxctl")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(`
embedding:
  provider: lmstudio
  model: text-embedding-embeddinggemma-300m-qat
  base_url: http://127.0.0.1:1234/v1
`), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	workspace := filepath.Join(tmp, "foxctl")
	if err := os.MkdirAll(filepath.Join(workspace, ".foxctl"), 0o755); err != nil {
		t.Fatalf("mkdir workspace config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".foxctl", "config.yaml"), []byte(`
embedding:
  provider: lmstudio
  model: text-embedding-qwen3-embedding-8b
  base_url: http://127.0.0.1:1234/v1
`), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	cfg, err := config.Load(context.Background(), config.WithWorkspacePath(workspace))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	provider, err := NewProviderForModel("text-embedding-qwen3-embedding-8b", cfg, WithProvider(cfg.Embedding.Provider), WithAPIKey(cfg.Embedding.APIKey), WithBaseURL(cfg.Embedding.BaseURL))
	if err != nil {
		t.Fatalf("NewProviderForModel: %v", err)
	}
	if _, ok := provider.(*OpenAICompatProvider); !ok {
		t.Fatalf("expected OpenAICompatProvider, got %T", provider)
	}
	if provider.Model() != "text-embedding-qwen3-embedding-8b" {
		t.Fatalf("model=%q", provider.Model())
	}
}

func TestNewProviderForModel_ModelFamilyOverridesConfiguredProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.Embedding.Provider = "gemini"
	cfg.Embedding.Model = "text-embedding-embeddinggemma-300m-qat"
	cfg.Embedding.BaseURL = "http://127.0.0.1:1234/v1"

	provider, err := NewProviderForModel(
		"text-embedding-embeddinggemma-300m-qat",
		cfg,
		WithProvider(cfg.Embedding.Provider),
		WithBaseURL(cfg.Embedding.BaseURL),
	)
	if err != nil {
		t.Fatalf("NewProviderForModel: %v", err)
	}
	if _, ok := provider.(*OpenAICompatProvider); !ok {
		t.Fatalf("expected OpenAICompatProvider, got %T", provider)
	}
	if provider.Model() != "text-embedding-embeddinggemma-300m-qat" {
		t.Fatalf("model=%q", provider.Model())
	}
}

func TestDetectProviderForConfig_PrefersRepoConfiguredProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.Embedding.Provider = "lmstudio"
	cfg.Embedding.Model = "text-embedding-embeddinggemma-300m-qat"
	cfg.Embedding.BaseURL = "http://127.0.0.1:1234/v1"

	got := DetectProviderForConfig(cfg, "")
	if got != "openai_compat" {
		t.Fatalf("DetectProviderForConfig()=%q want openai_compat", got)
	}
}

func TestDetectProviderForConfig_DefaultsToOpenAICompatWhenRepoUnset(t *testing.T) {
	cfg := config.Config{}

	got := DetectProviderForConfig(cfg, "")
	if got != "openai_compat" {
		t.Fatalf("DetectProviderForConfig()=%q want openai_compat", got)
	}
}
