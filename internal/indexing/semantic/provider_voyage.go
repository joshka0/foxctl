package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// VoyageProvider implements EmbeddingProvider using Voyage AI's API.
// It supports code-optimized embeddings with voyage-code-3 model.
// See: https://docs.voyageai.com/docs/embeddings
type VoyageProvider struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client

	// Rate limiting for free tier (3 RPM without payment method)
	rateLimitMu   sync.Mutex
	requestTimes  []time.Time
	rateLimit     int           // Max requests per window (default: 3)
	rateWindow    time.Duration // Window duration (default: 62s)
	rateLimitWait bool          // Whether to wait or error on rate limit
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

	// RateLimit is max requests per window. Default: 3 (free tier without payment)
	// Set to 0 to disable rate limiting (for paid accounts)
	RateLimit int

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

	// Rate limiting defaults (free tier: 3 RPM)
	rateLimit := cfg.RateLimit
	if rateLimit == 0 {
		rateLimit = 3 // Default to free tier limit
	}
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
			return fmt.Errorf("voyage: rate limit exceeded (3 RPM free tier); retry in %v or add payment method at dashboard.voyageai.com", waitDuration.Round(time.Second))
		}

		// Wait for the duration or context cancellation
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

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	reqBody := voyageEmbedRequest{
		Input:     texts,
		Model:     p.model,
		InputType: inputType,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("voyage: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Wait for rate limit slot (blocks if at capacity)
		if err := p.waitForRateLimit(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("voyage: create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

		resp, err := p.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("voyage: request failed: %w", err)
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
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
					return nil, ctx.Err()
				case <-time.After(waitTime):
					continue
				}
			}
			return nil, lastErr
		}

		if resp.StatusCode != http.StatusOK {
			var errResp voyageErrorResponse
			if json.Unmarshal(respBody, &errResp) == nil && errResp.Detail != "" {
				return nil, fmt.Errorf("voyage: API error %d: %s", resp.StatusCode, errResp.Detail)
			}
			return nil, fmt.Errorf("voyage: API returned status %d: %s", resp.StatusCode, string(respBody))
		}

		var embedResp voyageEmbedResponse
		if err := json.Unmarshal(respBody, &embedResp); err != nil {
			return nil, fmt.Errorf("voyage: unmarshal response: %w", err)
		}

		if len(embedResp.Data) != len(texts) {
			return nil, fmt.Errorf("voyage: expected %d embeddings, got %d", len(texts), len(embedResp.Data))
		}

		// Sort by index to maintain order (API returns in order but let's be safe)
		result := make([][]float32, len(texts))
		for _, emb := range embedResp.Data {
			if emb.Index < 0 || emb.Index >= len(texts) {
				return nil, fmt.Errorf("voyage: invalid embedding index %d", emb.Index)
			}
			result[emb.Index] = emb.Embedding
		}

		return result, nil
	}

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
