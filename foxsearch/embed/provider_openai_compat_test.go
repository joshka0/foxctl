package embed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestOpenAICompatProviderPrefersKnownModelDimensionsOverDefaultConfig(t *testing.T) {
	provider, err := NewOpenAICompatProvider(OpenAICompatConfig{
		Model:      "text-embedding-qwen3-embedding-8b",
		BaseURL:    "http://127.0.0.1:1234/v1",
		Dimensions: 1024,
	})
	if err != nil {
		t.Fatalf("NewOpenAICompatProvider: %v", err)
	}
	if provider.Dimensions() != 4096 {
		t.Fatalf("dimensions=%d want 4096", provider.Dimensions())
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
	}
	for input, want := range tests {
		if got := NormalizeProviderName(input); got != want {
			t.Fatalf("NormalizeProviderName(%q)=%q want %q", input, got, want)
		}
	}
}
