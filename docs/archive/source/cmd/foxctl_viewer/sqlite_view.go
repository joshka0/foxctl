//go:build archived

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

// discoverDatabases finds all SQLite databases in ~/.foxctl
func discoverDatabases() ([]sqliteDBInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot get home directory: %w", err)
	}

	foxctlDir := filepath.Join(home, ".foxctl")
	var databases []sqliteDBInfo

	// Search in common locations
	searchDirs := []string{
		filepath.Join(foxctlDir, "storage"),
		filepath.Join(foxctlDir, "cache"),
		foxctlDir, // root level
	}

	seen := make(map[string]bool)

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
			if seen[fullPath] {
				continue
			}
			seen[fullPath] = true

			info, err := entry.Info()
			if err != nil {
				continue
			}

			databases = append(databases, sqliteDBInfo{
				Path:   fullPath,
				Name:   strings.TrimSuffix(name, ".db"),
				Size:   info.Size(),
				Tables: -1, // Lazy load
			})
		}
	}

	// Sort by name
	sort.Slice(databases, func(i, j int) bool {
		return databases[i].Name < databases[j].Name
	})

	return databases, nil
}

// validateDBPath ensures the database path is under ~/.foxctl for security
func validateDBPath(dbPath string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot get home directory: %w", err)
	}

	foxctlDir := filepath.Join(home, ".foxctl")

	// Resolve both paths to their canonical absolute forms
	canonicalPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("cannot resolve database path: %w", err)
	}
	canonicalPath, err = filepath.EvalSymlinks(canonicalPath)
	if err != nil {
		return fmt.Errorf("cannot resolve database path: %w", err)
	}

	canonicalRoot, err := filepath.Abs(foxctlDir)
	if err != nil {
		return fmt.Errorf("cannot resolve foxctl directory: %w", err)
	}
	canonicalRoot, err = filepath.EvalSymlinks(canonicalRoot)
	if err != nil {
		// If the foxctl dir doesn't exist or can't be resolved, use cleaned path
		canonicalRoot = filepath.Clean(foxctlDir)
	}

	// Use filepath.Rel for proper containment checking (symlink-safe)
	rel, err := filepath.Rel(canonicalRoot, canonicalPath)
	if err != nil {
		return fmt.Errorf("database path must be under ~/.foxctl")
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("database path must be under ~/.foxctl")
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

// listTables returns all tables in a database with row counts
func listTables(dbPath string) ([]sqliteTableInfo, error) {
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

	var tables []sqliteTableInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}

		// Get row count for each table
		var count int64
		countRow := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %q", name))
		if err := countRow.Scan(&count); err != nil {
			count = -1
		}

		tables = append(tables, sqliteTableInfo{
			Name:     name,
			RowCount: count,
		})
	}

	return tables, nil
}

// fetchTableRows retrieves rows from a table with limit
func fetchTableRows(dbPath, tableName string, limit int) ([]string, []sqliteRowData, error) {
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

	var result []sqliteRowData
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

		row := make(sqliteRowData)
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

// getTableCount returns the number of tables in a database
func getTableCount(dbPath string) (int, error) {
	db, err := openDatabase(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	var count int
	row := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
	`)

	if err := row.Scan(&count); err != nil {
		return 0, err
	}

	return count, nil
}

// formatBytes formats a byte size for display
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatCellValue formats a cell value for display
func formatCellValue(v any, maxWidth int) string {
	if v == nil {
		return "<null>"
	}

	var s string
	switch val := v.(type) {
	case []byte:
		s = string(val)
	case string:
		s = val
	case int64:
		s = fmt.Sprintf("%d", val)
	case float64:
		s = fmt.Sprintf("%.4f", val)
	case bool:
		if val {
			s = "true"
		} else {
			s = "false"
		}
	default:
		s = fmt.Sprintf("%v", val)
	}

	// Truncate if too long
	if len(s) > maxWidth {
		if maxWidth > 3 {
			return s[:maxWidth-3] + "..."
		}
		return s[:maxWidth]
	}

	return s
}
