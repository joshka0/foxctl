package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVoyageProvider_Rerank(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing or incorrect authorization header")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req voyageRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.Model != "rerank-2.5" {
			t.Errorf("unexpected model: %s", req.Model)
		}

		// Return reranked results with scores
		resp := voyageRerankResponse{
			Object: "list",
			Data: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 2, RelevanceScore: 0.95}, // Third doc is most relevant
				{Index: 0, RelevanceScore: 0.80}, // First doc is second
				{Index: 1, RelevanceScore: 0.60}, // Second doc is third
			},
			Model: "rerank-2.5",
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{
				TotalTokens: 150,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newTestProvider(t, handler, VoyageConfig{
		APIKey:    "test-key",
		Model:     "rerank-2.5",
		RateLimit: intPtr(0), // Disable rate limiting for tests
	})

	candidates := []Candidate{
		{ID: "doc1", Content: "First document about Go programming", OriginalScore: 0.7},
		{ID: "doc2", Content: "Second document about Python", OriginalScore: 0.8},
		{ID: "doc3", Content: "Third document about Go concurrency", OriginalScore: 0.6},
	}

	ctx := context.Background()
	results, err := provider.Rerank(ctx, "Go concurrency patterns", candidates, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Check that results are sorted by final score (descending)
	if results[0].ID != "doc3" {
		t.Errorf("expected doc3 first (highest rerank score), got %s", results[0].ID)
	}
	if results[1].ID != "doc1" {
		t.Errorf("expected doc1 second, got %s", results[1].ID)
	}
	if results[2].ID != "doc2" {
		t.Errorf("expected doc2 third, got %s", results[2].ID)
	}

	// Check rerank scores
	if results[0].RerankScore != 0.95 {
		t.Errorf("expected rerank score 0.95, got %f", results[0].RerankScore)
	}

	// Check ranks
	if results[0].NewRank != 1 {
		t.Errorf("expected new rank 1, got %d", results[0].NewRank)
	}
	if results[0].OriginalRank != 3 {
		t.Errorf("expected original rank 3, got %d", results[0].OriginalRank)
	}
}

func TestVoyageProvider_TopK(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req voyageRerankRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if req.TopK == nil || *req.TopK != 2 {
			t.Errorf("expected top_k=2, got %v", req.TopK)
		}

		// Return only top 2
		resp := voyageRerankResponse{
			Object: "list",
			Data: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.9},
				{Index: 2, RelevanceScore: 0.7},
			},
			Model: "rerank-2.5",
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 100},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newTestProvider(t, handler, VoyageConfig{
		APIKey:    "test-key",
		RateLimit: intPtr(0),
	})

	candidates := []Candidate{
		{ID: "doc1", Content: "First"},
		{ID: "doc2", Content: "Second"},
		{ID: "doc3", Content: "Third"},
	}

	ctx := context.Background()
	results, err := provider.Rerank(ctx, "query", candidates, 2)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestVoyageProvider_ScoreBlend(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := voyageRerankResponse{
			Object: "list",
			Data: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.8},
				{Index: 1, RelevanceScore: 0.9},
			},
			Model: "rerank-2.5",
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 50},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newTestProvider(t, handler, VoyageConfig{
		APIKey:     "test-key",
		RateLimit:  intPtr(0),
		ScoreBlend: 0.5, // 50% rerank, 50% original
	})

	candidates := []Candidate{
		{ID: "doc1", Content: "First", OriginalScore: 0.6},  // blend: 0.5*0.8 + 0.5*0.6 = 0.7
		{ID: "doc2", Content: "Second", OriginalScore: 0.2}, // blend: 0.5*0.9 + 0.5*0.2 = 0.55
	}

	ctx := context.Background()
	results, err := provider.Rerank(ctx, "query", candidates, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	// With blending, doc1 should win despite lower rerank score
	if results[0].ID != "doc1" {
		t.Errorf("expected doc1 first with blending, got %s", results[0].ID)
	}

	// Check blended scores
	expectedBlend1 := 0.5*0.8 + 0.5*0.6 // 0.7
	expectedBlend2 := 0.5*0.9 + 0.5*0.2 // 0.55

	if abs(results[0].FinalScore-expectedBlend1) > 0.001 {
		t.Errorf("expected final score %f, got %f", expectedBlend1, results[0].FinalScore)
	}
	if abs(results[1].FinalScore-expectedBlend2) > 0.001 {
		t.Errorf("expected final score %f, got %f", expectedBlend2, results[1].FinalScore)
	}
}

