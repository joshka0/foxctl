package workers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// Open opens a libsql-first runtime worker registry with sqlite fallback.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	if strings.TrimSpace(storageRoot) == "" {
		return nil, nil, fmt.Errorf("v2 workers open: storageRoot is required")
	}

	defaultCfg := dbdriver.DefaultLibSQLConfig(filepath.Join(storageRoot, "v2_workers.libsql"), true)
	cfg := defaultCfg
	if hasDriverOverride() {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig("V2_WORKERS", "v2_workers.db")
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, MigrateSchema)
	if err != nil && cfg.Driver == dbdriver.DriverLibSQL {
		fallback := dbdriver.DefaultSQLiteConfig(filepath.Join(storageRoot, "v2_workers.db"))
		db, closeFn, err = dbdriver.OpenDBCompatWithCloser(ctx, fallback, MigrateSchema)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("v2 workers open: %w", err)
	}
	return NewStore(db), closeFn, nil
}

func hasDriverOverride() bool {
	return os.Getenv("AGENTCTL_V2_WORKERS_DB_DRIVER") != "" || os.Getenv("AGENTCTL_DB_DRIVER") != ""
}
