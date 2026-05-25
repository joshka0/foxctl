package dbutil

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/quick"

	_ "modernc.org/sqlite"
)

func TestColumnExistsSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE example (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}

	exists, err := ColumnExists(ctx, db, "example", "name")
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected example.name to exist")
	}

	exists, err = ColumnExists(ctx, db, "example", "missing")
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expected example.missing to be absent")
	}
}

func TestAddColumnIfNotExistsSuppressesDuplicateColumnOnly(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE example (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	if err := AddColumnIfNotExists(ctx, db, "example", "name", "TEXT", ""); err != nil {
		t.Fatalf("first add column: %v", err)
	}
	if err := AddColumnIfNotExists(ctx, db, "example", "name", "TEXT", ""); err != nil {
		t.Fatalf("duplicate add column should be suppressed: %v", err)
	}
	if err := AddColumnIfNotExists(ctx, db, "missing", "name", "TEXT", ""); err == nil {
		t.Fatal("expected missing table error")
	}
}

func TestAddColumnIfNotExistsRejectsUnsafeDDLInputs(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, `CREATE TABLE example (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		table      string
		column     string
		columnType string
		defaultVal string
	}{
		{
			name:       "unsafe table",
			table:      "example; DROP TABLE example",
			column:     "name",
			columnType: "TEXT",
		},
		{
			name:       "unsafe column",
			table:      "example",
			column:     "bad-name",
			columnType: "TEXT",
		},
		{
			name:       "type carries default clause",
			table:      "example",
			column:     "name",
			columnType: "TEXT DEFAULT 0",
		},
		{
			name:       "default carries expression tail",
			table:      "example",
			column:     "name",
			columnType: "TEXT",
			defaultVal: "foo(1) OR (1)",
		},
		{
			name:       "default carries statement separator",
			table:      "example",
			column:     "name",
			columnType: "TEXT",
			defaultVal: "'ok'; DROP TABLE example",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AddColumnIfNotExists(ctx, db, tt.table, tt.column, tt.columnType, tt.defaultVal); err == nil {
				t.Fatal("expected unsafe DDL input to be rejected")
			}
		})
	}

	if exists, err := ColumnExists(ctx, db, "example", "id"); err != nil {
		t.Fatal(err)
	} else if !exists {
		t.Fatal("expected example table to remain intact")
	}
}

func TestSanitizeSQLIdentifierInvariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		valid bool
	}{
		{name: "plain table", input: "sessions", valid: true},
		{name: "schema qualified", input: "public.sessions", valid: true},
		{name: "underscore start", input: "_private", valid: true},
		{name: "digits after first", input: "session_2", valid: true},
		{name: "empty", input: "", valid: false},
		{name: "leading digit", input: "1session", valid: false},
		{name: "empty segment", input: "public.", valid: false},
		{name: "leading dot", input: ".sessions", valid: false},
		{name: "too many segments", input: "a.b.c", valid: false},
		{name: "dash", input: "session-id", valid: false},
		{name: "space", input: "session id", valid: false},
		{name: "quote", input: `session"id`, valid: false},
		{name: "statement separator", input: "sessions;DROP", valid: false},
		{name: "line comment", input: "sessions--", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeSQLIdentifier(tt.input)
			if tt.valid {
				if err != nil {
					t.Fatalf("expected valid identifier: %v", err)
				}
				if got != tt.input {
					t.Fatalf("sanitizeSQLIdentifier() = %q, want %q", got, tt.input)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected invalid identifier %q to be rejected", tt.input)
			}
		})
	}
}

