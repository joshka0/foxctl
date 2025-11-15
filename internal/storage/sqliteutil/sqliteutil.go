// Package sqliteutil centralizes helpers for working with SQLite-backed stores.
package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"

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
