// Package vector provides optional sqlite-vector integration for semantic search.
// This package requires CGO and is only available when built with the "vector" build tag.
// Build with: CGO_ENABLED=1 go build -tags vector
package vector

import (
	"context"
	"database/sql"
	"fmt"
)

// Enabled indicates whether vector support is available in this build.
const Enabled = false

// Store provides vector search capabilities (disabled in default build).
type Store struct{}

// Entry represents a memory entry with vector embedding.
type Entry struct {
	ID        string
	Name      string
	Workspace string
	Summary   string
	Embedding []float32
	Distance  float64
}

// SearchOptions contains parameters for vector similarity search.
type SearchOptions struct {
	Embedding []float32
	Limit     int
	Workspace string
}

// NewStore creates a new vector store (noop in default build).
func NewStore(_ *sql.DB, _ string) (*Store, error) {
	return nil, fmt.Errorf("vector: not available in this build (requires CGO_ENABLED=1 and -tags vector)")
}

// Search performs vector similarity search (noop in default build).
func (s *Store) Search(_ context.Context, _ SearchOptions) ([]Entry, error) {
	return nil, fmt.Errorf("vector: not available in this build")
}

// SaveEmbedding stores a vector embedding (noop in default build).
func (s *Store) SaveEmbedding(_ context.Context, _, _, _ string, _ []float32) error {
	return fmt.Errorf("vector: not available in this build")
}

// Close releases resources (noop in default build).
func (s *Store) Close() error {
	return nil
}
