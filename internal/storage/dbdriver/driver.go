package dbdriver

import (
	"context"
	"database/sql"
)

// DB is a unified database interface that works with both SQLite and Turso
type DB interface {
	// Close closes the database connection
	Close() error

	// Exec executes a query without returning any rows
	Exec(query string, args ...any) (sql.Result, error)

	// ExecContext executes a query without returning any rows with context
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)

	// Query executes a query that returns rows
	Query(query string, args ...any) (*sql.Rows, error)

	// QueryContext executes a query that returns rows with context
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)

	// QueryRow executes a query that is expected to return at most one row
	QueryRow(query string, args ...any) *sql.Row

	// QueryRowContext executes a query that is expected to return at most one row with context
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row

	// Begin starts a transaction
	Begin() (*sql.Tx, error)

	// BeginTx starts a transaction with context
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)

	// Ping verifies a connection to the database is still alive
	Ping() error

	// PingContext verifies a connection to the database is still alive with context
	PingContext(ctx context.Context) error

	// SetMaxOpenConns sets the maximum number of open connections
	SetMaxOpenConns(n int)

	// SetMaxIdleConns sets the maximum number of idle connections
	SetMaxIdleConns(n int)

	// SetConnMaxLifetime sets the maximum amount of time a connection may be reused
	SetConnMaxLifetime(d any)

	// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle
	SetConnMaxIdleTime(d any)

	// Stats returns database statistics
	Stats() sql.DBStats

	// GetUnderlyingDB returns the underlying *sql.DB if available
	// This is useful for operations that need direct access to sql.DB
	GetUnderlyingDB() (*sql.DB, bool)

	// IsVectorSearchEnabled returns true if vector search is available
	IsVectorSearchEnabled() bool

	// GetDriverType returns the driver type (sqlite or turso)
	GetDriverType() DriverType
}

// MigrationFunc is a function that performs database migrations
type MigrationFunc func(ctx context.Context, db *sql.DB) error

// OpenDB opens a database connection based on the provided configuration
func OpenDB(ctx context.Context, cfg Config, migrate MigrationFunc) (DB, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	switch cfg.Driver {
	case DriverSQLite:
		return openSQLite(ctx, cfg.SQLite, migrate)
	case DriverTurso:
		return openTurso(ctx, cfg.Turso, migrate)
	default:
		return nil, nil // unreachable due to Validate()
	}
}
