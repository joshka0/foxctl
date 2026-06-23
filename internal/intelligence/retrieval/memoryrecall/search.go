package memoryrecall

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/searchrank"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	DefaultMinSimilarity = 0.3

	MethodBM25   = "bm25"
	MethodVector = "vector"
	MethodHybrid = "hybrid"

	sourceVector  = searchrank.SourceID("vector")
	sourceLexical = searchrank.SourceID("lexical")

	// defaultStandardCandidateThreshold is the lifecycle gate for candidate
	// and stale memories in standard (point-fact) queries. It is intentionally
	// lower than the previous hardcoded 0.9, which suppressed legitimate
	// synonymy and paraphrase matches before they could reach evidence
	// verification.
	defaultStandardCandidateThreshold = 0.38
	// defaultDeepCandidateThreshold is the gate for deep (exploratory,
	// aggregation, categorical) queries where wider recall is preferred.
	defaultDeepCandidateThreshold = 0.28
)

// Store is the named-memory retrieval surface required for hybrid recall.
type Store interface {
	Search(ctx context.Context, workspace, query string, limit int) ([]storage.ScoredEntry, error)
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]storage.ScoredEntry, error)
}

type QueryRequest struct {
	Workspace      string
	Query          string
	QueryEmbedding []float32
	EmbeddingError error
	VectorContext  context.Context
	Limit          int
}

type QueryResponse struct {
	Entries []storage.ScoredEntry
	Method  string
	Hint    string
}

// Search runs the canonical named-memory recall flow: vector candidates and
// lexical candidates are fetched concurrently, then weighted fusion is applied
// when both sources are available. When vector search fails or times out,
// lexical results (already in flight) are used without waiting.
func Search(ctx context.Context, store Store, req QueryRequest) (QueryResponse, error) {
	if store == nil {
		return QueryResponse{Method: MethodBM25}, fmt.Errorf("memory recall store is nil")
	}
	if strings.TrimSpace(req.Query) == "" {
		return QueryResponse{Method: MethodBM25}, fmt.Errorf("memory recall query is empty")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}

	// If no embedding is available, skip straight to BM25.
	if len(req.QueryEmbedding) == 0 || req.EmbeddingError != nil {
		lexicalEntries, lexicalErr := store.Search(ctx, req.Workspace, req.Query, limit)
		if lexicalErr != nil {
			return QueryResponse{Method: MethodBM25}, lexicalErr
		}
		hint := ""
		if req.EmbeddingError != nil {
			hint = fmt.Sprintf("vector search failed: %v; using BM25", req.EmbeddingError)
		}
		return QueryResponse{Entries: lexicalEntries, Method: MethodBM25, Hint: hint}, nil
	}

	// Run vector and lexical concurrently.
	type vectorResult struct {
		entries []storage.ScoredEntry
		err     error
	}
	type lexicalResult struct {
		entries []storage.ScoredEntry
		err     error
	}

	vectorCh := make(chan vectorResult, 1)
	lexicalCh := make(chan lexicalResult, 1)

	go func() {
		entries, err := searchVector(ctx, store, req, limit)
		vectorCh <- vectorResult{entries: entries, err: err}
	}()
	go func() {
		entries, err := store.Search(ctx, req.Workspace, req.Query, limit)
		lexicalCh <- lexicalResult{entries: entries, err: err}
	}()

	vr := <-vectorCh
	lr := <-lexicalCh

	vectorEntries := vr.entries
	vectorErr := vr.err
	lexicalEntries := lr.entries
	lexicalErr := lr.err

	// Both failed.
	if vectorErr != nil && lexicalErr != nil {
		if ctx.Err() != nil {
			return QueryResponse{Method: MethodVector}, vectorErr
		}
		return QueryResponse{Method: MethodBM25}, lexicalErr
	}

	// Vector failed — use lexical.
	if vectorErr != nil {
		if len(lexicalEntries) == 0 {
			return QueryResponse{Method: MethodBM25}, lexicalErr
		}
		return QueryResponse{
			Entries: lexicalEntries,
			Method:  MethodBM25,
			Hint:    fmt.Sprintf("vector search failed: %v; using BM25", vectorErr),
		}, nil
	}

	// Lexical failed — use vector.
	if lexicalErr != nil {
		if len(vectorEntries) == 0 {
			return QueryResponse{Method: MethodVector}, lexicalErr
		}
		return QueryResponse{
			Entries: vectorEntries,
			Method:  MethodVector,
			Hint:    fmt.Sprintf("BM25 search failed: %v; using vector", lexicalErr),
		}, nil
	}

	// Both succeeded — fuse if both have entries.
	if len(vectorEntries) == 0 {
		return QueryResponse{
			Entries: lexicalEntries,
			Method:  MethodBM25,
			Hint:    "vector search returned no records; using BM25",
		}, nil
	}
	if len(lexicalEntries) == 0 {
		return QueryResponse{Entries: vectorEntries, Method: MethodVector}, nil
	}
	return QueryResponse{
		Entries: fuseResults(vectorEntries, lexicalEntries, limit),
		Method:  MethodHybrid,
	}, nil
}

func searchVector(ctx context.Context, store Store, req QueryRequest, limit int) ([]storage.ScoredEntry, error) {
	if req.EmbeddingError != nil {
		return nil, req.EmbeddingError
	}
	if len(req.QueryEmbedding) == 0 {
		return nil, fmt.Errorf("query embedding is empty")
	}
	vectorCtx := req.VectorContext
	if vectorCtx == nil {
		vectorCtx = ctx
	}
	return store.SearchSimilar(vectorCtx, req.Workspace, req.QueryEmbedding, limit)
}

