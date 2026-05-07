package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

func TestOpenWithConfigTursoHonorsConfiguredVectorDimensions(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "")
	t.Setenv("FOXCTL_MEMORY_DB_PATH", "")
	t.Setenv("FOXCTL_MEMORY_VECTOR_DIMS", "")

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")
	cfg.Database.Driver = "turso"
	cfg.Database.Vector.Dimensions = 4

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	tursoStore, ok := store.(*TursoStore)
	if !ok {
		t.Fatalf("OpenWithConfig() store type = %T, want *TursoStore", store)
	}
	if tursoStore.vectorDimension != 4 {
		t.Fatalf("vectorDimension = %d, want 4", tursoStore.vectorDimension)
	}
}

func TestOpenWithConfigTursoHonorsMemoryEnvPathAndDimensions(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "turso")
	t.Setenv("FOXCTL_MEMORY_VECTOR_DIMS", "7")
	customPath := filepath.Join(t.TempDir(), "custom-memory.turso")
	t.Setenv("FOXCTL_MEMORY_DB_PATH", customPath)

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")
	cfg.Database.Driver = "sqlite"
	cfg.Database.Vector.Dimensions = 4

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	tursoStore, ok := store.(*TursoStore)
	if !ok {
		t.Fatalf("OpenWithConfig() store type = %T, want *TursoStore", store)
	}
	if tursoStore.vectorDimension != 7 {
		t.Fatalf("vectorDimension = %d, want 7", tursoStore.vectorDimension)
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("expected custom Turso path %q to exist: %v", customPath, err)
	}
}

func TestOpenWithConfigTursoMemoryURLEnvCanDisableGlobalURL(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "turso")
	t.Setenv("FOXCTL_MEMORY_DB_URL", "")
	t.Setenv("FOXCTL_MEMORY_DB_TOKEN", "")
	customPath := filepath.Join(t.TempDir(), "local-memory.turso")
	t.Setenv("FOXCTL_MEMORY_DB_PATH", customPath)

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")
	cfg.Database.Driver = "turso"
	cfg.Database.Turso.URL = "libsql://example.invalid"
	cfg.Database.Turso.AuthToken = "unused"
	cfg.Database.Vector.Dimensions = 4

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	tursoStore, ok := store.(*TursoStore)
	if !ok {
		t.Fatalf("OpenWithConfig() store type = %T, want *TursoStore", store)
	}
	syncer, ok := tursoStore.db.(dbdriver.Syncer)
	if !ok {
		t.Fatalf("Turso DB does not expose sync state")
	}
	if syncer.IsSyncEnabled() {
		t.Fatal("sync enabled = true, want local-only store")
	}
	if syncer.GetSyncURL() != "" {
		t.Fatalf("sync URL = %q, want empty", syncer.GetSyncURL())
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("expected local Turso path %q to exist: %v", customPath, err)
	}
}

func TestOpenWithConfigUnknownDriverErrors(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "")

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")
	cfg.Database.Driver = "unknown-driver"

	store, err := OpenWithConfig(context.Background(), cfg)
	if err == nil {
		if store != nil {
			_ = store.Close()
		}
		t.Fatal("OpenWithConfig() error = nil, want unsupported driver error")
	}
	if !strings.Contains(err.Error(), "unsupported database driver") {
		t.Fatalf("OpenWithConfig() error = %v, want unsupported driver error", err)
	}
}
