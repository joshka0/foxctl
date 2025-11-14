package sqlutil

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestMigratorMigrate(t *testing.T) {
	db := openTestDB(t)

	migrator := NewMigrator(db)
	migrator.Add(1, "create-things", `
CREATE TABLE things (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        name TEXT NOT NULL
);
`)
	migrator.Add(2, "create-index", `CREATE INDEX idx_things_name ON things(name);`)

	if err := migrator.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO things(name) VALUES ('alpha')`); err != nil {
		t.Fatalf("insert thing: %v", err)
	}

	if err := migrator.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied != 2 {
		t.Fatalf("expected 2 migrations applied, got %d", applied)
	}
}

func TestMigratorDetectsDuplicates(t *testing.T) {
	db := openTestDB(t)

	migrator := NewMigrator(db)
	migrator.Add(1, "one", `CREATE TABLE foo(id INTEGER);`)
	migrator.Add(1, "duplicate", `CREATE TABLE bar(id INTEGER);`)

	err := migrator.Migrate(context.Background())
	if err == nil {
		t.Fatalf("expected duplicate migration error")
	}
}

func TestMigratorValidatesInputs(t *testing.T) {
	db := openTestDB(t)

	migrator := NewMigrator(db)
	migrator.Add(0, "bad", `CREATE TABLE nope(id INTEGER);`)

	if err := migrator.Migrate(context.Background()); err == nil {
		t.Fatalf("expected error for non-positive version")
	}

	migrator = NewMigrator(db)
	migrator.Add(1, "empty", "")
	if err := migrator.Migrate(context.Background()); err == nil {
		t.Fatalf("expected error for empty up statement")
	}
}

func TestMigratorNilDB(t *testing.T) {
	var m *Migrator
	if err := m.Migrate(context.Background()); err == nil {
		t.Fatalf("expected error when migrator is nil")
	}

	m = NewMigrator(nil)
	if err := m.Migrate(context.Background()); err == nil {
		t.Fatalf("expected error when db is nil")
	}
}

func TestMigratorRecordsTimestamp(t *testing.T) {
	db := openTestDB(t)

	migrator := NewMigrator(db)
	migrator.Add(1, "init", `CREATE TABLE sample(id INTEGER);`)

	if err := migrator.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var appliedAt sql.NullString
	if err := db.QueryRow(`SELECT applied_at FROM schema_migrations WHERE version = 1`).Scan(&appliedAt); err != nil {
		t.Fatalf("fetch applied_at: %v", err)
	}
	if !appliedAt.Valid || appliedAt.String == "" {
		t.Fatalf("expected applied_at to be recorded")
	}
}