func TestSanitizeSQLIdentifierPropertyAcceptedShape(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		if len(raw) > 96 {
			raw = raw[:96]
		}
		input := string(raw)
		got, err := sanitizeSQLIdentifier(input)
		if err != nil {
			return true
		}
		if got != input {
			t.Logf("identifier was rewritten: got %q want %q", got, input)
			return false
		}
		if !hasSafeIdentifierShape(got) {
			t.Logf("accepted unsafe identifier shape: %q", got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeSQLTypeInvariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{name: "text", input: "TEXT", want: "TEXT", valid: true},
		{name: "not null", input: "TEXT NOT NULL", want: "TEXT NOT NULL", valid: true},
		{name: "integer", input: "INTEGER", want: "INTEGER", valid: true},
		{name: "real", input: "REAL", want: "REAL", valid: true},
		{name: "blob", input: "BLOB", want: "BLOB", valid: true},
		{name: "varchar size", input: "VARCHAR(255)", want: "VARCHAR(255)", valid: true},
		{name: "numeric precision", input: "NUMERIC(10,2)", want: "NUMERIC(10,2)", valid: true},
		{name: "trimmed", input: " TEXT NOT NULL ", want: "TEXT NOT NULL", valid: true},
		{name: "default clause", input: "TEXT DEFAULT 0", valid: false},
		{name: "check clause", input: "TEXT CHECK (length(name) > 0)", valid: false},
		{name: "references clause", input: "INTEGER REFERENCES users(id)", valid: false},
		{name: "statement separator", input: "TEXT; DROP TABLE sessions", valid: false},
		{name: "unknown type keyword", input: "DROP", valid: false},
		{name: "empty parameter list", input: "VARCHAR()", valid: false},
		{name: "non-numeric parameter", input: "VARCHAR(max)", valid: false},
		{name: "unsupported trailing word", input: "TEXT NULL DROP", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeSQLType(tt.input)
			if tt.valid {
				if err != nil {
					t.Fatalf("expected valid type: %v", err)
				}
				if got != tt.want {
					t.Fatalf("sanitizeSQLType() = %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected invalid type %q to be rejected", tt.input)
			}
		})
	}
}

func TestSanitizeSQLTypePropertyAcceptedValuesCannotCarryClauses(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		if len(raw) > 96 {
			raw = raw[:96]
		}
		got, err := sanitizeSQLType(string(raw))
		if err != nil {
			return true
		}
		if hasSQLStatementBoundary(got) {
			t.Logf("accepted type with statement boundary: %q", got)
			return false
		}
		upper := " " + strings.ToUpper(got) + " "
		for _, keyword := range []string{" DEFAULT ", " CHECK ", " REFERENCES ", " DROP ", " ALTER ", " CREATE ", " INSERT ", " UPDATE ", " DELETE ", " SELECT "} {
			if strings.Contains(upper, keyword) {
				t.Logf("accepted type with unsafe clause %s: %q", keyword, got)
				return false
			}
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestSanitizeDefaultValueInvariants(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{name: "empty string literal", input: "''", want: "''", valid: true},
		{name: "json object literal", input: "'{}'", want: "'{}'", valid: true},
		{name: "escaped quote", input: "'it''s'", want: "'it''s'", valid: true},
		{name: "integer", input: "0", want: "0", valid: true},
		{name: "negative integer", input: "-1", want: "-1", valid: true},
		{name: "decimal", input: "3.14", want: "3.14", valid: true},
		{name: "boolean", input: "TRUE", want: "TRUE", valid: true},
		{name: "timestamp keyword", input: "CURRENT_TIMESTAMP", want: "CURRENT_TIMESTAMP", valid: true},
		{name: "now function", input: "NOW()", want: "NOW()", valid: true},
		{name: "sqlite datetime now", input: "datetime('now')", want: "datetime('now')", valid: true},
		{name: "trimmed", input: "  FALSE  ", want: "FALSE", valid: true},
		{name: "unbalanced quoted string", input: "'unterminated", valid: false},
		{name: "quoted statement separator", input: "'ok;still'", valid: false},
		{name: "quoted line comment", input: "'ok--still'", valid: false},
		{name: "arbitrary function call", input: "foo(1)", valid: false},
		{name: "function expression tail", input: "foo(1) OR (1)", valid: false},
		{name: "statement separator", input: "0; DROP TABLE sessions", valid: false},
		{name: "line comment", input: "0 -- comment", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sanitizeDefaultValue(tt.input)
			if tt.valid {
				if err != nil {
					t.Fatalf("expected valid default: %v", err)
				}
				if got != tt.want {
					t.Fatalf("sanitizeDefaultValue() = %q, want %q", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected invalid default %q to be rejected", tt.input)
			}
		})
	}
}

func TestSanitizeDefaultValuePropertyAcceptedValuesCannotCarryStatementBoundaries(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		if len(raw) > 96 {
			raw = raw[:96]
		}
		got, err := sanitizeDefaultValue(string(raw))
		if err != nil {
			return true
		}
		if got != strings.TrimSpace(got) {
			t.Logf("accepted default was not canonicalized: %q", got)
			return false
		}
		if hasSQLCommentOrSeparator(got) {
			t.Logf("accepted default with statement boundary: %q", got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func hasSafeIdentifierShape(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 64 || !isASCIIIdentifierStartForTest(part[0]) {
			return false
		}
		for i := 1; i < len(part); i++ {
			if !isASCIIIdentifierCharForTest(part[i]) {
				return false
			}
		}
	}
	return true
}

func isASCIIIdentifierStartForTest(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isASCIIIdentifierCharForTest(c byte) bool {
	return isASCIIIdentifierStartForTest(c) || (c >= '0' && c <= '9')
}

func hasSQLStatementBoundary(s string) bool {
	return hasSQLCommentOrSeparator(s) ||
		strings.ContainsAny(s, "'\"")
}

func hasSQLCommentOrSeparator(s string) bool {
	return strings.Contains(s, ";") ||
		strings.Contains(s, "--") ||
		strings.Contains(s, "/*") ||
		strings.Contains(s, "*/")
}
