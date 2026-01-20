package semantic

import (
	"context"
	"os"
	"strings"
	"sync"
)

// EmbeddingScope identifies the type of content being embedded.
// Different scopes may use different embedding models optimized for that content type.
type EmbeddingScope string

const (
	// ScopeSymbols is for code symbols (functions, classes, variables).
	// Best model: voyage-code-3 (optimized for code retrieval).
	ScopeSymbols EmbeddingScope = "symbols"

	// ScopeMemory is for memories, gotchas, and learnings.
	// Best model: voyage-3.5 (good general-purpose, $0.06/1M tokens).
	ScopeMemory EmbeddingScope = "memory"

	// ScopeTasks is for task descriptions and notes.
	// Best model: voyage-3.5 (good general-purpose, $0.06/1M tokens).
	ScopeTasks EmbeddingScope = "tasks"

	// ScopeSessions is for session summaries and context.
	// Best model: voyage-3.5 (good general-purpose, $0.06/1M tokens).
	ScopeSessions EmbeddingScope = "sessions"

	// ScopeCodemaps is for semantic codemaps (code relationship maps).
	// Best model: voyage-3.5 (good general-purpose, $0.06/1M tokens).
	ScopeCodemaps EmbeddingScope = "codemaps"

	// ScopeFileSummaries is for file-level summaries used in tree search.
	// Best model: voyage-code-3 (optimized for code retrieval).
	ScopeFileSummaries EmbeddingScope = "file_summaries"

	// ScopeDefault is the fallback scope for unspecified content.
	ScopeDefault EmbeddingScope = "default"
)

// ScopeModelRecommendation returns the recommended Voyage model for a scope.
// Returns (model, isCodeModel) where isCodeModel indicates if voyage-code-3 should be used.
//
// Environment variable overrides (checked in order):
//  1. Per-scope vars: AGENTCTL_EMBEDDING_MODEL_SYMBOLS, _MEMORY, _TASKS, _SESSIONS, _CODEMAPS
//  2. Category vars: AGENTCTL_EMBEDDING_MODEL_CODE (symbols), AGENTCTL_EMBEDDING_MODEL_TEXT (others)
//  3. Defaults: voyage-code-3 (symbols), voyage-3-large (memory), voyage-3.5 (tasks, sessions, codemaps)
//
// Model strategy (Jan 2025):
// - voyage-code-3: Best for code retrieval (13.80% better than OpenAI), $0.18/1M
// - voyage-3-large: Best quality for text retrieval, $0.06/1M
// - voyage-3.5: Good price/performance for general text, $0.06/1M
//
// All models use 1024 dimensions by default, ensuring storage compatibility.
func ScopeModelRecommendation(scope EmbeddingScope) (model string, isCodeModel bool) {
	// Check per-scope env var first
	scopeEnvVar := "AGENTCTL_EMBEDDING_MODEL_" + strings.ToUpper(string(scope))
	if env := os.Getenv(scopeEnvVar); env != "" {
		return env, scope == ScopeSymbols
	}

	switch scope {
	case ScopeSymbols:
		if env := os.Getenv("AGENTCTL_EMBEDDING_MODEL_CODE"); env != "" {
			return env, true
		}
		return "voyage-code-3", true
	case ScopeFileSummaries:
		if env := os.Getenv("AGENTCTL_EMBEDDING_MODEL_CODE"); env != "" {
			return env, true
		}
		return "voyage-code-3", true
	case ScopeMemory:
		if env := os.Getenv("AGENTCTL_EMBEDDING_MODEL_TEXT"); env != "" {
			return env, false
		}
		return "voyage-3-large", false // Best quality for memories/gotchas
	case ScopeCodemaps, ScopeTasks, ScopeSessions:
		if env := os.Getenv("AGENTCTL_EMBEDDING_MODEL_TEXT"); env != "" {
			return env, false
		}
		return "voyage-3.5", false
	default:
		if env := os.Getenv("AGENTCTL_EMBEDDING_MODEL_TEXT"); env != "" {
			return env, false
		}
		return "voyage-3.5", false
	}
}

// ScopeInputType returns the appropriate Voyage input_type for a scope.
// Using input_type improves retrieval quality by adding scope-specific prompts.
func ScopeInputType(scope EmbeddingScope, isQuery bool) string {
	if isQuery {
		return "query"
	}
	return "document"
}

// ScopePricePerMillionTokens returns the price per million tokens for a scope.
// Useful for cost estimation before making API calls.
func ScopePricePerMillionTokens(scope EmbeddingScope) float64 {
	model, _ := ScopeModelRecommendation(scope)
	switch model {
	case "voyage-code-3":
		return 0.18
	case "voyage-3.5":
		return 0.06
	case "voyage-3.5-lite":
		return 0.02
	default:
		return 0.06 // Default to voyage-3.5 pricing
	}
}

