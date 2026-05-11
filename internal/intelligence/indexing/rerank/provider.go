package rerank

import (
	"context"
	"sync"
)

// Provider reranks candidate documents for a query.
// Implementations may call local models or deterministic test providers.
type Provider interface {
	// Rerank reorders candidates by relevance to the query.
	// Returns ranked results in descending order of relevance.
	// The topK parameter limits results; use 0 for all candidates.
	Rerank(ctx context.Context, query string, candidates []Candidate, topK int) ([]RankedResult, error)

	// Model returns the model identifier used by this provider.
	Model() string
}

// Candidate represents a document to be reranked.
type Candidate struct {
	// ID uniquely identifies this candidate (e.g., file path, chunk ID).
	ID string

	// Content is the text content to compare against the query.
	Content string

	// OriginalScore is the score from the first-stage retrieval (embedding/BM25).
	// Preserved for optional score blending.
	OriginalScore float64

	// Metadata holds additional context (passed through to results).
	Metadata map[string]any
}

// RankedResult is a reranked candidate with relevance score.
type RankedResult struct {
	// ID from the input candidate.
	ID string

	// Content from the input candidate.
	Content string

	// RerankScore is the relevance score from the reranker (0-1 scale).
	RerankScore float64

	// OriginalScore preserved from input candidate.
	OriginalScore float64

	// FinalScore combines rerank and original scores based on blend factor.
	// FinalScore = (1 - blend) * RerankScore + blend * OriginalScore
	FinalScore float64

	// OriginalRank is the 1-based position in the original candidate list.
	OriginalRank int

	// NewRank is the 1-based position after reranking.
	NewRank int

	// Metadata passed through from input candidate.
	Metadata map[string]any
}

// UsageTrackingProvider extends Provider with usage statistics.
type UsageTrackingProvider interface {
	Provider

	// Usage returns cumulative usage statistics since provider creation.
	Usage() Usage

	// ResetUsage resets the usage counters to zero.
	ResetUsage()
}

// Usage tracks API usage for reranking providers.
type Usage struct {
	// Provider identifies the reranking provider.
	Provider string `json:"provider"`

	// Model is the specific model used.
	Model string `json:"model"`

	// Requests is the number of API requests made.
	Requests int64 `json:"requests"`

	// CandidatesProcessed is the total number of candidates reranked.
	CandidatesProcessed int64 `json:"candidates_processed"`

	// TokensEstimated is the estimated input token count.
	TokensEstimated int64 `json:"tokens_estimated"`

	// TokensActual is the actual token count returned by the API.
	TokensActual int64 `json:"tokens_actual,omitempty"`

	// CostUSD is the estimated cost in USD.
	CostUSD float64 `json:"cost_usd"`
}

// Add adds another usage record to this one (for aggregation).
func (u *Usage) Add(other Usage) {
	u.Requests += other.Requests
	u.CandidatesProcessed += other.CandidatesProcessed
	u.TokensEstimated += other.TokensEstimated
	u.TokensActual += other.TokensActual
	u.CostUSD += other.CostUSD
}

// usageTracker provides thread-safe usage tracking for providers.
type usageTracker struct {
	mu    sync.Mutex
	usage Usage
}

func newUsageTracker(provider, model string) *usageTracker {
	return &usageTracker{
		usage: Usage{
			Provider: provider,
			Model:    model,
		},
	}
}

func (t *usageTracker) record(requests, candidatesProcessed int, estimatedTokens, actualTokens int64, costUSD float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.usage.Requests += int64(requests)
	t.usage.CandidatesProcessed += int64(candidatesProcessed)
	t.usage.TokensEstimated += estimatedTokens
	t.usage.TokensActual += actualTokens
	t.usage.CostUSD += costUSD
}

func (t *usageTracker) get() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.usage
}

func (t *usageTracker) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	provider := t.usage.Provider
	model := t.usage.Model
	t.usage = Usage{
		Provider: provider,
		Model:    model,
	}
}

// estimateTokens estimates token count from query and candidates.
// Uses ~4 characters per token as a rough approximation for English text.
func estimateTokens(query string, candidates []Candidate) int64 {
	totalChars := int64(len(query))
	for _, c := range candidates {
		totalChars += int64(len(c.Content))
	}
	// ~4 characters per token is a common approximation
	return (totalChars + 3) / 4
}

// NoOpProvider is a stub provider that returns candidates in original order.
// Used when reranking is not enabled or for testing.
type NoOpProvider struct {
	model string
}

// NewNoOpProvider creates a no-op reranking provider.
func NewNoOpProvider() *NoOpProvider {
	return &NoOpProvider{model: "noop"}
}

// Rerank returns candidates in original order with rerank score = original score.
func (p *NoOpProvider) Rerank(_ context.Context, _ string, candidates []Candidate, topK int) ([]RankedResult, error) {
	results := make([]RankedResult, len(candidates))
	for i, c := range candidates {
		results[i] = RankedResult{
			ID:            c.ID,
			Content:       c.Content,
			RerankScore:   c.OriginalScore,
			OriginalScore: c.OriginalScore,
			FinalScore:    c.OriginalScore,
			OriginalRank:  i + 1,
			NewRank:       i + 1,
			Metadata:      c.Metadata,
		}
	}
	if topK > 0 && topK < len(results) {
		results = results[:topK]
	}
	return results, nil
}

// Model returns the model identifier.
func (p *NoOpProvider) Model() string {
	return p.model
}
