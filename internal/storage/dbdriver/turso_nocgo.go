//go:build !cgo

package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
)

// openTurso is a stub used when building without cgo.
// The Turso driver depends on libsql, which requires cgo.
func openTurso(ctx context.Context, cfg TursoConfig, migrate MigrationFunc) (DB, error) {
	return nil, fmt.Errorf("turso driver requires cgo (build with CGO_ENABLED=1)")
}

// ensureVectorSupport is a stub used when building without cgo.
// Vector search is not available without a cgo-enabled libsql build.
func ensureVectorSupport(ctx context.Context, db *sql.DB, dimensions int) error {
	return fmt.Errorf("vector search requires cgo-enabled libsql/turso build")
}
