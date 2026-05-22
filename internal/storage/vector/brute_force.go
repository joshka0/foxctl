package vector

import (
	"context"
	"sort"
)

// BruteForceSearcher performs exact cosine similarity search over candidates
// in-process. It is always available and requires no external dependencies.
type BruteForceSearcher struct{}

// Search returns the top-k most similar IDs to the query vector using exact
// cosine similarity computed over all candidates.
func (s *BruteForceSearcher) Search(_ context.Context, query []float32, candidates []IDEmbedding, k int) ([]ScoredID, error) {
	return bruteForceSearch(query, candidates, k, nil), nil
}

// SearchFiltered returns top-k results restricted to the given allowlist.
func (s *BruteForceSearcher) SearchFiltered(_ context.Context, query []float32, candidates []IDEmbedding, k int, allowlist map[string]bool) ([]ScoredID, error) {
	return bruteForceSearch(query, candidates, k, allowlist), nil
}

// IsAvailable always returns true for brute-force search.
func (s *BruteForceSearcher) IsAvailable() bool {
	return true
}

// bruteForceSearch computes exact cosine similarity against all candidates
// (optionally filtered by allowlist) and returns the top-k scored IDs.
func bruteForceSearch(query []float32, candidates []IDEmbedding, k int, allowlist map[string]bool) []ScoredID {
	if len(candidates) == 0 || k <= 0 {
		return nil
	}

	scores := make([]ScoredID, 0, len(candidates))
	for _, c := range candidates {
		if allowlist != nil && !allowlist[c.ID] {
			continue
		}
		score := Cosine(query, c.Embedding)
		scores = append(scores, ScoredID{
			ID:    c.ID,
			Score: score,
		})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	if len(scores) > k {
		scores = scores[:k]
	}
	return scores
}
