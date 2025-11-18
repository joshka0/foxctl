package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tursodatabase/go-libsql"
)

// libsqlDB wraps libsql connection for local file-based database
type libsqlDB struct {
	db                 *sql.DB
	connector          *libsql.Connector
	enableVectorSearch bool
	vectorDimensions   int
	driverType         DriverType
}

// openLibSQL opens a local libSQL database file
func openLibSQL(ctx context.Context, cfg LibSQLConfig, migrate MigrationFunc) (DB, error) {
	// Create parent directories if they don't exist
	if dir := filepath.Dir(cfg.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Set default vector dimensions if not specified
	vectorDims := cfg.VectorDimensions
	if vectorDims == 0 {
		vectorDims = 384 // Default to all-MiniLM-L6-v2 dimensions
	}

	// Create libSQL connector for local file database
	// For local files, just pass the path directly
	connector, err := libsql.NewEmbeddedReplicaConnector(cfg.Path, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create libsql connector: %w", err)
	}

	// Open database connection
	db := sql.OpenDB(connector)

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to connect to libsql database: %w", err)
	}

	// Run migrations if provided
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			_ = db.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	// If vector search is enabled, ensure vector columns exist
	if cfg.EnableVectorSearch {
		if err := ensureVectorSupport(ctx, db, vectorDims); err != nil {
			_ = db.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to enable vector search: %w", err)
		}
	}

	return &libsqlDB{
		db:                 db,
		connector:          connector,
		enableVectorSearch: cfg.EnableVectorSearch,
		vectorDimensions:   vectorDims,
		driverType:         DriverLibSQL,
	}, nil
}

// Close closes the database connection
func (l *libsqlDB) Close() error {
	var err error
	if l.db != nil {
		err = l.db.Close()
	}
	if l.connector != nil {
		if closeErr := l.connector.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// Exec executes a query without returning any rows
func (l *libsqlDB) Exec(query string, args ...any) (sql.Result, error) {
	return l.db.Exec(query, args...)
}

// ExecContext executes a query without returning any rows with context
func (l *libsqlDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return l.db.ExecContext(ctx, query, args...)
}

// Query executes a query that returns rows
func (l *libsqlDB) Query(query string, args ...any) (*sql.Rows, error) {
	return l.db.Query(query, args...)
}

// QueryContext executes a query that returns rows with context
func (l *libsqlDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return l.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that is expected to return at most one row
func (l *libsqlDB) QueryRow(query string, args ...any) *sql.Row {
	return l.db.QueryRow(query, args...)
}

// QueryRowContext executes a query that is expected to return at most one row with context
func (l *libsqlDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return l.db.QueryRowContext(ctx, query, args...)
}

// Begin starts a transaction
func (l *libsqlDB) Begin() (*sql.Tx, error) {
	return l.db.Begin()
}

// BeginTx starts a transaction with context
func (l *libsqlDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return l.db.BeginTx(ctx, opts)
}

// Ping verifies a connection to the database is still alive
func (l *libsqlDB) Ping() error {
	return l.db.Ping()
}

// PingContext verifies a connection to the database is still alive with context
func (l *libsqlDB) PingContext(ctx context.Context) error {
	return l.db.PingContext(ctx)
}

// SetMaxOpenConns sets the maximum number of open connections
func (l *libsqlDB) SetMaxOpenConns(n int) {
	l.db.SetMaxOpenConns(n)
}

// SetMaxIdleConns sets the maximum number of idle connections
func (l *libsqlDB) SetMaxIdleConns(n int) {
	l.db.SetMaxIdleConns(n)
}

// SetConnMaxLifetime sets the maximum amount of time a connection may be reused
func (l *libsqlDB) SetConnMaxLifetime(d any) {
	if duration, ok := d.(time.Duration); ok {
		l.db.SetConnMaxLifetime(duration)
	}
}

// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle
func (l *libsqlDB) SetConnMaxIdleTime(d any) {
	if duration, ok := d.(time.Duration); ok {
		l.db.SetConnMaxIdleTime(duration)
	}
}

// Stats returns database statistics
func (l *libsqlDB) Stats() sql.DBStats {
	return l.db.Stats()
}

// GetUnderlyingDB returns the underlying *sql.DB
func (l *libsqlDB) GetUnderlyingDB() (*sql.DB, bool) {
	return l.db, true
}

// IsVectorSearchEnabled returns true if vector search is enabled
func (l *libsqlDB) IsVectorSearchEnabled() bool {
	return l.enableVectorSearch
}

// GetDriverType returns DriverLibSQL
func (l *libsqlDB) GetDriverType() DriverType {
	return l.driverType
}

// GetVectorDimensions returns the configured vector dimensions
func (l *libsqlDB) GetVectorDimensions() int {
	return l.vectorDimensions
}
