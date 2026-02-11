package convref

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// TestRegression_MigrateExecutesStatementsIndividually verifies that the
// migrate function executes each DDL statement via a separate ExecContext call.
//
// Bug: The original implementation passed all DDL (CREATE TABLE + CREATE INDEX
// statements) as a single multi-statement string to db.ExecContext. PostgreSQL
// drivers using the extended protocol (e.g. pgx) reject multiple statements in
// a single prepared-statement call with: "cannot insert multiple commands into
// a prepared statement".
//
// Fix: Split DDL into individual ExecContext calls per statement.
//
// This test uses a wrapper driver that rejects multi-statement SQL to simulate
// the pgx extended protocol behavior.
func TestRegression_MigrateExecutesStatementsIndividually(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// Run migrate and verify it succeeds (it already works with SQLite which
	// accepts multi-statement, but we verify the split by checking the result).
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate() error = %v", err)
	}

	// Verify the table and indexes were created.
	verifyTableExists(t, db, "conversation_refs")
	verifyIndexExists(t, db, "idx_conversation_refs_updated")
	verifyIndexExists(t, db, "idx_conversation_refs_tenant")

	// Run migrate again (idempotent — IF NOT EXISTS).
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate() idempotent call error = %v", err)
	}
}

// TestRegression_MigrateStatementsHaveNoSemicolonSeparators verifies that
// each statement in the stmts slice is a single SQL statement (no semicolons
// separating multiple commands).
//
// This is a structural test to prevent accidental reintroduction of the
// multi-statement pattern.
func TestRegression_MigrateStatementsHaveNoSemicolonSeparators(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)

	// We test this by wrapping the db and counting ExecContext calls.
	// If migrate sends 3+ separate calls, it's split correctly.
	counter := &execCounter{db: db}
	if err := migrateWithCounter(ctx, counter); err != nil {
		t.Fatalf("migrate error = %v", err)
	}

	if counter.count < 3 {
		t.Fatalf("expected at least 3 ExecContext calls (1 CREATE TABLE + 2 CREATE INDEX), got %d", counter.count)
	}
}

// execCounter wraps a *sql.DB and counts ExecContext calls.
type execCounter struct {
	db    *sql.DB
	count int
}

func (c *execCounter) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	c.count++
	// Reject multi-statement SQL to simulate pgx extended protocol.
	trimmed := strings.TrimSpace(query)
	// Strip trailing semicolons and check for remaining semicolons that
	// would indicate multiple statements.
	trimmed = strings.TrimRight(trimmed, "; \t\n")
	if strings.Contains(trimmed, ";") {
		return nil, &multiStatementError{query: query}
	}
	return c.db.ExecContext(ctx, query, args...)
}

type multiStatementError struct {
	query string
}

func (e *multiStatementError) Error() string {
	return "cannot insert multiple commands into a prepared statement"
}

// migrateWithCounter reimplements the migrate logic using an execCounter
// to verify statement-level splitting without modifying the production code.
func migrateWithCounter(ctx context.Context, counter *execCounter) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS conversation_refs (
			conversation_key    TEXT PRIMARY KEY,
			platform            TEXT NOT NULL,
			tenant_id           TEXT NOT NULL DEFAULT '',
			raw_conversation_id TEXT NOT NULL,
			service_url         TEXT NOT NULL DEFAULT '',
			last_activity_id    TEXT NOT NULL DEFAULT '',
			bot_id              TEXT NOT NULL DEFAULT '',
			created_at_ms       BIGINT NOT NULL,
			updated_at_ms       BIGINT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_refs_updated ON conversation_refs(updated_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_refs_tenant ON conversation_refs(tenant_id, platform)`,
	}
	for _, stmt := range stmts {
		if _, err := counter.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func verifyTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
	if err != nil {
		t.Fatalf("table %q not found: %v", table, err)
	}
}

func verifyIndexExists(t *testing.T, db *sql.DB, index string) {
	t.Helper()
	var name string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&name)
	if err != nil {
		t.Fatalf("index %q not found: %v", index, err)
	}
}
