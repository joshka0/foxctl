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
func OpenTurso(ctx context.Context, cfg dbdriver.TursoConfig) (*TursoStore, error) {
	return nil, errors.New("turso session store requires CGO (build with CGO_ENABLED=1)")
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
func (s *TursoStore) Save(ctx context.Context, session Session) (Session, error) {
	return Session{}, errors.New("turso store not available")
}

// Get is a stub.
func (s *TursoStore) Get(ctx context.Context, id string) (Session, error) {
	return Session{}, errors.New("turso store not available")
}

// List is a stub.
func (s *TursoStore) List(ctx context.Context, opts ListOptions) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// Delete is a stub.
func (s *TursoStore) Delete(ctx context.Context, id string) error {
	return errors.New("turso store not available")
}

// Search is a stub.
func (s *TursoStore) Search(ctx context.Context, query string, limit int) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// SearchSimilar is a stub.
func (s *TursoStore) SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]storage.SimilarSession, error) {
	return nil, errors.New("turso store not available")
}

// UpdateSummary is a stub.
func (s *TursoStore) UpdateSummary(ctx context.Context, id string, summary string, accomplished, decisions, gotchas, userInsights, tags, keyFiles []string, toolsPattern string) error {
	return errors.New("turso store not available")
}

// SetEmbedding is a stub.
func (s *TursoStore) SetEmbedding(ctx context.Context, id string, embedding []byte, model string) error {
	return errors.New("turso store not available")
}

// CreateVectorIndex is a stub.
func (s *TursoStore) CreateVectorIndex(ctx context.Context) error {
	return errors.New("turso store not available")
}

// SaveTurn is a stub.
func (s *TursoStore) SaveTurn(ctx context.Context, turn SessionTurn) (SessionTurn, error) {
	return SessionTurn{}, errors.New("turso store not available")
}

// SaveTurns is a stub.
func (s *TursoStore) SaveTurns(ctx context.Context, turns []SessionTurn) error {
	return errors.New("turso store not available")
}

// GetTurns is a stub.
func (s *TursoStore) GetTurns(ctx context.Context, sessionID string, opts TurnListOptions) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// GetTurnsWithErrors is a stub.
func (s *TursoStore) GetTurnsWithErrors(ctx context.Context, sessionID string) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// SearchTurns is a stub.
func (s *TursoStore) SearchTurns(ctx context.Context, query string, limit int) ([]SessionTurn, error) {
	return nil, errors.New("turso store not available")
}

// DeleteTurns is a stub.
func (s *TursoStore) DeleteTurns(ctx context.Context, sessionID string) error {
	return errors.New("turso store not available")
}

// SaveChunk is a stub.
func (s *TursoStore) SaveChunk(ctx context.Context, chunk SessionChunk) (SessionChunk, error) {
	return SessionChunk{}, errors.New("turso store not available")
}

// SaveChunks is a stub.
func (s *TursoStore) SaveChunks(ctx context.Context, chunks []SessionChunk) error {
	return errors.New("turso store not available")
}

// GetChunks is a stub.
func (s *TursoStore) GetChunks(ctx context.Context, sessionID string, limit int) ([]SessionChunk, error) {
	return nil, errors.New("turso store not available")
}

// GetChunk is a stub.
func (s *TursoStore) GetChunk(ctx context.Context, sessionID string, chunkIndex int) (SessionChunk, error) {
	return SessionChunk{}, errors.New("turso store not available")
}

// SearchChunks is a stub.
func (s *TursoStore) SearchChunks(ctx context.Context, embedding []float32, limit int) ([]ScoredChunk, error) {
	return nil, errors.New("turso store not available")
}

// DeleteChunks is a stub.
func (s *TursoStore) DeleteChunks(ctx context.Context, sessionID string) error {
	return errors.New("turso store not available")
}

// SetArchivePath is a stub.
func (s *TursoStore) SetArchivePath(ctx context.Context, sessionID, archivePath string) error {
	return errors.New("turso store not available")
}

// GetArchivePath is a stub.
func (s *TursoStore) GetArchivePath(ctx context.Context, sessionID string) (string, error) {
	return "", errors.New("turso store not available")
}

// GetActive is a stub.
func (s *TursoStore) GetActive(ctx context.Context, workspace, agentID string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// SetStatus is a stub.
func (s *TursoStore) SetStatus(ctx context.Context, id, status string) error {
	return errors.New("turso store not available")
}

// FindLastSession is a stub.
func (s *TursoStore) FindLastSession(ctx context.Context, workspace, agentID string, statuses []string) (*Session, error) {
	return nil, errors.New("turso store not available")
}

// SaveEdge is a stub.
func (s *TursoStore) SaveEdge(ctx context.Context, edge storage.SessionEdge) error {
	return errors.New("turso store not available")
}

// GetAncestorChain is a stub.
func (s *TursoStore) GetAncestorChain(ctx context.Context, sessionID string, maxDepth int) ([]Session, error) {
	return nil, errors.New("turso store not available")
}

// GetEdges is a stub.
func (s *TursoStore) GetEdges(ctx context.Context, sessionID string) ([]storage.SessionEdge, error) {
	return nil, errors.New("turso store not available")
}
