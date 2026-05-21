package searchindex

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/joshka0/foxctl/internal/intelligence/turbovec"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

// TurboVecConfig controls the turbovec vector index integration.
type TurboVecConfig struct {
	// Enabled determines whether the turbovec sidecar is used for vector recall.
	// When false (default), VectorRecall falls back to brute-force SQLite cosine similarity.
	Enabled bool

	// SocketPath is the path to the turbovecd Unix socket.
	// Empty uses the default (~/.foxctl/turbovecd.sock).
	SocketPath string

	// DataDir is the directory for persisting compressed index files.
	// Empty uses the default (~/.foxctl/storage/).
	DataDir string

	// BitWidth is the quantization bit width (2, 3, or 4). Default: 4.
	BitWidth int

	// OversampleFactor controls how many extra candidates are fetched from the
	// approximate index before exact reranking. For example, with Limit=20 and
	// OversampleFactor=3, turbovec returns 60 candidates which are then reranked
	// using exact cosine similarity from SQLite, and only the top 20 are returned.
	// Default: 3 when turbovec is enabled and this field is 0.
	OversampleFactor int
}

// OpenWithTurboVec opens a SQL-backed search index and, when cfg.Enabled is
// true, wraps it with the turbovec-accelerated VectorRecall path. The caller
// must supply a non-empty workspaceID and the embedding dimensionality (dim)
// for the workspace so the turbovec index can be initialised correctly.
func OpenWithTurboVec(ctx context.Context, root, workspaceID string, dim int, cfg TurboVecConfig) (Store, error) {
	store, err := Open(ctx, root)
	if err != nil {
		return nil, err
	}
	return WrapWithTurboVec(store, workspaceID, dim, cfg), nil
}

// OpenEphemeralWithTurboVec creates an isolated search index under a temporary
// root and, when cfg.Enabled is true, wraps it with the turbovec accelerator.
// It returns the store and a cleanup function that closes the store and removes
// the temporary directory.
func OpenEphemeralWithTurboVec(ctx context.Context, baseRoot, workspaceID string, dim int, cfg TurboVecConfig) (Store, func() error, error) {
	store, cleanup, err := OpenEphemeral(ctx, baseRoot)
	if err != nil {
		return nil, nil, err
	}
	wrapped := WrapWithTurboVec(store, workspaceID, dim, cfg)
	return wrapped, cleanup, nil
}

// turbovecStore wraps a sqlStore and accelerates VectorRecall via the
// turbovec sidecar. All other operations (LexicalRecall, ExactRecall, Upsert,
// etc.) delegate to the underlying sqlStore.
//
// On Upsert, vectors are stored in both SQLite (for fallback) and the turbovec
// index (for fast search). On VectorRecall, if the turbovec sidecar is available
// the compressed index is queried; otherwise the brute-force SQLite path is used.
type turbovecStore struct {
	Store // underlying sqlStore
	vec   *turbovec.VectorIndex
	cfg   TurboVecConfig
	_     sync.Mutex // reserved for future concurrent access
	wsDim int        // embedding dimensions for the workspace
}

// WrapWithTurboVec wraps a Store with turbovec-accelerated VectorRecall.
// If cfg.Enabled is false, returns the underlying store unchanged.
func WrapWithTurboVec(store Store, workspaceID string, dim int, cfg TurboVecConfig) Store {
	if !cfg.Enabled {
		return store
	}

	// Default oversample factor when not explicitly set.
	if cfg.OversampleFactor <= 0 {
		cfg.OversampleFactor = 3
	}

	vec := turbovec.NewVectorIndex(workspaceID, dim, turbovec.IndexConfig{
		SocketPath: cfg.SocketPath,
		DataDir:    cfg.DataDir,
		BitWidth:   cfg.BitWidth,
	})

	return &turbovecStore{
		Store: store,
		vec:   vec,
		cfg:   cfg,
		wsDim: dim,
	}
}

