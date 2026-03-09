package searchindex

import "context"

// RecallOptions configure lexical recall.
type RecallOptions struct {
	// Limit caps the number of returned hits. Defaults to 20.
	Limit int
	// MinScore filters low-confidence lexical matches.
	MinScore float64
}

// ExactRecallOptions configure exact-match recall.
type ExactRecallOptions struct {
	Limit int
}

// VectorRecallOptions configure embedding recall.
type VectorRecallOptions struct {
	// Limit caps the number of returned hits. Defaults to 20.
	Limit int
	// MinScore filters embedding matches below the threshold.
	MinScore float64
	// EmbeddingModel filters documents that were indexed with a different model.
	EmbeddingModel string
}

// Store exposes recall and maintenance operations for retrieval documents.
//
// This interface is intentionally narrow for phase-1 and avoids tying callers to
// storage-specific internals.
type Store interface {
	// Close releases the backing DB and any associated resources.
	Close() error

	// Upsert stores a single document, replacing existing rows with the same ID.
	Upsert(ctx context.Context, doc Document) error

	// Delete removes one document by ID.
	Delete(ctx context.Context, id string) error

	// DeleteWorkspace removes all documents for a workspace.
	DeleteWorkspace(ctx context.Context, workspaceID string) error

	// CountWorkspace returns the number of persisted documents for a workspace.
	CountWorkspace(ctx context.Context, workspaceID string) (int, error)

	// LexicalRecall returns raw scored matches scored by basic lexical matching.
	LexicalRecall(ctx context.Context, workspaceID, query string, opts RecallOptions) ([]SearchHit, error)

	// ExactRecall returns raw scored matches for exact symbol/title/path-style queries.
	ExactRecall(ctx context.Context, workspaceID, query string, opts ExactRecallOptions) ([]SearchHit, error)

	// VectorRecall returns raw scored matches based on embedding similarity.
	VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts VectorRecallOptions) ([]SearchHit, error)
}
