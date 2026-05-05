package workers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// Open opens a Turso-first runtime worker registry.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, nil, fmt.Errorf("v2 workers open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultTursoLocalConfig(filepath.Join(storageRoot, "v2_workers.turso"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_WORKERS", "v2_workers.db")
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("v2 workers open: %w", err)
	}
	return NewStore(db), closeFn, nil
}

func hasDriverOverride() bool {
	return os.Getenv("FOXCTL_V2_WORKERS_DB_DRIVER") != "" || os.Getenv("FOXCTL_DB_DRIVER") != ""
}
