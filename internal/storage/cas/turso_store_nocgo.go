//go:build !cgo || race

package cas

import (
	"context"
	"fmt"
	"io"

	"github.com/jkatigb/agentctl/internal/storage"
)

// TursoStore is a stub for non-cgo builds.
type TursoStore struct{}

// Ensure TursoStore implements storage.CASStore.
var _ storage.CASStore = (*TursoStore)(nil)

// NewTursoStore returns an error when cgo is not enabled.
func NewTursoStore(_ context.Context, _ TursoConfig) (*TursoStore, error) {
	return nil, fmt.Errorf("cas: turso driver requires cgo (build with CGO_ENABLED=1)")
}

// Close is a stub.
func (s *TursoStore) Close() error {
	return fmt.Errorf("cas: turso driver requires cgo")
}

// Put is a stub.
func (s *TursoStore) Put(_ context.Context, _ io.Reader, _ string, _ []string) (Object, error) {
	return Object{}, fmt.Errorf("cas: turso driver requires cgo")
}

// Get is a stub.
func (s *TursoStore) Get(_ context.Context, _ string) (io.ReadCloser, Metadata, error) {
	return nil, Metadata{}, fmt.Errorf("cas: turso driver requires cgo")
}

// Head is a stub.
func (s *TursoStore) Head(_ context.Context, _ string) (Object, error) {
	return Object{}, fmt.Errorf("cas: turso driver requires cgo")
}

// List is a stub.
func (s *TursoStore) List(_ context.Context) ([]Object, error) {
	return nil, fmt.Errorf("cas: turso driver requires cgo")
}

// Remove is a stub.
func (s *TursoStore) Remove(_ context.Context, _ string) error {
	return fmt.Errorf("cas: turso driver requires cgo")
}

// Pin is a stub.
func (s *TursoStore) Pin(_ context.Context, _ string) error {
	return fmt.Errorf("cas: turso driver requires cgo")
}

// Unpin is a stub.
func (s *TursoStore) Unpin(_ context.Context, _ string) error {
	return fmt.Errorf("cas: turso driver requires cgo")
}

// AddTags is a stub.
func (s *TursoStore) AddTags(_ context.Context, _ string, _ []string) error {
	return fmt.Errorf("cas: turso driver requires cgo")
}

// GC is a stub.
func (s *TursoStore) GC(_ context.Context, _ GCOptions) (GCResult, error) {
	return GCResult{}, fmt.Errorf("cas: turso driver requires cgo")
}
