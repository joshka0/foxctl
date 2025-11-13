// Package sqliteutil centralizes helpers for working with SQLite-backed stores.
package sqliteutil

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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
		_ = db.Close()
		return nil, fmt.Errorf("sqliteutil: enable wal: %w", err)
	}
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return db, nil
}
