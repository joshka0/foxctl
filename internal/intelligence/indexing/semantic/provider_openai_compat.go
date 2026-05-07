package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const defaultOpenAICompatBaseURL = "http://127.0.0.1:1234/v1"

// OpenAICompatProvider implements EmbeddingProvider against an OpenAI-compatible embeddings endpoint.
type OpenAICompatProvider struct {
	apiKey        string
	model         string
	baseURL       string
	dimensions    atomic.Int64
	httpClient    *http.Client
	rateLimitWait bool
	tracker       *usageTracker
}

// OpenAICompatConfig holds configuration for the OpenAI-compatible embedding provider.
type OpenAICompatConfig struct {
	APIKey        string
	Model         string
	BaseURL       string
	Timeout       time.Duration
	Dimensions    int
	RateLimitWait *bool
}

// NewOpenAICompatProvider creates a new OpenAI-compatible embedding provider.
func NewOpenAICompatProvider(cfg OpenAICompatConfig) (*OpenAICompatProvider, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible: model required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	wait := true
	if cfg.RateLimitWait != nil {
		wait = *cfg.RateLimitWait
	}
	provider := &OpenAICompatProvider{
		apiKey:        strings.TrimSpace(cfg.APIKey),
		model:         model,
		baseURL:       normalizeOpenAICompatBaseURL(cfg.BaseURL),
		httpClient:    &http.Client{Timeout: timeout},
		rateLimitWait: wait,
		tracker:       newUsageTracker("openai_compat", model),
	}
	provider.dimensions.Store(int64(resolveOpenAICompatDimensions(model, cfg.Dimensions)))
	return provider, nil
}

type openAICompatEmbedRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"`
}

type openAICompatEmbedResponse struct {
	Model string `json:"model"`
	Data  []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error json.RawMessage `json:"error,omitempty"`
}

func (p *OpenAICompatProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("openai-compatible: no embedding returned")
	}
	return results[0], nil
}

func (p *OpenAICompatProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return p.Embed(ctx, query)
}

func (p *OpenAICompatProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload, err := json.Marshal(openAICompatEmbedRequest{
		Model: p.model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("openai-compatible: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("openai-compatible: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	start := time.Now()
	resp, err := p.httpClient.Do(req)
	if err != nil {
		err = newOpenAICompatRequestError(p.model, p.baseURL, err)
		p.emitEvent(ctx, start, len(texts), estimateTokens(texts), 0, false, err)
		return nil, err
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
				msg = truncateOpenAICompat(text, 240)
			}
		}
		err = newOpenAICompatStatusError(p.model, p.baseURL, resp.StatusCode, msg)
		p.emitEvent(ctx, start, len(texts), estimateTokens(texts), resp.StatusCode, false, err)
		return nil, err
	}

	var res openAICompatEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		p.emitEvent(ctx, start, len(texts), estimateTokens(texts), resp.StatusCode, false, err)
		return nil, fmt.Errorf("openai-compatible: decode response: %w", err)
	}
	if msg := parseOpenAICompatError(res.Error); msg != "" {
		err = fmt.Errorf("openai-compatible: %s", msg)
		p.emitEvent(ctx, start, len(texts), estimateTokens(texts), resp.StatusCode, false, err)
		return nil, err
	}

	sort.Slice(res.Data, func(i, j int) bool { return res.Data[i].Index < res.Data[j].Index })
	out := make([][]float32, 0, len(res.Data))
	for _, item := range res.Data {
		out = append(out, append([]float32(nil), item.Embedding...))
		if p.dimensions.Load() == 0 && len(item.Embedding) > 0 {
			p.dimensions.Store(int64(len(item.Embedding)))
		}
	}

	p.tracker.record(1, len(texts), estimateTokens(texts), 0, 0)
	p.emitEvent(ctx, start, len(texts), estimateTokens(texts), resp.StatusCode, true, nil)
	return out, nil
}

func (p *OpenAICompatProvider) Model() string {
	return p.model
}

func (p *OpenAICompatProvider) Dimensions() int {
	return int(p.dimensions.Load())
}

func (p *OpenAICompatProvider) Usage() EmbeddingUsage {
	return p.tracker.get()
}

func (p *OpenAICompatProvider) ResetUsage() {
	p.tracker.reset()
}

func (p *OpenAICompatProvider) emitEvent(_ context.Context, _ time.Time, _ int, _ int64, _ int, _ bool, _ error) {
	// Intentionally lightweight for now; OpenAI-compatible embeddings are mainly used for local evals and semantic search.
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
		return truncateOpenAICompat(msg, 240)
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

func truncateOpenAICompat(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	if max <= 3 {
		return text[:max]
	}
	return text[:max-3] + "..."
}

func resolveOpenAICompatDimensions(model string, configured int) int {
	return ResolveDimensionsForModel(model, configured)
}

var (
	_ EmbeddingProvider      = (*OpenAICompatProvider)(nil)
	_ QueryEmbeddingProvider = (*OpenAICompatProvider)(nil)
	_ UsageTrackingProvider  = (*OpenAICompatProvider)(nil)
)
