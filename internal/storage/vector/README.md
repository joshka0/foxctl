# Vector Search Integration

This package provides optional vector search capabilities using [sqlite-vector](https://github.com/sqliteai/sqlite-vector) for semantic similarity search in agentctl.

## Overview

Vector search enables semantic search capabilities, allowing you to find similar memory entries based on their meaning rather than just keyword matching. This is particularly useful for:

- Semantic search across stored results
- Finding similar executions or outcomes
- Clustering related memory entries
- Recommendation systems based on context

## Requirements

Vector search is an **optional feature** that requires:

1. **CGO enabled**: `CGO_ENABLED=1`
2. **Build tag**: `-tags vector`
3. **GCC compiler**: Required for compiling CGO code
4. **sqlite-vector extension**: Pre-built binary included in `extensions/vector.so`

## Building with Vector Support

### Standard Build (without vector support)
```bash
# Default build - no vector support, no CGO required
make build
```

### Vector-Enabled Build
```bash
# Build with vector support (requires CGO and GCC)
CGO_ENABLED=1 go build -tags vector -o bin/agentctl ./cmd/agentctl
```

### Testing Vector Functionality
```bash
# Run vector tests (requires CGO)
CGO_ENABLED=1 go test -tags vector ./internal/storage/vector/...
```

## Architecture

The vector integration uses **build tags** to make it completely optional:

- `vector.go`: Default implementation (no-op, returns errors)
- `vector_cgo.go`: CGO-enabled implementation (only compiled with `-tags vector`)

This approach ensures that:
- Projects without vector support can still build with `CGO_ENABLED=0`
- The vector feature is opt-in and doesn't affect default builds
- All code remains testable in both modes

## Usage

### Initializing Vector Store

```go
import (
    "database/sql"
    "github.com/jkatigb/agentctl/internal/storage/vector"
    _ "github.com/mattn/go-sqlite3" // Required for vector support
)

// Open database with mattn/go-sqlite3 driver (not modernc.org/sqlite)
db, err := sql.Open("sqlite3", "memory.db")
if err != nil {
    log.Fatal(err)
}

// Create vector store
store, err := vector.NewStore(db, "named_memory")
if err != nil {
    log.Fatal(err)
}
defer store.Close()

// Initialize vectors (must be done after inserting embeddings)
err = store.InitializeVectors(ctx, 384, "L2")
if err != nil {
    log.Fatal(err)
}

// Optional: Quantize for 4-5x speedup
err = store.Quantize(ctx)
if err != nil {
    log.Fatal(err)
}
```

### Saving Embeddings

```go
// Generate embedding (using your preferred model)
embedding := generateEmbedding("some text content") // []float32

// Save to database
err := store.SaveEmbedding(ctx, entryID, workspace, name, embedding)
if err != nil {
    log.Fatal(err)
}
```

### Searching for Similar Entries

```go
// Generate query embedding
queryEmbedding := generateEmbedding("search query")

// Search for similar entries
results, err := store.Search(ctx, vector.SearchOptions{
    Embedding: queryEmbedding,
    Limit:     10,
    Workspace: "default",
})
if err != nil {
    log.Fatal(err)
}

for _, result := range results {
    fmt.Printf("%s (distance: %f)\n", result.Name, result.Distance)
}
```

## Distance Metrics

sqlite-vector supports multiple distance metrics:

- **L2** (Euclidean): Default, measures straight-line distance
- **L1** (Manhattan): Sum of absolute differences
- **COSINE**: Measures angle between vectors (good for normalized embeddings)
- **DOT**: Dot product similarity
- **SQUARED_L2**: Squared Euclidean distance (faster, no square root)

Specify the metric when initializing:
```go
store.InitializeVectors(ctx, 384, "COSINE")
```

## Vector Formats

The extension supports multiple precision levels:

- **FLOAT32**: Full precision (default, 4 bytes per dimension)
- **FLOAT16**: Half precision (2 bytes per dimension)
- **BFLOAT16**: Brain floating point (2 bytes per dimension)
- **INT8**: Integer quantization (1 byte per dimension)
- **UINT8**: Unsigned integer (1 byte per dimension)

Currently, the Go bindings use FLOAT32 by default.

## Performance Optimization

### Quantization

Quantization compresses vectors for faster search:

```go
// Create quantized index (required for fast searches)
err := store.Quantize(ctx)

// Optional: Preload into memory for 4-5x speedup
err := store.QuantizePreload(ctx)
```

Benefits:
- 4-5x faster searches when preloaded
- Reduced memory footprint
- Approximatequality (usually negligible impact on accuracy)

### Memory Usage

By default, sqlite-vector uses ~30MB of RAM. This can be tuned if needed for your use case.

## Limitations

1. **CGO Dependency**: Requires CGO to be enabled and a C compiler
2. **Driver Requirement**: Must use `mattn/go-sqlite3` instead of `modernc.org/sqlite`
3. **Build Complexity**: Adds compilation time and complexity
4. **Platform Support**: Extension must be compiled for each target platform

## Database Schema

The vector integration adds an `embedding` column to the `named_memory` table:

```sql
ALTER TABLE named_memory ADD COLUMN embedding BLOB DEFAULT NULL;
```

This column stores vector embeddings as binary BLOBs in FLOAT32 format.

## Extension Location

The sqlite-vector extension is expected at:
```
<project-root>/extensions/vector.so  (Linux)
<project-root>/extensions/vector.dylib  (macOS)
<project-root>/extensions/vector.dll  (Windows)
```

The extension is automatically loaded when the vector store is initialized.

## Licensing

**Important**: sqlite-vector is licensed under the [Elastic License 2.0](https://github.com/sqliteai/sqlite-vector/blob/main/LICENSE.md).

- ✅ Free for non-production use
- ✅ Free for open-source projects
- ❌ Requires commercial license for production/managed service use

For production use, contact [SQLite Cloud, Inc](mailto:info@sqlitecloud.io) for licensing.

## Troubleshooting

### "vector: not available in this build"

You're trying to use vector functionality but the binary wasn't built with vector support.

**Solution**: Rebuild with `CGO_ENABLED=1 go build -tags vector`

### "Failed to load extension"

The sqlite-vector extension file wasn't found.

**Solution**: Ensure `extensions/vector.so` exists in the project root.

### "CGO_ENABLED=0 but cgo required"

You're trying to build with vector support but CGO is disabled.

**Solution**: Set `CGO_ENABLED=1` before building.

### "gcc: command not found"

CGO requires a C compiler.

**Solution**: Install GCC:
- Ubuntu/Debian: `sudo apt-get install build-essential`
- macOS: `xcode-select --install`
- Windows: Install MinGW or use WSL

## References

- [sqlite-vector GitHub](https://github.com/sqliteai/sqlite-vector)
- [sqlite-vector API Documentation](https://github.com/sqliteai/sqlite-vector/blob/main/API.md)
- [Quantization Details](https://github.com/sqliteai/sqlite-vector/blob/main/QUANTIZATION.md)
