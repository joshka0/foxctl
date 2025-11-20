# Vector Search Implementation Notes

## Integration Approaches Evaluated

### ✅ Implemented: CGO with Native Extension

**Status**: Implemented and working

The current implementation uses the native `sqlite-vector` extension (from sqliteai) loaded via `mattn/go-sqlite3`:

- **Package**: `@sqliteai/sqlite-vector` (native extension)
- **Build**: `CGO_ENABLED=1 go build -tags vector`
- **Performance**: Best (native C with SIMD)
- **Portability**: Requires CGO + C compiler
- **License**: Elastic License 2.0

### ❌ Not Feasible: WASM Integration with Go

**Status**: Not implemented (technical limitations)

The `@sqliteai/sqlite-wasm` package was evaluated but is **not suitable for Go integration**:

**Why it doesn't work:**
1. **JavaScript-only API**: The WASM binary expects JavaScript host functions and runtime environment
2. **Browser/Node.js Target**: Designed for web browsers and Node.js, not Go
3. **Complex Glue Code**: Would require reimplementing entire JavaScript API layer in Go
4. **Memory Model Mismatch**: JavaScript-centric memory management

**What it provides:**
- Pre-compiled SQLite with sqlite-vector as a WASM module
- JavaScript wrapper for browser/Node.js use
- OPFS (Origin Private File System) support for browsers

**Conclusion**: The WASM version is excellent for JavaScript applications but incompatible with Go's ecosystem.

### 🤔 Alternative Considered: sqlite-vec with ncruces/go-sqlite3

**Status**: Evaluated but not implemented

`sqlite-vec` (by asg017) has dedicated Go WASM bindings:

**Pros:**
- Works without CGO using wazero runtime
- Proper Go bindings (`github.com/asg017/sqlite-vec-go-bindings/ncruces`)
- MIT/Apache 2.0 licensed (more permissive)
- Similar functionality to sqlite-vector

**Cons:**
- Different project than sqlite-vector (user requested sqlite-vector specifically)
- Would require using `ncruces/go-sqlite3` instead of `modernc.org/sqlite`
- Virtual table approach vs. BLOB approach

**Could be implemented if needed**, but user specifically requested sqlite-vector.

## Current Solution

The project now supports **optional vector search** via:

1. **Default Build** (No Vector Support):
   ```bash
   CGO_ENABLED=0 go build ./cmd/agentctl
   ```
   - Uses `modernc.org/sqlite` (pure Go)
   - No vector search capabilities
   - Zero CGO dependencies

2. **Vector-Enabled Build** (Requires CGO):
   ```bash
   CGO_ENABLED=1 go build -tags vector ./cmd/agentctl
   ```
   - Uses `mattn/go-sqlite3` with sqlite-vector extension
   - Full vector search capabilities
   - Requires GCC/Clang compiler

## Distribution Formats of sqlite-vector

For reference, sqlite-vector is distributed in multiple formats:

| Platform | Package | Integration Method |
|----------|---------|-------------------|
| **Go (CGO)** | Native `.so`/`.dylib`/`.dll` | ✅ Implemented |
| **Python** | `pip install sqliteai-vector` | PyPI package |
| **Android** | `ai.sqlite:vector:0.9.52` | Gradle dependency |
| **Node.js** | `npm install @sqliteai/sqlite-vector` | NPM package |
| **WASM/Browser** | `npm install @sqliteai/sqlite-wasm` | ❌ Not Go-compatible |
| **iOS** | Swift Package | Swift Package Manager |

## Recommendations

### For Production Use

**If CGO is acceptable:**
- ✅ Use the current implementation with `-tags vector`
- Best performance and native integration

**If CGO is NOT acceptable:**
- ❌ Vector search not currently available
- Consider:
  - Using an external vector database (Qdrant, Chroma, Pinecone)
  - Implementing pure Go similarity search (slower, but no CGO)
  - Using sqlite-vec with ncruces/go-sqlite3 (different project)

### Future Possibilities

1. **Pure Go Vector Search**: Implement basic cosine/euclidean similarity in Go
   - No CGO required
   - Slower than native C
   - Simple brute-force search for small datasets

2. **External Vector DB**: Integrate with dedicated vector databases
   - Best for large-scale deployments
   - Network dependency
   - More operational complexity

3. **sqlite-vec Alternative**: Switch to asg017's sqlite-vec with WASM bindings
   - No CGO via ncruces/go-sqlite3
   - Different API from sqlite-vector
   - Requires changing SQLite driver

## Technical Deep Dive: Why WASM Doesn't Work

The `@sqliteai/sqlite-wasm` package structure:

```
@sqliteai/sqlite-wasm/
├── index.mjs           # JavaScript entry point
├── sqlite3.wasm        # Compiled SQLite + vector extension
├── sqlite3.js          # JavaScript glue code
└── sqlite3-worker1.js  # Web Worker implementation
```

The WASM binary expects:
- JavaScript host functions (memory allocators, file system APIs)
- JavaScript callbacks for SQL execution
- Browser/Node.js environment variables

Go's wazero runtime would need to:
- Implement ~50+ JavaScript host functions
- Manage complex memory sharing
- Emulate JavaScript async patterns
- Handle JavaScript-specific data types

**Effort estimate**: 2-3 weeks of development + ongoing maintenance
**Benefit**: Questionable (native extension works better)
**Verdict**: Not worth the complexity

## References

- [sqlite-vector GitHub](https://github.com/sqliteai/sqlite-vector)
- [sqlite-wasm GitHub](https://github.com/sqliteai/sqlite-wasm)
- [sqlite-vec (alternative)](https://github.com/asg017/sqlite-vec)
- [ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3) - Go WASM SQLite
- [wazero](https://wazero.io/) - WebAssembly runtime for Go
