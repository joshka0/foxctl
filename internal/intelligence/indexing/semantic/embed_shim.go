package semantic

import (
	"github.com/joshka0/foxsearch/embed"
)

// This file re-exports the embedding provider layer from
// github.com/joshka0/foxsearch/embed. The implementation moved there as part
// of the foxsearch extraction; these aliases preserve the foxctl import path
// for existing consumers. Foxctl-specific wiring (observability events,
// dbdriver dimension defaults, config-based provider selection) stays in this
// package — see embed_events.go and embedder_config.go.

type (
	// EmbeddingProvider generates embeddings for text content.
	EmbeddingProvider = embed.EmbeddingProvider
	// QueryEmbeddingProvider extends EmbeddingProvider with query-specific embeddings.
	QueryEmbeddingProvider = embed.QueryEmbeddingProvider
	// UsageTrackingProvider extends EmbeddingProvider with usage statistics.
	UsageTrackingProvider = embed.UsageTrackingProvider
	// EmbeddingUsage tracks API usage for embedding providers.
	EmbeddingUsage = embed.EmbeddingUsage
	// EmbeddingScope identifies the type of content being embedded.
	EmbeddingScope = embed.EmbeddingScope
	// EmbeddingServiceError classifies failures from external embedding services.
	EmbeddingServiceError = embed.EmbeddingServiceError
	// EmbedResult contains the embedding vector and generation metadata.
	EmbedResult = embed.EmbedResult
	// Embedder provides a unified interface for generating embeddings.
	Embedder = embed.Embedder
	// EmbedderOption configures an Embedder.
	EmbedderOption = embed.EmbedderOption
	// GuardFunc wraps an operation with external protection (e.g. circuit breaker).
	GuardFunc = embed.GuardFunc
	// OpenAICompatProvider implements EmbeddingProvider against an OpenAI-compatible endpoint.
	OpenAICompatProvider = embed.OpenAICompatProvider
	// OpenAICompatConfig holds configuration for the OpenAI-compatible provider.
	OpenAICompatConfig = embed.OpenAICompatConfig
	// GeminiProvider implements EmbeddingProvider against the Gemini API.
	GeminiProvider = embed.GeminiProvider
	// GeminiConfig holds configuration for the Gemini provider.
	GeminiConfig = embed.GeminiConfig
	// MistralProvider implements EmbeddingProvider against the Mistral API.
	MistralProvider = embed.MistralProvider
	// MistralConfig holds configuration for the Mistral provider.
	MistralConfig = embed.MistralConfig
	// CodestralProvider implements EmbeddingProvider against the Codestral API.
	CodestralProvider = embed.CodestralProvider
	// CodestralConfig holds configuration for the Codestral provider.
	CodestralConfig = embed.CodestralConfig
	// NoOpProvider is a stub provider that returns empty embeddings.
	NoOpProvider = embed.NoOpProvider
)

const (
	// DefaultEmbeddingProvider is the canonical local embedding provider.
	DefaultEmbeddingProvider = embed.DefaultEmbeddingProvider
	// DefaultEmbeddingModel is the canonical local embedding model.
	DefaultEmbeddingModel = embed.DefaultEmbeddingModel

	// ScopeSymbols is for code symbols (functions, classes, variables).
	ScopeSymbols = embed.ScopeSymbols
	// ScopeMemory is for memories, gotchas, and learnings.
	ScopeMemory = embed.ScopeMemory
	// ScopeTasks is for task descriptions and notes.
	ScopeTasks = embed.ScopeTasks
	// ScopeSessions is for session summaries and context.
	ScopeSessions = embed.ScopeSessions
	// ScopeCodemaps is for semantic codemaps (code relationship maps).
	ScopeCodemaps = embed.ScopeCodemaps
	// ScopeFileSummaries is for file-level summaries used in tree search.
	ScopeFileSummaries = embed.ScopeFileSummaries
	// ScopeDefault is the fallback scope for unspecified content.
	ScopeDefault = embed.ScopeDefault
)

var (
	// NewEmbedder creates an Embedder for the given scope.
	NewEmbedder = embed.NewEmbedder
	// NewEmbedderWithModel creates an Embedder while honoring a model override.
	NewEmbedderWithModel = embed.NewEmbedderWithModel
	// MustNewEmbedder creates an Embedder or panics on error.
	MustNewEmbedder = embed.MustNewEmbedder
	// ResolveModelOverride returns the model override for a scope, if configured.
	ResolveModelOverride = embed.ResolveModelOverride
	// ScopeModelRecommendation returns the recommended embedding model for a scope.
	ScopeModelRecommendation = embed.ScopeModelRecommendation
	// ScopeInputType returns the query/document role for asymmetric providers.
	ScopeInputType = embed.ScopeInputType
	// ScopePricePerMillionTokens returns the price per million tokens for a scope.
	ScopePricePerMillionTokens = embed.ScopePricePerMillionTokens
	// AllScopes returns all defined embedding scopes.
	AllScopes = embed.AllScopes
	// DimensionsForModel returns the expected embedding dimensions for a model.
	DimensionsForModel = embed.DimensionsForModel
	// ResolveDimensionsForModel chooses expected dimensions for a model and optional config.
	ResolveDimensionsForModel = embed.ResolveDimensionsForModel

	// WithProvider sets the preferred provider name.
	WithProvider = embed.WithProvider
	// WithAPIKey sets a generic embedding provider API key.
	WithAPIKey = embed.WithAPIKey
	// WithBaseURL sets a provider base URL.
	WithBaseURL = embed.WithBaseURL
	// WithGeminiKey sets the Gemini API key.
	WithGeminiKey = embed.WithGeminiKey
	// WithModelOverride forces a specific model.
	WithModelOverride = embed.WithModelOverride
	// WithRateLimitWait enables waiting for rate limits instead of failing.
	WithRateLimitWait = embed.WithRateLimitWait
	// WithGuardFunc sets a guard function that wraps each API call.
	WithGuardFunc = embed.WithGuardFunc

	// NewOpenAICompatProvider creates an OpenAI-compatible provider.
	NewOpenAICompatProvider = embed.NewOpenAICompatProvider
	// NewGeminiProvider creates a Gemini provider.
	NewGeminiProvider = embed.NewGeminiProvider
	// NewMistralProvider creates a Mistral provider.
	NewMistralProvider = embed.NewMistralProvider
	// NewCodestralProvider creates a Codestral provider.
	NewCodestralProvider = embed.NewCodestralProvider
	// NewNoOpProvider creates a no-op embedding provider.
	NewNoOpProvider = embed.NewNoOpProvider
)
