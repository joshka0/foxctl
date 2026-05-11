package semantic

import (
	"context"
	"fmt"
	"strings"
)

// EmbedResult contains the embedding vector and metadata about how it was generated.
type EmbedResult struct {
	// Vec is the embedding vector.
	Vec []float32

	// Provider is the provider name.
	Provider string

	// Model is the model identifier used.
	Model string

	// Dims is the vector dimensions.
	Dims int
}

// GuardFunc wraps an operation with external protection (e.g. circuit breaker).
// The guard calls fn and may short-circuit based on prior failures.
type GuardFunc func(ctx context.Context, fn func(context.Context) error) error

// Embedder provides a unified interface for generating embeddings with automatic
// provider selection, rate limiting, and fallback behavior.
type Embedder struct {
	scope         EmbeddingScope
	provider      EmbeddingProvider
	providerName  string
	rateLimitWait bool
	allowFallback bool
	guard         GuardFunc
}

// EmbedderOption configures an Embedder.
type EmbedderOption func(*embedderConfig)

type embedderConfig struct {
	provider      string
	apiKey        string
	baseURL       string
	geminiKey     string
	modelOverride string
	rateLimitWait bool
	guard         GuardFunc
}

func newEmbedderConfig(opts ...EmbedderOption) *embedderConfig {
	// FC/IS compliant: no os.Getenv in core. Callers pass keys loaded at the boundary.
	cfg := &embedderConfig{
		rateLimitWait: true, // Default to waiting for rate limits
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

// WithProvider sets the preferred provider name.
func WithProvider(provider string) EmbedderOption {
	return func(c *embedderConfig) {
		c.provider = strings.TrimSpace(provider)
	}
}

// WithAPIKey sets a generic embedding provider API key.
func WithAPIKey(key string) EmbedderOption {
	return func(c *embedderConfig) {
		c.apiKey = key
	}
}

// WithBaseURL sets a provider base URL.
func WithBaseURL(baseURL string) EmbedderOption {
	return func(c *embedderConfig) {
		c.baseURL = strings.TrimSpace(baseURL)
	}
}

// WithGeminiKey sets the Gemini API key.
func WithGeminiKey(key string) EmbedderOption {
	return func(c *embedderConfig) {
		c.geminiKey = key
	}
}

// WithModelOverride forces a specific model instead of using scope-based recommendation.
func WithModelOverride(model string) EmbedderOption {
	return func(c *embedderConfig) {
		c.modelOverride = model
	}
}

// WithRateLimitWait enables waiting for rate limits instead of failing.
func WithRateLimitWait(wait bool) EmbedderOption {
	return func(c *embedderConfig) {
		c.rateLimitWait = wait
	}
}

// WithGuardFunc sets a guard function that wraps each API call (e.g. circuit breaker).
func WithGuardFunc(fn GuardFunc) EmbedderOption {
	return func(c *embedderConfig) {
		c.guard = fn
	}
}

// NewEmbedder creates an Embedder for the given scope.
// It automatically selects the appropriate provider based on available API keys
// and scope-based model recommendations.
//
// Provider selection priority:
//  1. Explicit provider/model config
//  2. OpenAI-compatible local endpoint (LM Studio by default)
//  3. Gemini when explicitly selected
func NewEmbedder(scope EmbeddingScope, opts ...EmbedderOption) (*Embedder, error) {
	cfg := newEmbedderConfig(opts...)

	e := &Embedder{
		scope:         scope,
		rateLimitWait: cfg.rateLimitWait,
		guard:         cfg.guard,
	}

	// Get recommended model for scope (can be overridden)
	model, _ := ScopeModelRecommendation(scope)
	if cfg.modelOverride != "" {
		model = cfg.modelOverride
	}

	preferredProvider := normalizeEmbeddingProviderName(cfg.provider)
	if preferredProvider == "" {
		if inferred := providerFromModel(model); inferred != "" {
			preferredProvider = inferred
		} else if strings.TrimSpace(cfg.baseURL) != "" {
			preferredProvider = "openai_compat"
		}
	}

	tryOpenAICompat := func() error {
		op, err := NewOpenAICompatProvider(OpenAICompatConfig{
			APIKey:        cfg.apiKey,
			Model:         model,
			BaseURL:       cfg.baseURL,
			RateLimitWait: &cfg.rateLimitWait,
		})
		if err != nil {
			return err
		}
		e.provider = op
		e.providerName = "openai_compat"
		return nil
	}

	if preferredProvider == "" {
		preferredProvider = DefaultEmbeddingProvider
	}

	if preferredProvider == "openai_compat" {
		if err := tryOpenAICompat(); err != nil {
			return nil, fmt.Errorf("openai-compatible provider: %w", err)
		}
		return e, nil
	}

	if preferredProvider == "gemini" {
		if cfg.geminiKey == "" {
			return nil, fmt.Errorf("gemini provider requires GEMINI_API_KEY")
		}
		gp, err := NewGeminiProvider(GeminiConfig{
			APIKey: cfg.geminiKey,
			Model:  model,
		})
		if err == nil {
			e.provider = gp
			e.providerName = "gemini"
			return e, nil
		}
		return nil, fmt.Errorf("gemini provider: %w", err)
	}

	if preferredProvider == "noop" {
		e.provider = NewNoOpProvider(model, ResolveDimensionsForModel(model, 0))
		e.providerName = "noop"
		return e, nil
	}

	return nil, fmt.Errorf("unsupported embedding provider %q: use lmstudio/openai_compat, gemini, or noop", preferredProvider)
}

// NewEmbedderWithModel creates an Embedder while honoring a model override.
// If modelOverride is empty, this behaves the same as NewEmbedder.
func NewEmbedderWithModel(scope EmbeddingScope, modelOverride string, opts ...EmbedderOption) (*Embedder, error) {
	if strings.TrimSpace(modelOverride) != "" {
		opts = append(opts, WithModelOverride(modelOverride))
	}
	return NewEmbedder(scope, opts...)
}

// ResolveModelOverride returns the model override for a scope, if configured.
// Per-scope overrides take precedence over the default model.
func ResolveModelOverride(scope EmbeddingScope, defaultModel string, overrides map[string]string) string {
	if overrides != nil {
		if model, ok := overrides[string(scope)]; ok && strings.TrimSpace(model) != "" {
			return strings.TrimSpace(model)
		}
	}
	return strings.TrimSpace(defaultModel)
}

// Embed generates an embedding for the given text.
func (e *Embedder) Embed(ctx context.Context, text string) (EmbedResult, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return EmbedResult{}, nil
	}

	var vec []float32
	embed := func(ctx context.Context) error {
		var err error
		vec, err = e.provider.Embed(ctx, text)
		return err
	}

	var err error
	if e.guard != nil {
		err = e.guard(ctx, embed)
	} else {
		err = embed(ctx)
	}
	if err != nil {
		return EmbedResult{}, err
	}

	return EmbedResult{
		Vec:      vec,
		Provider: e.providerName,
		Model:    e.provider.Model(),
		Dims:     e.provider.Dimensions(),
	}, nil
}

// EmbedQuery generates a query-optimized embedding when the provider supports it.
// It falls back to regular Embed behavior for providers without query/document modes.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) (EmbedResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return EmbedResult{}, nil
	}

	var vec []float32
	embed := func(ctx context.Context) error {
		var err error
		if qp, ok := e.provider.(QueryEmbeddingProvider); ok {
			vec, err = qp.EmbedQuery(ctx, query)
		} else {
			vec, err = e.provider.Embed(ctx, query)
		}
		return err
	}

	var err error
	if e.guard != nil {
		err = e.guard(ctx, embed)
	} else {
		err = embed(ctx)
	}
	if err != nil {
		return EmbedResult{}, err
	}

	return EmbedResult{
		Vec:      vec,
		Provider: e.providerName,
		Model:    e.provider.Model(),
		Dims:     e.provider.Dimensions(),
	}, nil
}

