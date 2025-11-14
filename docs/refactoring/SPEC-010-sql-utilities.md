# SPEC-010: Create Shared SQL Utilities Package

## Status
**Draft** | Priority: Low | Complexity: Low

## Problem Statement

While SPEC-003 addresses database **scanning** helpers, there are additional SQL-related utilities and patterns that are duplicated across storage packages:
- Query builders
- Transaction helpers
- Common WHERE clauses
- Migration helpers
- Schema utilities

This spec covers the **broader SQL utilities** beyond just scanning.

**Note**: This spec is **lower priority** than SPEC-003 and can be implemented after the core scanning helpers are in place.

## Current State Analysis

### Repeated Patterns

#### 1. Transaction Boilerplate (Repeated 6+ times)
```go
// internal/jobs/store.go:457
func (s *Store) FindOrPrepareSkillJob(...) (Job, bool, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return Job{}, false, err
    }

    defer func() {
        if err != nil {
            tx.Rollback()
        }
    }()

    // ... do work ...

    if err = tx.Commit(); err != nil {
        return Job{}, false, err
    }

    return job, isDup, nil
}
```

#### 2. Schema Creation (Repeated in each store)
```go
// internal/cache/store.go, jobs/store.go, memory/store.go
func Open(ctx context.Context, path string) (*Store, error) {
    db, err := sql.Open("sqlite", dbPath)
    if err != nil {
        return nil, err
    }

    // Create tables if not exist
    _, err = db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS cache_entries (
            key TEXT PRIMARY KEY,
            ...
        )
    `)
}
```

#### 3. WHERE Clause Building
```go
// Manual string concatenation for dynamic WHERE clauses
var conditions []string
var args []interface{}

if workspace != "" {
    conditions = append(conditions, "workspace = ?")
    args = append(args, workspace)
}

if state != "" {
    conditions = append(conditions, "state = ?")
    args = append(args, state)
}

where := ""
if len(conditions) > 0 {
    where = "WHERE " + strings.Join(conditions, " AND ")
}

query := "SELECT * FROM jobs " + where
```

## Proposed Solution

### SQL Utilities Package Structure

```go
// internal/storage/sqlutil/
├── scan.go           # Scanning helpers (from SPEC-003)
├── types.go          # Custom SQL types (from SPEC-003)
├── transaction.go    # Transaction helpers
├── query.go          # Query building utilities
├── migration.go      # Schema migration helpers
└── testing.go        # Test utilities
```

### 1. Transaction Helpers

```go
// internal/storage/sqlutil/transaction.go
package sqlutil

import (
    "context"
    "database/sql"
    "fmt"
)

// WithTransaction executes a function within a transaction
func WithTransaction(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
    tx, err := db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }

    defer func() {
        if p := recover(); p != nil {
            tx.Rollback()
            panic(p)
        }
    }()

    if err := fn(tx); err != nil {
        if rbErr := tx.Rollback(); rbErr != nil {
            return fmt.Errorf("transaction failed: %v (rollback error: %v)", err, rbErr)
        }
        return err
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit transaction: %w", err)
    }

    return nil
}

// WithTx is like WithTransaction but returns a value
func WithTx[T any](ctx context.Context, db *sql.DB, fn func(*sql.Tx) (T, error)) (T, error) {
    var result T

    err := WithTransaction(ctx, db, func(tx *sql.Tx) error {
        var err error
        result, err = fn(tx)
        return err
    })

    return result, err
}
```

Usage:
```go
// BEFORE
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
    return Job{}, false, err
}
defer func() {
    if err != nil {
        tx.Rollback()
    }
}()

// ... work ...

if err = tx.Commit(); err != nil {
    return Job{}, false, err
}

// AFTER
job, isDup, err := sqlutil.WithTx(ctx, s.db, func(tx *sql.Tx) (Job, bool, error) {
    // ... work ...
    return job, isDup, nil
})
```

### 2. Query Builder

```go
// internal/storage/sqlutil/query.go
package sqlutil

import (
    "fmt"
    "strings"
)

// QueryBuilder builds SQL queries with dynamic conditions
type QueryBuilder struct {
    table      string
    columns    []string
    conditions []string
    args       []interface{}
    orderBy    string
    limit      int
    offset     int
}

// NewQueryBuilder creates a new query builder
func NewQueryBuilder(table string) *QueryBuilder {
    return &QueryBuilder{
        table:   table,
        columns: []string{"*"},
    }
}

// Select sets the columns to select
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder {
    qb.columns = columns
    return qb
}

// Where adds a WHERE condition
func (qb *QueryBuilder) Where(condition string, args ...interface{}) *QueryBuilder {
    qb.conditions = append(qb.conditions, condition)
    qb.args = append(qb.args, args...)
    return qb
}

// WhereEq adds a WHERE column = ? condition
func (qb *QueryBuilder) WhereEq(column string, value interface{}) *QueryBuilder {
    return qb.Where(column+" = ?", value)
}

