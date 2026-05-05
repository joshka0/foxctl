package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestOpenWithConfigTursoHonorsConfiguredVectorDimensions(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "")

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
