package sourceimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultOpenAICompatBaseURL = "http://127.0.0.1:1234/v1"
	defaultOpenAICompatTimeout = 20 * time.Second
)

// OpenAICompatEmbedder uses an OpenAI-compatible embeddings endpoint.
// This is suitable for LMStudio local embeddings servers.
type OpenAICompatEmbedder struct {
	baseURL string
	model   string
	apiKey  string
	client  *http.Client
}

// NewOpenAICompatEmbedder creates an OpenAI-compatible embedder.
func NewOpenAICompatEmbedder(baseURL, model, apiKey string, timeout time.Duration) (*OpenAICompatEmbedder, error) {
	baseURL = normalizeOpenAICompatBaseURL(baseURL)

	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible embedder: model is required")
	}
	if timeout <= 0 {
		timeout = defaultOpenAICompatTimeout
	}
	return &OpenAICompatEmbedder{
		baseURL: baseURL,
		model:   model,
		apiKey:  strings.TrimSpace(apiKey),
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Embed generates one embedding vector from text.
func (e *OpenAICompatEmbedder) Embed(ctx context.Context, text string) (EmbeddingResult, error) {
	if e == nil {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: nil receiver")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return EmbeddingResult{}, nil
	}

	reqBody := struct {
		Model string `json:"model"`
		Input string `json:"input"`
	}{
		Model: e.model,
		Input: text,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if e.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.apiKey)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(resp.Status)
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		if readErr == nil {
			var errRes struct {
				Error json.RawMessage `json:"error,omitempty"`
			}
			if err := json.Unmarshal(body, &errRes); err == nil && len(errRes.Error) > 0 {
				if parsed := parseOpenAICompatError(errRes.Error); parsed != "" {
					msg = parsed
				}
			} else if text := strings.TrimSpace(string(body)); text != "" {
				msg = truncate(text, 240)
			}
		}
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: status %d: %s", resp.StatusCode, msg)
	}

	var res struct {
		Model string `json:"model"`
		Data  []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error json.RawMessage `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: decode response: %w", err)
	}
	if msg := parseOpenAICompatError(res.Error); msg != "" {
		return EmbeddingResult{}, fmt.Errorf("openai-compatible embedder: %s", msg)
	}
	if len(res.Data) == 0 || len(res.Data[0].Embedding) == 0 {
		return EmbeddingResult{}, nil
	}

	model := strings.TrimSpace(res.Model)
	if model == "" {
		model = e.model
	}
	return EmbeddingResult{
		Vector: append([]float32(nil), res.Data[0].Embedding...),
		Model:  model,
	}, nil
}

func parseOpenAICompatError(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}

	var object struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &object); err == nil {
		if msg := strings.TrimSpace(object.Message); msg != "" {
			return msg
		}
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return strings.TrimSpace(text)
	}

	if msg := strings.TrimSpace(string(raw)); msg != "" {
		return truncate(msg, 240)
	}
	return ""
}

func normalizeOpenAICompatBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return strings.TrimRight(defaultOpenAICompatBaseURL, "/")
	}
	baseURL = strings.TrimRight(baseURL, "/")

	u, err := url.Parse(baseURL)
	if err != nil || strings.TrimSpace(u.Scheme) == "" || strings.TrimSpace(u.Host) == "" {
		return baseURL
	}

	if strings.TrimSpace(u.Path) == "" || u.Path == "/" {
		u.Path = "/v1"
	} else {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return strings.TrimRight(u.String(), "/")
}
