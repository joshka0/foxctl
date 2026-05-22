package turso

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/storage/dbdriver"
)

// StoreOpenSpec describes the storage files and override prefix for a v2 store.
type StoreOpenSpec struct {
	Name             string
	EnvPrefix        string
	LocalFilename    string
	OverrideFilename string
}

// OpenStoreDB opens a Turso-first local store DB and applies its migration.
func OpenStoreDB(ctx context.Context, storageRoot string, spec StoreOpenSpec, migrate dbdriver.MigrationFunc) (*sql.DB, func() error, error) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = "v2 turso store"
	}
	if strings.TrimSpace(storageRoot) == "" {
		return nil, nil, fmt.Errorf("%s open: storageRoot is required", name)
	}
	localFilename := strings.TrimSpace(spec.LocalFilename)
	if localFilename == "" {
		return nil, nil, fmt.Errorf("%s open: local filename is required", name)
	}
	overrideFilename := strings.TrimSpace(spec.OverrideFilename)
	if overrideFilename == "" {
		return nil, nil, fmt.Errorf("%s open: override filename is required", name)
	}
	envPrefix := strings.TrimSpace(spec.EnvPrefix)
	if envPrefix == "" {
		return nil, nil, fmt.Errorf("%s open: env prefix is required", name)
	}

	cfg := dbdriver.DefaultTursoLocalConfig(filepath.Join(storageRoot, localFilename), true)
	if hasDriverOverride(envPrefix) {
		cfg = dbdriver.NewConfigLoader(storageRoot).LoadConfig(envPrefix, overrideFilename)
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, cfg, migrate)
	if err != nil {
		return nil, nil, fmt.Errorf("%s open: %w", name, err)
	}
	return db, closeFn, nil
}

func hasDriverOverride(envPrefix string) bool {
	return os.Getenv("FOXCTL_"+strings.ToUpper(envPrefix)+"_DB_DRIVER") != "" || os.Getenv("FOXCTL_DB_DRIVER") != ""
}
