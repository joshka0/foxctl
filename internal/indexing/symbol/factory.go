package symbol

import (
	"github.com/jkatigb/agentctl/internal/indexing"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/rs/zerolog"
)

// Factory creates symbol indexers from configuration.
type Factory struct {
	memoryStore   storage.MemoryStore
	workspaceRoot string
	registry      *ExtractorRegistry
	logger        zerolog.Logger
}

// NewFactory creates a new symbol indexer factory.
func NewFactory(
	memoryStore storage.MemoryStore,
	workspaceRoot string,
	logger zerolog.Logger,
) *Factory {
	return &Factory{
		memoryStore:   memoryStore,
		workspaceRoot: workspaceRoot,
		registry:      DefaultRegistry(),
		logger:        logger,
	}
}

// WithRegistry sets a custom extractor registry.
func (f *Factory) WithRegistry(registry *ExtractorRegistry) *Factory {
	f.registry = registry
	return f
}

// Create creates a symbol indexer from an indexer config.
func (f *Factory) Create(cfg indexing.IndexerConfig) *Indexer {
	symConfig := Config{
		Enabled:      cfg.Enabled,
		MaxFileKB:    cfg.MaxFileKB,
		IncludeGlobs: cfg.IncludeGlobs,
		ExcludeGlobs: cfg.ExcludeGlobs,
	}

	// Extract symbol-specific config from Extra
	// Note: JSON-unmarshaled numbers are float64, not int
	if cfg.Extra != nil {
		switch v := cfg.Extra["max_file_loc"].(type) {
		case int:
			symConfig.MaxFileLOC = v
		case float64:
			symConfig.MaxFileLOC = int(v)
		}

		if langs, ok := cfg.Extra["languages"].([]any); ok {
			for _, l := range langs {
				if s, ok := l.(string); ok {
					symConfig.Languages = append(symConfig.Languages, s)
				}
			}
		}
	}

	return NewIndexer(symConfig, f.memoryStore, f.registry, f.workspaceRoot, f.logger)
}

// RegisterWithHandler registers a symbol indexer with a post-review handler.
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
