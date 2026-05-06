package projections

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// Open opens a Turso-first v2 projection store.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, nil, fmt.Errorf("v2 projections open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultTursoLocalConfig(filepath.Join(storageRoot, "v2_projections.turso"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_PROJECTIONS", "v2_projections.db")
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("v2 projections open: %w", err)
	}
	return NewStore(db), closeFn, nil
}

func hasDriverOverride() bool {
	return os.Getenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER") != "" || os.Getenv("FOXCTL_DB_DRIVER") != ""
}
