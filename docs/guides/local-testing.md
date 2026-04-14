# Local Testing with libSQL

This guide explains how to test foxctl with full vector search support locally, without requiring Turso cloud access or internet connectivity.

## Overview

Agentctl now supports **local libSQL** - a file-based database with native vector search capabilities. This is perfect for:

- ✅ Integration tests
- ✅ Unit tests
- ✅ CI/CD pipelines
- ✅ Local development
- ✅ Offline testing

**No cloud connection, tokens, or Turso account required!**

## Quick Start

### 1. Configure Environment

```bash
# Use local libSQL with vector search
export AGENTCTL_MEMORY_DB_DRIVER=libsql
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
export AGENTCTL_MEMORY_VECTOR_DIMS=384

# Run your tests
go test ./...
```

That's it! Your tests now have full vector search capabilities locally.

### 2. Using Configuration File

```bash
# Copy the example configuration
cp .env.libsql.test.example .env.test

# Source it
source .env.test

# Run tests
go test -v ./...
```

## Database Driver Comparison

| Feature | SQLite | libSQL (local) | Turso (cloud) |
|---------|--------|----------------|---------------|
| **Location** | Local file | Local file | Cloud |
| **Vector Search** | ❌ No | ✅ Yes | ✅ Yes |
| **BM25 Search** | ✅ Yes | ✅ Yes | ✅ Yes |
| **Hybrid Search** | ❌ No | ✅ Yes | ✅ Yes |
| **Internet Required** | No | No | Yes |
| **Credentials Required** | No | No | Yes (token) |
| **Best For** | Basic tests | Full feature tests | Production |

## Configuration

### Environment Variables

```bash
# Driver selection
export AGENTCTL_MEMORY_DB_DRIVER=libsql

# Database path (optional, defaults to ~/.foxctl/memory.libsql)
export AGENTCTL_MEMORY_DB_PATH=/tmp/test-memory.libsql

# Enable vector search
export AGENTCTL_MEMORY_VECTOR_SEARCH=true

# Vector dimensions (match your embedding model)
export AGENTCTL_MEMORY_VECTOR_DIMS=384
```

### Programmatic Configuration

```go
package mytest

import (
    "context"
    "testing"
    "github.com/joshka0/foxctl/internal/storage/dbdriver"
)

func TestWithLibSQL(t *testing.T) {
    ctx := context.Background()

    // Create local libSQL configuration
    cfg := dbdriver.DefaultLibSQLConfig(
        "/tmp/test.libsql",  // path
        true,                 // enable vectors
    )

    // Open database
    db, err := dbdriver.OpenDB(ctx, cfg, nil)
    if err != nil {
        t.Fatal(err)
    }
    defer db.Close()

    // Now you have full vector search capabilities!
    if !db.IsVectorSearchEnabled() {
        t.Fatal("vector search should be enabled")
    }

    // Run your tests...
}
```

## Testing Strategies

### Strategy 1: SQLite for Basic, libSQL for Vectors

Use SQLite for tests that don't need vectors, libSQL when you do:

```go
func TestBasicOperations(t *testing.T) {
    // SQLite is fine - faster, simpler
    cfg := dbdriver.DefaultSQLiteConfig(":memory:")
    // ... test basic CRUD operations
}

func TestVectorSearch(t *testing.T) {
    // libSQL for vector features
    cfg := dbdriver.DefaultLibSQLConfig("/tmp/test.libsql", true)
    // ... test vector search, hybrid search, etc.
}
```

### Strategy 2: Always Use libSQL

Use libSQL for all tests for consistency:

```go
func TestMain(m *testing.M) {
    // Set up libSQL for all tests
    os.Setenv("AGENTCTL_MEMORY_DB_DRIVER", "libsql")
    os.Setenv("AGENTCTL_MEMORY_VECTOR_SEARCH", "true")

    code := m.Run()

    // Clean up
    os.Remove("/tmp/test.libsql")

    os.Exit(code)
}
```

### Strategy 3: Test All Drivers

Test against all three backends for maximum compatibility:

