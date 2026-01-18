package semantic

import (
	"encoding/json"

	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/rs/zerolog"
)

// Factory creates semantic indexers from configuration.
type Factory struct {
	memoryStore   storage.MemoryStore
	workspaceRoot string
	logger        zerolog.Logger
}

// NewFactory creates a new semantic indexer factory.
func NewFactory(
	memoryStore storage.MemoryStore,
	workspaceRoot string,
	logger zerolog.Logger,
) *Factory {
	return &Factory{
		memoryStore:   memoryStore,
		workspaceRoot: workspaceRoot,
		logger:        logger,
	}
}

// Create creates a semantic indexer from an indexer config.
// The config.Extra field may contain semantic-specific settings.
func (f *Factory) Create(cfg indexing.IndexerConfig) *Indexer {
	semConfig := configFromIndexer(cfg)

	// Use no-op provider by default (real providers can be swapped in)
	provider := NewNoOpProvider(semConfig.ProviderModel, 384)

	return NewIndexer(semConfig, f.memoryStore, provider, f.workspaceRoot, f.logger)
}

// CreateWithProvider creates a semantic indexer with a custom embedding provider.
func (f *Factory) CreateWithProvider(cfg indexing.IndexerConfig, provider EmbeddingProvider) *Indexer {
	semConfig := configFromIndexer(cfg)

	return NewIndexer(semConfig, f.memoryStore, provider, f.workspaceRoot, f.logger)
}

func configFromIndexer(cfg indexing.IndexerConfig) Config {
	semConfig := Config{
		Enabled:      cfg.Enabled,
		MaxFileKB:    cfg.MaxFileKB,
		IncludeGlobs: cfg.IncludeGlobs,
		ExcludeGlobs: cfg.ExcludeGlobs,
	}

	if cfg.Extra == nil {
		return semConfig
	}

	semConfig.ChunkBytes = extraInt(cfg.Extra, "chunk_bytes")
	semConfig.ChunkOverlapBytes = extraInt(cfg.Extra, "chunk_overlap_bytes")
	if v, ok := cfg.Extra["provider_model"].(string); ok {
		semConfig.ProviderModel = v
	}

	return semConfig
}

func extraInt(extra map[string]any, key string) int {
	switch v := extra[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	}
	return 0
}

// RegisterWithHandler registers a semantic indexer with a post-review handler.
// This is a convenience method for typical setups.
func RegisterWithHandler(
	handler *indexing.PostReviewHandler,
	memoryStore storage.MemoryStore,
	workspaceRoot string,
	cfg indexing.IndexerConfig,
	logger zerolog.Logger,
) error {
	factory := NewFactory(memoryStore, workspaceRoot, logger)
	indexer := factory.Create(cfg)
	return handler.RegisterIndexer(indexer)
}
