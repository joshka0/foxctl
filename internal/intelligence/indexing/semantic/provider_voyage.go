package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog/log"
)

// Voyage embedding pricing (per 1M tokens, as of Dec 2024)
// See: https://docs.voyageai.com/docs/pricing
const (
	// Voyage3LargePricePerMillionTokens is the price for voyage-3-large model.
	// Best for high-quality text retrieval (nDCG@10: 0.837).
	Voyage3LargePricePerMillionTokens = 0.18

	// VoyageCode3PricePerMillionTokens is the price for voyage-code-3 model.
	// Best for code retrieval (13.80% better than OpenAI-v3-large).
	VoyageCode3PricePerMillionTokens = 0.18

	// Voyage35PricePerMillionTokens is the price for voyage-3.5 model.
	// Good balance of quality (nDCG@10: 0.816) and cost.
	Voyage35PricePerMillionTokens = 0.06

	// Voyage35LitePricePerMillionTokens is the price for voyage-3.5-lite model.
	// Budget option for text embeddings.
	Voyage35LitePricePerMillionTokens = 0.02
)

// VoyageProvider implements EmbeddingProvider using Voyage AI's API.
// It supports code-optimized embeddings with voyage-code-3 model.
// See: https://docs.voyageai.com/docs/embeddings
//
// VoyageProvider also implements UsageTrackingProvider for cost monitoring.
// Pricing: voyage-code-3 = $0.18/1M tokens (200M free), voyage-3.5 = $0.06/1M, voyage-3.5-lite = $0.02/1M.
type VoyageProvider struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client

	// Rate limiting (disabled by default; set via config or AGENTCTL_EMBEDDING_RATE_LIMIT)
	rateLimitMu   sync.Mutex
	requestTimes  []time.Time
	rateLimit     int           // Max requests per window (0 = disabled)
	rateWindow    time.Duration // Window duration (default: 62s)
	rateLimitWait bool          // Whether to wait or error on rate limit

	// Usage tracking
	tracker *usageTracker
}

// VoyageConfig holds configuration for the Voyage AI embedding provider.
type VoyageConfig struct {
	// APIKey is the Voyage AI API key. If empty, reads from VOYAGE_API_KEY env var.
	APIKey string

	// Model is the embedding model to use. Default: "voyage-code-3"
	// Options: voyage-code-3, voyage-3.5, voyage-3.5-lite, voyage-3-large
	Model string

	// BaseURL is the API base URL. Default: "https://api.voyageai.com/v1"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s
	Timeout time.Duration

	// RateLimit is max requests per window.
	// nil = check env var, then default to no limit.
	// 0 = disable rate limiting (for paid accounts).
	// >0 = explicit limit.
	RateLimit *int

	// RateWindow is the rate limit window duration. Default: 62s
	RateWindow time.Duration

	// RateLimitWait controls behavior when rate limited.
	// If true, waits until a slot is available. If false, returns error immediately.
	// Default: true (wait)
	RateLimitWait *bool
}

// Voyage model dimensions
const (
	VoyageDimensionsDefault = 1024 // voyage-code-3, voyage-3.5, voyage-3.5-lite
	VoyageDimensionsLarge   = 1024 // voyage-3-large also 1024
)