// DefaultLifecycleAllows applies the named-memory recall default lifecycle
// gate. Explicit lifecycle filters live at the caller layer.
func DefaultLifecycleAllows(state string, score float64, query string) bool {
	return LifecycleAllowsThreshold(state, score, query, defaultStandardCandidateThreshold)
}

// LifecycleAllowsThreshold applies the named-memory recall lifecycle gate
// using the provided candidate threshold instead of the default standard
// threshold. Callers that classify queries as "deep" (exploratory,
// aggregation, categorical) should pass defaultDeepCandidateThreshold.
func LifecycleAllowsThreshold(state string, score float64, query string, candidateThreshold float64) bool {
	switch strings.TrimSpace(state) {
	case "", "active":
		return true
	case "candidate", "stale":
		if strings.TrimSpace(query) == "" {
			return false
		}
		return score >= candidateThreshold
	default:
		return false
	}
}

// QueryClassDeep returns true for queries that should use the deep-mode
// candidate threshold (wider recall). Deep queries include aggregations,
// broad categories, recommendations, and multi-session questions — patterns
// adapted from Quarq's search_mode heuristic but implemented as a pure
// string classifier without keyword heuristics on classification decisions.
// The classification is intentionally conservative: only queries that clearly
// ask for broad/exploratory recall are classified as deep.
func QueryClassDeep(query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return false
	}
	// Multi-clause questions (contain "and" joining two distinct question
	// clauses) are exploratory.
	clauseCount := strings.Count(q, "?")
	if clauseCount > 1 {
		return true
	}
	return false
}

// CandidateThresholdForQuery returns the appropriate candidate threshold
// for the given query.
func CandidateThresholdForQuery(query string) float64 {
	if QueryClassDeep(query) {
		return defaultDeepCandidateThreshold
	}
	return defaultStandardCandidateThreshold
}

// QuerySimilarityAllows applies the default named-memory query similarity gate.
func QuerySimilarityAllows(score float64, query string, minSimilarity float64) bool {
	if strings.TrimSpace(query) == "" {
		return true
	}
	if minSimilarity <= 0 {
		minSimilarity = DefaultMinSimilarity
	}
	return score >= minSimilarity
}

// NamedEntryText returns the default text surface for named-memory recall.
func NamedEntryText(entry storage.NamedEntry) string {
	base := firstNonEmpty(
		strings.TrimSpace(entry.AtomicText),
		strings.TrimSpace(entry.Summary),
		strings.TrimSpace(entry.Name),
	)
	tags := NamedEntryTags(entry)
	if len(tags) > 0 {
		base = strings.TrimSpace(base + " Tags: " + strings.Join(tags, ", "))
	}
	return base
}

// NamedEntryTags returns unique entity and keyword tags in stored order.
func NamedEntryTags(entry storage.NamedEntry) []string {
	seen := make(map[string]struct{})
	tags := make([]string, 0, len(entry.Entities)+len(entry.Keywords))
	for _, values := range [][]string{entry.Entities, entry.Keywords} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			tags = append(tags, value)
		}
	}
	return tags
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func fuseResults(vectorEntries, lexicalEntries []storage.ScoredEntry, limit int) []storage.ScoredEntry {
	sourceHits := map[searchrank.SourceID][]searchrank.SourceHit[storage.ScoredEntry]{
		sourceVector:  sourceHitsFromScoredEntries(sourceVector, vectorEntries),
		sourceLexical: sourceHitsFromScoredEntries(sourceLexical, lexicalEntries),
	}
	// BM25-dominant fusion weights: LongMemEval questions are mostly factual
	// with exact vocabulary overlap between question and evidence. Vector
	// search adds recall for synonymy but its lower precision on factual
	// queries causes regressions when weighted equally. BM25-dominant (0.75)
	// preserves exact-match ranking while still allowing vector results to
	// break ties and surface semantic near-misses.
	fused := searchrank.Fuse(sourceHits, searchrank.FuseOptions{
		Mode: searchrank.FuseModeWeighted,
		TopK: limit,
		RRFK: 60,
		SourceWeights: map[searchrank.SourceID]float64{
			sourceVector:  0.25,
			sourceLexical: 0.75,
		},
		MaxContributors: 2,
	})
	results := make([]storage.ScoredEntry, 0, len(fused))
	for _, hit := range fused {
		scored := hit.Document
		scored.Score = hit.Score
		results = append(results, scored)
	}
	return results
}

func sourceHitsFromScoredEntries(source searchrank.SourceID, entries []storage.ScoredEntry) []searchrank.SourceHit[storage.ScoredEntry] {
	hits := make([]searchrank.SourceHit[storage.ScoredEntry], 0, len(entries))
	for i, entry := range entries {
		id := strings.TrimSpace(entry.Entry.Name)
		if id == "" {
			id = strings.TrimSpace(entry.Entry.ID)
		}
		if id == "" {
			continue
		}
		hits = append(hits, searchrank.SourceHit[storage.ScoredEntry]{
			Source:   source,
			ID:       id,
			Document: entry,
			Score:    entry.Score,
			Rank:     i + 1,
		})
	}
	return hits
}
