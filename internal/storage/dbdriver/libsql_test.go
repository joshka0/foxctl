//go:build cgo && !race

package dbdriver

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenLibSQL_LocalOnly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := LibSQLConfig{
		Path:               filepath.Join(t.TempDir(), "local.libsql"),
		EnableVectorSearch: false,
	}

	db, err := openLibSQL(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("openLibSQL(local-only) error = %v", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("db.Close() error = %v", closeErr)
		}
	}()

	if got := db.GetDriverType(); got != DriverLibSQL {
		t.Fatalf("GetDriverType() = %q, want %q", got, DriverLibSQL)
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS smoke (id INTEGER PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatalf("create table error = %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO smoke(id, value) VALUES (1, 'ok')`); err != nil {
		t.Fatalf("insert error = %v", err)
	}

	var value string
	if err := db.QueryRowContext(ctx, `SELECT value FROM smoke WHERE id = 1`).Scan(&value); err != nil {
		t.Fatalf("select error = %v", err)
	}
	if value != "ok" {
		t.Fatalf("value = %q, want %q", value, "ok")
	}

	syncer, ok := db.(Syncer)
	if !ok {
		t.Fatalf("expected Syncer implementation for libsql DB")
	}
	if syncer.IsSyncEnabled() {
		t.Fatalf("IsSyncEnabled() = true, want false in local-only mode")
	}
	if err := syncer.Sync(); err != nil {
		t.Fatalf("Sync() in local-only mode error = %v", err)
	}
}