```go
func TestAgainstAllDrivers(t *testing.T) {
    backends := []struct {
        name   string
        config dbdriver.Config
    }{
        {
            name:   "SQLite",
            config: dbdriver.DefaultSQLiteConfig(":memory:"),
        },
        {
            name:   "libSQL",
            config: dbdriver.DefaultLibSQLConfig("/tmp/test.libsql", true),
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

            // Skip Turso if not configured
            if backend.name == "Turso" && backend.config.Turso.URL == "" {
                t.Skip("Turso not configured")
            }

            db, err := dbdriver.OpenDB(ctx, backend.config, nil)
            if err != nil {
                t.Fatal(err)
            }
            defer db.Close()

            // Run tests against this backend
            testDatabaseOperations(t, db)
        })
    }
}
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Test with libSQL

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.22'

      - name: Run tests with libSQL
        env:
          AGENTCTL_MEMORY_DB_DRIVER: libsql
          AGENTCTL_MEMORY_VECTOR_SEARCH: true
          AGENTCTL_MEMORY_VECTOR_DIMS: 384
        run: |
          go test -v -race -coverprofile=coverage.out ./...

      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out
```

### GitLab CI

```yaml
test:
  image: golang:1.22
  variables:
    AGENTCTL_MEMORY_DB_DRIVER: "libsql"
    AGENTCTL_MEMORY_VECTOR_SEARCH: "true"
    AGENTCTL_MEMORY_VECTOR_DIMS: "384"
  script:
    - go test -v -race ./...
  artifacts:
    reports:
      junit: report.xml
```

### CircleCI

```yaml
version: 2.1

jobs:
  test:
    docker:
      - image: cimg/go:1.22
    environment:
      AGENTCTL_MEMORY_DB_DRIVER: libsql
      AGENTCTL_MEMORY_VECTOR_SEARCH: true
      AGENTCTL_MEMORY_VECTOR_DIMS: 384
    steps:
      - checkout
      - run:
          name: Run tests
          command: go test -v ./...
```

## Complete Test Example

```go
package memory_test

import (
    "context"
    "os"
    "testing"

    "github.com/joshka0/foxctl/internal/storage/dbdriver"
    "github.com/joshka0/foxctl/internal/storage/memory"
)

func TestMemorySearchWithLibSQL(t *testing.T) {
    ctx := context.Background()

    // Use temporary file for test
    dbPath := "/tmp/test-memory.libsql"
    defer os.Remove(dbPath)

    // Configure local libSQL with vector search
    cfg := dbdriver.DefaultLibSQLConfig(dbPath, true)
    db, err := dbdriver.OpenDB(ctx, cfg, nil)
    if err != nil {
        t.Fatalf("failed to open database: %v", err)
    }
    defer db.Close()

    // Verify vector search is available
    if !db.IsVectorSearchEnabled() {
        t.Fatal("vector search should be enabled")
    }

    // Open memory store
    store, err := memory.Open(ctx, "/tmp", "/tmp/cas")
    if err != nil {
        t.Fatalf("failed to open store: %v", err)
    }
    defer store.Close()

    // Enable search
    searchStore, err := store.EnableSearch(db, "test-workspace")
    if err != nil {
        t.Fatalf("failed to enable search: %v", err)
    }

    // Test BM25 search
    t.Run("BM25Search", func(t *testing.T) {
        results, err := searchStore.SearchBM25(
            ctx,
            "test query",
            "test-workspace",
            10,
        )
        if err != nil {
            t.Errorf("BM25 search failed: %v", err)
        }
        t.Logf("Found %d results", len(results))
    })

    // Test vector search
    t.Run("VectorSearch", func(t *testing.T) {
        // Create mock embedding
        embedding := make(dbdriver.Vector, 384)
        for i := range embedding {
            embedding[i] = 0.1
        }

        results, err := searchStore.SearchVector(
            ctx,
            embedding,
            "test-workspace",
            10,
        )
        if err != nil {
            t.Errorf("Vector search failed: %v", err)
        }
        t.Logf("Found %d results", len(results))
    })

    // Test hybrid search
    t.Run("HybridSearch", func(t *testing.T) {
        embedding := make(dbdriver.Vector, 384)
        for i := range embedding {
            embedding[i] = 0.1
        }

        results, err := searchStore.SearchHybrid(
            ctx,
            "test query",
            embedding,
            "test-workspace",
            10,
        )
        if err != nil {
            t.Errorf("Hybrid search failed: %v", err)
        }
        t.Logf("Found %d results", len(results))
    })
}
```

