package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sqliteQueryTimeout = 5 * time.Second
	maxRowLimit        = 1000
	defaultRowLimit    = 50
)

// knownDatabases maps database filenames to friendly names
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

// SQLiteDBInfo holds information about a discovered SQLite database
type SQLiteDBInfo struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	FriendlyName string `json:"friendly_name"`
	Size         int64  `json:"size"`
	TableCount   int    `json:"table_count"`
}

// SQLiteTableInfo holds information about a table in a database
type SQLiteTableInfo struct {
	Name     string `json:"name"`
	RowCount int64  `json:"row_count"`
}

// SQLiteRowData represents a row of data from a table
type SQLiteRowData map[string]any

// discoverDatabases finds all SQLite databases in ~/.agentctl
// It prefers storage/ over cache/ when there are duplicate names
func discoverDatabases() ([]SQLiteDBInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot get home directory: %w", err)
	}

	agentctlDir := filepath.Join(home, ".agentctl")

	// Search in order of priority (storage first, then cache, then root)
	// Later entries override earlier ones for the same db name
	searchDirs := []string{
		agentctlDir, // root level (lowest priority)
		filepath.Join(agentctlDir, "cache"),
		filepath.Join(agentctlDir, "storage"), // highest priority
	}

	// Map by database name to deduplicate
	dbByName := make(map[string]SQLiteDBInfo)

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // Skip if directory doesn't exist
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".db") {
				continue
			}

			fullPath := filepath.Join(dir, name)
			dbName := strings.TrimSuffix(name, ".db")

			info, err := entry.Info()
			if err != nil {
				continue
			}

			friendlyName := dbName
			if fn, ok := knownDatabases[name]; ok {
				friendlyName = fn
			}

			// Overwrite earlier entries (storage takes priority)
			dbByName[dbName] = SQLiteDBInfo{
				Path:         fullPath,
				Name:         dbName,
				FriendlyName: friendlyName,
				Size:         info.Size(),
				TableCount:   -1, // Lazy load
			}
		}
	}

	// Convert map to slice
	var databases []SQLiteDBInfo
	for _, db := range dbByName {
		databases = append(databases, db)
	}

	// Sort by friendly name
	sort.Slice(databases, func(i, j int) bool {
		return databases[i].FriendlyName < databases[j].FriendlyName
	})

	return databases, nil
}

// validateDBPath ensures the database path is under ~/.agentctl for security
func validateDBPath(dbPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot get home directory: %w", err)
	}

	agentctlDir := filepath.Join(home, ".agentctl")

	// Resolve both paths to their canonical absolute forms
	canonicalPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("cannot resolve database path: %w", err)
	}
	canonicalPath, err = filepath.EvalSymlinks(canonicalPath)
	if err != nil {
		return fmt.Errorf("cannot resolve database path: %w", err)
	}

	canonicalRoot, err := filepath.Abs(agentctlDir)
	if err != nil {
		return fmt.Errorf("cannot resolve agentctl directory: %w", err)
	}
	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		// If the agentctl dir doesn't exist or can't be resolved, use cleaned path
		canonicalRoot = filepath.Clean(agentctlDir)
	}

	// Use filepath.Rel for proper containment checking (symlink-safe)
	rel, err := filepath.Rel(canonicalRoot, canonicalPath)
	if err != nil {
		return fmt.Errorf("database path must be under ~/.agentctl")
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("database path must be under ~/.agentctl")
	}

	return nil
}

// openDatabase opens a SQLite database in read-only mode with timeout
func openDatabase(dbPath string) (*sql.DB, error) {
	if err := validateDBPath(dbPath); err != nil {
		return nil, err
	}

	// Open in read-only mode with timeout
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool limits
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Second)

	return db, nil
}

// listTables returns all tables in a database (without row counts for speed)
func listTables(dbPath string) ([]SQLiteTableInfo, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Query sqlite_master for tables
	rows, err := db.Query(`
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	var tables []SQLiteTableInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}

		// Use approximate row count from sqlite_stat1 if available,
		// otherwise use a quick estimate based on page count
		var count int64 = -1

		// Try quick MAX(rowid) estimate first (instant for most tables)
		err := db.QueryRow(fmt.Sprintf("SELECT MAX(rowid) FROM %q", name)).Scan(&count)
		if err != nil {
			count = 0 // Assume empty or no rowid
		}

		tables = append(tables, SQLiteTableInfo{
			Name:     name,
			RowCount: count,
		})
	}

	return tables, nil
}

// fetchTableRows retrieves rows from a table with limit
func fetchTableRows(dbPath, tableName string, limit int) ([]string, []SQLiteRowData, error) {
	if limit <= 0 {
		limit = defaultRowLimit
	}
	if limit > maxRowLimit {
		limit = maxRowLimit
	}

	db, err := openDatabase(dbPath)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	// Get column names first
	query := fmt.Sprintf("SELECT * FROM %q LIMIT 1", tableName)
	sampleRows, err := db.Query(query)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query table: %w", err)
	}

	columns, err := sampleRows.Columns()
	_ = sampleRows.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get columns: %w", err)
	}

	// Fetch actual data
	dataQuery := fmt.Sprintf("SELECT * FROM %q LIMIT %d", tableName, limit)
	rows, err := db.Query(dataQuery)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch rows: %w", err)
	}
	defer rows.Close()

	var result []SQLiteRowData
	for rows.Next() {
		// Create slice of interface{} pointers
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		row := make(SQLiteRowData)
		for i, col := range columns {
			row[col] = values[i]
		}
		result = append(result, row)
	}

	return columns, result, nil
}

// getTableSchema returns the CREATE TABLE statement for a table
func getTableSchema(dbPath, tableName string) (string, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	var schema string
	row := db.QueryRow(`
		SELECT sql FROM sqlite_master
		WHERE type='table' AND name=?
	`, tableName)

	if err := row.Scan(&schema); err != nil {
		return "", fmt.Errorf("failed to get schema: %w", err)
	}

	return schema, nil
}

// resolveDatabasePath looks up a database by name and returns its full path
func resolveDatabasePath(dbName string) (string, error) {
	databases, err := discoverDatabases()
	if err != nil {
		return "", err
	}

	for _, db := range databases {
		if db.Name == dbName || db.FriendlyName == dbName {
			return db.Path, nil
		}
	}

	return "", fmt.Errorf("database not found: %s", dbName)
}