// NewVoyageProvider creates a new Voyage AI embedding provider.
func NewVoyageProvider(cfg VoyageConfig) (*VoyageProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("VOYAGE_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("voyage: API key required (set VOYAGE_API_KEY or provide in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "voyage-code-3" // Best for code semantic search
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.voyageai.com/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	// Rate limiting: check env var first, then config, then default to unlimited.
	// AGENTCTL_EMBEDDING_RATE_LIMIT: 0 = disabled, >0 = requests per minute
	var rateLimit int
	if v := os.Getenv("AGENTCTL_EMBEDDING_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rateLimit = n // 0 means disabled, >0 means limit
		}
	} else if cfg.RateLimit != nil {
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

	// All Voyage models use 1024 dimensions
	dimensions := VoyageDimensionsDefault

	return &VoyageProvider{
		apiKey:        apiKey,
		model:         model,
		baseURL:       baseURL,
		dimensions:    dimensions,
		httpClient:    &http.Client{Timeout: timeout},
		requestTimes:  make([]time.Time, 0, rateLimit),
		rateLimit:     rateLimit,
		rateWindow:    rateWindow,
		rateLimitWait: rateLimitWait,
		tracker:       newUsageTracker("voyage", model),
	}, nil
}

// voyageEmbedRequest is the request body for the Voyage embed API.
type voyageEmbedRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type,omitempty"` // "query" or "document"
}

// voyageEmbedResponse is the response from the Voyage embed API.
type voyageEmbedResponse struct {
	Object string `json:"object"` // "list"
	Data   []struct {
		Object    string    `json:"object"` // "embedding"
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
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
// Returns error if context is cancelled or rate limiting is disabled and limit exceeded.
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
			// Record this request
			p.requestTimes = append(p.requestTimes, now)
			p.rateLimitMu.Unlock()
			return nil
		}

		// Calculate wait time until oldest request expires
		oldestTime := p.requestTimes[0]
		waitDuration := oldestTime.Add(p.rateWindow).Sub(now) + 100*time.Millisecond // Small buffer

		p.rateLimitMu.Unlock()

		if !p.rateLimitWait {
			return fmt.Errorf("voyage: rate limit exceeded (%d RPM); retry in %v", p.rateLimit, waitDuration.Round(time.Second))
		}

		// Wait for the duration or context cancellation
		log.Warn().
			Str("provider", "voyage").
			Dur("wait_duration", waitDuration).
			Msg("Rate limited, waiting for slot")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(waitDuration):
			// Continue to retry
		}
	}
}

