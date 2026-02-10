package dbdriver

import (
	"testing"
)

func TestRebindToPositional(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no placeholders",
			input:  "SELECT * FROM t",
			expect: "SELECT * FROM t",
		},
		{
			name:   "single placeholder",
			input:  "SELECT * FROM t WHERE id = ?",
			expect: "SELECT * FROM t WHERE id = $1",
		},
		{
			name:   "multiple placeholders",
			input:  "INSERT INTO t (a, b, c) VALUES (?, ?, ?)",
			expect: "INSERT INTO t (a, b, c) VALUES ($1, $2, $3)",
		},
		{
			name:   "placeholder in quoted string ignored",
			input:  "SELECT * FROM t WHERE name = '?' AND id = ?",
			expect: "SELECT * FROM t WHERE name = '?' AND id = $1",
		},
		{
			name:   "escaped single quote",
			input:  "SELECT * FROM t WHERE name = 'it''s' AND id = ?",
			expect: "SELECT * FROM t WHERE name = 'it''s' AND id = $1",
		},
		{
			name:   "double-quoted identifier",
			input:  `SELECT * FROM "table?" WHERE id = ?`,
			expect: `SELECT * FROM "table?" WHERE id = $1`,
		},
		{
			name:   "complex query",
			input:  "INSERT INTO t (a, b) VALUES (?, ?) ON CONFLICT(a) DO UPDATE SET b = ?",
			expect: "INSERT INTO t (a, b) VALUES ($1, $2) ON CONFLICT(a) DO UPDATE SET b = $3",
		},
		{
			name:   "unterminated single quote",
			input:  "SELECT * FROM t WHERE name = 'broken AND id = ?",
			expect: "SELECT * FROM t WHERE name = 'broken AND id = ?",
		},
		{
			name:   "unterminated double quote",
			input:  `SELECT * FROM "broken WHERE id = ?`,
			expect: `SELECT * FROM "broken WHERE id = ?`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rebindToPositional(tt.input)
			if got != tt.expect {
				t.Errorf("rebindToPositional(%q)\n  got:  %q\n  want: %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestSQLiteDialect(t *testing.T) {
	d := SQLiteDialect{}

	if d.Name() != "sqlite" {
		t.Errorf("Name() = %q, want %q", d.Name(), "sqlite")
	}

	if got := d.Now(); got != "datetime('now')" {
		t.Errorf("Now() = %q, want %q", got, "datetime('now')")
	}

	if got := d.JSONType(); got != "TEXT" {
		t.Errorf("JSONType() = %q, want %q", got, "TEXT")
	}

	if got := d.BlobType(); got != "BLOB" {
		t.Errorf("BlobType() = %q, want %q", got, "BLOB")
	}

	if got := d.VectorType(1024); got != "BLOB" {
		t.Errorf("VectorType(1024) = %q, want %q", got, "BLOB")
	}

	if got := d.AutoIncrement(); got != "INTEGER PRIMARY KEY AUTOINCREMENT" {
		t.Errorf("AutoIncrement() = %q, want %q", got, "INTEGER PRIMARY KEY AUTOINCREMENT")
	}

	if got := d.JSONExtract("data", "name"); got != "json_extract(data, '$.name')" {
		t.Errorf("JSONExtract() = %q, want %q", got, "json_extract(data, '$.name')")
	}

	// Verify sanitization strips unsafe chars
	if got := d.JSONExtract("data", "name'; DROP TABLE--"); got != "json_extract(data, '$.nameDROPTABLE--')" {
		t.Errorf("JSONExtract(unsafe) = %q, want sanitized", got)
	}

	if got := d.Placeholder(3); got != "$3" {
		t.Errorf("Placeholder(3) = %q, want %q", got, "$3")
	}

	// SQLite Rebind is pass-through
	q := "SELECT * FROM t WHERE id = ? AND name = ?"
	if got := d.Rebind(q); got != q {
		t.Errorf("Rebind should be pass-through for SQLite, got %q", got)
	}

	// AddColumnIfNotExists returns empty for SQLite
	if got := d.AddColumnIfNotExists("t", "col", "TEXT", "''"); got != "" {
		t.Errorf("AddColumnIfNotExists should be empty for SQLite, got %q", got)
	}
}

func TestPostgresDialect(t *testing.T) {
	d := PostgresDialect{}

	if d.Name() != "postgres" {
		t.Errorf("Name() = %q, want %q", d.Name(), "postgres")
	}

	if got := d.Now(); got != "NOW()" {
		t.Errorf("Now() = %q, want %q", got, "NOW()")
	}

	if got := d.JSONType(); got != "JSONB" {
		t.Errorf("JSONType() = %q, want %q", got, "JSONB")
	}

	if got := d.BlobType(); got != "BYTEA" {
		t.Errorf("BlobType() = %q, want %q", got, "BYTEA")
	}

	if got := d.VectorType(1024); got != "vector(1024)" {
		t.Errorf("VectorType(1024) = %q, want %q", got, "vector(1024)")
	}

	if got := d.AutoIncrement(); got != "BIGSERIAL PRIMARY KEY" {
		t.Errorf("AutoIncrement() = %q, want %q", got, "BIGSERIAL PRIMARY KEY")
	}

	if got := d.JSONExtract("data", "name"); got != "data->>'name'" {
		t.Errorf("JSONExtract() = %q, want %q", got, "data->>'name'")
	}

	if got := d.Placeholder(3); got != "$3" {
		t.Errorf("Placeholder(3) = %q, want %q", got, "$3")
	}

	// Postgres Rebind rewrites ? to $N
	got := d.Rebind("SELECT * FROM t WHERE id = ? AND name = ?")
	want := "SELECT * FROM t WHERE id = $1 AND name = $2"
	if got != want {
		t.Errorf("Rebind()\n  got:  %q\n  want: %q", got, want)
	}

	// AddColumnIfNotExists
	got = d.AddColumnIfNotExists("users", "email", "TEXT", "''")
	want = "ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT DEFAULT ''"
	if got != want {
		t.Errorf("AddColumnIfNotExists()\n  got:  %q\n  want: %q", got, want)
	}

	// Without default
	got = d.AddColumnIfNotExists("users", "email", "TEXT", "")
	want = "ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT"
	if got != want {
		t.Errorf("AddColumnIfNotExists() no default\n  got:  %q\n  want: %q", got, want)
	}
}

func TestDialectFor(t *testing.T) {
	tests := []struct {
		driver DriverType
		expect string
	}{
		{DriverSQLite, "sqlite"},
		{DriverLibSQL, "sqlite"},
		{DriverTurso, "sqlite"},
		{DriverPostgres, "postgres"},
		{"unknown", "sqlite"},
	}

	for _, tt := range tests {
		t.Run(string(tt.driver), func(t *testing.T) {
			d := DialectFor(tt.driver)
			if d.Name() != tt.expect {
				t.Errorf("DialectFor(%q).Name() = %q, want %q", tt.driver, d.Name(), tt.expect)
			}
		})
	}
}
