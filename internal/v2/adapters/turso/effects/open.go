package effects

import (
	"context"

	tursoadapter "github.com/joshka0/foxctl/internal/v2/adapters/turso"
)

var openSpec = tursoadapter.StoreOpenSpec{
	Name:             "v2 effects",
	EnvPrefix:        "V2_EFFECTS",
	LocalFilename:    "v2_effects.turso",
	OverrideFilename: "v2_effects.db",
}

// Open opens a Turso-first v2 effect journal store.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	db, closeFn, err := tursoadapter.OpenStoreDB(ctx, storageRoot, openSpec, MigrateSchema)
	if err != nil {
		return nil, nil, err
	}
	return NewStore(db), closeFn, nil
}