// Embed generates an embedding vector for the given text.
func (p *VoyageProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("voyage: no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Voyage API supports up to 128 texts per batch.
func (p *VoyageProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Voyage API limit is 128 texts per request
	const maxBatchSize = 128
	if len(texts) > maxBatchSize {
		// Process in chunks
		result := make([][]float32, 0, len(texts))
		for i := 0; i < len(texts); i += maxBatchSize {
			end := i + maxBatchSize
			if end > len(texts) {
				end = len(texts)
			}
			batch, err := p.embedBatchInternal(ctx, texts[i:end])
			if err != nil {
				return nil, fmt.Errorf("voyage: batch %d-%d failed: %w", i, end, err)
			}
			result = append(result, batch...)
		}
		return result, nil
	}

	return p.embedBatchInternal(ctx, texts)
}

func (p *VoyageProvider) embedBatchInternal(ctx context.Context, texts []string) ([][]float32, error) {
	return p.doEmbedRequest(ctx, texts, "document")
}

// doEmbedRequest performs an embedding request with retry logic for 429 errors.
// inputType should be "document" for indexing or "query" for search.
func (p *VoyageProvider) doEmbedRequest(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	const maxRetries = 5

	start := time.Now()
	textsCount := len(texts)
	estimatedTokens := estimateTokens(texts)

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	reqBody := voyageEmbedRequest{
		Input:     texts,
		Model:     p.model,
		InputType: inputType,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("voyage: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Wait for rate limit slot (blocks if at capacity)
		if err := p.waitForRateLimit(ctx); err != nil {
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

		resp, err := p.httpClient.Do(req)
		if err != nil {
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage: request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage: read response: %w", err)
		}

		// Handle 429 rate limit with retry
		if resp.StatusCode == http.StatusTooManyRequests {
			var errResp voyageErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != "" {
				lastErr = fmt.Errorf("voyage: rate limited (429): %s", errResp.Detail)
			} else {
				lastErr = fmt.Errorf("voyage: rate limited (429)")
			}

			if attempt < maxRetries {
				// Wait full window before retry (server-side rate limit is stricter)
				waitTime := p.rateWindow + time.Duration(attempt)*10*time.Second
				select {
				case <-ctx.Done():
					p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusCanceled, ctx.Err())
					return nil, ctx.Err()
				case <-time.After(waitTime):
					continue
				}
			}
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, lastErr)
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			var errResp voyageErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != "" {
				err := fmt.Errorf("voyage: API error %d: %s", resp.StatusCode, errResp.Detail)
				p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}
			err := fmt.Errorf("voyage: API returned status %d: %s", resp.StatusCode, string(respBody))
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		var embedResp voyageEmbedResponse
		if err := json.Unmarshal(respBody, &embedResp); err != nil {
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, fmt.Errorf("voyage: unmarshal response: %w", err)
		}

		if len(embedResp.Data) != len(texts) {
			err := fmt.Errorf("voyage: expected %d embeddings, got %d", len(texts), len(embedResp.Data))
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}

		// Sort by index to maintain order (API returns in order but let's be safe)
		result := make([][]float32, len(texts))
		for _, emb := range embedResp.Data {
			if emb.Index < 0 || emb.Index >= len(texts) {
				err := fmt.Errorf("voyage: invalid embedding index %d", emb.Index)
				p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
				return nil, err
			}
			result[emb.Index] = emb.Embedding
		}

		// Track successful usage with actual token count from API
		actualTokens := int64(embedResp.Usage.TotalTokens)
		costUSD := float64(actualTokens) * p.pricePerToken()
		p.tracker.record(1, textsCount, estimatedTokens, actualTokens, costUSD)
		p.emitEvent(ctx, start, textsCount, estimatedTokens, actualTokens, observability.StatusOK, nil)

		return result, nil
	}

	p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, lastErr)
	return nil, lastErr
}

// EmbedQuery generates an embedding for a search query.
// Uses input_type="query" for better retrieval performance.
func (p *VoyageProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	embeddings, err := p.doEmbedRequest(ctx, []string{query}, "query")
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("voyage: no embedding returned")
	}
	return embeddings[0], nil
}

// Model returns the model identifier.
func (p *VoyageProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding vector dimension.
func (p *VoyageProvider) Dimensions() int {
	return p.dimensions
}

// Usage returns cumulative usage statistics since provider creation.
func (p *VoyageProvider) Usage() EmbeddingUsage {
	return p.tracker.get()
}

// ResetUsage resets the usage counters to zero.
func (p *VoyageProvider) ResetUsage() {
	p.tracker.reset()
}

// pricePerToken returns the price per token based on the model.
func (p *VoyageProvider) pricePerToken() float64 {
	switch {
	case strings.Contains(p.model, "3.5-lite"):
		return Voyage35LitePricePerMillionTokens / 1_000_000
	case strings.Contains(p.model, "3.5"):
		return Voyage35PricePerMillionTokens / 1_000_000
	case strings.Contains(p.model, "3-large"):
		return Voyage3LargePricePerMillionTokens / 1_000_000
	case strings.Contains(p.model, "code-3"):
		return VoyageCode3PricePerMillionTokens / 1_000_000
	default:
		// Default to voyage-3.5 pricing for unknown models
		return Voyage35PricePerMillionTokens / 1_000_000
	}
}

// emitEvent emits a wide event for observability.
func (p *VoyageProvider) emitEvent(ctx context.Context, start time.Time, textsCount int, estimatedTokens, actualTokens int64, status observability.Status, err error) {
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
		Operation:  "embedding.generate",
		Command:    "voyage",
		Subtype:    p.model,
		Status:     status,
		DurationMS: durationMS,
		Data: map[string]any{
			"provider":         "voyage",
			"model":            p.model,
			"texts_count":      textsCount,
			"tokens_estimated": estimatedTokens,
			"tokens_actual":    actualTokens,
			"dimensions":       p.dimensions,
			"cost_usd":         costUSD,
		},
	}

	if err != nil {
		event.ErrorMessage = err.Error()
	}

	observability.Emit(ctx, event)
}
