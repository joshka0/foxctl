package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
