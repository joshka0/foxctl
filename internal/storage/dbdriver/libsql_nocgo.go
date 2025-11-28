//go:build !cgo || race

package dbdriver

import (
	"context"
	"fmt"
)

// openLibSQL is a stub used when building without cgo.
// libsql requires cgo, so this variant always returns an error.
func openLibSQL(_ context.Context, _ LibSQLConfig, _ MigrationFunc) (DB, error) {
	return nil, fmt.Errorf("libsql driver requires cgo (build with CGO_ENABLED=1)")
}
