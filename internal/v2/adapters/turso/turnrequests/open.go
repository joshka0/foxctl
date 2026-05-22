package turnrequests

import (
	"context"

	tursoadapter "github.com/joshka0/foxctl/internal/v2/adapters/turso"
)

var openSpec = tursoadapter.StoreOpenSpec{
	Name:             "v2 turn requests",
	EnvPrefix:        "V2_TURN_REQUESTS",
	LocalFilename:    "v2_turn_requests.turso",
	OverrideFilename: "v2_turn_requests.db",
}

// Open opens a Turso-first v2 turn request registry.
func Open(ctx context.Context, storageRoot string) (*Store, func() error, error) {
	db, closeFn, err := tursoadapter.OpenStoreDB(ctx, storageRoot, openSpec, MigrateSchema)
	if err != nil {
		return nil, nil, err
	}
	return NewStore(db), closeFn, nil
}
