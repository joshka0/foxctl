package workspacerepair

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// WorkspaceColumn identifies a table column that stores workspace keys.
type WorkspaceColumn struct {
	Table  string
	Column string
}

// AnyPathWorkspace reports whether any workspace column has a path-like value.
func AnyPathWorkspace(ctx context.Context, db *sql.DB, columns ...WorkspaceColumn) bool {
	for _, column := range columns {
		if TableHasPathWorkspace(ctx, db, column) {
			return true
		}
	}
	return false
}

// TableHasPathWorkspace reports whether a workspace column has a path-like value.
func TableHasPathWorkspace(ctx context.Context, db *sql.DB, column WorkspaceColumn) bool {
	table, name, ok := column.normalized()
	if !ok {
		return false
	}

	var one int
	// Identifiers are schema constants validated above; values stay parameterized.
	query := fmt.Sprintf("SELECT 1 FROM %s WHERE %s LIKE ? OR %s LIKE ? LIMIT 1", table, name, name)
	err := db.QueryRowContext(ctx, query, "%/%", "~%").Scan(&one)
	return err == nil
}

// CollectPathWorkspaces returns distinct path-like workspace values from columns.
func CollectPathWorkspaces(ctx context.Context, db *sql.DB, columns ...WorkspaceColumn) map[string]struct{} {
	out := make(map[string]struct{})
	for _, column := range columns {
		collectPathWorkspaces(ctx, db, column, out)
	}
	return out
}

func collectPathWorkspaces(ctx context.Context, db *sql.DB, column WorkspaceColumn, out map[string]struct{}) {
	table, name, ok := column.normalized()
	if !ok {
		return
	}

	// Identifiers are schema constants validated above; values stay parameterized.
	query := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s LIKE ? OR %s LIKE ?", name, table, name, name)
	rows, err := db.QueryContext(ctx, query, "%/%", "~%")
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var workspace string
		if err := rows.Scan(&workspace); err != nil {
			continue
		}
		workspace = strings.TrimSpace(workspace)
		if workspace == "" {
			continue
		}
		out[workspace] = struct{}{}
	}
}

func (c WorkspaceColumn) normalized() (string, string, bool) {
	table := strings.TrimSpace(c.Table)
	column := strings.TrimSpace(c.Column)
	if !isSQLIdentifier(table) || !isSQLIdentifier(column) {
		return "", "", false
	}
	return table, column, true
}

func isSQLIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		isLetter := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		isDigit := ch >= '0' && ch <= '9'
		if !(isLetter || ch == '_' || (i > 0 && isDigit)) {
			return false
		}
	}
	return true
}
