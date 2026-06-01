package dbutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// AddColumnIfNotExists adds a column to a table if it doesn't already exist.
// Works on both SQLite and PostgreSQL by attempting ALTER TABLE ADD COLUMN
// and suppressing only duplicate-column/already-exists errors.
//
// All identifier arguments (table, column, colType) are sanitized to prevent
// SQL injection. defaultVal is validated against a strict whitelist.
func AddColumnIfNotExists(ctx context.Context, db *sql.DB, table, column, colType, defaultVal string) error {
	safeTable, err := sanitizeSQLIdentifier(table)
	if err != nil {
		return fmt.Errorf("add column: invalid table name: %w", err)
	}
	safeColumn, err := sanitizeSQLIdentifier(column)
	if err != nil {
		return fmt.Errorf("add column: invalid column name: %w", err)
	}
	safeColType, err := sanitizeSQLType(colType)
	if err != nil {
		return fmt.Errorf("add column: invalid column type: %w", err)
	}

	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", safeTable, safeColumn, safeColType)
	if defaultVal != "" {
		safeDefault, defErr := sanitizeDefaultValue(defaultVal)
		if defErr != nil {
			return fmt.Errorf("add column %s.%s: invalid default: %w", table, column, defErr)
		}
		stmt += " DEFAULT " + safeDefault
	}
	_, err = db.ExecContext(ctx, stmt)
	if err != nil {
		// Both SQLite and PostgreSQL raise errors for duplicate columns.
		// SQLite: "duplicate column name: X"
		// PostgreSQL: "column "X" of relation "Y" already exists"
		errMsg := err.Error()
		if containsCI(errMsg, "duplicate column") || containsCI(errMsg, "already exists") {
			return nil
		}
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

// ColumnExists reports whether table.column exists for the SQLite and
// PostgreSQL-compatible stores foxctl migrates with raw *sql.DB handles.
func ColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	if strings.TrimSpace(table) == "" {
		return false, fmt.Errorf("table name is required")
	}
	if strings.TrimSpace(column) == "" {
		return false, fmt.Errorf("column name is required")
	}

	exists, sqliteErr := queryColumnExists(
		ctx, db,
		`SELECT 1 FROM pragma_table_info(?) WHERE name = ?`,
		table,
		column,
	)
	if sqliteErr == nil {
		return exists, nil
	}

	exists, postgresErr := queryColumnExists(
		ctx, db,
		`SELECT 1 FROM information_schema.columns WHERE table_name = $1 AND column_name = $2 AND table_schema = current_schema()`,
		table,
		column,
	)
	if postgresErr == nil {
		return exists, nil
	}

	return false, fmt.Errorf("check column %s.%s: sqlite: %v; postgres: %w", table, column, sqliteErr, postgresErr)
}

func queryColumnExists(ctx context.Context, db *sql.DB, query, table, column string) (bool, error) {
	var exists int
	err := db.QueryRowContext(ctx, query, table, column).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// sanitizeSQLIdentifier ensures a string is safe to use as a SQL identifier.
// Allows either one identifier or one schema-qualified identifier.
func sanitizeSQLIdentifier(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("identifier must not be empty")
	}
	parts := strings.Split(s, ".")
	if len(parts) > 2 {
		return "", fmt.Errorf("identifier %q has too many qualification segments", s)
	}
	for _, part := range parts {
		if err := validateSQLIdentifierPart(part); err != nil {
			return "", fmt.Errorf("identifier %q is unsafe: %w", s, err)
		}
	}
	return s, nil
}

func validateSQLIdentifierPart(part string) error {
	if part == "" {
		return fmt.Errorf("identifier segment must not be empty")
	}
	if len(part) > 64 {
		return fmt.Errorf("identifier segment %q is too long", part)
	}
	if !isSQLIdentifierStart(part[0]) {
		return fmt.Errorf("identifier segment %q must start with a letter or underscore", part)
	}
	for i := 1; i < len(part); i++ {
		if !isSQLIdentifierChar(part[i]) {
			return fmt.Errorf("identifier segment %q contains unsafe character %q at position %d", part, string(part[i]), i)
		}
	}
	return nil
}

func isSQLIdentifierStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isSQLIdentifierChar(c byte) bool {
	return isSQLIdentifierStart(c) || (c >= '0' && c <= '9')
}

// sanitizeSQLType validates a SQL type string.
// Allows a known column type with optional size/precision and optional nullability.
func sanitizeSQLType(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("type must not be empty")
	}

	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return "", fmt.Errorf("type must not be empty")
	}
	if !isAllowedSQLTypeBase(fields[0]) {
		return "", fmt.Errorf("type %q is not an allowed column type", s)
	}
	switch len(fields) {
	case 1:
		return trimmed, nil
	case 2:
		if strings.EqualFold(fields[1], "NULL") {
			return trimmed, nil
		}
	case 3:
		if strings.EqualFold(fields[1], "NOT") && strings.EqualFold(fields[2], "NULL") {
			return trimmed, nil
		}
	}
	return "", fmt.Errorf("type %q has unsupported modifiers", s)
}

