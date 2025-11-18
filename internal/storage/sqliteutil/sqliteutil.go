// Package sqliteutil centralizes helpers for working with SQLite-backed stores.
package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"

	_ "modernc.org/sqlite" // register sqlite driver
)

// OpenDB creates parent directories for the provided path, opens a SQLite database,
// enables WAL journaling, and runs the provided migration function. Callers are
// responsible for closing the returned *sql.DB.
func OpenDB(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqliteutil: empty path")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sqliteutil: ensure dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqliteutil: open: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`); err != nil {
		errs.Ignore(db.Close(), "close sqlite db after WAL failure")
		return nil, fmt.Errorf("sqliteutil: enable wal: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout=5000;`); err != nil {
		errs.Ignore(db.Close(), "close sqlite db after busy_timeout failure")
		return nil, fmt.Errorf("sqliteutil: set busy_timeout: %w", err)
	}
	if _, err := db.ExecContext(ctx, `PRAGMA foreign_keys=ON;`); err != nil {
		errs.Ignore(db.Close(), "close sqlite db after foreign_keys failure")
		return nil, fmt.Errorf("sqliteutil: enable foreign_keys: %w", err)
	}
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			errs.Ignore(db.Close(), "close sqlite db after migrate failure")
			return nil, err
		}
	}
	return db, nil
}

// OpenDBWithDriver opens a database using the new driver abstraction system.
// This supports both SQLite and Turso databases based on configuration.
// The config can be loaded from environment variables using dbdriver.ConfigLoader.
func OpenDBWithDriver(ctx context.Context, cfg dbdriver.Config, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	return dbdriver.OpenDBCompat(ctx, cfg, migrate)
}

// OpenDBWithAutoConfig opens a database with automatic configuration detection.
// It checks environment variables to determine whether to use SQLite or Turso.
// dbType should be one of: "cache", "jobs", or "memory"
// defaultPath is the default SQLite path (e.g., "cache.db")
func OpenDBWithAutoConfig(ctx context.Context, rootDir string, dbType string, defaultPath string, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	loader := dbdriver.NewConfigLoader(rootDir)

	var cfg dbdriver.Config
	switch dbType {
	case "cache":
		cfg = loader.LoadCacheConfig()
	case "jobs":
		cfg = loader.LoadJobsConfig()
	case "memory":
		cfg = loader.LoadMemoryConfig()
	default:
		return nil, fmt.Errorf("sqliteutil: unknown database type: %s", dbType)
	}

	// Apply default path if not configured and using SQLite
	if (cfg.Driver == "" || cfg.Driver == dbdriver.DriverSQLite) && cfg.SQLite.Path == "" {
		cfg.SQLite.Path = defaultPath
	}

	return OpenDBWithDriver(ctx, cfg, migrate)
}
