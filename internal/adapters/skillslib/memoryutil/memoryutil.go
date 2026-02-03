package memoryutil

import (
	"context"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Store is a narrow memory store interface for embedding updates.
type Store interface {
	UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error
	Close() error
}

// OpenFromConfig opens a memory store from config.
func OpenFromConfig(ctx context.Context, cfg config.Config) (Store, error) {
	return memory.OpenFromConfig(ctx, cfg)
}
