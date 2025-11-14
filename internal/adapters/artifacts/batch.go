package artifacts

import (
	"context"
	"fmt"
)

// BatchOperation represents a group of artifact operations.
type BatchOperation struct {
	manager Manager
	pins    []string
	unpins  []string
}

// NewBatch creates a new batch operation.
func NewBatch(manager Manager) *BatchOperation {
	return &BatchOperation{
		manager: manager,
		pins:    make([]string, 0),
		unpins:  make([]string, 0),
	}
}

// Pin queues digests for pinning.
func (b *BatchOperation) Pin(digests ...string) *BatchOperation {
	b.pins = append(b.pins, digests...)
	return b
}

// Unpin queues digests for unpinning.
func (b *BatchOperation) Unpin(digests ...string) *BatchOperation {
	b.unpins = append(b.unpins, digests...)
	return b
}

// Execute performs all queued operations.
func (b *BatchOperation) Execute(ctx context.Context) error {
	// Pin first
	if err := b.manager.Pin(ctx, b.pins...); err != nil {
		return fmt.Errorf("batch pin: %w", err)
	}

	// Then unpin
	if err := b.manager.Unpin(ctx, b.unpins...); err != nil {
		return fmt.Errorf("batch unpin: %w", err)
	}

	return nil
}
