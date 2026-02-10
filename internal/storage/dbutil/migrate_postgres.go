package dbutil

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// AddColumnIfNotExists adds a column to a table if it doesn't already exist.
// Works on both SQLite and PostgreSQL by attempting ALTER TABLE ADD COLUMN
// and ignoring "duplicate column" / "already exists" errors.
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

// sanitizeSQLIdentifier ensures a string is safe to use as a SQL identifier.
// Only allows alphanumeric, underscore, and dot (for schema-qualified names).
func sanitizeSQLIdentifier(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("identifier must not be empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.') {
			return "", fmt.Errorf("identifier %q contains unsafe character %q at position %d", s, string(c), i)
		}
	}
	return s, nil
}

// sanitizeSQLType validates a SQL type string.
// Allows common SQL types: alphanumeric, spaces, parentheses (for e.g. "VARCHAR(255)"), and commas.
func sanitizeSQLType(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("type must not be empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == ' ' || c == '(' || c == ')' || c == ',') {
			return "", fmt.Errorf("type %q contains unsafe character %q at position %d", s, string(c), i)
		}
	}
	return s, nil
}

// sanitizeDefaultValue validates a SQL default value against a strict whitelist.
// Allowed forms:
//   - Boolean/null keywords: TRUE, FALSE, NULL
//   - Timestamp keywords: CURRENT_TIMESTAMP, CURRENT_DATE, CURRENT_TIME
//   - Numeric literals: 0, 1, -1, 3.14
//   - Single-quoted string literals: '', 'hello', 'it''s' (balanced, no nested injection)
//   - Simple function calls: NOW(), datetime('now')
func sanitizeDefaultValue(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("default value must not be empty")
	}
	trimmed := strings.TrimSpace(s)
	upper := strings.ToUpper(trimmed)

	// Allow boolean/null keywords
	if upper == "TRUE" || upper == "FALSE" || upper == "NULL" {
		return s, nil
	}

	// Allow timestamp keywords
	if upper == "CURRENT_TIMESTAMP" || upper == "CURRENT_DATE" || upper == "CURRENT_TIME" {
		return s, nil
	}

	// Allow numeric values (digits, optional leading sign, optional decimal point)
	if isNumericLiteral(trimmed) {
		return s, nil
	}

	// Allow single-quoted string literals with balanced quotes
	if len(trimmed) >= 2 && trimmed[0] == '\'' {
		if isValidQuotedString(trimmed) {
			return s, nil
		}
		return "", fmt.Errorf("default value %q has unbalanced or invalid single quotes", s)
	}

	// Allow simple function calls: identifier(args) where args are literals or quoted strings
	if isSimpleFunctionCall(trimmed) {
		return s, nil
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
// Escaped quotes are represented as '' (two single quotes).
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
			// This is the closing quote — it must be the last character
			return i == len(s)-1
		}
		// Reject characters that could be used for injection
		if s[i] == ';' {
			return false
		}
		i++
	}
	// Reached end without closing quote
	return false
}

// isSimpleFunctionCall checks if s matches pattern: IDENTIFIER(ARGS)
// where IDENTIFIER is alphanumeric+underscore and ARGS contains only safe characters.
func isSimpleFunctionCall(s string) bool {
	// Find opening paren
	parenIdx := strings.IndexByte(s, '(')
	if parenIdx <= 0 || s[len(s)-1] != ')' {
		return false
	}

	// Validate function name: alphanumeric + underscore
	name := s[:parenIdx]
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}

	// Validate args: no semicolons, no double-dashes, no unbalanced quotes
	args := s[parenIdx+1 : len(s)-1]
	for i := 0; i < len(args); i++ {
		c := args[i]
		if c == ';' {
			return false
		}
		if c == '-' && i+1 < len(args) && args[i+1] == '-' {
			return false
		}
	}

	// If args contain quotes, they must be balanced
	quoteCount := 0
	for i := 0; i < len(args); i++ {
		if args[i] == '\'' {
			quoteCount++
		}
	}
	return quoteCount%2 == 0
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
