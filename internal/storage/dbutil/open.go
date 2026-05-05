package dbutil

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
)

// OpenStoreDB opens a database for the given logical store name using the dbdriver config system.
//
// Callers should pass:
// - storageRoot: the directory containing store databases (typically cfg.Storage.Root, e.g. "~/.foxctl/storage")
// - storeName: the canonical store name used for env var prefixes (e.g. "SESSIONS", "TASKS", "MEMORY")
// - defaultFile: the default DB filename for this store (e.g. "sessions.db")
//
// The returned closer releases any driver resources (pool lease, embedded replica connector, temp dirs).
//
// Index:
// - Purpose: Centralize store DB opening behind a driver-agnostic facade (sqlite/turso/postgres)
// - Flow: load dbdriver.Config from env → open via sqlite shared pool or dbdriver compat → return (*sql.DB, closeFn)
// - SideEffects: may create local replica directories/files; may run migrations
// - FailureModes: invalid config, filesystem permissions, network/auth errors for remote sync, migration errors
// - Related: dbdriver.ConfigLoader.LoadConfig, sqliteutil.OpenDBShared, dbdriver.OpenDBCompatWithCloser
// - Keywords: dbutil, dbdriver, store_db, turso, sqlite, postgres
func OpenStoreDB(
	ctx context.Context,
	storageRoot string,
	storeName string,
	defaultFile string,
	migrate func(context.Context, *sql.DB) error,
) (*sql.DB, func() error, error) {
	if storageRoot == "" {
		return nil, nil, fmt.Errorf("dbutil: storage root is required")
	}
	if storeName == "" {
		return nil, nil, fmt.Errorf("dbutil: store name is required")
	}
	if defaultFile == "" {
		return nil, nil, fmt.Errorf("dbutil: default file is required")
	}

	loader := dbdriver.NewConfigLoader(storageRoot)
	cfg := loader.LoadConfig(storeName, defaultFile)

	// Preserve existing SQLite pooling/migrate-once behavior for local files.
	// Other drivers currently open per caller; callsites that need pooling should
	// keep handles open at a higher level (server/daemon lifetime).
	if cfg.Driver == dbdriver.DriverSQLite {
		return sqliteutil.OpenDBShared(ctx, cfg.SQLite.Path, migrate)
	}

	return dbdriver.OpenDBCompatWithCloser(ctx, cfg, migrate)
}
