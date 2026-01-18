//go:build !cgo || race || vector

// Package sessions implements storage for captured Claude Code conversation sessions.
package sessions

import (
	"context"
	"errors"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// TursoStore is a stub for non-CGO builds.
type TursoStore struct{}

// OpenTurso returns an error when CGO is not available.
func OpenTurso(_ context.Context, _ dbdriver.TursoConfig) (*TursoStore, error) {
	return nil, errors.New("turso session store requires CGO (build with CGO_ENABLED=1)")
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
func (s *TursoStore) Save(_ context.Context, _ Session) (Session, error) {
	return Session{}, errors.New("turso store not available")
}

// Get is a stub.
func (s *TursoStore) Get(_ context.Context, _ string) (Session, error) {
	return Session{}, errors.New("turso store not available")
}

// List is a stub.
func (s *TursoStore) List(_ context.Context, _ ListOptions) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// Delete is a stub.
func (s *TursoStore) Delete(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// Search is a stub.
func (s *TursoStore) Search(_ context.Context, _ string, _ int) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// SearchSimilar is a stub.
func (s *TursoStore) SearchSimilar(_ context.Context, _ string, _ []float32, _ int) ([]storage.SimilarSession, error) {
	return nil, errors.New("turso store not available")
}

// UpdateSummary is a stub.
func (s *TursoStore) UpdateSummary(_ context.Context, _ string, _ string, _ []string, _ []string, _ []string, _ []string, _ []string, _ []string, _ string) error {
	return errors.New("turso store not available")
}

// SetEmbedding is a stub.
func (s *TursoStore) SetEmbedding(_ context.Context, _ string, _ []byte, _ string) error {
	return errors.New("turso store not available")
}

// FindByContentHash is a stub.
func (s *TursoStore) FindByContentHash(_ context.Context, _ string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// SetContentHash is a stub.
func (s *TursoStore) SetContentHash(_ context.Context, _, _ string) error {
	return errors.New("turso store not available")
}

// CreateVectorIndex is a stub.
func (s *TursoStore) CreateVectorIndex(_ context.Context) error {
	return errors.New("turso store not available")
}

// SaveTurn is a stub.
func (s *TursoStore) SaveTurn(_ context.Context, _ SessionTurn) (SessionTurn, error) {
	return SessionTurn{}, errors.New("turso store not available")
}

// SaveTurns is a stub.
func (s *TursoStore) SaveTurns(_ context.Context, _ []SessionTurn) error {
	return errors.New("turso store not available")
}

// GetTurns is a stub.
func (s *TursoStore) GetTurns(_ context.Context, _ string, _ TurnListOptions) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// GetTurnsWithErrors is a stub.
func (s *TursoStore) GetTurnsWithErrors(_ context.Context, _ string) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// SearchTurns is a stub.
func (s *TursoStore) SearchTurns(_ context.Context, _ string, _ int) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// DeleteTurns is a stub.
func (s *TursoStore) DeleteTurns(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// SaveChunk is a stub.
func (s *TursoStore) SaveChunk(_ context.Context, _ SessionChunk) (SessionChunk, error) {
	return SessionChunk{}, errors.New("turso store not available")
}

// SaveChunks is a stub.
func (s *TursoStore) SaveChunks(_ context.Context, _ []SessionChunk) error {
	return errors.New("turso store not available")
}

// GetChunks is a stub.
func (s *TursoStore) GetChunks(_ context.Context, _ string, _ int) ([]SessionChunk, error) {
	return nil, errors.New("turso store not available")
}

// GetChunk is a stub.
func (s *TursoStore) GetChunk(_ context.Context, _ string, _ int) (SessionChunk, error) {
	return SessionChunk{}, errors.New("turso store not available")
}

// SearchChunks is a stub.
func (s *TursoStore) SearchChunks(_ context.Context, _ []float32, _ int) ([]ScoredChunk, error) {
	return nil, errors.New("turso store not available")
}

// DeleteChunks is a stub.
func (s *TursoStore) DeleteChunks(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// DeleteChunkSummaries is a stub.
func (s *TursoStore) DeleteChunkSummaries(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// SetArchivePath is a stub.
func (s *TursoStore) SetArchivePath(_ context.Context, _ string, _ string) error {
	return errors.New("turso store not available")
}

// GetArchivePath is a stub.
func (s *TursoStore) GetArchivePath(_ context.Context, _ string) (string, error) {
	return "", errors.New("turso store not available")
}

// GetActive is a stub.
func (s *TursoStore) GetActive(_ context.Context, _ string, _ string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// SetStatus is a stub.
func (s *TursoStore) SetStatus(_ context.Context, _ string, _ string) error {
	return errors.New("turso store not available")
}

// SetPendingRestore is a stub.
func (s *TursoStore) SetPendingRestore(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// ClearPendingRestore is a stub.
func (s *TursoStore) ClearPendingRestore(_ context.Context, _ string) error {
	return errors.New("turso store not available")
}

// GetPendingRestore is a stub.
func (s *TursoStore) GetPendingRestore(_ context.Context, _ string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// FindLastSession is a stub.
func (s *TursoStore) FindLastSession(_ context.Context, _ string, _ string, _ []string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// SaveEdge is a stub.
func (s *TursoStore) SaveEdge(_ context.Context, _ storage.SessionEdge) error {
	return errors.New("turso store not available")
}

// GetAncestorChain is a stub.
func (s *TursoStore) GetAncestorChain(_ context.Context, _ string, _ int) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// GetEdges is a stub.
func (s *TursoStore) GetEdges(_ context.Context, _ string) ([]storage.SessionEdge, error) {
	return nil, errors.New("turso store not available")
}
