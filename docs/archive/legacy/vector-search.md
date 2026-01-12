# Vector Search in agentctl

## Overview

agentctl includes optional vector search capabilities powered by [sqlite-vector](https://github.com/sqliteai/sqlite-vector), enabling semantic similarity search across stored memory entries.

**Note**: This integration uses the native C extension via CGO. The WASM version (`@sqliteai/sqlite-wasm`) is designed for JavaScript/browser environments and is not compatible with Go. See [implementation notes](./vector-search-implementation-notes.md) for details on the evaluation.

## What is Vector Search?

Vector search allows you to find semantically similar content by representing text, images, or other data as numerical vectors (embeddings) and computing distances between them. Unlike traditional keyword search, vector search understands meaning and context.

### Use Cases

- **Semantic Memory Search**: Find relevant past executions based on meaning, not just keywords
- **Context Retrieval**: Retrieve similar execution contexts for AI agents
- **Skill Discovery**: Find related skills or tools based on descriptions
- **Anomaly Detection**: Identify unusual patterns in execution history

## Quick Start

### 1. Build with Vector Support

Vector search requires CGO and must be explicitly enabled:

```bash
# Set CGO_ENABLED and build with vector tag
CGO_ENABLED=1 go build -tags vector -o bin/agentctl ./cmd/agentctl
```

### 2. Prerequisites

- **GCC or Clang**: C compiler for CGO
- **sqlite-vector extension**: Included in `extensions/vector.so`
- **Go 1.22.5 or later**

### 3. Using Vector Search

```go
// Example: Semantic search in memory entries
import "github.com/jkatigb/agentctl/internal/storage/vector"

// Check if vector support is enabled
if vector.Enabled {
    // Create vector store
    store, err := vector.NewStore(db, "named_memory")

    // Initialize vectors with your embeddings' dimension and distance metric
    err = store.InitializeVectors(ctx, 384, "COSINE")

    // Perform semantic search
    results, err := store.Search(ctx, vector.SearchOptions{
        Embedding: queryVector,
        Limit:     10,
        Workspace: "default",
    })
}
```

## Building for Different Platforms

### Linux
```bash
CGO_ENABLED=1 go build -tags vector -o bin/agentctl ./cmd/agentctl
```

### macOS
```bash
# Ensure Xcode command line tools are installed
xcode-select --install

CGO_ENABLED=1 go build -tags vector -o bin/agentctl ./cmd/agentctl
```

### Windows
```bash
# Requires MinGW or similar C compiler
CGO_ENABLED=1 go build -tags vector -o bin/agentctl.exe ./cmd/agentctl
```

### Cross-Compilation

Cross-compiling with CGO is complex. Consider:
- Building natively on each platform
- Using Docker for Linux builds
- Using GitHub Actions for multi-platform releases

## Performance

### Benchmarks

With quantization enabled:
- **Query Speed**: ~1-5ms for 10K vectors (384 dimensions)
- **Memory Usage**: ~30MB default (configurable)
- **Speedup**: 4-5x with quantization preload

### Optimization Tips

1. **Use Quantization**: Essential for good performance
   ```go
   store.Quantize(ctx)
   store.QuantizePreload(ctx) // Preload for max speed
   ```

2. **Choose Appropriate Distance Metric**:
   - **COSINE**: Best for normalized embeddings (recommended)
   - **L2**: Good general-purpose metric
   - **DOT**: Fast when embeddings are already normalized

3. **Batch Operations**: Insert embeddings in transactions for better throughput

## Integration with Memory Store

The memory store automatically includes an `embedding` column when built with vector support:

```go
type NamedEntry struct {
    ID        string
    Name      string
    Workspace string
    Summary   string
    Result    []byte
    Embedding []float32  // Vector embedding (nullable)
    // ... other fields
}
```

## Production Considerations

### Licensing

⚠️ **Important**: sqlite-vector uses the Elastic License 2.0

- **Free** for development, testing, and non-commercial use
- **Free** for open-source projects
- **Commercial license required** for production/managed services

Contact [SQLite Cloud, Inc](mailto:info@sqlitecloud.io) for commercial licensing.

### Alternative: No-CGO Vector Search

If CGO is not acceptable for your deployment, consider:

1. **External Vector Database**: Use Qdrant, Chroma, Pinecone, or similar
2. **Pure Go Implementation**: Implement basic similarity search in pure Go (slower but no CGO)
3. **sqlite-vec Alternative**: Use [sqlite-vec](https://github.com/asg017/sqlite-vec) with `ncruces/go-sqlite3` (WASM-based, no CGO, MIT licensed)

See [implementation notes](./vector-search-implementation-notes.md) for detailed comparison.

### Deployment

When deploying with vector support:

1. **Include Extension**: Ensure `extensions/vector.so` is included in your deployment
2. **Set CGO_ENABLED=1**: Required during build
3. **Install C Toolchain**: Build environment needs GCC/Clang
4. **Test Thoroughly**: Vector builds have more dependencies

### Monitoring

Monitor these metrics in production:
- Query latency (should be <10ms for typical workloads)
- Memory usage (starts ~30MB, grows with dataset)
- Index rebuild time (after large updates)

## Disabling Vector Support

To build without vector support (default):

```bash
# No CGO required
CGO_ENABLED=0 go build -o bin/agentctl ./cmd/agentctl
```

The codebase gracefully handles missing vector support:
```go
if !vector.Enabled {
    // Fall back to traditional search methods
}
```

## Troubleshooting

### Build Errors

**Error**: `vector: not available in this build`
- **Cause**: Binary wasn't built with `-tags vector`
- **Fix**: Rebuild with `CGO_ENABLED=1 go build -tags vector`

**Error**: `failed to load extension`
- **Cause**: Extension file not found
- **Fix**: Ensure `extensions/vector.so` exists

**Error**: `undefined reference to ...`
- **Cause**: CGO not enabled or C compiler missing
- **Fix**: Install GCC and set `CGO_ENABLED=1`

### Runtime Errors

**Error**: `dimension mismatch`
- **Cause**: Query vector dimensions don't match stored vectors
- **Fix**: Ensure all embeddings have the same dimension

**Error**: `table not initialized`
- **Cause**: `InitializeVectors()` not called
- **Fix**: Call `InitializeVectors()` before searching

## Examples

### Complete Example: Semantic Search

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "log"

    "github.com/jkatigb/agentctl/internal/storage/vector"
    _ "github.com/mattn/go-sqlite3"
)

func main() {
    // Open database with sqlite3 driver
    db, err := sql.Open("sqlite3", "memory.db")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Initialize vector store
    store, err := vector.NewStore(db, "named_memory")
    if err != nil {
        log.Fatal(err)
    }
    defer store.Close()

    ctx := context.Background()

    // Initialize with 384-dimensional embeddings using cosine distance
    if err := store.InitializeVectors(ctx, 384, "COSINE"); err != nil {
        log.Fatal(err)
    }

    // Quantize for performance
    if err := store.Quantize(ctx); err != nil {
        log.Fatal(err)
    }

    // Search for similar entries
    queryEmbedding := getEmbedding("How do I deploy to production?")
    results, err := store.Search(ctx, vector.SearchOptions{
        Embedding: queryEmbedding,
        Limit:     5,
        Workspace: "default",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Top 5 similar entries:")
    for i, result := range results {
        fmt.Printf("%d. %s (similarity: %.3f)\n",
            i+1, result.Name, 1-result.Distance)
    }
}

func getEmbedding(text string) []float32 {
    // Implement your embedding generation here
    // (e.g., using OpenAI, sentence-transformers, etc.)
    return make([]float32, 384)
}
```

## Future Enhancements

Potential future improvements:
- Automatic embedding generation from memory content
- Multi-vector support (store multiple embeddings per entry)
- Hybrid search (combine keyword + vector search)
- Alternative backends (Chroma, Qdrant, etc.)

## References

- [sqlite-vector Documentation](https://github.com/sqliteai/sqlite-vector)
- [sqlite-vector API Reference](https://github.com/sqliteai/sqlite-vector/blob/main/API.md)
- [sqlite-wasm (JavaScript/Browser)](https://github.com/sqliteai/sqlite-wasm) - Not Go-compatible
- [Implementation Evaluation Notes](./vector-search-implementation-notes.md)
- [Vector Search Fundamentals](https://www.pinecone.io/learn/vector-search/)
- [Build Tags in Go](https://pkg.go.dev/cmd/go#hdr-Build_constraints)
