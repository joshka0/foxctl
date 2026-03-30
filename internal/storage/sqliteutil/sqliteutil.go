package sqliteutil

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
	"sync"
	"time"
	"unicode/utf8"

	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"

	_ "modernc.org/sqlite" // register sqlite driver
)

const (
	defaultBusyTimeoutMs = 5000
	openBusyRetryStep    = 50 * time.Millisecond
)

type migrateOnceEntry struct {
	once sync.Once
	err  error
}

// Ensures we don't re-run DDL migrations on every Open() in non-daemon mode.
// Keyed by DB path.
var migrateOnceByPath sync.Map // map[string]*migrateOnceEntry

func runMigrateOnce(ctx context.Context, path string, db *sql.DB, migrate func(context.Context, *sql.DB) error) error {
	if migrate == nil {
		return nil
	}
	// In-memory DBs are unique per open (buildSQLiteDSN generates unique names),
	// so we must migrate each time.
	if isInMemoryPath(path) {
		return migrate(ctx, db)
	}
	v, _ := migrateOnceByPath.LoadOrStore(path, &migrateOnceEntry{})
	entry := v.(*migrateOnceEntry)
	entry.once.Do(func() {
		entry.err = migrate(ctx, db)
	})
	return entry.err
}

// isSQLiteBusy reports whether err indicates that SQLite is busy.
// It returns false if err is nil, and true when the error message contains
// "database is locked" or "SQLITE_BUSY".
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "sqlite_busy")
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func retryBusyUntil(ctx context.Context, maxWait time.Duration, stepDelay time.Duration, fn func() error) error {
	if maxWait <= 0 {
		return fn()
	}
	if stepDelay <= 0 {
		stepDelay = 25 * time.Millisecond
	}
	deadline := time.Now().Add(maxWait)
	var lastErr error
	for {
		err := fn()
		if err == nil || !isSQLiteBusy(err) {
			return err
		}
		lastErr = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return lastErr
		}
		delay := stepDelay
		if remaining < delay {
			delay = remaining
		}
		if !sleepWithContext(ctx, delay) {
			return ctx.Err()
		}
	}
}

// validateDBPath reports an error if the provided filesystem path is unsafe or invalid
// for creating a SQLite database.
//
// It returns an error when the path:
// - contains a null byte,
// - is not valid UTF-8,
// - cannot be resolved to an absolute path, or
// - differs from its cleaned absolute form (indicating suspicious path components).
// Otherwise it returns nil.
func validateDBPath(path string) error {
	// Check for null bytes
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("sqliteutil: path contains null byte")
	}
	// Check for valid UTF-8
	if !utf8.ValidString(path) {
		return fmt.Errorf("sqliteutil: path contains invalid UTF-8")
	}
	// Ensure path resolves to an absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("sqliteutil: cannot resolve path: %w", err)
	}
	// Clean path and check it matches expectations
	cleaned := filepath.Clean(absPath)
	if cleaned != absPath {
		// Path had suspicious components that got cleaned away
		return fmt.Errorf("sqliteutil: path contains suspicious components")
	}
	return nil
}

// isInMemoryPath reports whether the given SQLite path represents an in-memory database.
// It returns `true` for ":memory:" or for "file:" URIs that include "mode=memory", and `false` otherwise.
func isInMemoryPath(path string) bool {
	if path == ":memory:" {
		return true
	}
	if strings.HasPrefix(path, "file:") && strings.Contains(path, "mode=memory") {
		return true
	}
	return false
}

