# Database Driver Abstraction Layer

This package provides a unified database interface that supports both SQLite and Turso backends, enabling hot replacement between local and cloud databases without code changes.

## Features

- **Hot Replacement**: Switch between SQLite and Turso via environment variables
- **Vector Search**: Native vector similarity search when using Turso
- **Backward Compatible**: Works with existing code expecting `*sql.DB`
- **Migration Tools**: Built-in utilities for transferring data between backends
- **Clean Abstraction**: Single interface for all database operations

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "github.com/joshka0/foxctl/internal/storage/dbdriver"
)

func main() {
    ctx := context.Background()

    // Load configuration from environment variables
    loader := dbdriver.NewConfigLoader("~/.foxctl")
    cfg := loader.LoadMemoryConfig()

    // Open database (SQLite or Turso based on env vars)
    db, err := dbdriver.OpenDB(ctx, cfg, nil)
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Use like any database
    db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, name TEXT)")
    db.Exec("INSERT INTO test (name) VALUES (?)", "example")

    var name string
    db.QueryRow("SELECT name FROM test WHERE id = 1").Scan(&name)
    println(name)
}
```

### Using with Existing Stores

The package is designed to work seamlessly with existing storage packages:

```go
package main

import (
    "context"
    "github.com/joshka0/foxctl/internal/storage/dbutil"
)

func main() {
    ctx := context.Background()

    // Automatically uses SQLite or Turso based on env vars.
    // Env var prefix is derived from storeName: e.g., FOXCTL_MEMORY_DB_DRIVER, FOXCTL_DB_DRIVER.
    db, closeFn, err := dbutil.OpenStoreDB(ctx, "~/.foxctl/storage", "MEMORY", "memory.db", nil)
    if err != nil {
        panic(err)
    }
    defer closeFn()

    // Use db normally
}
```

## Configuration

### Environment Variables

Configure the database backend using environment variables:

```bash
# Use SQLite (default)
export FOXCTL_MEMORY_DB_DRIVER=sqlite
export FOXCTL_MEMORY_DB_PATH=~/.foxctl/memory.db

# Use Turso
export FOXCTL_MEMORY_DB_DRIVER=turso
export FOXCTL_MEMORY_DB_URL=libsql://your-db.turso.io
export FOXCTL_MEMORY_DB_TOKEN=your_token_here

# Enable vector search (Turso only)
export FOXCTL_MEMORY_VECTOR_SEARCH=true
export FOXCTL_MEMORY_VECTOR_DIMS=384
```

### Programmatic Configuration

```go
// SQLite configuration
cfg := dbdriver.DefaultSQLiteConfig("~/.foxctl/memory.db")

// Turso configuration
cfg := dbdriver.DefaultTursoConfig(
    "libsql://your-db.turso.io",
    "your_token_here",
    "memory",
)
// DefaultTursoConfig uses memory.turso as the local replica path. Set
// cfg.Turso.Path when you need an absolute or storage-root-relative location.
cfg.Turso.EnableVectorSearch = true
cfg.Turso.VectorDimensions = 384

// Open with config
db, err := dbdriver.OpenDB(ctx, cfg, migrateFunc)
```

## Vector Search

### Setup

Vector search requires Turso with vector support enabled:

```go
cfg := dbdriver.DefaultTursoConfig(url, token, "memory")
cfg.Turso.EnableVectorSearch = true
cfg.Turso.VectorDimensions = 384

db, err := dbdriver.OpenDB(ctx, cfg, nil)
if err != nil {
    panic(err)
}

// Create vector helper
helper, err := dbdriver.NewVectorHelper(db)
if err != nil {
    panic(err)
}
```

### Basic Operations

```go
// Create vector
embedding := dbdriver.Vector{0.1, 0.2, 0.3, ...} // 384 dimensions

// Insert with vector
query := fmt.Sprintf(`
    INSERT INTO memories (name, embedding)
    VALUES (?, %s)
`, helper.VectorExpression(embedding))
db.Exec(query, "test")

// Search similar vectors
results, err := helper.SearchSimilar(
    ctx,
    "memories",           // table name
    "idx_memory_vector",  // index name
    "embedding",          // vector column
    queryVector,          // query embedding
    10,                   // limit
    "workspace = ?",      // additional filter
    "default",            // filter args
)
```

### Vector Helper Functions

```go
helper := dbdriver.NewVectorHelper(db)

// Vector operations
helper.VectorExpression(vec)              // vector('[1,2,3]')
helper.CosineSimilarity("col", vec)       // vector_distance_cos(col, '[1,2,3]')
helper.EuclideanDistance("col", vec)      // vector_distance_l2(col, '[1,2,3]')
helper.VectorTopK("idx", vec, k)          // vector_top_k('idx', '[1,2,3]', 10)
helper.ExtractVector("col")               // vector_extract(col)

// Schema operations
helper.CreateVectorColumn(ctx, "table", "embedding")
helper.CreateVectorIndex(ctx, "table", "embedding", "idx_name")

// Validation
helper.ValidateVector(vec)
```

## Data Migration

### Simple Migration

```go
// Open source and target databases
source, _ := dbdriver.OpenDB(ctx, sourceConfig, nil)
target, _ := dbdriver.OpenDB(ctx, targetConfig, nil)

// Create migrator
options := dbdriver.DefaultMigrationOptions()
migrator := dbdriver.NewMigrator(source, target, options)

// Run migration
stats, err := migrator.Migrate(ctx)
if err != nil {
    panic(err)
}

fmt.Printf("Migrated %d rows from %d tables\n",
    stats.RowsMigrated, stats.TablesProcessed)
