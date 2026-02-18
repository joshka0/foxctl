package idmap

import (
	"context"
	"database/sql"
	"fmt"
)

// MigrateSchema creates v1<->v2 id mapping tables.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("v2 idmap migrate: nil db")
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS v2_id_map (
			entity_type TEXT NOT NULL,
			legacy_id TEXT NOT NULL,
			v2_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(entity_type, legacy_id),
			UNIQUE(entity_type, v2_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_v2_id_map_v2 ON v2_id_map(entity_type, v2_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("v2 idmap migrate: %w", err)
		}
	}
	return nil
}
