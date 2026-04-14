package skillmain

import (
	"context"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/rerank"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
)

// Well-known circuit breaker names for external service calls.
// Skills should use these constants with rc.Breakers.Execute(ctx, name, fn).
const (
	// BreakerEmbeddingAPI guards calls to embedding providers (Voyage, Gemini).
	BreakerEmbeddingAPI = "embedding_api"

	// BreakerLLMProvider guards calls to LLM chat/completion providers.
	BreakerLLMProvider = "llm_provider"

	// BreakerHTTP guards outbound HTTP requests to external services.
	BreakerHTTP = "http_external"

	// BreakerGitCLI guards shell-out calls to git and other CLI tools.
	BreakerGitCLI = "git_cli"
)

// GuardedProvider wraps a semantic.EmbeddingProvider with circuit breaker protection.
type GuardedProvider struct {
	Inner semantic.EmbeddingProvider
	guard semantic.GuardFunc
}

// Embed generates an embedding, routing through the circuit breaker.
func (g *GuardedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	var result []float32
	err := g.guard(ctx, func(ctx context.Context) error {
		var e error
		result, e = g.Inner.Embed(ctx, text)
		return e
	})
	return result, err
}

// EmbedBatch generates embeddings for multiple texts, routing through the circuit breaker.
func (g *GuardedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	var result [][]float32
	err := g.guard(ctx, func(ctx context.Context) error {
		var e error
		result, e = g.Inner.EmbedBatch(ctx, texts)
		return e
	})
	return result, err
}

// Model returns the model identifier.
func (g *GuardedProvider) Model() string { return g.Inner.Model() }

// Dimensions returns the embedding vector dimension.
func (g *GuardedProvider) Dimensions() int { return g.Inner.Dimensions() }

// GuardProvider wraps an EmbeddingProvider with circuit breaker protection.
func GuardProvider(rc *RunContext, p semantic.EmbeddingProvider) semantic.EmbeddingProvider {
	if p == nil {
		return nil
	}
	return &GuardedProvider{
		Inner: p,
		guard: func(ctx context.Context, fn func(context.Context) error) error {
			if rc.Breakers == nil {
				return fn(ctx)
			}
			return rc.Breakers.Execute(ctx, BreakerEmbeddingAPI, fn)
		},
	}
}

// GuardCall wraps an external service call with the named circuit breaker.
// The call function receives a context and returns an error; any non-nil error
// is recorded as a failure. Use for LLM, HTTP, and shell calls.
//
// Usage:
//
//	var result string
//	err := skillmain.GuardCall(rc, skillmain.BreakerLLMProvider, ctx, func(ctx context.Context) error {
//	    var e error
//	    result, e = callLLM(ctx, provider, prompt)
//	    return e
//	})
func GuardCall(rc *RunContext, breakerName string, ctx context.Context, fn func(context.Context) error) error {
	if rc.Breakers == nil {
		return fn(ctx)
	}
	return rc.Breakers.Execute(ctx, breakerName, fn)
}

// GuardedReranker wraps a rerank.Provider with circuit breaker protection.
type GuardedReranker struct {
	Inner rerank.Provider
	guard func(ctx context.Context, fn func(context.Context) error) error
}

// Rerank reorders candidates, routing through the circuit breaker.
func (g *GuardedReranker) Rerank(ctx context.Context, query string, candidates []rerank.Candidate, topK int) ([]rerank.RankedResult, error) {
	var result []rerank.RankedResult
	err := g.guard(ctx, func(ctx context.Context) error {
		var e error
		result, e = g.Inner.Rerank(ctx, query, candidates, topK)
		return e
	})
	return result, err
}

// Model returns the model identifier.
func (g *GuardedReranker) Model() string { return g.Inner.Model() }

// GuardReranker wraps a rerank.Provider with circuit breaker protection.
func GuardReranker(rc *RunContext, p rerank.Provider) rerank.Provider {
	if p == nil {
		return nil
	}
	return &GuardedReranker{
		Inner: p,
		guard: func(ctx context.Context, fn func(context.Context) error) error {
			if rc.Breakers == nil {
				return fn(ctx)
			}
			return rc.Breakers.Execute(ctx, BreakerHTTP, fn)
		},
	}
}

// EmbeddingGuard returns a semantic.EmbedderOption that routes embedding API
// calls through the circuit breaker manager in rc.
// Usage:
//
//	embedder, err := semantic.NewEmbedderFromConfig(scope, cfg,
//	    skillmain.EmbeddingGuard(rc),
//	)
func EmbeddingGuard(rc *RunContext) semantic.EmbedderOption {
	return semantic.WithGuardFunc(func(ctx context.Context, fn func(context.Context) error) error {
		if rc.Breakers == nil {
			return fn(ctx)
		}
		return rc.Breakers.Execute(ctx, BreakerEmbeddingAPI, fn)
	})
}
