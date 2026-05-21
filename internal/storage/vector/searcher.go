package vector

import (
	"context"

	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
)

// ScoredID is a string ID ranked by similarity score.
type ScoredID struct {
	ID    string
	Score float64
}

// IDEmbedding pairs a string ID with its embedding vector.
type IDEmbedding struct {
	ID        string
	Embedding []float32
}

// VectorSearcher performs approximate or exact nearest-neighbor search
// over a set of (id, embedding) pairs.
type VectorSearcher interface {
	// Search returns the top-k most similar IDs to the query vector.
	// candidates provides the full set of searchable items.
	// If the searcher is backed by a persistent index (e.g. turbovec),
	// candidates may be used for exact reranking only.
	Search(ctx context.Context, query []float32, candidates []IDEmbedding, k int) ([]ScoredID, error)

	// SearchFiltered returns top-k results restricted to the given allowlist.
	SearchFiltered(ctx context.Context, query []float32, candidates []IDEmbedding, k int, allowlist map[string]bool) ([]ScoredID, error)

	// IsAvailable returns true if the searcher backend is reachable.
	IsAvailable() bool
}

// NewSearcher returns a TurbovecSearcher if the sidecar is reachable,
// otherwise falls back to BruteForceSearcher.
// indexPrefix is used to namespace turbovec indices (e.g. "memory", "sessions").
// socketPath is the path to the turbovecd Unix socket.
// dim is the embedding dimensionality.
func NewSearcher(indexPrefix, socketPath string, dim int) VectorSearcher {
	client, err := turbovec.Dial(socketPath)
	if err != nil {
		return &BruteForceSearcher{}
	}
	if err := client.Ping(); err != nil {
		client.Close()
		return &BruteForceSearcher{}
	}
	return &TurbovecSearcher{
		client:      client,
		indexPrefix: indexPrefix,
		dim:         uint32(dim),
		fallback:    &BruteForceSearcher{},
		populated:   make(map[string]bool),
		idMaps:      make(map[string]map[string]uint64),
		reverseMaps: make(map[string]map[uint64]string),
	}
}
