package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"
)

// ToSQLDB converts a DB interface to *sql.DB for backward compatibility
// This allows existing code that expects *sql.DB to work with our abstraction
func ToSQLDB(db DB) (*sql.DB, error) {
	if sqlDB, ok := db.GetUnderlyingDB(); ok {
		return sqlDB, nil
	}
	return nil, fmt.Errorf("dbdriver: underlying *sql.DB not available for driver %T", db)
}

// WrapSQLDB wraps an existing *sql.DB to implement our DB interface.
// This is useful for gradually migrating existing code.
// The DriverType parameter determines which dialect is reported by GetDialect().
func WrapSQLDB(sqlDB *sql.DB, dt DriverType) DB {
	return &wrappedDB{db: sqlDB, driverType: dt, dialect: DialectFor(dt)}
}

// wrappedDB wraps *sql.DB with a specific driver type and dialect.
type wrappedDB struct {
	db         *sql.DB
	driverType DriverType
	dialect    Dialect
}

func (w *wrappedDB) Close() error                                   { return w.db.Close() }
func (w *wrappedDB) Exec(q string, args ...any) (sql.Result, error) { return w.db.Exec(q, args...) }
func (w *wrappedDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return w.db.ExecContext(ctx, q, args...)
}

func (w *wrappedDB) Query(q string, args ...any) (*sql.Rows, error) { return w.db.Query(q, args...) }

func (w *wrappedDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return w.db.QueryContext(ctx, q, args...)
}
func (w *wrappedDB) QueryRow(q string, args ...any) *sql.Row { return w.db.QueryRow(q, args...) }
func (w *wrappedDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return w.db.QueryRowContext(ctx, q, args...)
}
func (w *wrappedDB) Begin() (*sql.Tx, error) { return w.db.Begin() }
func (w *wrappedDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return w.db.BeginTx(ctx, opts)
}
func (w *wrappedDB) Ping() error                           { return w.db.Ping() }
func (w *wrappedDB) PingContext(ctx context.Context) error { return w.db.PingContext(ctx) }
func (w *wrappedDB) SetMaxOpenConns(n int)                 { w.db.SetMaxOpenConns(n) }
func (w *wrappedDB) SetMaxIdleConns(n int)                 { w.db.SetMaxIdleConns(n) }
func (w *wrappedDB) SetConnMaxLifetime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		w.db.SetConnMaxLifetime(dur)
	}
}

func (w *wrappedDB) SetConnMaxIdleTime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		w.db.SetConnMaxIdleTime(dur)
	}
}
func (w *wrappedDB) Stats() sql.DBStats               { return w.db.Stats() }
func (w *wrappedDB) GetUnderlyingDB() (*sql.DB, bool) { return w.db, true }
func (w *wrappedDB) IsVectorSearchEnabled() bool      { return false }
func (w *wrappedDB) GetDriverType() DriverType        { return w.driverType }
func (w *wrappedDB) GetDialect() Dialect              { return w.dialect }

// OpenDBCompatWithCloser opens a database using the provided configuration and migration function and returns the underlying *sql.DB along with a closer function to release driver resources.
// If opening the database or obtaining the underlying *sql.DB fails, any opened resources are closed and the error is returned.
func OpenDBCompatWithCloser(ctx context.Context, cfg Config, migrate func(context.Context, *sql.DB) error) (*sql.DB, func() error, error) {
	db, err := OpenDB(ctx, cfg, migrate)
	if err != nil {
		return nil, nil, err
	}

	sqlDB, err := ToSQLDB(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	return sqlDB, db.Close, nil
}

// parseConnDuration accepts a time.Duration or a string parsable by time.ParseDuration.
// Returns the parsed duration and true on success, or zero and false on failure
// (with a diagnostic written to stderr for unexpected types).
func parseConnDuration(d any) (time.Duration, bool) {
	switch v := d.(type) {
	case time.Duration:
		return v, true
	case string:
		dur, err := time.ParseDuration(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dbdriver: invalid duration string %q: %v (expected time.Duration or parseable string like \"5m\")\n", v, err)
			return 0, false
		}
		return dur, true
	default:
		fmt.Fprintf(os.Stderr, "dbdriver: unexpected type %T for connection duration (expected time.Duration or string)\n", d)
		return 0, false
	}
}
