package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/tursodatabase/go-libsql"
)

// tursoDB wraps libsql connection to implement our DB interface
type tursoDB struct {
	db                 *sql.DB
	connector          *libsql.Connector
	enableVectorSearch bool
	vectorDimensions   int
	driverType         DriverType
}

// openTurso opens a Turso database connection
func openTurso(ctx context.Context, cfg TursoConfig, migrate MigrationFunc) (DB, error) {
	// Set default vector dimensions if not specified
	vectorDims := cfg.VectorDimensions
	if vectorDims == 0 {
		vectorDims = 384 // Default to all-MiniLM-L6-v2 dimensions
	}

	// Create libSQL connector for remote Turso database
	// Use empty string for local path to indicate remote-only mode
	connector, err := libsql.NewEmbeddedReplicaConnector("", cfg.URL,
		libsql.WithAuthToken(cfg.AuthToken),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create turso connector: %w", err)
	}

	// Open database connection
	db := sql.OpenDB(connector)

	// Test the connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to turso database: %w", err)
	}

	// Run migrations if provided
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	// If vector search is enabled for memory database, ensure vector columns exist
	if cfg.EnableVectorSearch {
		if err := ensureVectorSupport(ctx, db, vectorDims); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to enable vector search: %w", err)
		}
	}

	return &tursoDB{
		db:                 db,
		connector:          connector,
		enableVectorSearch: cfg.EnableVectorSearch,
		vectorDimensions:   vectorDims,
		driverType:         DriverTurso,
	}, nil
}

// ensureVectorSupport verifies that vector search is available
func ensureVectorSupport(ctx context.Context, db *sql.DB, dimensions int) error {
	// Test that vector functions are available by creating a temporary table
	// This will fail if vector support is not enabled in the Turso group
	testQuery := fmt.Sprintf(`
		CREATE TEMP TABLE IF NOT EXISTS _vector_test (
			id INTEGER PRIMARY KEY,
			embedding F32_BLOB(%d)
		)
	`, dimensions)

	if _, err := db.ExecContext(ctx, testQuery); err != nil {
		return fmt.Errorf("vector search not available (ensure your Turso group supports vectors): %w", err)
	}

	// Clean up test table (ignore errors on cleanup)
	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS _vector_test") //nolint:errcheck

	return nil
}

// Close closes the database connection
func (t *tursoDB) Close() error {
	var err error
	if t.db != nil {
		err = t.db.Close()
	}
	if t.connector != nil {
		if closeErr := t.connector.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// Exec executes a query without returning any rows
func (t *tursoDB) Exec(query string, args ...any) (sql.Result, error) {
	return t.db.Exec(query, args...)
}

// ExecContext executes a query without returning any rows with context
func (t *tursoDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, query, args...)
}

// Query executes a query that returns rows
func (t *tursoDB) Query(query string, args ...any) (*sql.Rows, error) {
	return t.db.Query(query, args...)
}

// QueryContext executes a query that returns rows with context
func (t *tursoDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that is expected to return at most one row
func (t *tursoDB) QueryRow(query string, args ...any) *sql.Row {
	return t.db.QueryRow(query, args...)
}

// QueryRowContext executes a query that is expected to return at most one row with context
func (t *tursoDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.db.QueryRowContext(ctx, query, args...)
}

// Begin starts a transaction
func (t *tursoDB) Begin() (*sql.Tx, error) {
	return t.db.Begin()
}

// BeginTx starts a transaction with context
func (t *tursoDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return t.db.BeginTx(ctx, opts)
}

// Ping verifies a connection to the database is still alive
func (t *tursoDB) Ping() error {
	return t.db.Ping()
}

// PingContext verifies a connection to the database is still alive with context
func (t *tursoDB) PingContext(ctx context.Context) error {
	return t.db.PingContext(ctx)
}

// SetMaxOpenConns sets the maximum number of open connections
func (t *tursoDB) SetMaxOpenConns(n int) {
	t.db.SetMaxOpenConns(n)
}

// SetMaxIdleConns sets the maximum number of idle connections
func (t *tursoDB) SetMaxIdleConns(n int) {
	t.db.SetMaxIdleConns(n)
}

// SetConnMaxLifetime sets the maximum amount of time a connection may be reused
func (t *tursoDB) SetConnMaxLifetime(d any) {
	if duration, ok := d.(time.Duration); ok {
		t.db.SetConnMaxLifetime(duration)
	}
}

// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle
func (t *tursoDB) SetConnMaxIdleTime(d any) {
	if duration, ok := d.(time.Duration); ok {
		t.db.SetConnMaxIdleTime(duration)
	}
}

// Stats returns database statistics
func (t *tursoDB) Stats() sql.DBStats {
	return t.db.Stats()
}

// GetUnderlyingDB returns the underlying *sql.DB
func (t *tursoDB) GetUnderlyingDB() (*sql.DB, bool) {
	return t.db, true
}

// IsVectorSearchEnabled returns true if vector search is enabled
func (t *tursoDB) IsVectorSearchEnabled() bool {
	return t.enableVectorSearch
}

// GetDriverType returns DriverTurso
func (t *tursoDB) GetDriverType() DriverType {
	return t.driverType
}

// GetVectorDimensions returns the configured vector dimensions
func (t *tursoDB) GetVectorDimensions() int {
	return t.vectorDimensions
}