// Upsert stores the document in SQLite and adds its embedding to the turbovec index.
func (s *turbovecStore) Upsert(ctx context.Context, doc Document) error {
	// Always store in SQL (fallback + metadata).
	if err := s.Store.Upsert(ctx, doc); err != nil {
		return err
	}

	// Add to turbovec index if we have an embedding.
	if len(doc.Embedding) > 0 {
		if err := s.vec.AddVector(doc.ID, doc.Embedding); err != nil {
			// Log but don't fail — SQL is the source of truth.
			// The turbovec index is a best-effort acceleration layer.
			fmt.Printf("turbovec: add vector warning: %v\n", err)
		}
	}

	return nil
}

// Delete removes the document from both SQLite and the turbovec index.
func (s *turbovecStore) Delete(ctx context.Context, id string) error {
	if err := s.Store.Delete(ctx, id); err != nil {
		return err
	}

	// Best-effort removal from turbovec.
	_ = s.vec.RemoveVector(id)
	return nil
}

// VectorRecall uses the turbovec sidecar for fast vector search, then reranks
// the oversampled candidates using exact cosine similarity from the SQL store.
// Falls back to the SQL brute-force path if the sidecar is unavailable.
func (s *turbovecStore) VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts VectorRecallOptions) ([]SearchHit, error) {
	workspaceID = workspace.CanonicalID(workspaceID)

	if opts.Limit <= 0 {
		opts.Limit = defaultSearchIndexLimit
	}

	// Oversample: request more candidates from the approximate index.
	k := opts.Limit * s.cfg.OversampleFactor

	// Try turbovec first. Use SearchFiltered when CandidateIDs is provided
	// (BM25-then-vector pipeline); otherwise do a full index scan.
	var (
		results []turbovec.SearchResult
		err     error
	)
	if opts.CandidateIDs != nil {
		results, err = s.vec.SearchFiltered(embedding, k, opts.CandidateIDs)
	} else {
		results, err = s.vec.Search(embedding, k)
	}
	if err != nil || len(results) == 0 {
		// Fall back to brute-force SQL.
		return s.Store.VectorRecall(ctx, workspaceID, embedding, opts)
	}

	// Collect candidate doc IDs.
	candidateIDs := make([]string, 0, len(results))
	for _, r := range results {
		candidateIDs = append(candidateIDs, r.DocID)
	}

	// Fetch exact embeddings for the candidate set from SQL.
	exactEmbeddings, err := s.Store.GetEmbeddingsByIDs(ctx, candidateIDs)
	if err != nil {
		// Rerank failed — return approximate results as-is.
		hits := make([]SearchHit, 0, len(results))
		for _, r := range results {
			hits = append(hits, SearchHit{
				Doc: Document{
					ID:          r.DocID,
					WorkspaceID: workspaceID,
				},
				Score: r.Score,
			})
		}
		if len(hits) > opts.Limit {
			hits = hits[:opts.Limit]
		}
		return hits, nil
	}

	// Rerank with exact cosine similarity.
	type scored struct {
		docID string
		score float64
	}
	reranked := make([]scored, 0, len(results))
	for _, r := range results {
		exactEmb, ok := exactEmbeddings[r.DocID]
		if !ok {
			continue // no embedding in SQL, skip
		}
		exactScore := cosineSimilarity(embedding, exactEmb)
		if exactScore <= 0 {
			continue
		}
		if opts.MinScore > 0 && exactScore < opts.MinScore {
			continue
		}
		reranked = append(reranked, scored{docID: r.DocID, score: exactScore})
	}

	// Sort by exact score descending.
	sort.Slice(reranked, func(i, j int) bool {
		return reranked[i].score > reranked[j].score
	})

	// Take top opts.Limit.
	if len(reranked) > opts.Limit {
		reranked = reranked[:opts.Limit]
	}

	hits := make([]SearchHit, 0, len(reranked))
	for _, r := range reranked {
		hits = append(hits, SearchHit{
			Doc: Document{
				ID:          r.docID,
				WorkspaceID: workspaceID,
			},
			Score: r.score,
		})
	}
	return hits, nil
}

// Close saves the turbovec index and closes the SQL store.
func (s *turbovecStore) Close() error {
	var errs []error
	if err := s.vec.Close(); err != nil {
		errs = append(errs, fmt.Errorf("turbovec close: %w", err))
	}
	if err := s.Store.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