// buildSQLiteDSN constructs a SQLite DSN for the given path with a busy timeout and optional foreign key enforcement.
//
// For empty path the function returns an error. If busyTimeoutMs is less than or equal to zero, a package default is used.
// The function recognizes in-memory paths (":memory:") and produces a unique, isolated in-memory DSN; it accepts already-formed
// DSNs beginning with "file:"; otherwise it builds a file-based DSN from the provided filesystem path.
// The resulting DSN always includes the `_busy_timeout` query parameter and, when enableForeignKeys is true, a `_pragma=foreign_keys(1)` parameter.
//
// It returns the constructed DSN string or an error if the path is empty, DSN parsing fails, or a required unique name cannot be generated.
func buildSQLiteDSN(path string, busyTimeoutMs int, enableForeignKeys bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("sqliteutil: empty path")
	}
	if busyTimeoutMs <= 0 {
		busyTimeoutMs = defaultBusyTimeoutMs
	}

	var u *url.URL
	if path == ":memory:" {
		// Generate unique name for each in-memory DB to ensure test isolation
		var randBytes [8]byte
		if _, err := rand.Read(randBytes[:]); err != nil {
			return "", fmt.Errorf("sqliteutil: generate unique name: %w", err)
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
			return "", fmt.Errorf("sqliteutil: parse dsn: %w", err)
		}
		u = parsed
	} else {
		// Convert to forward slashes for URL (required for Windows paths like C:\)
		urlPath := filepath.ToSlash(path)
		u = &url.URL{Scheme: "file", Path: urlPath}
	}

	q := u.Query()
	q.Set("_busy_timeout", strconv.Itoa(busyTimeoutMs))
	if enableForeignKeys {
		q.Add("_pragma", "foreign_keys(1)")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// OpenDB creates parent directories for the provided path, opens a SQLite database,
// enables WAL journaling, and runs the provided migration function. Callers are
// OpenDB opens a SQLite database for the given path, prepares the environment, and runs an optional migration.
//
// OpenDB validates the database path (except for in-memory paths), creates parent directories for file-backed
// databases, constructs an appropriate DSN with a default busy timeout, opens the connection, ensures the
// journal mode is WAL for file-backed databases, and enables foreign key enforcement (PRAGMA foreign_keys=ON).
// If a non-nil migrate function is provided it is executed before the open call returns. On any setup or
// migration failure the opened database is closed and the error is returned. The caller is responsible for
// closing the returned *sql.DB.
func OpenDB(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("sqliteutil: empty path")
	}
	if !isInMemoryPath(path) {
		var fsPath, resolvedPath string
		if strings.HasPrefix(path, "file:") {
			u, err := url.Parse(path)
			if err != nil {
				return nil, fmt.Errorf("sqliteutil: parse dsn: %w", err)
			}
			fsPath = filepath.FromSlash(u.Path)
			resolvedPath = path
		} else {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return nil, fmt.Errorf("sqliteutil: cannot resolve path: %w", err)
			}
			fsPath = absPath
			resolvedPath = absPath
		}
		// Validate path before creating directories
		if err := validateDBPath(fsPath); err != nil {
			return nil, err
		}
		dir := filepath.Dir(fsPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sqliteutil: ensure dir: %w", err)
		}
		path = resolvedPath
	}

	dsn, err := buildSQLiteDSN(path, defaultBusyTimeoutMs, true)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqliteutil: open: %w", err)
	}

	if !isInMemoryPath(path) {
		// Check if WAL is already enabled to avoid acquiring exclusive lock unnecessarily.
		// This prevents blocking for busy_timeout if the DB is held open by readers.
		var mode string
		if err := retryBusyUntil(ctx, defaultBusyTimeoutMs*time.Millisecond, openBusyRetryStep, func() error {
			return db.QueryRowContext(ctx, "PRAGMA journal_mode;").Scan(&mode)
		}); err != nil {
			errs.Ignore(db.Close(), "close sqlite db after journal_mode check failure")
			return nil, fmt.Errorf("sqliteutil: check journal_mode: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			if err := retryBusyUntil(ctx, defaultBusyTimeoutMs*time.Millisecond, openBusyRetryStep, func() error {
				_, execErr := db.ExecContext(ctx, `PRAGMA journal_mode=WAL;`)
				return execErr
			}); err != nil {
				errs.Ignore(db.Close(), "close sqlite db after WAL failure")
				return nil, fmt.Errorf("sqliteutil: enable wal: %w", err)
			}
		}
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

// OpenDBShared obtains a database handle for the given path, preferring a global shared pool when available.
// If a global pool is present, it returns a handle from the pool and a closer that releases the pooled resource
// for that path. If no pool is available, it opens a dedicated database and returns a closer that closes it.
// If `migrate` is non-nil it will be executed during open. The returned error is non-nil on failure.
func OpenDBShared(ctx context.Context, path string, migrate func(context.Context, *sql.DB) error) (*sql.DB, func() error, error) {
	if pool := GetGlobalPool(); pool != nil {
		db, err := pool.Get(ctx, path, migrate)
		if err != nil {
			return nil, nil, err
		}
		return db, func() error {
			pool.Release(path)
			return nil
		}, nil
	}

	db, err := OpenDB(ctx, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := runMigrateOnce(ctx, path, db, migrate); err != nil {
		errs.Ignore(db.Close(), "close sqlite db after migrate failure")
		return nil, nil, err
	}
	return db, db.Close, nil
}

// OpenInMemory opens a new SQLite in-memory database and applies optional migrations.
// If migrate is non-nil it is invoked with the opened DB; if it returns an error the
// connection is closed and that error is returned.
func OpenInMemory(ctx context.Context, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	return OpenDB(ctx, ":memory:", migrate)
}

// OpenDBWithDriver opens a database using the new driver abstraction system.
// This supports both SQLite and Turso databases based on configuration.
// OpenDBWithDriver opens a database using the provided driver configuration and returns a database handle and a closer function to release resources.
// If migrate is non-nil it will be executed; on migration failure the opened database is closed and the migration error is returned.
// The cfg may be populated from environment variables using dbdriver.ConfigLoader.
func OpenDBWithDriver(ctx context.Context, cfg dbdriver.Config, migrate func(context.Context, *sql.DB) error) (*sql.DB, func() error, error) {
	return dbdriver.OpenDBCompatWithCloser(ctx, cfg, migrate)
}
