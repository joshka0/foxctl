package dbdriver

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// sqliteDB wraps *sql.DB for SQLite
// Note: The SQLite driver is registered in the sqliteutil package
type sqliteDB struct {
	db *sql.DB
}

// It returns true when the error message contains "database is locked" or "SQLITE_BUSY".
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY")
}

// openSQLite opens and configures a SQLite database according to cfg and runs optional migrations.
// It validates cfg.Path, creates parent directories for file-system paths (skipping in-memory and file: URIs),
// and uses a default busy timeout of 5000ms when cfg.BusyTimeout is zero.
// The function constructs a DSN, opens the database, applies PRAGMA settings (busy_timeout and foreign_keys),
// and optionally enables WAL mode for persistent databases when cfg.EnableWAL is true.
// If migrate is non-nil it runs migrations against the opened connection.
// On any configuration or migration failure the opened connection is closed before returning an error.
// It returns a DB wrapping the opened connection or an error.
func openSQLite(ctx context.Context, cfg SQLiteConfig, migrate MigrationFunc) (DB, error) {
	// Create parent directories if they don't exist
	if cfg.Path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	if !isInMemoryPath(cfg.Path) && !strings.HasPrefix(cfg.Path, "file:") {
		if dir := filepath.Dir(cfg.Path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}

	busyTimeout := cfg.BusyTimeout
	if busyTimeout == 0 {
		busyTimeout = 5000
	}
	dsn, err := buildSQLiteDSN(cfg.Path, busyTimeout)
	if err != nil {
		return nil, err
	}

	// Open the database
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if _, err := db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d;", busyTimeout)); err != nil {
		_ = db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Enable WAL mode for better concurrency
	if cfg.EnableWAL && !isInMemoryPath(cfg.Path) {
		var mode string
		if err := db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode); err != nil {
			if !isSQLiteBusy(err) {
				_ = db.Close() //nolint:errcheck
				return nil, fmt.Errorf("failed to check journal mode: %w", err)
			}
		} else if !strings.EqualFold(mode, "wal") {
			if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL;"); err != nil {
				if !isSQLiteBusy(err) {
					_ = db.Close() //nolint:errcheck
					return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
				}
			}
		}
	}

	// Ensure foreign keys are enabled on this connection
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON;"); err != nil {
		_ = db.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Run migrations if provided
	if migrate != nil {
		if err := migrate(ctx, db); err != nil {
			_ = db.Close() //nolint:errcheck
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	return &sqliteDB{db: db}, nil
}

// isInMemoryPath reports whether the given SQLite path refers to an in-memory database.
// It returns true for the literal ":memory:" path or for "file:" URIs that include "mode=memory".
func isInMemoryPath(path string) bool {
	if path == ":memory:" {
		return true
	}
	if strings.HasPrefix(path, "file:") && strings.Contains(path, "mode=memory") {
		return true
	}
	return false
}

// buildSQLiteDSN constructs a SQLite DSN URL for the provided path and busy timeout.
//
// buildSQLiteDSN accepts a filesystem path, a file-style DSN (starting with "file:"),
// or the special ":memory:" indicator. For ":memory:" it generates a unique in-memory
// name using a shared cache so connections to the same generated name share the same
// in-memory database. If busyTimeoutMs is less than or equal to zero, a default of
// 5000 milliseconds is used. The returned DSN always includes the busy timeout and
// enables the `foreign_keys` PRAGMA.
//
// It returns the formatted DSN string or an error if the path is empty, if a provided
// "file:" DSN cannot be parsed, or if required random bytes cannot be generated for
// an in-memory name.
func buildSQLiteDSN(path string, busyTimeoutMs int) (string, error) {
	if path == "" {
		return "", fmt.Errorf("sqlite path is required")
	}
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = 5000
	}

	var u *url.URL
	if path == ":memory:" {
		// Generate a unique name for each in-memory database to ensure isolation
		// (with cache=shared, all connections to the same name share the same DB)
		var randBytes [8]byte
		if _, err := rand.Read(randBytes[:]); err != nil {
			return "", fmt.Errorf("failed to generate random bytes for in-memory db: %w", err)
		}
		uniqueName := "agentctl_mem_" + hex.EncodeToString(randBytes[:])
		u = &url.URL{Scheme: "file", Path: uniqueName}
		q := u.Query()
		q.Set("mode", "memory")
		q.Set("cache", "shared")
		u.RawQuery = q.Encode()
	} else if strings.HasPrefix(path, "file:") {
		parsed, err := url.Parse(path)
		if err != nil {
			return "", fmt.Errorf("failed to parse sqlite dsn: %w", err)
		}
		u = parsed
	} else {
		// Convert to forward slashes for URL (required for Windows paths like C:\)
		urlPath := filepath.ToSlash(path)
		u = &url.URL{Scheme: "file", Path: urlPath}
	}

	q := u.Query()
	q.Set("_busy_timeout", strconv.Itoa(busyTimeoutMs))
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	return u.String(), nil
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