// WhereIn adds a WHERE column IN (?, ?, ...) condition
func (qb *QueryBuilder) WhereIn(column string, values []interface{}) *QueryBuilder {
    if len(values) == 0 {
        return qb
    }

    placeholders := strings.Repeat("?,", len(values))
    placeholders = placeholders[:len(placeholders)-1] // Remove trailing comma

    return qb.Where(column+" IN ("+placeholders+")", values...)
}

// OrderBy sets the ORDER BY clause
func (qb *QueryBuilder) OrderBy(column string, direction string) *QueryBuilder {
    qb.orderBy = column + " " + direction
    return qb
}

// Limit sets the LIMIT clause
func (qb *QueryBuilder) Limit(limit int) *QueryBuilder {
    qb.limit = limit
    return qb
}

// Offset sets the OFFSET clause
func (qb *QueryBuilder) Offset(offset int) *QueryBuilder {
    qb.offset = offset
    return qb
}

// Build returns the SQL query and arguments
func (qb *QueryBuilder) Build() (string, []interface{}) {
    var parts []string

    // SELECT
    parts = append(parts, "SELECT "+strings.Join(qb.columns, ", "))

    // FROM
    parts = append(parts, "FROM "+qb.table)

    // WHERE
    if len(qb.conditions) > 0 {
        parts = append(parts, "WHERE "+strings.Join(qb.conditions, " AND "))
    }

    // ORDER BY
    if qb.orderBy != "" {
        parts = append(parts, "ORDER BY "+qb.orderBy)
    }

    // LIMIT
    if qb.limit > 0 {
        parts = append(parts, fmt.Sprintf("LIMIT %d", qb.limit))
    }

    // OFFSET
    if qb.offset > 0 {
        parts = append(parts, fmt.Sprintf("OFFSET %d", qb.offset))
    }

    return strings.Join(parts, " "), qb.args
}
```

Usage:
```go
// BEFORE
var conditions []string
var args []interface{}

if workspace != "" {
    conditions = append(conditions, "workspace = ?")
    args = append(args, workspace)
}

if state != "" {
    conditions = append(conditions, "state = ?")
    args = append(args, state)
}

where := ""
if len(conditions) > 0 {
    where = "WHERE " + strings.Join(conditions, " AND ")
}

query := "SELECT * FROM jobs " + where + " ORDER BY created_at DESC LIMIT ?"
args = append(args, limit)

// AFTER
qb := sqlutil.NewQueryBuilder("jobs")
if workspace != "" {
    qb = qb.WhereEq("workspace", workspace)
}
if state != "" {
    qb = qb.WhereEq("state", state)
}
qb = qb.OrderBy("created_at", "DESC").
    Limit(limit)

query, args := qb.Build()
```

### 3. Migration Helpers

```go
// internal/storage/sqlutil/migration.go
package sqlutil

import (
    "context"
    "database/sql"
    "fmt"
)

// Migration represents a database migration
type Migration struct {
    Version int
    Name    string
    Up      string
    Down    string
}

// Migrator handles database migrations
type Migrator struct {
    db         *sql.DB
    migrations []Migration
}

// NewMigrator creates a new migrator
func NewMigrator(db *sql.DB) *Migrator {
    return &Migrator{db: db}
}

// Add adds a migration
func (m *Migrator) Add(version int, name, up, down string) *Migrator {
    m.migrations = append(m.migrations, Migration{
        Version: version,
        Name:    name,
        Up:      up,
        Down:    down,
    })
    return m
}

// Migrate runs all pending migrations
func (m *Migrator) Migrate(ctx context.Context) error {
    // Create migrations table if not exists
    _, err := m.db.ExecContext(ctx, `
        CREATE TABLE IF NOT EXISTS schema_migrations (
            version INTEGER PRIMARY KEY,
            name TEXT NOT NULL,
            applied_at TEXT NOT NULL
        )
    `)
    if err != nil {
        return err
    }

    // Get current version
    var currentVersion int
    err = m.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").
        Scan(&currentVersion)
    if err != nil {
        return err
    }

    // Apply pending migrations
    for _, migration := range m.migrations {
        if migration.Version <= currentVersion {
            continue
        }

        err = WithTransaction(ctx, m.db, func(tx *sql.Tx) error {
            version := migration.Version
            name := migration.Name

            if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
                return fmt.Errorf("migration %d (%s) failed: %w", version, name, err)
            }

            _, err := tx.ExecContext(ctx, `
                INSERT INTO schema_migrations (version, name, applied_at)
                VALUES (?, ?, datetime('now'))
            `, version, name)

            return err
        })

        if err != nil {
            return err
        }
    }

    return nil
}
```

Usage:
```go
// In store Open() function
migrator := sqlutil.NewMigrator(db).
    Add(1, "initial_schema", `
        CREATE TABLE cache_entries (
            key TEXT PRIMARY KEY,
            workspace TEXT,
            result BLOB,
            created_at TEXT
        )
    `, "DROP TABLE cache_entries").
    Add(2, "add_digests_column", `
        ALTER TABLE cache_entries ADD COLUMN digests TEXT
    `, "ALTER TABLE cache_entries DROP COLUMN digests")