## Troubleshooting

### "vector search not available"

Make sure vector search is enabled:

```bash
export AGENTCTL_MEMORY_VECTOR_SEARCH=true
```

Or in code:

```go
cfg := dbdriver.DefaultLibSQLConfig("/tmp/test.libsql", true) // true = enable vectors
```

### File permission errors

Ensure the directory exists and is writable:

```bash
mkdir -p /tmp/test-dbs
chmod 755 /tmp/test-dbs
export AGENTCTL_MEMORY_DB_PATH=/tmp/test-dbs/memory.libsql
```

### Test databases persist between runs

Clean up test databases explicitly:

```go
func TestMain(m *testing.M) {
    code := m.Run()

    // Clean up test files
    os.Remove("/tmp/test-memory.libsql")
    os.Remove("/tmp/test-cache.libsql")
    os.Remove("/tmp/test-jobs.libsql")

    os.Exit(code)
}
```

Or use temporary directories:

```go
tmpDir, _ := os.MkdirTemp("", "foxctl-test-*")
defer os.RemoveAll(tmpDir)

cfg := dbdriver.DefaultLibSQLConfig(
    filepath.Join(tmpDir, "test.libsql"),
    true,
)
```

## Best Practices

### 1. Use Temporary Directories

```go
func setupTestDB(t *testing.T) (dbdriver.DB, func()) {
    tmpDir := t.TempDir() // Cleaned up automatically
    dbPath := filepath.Join(tmpDir, "test.libsql")

    cfg := dbdriver.DefaultLibSQLConfig(dbPath, true)
    db, err := dbdriver.OpenDB(context.Background(), cfg, nil)
    if err != nil {
        t.Fatal(err)
    }

    cleanup := func() {
        db.Close()
    }

    return db, cleanup
}
```

### 2. Parallel Tests

libSQL supports concurrent access:

```go
func TestParallel(t *testing.T) {
    t.Run("test1", func(t *testing.T) {
        t.Parallel()
        // Each test gets its own database file
        db, cleanup := setupTestDB(t)
        defer cleanup()
        // ... test code
    })

    t.Run("test2", func(t *testing.T) {
        t.Parallel()
        db, cleanup := setupTestDB(t)
        defer cleanup()
        // ... test code
    })
}
```

### 3. Table-Driven Tests

```go
func TestSearchModes(t *testing.T) {
    tests := []struct {
        name string
        mode dbdriver.SearchMode
    }{
        {"BM25", dbdriver.SearchModeBM25},
        {"Vector", dbdriver.SearchModeVector},
        {"Hybrid", dbdriver.SearchModeHybrid},
    }

    db, cleanup := setupTestDB(t)
    defer cleanup()

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test each search mode
            results, err := searchStore.Search(
                context.Background(),
                "query",
                embedding,
                "workspace",
                tt.mode,
                10,
            )
            if err != nil {
                t.Errorf("%s search failed: %v", tt.name, err)
            }
            t.Logf("%s: found %d results", tt.name, len(results))
        })
    }
}
```

## Performance Tips

- **In-memory mode**: Use `:memory:` for super-fast tests (no vector search)
- **Temp directories**: Use `/tmp` or `t.TempDir()` for faster I/O
- **Parallel tests**: libSQL supports concurrent access
- **Batch operations**: Group operations in transactions

## Migration Path

### Development → Testing → Production

```
┌─────────────┐
│ Development │  → SQLite (fast, simple)
└─────────────┘

┌─────────────┐
│   Testing   │  → libSQL (vectors, no cloud)
└─────────────┘

┌─────────────┐
│ Production  │  → Turso (cloud, scalable)
└─────────────┘
```

Your code works identically across all three!

## Summary

Local libSQL gives you the best of both worlds:

✅ **Full vector search** (just like Turso)
✅ **No cloud required** (just like SQLite)
✅ **Same API** (seamless transition)
✅ **Perfect for CI/CD** (no secrets needed)

Use it for thorough testing before deploying to Turso!

## Resources

- [libSQL Documentation](https://github.com/tursodatabase/libsql)
- [Storage Documentation](../general/storage.md)
- [Symbol Index Guide](../start/symbol_index.md)
- [Example Tests](../examples/skills_chain.md)
