package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/platform/observability"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"
)

const (
	DefaultQwenRerankModel   = "Qwen/Qwen3-Reranker-0.6B"
	DefaultQwenRerankBaseURL = "http://127.0.0.1:1234/v1"
)

// QwenProvider implements Provider using a local OpenAI-compatible /rerank endpoint.
type QwenProvider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client

	rateLimitMu   sync.Mutex
	requestTimes  []time.Time
	rateLimit     int
	rateWindow    time.Duration
	rateLimitWait bool

	scoreBlend float64
	tracker    *usageTracker
}

// QwenConfig holds configuration for the Qwen reranking provider.
type QwenConfig struct {
	// APIKey is optional for local servers. If empty, FOXCTL_RERANK_API_KEY,
	// FOXCTL_EMBEDDING_API_KEY, and OPENAI_API_KEY are checked.
	APIKey string

	// Model is the reranking model to use. Default: Qwen/Qwen3-Reranker-0.6B.
	Model string

	// BaseURL is the OpenAI-compatible base URL. Default: http://127.0.0.1:1234/v1.
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s.
	Timeout time.Duration

	// RateLimit is max requests per minute. nil or 0 disables client-side limiting.
	RateLimit *int

	// RateLimitWait controls behavior when rate limited. Default: true.
	RateLimitWait *bool

	// ScoreBlend controls how to combine rerank and original scores.
	// 0.0 = use rerank score only, 1.0 = use original score only.
	ScoreBlend float64
}

// NewQwenProvider creates a local OpenAI-compatible Qwen reranking provider.
func NewQwenProvider(cfg QwenConfig) (*QwenProvider, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = DefaultQwenRerankModel
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv(EnvRerankBaseURL)), "/")
	}
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(os.Getenv("FOXCTL_EMBEDDING_BASE_URL")), "/")
	}
	if baseURL == "" {
		baseURL = DefaultQwenRerankBaseURL
	}

	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(EnvRerankAPIKey))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("FOXCTL_EMBEDDING_API_KEY"))
	}
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	var rateLimit int
	if cfg.RateLimit != nil {
		rateLimit = *cfg.RateLimit
	}
	rateLimitWait := true
	if cfg.RateLimitWait != nil {
		rateLimitWait = *cfg.RateLimitWait
	}

	scoreBlend := cfg.ScoreBlend
	if scoreBlend < 0 {
		scoreBlend = 0
	}
	if scoreBlend > 1 {
		scoreBlend = 1
	}

	return &QwenProvider{
		apiKey:        apiKey,
		model:         model,
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: timeout},
		requestTimes:  make([]time.Time, 0, max(rateLimit, 0)),
		rateLimit:     rateLimit,
		rateWindow:    time.Minute,
		rateLimitWait: rateLimitWait,
		scoreBlend:    scoreBlend,
		tracker:       newUsageTracker("qwen", model),
	}, nil
}

type qwenRerankRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
	TopN      *int     `json:"top_n,omitempty"`
}

type qwenRerankResponse struct {
	Model   string           `json:"model"`
	Results []qwenRerankItem `json:"results"`
	Data    []qwenRerankItem `json:"data"`
	Usage   struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type qwenRerankItem struct {
	Index          int      `json:"index"`
	RelevanceScore *float64 `json:"relevance_score,omitempty"`
	Score          *float64 `json:"score,omitempty"`
}

type qwenErrorResponse struct {
	Detail  string `json:"detail"`
	Message string `json:"message"`
	Error   any    `json:"error"`
}

func (p *QwenProvider) waitForRateLimit(ctx context.Context) error {
	if p.rateLimit <= 0 {
		return nil
	}

	for {
		p.rateLimitMu.Lock()
		now := time.Now()
		cutoff := now.Add(-p.rateWindow)
		validTimes := make([]time.Time, 0, len(p.requestTimes))
		for _, t := range p.requestTimes {
			if t.After(cutoff) {
				validTimes = append(validTimes, t)
			}
		}
		p.requestTimes = validTimes
		if len(p.requestTimes) < p.rateLimit {
			p.requestTimes = append(p.requestTimes, now)
			p.rateLimitMu.Unlock()
			return nil
		}

		oldestTime := p.requestTimes[0]
		waitDuration := oldestTime.Add(p.rateWindow).Sub(now) + 100*time.Millisecond
		p.rateLimitMu.Unlock()

		if !p.rateLimitWait {
			return fmt.Errorf("qwen rerank: rate limit exceeded; retry in %v", waitDuration.Round(time.Second))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
		}
	}
}

// Rerank reorders candidates by relevance to the query.
func (p *QwenProvider) Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	if len(candidates) == 0 {
		return []RankedResult{}, nil
	}

	const maxBatchSize = 1000
	if len(candidates) > maxBatchSize {
		log.Warn().
			Int("total_candidates", len(candidates)).
			Int("max_batch_size", maxBatchSize).
			Msg("Truncating candidates to max rerank batch size")
		candidates = candidates[:maxBatchSize]
	}

	return p.rerankInternal(ctx, query, candidates, topK)
}

