package artifacts

import (
	"context"
	"fmt"

	"github.com/jkatigb/agentctl/internal/storage"
)

// Manager handles artifact lifecycle operations.
type Manager interface {
	// ExtractDigests extracts artifact digests from an envelope
	ExtractDigests(envelope []byte) ([]string, error)

	// Pin pins artifacts to prevent garbage collection
	Pin(ctx context.Context, digests ...string) error

	// Unpin unpins artifacts, allowing garbage collection
	Unpin(ctx context.Context, digests ...string) error

	// PinFromEnvelope extracts and pins digests from an envelope
	PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error)

	// UnpinFromEnvelope extracts and unpins digests from an envelope
	UnpinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error)
}

// CASManager implements Manager using a CAS store.
type CASManager struct {
	store storage.CASStore
}

// NewManager creates a new artifact manager that uses the given CAS store.
func NewManager(store storage.CASStore) Manager {
	return &CASManager{store: store}
}

// ExtractDigests extracts artifact digests from an envelope.
func (m *CASManager) ExtractDigests(envelope []byte) ([]string, error) {
	// Use existing Digests function
	digests := Digests(envelope)
	return digests, nil
}

// Pin pins multiple artifacts to prevent garbage collection.
func (m *CASManager) Pin(ctx context.Context, digests ...string) error {
	if len(digests) == 0 {
		return nil
	}

	for _, digest := range digests {
		if err := m.store.Pin(ctx, digest); err != nil {
			return fmt.Errorf("pin %s: %w", digest, err)
		}
	}

	return nil
}

// Unpin unpins multiple artifacts, allowing garbage collection.
func (m *CASManager) Unpin(ctx context.Context, digests ...string) error {
	if len(digests) == 0 {
		return nil
	}

	for _, digest := range digests {
		if err := m.store.Unpin(ctx, digest); err != nil {
			return fmt.Errorf("unpin %s: %w", digest, err)
		}
	}

	return nil
}

// PinFromEnvelope extracts and pins digests in one operation.
func (m *CASManager) PinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
	digests, err := m.ExtractDigests(envelope)
	if err != nil {
		return nil, err
	}

	if err := m.Pin(ctx, digests...); err != nil {
		return nil, err
	}

	return digests, nil
}

// UnpinFromEnvelope extracts and unpins digests in one operation.
func (m *CASManager) UnpinFromEnvelope(ctx context.Context, envelope []byte) ([]string, error) {
	digests, err := m.ExtractDigests(envelope)
	if err != nil {
		return nil, err
	}

	if err := m.Unpin(ctx, digests...); err != nil {
		return nil, err
	}

	return digests, nil
}
