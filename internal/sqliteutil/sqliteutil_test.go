package sqliteutil_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/sqliteutil"
)

func TestOpenDBCreatesDirectories(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dbPath := filepath.Join(base, "nested", "store.db")
	var migrated bool
	db, err := sqliteutil.OpenDB(ctx, dbPath, func(_ context.Context, _ *sql.DB) error {
		migrated = true
		return nil
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if !migrated {
		t.Fatalf("expected migration to be invoked")
	}
	if _, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("expected parent directory to exist: %v", err)
	}
}

func TestOpenDBMigrationError(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dbPath := filepath.Join(base, "fail.db")
	sentinel := errors.New("boom")
	_, err := sqliteutil.OpenDB(ctx, dbPath, func(_ context.Context, _ *sql.DB) error {
		return sentinel
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}
