package dbdriver

import "fmt"

// Dialect abstracts SQL syntax differences between database engines.
// Stores use the dialect to generate portable DDL and queries.
type Dialect interface {
	// Name returns the dialect identifier ("sqlite" or "postgres").
	Name() string

	// Rebind rewrites "?" positional placeholders to the dialect's native format.
	// SQLite: pass-through (also accepts $N). PostgreSQL: ? → $1, $2, ...
	//
	// Prefer writing queries with $1..$N directly (works in both SQLite and PostgreSQL)
	// rather than relying on runtime rebinding. Rebind exists for transitional callsites.
	Rebind(query string) string

	// Now returns the SQL expression for the current timestamp.
	// SQLite: "datetime('now')"  PostgreSQL: "NOW()"
	Now() string

	// JSONExtract returns SQL for extracting a JSON text field from a column.
	// SQLite: json_extract(col, '$.key')   PostgreSQL: col->>'key'
	JSONExtract(column, key string) string

	// JSONType returns the preferred column type for JSON payloads.
	// SQLite: "TEXT"   PostgreSQL: "JSONB"
	JSONType() string

	// BlobType returns the column type for binary data.
	// SQLite: "BLOB"   PostgreSQL: "BYTEA"
	BlobType() string

	// VectorType returns the column type for vector embeddings of the given dimensions.
	// SQLite: "BLOB"   PostgreSQL: "vector(dims)"
	// Turso stores vectors as BLOB values created by vector32()/vector64().
	VectorType(dims int) string

	// AutoIncrement returns the column type for auto-incrementing primary keys.
	// SQLite: "INTEGER PRIMARY KEY AUTOINCREMENT"
	// PostgreSQL: "BIGSERIAL PRIMARY KEY"
	AutoIncrement() string

	// Placeholder returns the placeholder for the n-th parameter (1-indexed).
	// SQLite: "$1"   PostgreSQL: "$1" (both use the same syntax).
	Placeholder(n int) string

	// TableExists returns a query that checks if a table exists.
	// The query should return at least one row if the table exists.
	// Takes the table name as a string parameter.
	TableExists() string

	// ColumnExists returns a query that checks if a column exists on a table.
	// Takes (table_name, column_name) as string parameters.
	ColumnExists() string

	// CurrentTimestampValue returns the SQL value expression for NOW.
	// SQLite: CURRENT_TIMESTAMP   PostgreSQL: NOW()
	// For use in DEFAULT clauses and value expressions.
	CurrentTimestampValue() string

	// AddColumnIfNotExists returns SQL to add a column if it doesn't exist.
	// On PostgreSQL this uses ALTER TABLE ... ADD COLUMN IF NOT EXISTS.
	// On SQLite we must check + ALTER separately (caller handles error).
	// Returns empty string if the dialect doesn't support IF NOT EXISTS on ALTER TABLE.
	AddColumnIfNotExists(table, column, colType, defaultVal string) string
}

// DialectFor returns the appropriate Dialect for the given driver type.
func DialectFor(dt DriverType) Dialect {
	switch dt {
	case DriverPostgres:
		return PostgresDialect{}
	default:
		return SQLiteDialect{}
	}
}

// sanitizeJSONKey strips characters that could be used for SQL injection
// in JSON path expressions. Only allows alphanumeric, underscore, hyphen, and dot.
func sanitizeJSONKey(key string) string {
	result := make([]byte, 0, len(key))
	for i := 0; i < len(key); i++ {
		c := key[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
			result = append(result, c)
		}
	}
	return string(result)
}

// rebindToPositional rewrites ?-style placeholders to $1, $2, ... style.
// It correctly handles quoted strings and escaped characters.
func rebindToPositional(query string) string {
	n := 0
	result := make([]byte, 0, len(query)+16)
	for i := 0; i < len(query); i++ {
		ch := query[i]
		switch ch {
		case '?':
			n++
			result = append(result, '$')
			result = append(result, []byte(fmt.Sprintf("%d", n))...)
		case '\'':
			// Skip single-quoted string literals (preserves unterminated quotes safely)
			result = append(result, ch)
			closed := false
			for i++; i < len(query); i++ {
				result = append(result, query[i])
				if query[i] == '\'' {
					if i+1 < len(query) && query[i+1] == '\'' {
						// Escaped quote
						i++
						result = append(result, query[i])
					} else {
						closed = true
						break
					}
				}
			}
			if !closed {
				// Unterminated quote: remaining bytes already appended, stop processing
				return string(result)
			}
		case '"':
			// Skip double-quoted identifiers (preserves unterminated quotes safely)
			result = append(result, ch)
			closed := false
			for i++; i < len(query); i++ {
				result = append(result, query[i])
				if query[i] == '"' {
					closed = true
					break
				}
			}
			if !closed {
				return string(result)
			}
		default:
			result = append(result, ch)
		}
	}
	return string(result)
}
