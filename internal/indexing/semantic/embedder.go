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

	// Provider is the provider name (e.g., "voyage", "gemini").
	Provider string

	// Model is the model identifier used (e.g., "voyage-3.5", "gemini-embedding-001").
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
	voyageKey     string
	geminiKey     string
	modelOverride string
	rateLimitWait bool
	allowFallback bool
	guard         GuardFunc
}

func newEmbedderConfig(opts ...EmbedderOption) *embedderConfig {
	// FC/IS compliant: no os.Getenv in core. Callers must pass keys via
	// WithVoyageKey/WithGeminiKey options (loaded from config at boundary).
	cfg := &embedderConfig{
		rateLimitWait: true,  // Default to waiting for rate limits
		allowFallback: false, // Default to no fallback
	}

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg
}

// WithVoyageKey sets the Voyage API key.
func WithVoyageKey(key string) EmbedderOption {
	return func(c *embedderConfig) {
		c.voyageKey = key
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

// WithAllowFallback enables falling back to Gemini if Voyage fails.
func WithAllowFallback(allow bool) EmbedderOption {
	return func(c *embedderConfig) {
		c.allowFallback = allow
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
//  1. Voyage (preferred for all scopes due to quality)
//  2. Gemini (fallback if Voyage unavailable)
//
// If no API keys are available, returns an error.
func NewEmbedder(scope EmbeddingScope, opts ...EmbedderOption) (*Embedder, error) {
	cfg := newEmbedderConfig(opts...)

	e := &Embedder{
		scope:         scope,
		rateLimitWait: cfg.rateLimitWait,
		allowFallback: cfg.allowFallback,
		guard:         cfg.guard,
	}

	// Get recommended model for scope (can be overridden)
	model, _ := ScopeModelRecommendation(scope)
	if cfg.modelOverride != "" {
		model = cfg.modelOverride
	}

	// Try Voyage first (preferred)
	if cfg.voyageKey != "" {
		vp, err := NewVoyageProvider(VoyageConfig{
			APIKey:        cfg.voyageKey,
			Model:         model,
			RateLimitWait: &cfg.rateLimitWait,
		})
		if err == nil {
			e.provider = vp
			e.providerName = "voyage"
			return e, nil
		}
		// If Voyage creation fails and we can't fallback, return error
		if !cfg.allowFallback || cfg.geminiKey == "" {
			return nil, fmt.Errorf("voyage provider: %w", err)
		}
	}

	// Try Gemini as fallback
	if cfg.geminiKey != "" {
		gp, err := NewGeminiProvider(GeminiConfig{
			APIKey: cfg.geminiKey,
		})
		if err == nil {
			e.provider = gp
			e.providerName = "gemini"
			return e, nil
		}
		return nil, fmt.Errorf("gemini provider: %w", err)
	}

	return nil, fmt.Errorf("no embedding provider available: set VOYAGE_API_KEY or GEMINI_API_KEY")
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
