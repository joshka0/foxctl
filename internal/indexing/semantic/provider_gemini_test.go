package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewGeminiProvider_MissingAPIKey(t *testing.T) {
	// Clear env var for this test
	t.Setenv("GEMINI_API_KEY", "")

	_, err := NewGeminiProvider(GeminiConfig{})
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestNewGeminiProvider_Defaults(t *testing.T) {
	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	if provider.Model() != "text-embedding-004" {
		t.Errorf("expected model text-embedding-004, got %s", provider.Model())
	}
	if provider.Dimensions() != 768 {
		t.Errorf("expected dimensions 768, got %d", provider.Dimensions())
	}
}

func TestNewGeminiProvider_GeminiEmbedding001(t *testing.T) {
	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey: "test-key",
		Model:  "gemini-embedding-001",
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	if provider.Dimensions() != 3072 {
		t.Errorf("expected dimensions 3072 for gemini-embedding-001, got %d", provider.Dimensions())
	}
}

func TestGeminiProvider_Embed(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		// Return mock embedding
		resp := geminiEmbedResponse{
			Embedding: struct {
				Values []float32 `json:"values"`
			}{
				Values: make([]float32, 768),
			},
		}
		// Set first few values to something identifiable
		resp.Embedding.Values[0] = 0.1
		resp.Embedding.Values[1] = 0.2

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	embedding, err := provider.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 768 {
		t.Errorf("expected embedding length 768, got %d", len(embedding))
	}
	if embedding[0] != 0.1 || embedding[1] != 0.2 {
		t.Error("embedding values don't match expected")
	}
}

func TestGeminiProvider_EmbedBatch(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return mock batch embeddings
		resp := geminiBatchEmbedResponse{
			Embeddings: []struct {
				Values []float32 `json:"values"`
			}{
				{Values: make([]float32, 768)},
				{Values: make([]float32, 768)},
				{Values: make([]float32, 768)},
			},
		}
		// Set identifiable values
		resp.Embeddings[0].Values[0] = 0.1
		resp.Embeddings[1].Values[0] = 0.2
		resp.Embeddings[2].Values[0] = 0.3

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	embeddings, err := provider.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(embeddings) != 3 {
		t.Errorf("expected 3 embeddings, got %d", len(embeddings))
	}

	// Verify each embedding
	for i, emb := range embeddings {
		if len(emb) != 768 {
			t.Errorf("embedding %d: expected length 768, got %d", i, len(emb))
		}
	}

	// Verify identifiable values
	if embeddings[0][0] != 0.1 || embeddings[1][0] != 0.2 || embeddings[2][0] != 0.3 {
		t.Error("embedding values don't match expected")
	}
}

func TestGeminiProvider_Embed_APIError(t *testing.T) {
	// Mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		resp := geminiEmbedResponse{
			Error: &geminiError{
				Code:    400,
				Message: "Invalid API key",
				Status:  "INVALID_ARGUMENT",
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey:  "bad-key",
		BaseURL: server.URL,
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	_, err = provider.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for API error response")
	}
}

func TestGeminiProvider_EmbedBatch_Empty(t *testing.T) {
	provider, err := NewGeminiProvider(GeminiConfig{
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("NewGeminiProvider failed: %v", err)
	}

	embeddings, err := provider.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if embeddings != nil {
		t.Errorf("expected nil for empty batch, got %v", embeddings)
	}
}
