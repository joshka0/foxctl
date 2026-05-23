package searchindex

import (
	"context"
)

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
	// CandidateIDs restricts the vector search to this set of document IDs.
	// When non-nil (even if empty), the turbovec-accelerated path uses
	// SearchFiltered instead of a full index scan, enabling the
	// BM25-then-vector pipeline where lexical hits are used as candidates.
	CandidateIDs []string
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

	// WorkspaceStats returns persisted retrieval corpus stats for a workspace.
	WorkspaceStats(ctx context.Context, workspaceID string) (WorkspaceStats, error)

	// GetEmbeddingMetadata returns the persisted embedding contract for a workspace.
	GetEmbeddingMetadata(ctx context.Context, workspaceID string) (*EmbeddingMetadata, error)

	// ValidateEmbeddingMetadata checks model and dimensions for a workspace.
	ValidateEmbeddingMetadata(ctx context.Context, workspaceID, model string, dimensions int) error

	// LexicalRecall returns raw scored matches scored by basic lexical matching.
	LexicalRecall(ctx context.Context, workspaceID, query string, opts RecallOptions) ([]SearchHit, error)

	// ExactRecall returns raw scored matches for exact symbol/title/path-style queries.
	ExactRecall(ctx context.Context, workspaceID, query string, opts ExactRecallOptions) ([]SearchHit, error)

	// VectorRecall returns raw scored matches based on embedding similarity.
	VectorRecall(ctx context.Context, workspaceID string, embedding []float32, opts VectorRecallOptions) ([]SearchHit, error)

	// GetEmbeddingsByIDs returns exact embeddings for the given document IDs.
	// IDs without a stored embedding are silently omitted from the result map.
	GetEmbeddingsByIDs(ctx context.Context, ids []string) (map[string][]float32, error)
}

type WorkspaceStats struct {
	WorkspaceID       string
	DocumentCount     int
	EmbeddedCount     int
	EmbeddingMetadata *EmbeddingMetadata
}
