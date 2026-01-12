# Hot Replacement: SQLite to Turso Migration Guide

This guide explains how to enable hot replacement from SQLite to Turso in agentctl, enabling cloud deployment and native vector search capabilities for better memory support.

## Overview

Agentctl now supports two database backends:

1. **SQLite** (default) - Local file-based database
2. **Turso** - Cloud-based libSQL with native vector search support

The hot replacement mechanism allows you to switch between these backends without code changes, using environment variables for configuration.

## Why Turso?

Turso provides several advantages over local SQLite:

- **Cloud-native**: Deploy agentctl in cloud environments without local file system dependencies
- **Native Vector Search**: Built-in vector similarity search for AI/LLM features (no extensions needed)
- **Multi-tenancy**: Database-per-tenant pattern with efficient resource usage
- **Zero-latency reads**: Embedded replicas for fast local access
- **Mobile support**: On-device inference with database replication

## Quick Start

### 1. Set Up Turso

First, install the Turso CLI and create a database:

```bash
# Install Turso CLI
curl -sSfL https://get.tur.so/install.sh | bash

# Sign up or login
turso auth signup

# Create a new database
turso db create agentctl-cache
turso db create agentctl-jobs
turso db create agentctl-memory

# For vector search support (memory database), create a group with vector support
turso group create vector-group
turso group update vector-group  # Ensure it's on the latest version

# Create memory database in the vector group
turso db create agentctl-memory --group vector-group
```

### 2. Get Database URLs and Tokens

```bash
# Get database URLs
turso db show agentctl-cache --url
turso db show agentctl-jobs --url
turso db show agentctl-memory --url

# Create authentication tokens
turso db tokens create agentctl-cache
turso db tokens create agentctl-jobs
turso db tokens create agentctl-memory
```

### 3. Configure Environment Variables

Set the following environment variables to enable Turso:

```bash
# Enable Turso for all databases
export AGENTCTL_CACHE_DB_DRIVER=turso
export AGENTCTL_JOBS_DB_DRIVER=turso
export AGENTCTL_MEMORY_DB_DRIVER=turso

# Set Turso URLs
export AGENTCTL_CACHE_DB_URL=libsql://agentctl-cache-your-org.turso.io
export AGENTCTL_JOBS_DB_URL=libsql://agentctl-jobs-your-org.turso.io
export AGENTCTL_MEMORY_DB_URL=libsql://agentctl-memory-your-org.turso.io

# Set Turso tokens
export AGENTCTL_CACHE_DB_TOKEN=your_cache_token_here
export AGENTCTL_JOBS_DB_TOKEN=your_jobs_token_here
export AGENTCTL_MEMORY_DB_TOKEN=your_memory_token_here

# Optional: Enable vector search for memory database
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
export AGENTCTL_MEMORY_VECTOR_DIMS=384  # Default: 384 (all-MiniLM-L6-v2)
```

### 4. Run Agentctl

Once configured, agentctl will automatically use Turso instead of SQLite:

```bash
agentctl run your-command
```

## Configuration Reference

### Environment Variables

All configuration is done via environment variables. The pattern is:

```
AGENTCTL_<DATABASE>_<SETTING>=<value>
```

Where `<DATABASE>` is one of: `CACHE`, `JOBS`, or `MEMORY`.

#### General Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `AGENTCTL_<DB>_DB_DRIVER` | Database driver (`sqlite` or `turso`) | `sqlite` | `turso` |

#### SQLite Settings

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `AGENTCTL_<DB>_DB_PATH` | Path to SQLite database file | `~/.agentctl/<db>.db` | `/data/cache.db` |
| `AGENTCTL_<DB>_DB_WAL` | Enable WAL mode | `true` | `true` |
| `AGENTCTL_<DB>_DB_TIMEOUT` | Busy timeout (milliseconds) | `5000` | `10000` |

#### Turso Settings

| Variable | Description | Required | Example |
|----------|-------------|----------|---------|
| `AGENTCTL_<DB>_DB_URL` | Turso database URL | Yes | `libsql://db.turso.io` |
| `AGENTCTL_<DB>_DB_TOKEN` | Turso auth token | Yes | `eyJ...` |
| `AGENTCTL_TURSO_URL` | Fallback URL for all databases | No | `libsql://db.turso.io` |
| `AGENTCTL_TURSO_TOKEN` | Fallback token for all databases | No | `eyJ...` |

#### Vector Search Settings (Memory Database Only)

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `AGENTCTL_MEMORY_VECTOR_SEARCH` | Enable vector search | `false` | `true` |
| `AGENTCTL_MEMORY_VECTOR_DIMS` | Vector dimensions | `384` | `768` |

Common vector dimensions:
- `384` - all-MiniLM-L6-v2 (default)
- `768` - BERT base models
- `1536` - OpenAI ada-002 embeddings

### Fallback Configuration

