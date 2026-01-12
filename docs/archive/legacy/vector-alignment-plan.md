# Vector Search Alignment Plan

## Current State

### Main Branch (libSQL/Turso Native Vectors)
- **Location**: `internal/storage/dbdriver/`
- **Approach**: Native libSQL vector support via `github.com/tursodatabase/go-libsql`
- **Column Type**: `F32_BLOB(dimensions)`
- **Functions**: `vector()`, `vector_distance_cos()`, `vector_top_k()`
- **Indexing**: `libsql_vector_idx()` with DiskANN algorithm
- **CGO Required**: Yes (for libSQL driver)
- **Build Tags**: Uses `//go:build cgo` and `//go:build !cgo`

### Current Branch (sqlite-vector Extension)
- **Location**: `internal/storage/vector/`
- **Approach**: sqlite-vector extension via `github.com/mattn/go-sqlite3`
- **Column Type**: `BLOB` (with `vector_init()`)
- **Functions**: `vector_init()`, `vector_quantize()`, `vector_quantize_scan()`
- **Indexing**: Quantization-based indexing
- **CGO Required**: Yes (for mattn/go-sqlite3)
- **Build Tags**: Uses `//go:build vector && cgo`

## Key Differences

| Feature | libSQL/Turso (Main) | sqlite-vector (Branch) |
|---------|---------------------|------------------------|
| **Provider** | Turso/libSQL native | SQLite Cloud extension |
| **License** | Apache 2.0 (libSQL) | Elastic License 2.0 |
| **Backend** | Cloud + Local | Local only |
| **Column Type** | F32_BLOB(n) | BLOB + initialization |
| **Insert Syntax** | `vector('[1,2,3]')` | Serialize to BLOB |
| **Search** | `vector_top_k()` | `vector_quantize_scan()` |
| **Use Case** | Cloud deployment | Standalone SQLite |

## Alignment Strategy

### Option 1: Merge into dbdriver Abstraction (Recommended)

Add sqlite-vector as another driver option in the dbdriver layer:

```
internal/storage/dbdriver/
├── sqlite.go           # Standard SQLite (no vectors)
├── sqlite_vector.go    # SQLite + sqlite-vector extension (new)
├── libsql.go           # Local libSQL (native vectors)
├── turso.go            # Cloud libSQL (native vectors)
└── vector.go           # Unified vector helper interface
```

**Benefits**:
- Consistent API across all backends
- Users can choose: SQLite, SQLite+vector, LibSQL, or Turso
- Unified configuration via environment variables
- Clean separation of concerns

**Implementation**:
1. Move `internal/storage/vector/` logic into `internal/storage/dbdriver/sqlite_vector.go`
2. Extend `VectorHelper` to support both libSQL and sqlite-vector APIs
3. Add `DriverSQLiteVector` driver type
4. Update config to support sqlite-vector configuration

### Option 2: Keep Separate (Current Approach)

Maintain two separate vector implementations:
- `dbdriver` for libSQL/Turso
- `storage/vector` for sqlite-vector

**Benefits**:
- Clear separation between cloud and local approaches
- No mixing of different APIs
- Easier to maintain independently

**Drawbacks**:
- Duplicate functionality
- Confusing for users (two ways to do vectors)
- Different APIs for similar features

### Option 3: Adapter Pattern

Create an adapter that maps sqlite-vector API to libSQL API:

```go
type VectorAdapter interface {
    CreateColumn(table, column string, dims int) error
    InsertVector(vector []float32) string // SQL expression
    SearchSimilar(query Vector, k int) (*sql.Rows, error)
}

// Implementations:
type LibSQLVectorAdapter struct { ... }
type SQLiteVectorAdapter struct { ... }
```

## Recommended Approach

**Merge into dbdriver** (Option 1) for these reasons:

1. **Unified User Experience**: Single configuration system
2. **Flexibility**: Users choose backend based on needs:
   - Local dev: SQLite or LibSQL
   - Production: Turso cloud
   - Privacy-focused: sqlite-vector (no cloud)
3. **Consistent API**: One VectorHelper interface
4. **Future-proof**: Easy to add more backends

## Implementation Steps

### 1. Extend Driver Types
```go
const (
    DriverSQLite       DriverType = "sqlite"        // No vectors
    DriverSQLiteVector DriverType = "sqlite-vector" // With extension
    DriverLibSQL       DriverType = "libsql"        // Native vectors
    DriverTurso        DriverType = "turso"         // Cloud vectors
)
```

### 2. Create SQLiteVector Config
```go
type SQLiteVectorConfig struct {
    Path              string
    ExtensionPath     string // Path to vector.so
    VectorDimensions  int
    DistanceMetric    string // L2, COSINE, etc.
}
```

### 3. Implement sqlite-vector Driver
```go
//go:build cgo

package dbdriver

import "github.com/mattn/go-sqlite3"

func openSQLiteVector(ctx context.Context, cfg SQLiteVectorConfig, migrate MigrationFunc) (DB, error) {
    // Load extension, initialize vectors, etc.
}
```

### 4. Extend VectorHelper
```go
type VectorHelper struct {
    db          DB
    dimensions  int
    backend     VectorBackend
}

type VectorBackend interface {
    InsertVectorSQL(v Vector) string
    SearchSQL(indexName string, v Vector, k int) string
    CreateIndex(table, column, index string) error
}
```

### 5. Update Documentation
- Expand `docs/vector-search.md` to cover all backends
- Update `docs/turso-migration.md` to include sqlite-vector comparison
- Add decision guide: "Which vector backend should I use?"

## Migration Path

For users of this branch:
1. Merge changes to use new dbdriver approach
2. Update config to use `driver: sqlite-vector`
3. Extension path automatically detected from `extensions/vector.so`

## Benefits of Alignment

✅ Single configuration system
✅ Consistent API across backends
✅ Choose best backend for use case
✅ Cloud-ready with Turso
✅ Privacy-friendly with sqlite-vector
✅ Easy to test and maintain

## Next Steps

1. Review this plan with team/user
2. Implement sqlite-vector in dbdriver
3. Update configuration system
4. Update all documentation
5. Add integration tests for all backends
6. Create migration guide
