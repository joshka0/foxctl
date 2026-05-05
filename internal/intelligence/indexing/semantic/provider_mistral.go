package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/oklog/ulid/v2"
)

// Mistral embedding pricing (per 1M tokens, as of Dec 2024)
// See: https://mistral.ai/pricing
const (
	// MistralEmbedPricePerMillionTokens is the price for mistral-embed model.
	MistralEmbedPricePerMillionTokens = 0.10
)

// MistralProvider implements EmbeddingProvider using Mistral AI's API.
// It uses the mistral-embed model (1024 dimensions) for general-purpose text embeddings.
// See: https://docs.mistral.ai/capabilities/embeddings/
//
// MistralProvider also implements UsageTrackingProvider for cost monitoring.
type MistralProvider struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client

	// Usage tracking
	tracker *usageTracker
}

// MistralConfig holds configuration for the Mistral AI embedding provider.
type MistralConfig struct {
	// APIKey is the Mistral AI API key. If empty, reads from MISTRAL_API_KEY env var.
	APIKey string

	// Model is the embedding model to use. Default: "mistral-embed"
	Model string

	// BaseURL is the API base URL. Default: "https://api.mistral.ai/v1"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s
	Timeout time.Duration
}

// Mistral model dimensions
const (
	MistralEmbedDimensions = 1024
)

// NewMistralProvider creates a new Mistral AI embedding provider.
func NewMistralProvider(cfg MistralConfig) (*MistralProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MISTRAL_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("mistral: API key required (set MISTRAL_API_KEY or provide in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "mistral-embed"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.mistral.ai/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &MistralProvider{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		dimensions: MistralEmbedDimensions,
		httpClient: &http.Client{Timeout: timeout},
		tracker:    newUsageTracker("mistral", model),
	}, nil
}

// mistralEmbedRequest is the request body for the Mistral embed API.
type mistralEmbedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// mistralEmbedResponse is the response from the Mistral embed API.
type mistralEmbedResponse struct {
	Object string `json:"object"` // "list"
	Data   []struct {
		Object    string    `json:"object"` // "embedding"
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// mistralErrorResponse is the error response from the Mistral API.
type mistralErrorResponse struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Embed generates an embedding vector for the given text.
func (p *MistralProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("mistral: no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Mistral API supports batching natively.
func (p *MistralProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	start := time.Now()
	textsCount := len(texts)
	estimatedTokens := estimateTokens(texts)

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	reqBody := mistralEmbedRequest{
		Input: texts,
		Model: p.model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("mistral: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("mistral: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("mistral: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("mistral: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp mistralErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			err := fmt.Errorf("mistral: API error %d: %s", resp.StatusCode, errResp.Message)
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}
		err := fmt.Errorf("mistral: API returned status %d: %s", resp.StatusCode, string(respBody))
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, err
	}

	var embedResp mistralEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("mistral: unmarshal response: %w", err)
	}

	if len(embedResp.Data) != len(texts) {
		err := fmt.Errorf("mistral: expected %d embeddings, got %d", len(texts), len(embedResp.Data))
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, err
	}

	// Sort by index to maintain order
	result := make([][]float32, len(texts))
	for _, emb := range embedResp.Data {
		if emb.Index < 0 || emb.Index >= len(texts) {
			err := fmt.Errorf("mistral: invalid embedding index %d", emb.Index)
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}
		result[emb.Index] = emb.Embedding
	}

	// Track successful usage with actual token count from API
	actualTokens := int64(embedResp.Usage.TotalTokens)
	costUSD := float64(actualTokens) * MistralEmbedPricePerMillionTokens / 1_000_000
	p.tracker.record(1, textsCount, estimatedTokens, actualTokens, costUSD)
	p.emitEvent(ctx, start, textsCount, estimatedTokens, actualTokens, observability.StatusOK, nil)

	return result, nil
}

// Model returns the model identifier.
func (p *MistralProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding vector dimension.
func (p *MistralProvider) Dimensions() int {
	return p.dimensions
}

// Usage returns cumulative usage statistics since provider creation.
func (p *MistralProvider) Usage() EmbeddingUsage {
	return p.tracker.get()
}

// ResetUsage resets the usage counters to zero.
func (p *MistralProvider) ResetUsage() {
	p.tracker.reset()
}

// emitEvent emits a foxcular event for observability.
func (p *MistralProvider) emitEvent(ctx context.Context, start time.Time, textsCount int, estimatedTokens, actualTokens int64, status observability.Status, err error) {
	duration := time.Since(start)

	costUSD := 0.0
	if actualTokens > 0 {
		costUSD = float64(actualTokens) * MistralEmbedPricePerMillionTokens / 1_000_000
	}

	event := &observability.Event{
		Timestamp: time.Now().UTC(),
		TraceID:   observability.TraceIDFromContext(ctx),
		SpanID:    ulid.Make().String(),
		Operation: "embedding.generate",
		Name:      "mistral",
		Status:    status,
		Duration:  duration,
		Data: map[string]any{
			observability.DataKeyService:   "foxctl",
			observability.DataKeyComponent: observability.ComponentSkill,
			observability.DataKeySubtype:   p.model,
			"provider":                     "mistral",
			"model":                        p.model,
			"texts_count":                  textsCount,
			"tokens_estimated":             estimatedTokens,
			"tokens_actual":                actualTokens,
			"dimensions":                   p.dimensions,
			"cost_usd":                     costUSD,
		},
	}

	if err != nil {
		event.ErrorMessage = err.Error()
	}

	observability.Emit(ctx, event)
}