```

### Advanced Migration

```go
options := dbdriver.MigrationOptions{
    Tables: []string{"memories", "cache"},  // specific tables
    BatchSize: 500,                          // larger batches
    DropTargetTables: true,                  // recreate tables
    ContinueOnError: true,                   // skip errors
    ProgressCallback: func(stats dbdriver.MigrationStats) {
        fmt.Printf("Progress: %d/%d tables, %d rows\n",
            stats.TablesProcessed, len(options.Tables), stats.RowsMigrated)
    },
}

migrator := dbdriver.NewMigrator(source, target, options)
stats, err := migrator.Migrate(ctx)

// Check for errors
if len(stats.Errors) > 0 {
    for _, err := range stats.Errors {
        fmt.Printf("Error: %v\n", err)
    }
}
```

### Export to SQL

```go
migrator := dbdriver.NewMigrator(source, target, options)

file, _ := os.Create("export.sql")
defer file.Close()

err := migrator.ExportToSQL(ctx, file)
```

## Backward Compatibility

For code that expects `*sql.DB`:

```go
// Open with new driver
db, err := dbdriver.OpenDB(ctx, cfg, nil)

// Convert to *sql.DB for legacy code
sqlDB, ok := dbdriver.ToSQLDB(db)
if !ok {
    panic("failed to convert")
}

// Use as normal *sql.DB
legacyFunction(sqlDB)

// Or use OpenDBCompat directly
sqlDB, err := dbdriver.OpenDBCompat(ctx, cfg, nil)
```

## Interface

The `DB` interface provides a unified API:

```go
type DB interface {
    // Standard database operations
    Close() error
    Exec(query string, args ...any) (sql.Result, error)
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    Query(query string, args ...any) (*sql.Rows, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRow(query string, args ...any) *sql.Row
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row

    // Transaction support
    Begin() (*sql.Tx, error)
    BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)

    // Connection management
    Ping() error
    PingContext(ctx context.Context) error
    SetMaxOpenConns(n int)
    SetMaxIdleConns(n int)
    SetConnMaxLifetime(d any)
    SetConnMaxIdleTime(d any)
    Stats() sql.DBStats

    // Additional capabilities
    GetUnderlyingDB() (*sql.DB, bool)
    IsVectorSearchEnabled() bool
    GetDriverType() DriverType
}
```

## Testing

Example test with both backends:

```go
func TestWithBothBackends(t *testing.T) {
    backends := []struct {
        name   string
        config dbdriver.Config
    }{
        {
            name:   "SQLite",
            config: dbdriver.DefaultSQLiteConfig(":memory:"),
        },
        {
            name: "Turso",
            config: dbdriver.DefaultTursoConfig(
                os.Getenv("TEST_TURSO_URL"),
                os.Getenv("TEST_TURSO_TOKEN"),
                "test",
            ),
        },
    }

    for _, backend := range backends {
        t.Run(backend.name, func(t *testing.T) {
            ctx := context.Background()
            db, err := dbdriver.OpenDB(ctx, backend.config, nil)
            if err != nil {
                t.Skip("Backend not available")
            }
            defer db.Close()

            // Run tests
            testDatabaseOperations(t, db)
        })
    }
}
```

## Architecture

```
┌─────────────────────────────────────┐
│        config.go                     │
│  Configuration & Env Loading         │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│        driver.go                     │
│    DB Interface & OpenDB()           │
└──────┬───────────────────┬──────────┘
       │                   │
       ▼                   ▼
┌─────────────┐     ┌─────────────┐
│  sqlite.go  │     │  turso.go   │
│             │     │             │
│  SQLite     │     │  Turso      │
│  Backend    │     │  Backend    │
└─────────────┘     └─────────────┘
       │                   │
       └─────────┬─────────┘
                 │
                 ▼
       ┌─────────────────┐
       │   vector.go     │
       │                 │
       │  Vector Search  │
       │  Operations     │
       └─────────────────┘
```

## Files

- `config.go` - Configuration types and loaders
- `config_loader.go` - Environment variable loader
- `driver.go` - Main DB interface and OpenDB function
- `sqlite.go` - SQLite implementation
- `turso.go` - Turso implementation
- `vector.go` - Vector search helpers
- `migrate.go` - Data migration utilities
- `compat.go` - Backward compatibility helpers

## Best Practices

1. **Use environment variables for configuration** - Makes deployment easier
2. **Test with both backends** - Ensures compatibility
3. **Use context for cancellation** - All operations support context
4. **Handle vector dimensions consistently** - Document expected dimensions
5. **Migrate incrementally** - Start with one database, then expand
6. **Monitor performance** - Vector search can be resource intensive
7. **Back up before migration** - Always have a rollback plan

## FAQ

**Q: Can I use SQLite and Turso simultaneously?**
A: Yes! You can use SQLite for some databases (cache, jobs) and Turso for others (memory).

**Q: Does vector search work with SQLite?**
A: No, native vector search is only available with Turso. You'll need to use an extension like sqlite-vss for SQLite.

**Q: How do I know which backend is being used?**
A: Use `db.GetDriverType()` to check, or look at startup logs.

**Q: What happens to existing SQLite data?**
A: Use the migration utilities to transfer data to Turso. Your SQLite files remain unchanged.

**Q: Can I switch back to SQLite after using Turso?**
A: Yes, just change the environment variables and restart. Use migration tools to transfer data back if needed.

## Support

For issues, questions, or contributions:
- Open an issue on GitHub
- See main documentation in `/docs/turso-migration.md`
- Check Turso docs: https://docs.turso.tech/