You can set fallback values that apply to all databases:

```bash
# Use the same Turso instance for all databases
export AGENTCTL_TURSO_URL=libsql://agentctl.turso.io
export AGENTCTL_TURSO_TOKEN=your_token_here

# Enable Turso for specific databases
export AGENTCTL_CACHE_DB_DRIVER=turso
export AGENTCTL_JOBS_DB_DRIVER=turso
export AGENTCTL_MEMORY_DB_DRIVER=turso
```

Database-specific URLs/tokens override the fallback values.

## Migrating Data from SQLite to Turso

### Programmatic Migration

Use the built-in migration utilities to transfer data:

```go
package main

import (
    "context"
    "fmt"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func migrateToTurso() error {
    ctx := context.Background()

    // Open source SQLite database
    sourceConfig := dbdriver.DefaultSQLiteConfig("~/.agentctl/memory.db")
    source, err := dbdriver.OpenDB(ctx, sourceConfig, nil)
    if err != nil {
        return err
    }
    defer source.Close()

    // Open target Turso database
    targetConfig := dbdriver.DefaultTursoConfig(
        "libsql://agentctl-memory.turso.io",
        "your_token_here",
        "memory",
    )
    target, err := dbdriver.OpenDB(ctx, targetConfig, nil)
    if err != nil {
        return err
    }
    defer target.Close()

    // Create migrator
    options := dbdriver.DefaultMigrationOptions()
    options.BatchSize = 100
    options.ContinueOnError = false
    options.ProgressCallback = func(stats dbdriver.MigrationStats) {
        fmt.Printf("Migrated %d rows from %d tables\n",
            stats.RowsMigrated, stats.TablesProcessed)
    }

    migrator := dbdriver.NewMigrator(source, target, options)

    // Run migration
    stats, err := migrator.Migrate(ctx)
    if err != nil {
        return fmt.Errorf("migration failed: %w", err)
    }

    fmt.Printf("Migration complete: %d tables, %d rows\n",
        stats.TablesProcessed, stats.RowsMigrated)

    return nil
}
```

### Export to SQL

You can also export SQLite data to SQL statements:

```go
package main

import (
    "context"
    "os"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func exportToSQL() error {
    ctx := context.Background()

    sourceConfig := dbdriver.DefaultSQLiteConfig("~/.agentctl/memory.db")
    source, err := dbdriver.OpenDB(ctx, sourceConfig, nil)
    if err != nil {
        return err
    }
    defer source.Close()

    targetConfig := dbdriver.DefaultTursoConfig("", "", "")
    target, _ := dbdriver.OpenDB(ctx, targetConfig, nil)

    migrator := dbdriver.NewMigrator(source, target, dbdriver.DefaultMigrationOptions())

    file, err := os.Create("export.sql")
    if err != nil {
        return err
    }
    defer file.Close()

    return migrator.ExportToSQL(ctx, file)
}
```

Then import using Turso CLI:

```bash
turso db shell agentctl-memory < export.sql
```

## Vector Search Usage

### Enabling Vector Search

Vector search is only available for the memory database when using Turso:

```bash
export AGENTCTL_MEMORY_DB_DRIVER=turso
export AGENTCTL_MEMORY_DB_URL=libsql://agentctl-memory.turso.io
export AGENTCTL_MEMORY_DB_TOKEN=your_token
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
export AGENTCTL_MEMORY_VECTOR_DIMS=384
```

### Using Vector Search in Code

```go
package main

import (
    "context"
    "github.com/jkatigb/agentctl/internal/storage/memory"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func useVectorSearch() error {
    ctx := context.Background()

    // Open memory store with Turso
    store, err := memory.Open(ctx, "~/.agentctl", "~/.agentctl/cas")
    if err != nil {
        return err
    }

    // Enable vector search (if using Turso with vector support)
    // Get the underlying DB connection
    cfg := dbdriver.DefaultTursoConfig(
        "libsql://agentctl-memory.turso.io",
        "your_token",
        "memory",
    )
    cfg.Turso.EnableVectorSearch = true

    db, err := dbdriver.OpenDB(ctx, cfg, nil)
    if err != nil {
        return err
    }

    vectorStore, err := store.EnableVectorSearch(db)
    if err != nil {
        return err
    }

    // Save memory with embedding
    entry := memory.VectorEntry{
        NamedEntry: memory.NamedEntry{
            Name:      "test-memory",
            Workspace: "default",
            Summary:   "Test memory with vector",
        },
        Embedding: dbdriver.Vector{0.1, 0.2, 0.3, /* ... 384 dimensions ... */},
    }

    _, err = vectorStore.SaveWithEmbedding(ctx, entry)
    if err != nil {
        return err
    }

    // Search similar memories
    queryEmbedding := dbdriver.Vector{0.11, 0.19, 0.31, /* ... */}
    results, err := vectorStore.SearchSimilar(ctx, queryEmbedding, "default", 10)
    if err != nil {
        return err
    }

    for _, result := range results {
        println("Found:", result.Name, result.Summary)
    }

    return nil
}
```