func TestVoyageProvider_EmptyCandidates(t *testing.T) {
	provider, err := NewVoyageProvider(VoyageConfig{
		APIKey:    "test-key",
		RateLimit: intPtr(0),
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	ctx := context.Background()
	results, err := provider.Rerank(ctx, "query", nil, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if results == nil {
		t.Errorf("expected empty slice for empty candidates, got nil")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty candidates, got %d", len(results))
	}
}

func TestVoyageProvider_RateLimit(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := voyageRerankResponse{
			Object: "list",
			Data: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.9},
			},
			Model: "rerank-2.5",
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 10},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	rateLimitWait := false
	provider := newTestProvider(t, handler, VoyageConfig{
		APIKey:        "test-key",
		RateLimit:     intPtr(1),
		RateWindow:    100 * time.Millisecond,
		RateLimitWait: &rateLimitWait,
	})

	candidates := []Candidate{{ID: "doc1", Content: "test"}}
	ctx := context.Background()
	var err error

	// First call should succeed
	_, err = provider.Rerank(ctx, "query", candidates, 0)
	if err != nil {
		t.Fatalf("first rerank: %v", err)
	}

	// Second call should fail due to rate limit
	_, err = provider.Rerank(ctx, "query", candidates, 0)
	if err == nil {
		t.Error("expected rate limit error")
	}

	// Wait for rate limit window to pass
	time.Sleep(150 * time.Millisecond)

	// Third call should succeed
	_, err = provider.Rerank(ctx, "query", candidates, 0)
	if err != nil {
		t.Fatalf("third rerank: %v", err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 server calls, got %d", callCount)
	}
}

func TestVoyageProvider_Usage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := voyageRerankResponse{
			Object: "list",
			Data: []struct {
				Index          int     `json:"index"`
				RelevanceScore float64 `json:"relevance_score"`
			}{
				{Index: 0, RelevanceScore: 0.9},
			},
			Model: "rerank-2.5",
			Usage: struct {
				TotalTokens int `json:"total_tokens"`
			}{TotalTokens: 100},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	provider := newTestProvider(t, handler, VoyageConfig{
		APIKey:    "test-key",
		RateLimit: intPtr(0),
	})

	candidates := []Candidate{{ID: "doc1", Content: "test content"}}
	ctx := context.Background()

	_, err := provider.Rerank(ctx, "query", candidates, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	usage := provider.Usage()
	if usage.Requests != 1 {
		t.Errorf("expected 1 request, got %d", usage.Requests)
	}
	if usage.CandidatesProcessed != 1 {
		t.Errorf("expected 1 candidate, got %d", usage.CandidatesProcessed)
	}
	if usage.TokensActual != 100 {
		t.Errorf("expected 100 tokens, got %d", usage.TokensActual)
	}
	if usage.Provider != "voyage" {
		t.Errorf("expected provider 'voyage', got %s", usage.Provider)
	}
	if usage.Model != "rerank-2.5" {
		t.Errorf("expected model 'rerank-2.5', got %s", usage.Model)
	}

	provider.ResetUsage()
	usage = provider.Usage()
	if usage.Requests != 0 {
		t.Error("usage not reset")
	}
}

func TestNoOpProvider(t *testing.T) {
	provider := NewNoOpProvider()

	if provider.Model() != "noop" {
		t.Errorf("expected model 'noop', got %s", provider.Model())
	}

	candidates := []Candidate{
		{ID: "doc1", Content: "First", OriginalScore: 0.8},
		{ID: "doc2", Content: "Second", OriginalScore: 0.6},
	}

	ctx := context.Background()
	results, err := provider.Rerank(ctx, "query", candidates, 0)
	if err != nil {
		t.Fatalf("rerank: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// NoOp preserves original order
	if results[0].ID != "doc1" || results[1].ID != "doc2" {
		t.Error("NoOp should preserve original order")
	}

	// Scores should equal original scores
	if results[0].RerankScore != 0.8 || results[0].FinalScore != 0.8 {
		t.Error("NoOp should use original score as rerank score")
	}
}

func TestConfig_FromEnv(t *testing.T) {
	t.Setenv("AGENTCTL_RERANK_ENABLED", "true")
	t.Setenv("AGENTCTL_RERANK_TOP_K", "100")
	t.Setenv("AGENTCTL_RERANK_FINAL_K", "20")
	t.Setenv("AGENTCTL_RERANK_SCORE_BLEND", "0.3")
	t.Setenv("AGENTCTL_RERANK_MODEL", "rerank-2")

	cfg := FromEnv()

	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.TopK != 100 {
		t.Errorf("expected TopK=100, got %d", cfg.TopK)
	}
	if cfg.FinalK != 20 {
		t.Errorf("expected FinalK=20, got %d", cfg.FinalK)
	}
	if cfg.ScoreBlend == nil || *cfg.ScoreBlend != 0.3 {
		t.Errorf("expected ScoreBlend=0.3, got %v", cfg.ScoreBlend)
	}
	if cfg.Model != "rerank-2" {
		t.Errorf("expected Model='rerank-2', got %s", cfg.Model)
	}
}

func TestConfig_Merge(t *testing.T) {
	base := Config{
		Enabled:    false,
		TopK:       50,
		FinalK:     10,
		ScoreBlend: floatPtr(0.0),
		Model:      "rerank-2.5",
	}

	override := Config{
		Enabled: true,
		TopK:    100,
		// FinalK not set (0)
		// ScoreBlend not set (nil)
		Model: "rerank-2",
	}

	merged := base.Merge(override)

	if !merged.Enabled {
		t.Error("Enabled should be overridden to true")
	}
	if merged.TopK != 100 {
		t.Errorf("TopK should be 100, got %d", merged.TopK)
	}
	if merged.FinalK != 10 {
		t.Errorf("FinalK should remain 10, got %d", merged.FinalK)
	}
	if merged.Model != "rerank-2" {
		t.Errorf("Model should be 'rerank-2', got %s", merged.Model)
	}
	// ScoreBlend should remain from base since override is nil
	if merged.ScoreBlend == nil || *merged.ScoreBlend != 0.0 {
		t.Errorf("ScoreBlend should remain 0.0, got %v", merged.ScoreBlend)
	}
}

func TestConfig_Merge_ScoreBlendOverride(t *testing.T) {
	base := Config{
		Enabled:    false,
		TopK:       50,
		FinalK:     10,
		ScoreBlend: floatPtr(0.5),
		Model:      "rerank-2.5",
	}

	// Explicitly set ScoreBlend to 0.0
	override := Config{
		ScoreBlend: floatPtr(0.0),
	}

	merged := base.Merge(override)

	// ScoreBlend should be overridden to 0.0
	if merged.ScoreBlend == nil || *merged.ScoreBlend != 0.0 {
		t.Errorf("ScoreBlend should be overridden to 0.0, got %v", merged.ScoreBlend)
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

func newTestProvider(t *testing.T, handler http.Handler, cfg VoyageConfig) *VoyageProvider {
	t.Helper()
	cfg.BaseURL = "http://mock"
	provider, err := NewVoyageProvider(cfg)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	provider.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}
	return provider
}

// Helper functions

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func intPtr(i int) *int {
	return &i
}

func floatPtr(f float64) *float64 {
	return &f
}
