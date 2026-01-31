package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/platform/config"

	_ "modernc.org/sqlite"
)

// validIdentifier matches valid SQLite identifiers (alphanumeric + underscore).
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// isValidIdentifier checks if a string is a valid SQLite identifier.
func isValidIdentifier(s string) bool {
	return validIdentifier.MatchString(s) && len(s) <= 128
}

// quoteIdentifier safely quotes an identifier for use in SQL.
// quoteIdentifier wraps s in double quotes and doubles any internal double quotes so it can be used as a SQL identifier.
// Callers must validate s with isValidIdentifier before calling.
func quoteIdentifier(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

const sqliteBusyTimeoutMs = 5000

// openSQLiteReadOnly opens a read-only SQLite database at dbPath.
// It converts Windows paths to use forward slashes, builds a file:// URL with mode=ro,
// sets the busy timeout to 5000ms, and enables the foreign_keys pragma before opening the connection.
func openSQLiteReadOnly(dbPath string) (*sql.DB, error) {
	// Convert to forward slashes for URL (required for Windows paths like C:\)
	urlPath := filepath.ToSlash(dbPath)
	u := url.URL{Scheme: "file", Path: urlPath}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_busy_timeout", strconv.Itoa(sqliteBusyTimeoutMs))
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	return sql.Open("sqlite", u.String())
}

// SQLiteDatabaseResponse represents a database in API responses.
type SQLiteDatabaseResponse struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// SQLiteTableResponse represents a table in API responses.
type SQLiteTableResponse struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	RowCount int64  `json:"row_count"`
}

// SQLiteColumnResponse represents a column schema.
type SQLiteColumnResponse struct {
	CID        int    `json:"cid"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	NotNull    bool   `json:"not_null"`
	DefaultVal any    `json:"default_value"`
	PK         bool   `json:"pk"`
}

// SQLiteHandler returns a handler for /api/sqlite endpoints.
func SQLiteHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Route based on path: /api/sqlite, /api/sqlite/{db}, /api/sqlite/{db}/{table}
		path := strings.TrimPrefix(r.URL.Path, "/api/sqlite")
		path = strings.TrimPrefix(path, "/")
		parts := strings.Split(path, "/")

		switch {
		case path == "" || path == "/":
			// List databases
			handleListDatabases(w, r, cfg, log)
		case len(parts) == 1:
			// List tables in database
			handleListTables(w, r, cfg, log, parts[0])
		case len(parts) == 2 && parts[1] == "indexes":
			// List indexes
			handleListIndexes(w, r, cfg, log, parts[0])
		case len(parts) == 2 && parts[1] == "query":
			// Execute query
			handleQuery(w, r, cfg, log, parts[0])
		case len(parts) == 2:
			// Get table data
			handleTableData(w, r, cfg, log, parts[0], parts[1])
		case len(parts) == 3 && parts[2] == "schema":
			// Get table schema
			handleTableSchema(w, r, cfg, log, parts[0], parts[1])
		default:
			httpError(w, http.StatusNotFound, "not found")
		}
	}
}

func handleListDatabases(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// List .db files in storage root
	var databases []SQLiteDatabaseResponse
	dbFiles := []string{
		"jobs.db", "tasks.db", "sessions.db", "memory.db",
		"agents.db", "mailbox.db", "blackboard.db", "trajectory.db",
	}

	for _, name := range dbFiles {
		dbPath := filepath.Join(cfg.Storage.Root, name)
		info, err := os.Stat(dbPath)
		if err == nil && !info.IsDir() {
			databases = append(databases, SQLiteDatabaseResponse{
				Name: strings.TrimSuffix(name, ".db"),
				Path: dbPath,
				Size: info.Size(),
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"databases": databases,
	})
}

// handleListTables writes a JSON list of tables and views for the named SQLite database.
// It requires an HTTP GET request and a valid database identifier. The handler opens the
// database in read-only mode, enumerates user tables and views (excluding internal
// sqlite_ entries), attempts to include a row count for each table when available, and
// responds with HTTP 200 and a JSON payload of the form {"tables": [...]}. On invalid
// input or internal errors it writes the corresponding HTTP error response.
func handleListTables(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, dbName string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isValidIdentifier(dbName) {
		httpError(w, http.StatusBadRequest, "invalid database name")
		return
	}

	dbPath := filepath.Join(cfg.Storage.Root, dbName+".db")
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		log.Error().Err(err).Str("db", dbName).Msg("failed to open database")
		httpError(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	defer db.Close()

	// Query sqlite_master for tables
	rows, err := db.Query(`
		SELECT name, type FROM sqlite_master
		WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		log.Error().Err(err).Str("db", dbName).Msg("failed to query tables")
		httpError(w, http.StatusInternalServerError, "failed to query tables")
		return
	}
	defer rows.Close()

	var tables []SQLiteTableResponse
	for rows.Next() {
		var t SQLiteTableResponse
		if err := rows.Scan(&t.Name, &t.Type); err != nil {
			continue
		}
		// Get row count using quoted identifier to prevent SQL injection
		var count int64
		err := db.QueryRow("SELECT COUNT(*) FROM " + quoteIdentifier(t.Name)).Scan(&count)
		if err == nil {
			t.RowCount = count
		}
		tables = append(tables, t)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tables": tables,
	})
}