// AllScopes returns all defined embedding scopes.
func AllScopes() []EmbeddingScope {
	return []EmbeddingScope{
		ScopeSymbols,
		ScopeFileSummaries,
		ScopeMemory,
		ScopeTasks,
		ScopeSessions,
		ScopeCodemaps,
	}
}

// EmbeddingProvider generates embeddings for text content.
// Implementations may call external APIs, use WASI skills, or local models.
type EmbeddingProvider interface {
	// Embed generates an embedding vector for the given text.
	// Returns the embedding as a float32 slice.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	// Returns embeddings in the same order as inputs.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Model returns the model identifier used by this provider.
	Model() string

	// Dimensions returns the embedding vector dimension.
	Dimensions() int
}

// QueryEmbeddingProvider extends EmbeddingProvider with query-specific embeddings.
// Providers that support query/document input types should implement this interface.
type QueryEmbeddingProvider interface {
	EmbeddingProvider
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// UsageTrackingProvider extends EmbeddingProvider with usage statistics.
// Providers that track API usage should implement this interface.
type UsageTrackingProvider interface {
	EmbeddingProvider

	// Usage returns cumulative usage statistics since provider creation.
	Usage() EmbeddingUsage

	// ResetUsage resets the usage counters to zero.
	ResetUsage()
}

// EmbeddingUsage tracks API usage for embedding providers.
type EmbeddingUsage struct {
	// Provider identifies the embedding provider (e.g., "gemini", "voyage").
	Provider string `json:"provider"`

	// Model is the specific model used (e.g., "gemini-embedding-001").
	Model string `json:"model"`

	// Requests is the number of API requests made.
	Requests int64 `json:"requests"`

	// TokensEstimated is the estimated input token count.
	// For providers that don't return token counts (like Gemini),
	// this is estimated at ~4 characters per token.
	TokensEstimated int64 `json:"tokens_estimated"`

	// TokensActual is the actual token count returned by the API.
	// Only populated for providers that return this info (like Voyage).
	TokensActual int64 `json:"tokens_actual,omitempty"`

	// TextsProcessed is the number of individual texts embedded.
	TextsProcessed int64 `json:"texts_processed"`

	// CostUSD is the estimated cost in USD.
	// Based on provider pricing (Gemini is currently free).
	CostUSD float64 `json:"cost_usd"`
}

// Add adds another usage record to this one (for aggregation).
func (u *EmbeddingUsage) Add(other EmbeddingUsage) {
	u.Requests += other.Requests
	u.TokensEstimated += other.TokensEstimated
	u.TokensActual += other.TokensActual
	u.TextsProcessed += other.TextsProcessed
	u.CostUSD += other.CostUSD
}

// usageTracker provides thread-safe usage tracking for providers.
type usageTracker struct {
	mu    sync.Mutex
	usage EmbeddingUsage
}

func newUsageTracker(provider, model string) *usageTracker {
	return &usageTracker{
		usage: EmbeddingUsage{
			Provider: provider,
			Model:    model,
		},
	}
}

func (t *usageTracker) record(requests, textsProcessed int, estimatedTokens, actualTokens int64, costUSD float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage.Requests += int64(requests)
	t.usage.TextsProcessed += int64(textsProcessed)
	t.usage.TokensEstimated += estimatedTokens
	t.usage.TokensActual += actualTokens
	t.usage.CostUSD += costUSD
}

func (t *usageTracker) get() EmbeddingUsage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}

func (t *usageTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	provider := t.usage.Provider
	model := t.usage.Model
	t.usage = EmbeddingUsage{
		Provider: provider,
		Model:    model,
	}
}

// estimateTokens estimates token count from text.
// Uses ~4 characters per token as a rough approximation for English text.
func estimateTokens(texts []string) int64 {
	var totalChars int64
	for _, text := range texts {
		totalChars += int64(len(text))
	}
	// ~4 characters per token is a common approximation
	return (totalChars + 3) / 4
}

// NoOpProvider is a stub provider that returns empty embeddings.
// Used when vector support is not enabled or for testing.
type NoOpProvider struct {
	model      string
	dimensions int
}

// NewNoOpProvider creates a no-op embedding provider.
func NewNoOpProvider(model string, dimensions int) *NoOpProvider {
	if model == "" {
		model = "noop"
	}
	if dimensions <= 0 {
		dimensions = 384 // Common default (e.g., sentence-transformers)
	}
	return &NoOpProvider{model: model, dimensions: dimensions}
}

// Embed returns a zero vector of the configured dimension.
func (p *NoOpProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, p.dimensions), nil
}

// EmbedBatch returns zero vectors for all inputs.
func (p *NoOpProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = make([]float32, p.dimensions)
	}
	return result, nil
}

// Model returns the model identifier.
func (p *NoOpProvider) Model() string {
	return p.model
}

// Dimensions returns the embedding dimension.
func (p *NoOpProvider) Dimensions() int {
	return p.dimensions
}