if err := migrator.Migrate(ctx); err != nil {
    return nil, fmt.Errorf("migrate: %w", err)
}
```

### 4. Test Utilities

```go
// internal/storage/sqlutil/testing.go
package sqlutil

import (
    "context"
    "database/sql"
    "testing"
)

// TestDB creates an in-memory SQLite database for testing
func TestDB(t *testing.T) *sql.DB {
    db, err := sql.Open("sqlite", ":memory:")
    if err != nil {
        t.Fatalf("open test db: %v", err)
    }

    t.Cleanup(func() {
        db.Close()
    })

    return db
}

// TestStore creates a test store with schema
func TestStore(t *testing.T, schema string) *sql.DB {
    db := TestDB(t)

    _, err := db.ExecContext(context.Background(), schema)
    if err != nil {
        t.Fatalf("create schema: %v", err)
    }

    return db
}

// MustExec executes SQL and fails test on error
func MustExec(t *testing.T, db *sql.DB, query string, args ...interface{}) {
    _, err := db.ExecContext(context.Background(), query, args...)
    if err != nil {
        t.Fatalf("exec failed: %v\nquery: %s", err, query)
    }
}

// MustQuery executes a query and fails test on error
func MustQuery(t *testing.T, db *sql.DB, query string, args ...interface{}) *sql.Rows {
    rows, err := db.QueryContext(context.Background(), query, args...)
    if err != nil {
        t.Fatalf("query failed: %v\nquery: %s", err, query)
    }
    return rows
}
```

Usage in tests:
```go
func TestCacheStore_Get(t *testing.T) {
    db := sqlutil.TestStore(t, cacheSchema)

    sqlutil.MustExec(t, db, `
        INSERT INTO cache_entries (key, workspace, result)
        VALUES (?, ?, ?)
    `, "key1", "ws1", []byte("data"))

    // Test store operations...
}
```

## Implementation Plan

### Step 1: Implement Transaction Helpers (1.5 hours)
- [ ] Create `transaction.go`
- [ ] Implement WithTransaction
- [ ] Implement WithTx
- [ ] Add tests

### Step 2: Implement Query Builder (2 hours)
- [ ] Create `query.go`
- [ ] Implement QueryBuilder
- [ ] Add WHERE, ORDER BY, LIMIT support
- [ ] Add tests

### Step 3: Implement Migration Helpers (2 hours)
- [ ] Create `migration.go`
- [ ] Implement Migrator
- [ ] Add version tracking
- [ ] Add tests

### Step 4: Implement Test Utilities (1 hour)
- [ ] Create `testing.go`
- [ ] Implement TestDB, TestStore
- [ ] Implement MustExec, MustQuery
- [ ] Add examples

### Step 5: Migrate Existing Code (3 hours)
- [ ] Update jobs package to use transaction helpers
- [ ] Update cache/memory/jobs to use query builder where appropriate
- [ ] Update tests to use test utilities
- [ ] Verify all tests pass

### Step 6: Documentation (0.5 hours)
- [ ] Add godoc to all utilities
- [ ] Add usage examples
- [ ] Document best practices

**Total Estimated Time**: 10 hours

## Benefits

### Before: Repeated Transaction Code
```go
// 15 lines of boilerplate per transaction
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
    return Job{}, false, err
}
defer func() {
    if err != nil {
        tx.Rollback()
    }
}()

// actual work

if err = tx.Commit(); err != nil {
    return Job{}, false, err
}
```

### After: 1 Line
```go
job, isDup, err := sqlutil.WithTx(ctx, s.db, func(tx *sql.Tx) (Job, bool, error) {
    // actual work
})
```

### Improvements
- ✅ **Reduced boilerplate**: 15 lines → 1 line per transaction
- ✅ **Consistent error handling**: Automatic rollback
- ✅ **Safer**: Handles panics
- ✅ **Type-safe**: Generic WithTx for return values
- ✅ **Testable**: Test utilities simplify tests
- ✅ **Maintainable**: Query builder for dynamic queries

## Success Criteria

- [ ] Transaction helpers implemented
- [ ] Query builder implemented
- [ ] Migration helpers implemented
- [ ] Test utilities implemented
- [ ] At least 3 packages migrated to use utilities
- [ ] All tests pass
- [ ] 90%+ test coverage
- [ ] Documentation complete

## Related Specs
- **SPEC-003**: Database Scanning Helpers (complementary - handles scanning, this handles queries)
- SPEC-001: Storage Interfaces (utilities work with interfaces)
- SPEC-008: Reorganize Packages (sqlutil goes in storage/)

## References
- database/sql best practices
- GORM/sqlx for inspiration (but simpler)
- Go database testing patterns