## Hybrid Configuration

You can mix SQLite and Turso for different databases:

```bash
# Use SQLite for cache and jobs (local)
export AGENTCTL_CACHE_DB_DRIVER=sqlite
export AGENTCTL_JOBS_DB_DRIVER=sqlite

# Use Turso for memory (cloud with vector search)
export AGENTCTL_MEMORY_DB_DRIVER=turso
export AGENTCTL_MEMORY_DB_URL=libsql://agentctl-memory.turso.io
export AGENTCTL_MEMORY_DB_TOKEN=your_token
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
```

## Best Practices

### 1. Security

- **Never commit tokens to version control**
- Use environment variables or secret management systems
- Rotate tokens regularly
- Use separate tokens for development and production

### 2. Performance

- Use appropriate vector dimensions for your use case
- Start with 384 dimensions (all-MiniLM-L6-v2) for general use
- Monitor query performance and adjust accordingly
- Use vector indexes for datasets > 10,000 vectors

### 3. Migration

- Test migration with a subset of data first
- Back up SQLite databases before migration
- Verify data integrity after migration
- Keep SQLite backups until Turso is fully validated

### 4. Development Workflow

- Use SQLite for local development (faster, simpler)
- Use Turso for staging/production (cloud, vector search)
- Use environment-specific `.env` files

Example `.env.development`:
```bash
AGENTCTL_CACHE_DB_DRIVER=sqlite
AGENTCTL_JOBS_DB_DRIVER=sqlite
AGENTCTL_MEMORY_DB_DRIVER=sqlite
```

Example `.env.production`:
```bash
AGENTCTL_CACHE_DB_DRIVER=turso
AGENTCTL_JOBS_DB_DRIVER=turso
AGENTCTL_MEMORY_DB_DRIVER=turso
AGENTCTL_TURSO_URL=libsql://agentctl-prod.turso.io
AGENTCTL_TURSO_TOKEN=${TURSO_TOKEN}
AGENTCTL_MEMORY_VECTOR_SEARCH=true
```

## Troubleshooting

### Vector Search Not Available

If you see "vector search not available" errors:

1. Ensure your Turso group supports vectors:
   ```bash
   turso group list
   turso group update your-group
   ```

2. Check that the database is in the vector-enabled group:
   ```bash
   turso db show agentctl-memory
   ```

3. Verify environment variables are set correctly:
   ```bash
   echo $AGENTCTL_MEMORY_VECTOR_SEARCH
   echo $AGENTCTL_MEMORY_DB_DRIVER
   ```

### Connection Errors

If you can't connect to Turso:

1. Verify URL format: `libsql://your-db.turso.io`
2. Check token validity: `turso db tokens validate your-db`
3. Test connection: `turso db shell your-db`

### Migration Issues

If migration fails:

1. Check source database integrity
2. Verify target database permissions
3. Try smaller batch sizes
4. Enable `ContinueOnError` to skip problematic rows
5. Check migration stats for specific errors

## Architecture

### Database Driver Abstraction

The implementation uses a clean abstraction layer:

```
┌─────────────────────────────────────┐
│         Application Code             │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│      Storage Interfaces              │
│   (Cache, Jobs, Memory)              │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│      Database Driver Layer           │
│       (dbdriver package)             │
└──────────┬──────────────┬───────────┘
           │              │
           ▼              ▼
    ┌──────────┐   ┌──────────┐
    │  SQLite  │   │  Turso   │
    │  Driver  │   │  Driver  │
    └──────────┘   └──────────┘
```

### Key Components

1. **Config System** (`config.go`) - Environment-based configuration
2. **Driver Interface** (`driver.go`) - Unified database interface
3. **SQLite Implementation** (`sqlite.go`) - SQLite backend
4. **Turso Implementation** (`turso.go`) - Turso/libSQL backend
5. **Vector Search** (`vector.go`) - Vector operations and helpers
6. **Migration** (`migrate.go`) - Data migration utilities
7. **Backward Compatibility** (`compat.go`) - Legacy code support

## Contributing

To add support for additional database backends:

1. Implement the `DB` interface in `driver.go`
2. Add configuration in `config.go`
3. Update `OpenDB()` in `driver.go`
4. Add tests for the new backend
5. Update documentation

## Resources

- [Turso Documentation](https://docs.turso.tech/)
- [Turso Vector Search Guide](https://docs.turso.tech/features/vector-search)
- [libSQL GitHub](https://github.com/tursodatabase/libsql)
- [Turso Vector Search Blog Post](https://turso.tech/blog/introducing-turso-vector-search)

## License

This feature is part of agentctl and follows the same license.
