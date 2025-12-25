//go:build !cgo || race

// Package memory implements named memory storage for skill execution results and context data.
package memory

import (
	"context"
	"errors"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// VectorMemoryStore extends MemoryStore with vector search capabilities.
type VectorMemoryStore interface {
	// SearchSimilar finds entries similar to the given embedding using vector search.
	SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error)
	// SaveWithEmbedding saves a named memory with its embedding vector.
	SaveWithEmbedding(ctx context.Context, entry NamedEntry, embedding []float32, model string) (NamedEntry, error)
	// Close releases resources.
	Close() error
	// Stats returns memory statistics.
	Stats(ctx context.Context) (Stats, error)
}

// TursoStore is a stub for non-CGO builds.
type TursoStore struct{}

// OpenTurso returns an error when CGO is not available.
func OpenTurso(ctx context.Context, cfg dbdriver.TursoConfig) (*TursoStore, error) {
	return nil, errors.New("turso memory store requires CGO (build with CGO_ENABLED=1)")
}

// Close is a no-op stub.
func (s *TursoStore) Close() error {
	return errors.New("turso store not available")
}

// Stats is a stub.
func (s *TursoStore) Stats(ctx context.Context) (Stats, error) {
	return Stats{}, errors.New("turso store not available")
}

// Save is a stub.
func (s *TursoStore) Save(ctx context.Context, entry NamedEntry) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// SaveWithEmbedding is a stub.
func (s *TursoStore) SaveWithEmbedding(ctx context.Context, entry NamedEntry, embedding []float32, model string) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// Get is a stub.
func (s *TursoStore) Get(ctx context.Context, name, workspace string) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// List is a stub.
func (s *TursoStore) List(ctx context.Context, workspace string, limit int) ([]NamedEntry, error) {
	return nil, errors.New("turso store not available")
}

// Delete is a stub.
func (s *TursoStore) Delete(ctx context.Context, name, workspace string) error {
	return errors.New("turso store not available")
}

// Search is a stub.
func (s *TursoStore) Search(ctx context.Context, workspace, query string, limit int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// SearchSimilar is a stub.
func (s *TursoStore) SearchSimilar(ctx context.Context, workspace string, embedding []float32, limit int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// CreateVectorIndex is a stub.
func (s *TursoStore) CreateVectorIndex(ctx context.Context) error {
	return errors.New("turso store not available")
}

// UpdateEmbedding is a stub.
func (s *TursoStore) UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error {
	return errors.New("turso store not available")
}
