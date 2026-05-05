//go:build cgo && !race

package memory

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestOpenWithConfigLibSQLHonorsConfiguredVectorDimensions(t *testing.T) {
	t.Setenv("FOXCTL_MEMORY_DB_DRIVER", "")

	cfg := config.Config{}
	cfg.Storage.Root = t.TempDir()
	cfg.Paths.CAS = filepath.Join(cfg.Storage.Root, "cas")
	cfg.Database.Driver = "libsql"
	cfg.Database.Vector.Dimensions = 4

	store, err := OpenWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("OpenWithConfig() error = %v", err)
	}
	defer func() { _ = store.Close() }()

	libSQLStore, ok := store.(*TursoStore)
	if !ok {
		t.Fatalf("OpenWithConfig() store type = %T, want *TursoStore", store)
	}
	if libSQLStore.vectorDimension != 4 {
		t.Fatalf("vectorDimension = %d, want 4", libSQLStore.vectorDimension)
	}
}
