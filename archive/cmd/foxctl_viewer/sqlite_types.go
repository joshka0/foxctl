//go:build archived

package main

import (
	"strings"

	"github.com/joshka0/foxctl/internal/storage"
)

// sqlitePane represents which pane is active in the SQLite browser.
type sqlitePane int

const (
	paneDatabases sqlitePane = iota
	paneTables
	paneData
)

// sqliteDBInfo holds information about a discovered SQLite database.
type sqliteDBInfo struct {
	Path   string // full path to the database file
	Name   string // basename without .db extension
	Size   int64  // file size in bytes
	Tables int    // table count (lazy loaded, -1 if not loaded)
}

// sqliteTableInfo holds information about a table in a database.
type sqliteTableInfo struct {
	Name     string
	RowCount int64
}

// sqliteRowData represents a row of data from a table.
type sqliteRowData map[string]any

// knownDatabases maps canonical database filenames to friendly names.
// It is derived from the central store registry to avoid drift between viewer and runtime.
var knownDatabases = func() map[string]string {
	out := make(map[string]string)
	for _, spec := range storage.CanonicalStores() {
		if spec.Class == storage.StoreClassExternal || spec.Class == storage.StoreClassObservability {
			continue
		}

		// Skip dynamically named and nested DBs (e.g., repoindex/<key>.db).
		if strings.Contains(spec.DefaultFile, "<") || strings.Contains(spec.DefaultFile, "/") {
			continue
		}
		if !strings.HasSuffix(spec.DefaultFile, ".db") {
			continue
		}
		out[spec.DefaultFile] = friendlyStoreName(string(spec.Name))
	}
	return out
}()

// friendlyStoreName converts a canonical store name (e.g., "EMBEDDING_QUEUE") into a human-friendly label.
//
// Index:
// - Purpose: Keep viewer display names derived from the canonical store registry (avoid hand-maintained maps)
// - Flow: normalize → special-case acronyms → title-case underscore-separated tokens
// - SideEffects: none
// - FailureModes: none
// - Related: knownDatabases, storage.CanonicalStores
// - Keywords: sqlite_viewer, store_registry, display_name
func friendlyStoreName(store string) string {
	store = strings.TrimSpace(store)
	if store == "" {
		return store
	}
	if store == "CAS" {
		return "CAS"
	}

	parts := strings.Split(strings.ToLower(store), "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// getFriendlyName returns a friendly name for a database, or the filename if unknown.
func (db sqliteDBInfo) getFriendlyName() string {
	if name, ok := knownDatabases[db.Name+".db"]; ok {
		return name
	}
	return db.Name
}
