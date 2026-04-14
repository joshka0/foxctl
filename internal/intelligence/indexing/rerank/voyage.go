package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"
)

// Voyage rerank pricing (per 1M tokens, as of Dec 2024)
// See: https://docs.voyageai.com/docs/pricing
const (
	// Rerank2PricePerMillionTokens is the price for rerank-2 model.
	Rerank2PricePerMillionTokens = 0.05

	// Rerank25PricePerMillionTokens is the price for rerank-2.5 model.
	// Instruction-following, 32K context, ~7-12% improvement over rerank-2.
	Rerank25PricePerMillionTokens = 0.05

	// RerankLitePricePerMillionTokens is the price for rerank-lite-1 model.
	// Budget option for reranking.
	RerankLitePricePerMillionTokens = 0.02
)

// VoyageProvider implements Provider using Voyage AI's rerank API.
// It supports instruction-following reranking with rerank-2.5 model.
// See: https://docs.voyageai.com/docs/reranker
type VoyageProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client

	// Rate limiting for free tier (3 RPM without payment method)
	rateLimitMu   sync.Mutex
	requestTimes  []time.Time
	rateLimit     int           // Max requests per window (default: 3)
	rateWindow    time.Duration // Window duration (default: 62s)
	rateLimitWait bool          // Whether to wait or error on rate limit

	// Score blending configuration
	scoreBlend float64 // 0=rerank only, 1=original only

	// Usage tracking
	tracker *usageTracker
}

// VoyageConfig holds configuration for the Voyage AI reranking provider.
type VoyageConfig struct {
	// APIKey is the Voyage AI API key. If empty, reads from VOYAGE_API_KEY env var.
	APIKey string

	// Model is the reranking model to use. Default: "rerank-2.5"
	// Options: rerank-2.5, rerank-2, rerank-lite-1
	Model string

	// BaseURL is the API base URL. Default: "https://api.voyageai.com/v1"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s
	Timeout time.Duration

	// RateLimit is max requests per window. Default: no limit (nil or 0 = disabled).
	// Set to >0 to enable rate limiting.
	RateLimit *int

	// RateWindow is the rate limit window duration. Default: 62s
	RateWindow time.Duration

	// RateLimitWait controls behavior when rate limited.
	// If true, waits until a slot is available. If false, returns error immediately.
	// Default: true (wait)
	RateLimitWait *bool

	// ScoreBlend controls how to combine rerank and original scores.
	// 0.0 = use rerank score only (default)
	// 1.0 = use original score only
	// 0.3 = 70% rerank + 30% original
	ScoreBlend float64
}

// NewVoyageProvider creates a new Voyage AI reranking provider.
func NewVoyageProvider(cfg VoyageConfig) (*VoyageProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("VOYAGE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("voyage rerank: API key required (set VOYAGE_API_KEY or provide in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "rerank-2.5" // Latest instruction-following model
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.voyageai.com/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	// Rate limiting: nil → no limit, *0 → disabled, *>0 → use value
	var rateLimit int
	if cfg.RateLimit != nil {
		rateLimit = *cfg.RateLimit
	}
	// Default: no rate limit (0 = disabled)
	rateWindow := cfg.RateWindow
	if rateWindow == 0 {
		rateWindow = 62 * time.Second // Slightly over 1 minute to be safe
	}
	rateLimitWait := true
	if cfg.RateLimitWait != nil {
		rateLimitWait = *cfg.RateLimitWait
	}

	// Validate score blend
	scoreBlend := cfg.ScoreBlend
	if scoreBlend < 0 {
		scoreBlend = 0
	}
	if scoreBlend > 1 {
		scoreBlend = 1
	}

	return &VoyageProvider{
		apiKey:        apiKey,
		model:         model,
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: timeout},
		requestTimes:  make([]time.Time, 0, rateLimit),
		rateLimit:     rateLimit,
		rateWindow:    rateWindow,
		rateLimitWait: rateLimitWait,
		scoreBlend:    scoreBlend,
		tracker:       newUsageTracker("voyage", model),
	}, nil
}

// voyageRerankRequest is the request body for the Voyage rerank API.
type voyageRerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	Model     string   `json:"model"`
	TopK      *int     `json:"top_k,omitempty"`      // Optional: return top K results
	Truncate  bool     `json:"truncation,omitempty"` // Truncate long docs (default: true)
}

