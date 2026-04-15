package projections

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// Open opens a libsql-first v2 projection store with sqlite fallback.
//
// Behavior:
// 1) default to local libsql file (`v2_projections.libsql`)
// 2) respect explicit env driver overrides
// 3) fallback to sqlite in environments without cgo/libsql support
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, nil, fmt.Errorf("v2 projections open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultLibSQLConfig(filepath.Join(storageRoot, "v2_projections.libsql"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_PROJECTIONS", "v2_projections.db")
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil && cfg.Driver == dbdriver.DriverLibSQL {
		fallback := dbdriver.DefaultSQLiteConfig(filepath.Join(storageRoot, "v2_projections.db"))
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, fallback, MigrateSchema)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("v2 projections open: %w", err)
	}
	return NewStore(db), closeFn, nil
}

func hasDriverOverride() bool {
	return os.Getenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER") != "" || os.Getenv("FOXCTL_DB_DRIVER") != ""
}
