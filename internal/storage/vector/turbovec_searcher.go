package vector

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
)

// TurbovecSearcher uses the turbovec sidecar for approximate nearest-neighbor
// search with exact cosine reranking. On first Search call for a given index
// label (derived from indexPrefix + a sub-label), it batch-upserts all
// candidates into a named turbovec index. Subsequent searches reuse the
// populated index. Falls back to BruteForceSearcher if the sidecar is not
// reachable.
type TurbovecSearcher struct {
	mu          sync.Mutex
	client      *turbovec.Client
	indexPrefix string
	dim         uint32
	fallback    *BruteForceSearcher

	// populated tracks which index names have been populated in the sidecar.
	populated map[string]bool
	// idMaps[indexName] maps string ID → uint64 external ID for that index.
	idMaps map[string]map[string]uint64
	// reverseMaps[indexName] maps uint64 external ID → string ID.
	reverseMaps map[string]map[uint64]string
}

// Search returns the top-k most similar IDs using turbovec with exact reranking.
func (ts *TurbovecSearcher) Search(ctx context.Context, query []float32, candidates []IDEmbedding, k int) ([]ScoredID, error) {
	return ts.search(ctx, query, candidates, k, nil)
}

// SearchFiltered returns top-k results restricted to the given allowlist.
func (ts *TurbovecSearcher) SearchFiltered(ctx context.Context, query []float32, candidates []IDEmbedding, k int, allowlist map[string]bool) ([]ScoredID, error) {
	return ts.search(ctx, query, candidates, k, allowlist)
}

// IsAvailable returns true if the turbovec sidecar is reachable.
func (ts *TurbovecSearcher) IsAvailable() bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.client == nil {
		return false
	}
	return ts.client.Ping() == nil
}

func (ts *TurbovecSearcher) search(ctx context.Context, query []float32, candidates []IDEmbedding, k int, allowlist map[string]bool) ([]ScoredID, error) {
	if len(candidates) == 0 || k <= 0 {
		return nil, nil
	}

	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Use the indexPrefix as the index name.
	indexName := ts.indexPrefix

	// Ensure index is populated.
	if err := ts.ensureIndex(indexName, candidates); err != nil {
		// Fallback to brute force if sidecar is unreachable.
		return ts.fallback.Search(ctx, query, candidates, k)
	}

	// Oversample for better recall, then exact rerank.
	fetchK := uint32(k * 3)
	if fetchK < uint32(k) {
		fetchK = uint32(k) // overflow guard
	}
	if fetchK > uint32(len(candidates)) {
		fetchK = uint32(len(candidates))
	}

	var hits []turbovec.SearchHit
	var err error

	if allowlist != nil {
		// Build uint64 allowlist from string IDs.
		idMap := ts.idMaps[indexName]
		extIDs := make([]uint64, 0, len(allowlist))
		for id := range allowlist {
			if extID, ok := idMap[id]; ok {
				extIDs = append(extIDs, extID)
			}
		}
		if len(extIDs) == 0 {
			return nil, nil
		}
		hits, err = ts.client.SearchFiltered(indexName, query, fetchK, extIDs)
	} else {
		hits, err = ts.client.Search(indexName, query, fetchK)
	}

	if err != nil {
		// Fallback to brute force on error.
		return ts.fallback.Search(ctx, query, candidates, k)
	}

	// Translate hits back to string IDs and exact-rerank using cosine.
	reverse := ts.reverseMaps[indexName]
	// Build a quick lookup from string ID to embedding for reranking.
	embedMap := make(map[string][]float32, len(candidates))
	for _, c := range candidates {
		embedMap[c.ID] = c.Embedding
	}

	rerankCandidates := make([]ScoredID, 0, len(hits))
	for _, h := range hits {
		docID, ok := reverse[h.ID]
		if !ok {
			continue
		}
		vec := embedMap[docID]
		if vec == nil {
			continue
		}
		score := Cosine(query, vec)
		rerankCandidates = append(rerankCandidates, ScoredID{
			ID:    docID,
			Score: score,
		})
	}

	sort.Slice(rerankCandidates, func(i, j int) bool {
		return rerankCandidates[i].Score > rerankCandidates[j].Score
	})

	if len(rerankCandidates) > k {
		rerankCandidates = rerankCandidates[:k]
	}
	return rerankCandidates, nil
}

// ensureIndex creates and populates the turbovec index if not already done.
// Must be called with ts.mu held.
func (ts *TurbovecSearcher) ensureIndex(indexName string, candidates []IDEmbedding) error {
	if ts.populated[indexName] {
		return nil
	}

	if ts.client == nil {
		return fmt.Errorf("turbovec: client not connected")
	}

	// Check sidecar is alive.
	if err := ts.client.Ping(); err != nil {
		return fmt.Errorf("turbovec: sidecar unreachable: %w", err)
	}

	// Drop any stale index with the same name (idempotent creation).
	_ = ts.client.Drop(indexName)

	if err := ts.client.Create(indexName, ts.dim, 4); err != nil {
		return fmt.Errorf("turbovec: create index %q: %w", indexName, err)
	}

	if len(candidates) == 0 {
		ts.populated[indexName] = true
		return nil
	}

	// Assign uint64 IDs and batch-add all vectors.
	idMap := make(map[string]uint64, len(candidates))
	reverse := make(map[uint64]string, len(candidates))
	dim := int(ts.dim)
	totalFloats := len(candidates) * dim
	allVecs := make([]float32, 0, totalFloats)
	extIDs := make([]uint64, 0, len(candidates))

	for i, c := range candidates {
		if len(c.Embedding) != dim {
			continue
		}
		extID := uint64(i) + 1
		idMap[c.ID] = extID
		reverse[extID] = c.ID
		allVecs = append(allVecs, c.Embedding...)
		extIDs = append(extIDs, extID)
	}

	if len(extIDs) > 0 {
		if _, err := ts.client.AddBatch(indexName, allVecs, ts.dim, extIDs); err != nil {
			return fmt.Errorf("turbovec: batch add to %q: %w", indexName, err)
		}
	}

	ts.idMaps[indexName] = idMap
	ts.reverseMaps[indexName] = reverse
	ts.populated[indexName] = true
	return nil
}