// handleTableData serves GET requests that return rows and metadata for a table
// in a read-only SQLite database.
//
// It validates the HTTP method and that dbName and tableName are valid identifiers,
// accepts optional query parameters `limit` (default 100, max 1000) and `offset`
// (default 0), and responds with JSON containing `columns`, `rows`, `total_count`,
// `limit`, and `offset`. On error it writes an appropriate HTTP error status.
func handleTableData(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, dbName, tableName string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isValidIdentifier(dbName) {
		httpError(w, http.StatusBadRequest, "invalid database name")
		return
	}
	if !isValidIdentifier(tableName) {
		httpError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			offset = n
		}
	}

	dbPath := filepath.Join(cfg.Storage.Root, dbName+".db")
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		log.Error().Err(err).Str("db", dbName).Msg("failed to open database")
		httpError(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	defer db.Close()

	// Get total count using quoted identifier
	quotedTable := quoteIdentifier(tableName)
	var totalCount int64
	err = db.QueryRow("SELECT COUNT(*) FROM " + quotedTable).Scan(&totalCount)
	if err != nil {
		log.Error().Err(err).Str("table", tableName).Msg("failed to count rows")
		httpError(w, http.StatusInternalServerError, "failed to count rows")
		return
	}

	// Get rows using quoted identifier
	query := "SELECT * FROM " + quotedTable + " LIMIT ? OFFSET ?"
	rows, err := db.Query(query, limit, offset)
	if err != nil {
		log.Error().Err(err).Str("table", tableName).Msg("failed to query table")
		httpError(w, http.StatusInternalServerError, "failed to query table")
		return
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to get columns")
		return
	}

	// Scan rows
	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for JSON
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"columns":     columns,
		"rows":        results,
		"total_count": totalCount,
		"limit":       limit,
		"offset":      offset,
	})
}

// handleTableSchema returns the CREATE statement and column metadata for a table in the specified SQLite database.
// It validates the database and table names, opens the database read-only, retrieves the table's SQL from sqlite_master,
// and collects column information via PRAGMA table_info. The handler writes a JSON response with "schema" (string)
// and "columns" (array of objects with cid, name, type, not_null, default_value, pk). It responds with 405 for non-GET,
// 400 for invalid identifiers, 404 if the table is not found, and 500 on internal errors.
func handleTableSchema(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, dbName, tableName string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isValidIdentifier(dbName) {
		httpError(w, http.StatusBadRequest, "invalid database name")
		return
	}
	if !isValidIdentifier(tableName) {
		httpError(w, http.StatusBadRequest, "invalid table name")
		return
	}

	dbPath := filepath.Join(cfg.Storage.Root, dbName+".db")
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	defer db.Close()

	// Get schema SQL
	var schema string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE name = ?", tableName).Scan(&schema)
	if err != nil {
		httpError(w, http.StatusNotFound, "table not found")
		return
	}

	// Get column info using quoted identifier
	rows, err := db.Query("PRAGMA table_info(" + quoteIdentifier(tableName) + ")")
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to get table info")
		return
	}
	defer rows.Close()

	var columns []SQLiteColumnResponse
	for rows.Next() {
		var c SQLiteColumnResponse
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&c.CID, &c.Name, &c.Type, &notNull, &dflt, &pk); err != nil {
			continue
		}
		c.NotNull = notNull == 1
		c.PK = pk == 1
		if dflt.Valid {
			c.DefaultVal = dflt.String
		}
		columns = append(columns, c)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"schema":  schema,
		"columns": columns,
	})
}

