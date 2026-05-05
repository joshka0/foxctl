package memory

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

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
//   - Turso: when database.driver=turso (local or remote sync)
//   - SQLite: when database.driver is empty or sqlite
//
// This is the recommended way to open a memory store in skills.
func OpenWithConfig(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	driver := dbdriver.DriverType(strings.ToLower(strings.TrimSpace(cfg.Database.Driver)))

	// Check environment variable override
	if envDriver := os.Getenv("FOXCTL_MEMORY_DB_DRIVER"); envDriver != "" {
		driver = dbdriver.DriverType(strings.ToLower(strings.TrimSpace(envDriver)))
	}

	switch driver {
	case dbdriver.DriverTurso:
		return openTursoFromConfig(ctx, cfg)
	case "", dbdriver.DriverSQLite:
		return Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	default:
		return nil, fmt.Errorf("memory: unsupported database driver %q", driver)
	}
}

// openTursoFromConfig opens a TursoStore from platform config.
func openTursoFromConfig(ctx context.Context, cfg config.Config) (*TursoStore, error) {
	tursoCfg := dbdriver.TursoConfig{
		URL:                cfg.Database.Turso.URL,
		AuthToken:          cfg.Database.Turso.AuthToken,
		EnableVectorSearch: true,
		VectorDimensions:   cfg.Database.Vector.Dimensions,
		Path:               cfg.Storage.Root + "/memory.turso",
		ReplicaPath:        cfg.Storage.Root + "/memory.turso",
	}

	store, err := OpenTurso(ctx, tursoCfg)
	if err != nil {
		return nil, fmt.Errorf("memory: open turso: %w", err)
	}

	if cfg.Database.Turso.URL != "" {
		logger.Info().Str("url", redactURL(cfg.Database.Turso.URL)).Bool("vector", true).Msg("opened Turso store with sync")
	} else {
		logger.Info().Str("path", tursoCfg.Path).Bool("vector", true).Msg("opened local Turso store")
	}
	return store, nil
}