func isAllowedSQLTypeBase(s string) bool {
	name := s
	if open := strings.IndexByte(s, '('); open >= 0 {
		if !strings.HasSuffix(s, ")") {
			return false
		}
		name = s[:open]
		params := s[open+1 : len(s)-1]
		if !isSQLTypeParameterList(params) {
			return false
		}
	}
	switch strings.ToUpper(name) {
	case "BIGINT", "BIGSERIAL", "BLOB", "BOOL", "BOOLEAN", "BYTEA",
		"CHAR", "DATE", "DECIMAL", "DOUBLE", "INTEGER", "JSON",
		"JSONB", "NUMERIC", "REAL", "SERIAL", "SMALLINT", "TEXT",
		"TIME", "TIMESTAMP", "TIMESTAMPTZ", "UUID", "VARCHAR", "VECTOR":
		return true
	default:
		return false
	}
}

func isSQLTypeParameterList(s string) bool {
	if s == "" {
		return false
	}
	for _, part := range strings.Split(s, ",") {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}

// sanitizeDefaultValue validates a SQL default value against a strict whitelist.
// Allowed forms:
//   - Boolean/null keywords: TRUE, FALSE, NULL
//   - Timestamp keywords: CURRENT_TIMESTAMP, CURRENT_DATE, CURRENT_TIME
//   - Numeric literals: 0, 1, -1, 3.14
//   - Single-quoted string literals with doubled quote escapes.
//   - Simple function calls: NOW(), datetime('now')
func sanitizeDefaultValue(s string) (string, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return "", fmt.Errorf("default value must not be empty")
	}
	upper := strings.ToUpper(trimmed)

	// Allow boolean/null keywords
	if upper == "TRUE" || upper == "FALSE" || upper == "NULL" {
		return trimmed, nil
	}

	// Allow timestamp keywords
	if upper == "CURRENT_TIMESTAMP" || upper == "CURRENT_DATE" || upper == "CURRENT_TIME" {
		return trimmed, nil
	}

	// Allow numeric values (digits, optional leading sign, optional decimal point)
	if isNumericLiteral(trimmed) {
		return trimmed, nil
	}

	// Allow single-quoted string literals with balanced quotes
	if len(trimmed) >= 2 && trimmed[0] == '\'' {
		if isValidQuotedString(trimmed) {
			return trimmed, nil
		}
		return "", fmt.Errorf("default value %q has unbalanced or invalid single quotes", s)
	}

	if isAllowedDefaultFunction(trimmed) {
		return trimmed, nil
	}

	return "", fmt.Errorf("default value %q is not a valid literal or allowed function call", s)
}

// isNumericLiteral checks if s is a valid numeric literal (integer or decimal).
func isNumericLiteral(s string) bool {
	if len(s) == 0 {
		return false
	}
	start := 0
	if s[0] == '+' || s[0] == '-' {
		start = 1
	}
	if start >= len(s) {
		return false
	}
	hasDigit := false
	hasDot := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			hasDigit = true
		} else if c == '.' && !hasDot {
			hasDot = true
		} else {
			return false
		}
	}
	return hasDigit
}

// isValidQuotedString checks if s is a properly balanced single-quoted SQL string.
// Escaped quotes are represented as two single quotes.
func isValidQuotedString(s string) bool {
	if len(s) < 2 || s[0] != '\'' {
		return false
	}
	// Walk through the string checking for balanced quotes
	i := 1
	for i < len(s) {
		if s[i] == '\'' {
			// Check if this is an escaped quote ''
			if i+1 < len(s) && s[i+1] == '\'' {
				i += 2 // skip escaped quote
				continue
			}
			// This is the closing quote; it must be the last character.
			return i == len(s)-1
		}
		// Reject statement separators and comment tokens even inside defaults.
		if s[i] == ';' {
			return false
		}
		if s[i] == '-' && i+1 < len(s) && s[i+1] == '-' {
			return false
		}
		if s[i] == '/' && i+1 < len(s) && s[i+1] == '*' {
			return false
		}
		if s[i] == '*' && i+1 < len(s) && s[i+1] == '/' {
			return false
		}
		i++
	}
	// Reached end without closing quote
	return false
}

func isAllowedDefaultFunction(s string) bool {
	switch strings.ToLower(s) {
	case "now()", "datetime('now')":
		return true
	default:
		return false
	}
}

// containsCI is a case-insensitive substring check.
func containsCI(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			sc := s[i+j]
			tc := substr[j]
			if sc >= 'A' && sc <= 'Z' {
				sc += 'a' - 'A'
			}
			if tc >= 'A' && tc <= 'Z' {
				tc += 'a' - 'A'
			}
			if sc != tc {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
