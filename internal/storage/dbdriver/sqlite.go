package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sqliteDB wraps *sql.DB for SQLite
// Note: The SQLite driver is registered in the sqliteutil package
type sqliteDB struct {
	db *sql.DB
}

// openSQLite opens a SQLite database connection
func openSQLite(ctx context.Context, cfg SQLiteConfig, migrate MigrationFunc) (DB, error) {
	// Create parent directories if they don't exist
	if dir := filepath.Dir(cfg.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Open the database
	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if cfg.EnableWAL {
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
			_ = db.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
		}
	}

	// Set busy timeout
	busyTimeout := cfg.BusyTimeout
	if busyTimeout == 0 {
		busyTimeout = 5000
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", busyTimeout)); err != nil {
		_ = db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Enable foreign keys
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run migrations if provided
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return &sqliteDB{db: db}, nil
}

// Close closes the database connection
func (s *sqliteDB) Close() error {
	return s.db.Close()
}

// Exec executes a query without returning any rows
func (s *sqliteDB) Exec(query string, args ...any) (sql.Result, error) {
	return s.db.Exec(query, args...)
}

// ExecContext executes a query without returning any rows with context
func (s *sqliteDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

// Query executes a query that returns rows
func (s *sqliteDB) Query(query string, args ...any) (*sql.Rows, error) {
	return s.db.Query(query, args...)
}

// QueryContext executes a query that returns rows with context
func (s *sqliteDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// QueryRow executes a query that is expected to return at most one row
func (s *sqliteDB) QueryRow(query string, args ...any) *sql.Row {
	return s.db.QueryRow(query, args...)
}

// QueryRowContext executes a query that is expected to return at most one row with context
func (s *sqliteDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

// Begin starts a transaction
func (s *sqliteDB) Begin() (*sql.Tx, error) {
	return s.db.Begin()
}

// BeginTx starts a transaction with context
func (s *sqliteDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, opts)
}

// Ping verifies a connection to the database is still alive
func (s *sqliteDB) Ping() error {
	return s.db.Ping()
}

// PingContext verifies a connection to the database is still alive with context
func (s *sqliteDB) PingContext(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// SetMaxOpenConns sets the maximum number of open connections
func (s *sqliteDB) SetMaxOpenConns(n int) {
	s.db.SetMaxOpenConns(n)
}

// SetMaxIdleConns sets the maximum number of idle connections
func (s *sqliteDB) SetMaxIdleConns(n int) {
	s.db.SetMaxIdleConns(n)
}

// SetConnMaxLifetime sets the maximum amount of time a connection may be reused
func (s *sqliteDB) SetConnMaxLifetime(d any) {
	if duration, ok := d.(time.Duration); ok {
		s.db.SetConnMaxLifetime(duration)
	}
}

// SetConnMaxIdleTime sets the maximum amount of time a connection may be idle
func (s *sqliteDB) SetConnMaxIdleTime(d any) {
	if duration, ok := d.(time.Duration); ok {
		s.db.SetConnMaxIdleTime(duration)
	}
}

// Stats returns database statistics
func (s *sqliteDB) Stats() sql.DBStats {
	return s.db.Stats()
}

// GetUnderlyingDB returns the underlying *sql.DB
func (s *sqliteDB) GetUnderlyingDB() (*sql.DB, bool) {
	return s.db, true
}

// IsVectorSearchEnabled returns false for SQLite (no native vector search)
func (s *sqliteDB) IsVectorSearchEnabled() bool {
	return false
}

// GetDriverType returns DriverSQLite
func (s *sqliteDB) GetDriverType() DriverType {
	return DriverSQLite
}
