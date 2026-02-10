package dbdriver

import "fmt"

// SQLiteDialect implements Dialect for SQLite and libSQL databases.
type SQLiteDialect struct{}

func (SQLiteDialect) Name() string { return "sqlite" }

// Rebind is a pass-through for SQLite — it accepts both ? and $N placeholders.
func (SQLiteDialect) Rebind(query string) string { return query }

func (SQLiteDialect) Now() string { return "datetime('now')" }

func (SQLiteDialect) JSONExtract(column, key string) string {
	return fmt.Sprintf("json_extract(%s, '$.%s')", column, sanitizeJSONKey(key))
}

func (SQLiteDialect) JSONType() string { return "TEXT" }

func (SQLiteDialect) BlobType() string { return "BLOB" }

func (SQLiteDialect) VectorType(dims int) string { return "BLOB" }

func (SQLiteDialect) AutoIncrement() string { return "INTEGER PRIMARY KEY AUTOINCREMENT" }

func (SQLiteDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

func (SQLiteDialect) TableExists() string {
	return "SELECT 1 FROM sqlite_master WHERE type='table' AND name=$1"
}

func (SQLiteDialect) ColumnExists() string {
	return "SELECT 1 FROM pragma_table_info($1) WHERE name=$2"
}

func (SQLiteDialect) CurrentTimestampValue() string { return "CURRENT_TIMESTAMP" }

// AddColumnIfNotExists returns empty string for SQLite since SQLite does not support
// ALTER TABLE ... ADD COLUMN IF NOT EXISTS. Callers should attempt the ALTER and
// ignore "duplicate column" errors.
func (SQLiteDialect) AddColumnIfNotExists(table, column, colType, defaultVal string) string {
	return ""
}
