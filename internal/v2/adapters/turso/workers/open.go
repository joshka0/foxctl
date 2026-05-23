package workers

import (
	"context"

	tursoadapter "github.com/joshka0/foxctl/internal/v2/adapters/turso"
)

var openSpec = tursoadapter.StoreOpenSpec{
	Name:             "v2 workers",
	EnvPrefix:        "V2_WORKERS",
	LocalFilename:    "v2_workers.turso",
	OverrideFilename: "v2_workers.db",
}

// Open opens a Turso-first runtime worker registry.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	db, closeFn, err := tursoadapter.OpenStoreDB(ctx, storageRoot, openSpec, MigrateSchema)
	if err != nil {
		return nil, nil, err
	}
	return NewStore(db), closeFn, nil
}
