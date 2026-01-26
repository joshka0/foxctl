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
func OpenTurso(_ context.Context, _ dbdriver.TursoConfig) (*TursoStore, error) {
	return nil, errors.New("turso memory store requires CGO (build with CGO_ENABLED=1)")
}

// Close is a no-op stub.
func (s *TursoStore) Close() error {
	return errors.New("turso store not available")
}

// Stats is a stub.
func (s *TursoStore) Stats(_ context.Context) (Stats, error) {
	return Stats{}, errors.New("turso store not available")
}

// Save is a stub.
func (s *TursoStore) Save(_ context.Context, _ NamedEntry) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// SaveWithEmbedding is a stub.
func (s *TursoStore) SaveWithEmbedding(_ context.Context, _ NamedEntry, _ []float32, _ string) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// Get is a stub.
func (s *TursoStore) Get(_ context.Context, _ string, _ string) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// List is a stub.
func (s *TursoStore) List(_ context.Context, _ string, _ int) ([]NamedEntry, error) {
	return nil, errors.New("turso store not available")
}

// Delete is a stub.
func (s *TursoStore) Delete(_ context.Context, _ string, _ string) error {
	return errors.New("turso store not available")
}

// Search is a stub.
func (s *TursoStore) Search(_ context.Context, _ string, _ string, _ int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// SearchSimilar is a stub.
func (s *TursoStore) SearchSimilar(_ context.Context, _ string, _ []float32, _ int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// CreateVectorIndex is a stub.
func (s *TursoStore) CreateVectorIndex(_ context.Context) error {
	return errors.New("turso store not available")
}

// UpdateEmbedding is a stub.
func (s *TursoStore) UpdateEmbedding(_ context.Context, _ string, _ string, _ []float32) error {
	return errors.New("turso store not available")
}

// SearchSimilarGlobal is a stub for cross-workspace global search.
func (s *TursoStore) SearchSimilarGlobal(_ context.Context, _ []float32, _ int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// SearchSimilarMultiWorkspace is a stub for multi-workspace search.
func (s *TursoStore) SearchSimilarMultiWorkspace(_ context.Context, _ []string, _ []float32, _ int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// ListWorkspaces is a stub for listing all workspaces.
func (s *TursoStore) ListWorkspaces(_ context.Context) ([]string, error) {
	return nil, errors.New("turso store not available")
}

// DeleteByNamePrefix is a stub.
func (s *TursoStore) DeleteByNamePrefix(_ context.Context, _ string, _ string) (int, error) {
	return 0, errors.New("turso store not available")
}

// Update is a stub.
func (s *TursoStore) Update(_ context.Context, _ string, _ string, _ *string, _ *string) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// Relevant is a stub.
func (s *TursoStore) Relevant(_ context.Context, _ string, _ int) ([]ScoredEntry, error) {
	return nil, errors.New("turso store not available")
}

// SaveFromResult is a stub.
func (s *TursoStore) SaveFromResult(_ context.Context, _ string, _ string, _ string, _ string, _ []byte) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// SaveResult is a stub.
func (s *TursoStore) SaveResult(_ context.Context, _ SaveOptions) (NamedEntry, error) {
	return NamedEntry{}, errors.New("turso store not available")
}

// ListFiltered is a stub.
func (s *TursoStore) ListFiltered(_ context.Context, _ string, _ ListFilter, _ int, _ int) ([]NamedEntry, int, error) {
	return nil, 0, errors.New("turso store not available")
}

// ListWithoutEmbedding is a stub.
func (s *TursoStore) ListWithoutEmbedding(_ context.Context, _ string, _ int) ([]NamedEntry, error) {
	return nil, errors.New("turso store not available")
}

// ValidateEmbeddingDimensions is a stub.
func (s *TursoStore) ValidateEmbeddingDimensions(_ context.Context, _ string, _ int) error {
	return errors.New("turso store not available")
}

// SetEmbeddingMetadata is a stub.
func (s *TursoStore) SetEmbeddingMetadata(_ context.Context, _ EmbeddingMetadata) error {
	return errors.New("turso store not available")
}

// ExistsByNameSuffix is a stub.
func (s *TursoStore) ExistsByNameSuffix(_ context.Context, _ string, _ string) (bool, error) {
	return false, errors.New("turso store not available")
}

// UpdateAtomic is a stub.
func (s *TursoStore) UpdateAtomic(_ context.Context, _, _, _ string, _, _ []string) error {
	return errors.New("turso store not available")
}
