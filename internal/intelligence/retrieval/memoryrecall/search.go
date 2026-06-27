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
