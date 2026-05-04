package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestOpenAICompatProviderEmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/embeddings" {
			t.Fatalf("path=%q want /v1/embeddings", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization=%q", got)
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Model != "text-embedding-embeddinggemma-300m-qat" {
			t.Fatalf("model=%q", body.Model)
		}
		if len(body.Input) != 2 {
			t.Fatalf("inputs=%d", len(body.Input))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": body.Model,
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0.3, 0.4}},
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer server.Close()

	provider, err := NewOpenAICompatProvider(OpenAICompatConfig{
		APIKey:  "test-key",
		Model:   "text-embedding-embeddinggemma-300m-qat",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	vecs, err := provider.EmbedBatch(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatalf("EmbedBatch: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len(vecs)=%d", len(vecs))
	}
	if provider.Dimensions() != 768 && provider.Dimensions() != 2 {
		t.Fatalf("dimensions=%d", provider.Dimensions())
	}
	if got := vecs[0][0]; got != 0.1 {
		t.Fatalf("first vector first value=%f", got)
	}
}

func TestOpenAICompatProviderConnectionRefusedIsActionable(t *testing.T) {
	provider, err := NewOpenAICompatProvider(OpenAICompatConfig{
		Model:   "text-embedding-embeddinggemma-300m-qat",
		BaseURL: "http://127.0.0.1:1/v1",
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	_, err = provider.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected embedding request error")
	}
	var svcErr *EmbeddingServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T want EmbeddingServiceError: %v", err, err)
	}
	if svcErr.Kind != "connection_refused" {
		t.Fatalf("kind=%q want connection_refused", svcErr.Kind)
	}
	if !strings.Contains(err.Error(), "start LM Studio") {
		t.Fatalf("error=%q missing LM Studio hint", err.Error())
	}
}

func TestOpenAICompatProviderStatusErrorIsActionable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "model not found"},
		})
	}))
	defer server.Close()

	provider, err := NewOpenAICompatProvider(OpenAICompatConfig{
		Model:   "missing-model",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	_, err = provider.Embed(context.Background(), "test")
	if err == nil {
		t.Fatal("expected embedding status error")
	}
	var svcErr *EmbeddingServiceError
	if !errors.As(err, &svcErr) {
		t.Fatalf("error type=%T want EmbeddingServiceError: %v", err, err)
	}
	if svcErr.Kind != "http_404" {
		t.Fatalf("kind=%q want http_404", svcErr.Kind)
	}
	if !strings.Contains(err.Error(), "/v1/embeddings") {
		t.Fatalf("error=%q missing endpoint hint", err.Error())
	}
}

func TestNormalizeEmbeddingProviderName(t *testing.T) {
	tests := map[string]string{
		"openai_compat":     "openai_compat",
		"openai-compatible": "openai_compat",
		"lmstudio":          "openai_compat",
		"voyage":            "voyage",
	}
	for input, want := range tests {
		if got := normalizeEmbeddingProviderName(input); got != want {
			t.Fatalf("normalizeEmbeddingProviderName(%q)=%q want %q", input, got, want)
		}
	}
}

func TestNewProviderForModel_WorkspaceVoyageOverrideWins(t *testing.T) {
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
  provider: voyage
  model: voyage-3.5
`), 0o644); err != nil {
		t.Fatalf("write workspace config: %v", err)
	}

	cfg, err := config.Load(context.Background(), config.WithWorkspacePath(workspace))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	provider, err := NewProviderForModel("voyage-3.5", cfg, WithProvider(cfg.Embedding.Provider), WithAPIKey(cfg.Embedding.APIKey), WithBaseURL(cfg.Embedding.BaseURL), WithVoyageKey("test-voyage-key"))
	if err != nil {
		t.Fatalf("NewProviderForModel: %v", err)
	}
	if _, ok := provider.(*VoyageProvider); !ok {
		t.Fatalf("expected VoyageProvider, got %T", provider)
	}
}

func TestNewProviderForModel_GemmaModelOverridesVoyageProvider(t *testing.T) {
	cfg := config.Config{}
	cfg.Embedding.Provider = "voyage"
	cfg.Embedding.Model = "text-embedding-embeddinggemma-300m-qat"
	cfg.Embedding.BaseURL = "http://127.0.0.1:1234/v1"

	provider, err := NewProviderForModel(
		"text-embedding-embeddinggemma-300m-qat",
		cfg,
		WithProvider(cfg.Embedding.Provider),
		WithBaseURL(cfg.Embedding.BaseURL),
		WithVoyageKey("test-voyage-key"),
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

	got := DetectProviderForConfig(cfg, "voyage-key-present", "")
	if got != "openai_compat" {
		t.Fatalf("DetectProviderForConfig()=%q want openai_compat", got)
	}
}

func TestDetectProviderForConfig_FallsBackToEnvWhenRepoUnset(t *testing.T) {
	cfg := config.Config{}

	got := DetectProviderForConfig(cfg, "voyage-key-present", "")
	if got != "voyage" {
		t.Fatalf("DetectProviderForConfig()=%q want voyage", got)
	}
}
