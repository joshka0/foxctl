package memory

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
		Path:               filepath.Join(cfg.Storage.Root, "memory.turso"),
		ReplicaPath:        filepath.Join(cfg.Storage.Root, "memory.turso"),
	}
	if url, ok := envString("FOXCTL_MEMORY_DB_URL"); ok {
		tursoCfg.URL = url
	} else if url := firstNonEmptyEnv("FOXCTL_TURSO_URL"); url != "" {
		tursoCfg.URL = url
	}
	if token, ok := envString("FOXCTL_MEMORY_DB_TOKEN"); ok {
		tursoCfg.AuthToken = token
	} else if token := firstNonEmptyEnv("FOXCTL_TURSO_TOKEN"); token != "" {
		tursoCfg.AuthToken = token
	}
	if path := firstNonEmptyEnv("FOXCTL_MEMORY_DB_PATH"); path != "" {
		path = expandUserPath(path)
		tursoCfg.Path = path
		tursoCfg.ReplicaPath = path
	}
	if dims, ok := firstPositiveEnvInt("FOXCTL_MEMORY_VECTOR_DIMS", "FOXCTL_VECTOR_DIMS"); ok {
		tursoCfg.VectorDimensions = dims
	}

	store, err := OpenTurso(ctx, tursoCfg)
	if err != nil {
		return nil, formatTursoOpenError(tursoCfg, err)
	}

	if tursoCfg.URL != "" {
		logger.Info().Str("url", redactURL(tursoCfg.URL)).Bool("vector", true).Msg("opened Turso store with sync")
	} else {
		logger.Info().Str("path", tursoCfg.Path).Bool("vector", true).Msg("opened local Turso store")
	}
	return store, nil
}

func formatTursoOpenError(cfg dbdriver.TursoConfig, err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "libsql_vector_idx") {
		return fmt.Errorf("memory: open turso: %w", err)
	}

	parts := []string{
		"legacy libSQL vector index detected",
		"use a fresh Turso memory database",
	}
	if cfg.URL != "" {
		parts = append(parts, "set FOXCTL_MEMORY_DB_URL to a memory-specific remote if the configured remote is stale")
	}
	if path := firstNonEmptyString(cfg.ReplicaPath, cfg.Path); path != "" {
		parts = append(parts, fmt.Sprintf("move local replica %q aside if only the local cache is stale", path))
	}
	return fmt.Errorf("memory: open turso: %s: %w", strings.Join(parts, "; "), err)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func envString(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	return strings.TrimSpace(value), ok
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func firstPositiveEnvInt(names ...string) (int, bool) {
	for _, name := range names {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func expandUserPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
