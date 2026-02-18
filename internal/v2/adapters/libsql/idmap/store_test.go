package idmap

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
)

func TestIdMap_WriteRead_RoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, closeFn, err := dbutil.OpenSQLiteDBShared(ctx, filepath.Join(t.TempDir(), "idmap.db"), nil)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	t.Cleanup(func() { _ = closeFn() })
	if err := MigrateSchema(ctx, db); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	store := NewStore(db)
	if err := store.Put(ctx, "run", "legacy-run-001", "run-v2-001"); err != nil {
		t.Fatalf("put mapping: %v", err)
	}
	// Same mapping should be idempotent.
	if err := store.Put(ctx, "run", "legacy-run-001", "run-v2-001"); err != nil {
		t.Fatalf("put same mapping: %v", err)
	}

	v2ID, err := store.ResolveV2ID(ctx, "run", "legacy-run-001")
	if err != nil {
		t.Fatalf("resolve v2 id: %v", err)
	}
	if v2ID != "run-v2-001" {
		t.Fatalf("v2_id=%q want run-v2-001", v2ID)
	}

	legacyID, err := store.ResolveLegacyID(ctx, "run", "run-v2-001")
	if err != nil {
		t.Fatalf("resolve legacy id: %v", err)
	}
	if legacyID != "legacy-run-001" {
		t.Fatalf("legacy_id=%q want legacy-run-001", legacyID)
	}

	err = store.Put(ctx, "run", "legacy-run-001", "run-v2-002")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict error=%v want ErrConflict", err)
	}

	_, err = store.ResolveV2ID(ctx, "run", "missing")
	if !errors.Is(err, v2events.ErrNotFound) {
		t.Fatalf("resolve missing error=%v want ErrNotFound", err)
	}
}
