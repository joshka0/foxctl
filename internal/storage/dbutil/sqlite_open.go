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
//
//	Purpose: Provide a dbutil-owned facade for opening pooled SQLite databases by path
//	Keywords: sqlite, open, pooled, dbutil_facade
//	Related: OpenStoreDB, sqliteutil.OpenDBShared
//	Flow: delegate to sqliteutil.OpenDBShared
//	Resources: filesystem path
//	Events: none
//	OutputFields: *sql.DB, closeFn
//
// [[protocol:store-driver-abstraction]]
// [[risk:filesystem-permission-denied]]
func OpenSQLiteDBShared(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, func() error, error) {
	return sqliteutil.OpenDBShared(ctx, path, migrate)
}

// OpenSQLiteInMemory opens an in-memory SQLite database and runs migrations.
//
// This is primarily intended for sandbox/read-only environments and deterministic tests.
//
// Index:
//
//	Purpose: Provide a dbutil-owned facade for opening an in-memory SQLite database
//	Keywords: sqlite, in_memory, dbutil_facade, sandbox
//	Related: sqliteutil.OpenInMemory
//	Flow: delegate to sqliteutil.OpenInMemory
//	Resources: in-memory SQLite
//	Events: none
//	OutputFields: *sql.DB
//
// [[protocol:store-driver-abstraction]]
// [[test-contract:in-memory-db-is-deterministic]]
func OpenSQLiteInMemory(ctx context.Context, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	return sqliteutil.OpenInMemory(ctx, migrate)
}