// voyageRerankResponse is the response from the Voyage rerank API.
type voyageRerankResponse struct {
	Object string `json:"object"` // "list"
	Data   []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// voyageErrorResponse is the error response from the Voyage API.
type voyageErrorResponse struct {
	Detail string `json:"detail"`
}

// waitForRateLimit blocks until a request slot is available.
func (p *VoyageProvider) waitForRateLimit(ctx context.Context) error {
	if p.rateLimit <= 0 {
		return nil // Rate limiting disabled
	}

	for {
		p.rateLimitMu.Lock()

		// Clean up old request times outside the window
		now := time.Now()
		cutoff := now.Add(-p.rateWindow)
		validTimes := make([]time.Time, 0, len(p.requestTimes))
		for _, t := range p.requestTimes {
			if t.After(cutoff) {
				validTimes = append(validTimes, t)
			}
		}
		p.requestTimes = validTimes

		// Check if we have capacity
		if len(p.requestTimes) < p.rateLimit {
			p.requestTimes = append(p.requestTimes, now)
			p.rateLimitMu.Unlock()
			return nil
		}

		// Calculate wait time until oldest request expires
		oldestTime := p.requestTimes[0]
		waitDuration := oldestTime.Add(p.rateWindow).Sub(now) + 100*time.Millisecond

		p.rateLimitMu.Unlock()

		if !p.rateLimitWait {
			return fmt.Errorf("voyage rerank: rate limit exceeded (3 RPM free tier); retry in %v", waitDuration.Round(time.Second))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Continue to retry
		}
	}
}

// Rerank reorders candidates by relevance to the query.
func (p *VoyageProvider) Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	if len(candidates) == 0 {
		return []RankedResult{}, nil
	}

	// Voyage API supports up to 1000 documents per request
	const maxBatchSize = 1000
	if len(candidates) > maxBatchSize {
		log.Warn().
			Int("total_candidates", len(candidates)).
			Int("max_batch_size", maxBatchSize).
			Msg("Truncating candidates to max batch size; batching not yet implemented")
		candidates = candidates[:maxBatchSize]
	}

	return p.rerankInternal(ctx, query, candidates, topK)
}

