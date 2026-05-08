package memoryutil

import (
	"context"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// Store is the configured named-memory backend used for embedding updates.
type Store = storage.MemoryStore

// SymbolSyncStore is implemented by stores that can copy queued symbol embeddings
// into named_memory rows. Local SQLite legacy storage supports this path.
type SymbolSyncStore interface {
	SyncSymbolEmbeddings(ctx context.Context, embeddingDBPath string, opts SyncSymbolEmbeddingsOptions) (int, error)
}

// SyncSymbolEmbeddingsOptions configures sync from symbol_embeddings into named_memory.
type SyncSymbolEmbeddingsOptions = memory.SyncSymbolEmbeddingsOptions

// OpenFromConfig opens a memory store from config.
func OpenFromConfig(ctx context.Context, cfg config.Config) (Store, error) {
	return memory.OpenWithConfig(ctx, cfg)
}