func (p *QwenProvider) rerankInternal(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error) {
	start := time.Now()
	candidatesCount := len(candidates)
	estimatedTokens := estimateTokens(query, candidates)

	documents := make([]string, len(candidates))
	for i, c := range candidates {
		documents[i] = c.Content
	}

	reqBody := qwenRerankRequest{
		Query:     query,
		Documents: documents,
		Model:     p.model,
	}
	if topK > 0 {
		reqBody.TopN = &topK
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("qwen rerank: marshal request: %w", err)
	}

	const maxRetries = 2
	var lastErr error
	url := p.rerankURL()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := p.waitForRateLimit(ctx); err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("qwen rerank: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}

		resp, err := p.httpClient.Do(req)
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("qwen rerank: request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("qwen rerank: read response: %w", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			lastErr = qwenHTTPError(resp.StatusCode, respBody)
			if attempt < maxRetries {
				waitTime := time.Second * time.Duration(attempt+1)
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

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			err := qwenHTTPError(resp.StatusCode, respBody)
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		var rerankResp qwenRerankResponse
		if err := json.Unmarshal(respBody, &rerankResp); err != nil {
			p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("qwen rerank: unmarshal response: %w", err)
		}

		items := rerankResp.Results
		if len(items) == 0 {
			items = rerankResp.Data
		}

		results := make([]RankedResult, len(items))
		for i, item := range items {
			if item.Index < 0 || item.Index >= len(candidates) {
				err := fmt.Errorf("qwen rerank: invalid index %d", item.Index)
				p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}

			score, ok := qwenItemScore(item)
			if !ok {
				err := fmt.Errorf("qwen rerank: missing score for index %d", item.Index)
				p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}

			c := candidates[item.Index]
			rerankScore := normalizeRerankScore(score)
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

		sort.Slice(results, func(i, j int) bool {
			if results[i].FinalScore != results[j].FinalScore {
				return results[i].FinalScore > results[j].FinalScore
			}
			return results[i].OriginalRank < results[j].OriginalRank
		})
		for i := range results {
			results[i].NewRank = i + 1
		}

		actualTokens := int64(rerankResp.Usage.TotalTokens)
		p.tracker.record(1, candidatesCount, estimatedTokens, actualTokens, 0)
		p.emitEvent(ctx, start, candidatesCount, estimatedTokens, actualTokens, observability.StatusOK, nil)
		return results, nil
	}

	p.emitEvent(ctx, start, candidatesCount, estimatedTokens, 0, observability.StatusError, lastErr)
	return nil, lastErr
}

func (p *QwenProvider) rerankURL() string {
	base := strings.TrimRight(p.baseURL, "/")
	if strings.HasSuffix(base, "/rerank") {
		return base
	}
	return base + "/rerank"
}

func qwenItemScore(item qwenRerankItem) (float64, bool) {
	if item.RelevanceScore != nil {
		return *item.RelevanceScore, true
	}
	if item.Score != nil {
		return *item.Score, true
	}
	return 0, false
}

func normalizeRerankScore(score float64) float64 {
	if math.IsNaN(score) {
		return 0
	}
	if score >= 0 && score <= 1 {
		return score
	}
	if score > 36 {
		return 1
	}
	if score < -36 {
		return 0
	}
	return 1 / (1 + math.Exp(-score))
}

func qwenHTTPError(status int, body []byte) error {
	var errResp qwenErrorResponse
	if json.Unmarshal(body, &errResp) == nil {
		switch {
		case strings.TrimSpace(errResp.Detail) != "":
			return fmt.Errorf("qwen rerank: API error %d: %s", status, errResp.Detail)
		case strings.TrimSpace(errResp.Message) != "":
			return fmt.Errorf("qwen rerank: API error %d: %s", status, errResp.Message)
		case errResp.Error != nil:
			return fmt.Errorf("qwen rerank: API error %d: %v", status, errResp.Error)
		}
	}
	if len(body) > 0 {
		return fmt.Errorf("qwen rerank: API returned status %d: %s", status, string(body))
	}
	return fmt.Errorf("qwen rerank: API returned status %d", status)
}

// Model returns the model identifier.
func (p *QwenProvider) Model() string {
	return p.model
}

// Usage returns cumulative usage statistics since provider creation.
func (p *QwenProvider) Usage() Usage {
	return p.tracker.get()
}

// ResetUsage resets the usage counters to zero.
func (p *QwenProvider) ResetUsage() {
	p.tracker.reset()
}

func (p *QwenProvider) emitEvent(ctx context.Context, start time.Time, candidatesCount int, estimatedTokens, actualTokens int64, status observability.Status, err error) {
	event := &observability.Event{
		Timestamp: time.Now().UTC(),
		TraceID:   observability.TraceIDFromContext(ctx),
		SpanID:    ulid.Make().String(),
		Operation: "rerank.execute",
		Name:      "qwen",
		Status:    status,
		Duration:  time.Since(start),
		Data: map[string]any{
			observability.DataKeyService:   "foxctl",
			observability.DataKeyComponent: observability.ComponentSkill,
			observability.DataKeySubtype:   p.model,
			"provider":                     "qwen",
			"model":                        p.model,
			"candidates_count":             candidatesCount,
			"tokens_estimated":             estimatedTokens,
			"tokens_actual":                actualTokens,
			"score_blend":                  p.scoreBlend,
		},
	}
	if err != nil {
		event.ErrorMessage = err.Error()
	}
	observability.Emit(ctx, event)
}