// handleListIndexes returns a JSON listing of indexes defined in the specified SQLite database.
// It validates dbName, accepts an optional "table" query parameter to restrict results to a single table,
// opens the database read-only, and responds with an "indexes" array where each entry contains
// "name", "table_name", and "sql". Request validation failures or database errors are reported via HTTP error responses.
func handleListIndexes(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, dbName string) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isValidIdentifier(dbName) {
		httpError(w, http.StatusBadRequest, "invalid database name")
		return
	}

	table := r.URL.Query().Get("table")

	dbPath := filepath.Join(cfg.Storage.Root, dbName+".db")
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	defer db.Close()

	query := `SELECT name, tbl_name, sql FROM sqlite_master WHERE type = 'index' AND sql IS NOT NULL`
	if table != "" {
		query += " AND tbl_name = ?"
	}
	query += " ORDER BY name"

	var rows *sql.Rows
	if table != "" {
		rows, err = db.Query(query, table)
	} else {
		rows, err = db.Query(query)
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to query indexes")
		return
	}
	defer rows.Close()

	var indexes []map[string]any
	for rows.Next() {
		var name, tblName, sqlStr string
		if err := rows.Scan(&name, &tblName, &sqlStr); err != nil {
			continue
		}
		indexes = append(indexes, map[string]any{
			"name":       name,
			"table_name": tblName,
			"sql":        sqlStr,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"indexes": indexes,
	})
}

// handleQuery executes a read-only SQL query against the named SQLite database and returns the results as JSON.
// It accepts POST requests with a JSON body containing `query` (string) and optional `limit` (int).
// The handler validates `dbName`, allows only queries beginning with `SELECT` or `PRAGMA`, enforces a default limit of 100 and a maximum of 1000, and appends a LIMIT clause if one is not present.
// On success it responds with a JSON object containing `columns`, `rows`, and `rows_affected`. If query execution fails it responds with a JSON object containing `error` and empty `columns`/`rows`.
func handleQuery(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, dbName string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if !isValidIdentifier(dbName) {
		httpError(w, http.StatusBadRequest, "invalid database name")
		return
	}

	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Query == "" {
		httpError(w, http.StatusBadRequest, "query required")
		return
	}

	limit := 100
	if req.Limit > 0 && req.Limit <= 1000 {
		limit = req.Limit
	}

	// Basic SQL injection prevention - only allow SELECT
	queryUpper := strings.ToUpper(strings.TrimSpace(req.Query))
	if !strings.HasPrefix(queryUpper, "SELECT") && !strings.HasPrefix(queryUpper, "PRAGMA") {
		httpError(w, http.StatusBadRequest, "only SELECT and PRAGMA queries allowed")
		return
	}

	dbPath := filepath.Join(cfg.Storage.Root, dbName+".db")
	db, err := openSQLiteReadOnly(dbPath)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to open database")
		return
	}
	defer db.Close()

	// Add LIMIT if not present
	if !strings.Contains(queryUpper, "LIMIT") {
		req.Query += " LIMIT " + strconv.Itoa(limit)
	}

	rows, err := db.Query(req.Query)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"error":   err.Error(),
			"columns": []string{},
			"rows":    []any{},
		})
		return
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to get columns")
		return
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		row := make(map[string]any)
		for i, col := range columns {
			val := values[i]
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"columns":       columns,
		"rows":          results,
		"rows_affected": len(results),
	})
}