package memoryutil

import (
	"context"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// Store is a narrow memory store interface for embedding updates.
type Store interface {
	UpdateEmbedding(ctx context.Context, name, workspace string, embedding []float32) error
	SyncSymbolEmbeddings(ctx context.Context, embeddingDBPath string, opts SyncSymbolEmbeddingsOptions) (int, error)
	Close() error
}

// SyncSymbolEmbeddingsOptions configures sync from symbol_embeddings into named_memory.
type SyncSymbolEmbeddingsOptions = memory.SyncSymbolEmbeddingsOptions

// OpenFromConfig opens a memory store from config.
func OpenFromConfig(ctx context.Context, cfg config.Config) (Store, error) {
	return memory.OpenFromConfig(ctx, cfg)
}
