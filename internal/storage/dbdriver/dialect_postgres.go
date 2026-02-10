package dbdriver

import "fmt"

// PostgresDialect implements Dialect for PostgreSQL databases (including pgvector).
type PostgresDialect struct{}

func (PostgresDialect) Name() string { return "postgres" }

// Rebind rewrites ?-style placeholders to $1, $2, ... for PostgreSQL.
func (PostgresDialect) Rebind(query string) string {
	return rebindToPositional(query)
}

func (PostgresDialect) Now() string { return "NOW()" }

func (PostgresDialect) JSONExtract(column, key string) string {
	return fmt.Sprintf("%s->>'%s'", column, sanitizeJSONKey(key))
}

func (PostgresDialect) JSONType() string { return "JSONB" }

func (PostgresDialect) BlobType() string { return "BYTEA" }

func (PostgresDialect) VectorType(dims int) string {
	return fmt.Sprintf("vector(%d)", dims)
}

func (PostgresDialect) AutoIncrement() string { return "BIGSERIAL PRIMARY KEY" }

func (PostgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

func (PostgresDialect) TableExists() string {
	return "SELECT 1 FROM information_schema.tables WHERE table_name=$1 AND table_schema=current_schema()"
}

func (PostgresDialect) ColumnExists() string {
	return "SELECT 1 FROM information_schema.columns WHERE table_name=$1 AND column_name=$2 AND table_schema=current_schema()"
}

func (PostgresDialect) CurrentTimestampValue() string { return "NOW()" }

// AddColumnIfNotExists returns ALTER TABLE ... ADD COLUMN IF NOT EXISTS for PostgreSQL.
func (PostgresDialect) AddColumnIfNotExists(table, column, colType, defaultVal string) string {
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s", table, column, colType)
	if defaultVal != "" {
		stmt += " DEFAULT " + defaultVal
	}
	return stmt
}
