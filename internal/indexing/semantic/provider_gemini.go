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

// GeminiProvider implements EmbeddingProvider using Google's Gemini API.
// It uses the text-embedding-004 model by default (768 dimensions).
// See: https://ai.google.dev/gemini-api/docs/embeddings
type GeminiProvider struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client

	// Rate limiting (free tier: 15 RPM for text-embedding-004, 5 RPM for gemini-embedding-001)
	rateLimitMu   sync.Mutex
	requestTimes  []time.Time
	rateLimit     int           // Max requests per window
	rateWindow    time.Duration // Window duration
	rateLimitWait bool          // Whether to wait or error on rate limit
}

// GeminiConfig holds configuration for the Gemini embedding provider.
type GeminiConfig struct {
	// APIKey is the Gemini API key. If empty, reads from GEMINI_API_KEY env var.
	APIKey string

	// Model is the embedding model to use. Default: "text-embedding-004"
	// Options: text-embedding-004 (768 dims), gemini-embedding-001 (3072 dims)
	Model string

	// BaseURL is the API base URL. Default: "https://generativelanguage.googleapis.com/v1beta"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s
	Timeout time.Duration

	// RateLimit is max requests per window. Default: 15 (free tier for text-embedding-004)
	// Set to 0 to disable rate limiting (for higher tier accounts)
	RateLimit int

	// RateWindow is the rate limit window duration. Default: 62s
	RateWindow time.Duration

	// RateLimitWait controls behavior when rate limited.
	// If true, waits until a slot is available. If false, returns error immediately.
	// Default: true (wait)
	RateLimitWait *bool
}

// NewGeminiProvider creates a new Gemini embedding provider.
func NewGeminiProvider(cfg GeminiConfig) (*GeminiProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("GEMINI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("gemini: API key required (set GEMINI_API_KEY or provide in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "gemini-embedding-001" // 3072 dims, consistent with existing embeddings
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	// Model dimensions (gemini-embedding-001 = 3072, text-embedding-004 = 768)
	dimensions := 3072
	if model == "text-embedding-004" {
		dimensions = 768
	}

	// Rate limiting defaults based on model
	rateLimit := cfg.RateLimit
	if rateLimit == 0 {
		// Default to free tier limits
		if model == "text-embedding-004" {
			rateLimit = 15 // 15 RPM for text-embedding-004
		} else {
			rateLimit = 5 // 5 RPM for gemini-embedding-001 (default)
		}
	}
	rateWindow := cfg.RateWindow
	if rateWindow == 0 {
		rateWindow = 62 * time.Second // Slightly over 1 minute to be safe
	}
	rateLimitWait := true
	if cfg.RateLimitWait != nil {
		rateLimitWait = *cfg.RateLimitWait
	}

	return &GeminiProvider{
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

// geminiEmbedRequest is the request body for the Gemini embed API.
type geminiEmbedRequest struct {
	Model   string            `json:"model"`
	Content geminiContentPart `json:"content"`
}

type geminiContentPart struct {
	Parts []geminiTextPart `json:"parts"`
}

type geminiTextPart struct {
	Text string `json:"text"`
}

// geminiEmbedResponse is the response from the Gemini embed API.
type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
	Error *geminiError `json:"error,omitempty"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// geminiBatchEmbedRequest is the request body for batch embedding.
type geminiBatchEmbedRequest struct {
	Requests []geminiEmbedContentRequest `json:"requests"`
}

type geminiEmbedContentRequest struct {
	Model   string            `json:"model"`
	Content geminiContentPart `json:"content"`
}

type geminiBatchEmbedResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
	Error *geminiError `json:"error,omitempty"`
}

// waitForRateLimit blocks until a request slot is available.
func (p *GeminiProvider) waitForRateLimit(ctx context.Context) error {
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
		waitDuration := oldestTime.Add(p.rateWindow).Sub(now) + 100*time.Millisecond

		p.rateLimitMu.Unlock()

		if !p.rateLimitWait {
			return fmt.Errorf("gemini: rate limit exceeded (%d RPM free tier); retry in %v", p.rateLimit, waitDuration.Round(time.Second))
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
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	// Wait for rate limit slot
	if err := p.waitForRateLimit(ctx); err != nil {
		return nil, err
	}

	// Use header-based auth to prevent key leakage in logs
	url := fmt.Sprintf("%s/models/%s:embedContent", p.baseURL, p.model)

	reqBody := geminiEmbedRequest{
		Model: fmt.Sprintf("models/%s", p.model),
		Content: geminiContentPart{
			Parts: []geminiTextPart{{Text: text}},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("gemini: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	// Handle 429 rate limit
	if resp.StatusCode == http.StatusTooManyRequests {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini: rate limited (429): %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini: rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini: API error %d (%s): %s", errResp.Error.Code, errResp.Error.Status, errResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini: API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var embedResp geminiEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("gemini: unmarshal response: %w", err)
	}

	if len(embedResp.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini: empty embedding returned")
	}

	return embedResp.Embedding.Values, nil
}

// EmbedQuery generates an embedding for a search query.
// Gemini doesn't distinguish between document and query embeddings.
func (p *GeminiProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return p.Embed(ctx, query)
}

// EmbedBatch generates embeddings for multiple texts using the batch API.
func (p *GeminiProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Wait for rate limit slot (batch counts as one request)
	if err := p.waitForRateLimit(ctx); err != nil {
		return nil, err
	}

	// Use batch endpoint with header-based auth
	url := fmt.Sprintf("%s/models/%s:batchEmbedContents", p.baseURL, p.model)

	requests := make([]geminiEmbedContentRequest, len(texts))
	for i, text := range texts {
		requests[i] = geminiEmbedContentRequest{
			Model: fmt.Sprintf("models/%s", p.model),
			Content: geminiContentPart{
				Parts: []geminiTextPart{{Text: text}},
			},
		}
	}

	reqBody := geminiBatchEmbedRequest{Requests: requests}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("gemini: marshal batch request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("gemini: create batch request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: batch request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read batch response: %w", err)
	}

	// Handle 429 rate limit
	if resp.StatusCode == http.StatusTooManyRequests {
		var errResp geminiBatchEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini: batch rate limited (429): %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini: batch rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiBatchEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini: batch API error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini: batch API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var batchResp geminiBatchEmbedResponse
	if err := json.Unmarshal(respBody, &batchResp); err != nil {
		return nil, fmt.Errorf("gemini: unmarshal batch response: %w", err)
	}

	if len(batchResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini: expected %d embeddings, got %d", len(texts), len(batchResp.Embeddings))
	}

	result := make([][]float32, len(texts))
	for i, emb := range batchResp.Embeddings {
		result[i] = emb.Values
	}

	return result, nil
}

// Model returns the model identifier.
func (p *GeminiProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding vector dimension.
func (p *GeminiProvider) Dimensions() int {
	return p.dimensions
}