// EmbedBatch generates embeddings for multiple texts.
func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) ([]EmbedResult, error) {
	// Trim all texts
	trimmed := make([]string, len(texts))
	for i, t := range texts {
		trimmed[i] = strings.TrimSpace(t)
	}

	var vecs [][]float32
	batch := func(ctx context.Context) error {
		var err error
		vecs, err = e.provider.EmbedBatch(ctx, trimmed)
		return err
	}

	var err error
	if e.guard != nil {
		err = e.guard(ctx, batch)
	} else {
		err = batch(ctx)
	}
	if err != nil {
		return nil, err
	}

	results := make([]EmbedResult, len(vecs))
	for i, vec := range vecs {
		results[i] = EmbedResult{
			Vec:      vec,
			Provider: e.providerName,
			Model:    e.provider.Model(),
			Dims:     e.provider.Dimensions(),
		}
	}
	return results, nil
}

// Provider returns the name of the underlying provider.
func (e *Embedder) Provider() string {
	return e.providerName
}

// Model returns the model identifier.
func (e *Embedder) Model() string {
	return e.provider.Model()
}

// Dimensions returns the embedding vector dimension.
func (e *Embedder) Dimensions() int {
	return e.provider.Dimensions()
}

// Scope returns the embedding scope this embedder is configured for.
func (e *Embedder) Scope() EmbeddingScope {
	return e.scope
}

// MustNewEmbedder creates an Embedder or panics on error.
// Useful for initialization in main() where failure is fatal.
func MustNewEmbedder(scope EmbeddingScope, opts ...EmbedderOption) *Embedder {
	e, err := NewEmbedder(scope, opts...)
	if err != nil {
		panic(err)
	}
	return e
}
