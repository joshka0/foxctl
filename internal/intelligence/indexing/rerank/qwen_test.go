package rerank

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQwenProviderRerank(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authorization header")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req qwenRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != DefaultQwenRerankModel {
			t.Errorf("model=%q want %q", req.Model, DefaultQwenRerankModel)
		}

		resp := qwenRerankResponse{
			Results: []qwenRerankItem{
				{Index: 2, RelevanceScore: floatPtr(0.95)},
				{Index: 0, RelevanceScore: floatPtr(0.80)},
				{Index: 1, RelevanceScore: floatPtr(0.60)},
			},
		}
		resp.Usage.TotalTokens = 150
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newQwenTestProvider(t, handler, QwenConfig{
		APIKey:    "test-key",
		RateLimit: intPtr(0),
	})

	candidates := []Candidate{
		{ID: "doc1", Content: "First document about Go programming", OriginalScore: 0.7},
		{ID: "doc2", Content: "Second document about Python", OriginalScore: 0.8},
		{ID: "doc3", Content: "Third document about Go concurrency", OriginalScore: 0.6},
	}
	results, err := provider.Rerank(context.Background(), "Go concurrency patterns", candidates, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results=%d want 3", len(results))
	}
	if results[0].ID != "doc3" || results[1].ID != "doc1" || results[2].ID != "doc2" {
		t.Fatalf("unexpected order: %+v", results)
	}
	if results[0].RerankScore != 0.95 {
		t.Fatalf("rerank_score=%f want 0.95", results[0].RerankScore)
	}
	if results[0].OriginalRank != 3 || results[0].NewRank != 1 {
		t.Fatalf("ranks original=%d new=%d", results[0].OriginalRank, results[0].NewRank)
	}
}

func TestQwenProviderTopN(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req qwenRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.TopN == nil || *req.TopN != 2 {
			t.Fatalf("top_n=%v want 2", req.TopN)
		}
		resp := qwenRerankResponse{
			Results: []qwenRerankItem{
				{Index: 0, RelevanceScore: floatPtr(0.9)},
				{Index: 2, RelevanceScore: floatPtr(0.7)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newQwenTestProvider(t, handler, QwenConfig{RateLimit: intPtr(0)})
	results, err := provider.Rerank(context.Background(), "query", []Candidate{
		{ID: "doc1", Content: "First"},
		{ID: "doc2", Content: "Second"},
		{ID: "doc3", Content: "Third"},
	}, 2)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results=%d want 2", len(results))
	}
}

func TestQwenProviderDataResponseAndScoreBlend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := qwenRerankResponse{
			Data: []qwenRerankItem{
				{Index: 0, Score: floatPtr(0.8)},
				{Index: 1, Score: floatPtr(0.9)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newQwenTestProvider(t, handler, QwenConfig{
		RateLimit:  intPtr(0),
		ScoreBlend: 0.5,
	})
	results, err := provider.Rerank(context.Background(), "query", []Candidate{
		{ID: "doc1", Content: "First", OriginalScore: 0.6},
		{ID: "doc2", Content: "Second", OriginalScore: 0.2},
	}, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if results[0].ID != "doc1" {
		t.Fatalf("top=%q want doc1 with score blending", results[0].ID)
	}
	if math.Abs(results[0].FinalScore-0.7) > 0.001 {
		t.Fatalf("final_score=%f want 0.7", results[0].FinalScore)
	}
}

func TestQwenProviderEmptyCandidates(t *testing.T) {
	provider, err := NewQwenProvider(QwenConfig{RateLimit: intPtr(0)})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	results, err := provider.Rerank(context.Background(), "query", nil, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}
	if results == nil || len(results) != 0 {
		t.Fatalf("results=%v want empty slice", results)
	}
}

func TestQwenProviderRateLimit(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		resp := qwenRerankResponse{
			Results: []qwenRerankItem{{Index: 0, RelevanceScore: floatPtr(0.9)}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	wait := false
	provider := newQwenTestProvider(t, handler, QwenConfig{
		RateLimit:     intPtr(1),
		RateLimitWait: &wait,
	})
	provider.rateWindow = 100 * time.Millisecond

	candidates := []Candidate{{ID: "doc1", Content: "test"}}
	if _, err := provider.Rerank(context.Background(), "query", candidates, 0); err != nil {
		t.Fatalf("first rerank: %v", err)
	}
	if _, err := provider.Rerank(context.Background(), "query", candidates, 0); err == nil {
		t.Fatal("expected rate limit error")
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := provider.Rerank(context.Background(), "query", candidates, 0); err != nil {
		t.Fatalf("third rerank: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("server calls=%d want 2", callCount)
	}
}

func TestQwenProviderUsage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := qwenRerankResponse{
			Results: []qwenRerankItem{{Index: 0, RelevanceScore: floatPtr(0.9)}},
		}
		resp.Usage.TotalTokens = 100
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newQwenTestProvider(t, handler, QwenConfig{RateLimit: intPtr(0)})
	if _, err := provider.Rerank(context.Background(), "query", []Candidate{{ID: "doc1", Content: "test content"}}, 0); err != nil {
		t.Fatalf("rerank: %v", err)
	}

	usage := provider.Usage()
	if usage.Requests != 1 || usage.CandidatesProcessed != 1 || usage.TokensActual != 100 {
		t.Fatalf("usage=%+v", usage)
	}
	if usage.Provider != "qwen" || usage.Model != DefaultQwenRerankModel {
		t.Fatalf("usage provider/model=%s/%s", usage.Provider, usage.Model)
	}
	provider.ResetUsage()
	if provider.Usage().Requests != 0 {
		t.Fatal("usage not reset")
	}
}

func TestConfigFromEnvQwenDefaults(t *testing.T) {
	t.Setenv(EnvRerankEnabled, "true")
	t.Setenv(EnvRerankProvider, "qwen")
	t.Setenv(EnvRerankBaseURL, "http://localhost:8000/v1")
	t.Setenv(EnvRerankAPIKey, "test-key")
	t.Setenv(EnvRerankTopK, "100")
	t.Setenv(EnvRerankFinalK, "20")
	t.Setenv(EnvRerankScoreBlend, "0.3")
	t.Setenv(EnvRerankModel, "Qwen/Qwen3-Reranker-4B")

	cfg := FromEnv()
	if !cfg.Enabled || cfg.Provider != "qwen" || cfg.BaseURL != "http://localhost:8000/v1" || cfg.APIKey != "test-key" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.TopK != 100 || cfg.FinalK != 20 || cfg.Model != "Qwen/Qwen3-Reranker-4B" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.ScoreBlend == nil || *cfg.ScoreBlend != 0.3 {
		t.Fatalf("score_blend=%v want 0.3", cfg.ScoreBlend)
	}
}

type handlerTransport struct {
	handler http.Handler
}

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}

func newQwenTestProvider(t *testing.T, handler http.Handler, cfg QwenConfig) *QwenProvider {
	t.Helper()
	cfg.BaseURL = "http://mock"
	provider, err := NewQwenProvider(cfg)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	provider.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}
	return provider
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
