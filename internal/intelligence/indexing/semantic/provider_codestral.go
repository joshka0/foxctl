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

	"github.com/jkatigb/agentctl/internal/runtime/observability"
	"github.com/oklog/ulid/v2"
)

// Codestral embedding pricing (per 1M tokens, as of Dec 2024)
// See: https://mistral.ai/news/codestral-embed
const (
	// CodestralEmbedPricePerMillionTokens is the standard API price.
	CodestralEmbedPricePerMillionTokens = 0.15

	// CodestralEmbedBatchPricePerMillionTokens is the batch API price (50% discount).
	CodestralEmbedBatchPricePerMillionTokens = 0.075
)

// CodestralProvider implements EmbeddingProvider using Mistral AI's Codestral Embed API.
// It uses the codestral-embed-2505 model (1024 dimensions) specialized for code embeddings.
// See: https://docs.mistral.ai/capabilities/embeddings/
//
// Codestral Embed is optimized for:
// - Code retrieval and semantic search
// - RAG for coding agents
// - Code similarity and clustering
// - Semantic understanding of code across languages
//
// CodestralProvider also implements UsageTrackingProvider for cost monitoring.
type CodestralProvider struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client

	// Usage tracking
	tracker *usageTracker
}

// CodestralConfig holds configuration for the Codestral embedding provider.
type CodestralConfig struct {
	// APIKey is the Mistral AI API key. If empty, reads from MISTRAL_API_KEY env var.
	APIKey string

	// Model is the embedding model to use. Default: "codestral-embed-2505"
	Model string

	// BaseURL is the API base URL. Default: "https://api.mistral.ai/v1"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 60s
	Timeout time.Duration
}

// Codestral model dimensions
const (
	CodestralEmbedDimensions = 1024
)

// NewCodestralProvider creates a new Codestral embedding provider.
func NewCodestralProvider(cfg CodestralConfig) (*CodestralProvider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MISTRAL_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("codestral: API key required (set MISTRAL_API_KEY or provide in config)")
	}

	model := cfg.Model
	if model == "" {
		model = "codestral-embed-2505"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.mistral.ai/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	return &CodestralProvider{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		dimensions: CodestralEmbedDimensions,
		httpClient: &http.Client{Timeout: timeout},
		tracker:    newUsageTracker("codestral", model),
	}, nil
}

// codestralEmbedRequest is the request body for the Codestral embed API.
type codestralEmbedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// codestralEmbedResponse is the response from the Codestral embed API.
type codestralEmbedResponse struct {
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

// codestralErrorResponse is the error response from the Mistral API.
type codestralErrorResponse struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// Embed generates an embedding vector for the given text.
func (p *CodestralProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("codestral: no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
// Codestral API supports batching natively via Mistral's API.
func (p *CodestralProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	start := time.Now()
	textsCount := len(texts)
	estimatedTokens := estimateTokens(texts)

	url := fmt.Sprintf("%s/embeddings", p.baseURL)
	reqBody := codestralEmbedRequest{
		Input: texts,
		Model: p.model,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("codestral: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("codestral: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("codestral: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("codestral: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp codestralErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			err := fmt.Errorf("codestral: API error %d: %s", resp.StatusCode, errResp.Message)
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}
		err := fmt.Errorf("codestral: API returned status %d: %s", resp.StatusCode, string(respBody))
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, err
	}

	var embedResp codestralEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, fmt.Errorf("codestral: unmarshal response: %w", err)
	}

	if len(embedResp.Data) != len(texts) {
		err := fmt.Errorf("codestral: expected %d embeddings, got %d", len(texts), len(embedResp.Data))
		p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
		return nil, err
	}

	// Sort by index to maintain order
	result := make([][]float32, len(texts))
	for _, emb := range embedResp.Data {
		if emb.Index < 0 || emb.Index >= len(texts) {
			err := fmt.Errorf("codestral: invalid embedding index %d", emb.Index)
			p.emitEvent(ctx, start, textsCount, estimatedTokens, 0, observability.StatusError, err)
			return nil, err
		}
		result[emb.Index] = emb.Embedding
	}

	// Track successful usage with actual token count from API
	actualTokens := int64(embedResp.Usage.TotalTokens)
	costUSD := float64(actualTokens) * CodestralEmbedPricePerMillionTokens / 1_000_000
	p.tracker.record(1, textsCount, estimatedTokens, actualTokens, costUSD)
	p.emitEvent(ctx, start, textsCount, estimatedTokens, actualTokens, observability.StatusOK, nil)

	return result, nil
}

// Model returns the model identifier.
func (p *CodestralProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding vector dimension.
func (p *CodestralProvider) Dimensions() int {
	return p.dimensions
}

// Usage returns cumulative usage statistics since provider creation.
func (p *CodestralProvider) Usage() EmbeddingUsage {
	return p.tracker.get()
}

// ResetUsage resets the usage counters to zero.
func (p *CodestralProvider) ResetUsage() {
	p.tracker.reset()
}

// emitEvent emits a wide event for observability.
func (p *CodestralProvider) emitEvent(ctx context.Context, start time.Time, textsCount int, estimatedTokens, actualTokens int64, status observability.Status, err error) {
	durationMS := time.Since(start).Milliseconds()

	costUSD := 0.0
	if actualTokens > 0 {
		costUSD = float64(actualTokens) * CodestralEmbedPricePerMillionTokens / 1_000_000
	}

	event := &observability.WideEvent{
		Ts:         time.Now().UTC(),
		TraceID:    observability.TraceIDFromContext(ctx),
		SpanID:     ulid.Make().String(),
		Service:    "agentctl",
		Component:  observability.ComponentSkill,
		Operation:  "embedding.generate",
		Command:    "codestral",
		Subtype:    p.model,
		Status:     status,
		DurationMS: durationMS,
		Data: map[string]any{
			"provider":         "codestral",
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
