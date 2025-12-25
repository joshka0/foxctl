package cas

import (
	"context"
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/storage"
)

// OpenCAS opens a CAS store based on the provided configuration.
// If auto-migration is enabled and a legacy file-based CAS exists,
// it will be migrated to the new store.
func OpenCAS(ctx context.Context, cfg Config) (storage.CASStore, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var store storage.CASStore
	var err error

	switch cfg.Driver {
	case DriverFile:
		store, err = NewStore(cfg.File.Path)
	case DriverSQLite:
		store, err = NewSQLiteStore(ctx, cfg.SQLite)
	case DriverTurso:
		store, err = NewTursoStore(ctx, cfg.Turso)
	default:
		// Fall back to file-based for backward compatibility
		store, err = NewStore(cfg.File.Path)
	}

	if err != nil {
		return nil, err
	}

	// Check for auto-migration
	if cfg.Migration.AutoMigrate && cfg.Driver != DriverFile {
		if err := autoMigrate(ctx, cfg, store); err != nil {
			// Log but don't fail - migration is best-effort
			fmt.Fprintf(os.Stderr, "cas: auto-migration warning: %v\n", err)
		}
	}

	return store, nil
}

// OpenDefault opens a CAS store using environment variables for configuration.
// This is the recommended way to open CAS for most use cases.
func OpenDefault(ctx context.Context, rootDir string) (storage.CASStore, error) {
	cfg := LoadConfig(rootDir)
	return OpenCAS(ctx, cfg)
}

// OpenFile opens a file-based CAS store (legacy mode).
func OpenFile(rootDir string) (storage.CASStore, error) {
	cfg := DefaultFileConfig(rootDir)
	return NewStore(cfg.File.Path)
}

// OpenSQLite opens a SQLite-based CAS store.
func OpenSQLite(ctx context.Context, dbPath string) (storage.CASStore, error) {
	cfg := Config{
		Driver: DriverSQLite,
		SQLite: SQLiteConfig{
			DBPath:        dbPath,
			BlobThreshold: 1 << 20,
			EnableWAL:     true,
			BusyTimeout:   5000,
		},
	}
	return NewSQLiteStore(ctx, cfg.SQLite)
}

// OpenTurso opens a Turso-based CAS store.
func OpenTurso(ctx context.Context, url, token string) (storage.CASStore, error) {
	cfg := Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			URL:           url,
			AuthToken:     token,
			BlobThreshold: 10 << 20,
		},
	}
	return NewTursoStore(ctx, cfg.Turso)
}
