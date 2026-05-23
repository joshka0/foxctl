package projections

import (
	"context"

	tursoadapter "github.com/joshka0/foxctl/internal/v2/adapters/turso"
)

var openSpec = tursoadapter.StoreOpenSpec{
	Name:             "v2 projections",
	EnvPrefix:        "V2_PROJECTIONS",
	LocalFilename:    "v2_projections.turso",
	OverrideFilename: "v2_projections.db",
}

// Open opens a Turso-first v2 projection store.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	db, closeFn, err := tursoadapter.OpenStoreDB(ctx, storageRoot, openSpec, MigrateSchema)
	if err != nil {
		return nil, nil, err
	}
	return NewStore(db), closeFn, nil
}
