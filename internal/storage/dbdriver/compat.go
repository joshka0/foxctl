package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
)

// ToSQLDB converts a DB interface to *sql.DB for backward compatibility
// This allows existing code that expects *sql.DB to work with our abstraction
func ToSQLDB(db DB) (*sql.DB, error) {
	if sqlDB, ok := db.GetUnderlyingDB(); ok {
		return sqlDB, nil
	}
	return nil, fmt.Errorf("dbdriver: underlying *sql.DB not available for driver %T", db)
}

// WrapSQLDB wraps an existing *sql.DB to implement our DB interface
// This is useful for gradually migrating existing code
func WrapSQLDB(sqlDB *sql.DB, _ DriverType) DB {
	return &sqliteDB{db: sqlDB}
}

// OpenDBCompat is a backward-compatible version of OpenDB that returns *sql.DB
// OpenDBCompat opens a database via the package's DB layer and returns its underlying *sql.DB for backward compatibility with callers that expect the standard library type.
// It returns the underlying *sql.DB and nil on success; if opening the database or obtaining the underlying *sql.DB fails, it returns nil and the corresponding error.
func OpenDBCompat(ctx context.Context, cfg Config, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	db, err := OpenDB(ctx, cfg, migrate)
	if err != nil {
		return nil, err
	}
	return ToSQLDB(db)
}

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
