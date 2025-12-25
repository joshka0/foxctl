package main

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

// knownDatabases maps database filenames to friendly names.
var knownDatabases = map[string]string{
	"tasks.db":           "Tasks",
	"agents.db":          "Agents",
	"jobs.db":            "Jobs",
	"blackboard.db":      "Blackboard",
	"mailbox.db":         "Mailbox",
	"memory.db":          "Memory",
	"knowledge.db":       "Knowledge",
	"trajectory.db":      "Trajectory",
	"cache.db":           "Cache",
	"test_watch.db":      "Test Watch",
	"embedding_queue.db": "Embeddings",
	"daemon_dedupe.db":   "Daemon Dedupe",
}

// getFriendlyName returns a friendly name for a database, or the filename if unknown.
func (db sqliteDBInfo) getFriendlyName() string {
	if name, ok := knownDatabases[db.Name+".db"]; ok {
		return name
	}
	return db.Name
}
