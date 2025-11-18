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
// This allows existing code to gradually adopt the new driver system
func OpenDBCompat(ctx context.Context, cfg Config, migrate func(context.Context, *sql.DB) error) (*sql.DB, error) {
	db, err := OpenDB(ctx, cfg, migrate)
	if err != nil {
		return nil, err
	}
	return ToSQLDB(db)
}
