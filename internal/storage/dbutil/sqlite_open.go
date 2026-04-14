package dbutil

import (
	"context"
	"database/sql"

	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
)

// OpenSQLiteDBShared opens a SQLite database at an explicit filesystem path using the shared SQLite pool.
//
// This is a small adapter to keep callers from importing sqliteutil directly while we consolidate all
// database-opening behavior behind dbutil.
//
// Index:
// - Purpose: Provide a dbutil-owned facade for opening pooled SQLite databases by path
// - Flow: delegate to sqliteutil.OpenDBShared
// - SideEffects: may create parent directories; may run migrations; may configure WAL/busy timeout (sqliteutil behavior)
// - FailureModes: filesystem permissions, migration errors, SQLite open errors
// - Related: OpenStoreDB, sqliteutil.OpenDBShared
// - Keywords: sqlite, open, pooled, dbutil_facade
func OpenSQLiteDBShared(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, func() error, error) {
	return sqliteutil.OpenDBShared(ctx, path, migrate)
}

// OpenSQLiteInMemory opens an in-memory SQLite database and runs migrations.
//
// This is primarily intended for sandbox/read-only environments and deterministic tests.
//
// Index:
// - Purpose: Provide a dbutil-owned facade for opening an in-memory SQLite database
// - Flow: delegate to sqliteutil.OpenInMemory
// - SideEffects: allocates an in-memory SQLite DB; may run migrations
// - FailureModes: migration errors, SQLite open errors
// - Related: sqliteutil.OpenInMemory
// - Keywords: sqlite, in_memory, dbutil_facade, sandbox
func OpenSQLiteInMemory(ctx context.Context, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	return sqliteutil.OpenInMemory(ctx, migrate)
}
