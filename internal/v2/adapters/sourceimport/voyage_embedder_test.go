package sourceimport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVoyageEmbedder_Embed_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
			http.Error(w, "bad method", http.StatusMethodNotAllowed)
			return
		}
		if got := r.URL.Path; got != "/v1/embeddings" {
			t.Errorf("path=%s want /v1/embeddings", got)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if auth != "Bearer test-key" {
			t.Errorf("Authorization=%q want %q", auth, "Bearer test-key")
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode req: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req["model"] != "voyage-3.5" {
			t.Errorf("model=%v want voyage-3.5", req["model"])
			http.Error(w, "bad model", http.StatusBadRequest)
			return
		}
		if req["input_type"] != "document" {
			t.Errorf("input_type=%v want document", req["input_type"])
			http.Error(w, "bad input_type", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"embedding": []float32{0.1, 0.2, 0.3},
					"index":     0,
				},
			},
			"model": "voyage-3.5",
			"usage": map[string]any{
				"total_tokens": 7,
			},
		})
	}))
	defer srv.Close()

	embedder, err := NewVoyageEmbedder(srv.URL+"/v1", "voyage-3.5", "test-key", 2*time.Second)
	if err != nil {
		t.Fatalf("NewVoyageEmbedder() error = %v", err)
	}

	res, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := embedder.Dimensions(); got != 1024 {
		t.Fatalf("Dimensions()=%d want 1024", got)
	}
	if got := len(res.Vector); got != 3 {
		t.Fatalf("vector dims=%d want 3", got)
	}
	if res.Model != "voyage-3.5" {
		t.Fatalf("model=%q want voyage-3.5", res.Model)
	}
}

func TestVoyageEmbedder_Embed_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": "invalid request",
		})
	}))
	defer srv.Close()

	embedder, err := NewVoyageEmbedder(srv.URL+"/v1", "voyage-3.5", "test-key", 2*time.Second)
	if err != nil {
		t.Fatalf("NewVoyageEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Fatalf("Embed() error=%q want contains invalid request", err.Error())
	}
}

func TestVoyageEmbedder_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode req: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req["model"] != "voyage-4" {
			t.Errorf("model=%v want voyage-4", req["model"])
			http.Error(w, "bad model", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{
					"object":    "embedding",
					"embedding": []float32{0.1, 0.2},
					"index":     0,
				},
			},
			"model": "voyage-4",
		})
	}))
	defer srv.Close()

	embedder, err := NewVoyageEmbedder(srv.URL+"/v1", "", "test-key", 2*time.Second)
	if err != nil {
		t.Fatalf("NewVoyageEmbedder() error = %v", err)
	}

	res, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if res.Model != "voyage-4" {
		t.Fatalf("model=%q want voyage-4", res.Model)
	}
}
