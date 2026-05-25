package dbutil

import (
	"context"
	"database/sql"
	"testing"
)

func TestOpenSQLiteInMemoryRunsMigrationForIsolatedDatabases(t *testing.T) {
	ctx := context.Background()
	migrations := 0
	migrate := func(ctx context.Context, db *sql.DB) error {
		migrations++
		_, err := db.ExecContext(ctx, `CREATE TABLE items (id TEXT PRIMARY KEY)`)
		return err
	}

	first, err := OpenSQLiteInMemory(ctx, migrate)
	if err != nil {
		t.Fatalf("open first in-memory db: %v", err)
	}
	defer first.Close()
	second, err := OpenSQLiteInMemory(ctx, migrate)
	if err != nil {
		t.Fatalf("open second in-memory db: %v", err)
	}
	defer second.Close()

	if migrations != 2 {
		t.Fatalf("migrations=%d want 2, one per in-memory db", migrations)
	}

	if _, err := first.ExecContext(ctx, `INSERT INTO items (id) VALUES ('first-only')`); err != nil {
		t.Fatalf("insert into first db: %v", err)
	}

	var firstCount int
	if err := first.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&firstCount); err != nil {
		t.Fatalf("count first db: %v", err)
	}
	if firstCount != 1 {
		t.Fatalf("first db count=%d want 1", firstCount)
	}

	var secondCount int
	if err := second.QueryRowContext(ctx, `SELECT COUNT(*) FROM items`).Scan(&secondCount); err != nil {
		t.Fatalf("count second db: %v", err)
	}
	if secondCount != 0 {
		t.Fatalf("second db count=%d want isolated empty database", secondCount)
	}
}
