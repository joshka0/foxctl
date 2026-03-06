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

func TestOpenAICompatEmbedder_Embed_Success(t *testing.T) {
	t.Parallel()

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
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode req: %v", err)
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		if req["model"] != "test-model" {
			t.Errorf("model=%v want test-model", req["model"])
			http.Error(w, "bad model", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	res, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := len(res.Vector); got != 3 {
		t.Fatalf("vector dims=%d want 3", got)
	}
	if res.Model != "test-model" {
		t.Fatalf("model=%q want test-model", res.Model)
	}
}

func TestOpenAICompatEmbedder_Embed_Success_HostOnlyBaseURLAutoV1(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/embeddings" {
			t.Errorf("path=%s want /v1/embeddings", got)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"data": []map[string]any{
				{"embedding": []float32{0.1}},
			},
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL, "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	res, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := len(res.Vector); got != 1 {
		t.Fatalf("vector dims=%d want 1", got)
	}
}

func TestOpenAICompatEmbedder_Embed_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "bad input",
			},
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("Embed() error=%q want contains bad input", err.Error())
	}
}

func TestOpenAICompatEmbedder_Embed_HTTPErrorStringField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "bad input string",
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "bad input string") {
		t.Fatalf("Embed() error=%q want contains bad input string", err.Error())
	}
}

func TestOpenAICompatEmbedder_Embed_SuccessWithEmptyStringErrorField(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"error": "",
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	res, err := embedder.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if got := len(res.Vector); got != 2 {
		t.Fatalf("vector dims=%d want 2", got)
	}
}

func TestOpenAICompatEmbedder_Embed_HTTP200ErrorStringPayload(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "input too long",
			"data":  []map[string]any{},
		})
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "input too long") {
		t.Fatalf("Embed() error=%q want contains input too long", err.Error())
	}
}

func TestOpenAICompatEmbedder_Embed_HTTPErrorPlainTextBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("model overloaded"))
	}))
	defer srv.Close()

	embedder, err := NewOpenAICompatEmbedder(srv.URL+"/v1", "test-model", "", 2*time.Second)
	if err != nil {
		t.Fatalf("NewOpenAICompatEmbedder() error = %v", err)
	}

	_, err = embedder.Embed(context.Background(), "hello")
	if err == nil {
		t.Fatal("Embed() error=nil want non-nil")
	}
	if !strings.Contains(err.Error(), "model overloaded") {
		t.Fatalf("Embed() error=%q want contains plain-text body", err.Error())
	}
}
