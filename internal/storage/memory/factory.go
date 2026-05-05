package memory

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/rs/zerolog"
)

// redactURL removes credentials and query parameters from a URL for safe logging.
func redactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid-url>"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	return parsed.String()
}

var logger = zerolog.New(os.Stderr).With().Str("component", "memory").Timestamp().Logger()

// OpenWithConfig opens a memory store based on the provided configuration.
// It automatically selects the appropriate backend:
//   - Turso: when database.driver=turso and Turso URL is configured
//   - LibSQL: when database.driver=libsql (with optional sync)
//   - SQLite: default fallback
//
// This is the recommended way to open a memory store in skills.
func OpenWithConfig(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	driver := dbdriver.DriverType(cfg.Database.Driver)

	// Check environment variable override
	if envDriver := os.Getenv("FOXCTL_MEMORY_DB_DRIVER"); envDriver != "" {
		driver = dbdriver.DriverType(envDriver)
	}

	switch driver {
	case dbdriver.DriverTurso:
		return openTursoFromConfig(ctx, cfg)
	case dbdriver.DriverLibSQL:
		return openLibSQLFromConfig(ctx, cfg)
	default:
		return Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	}
}

// openTursoFromConfig opens a TursoStore from platform config.
func openTursoFromConfig(ctx context.Context, cfg config.Config) (*TursoStore, error) {
	if cfg.Database.Turso.URL == "" {
		return nil, fmt.Errorf("memory: turso URL not configured (set database.turso.url or FOXCTL_TURSO_URL)")
	}

	tursoCfg := dbdriver.TursoConfig{
		URL:                cfg.Database.Turso.URL,
		AuthToken:          cfg.Database.Turso.AuthToken,
		EnableVectorSearch: true,
		VectorDimensions:   cfg.Database.Vector.Dimensions,
		ReplicaPath:        cfg.Storage.Root + "/memory.turso.replica",
	}

	store, err := OpenTurso(ctx, tursoCfg)
	if err != nil {
		return nil, fmt.Errorf("memory: open turso: %w", err)
	}

	logger.Info().Str("url", redactURL(cfg.Database.Turso.URL)).Bool("vector", true).Msg("opened Turso store")
	return store, nil
}

// openLibSQLFromConfig opens a LibSQL-backed memory store from platform config.
// It supports both local-only libSQL files and embedded-replica sync mode.
func openLibSQLFromConfig(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	// Build LibSQL config from platform config and environment
	loader := dbdriver.NewConfigLoader(cfg.Storage.Root)
	dbCfg := loader.LoadMemoryConfig()

	// If driver is libsql but config returned something else, force libsql
	if dbCfg.Driver != dbdriver.DriverLibSQL {
		dbCfg = dbdriver.Config{
			Driver: dbdriver.DriverLibSQL,
			LibSQL: dbdriver.LibSQLConfig{
				Path:               cfg.Storage.Root + "/memory.libsql",
				EnableVectorSearch: true,
				VectorDimensions:   cfg.Database.Vector.Dimensions,
			},
		}
	}

	// Ensure vector search is enabled for memory
	dbCfg.LibSQL.EnableVectorSearch = true
	if cfg.Database.Vector.Dimensions > 0 {
		dbCfg.LibSQL.VectorDimensions = cfg.Database.Vector.Dimensions
	}

	store, err := OpenLibSQL(ctx, dbCfg.LibSQL)
	if err != nil {
		return nil, fmt.Errorf("memory: open libsql: %w", err)
	}

	if dbCfg.LibSQL.SyncURL != "" {
		logger.Info().Str("path", dbCfg.LibSQL.Path).Str("sync_url", dbCfg.LibSQL.SyncURL).Msg("opened LibSQL store with sync")
	} else {
		logger.Info().Str("path", dbCfg.LibSQL.Path).Msg("opened LibSQL store (local-only)")
	}

	return store, nil
}
