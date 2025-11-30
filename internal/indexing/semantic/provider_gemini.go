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
}

// GeminiConfig holds configuration for the Gemini embedding provider.
type GeminiConfig struct {
	// APIKey is the Gemini API key. If empty, reads from GEMINI_API_KEY env var.
	APIKey string

	// Model is the embedding model to use. Default: "text-embedding-004"
	Model string

	// BaseURL is the API base URL. Default: "https://generativelanguage.googleapis.com/v1beta"
	BaseURL string

	// Timeout is the HTTP request timeout. Default: 30s
	Timeout time.Duration
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
		model = "text-embedding-004"
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	// Model dimensions (text-embedding-004 = 768, gemini-embedding-001 = 3072)
	dimensions := 768
	if model == "gemini-embedding-001" {
		dimensions = 3072
	}

	return &GeminiProvider{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		dimensions: dimensions,
		httpClient: &http.Client{Timeout: timeout},
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

// Embed generates an embedding vector for the given text.
func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", p.baseURL, p.model, p.apiKey)

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

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp geminiEmbedResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return nil, fmt.Errorf("gemini: API error %d: %s", errResp.Error.Code, errResp.Error.Message)
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

// EmbedBatch generates embeddings for multiple texts using the batch API.
func (p *GeminiProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Use batch endpoint for efficiency
	url := fmt.Sprintf("%s/models/%s:batchEmbedContents?key=%s", p.baseURL, p.model, p.apiKey)

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

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini: batch request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gemini: read batch response: %w", err)
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