func (p *VoyageProvider) rerankInternal(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	start := time.Now()
	candidatesCount := len(candidates)
	estimatedTokens := estimateTokens(query, candidates)

	// Build document list
	documents := make([]string, len(candidates))
	for i, c := range candidates {
		documents[i] = c.Content
	}

	url := fmt.Sprintf("%s/rerank", p.baseURL)
	reqBody := voyageRerankRequest{
		Query:     query,
		Documents: documents,
		Model:     p.model,
		Truncate:  true, // Let API handle truncation
	}
	if topK > 0 {
		reqBody.TopK = &topK
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("voyage rerank: marshal request: %w", err)
	}

	const maxRetries = 5
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := p.waitForRateLimit(ctx); err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage rerank: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

		resp, err := p.httpClient.Do(req)
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage rerank: request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage rerank: read response: %w", err)
		}

		// Handle 429 rate limit with retry
		if resp.StatusCode == http.StatusTooManyRequests {
			var errResp voyageErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != "" {
				lastErr = fmt.Errorf("voyage rerank: rate limited (429): %s", errResp.Detail)
			} else {
				lastErr = fmt.Errorf("voyage rerank: rate limited (429)")
			}

			if attempt < maxRetries {
				waitTime := p.rateWindow + time.Duration(attempt)*10*time.Second
				select {
				case <-ctx.Done():
					p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusCanceled, ctx.Err())
					return nil, ctx.Err()
				case <-time.After(waitTime):
					continue
				}
			}
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, lastErr)
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			var errResp voyageErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != "" {
				err := fmt.Errorf("voyage rerank: API error %d: %s", resp.StatusCode, errResp.Detail)
				p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}
			err := fmt.Errorf("voyage rerank: API returned status %d: %s", resp.StatusCode, string(respBody))
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		var rerankResp voyageRerankResponse
		if err := json.Unmarshal(respBody, &rerankResp); err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage rerank: unmarshal response: %w", err)
		}

		// Build results with scores
		results := make([]RankedResult, len(rerankResp.Data))
		for i, item := range rerankResp.Data {
			if item.Index < 0 || item.Index >= len(candidates) {
				err := fmt.Errorf("voyage rerank: invalid index %d", item.Index)
				p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}

			c := candidates[item.Index]
			rerankScore := item.RelevanceScore
			finalScore := (1-p.scoreBlend)*rerankScore + p.scoreBlend*c.OriginalScore

			results[i] = RankedResult{
				ID:            c.ID,
				Content:       c.Content,
				RerankScore:   rerankScore,
				OriginalScore: c.OriginalScore,
				FinalScore:    finalScore,
				OriginalRank:  item.Index + 1,
				Metadata:      c.Metadata,
			}
		}

		// Sort by final score descending
		sort.Slice(results, func(i, j int) bool {
			return results[i].FinalScore > results[j].FinalScore
		})

		// Assign new ranks
		for i := range results {
			results[i].NewRank = i + 1
		}

		// Track usage
		actualTokens := int64(rerankResp.Usage.TotalTokens)
		costUSD := float64(actualTokens) * p.pricePerToken()
		p.tracker.record(1, candidatesCount, estimatedTokens, actualTokens, costUSD)
		p.emitEvent(ctx, start, candidatesCount, estimatedTokens, actualTokens, observability.StatusOK, nil)

		return results, nil
	}

	p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, lastErr)
	return nil, lastErr
}

// Model returns the model identifier.
func (p *VoyageProvider) Model() string {
	return p.model
}

// Usage returns cumulative usage statistics since provider creation.
func (p *VoyageProvider) Usage() Usage {
	return p.tracker.get()
}

// ResetUsage resets the usage counters to zero.
func (p *VoyageProvider) ResetUsage() {
	p.tracker.reset()
}

// pricePerToken returns the price per token based on the model.
func (p *VoyageProvider) pricePerToken() float64 {
	switch p.model {
	case "rerank-lite-1":
		return RerankLitePricePerMillionTokens / 1_000_000
	case "rerank-2", "rerank-2.5":
		return Rerank25PricePerMillionTokens / 1_000_000
	default:
		return Rerank25PricePerMillionTokens / 1_000_000
	}
}

// emitEvent emits a wide event for observability.
func (p *VoyageProvider) emitEvent(ctx context.Context, start time.Time, candidatesCount int, estimatedTokens, actualTokens int64, status observability.Status, err error) {
	durationMS := time.Since(start).Milliseconds()

	costUSD := 0.0
	if actualTokens > 0 {
		costUSD = float64(actualTokens) * p.pricePerToken()
	}

	event := &observability.WideEvent{
		Ts:         time.Now().UTC(),
		TraceID:    observability.TraceIDFromContext(ctx),
		SpanID:     ulid.Make().String(),
		Service:    "foxctl",
		Component:  observability.ComponentSkill,
		Operation:  "rerank.execute",
		Command:    "voyage",
		Subtype:    p.model,
		Status:     status,
		DurationMS: durationMS,
		Data: map[string]any{
			"provider":         "voyage",
			"model":            p.model,
			"candidates_count": candidatesCount,
			"tokens_estimated": estimatedTokens,
			"tokens_actual":    actualTokens,
			"cost_usd":         costUSD,
			"score_blend":      p.scoreBlend,
		},
	}

	if err != nil {
		event.ErrorMessage = err.Error()
	}

	observability.Emit(ctx, event)
}
