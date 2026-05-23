package dbutil

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestColumnExistsSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE example (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}

	exists, err := ColumnExists(ctx, db, "example", "name")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected example.name to exist")
	}

	exists, err = ColumnExists(ctx, db, "example", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected example.missing to be absent")
	}
}

func TestAddColumnIfNotExistsSuppressesDuplicateColumnOnly(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE example (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	if err := AddColumnIfNotExists(ctx, db, "example", "name", "TEXT", ""); err != nil {
		t.Fatalf("first add column: %v", err)
	}
	if err := AddColumnIfNotExists(ctx, db, "example", "name", "TEXT", ""); err != nil {
		t.Fatalf("duplicate add column should be suppressed: %v", err)
	}
	if err := AddColumnIfNotExists(ctx, db, "missing", "name", "TEXT", ""); err == nil {
		t.Fatal("expected missing table error")
	}
}
