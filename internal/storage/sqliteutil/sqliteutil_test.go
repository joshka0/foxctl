package sqliteutil

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenDBCreatesDirectories(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	dbPath := filepath.Join(base, "nested", "store.db")
	var migrated bool
	db, err := OpenDB(ctx, dbPath, func(_ context.Context, _ *sql.DB) error {
		migrated = true
		return nil
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})

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
	_, err := OpenDB(ctx, dbPath, func(_ context.Context, _ *sql.DB) error {
		return sentinel
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
}

func TestOpenDBAcceptsRelativeFilePath(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	t.Chdir(workspace)

	db, err := OpenDB(ctx, filepath.Join(".foxctl", "runtime", "contextplane.db"), nil)
	if err != nil {
		t.Fatalf("open db with relative path: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
}

func TestRetryBusyUntilWaitsForBusyErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	attempts := 0
	start := time.Now()
	err := retryBusyUntil(ctx, 250*time.Millisecond, 10*time.Millisecond, func() error {
		attempts++
		if attempts < 4 {
			return errors.New("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retryBusyUntil() error = %v", err)
	}
	if attempts != 4 {
		t.Fatalf("attempts=%d want 4", attempts)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Fatalf("expected retries to wait, elapsed=%v", time.Since(start))
	}
}

func TestRetryBusyUntilReturnsLastBusyErrorAfterBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	want := errors.New("SQLITE_BUSY")
	err := retryBusyUntil(ctx, 30*time.Millisecond, 10*time.Millisecond, func() error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("retryBusyUntil() error = %v want %v", err, want)
	}
}
