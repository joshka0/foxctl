// Package turbovec re-exports the foxsearch turbovec module.
//
// The implementation moved to github.com/joshka0/foxsearch/turbovec as part
// of the foxsearch extraction; this shim preserves the foxctl import path for
// existing consumers. New code may import either path.
//
// One foxctl-specific behavior is retained here: NewVectorIndex canonicalizes
// path-like workspace selectors via workspace.CanonicalID before delegating.
package turbovec

import (
	"github.com/joshka0/foxsearch/turbovec"

	"github.com/joshka0/foxctl/internal/platform/workspace"
)

type (
	// Client is a connection to the turbovec sidecar.
	Client = turbovec.Client
	// SearchHit is one result from a vector search.
	SearchHit = turbovec.SearchHit
	// IndexInfo describes a named sidecar index.
	IndexInfo = turbovec.IndexInfo
	// NotFoundError reports a missing index or ID.
	NotFoundError = turbovec.NotFoundError
	// IndexConfig configures a VectorIndex.
	IndexConfig = turbovec.IndexConfig
	// VectorIndex is a per-workspace ANN index backed by the sidecar.
	VectorIndex = turbovec.VectorIndex
	// SearchResult pairs a document ID with its similarity score.
	SearchResult = turbovec.SearchResult
)

const (
	// DefaultBitWidth is the default quantization bit width (4 = 16 buckets per coordinate).
	DefaultBitWidth = turbovec.DefaultBitWidth
)

var (
	// ErrDimMismatch is returned when a vector dimension does not match the index.
	ErrDimMismatch = turbovec.ErrDimMismatch
	// Dial connects to the sidecar at the given socket path.
	Dial = turbovec.Dial
	// DefaultSocketPath returns the default sidecar socket path.
	DefaultSocketPath = turbovec.DefaultSocketPath
	// IsAvailable reports whether a sidecar is reachable at the default socket.
	IsAvailable = turbovec.IsAvailable
)

// NewVectorIndex creates a VectorIndex for the given workspace, canonicalizing
// path-like workspace selectors to stable IDs (foxctl behavior).
func NewVectorIndex(workspaceID string, dim int, cfg IndexConfig) *VectorIndex {
	return turbovec.NewVectorIndex(workspace.CanonicalID(workspaceID), dim, cfg)
}
