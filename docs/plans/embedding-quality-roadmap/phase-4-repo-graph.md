# Phase 4: Repo Graph Index v1 SQLite DAG Store

> **Status**: Planning
> **Dependencies**: None (can start immediately)
> **Estimated PRs**: 4
> **Priority**: High - Enables relationship-based navigation

## Overview

Phase 4 establishes the core infrastructure for a queryable repo graph index stored in SQLite. This enables navigation from any code element to related elements via containment, import, call, and reference edges.

**Goals:**
- Persist a directed acyclic graph (DAG) of code relationships
- Support FTS5 full-text search over nodes
- Enable multi-hop expansion from seed nodes
- Provide incremental rebuild for fast iteration

**Non-Goals (this phase):**
- Comment-driven edges (Phase 5)
- GUI visualization (Phase 7)
- Cross-repository linking

---

## PR 4.1: Repo Graph SQLite Store + Schema

### Summary

Create the foundational SQLite store with proper schema, migrations, and pragmas for the repo graph index. This PR establishes the database layer that all subsequent PRs build upon.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/intelligence/indexing/repoindex/store/store.go` | Create | Core store struct, Open/Close, CRUD operations |
| `internal/intelligence/indexing/repoindex/store/schema.go` | Create | Schema definitions, table DDL |
| `internal/intelligence/indexing/repoindex/store/migrations.go` | Create | Migration runner, version tracking |
| `internal/intelligence/indexing/repoindex/store/store_test.go` | Create | Unit tests for store operations |

**Note**: The existing `internal/intelligence/indexing/repoindex/store.go` will be refactored into the `store/` subpackage for cleaner separation.

### Schema Definition

```sql
-- nodes: All graph nodes (packages, files, symbols, concepts)
CREATE TABLE nodes (
    id          TEXT PRIMARY KEY,           -- namespaced: <repo_key>::<kind>:<rest> (e.g. "rk::pkg:import/path")
    kind        TEXT NOT NULL,              -- "package", "file", "symbol", "concept"
    pkg         TEXT,                       -- Package path (null for concepts)
    file        TEXT,                       -- Relative file path within package
    name        TEXT,                       -- Symbol/package/file name
    signature   TEXT,                       -- Function/method signature
    span_start  INTEGER DEFAULT 0,          -- 1-based start line
    span_end    INTEGER DEFAULT 0,          -- 1-based end line
    exported    INTEGER DEFAULT 0,          -- 1=exported, 0=unexported
    doc         TEXT,                       -- GoDoc/JSDoc content
    summary     TEXT,                       -- LLM-generated summary
    meta_json   TEXT,                       -- Additional metadata (Index: block parsed)
    hash        TEXT,                       -- Content hash for change detection
    repo_key    TEXT NOT NULL,              -- Workspace identifier (also used for filtering)
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    CHECK (substr(id, 1, length(repo_key)+2) = repo_key || '::')
);

-- edges: Directed relationships between nodes
CREATE TABLE edges (
    src         TEXT NOT NULL,              -- Source node ID (namespaced)
    dst         TEXT NOT NULL,              -- Destination node ID (namespaced)
    type        TEXT NOT NULL,              -- "CONTAINS", "IMPORTS", "CALLS", etc.
    weight      REAL DEFAULT 1.0,           -- Edge weight (1.0=hard, <1.0=soft/comment)
    meta_json   TEXT,                       -- Edge metadata (call site info, etc.)
    repo_key    TEXT NOT NULL,              -- Redundant but useful for filtering
    CHECK (substr(src, 1, length(repo_key)+2) = repo_key || '::'),
    CHECK (substr(dst, 1, length(repo_key)+2) = repo_key || '::'),
    PRIMARY KEY (src, dst, type, repo_key),
    FOREIGN KEY (src) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (dst) REFERENCES nodes(id) ON DELETE CASCADE
);

-- pkg_state: Track per-package state for incremental rebuilds
CREATE TABLE pkg_state (
    pkg         TEXT NOT NULL,
    repo_key    TEXT NOT NULL,
    files_hash  TEXT NOT NULL,              -- Hash of all file contents in package
    indexed_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (pkg, repo_key)
);

-- index_meta: Global index metadata
CREATE TABLE index_meta (
    repo_key        TEXT PRIMARY KEY,
    repo_root       TEXT NOT NULL,
    head_sha        TEXT,
    schema_version  INTEGER NOT NULL,
    indexed_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for common queries
CREATE INDEX idx_nodes_kind ON nodes(kind, repo_key);
CREATE INDEX idx_nodes_pkg ON nodes(pkg, repo_key);
CREATE INDEX idx_nodes_file ON nodes(file, repo_key);
CREATE INDEX idx_edges_src ON edges(src, repo_key);
CREATE INDEX idx_edges_dst ON edges(dst, repo_key);
CREATE INDEX idx_edges_type ON edges(type, repo_key);

-- FTS5 for full-text search (optional, created on demand)
CREATE VIRTUAL TABLE IF NOT EXISTS node_fts USING fts5(
    id UNINDEXED,
    name,
    doc,
    summary,
    content=nodes,
    content_rowid=rowid
);

-- Triggers to keep FTS in sync
CREATE TRIGGER nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO node_fts(rowid, id, name, doc, summary)
    VALUES (new.rowid, new.id, new.name, new.doc, new.summary);
END;

CREATE TRIGGER nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO node_fts(node_fts, rowid, id, name, doc, summary)
    VALUES ('delete', old.rowid, old.id, old.name, old.doc, old.summary);
END;

CREATE TRIGGER nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO node_fts(node_fts, rowid, id, name, doc, summary)
    VALUES ('delete', old.rowid, old.id, old.name, old.doc, old.summary);
    INSERT INTO node_fts(rowid, id, name, doc, summary)
    VALUES (new.rowid, new.id, new.name, new.doc, new.summary);
END;
```

**Repo-scoped FTS query:**
```sql
SELECT n.*
FROM node_fts f
JOIN nodes n ON n.rowid = f.rowid
WHERE f MATCH ? AND n.repo_key = ?
LIMIT ?;
```

### Node ID Spec (v1)

Node IDs are **repo-scoped** by prefixing with `repo_key`:

- `<repo_key>::pkg:<importPath>`
- `<repo_key>::file:<repoRelPath>`
- `<repo_key>::sym:<importPath>::<repoRelPath>::<name>::<sigHash?>`
- `<repo_key>::kw:<token>`
- `<repo_key>::event:<name>`

All APIs that accept a node ID expect the namespaced form.

`repo_key` is redundant with namespaced IDs, but retained for fast filtering and indexing. If we keep `repo_key` columns, enforce consistency with CHECK constraints:

```sql
CHECK (substr(id, 1, length(repo_key)+2) = repo_key || '::')
CHECK (substr(src, 1, length(repo_key)+2) = repo_key || '::')
CHECK (substr(dst, 1, length(repo_key)+2) = repo_key || '::')
```

### Implementation Details

**Store struct:**
```go
package store

type Store struct {
    db      *sql.DB
    repoKey string
    mu      sync.RWMutex
}

type Options struct {
    DBPath      string
    RepoRoot    string
    EnableFTS   bool  // Create FTS table
    BusyTimeout time.Duration
}

func Open(ctx context.Context, opts Options) (*Store, error)
func (s *Store) Close() error

// Node operations
func (s *Store) UpsertNode(ctx context.Context, node Node) error
func (s *Store) UpsertNodes(ctx context.Context, nodes []Node) error
func (s *Store) GetNode(ctx context.Context, id string) (Node, error)
func (s *Store) DeleteNode(ctx context.Context, id string) error
func (s *Store) DeleteNodesByPkg(ctx context.Context, pkg string) error

// Edge operations
func (s *Store) UpsertEdge(ctx context.Context, edge Edge) error
func (s *Store) UpsertEdges(ctx context.Context, edges []Edge) error
func (s *Store) DeleteEdgesByNode(ctx context.Context, nodeID string) error
func (s *Store) DeleteEdgesByPkg(ctx context.Context, pkg string) error

// Package state
func (s *Store) GetPkgState(ctx context.Context, pkg string) (PkgState, error)
func (s *Store) SetPkgState(ctx context.Context, state PkgState) error

// Index meta
func (s *Store) GetMeta(ctx context.Context) (IndexMeta, error)
func (s *Store) SetMeta(ctx context.Context, meta IndexMeta) error

// Stats
func (s *Store) Stats(ctx context.Context) (Stats, error)
```

**PRAGMAs (in Open):**
```go
pragmas := []string{
    "PRAGMA journal_mode=WAL",
    "PRAGMA synchronous=NORMAL",
    "PRAGMA busy_timeout=5000",
    "PRAGMA foreign_keys=ON",
    "PRAGMA cache_size=-64000", // 64MB cache
}
```

### Testing Strategy

1. **Unit tests** (`store_test.go`):
   - Test Open/Close with temp database
   - Test node CRUD operations
   - Test edge CRUD operations
   - Test FTS search returns expected results
   - Test cascade deletes work correctly
   - Test concurrent access safety

2. **Migration tests**:
   - Test fresh database creates all tables
   - Test migration from v0 to v1 schema

### Acceptance Criteria

- [ ] Store opens database with WAL mode and proper pragmas
- [ ] Schema version is tracked and migrations run on open
- [ ] Node IDs are repo-scoped (namespaced) to avoid collisions
- [ ] Nodes can be inserted, updated, queried, deleted
- [ ] Edges respect foreign key constraints
- [ ] FTS queries are scoped to repo_key (or ID prefix)
- [ ] `pkg_state` tracks per-package hashes
- [ ] All tests pass with race detector enabled

---

## PR 4.2: Builder Layer A (Containment + Imports)

### Summary

Implement the graph builder that populates nodes and edges for packages, files, and symbols. This PR focuses on containment edges (pkg→file, file→symbol) and import edges (pkg→pkg).

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/intelligence/indexing/repoindex/builder/builder.go` | Create | Main Builder struct and orchestration |
| `internal/intelligence/indexing/repoindex/builder/nodes.go` | Create | Node extraction from AST |
| `internal/intelligence/indexing/repoindex/builder/edges.go` | Create | Edge extraction (CONTAINS, IMPORTS) |
| `internal/intelligence/indexing/repoindex/builder/builder_test.go` | Create | Integration tests |

**Note**: Existing `builder.go` in repoindex root will be refactored/migrated.

### Implementation Details

**Builder struct:**
```go
package builder

type Builder struct {
    store     *store.Store
    opts      BuildOptions
    symIndex  *symbol.Store  // Existing symbol index for enrichment
    fileSumm  FileSummaryProvider
    symSumm   SymbolSummaryProvider
}

type BuildOptions struct {
    RepoRoot          string
    Patterns          []string   // Glob patterns
    IncludeGo         bool
    IncludeTypeScript bool
    IncludeElixir     bool
    IncludeTests      bool
    DryRun            bool
    ProgressFn        func(phase string, current, total int)
}

func New(store *store.Store, opts BuildOptions) *Builder
func (b *Builder) Build(ctx context.Context) (BuildResult, error)
func (b *Builder) BuildPackage(ctx context.Context, pkg string) error
```

**Node extraction (nodes.go):**
```go
// extractPackageNode creates a package node from go/packages
func extractPackageNode(repoKey string, pkg *packages.Package) Node {
    return Node{
        ID:   PackageID(repoKey, pkg.PkgPath),
        Kind: NodePackage,
        Pkg:  pkg.PkgPath,
        Name: path.Base(pkg.PkgPath),
        Doc:  extractPackageDoc(pkg),
    }
}

// extractFileNode creates a file node
func extractFileNode(repoKey string, pkg *packages.Package, file *ast.File, filename string) Node {
    return Node{
        ID:        FileID(repoKey, filename),
        Kind:      NodeFile,
        Pkg:       pkg.PkgPath,
        File:      filename,
        Name:      filename,
        SpanStart: 1,
        SpanEnd:   fset.Position(file.End()).Line,
    }
}

// extractSymbolNodes creates symbol nodes from declarations
func extractSymbolNodes(pkg *packages.Package, file *ast.File, filename string) []Node
```

**Edge extraction (edges.go):**
```go
// extractContainsEdges creates pkg→file and file→symbol edges
func extractContainsEdges(repoKey string, pkg *packages.Package, fileNodes []Node, symbolNodes []Node) []Edge {
    var edges []Edge
    
    pkgID := PackageID(repoKey, pkg.PkgPath)
    
    // pkg → file
    for _, fn := range fileNodes {
        edges = append(edges, Edge{
            Src:    pkgID,
            Dst:    fn.ID,
            Type:   EdgeContains,
            Weight: 1.0,
        })
    }
    
    // file → symbol
    for _, sn := range symbolNodes {
        fileID := FileID(repoKey, sn.File)
        edges = append(edges, Edge{
            Src:    fileID,
            Dst:    sn.ID,
            Type:   EdgeContains,
            Weight: 1.0,
        })
    }
    
    return edges
}

// extractImportEdges creates pkg→pkg edges for imports
func extractImportEdges(repoKey string, pkg *packages.Package) []Edge {
    var edges []Edge
    
    pkgID := PackageID(repoKey, pkg.PkgPath)
    
    for _, imp := range pkg.Imports {
        edges = append(edges, Edge{
            Src:    pkgID,
            Dst:    PackageID(repoKey, imp.PkgPath),
            Type:   EdgeImports,
            Weight: 1.0,
        })
    }
    
    return edges
}
```

**TypeScript support:**
- Reuse existing `tsconfig.go` and `tsimports.go` for TS parsing
- Extract symbols via ts-morph or tree-sitter (existing infra)
- Create same node/edge types

### Testing Strategy

1. **Unit tests** for node/edge extraction functions
2. **Integration test** with small Go package:
   - Build graph for testdata package
   - Verify expected nodes exist
   - Verify CONTAINS edges form proper hierarchy
   - Verify IMPORTS edges match `import` statements

3. **TypeScript test**:
   - Build graph for testdata TS project
   - Verify file→symbol containment

### Acceptance Criteria

- [ ] Builder processes Go packages via `go/packages`
- [ ] Package, file, symbol nodes created with proper IDs
- [ ] CONTAINS edges form pkg→file→symbol hierarchy
- [ ] IMPORTS edges created for all package imports
- [ ] TypeScript packages processed (if enabled)
- [ ] Summary providers integrated (if provided)
- [ ] Progress callback invoked during build
- [ ] DryRun mode logs but doesn't write

---

## PR 4.3: Query Engine v1

### Summary

Implement the query layer for searching and expanding the repo graph. Add CLI commands for interactive use.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/intelligence/indexing/repoindex/query/query.go` | Create | Core query functions |
| `internal/intelligence/indexing/repoindex/query/expand.go` | Create | Graph expansion algorithm |
| `internal/intelligence/indexing/repoindex/query/query_test.go` | Create | Query tests |
| `cmd/agentctl/cmd/repoindex.go` | Create | CLI commands |
| `cmd/agentctl/cmd/root.go` | Modify | Register repoindex command |

### Implementation Details

**Query functions (query.go):**
```go
package query

type Engine struct {
    store *store.Store
}

func New(store *store.Store) *Engine

// SearchFTS performs full-text search over nodes
func (e *Engine) SearchFTS(ctx context.Context, query string, opts SearchOptions) ([]Node, error)

type SearchOptions struct {
    Kinds  []NodeKind  // Filter by kind
    Limit  int         // Max results (default 50)
    Offset int         // Pagination
}

// Open retrieves a single node by ID
func (e *Engine) Open(ctx context.Context, id string) (Node, error)

// GetEdges returns edges for a node
func (e *Engine) GetEdges(ctx context.Context, nodeID string, opts EdgeOptions) ([]Edge, error)

type EdgeOptions struct {
    Types     []EdgeType
    Direction Direction  // "out" or "in"
}

// Expand performs multi-hop graph traversal from seed nodes
func (e *Engine) Expand(ctx context.Context, seeds []string, opts ExpandOptions) (ExpandResult, error)
```

**Expand algorithm (expand.go):**
```go
type ExpandOptions struct {
    EdgeTypes  []EdgeType  // Edge types to follow
    Direction  Direction   // "out", "in", or "both"
    Depth      int         // Max hops (default 1)
    Budget     int         // Max total nodes to return
    PerNodeCap int         // Max edges per node
}

type ExpandResult struct {
    Nodes []Node   `json:"nodes"`
    Edges []Edge   `json:"edges"`
    Trail []string `json:"trail,omitempty"` // "seed → edge → node" for debugging
}

func (e *Engine) Expand(ctx context.Context, seeds []string, opts ExpandOptions) (ExpandResult, error) {
    visited := make(map[string]bool)
    var result ExpandResult
    
    // BFS from seeds
    queue := seeds
    for depth := 0; depth < opts.Depth && len(queue) > 0 && len(result.Nodes) < opts.Budget; depth++ {
        var nextQueue []string
        
        for _, nodeID := range queue {
            if visited[nodeID] {
                continue
            }
            visited[nodeID] = true
            
            node, err := e.store.GetNode(ctx, nodeID)
            if err != nil {
                continue
            }
            result.Nodes = append(result.Nodes, node)
            
            edges, err := e.store.GetEdges(ctx, nodeID, EdgeOptions{
                Types:     opts.EdgeTypes,
                Direction: opts.Direction,
            })
            if err != nil {
                continue
            }
            
            // Apply per-node cap
            if opts.PerNodeCap > 0 && len(edges) > opts.PerNodeCap {
                edges = edges[:opts.PerNodeCap]
            }
            
            for _, edge := range edges {
                result.Edges = append(result.Edges, edge)
                target := edge.Dst
                if opts.Direction == DirIn {
                    target = edge.Src
                }
                if !visited[target] {
                    nextQueue = append(nextQueue, target)
                    result.Trail = append(result.Trail, fmt.Sprintf("%s -[%s]-> %s", nodeID, edge.Type, target))
                }
            }
        }
        
        queue = nextQueue
    }
    
    return result, nil
}
```

**CLI commands (repoindex.go):**
```go
var repoindexCmd = &cobra.Command{
    Use:   "repoindex",
    Short: "Manage repo graph index",
}

var repoindexBuildCmd = &cobra.Command{
    Use:   "build",
    Short: "Build repo graph index",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Implementation
    },
}

var repoindexStatusCmd = &cobra.Command{
    Use:   "status",
    Short: "Show index status and stats",
}

var repoindexSearchCmd = &cobra.Command{
    Use:   "search <query>",
    Short: "Search nodes by text",
}

var repoindexExpandCmd = &cobra.Command{
    Use:   "expand <node-id>",
    Short: "Expand from node",
    Flags: []Flag{
        "--edge", []string, "Edge types to follow (CONTAINS, IMPORTS, CALLS, etc.)",
        "--depth", int, "Max hops (default 1)",
        "--direction", string, "out, in, or both",
    },
}
```

### Testing Strategy

1. **Unit tests** for SearchFTS with various queries
2. **Expand tests**:
   - Single seed, depth=1
   - Multiple seeds, depth=2
   - Budget limits respected
   - Direction filtering works
3. **CLI tests** via `agentctl repoindex --help` and basic invocations

### Acceptance Criteria

- [ ] `SearchFTS` returns ranked nodes matching query
- [ ] `Open` retrieves node by exact ID
- [ ] `Expand` traverses graph respecting depth/budget/edge filters
- [ ] Trail output shows expansion path
- [ ] CLI `repoindex build` triggers full build
- [ ] CLI `repoindex status` shows node/edge counts
- [ ] CLI `repoindex search` returns JSON results
- [ ] CLI `repoindex expand` returns nodes + edges

---

## PR 4.4: Incremental Rebuild by Package

### Summary

Implement incremental rebuild that only reprocesses packages with changed files, using content hashes stored in `pkg_state`.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/intelligence/indexing/repoindex/store/incremental.go` | Create | Transaction helpers for package replacement |
| `internal/intelligence/indexing/repoindex/builder/incremental.go` | Create | Diff detection and selective rebuild |
| `internal/intelligence/indexing/repoindex/builder/hash.go` | Create | File/package hash computation |
| `internal/intelligence/indexing/repoindex/builder/incremental_test.go` | Create | Tests for incremental behavior |

### Implementation Details

**Package hash (hash.go):**
```go
// ComputeFilesHash generates a stable hash of all files in a package
func ComputeFilesHash(files []string) (string, error) {
    h := sha256.New()
    
    // Sort for determinism
    sort.Strings(files)
    
    for _, file := range files {
        content, err := os.ReadFile(file)
        if err != nil {
            return "", err
        }
        h.Write([]byte(file))
        h.Write(content)
    }
    
    return hex.EncodeToString(h.Sum(nil))[:16], nil
}
```

**Store incremental ops (incremental.go):**
```go
// ReplacePackageGraph atomically replaces all nodes/edges for a package
func (s *Store) ReplacePackageGraph(ctx context.Context, pkg string, nodes []Node, edges []Edge, filesHash string) error {
    return s.withTx(ctx, func(tx *sql.Tx) error {
        // Delete old nodes (cascade deletes edges)
        _, err := tx.ExecContext(ctx, 
            `DELETE FROM nodes WHERE pkg = ? AND repo_key = ?`,
            pkg, s.repoKey)
        if err != nil {
            return err
        }
        
        // Insert new nodes
        for _, node := range nodes {
            if err := insertNodeTx(ctx, tx, node, s.repoKey); err != nil {
                return err
            }
        }
        
        // Insert new edges
        for _, edge := range edges {
            if err := insertEdgeTx(ctx, tx, edge, s.repoKey); err != nil {
                return err
            }
        }
        
        // Update pkg_state
        _, err = tx.ExecContext(ctx,
            `INSERT OR REPLACE INTO pkg_state (pkg, repo_key, files_hash, indexed_at)
             VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
            pkg, s.repoKey, filesHash)
        
        return err
    })
}

// GetStalePackages returns packages where files_hash differs or is missing
func (s *Store) GetStalePackages(ctx context.Context, pkgHashes map[string]string) ([]string, error)
```

**Builder incremental (incremental.go):**
```go
// BuildIncremental only rebuilds packages with changed files
func (b *Builder) BuildIncremental(ctx context.Context) (BuildResult, error) {
    // 1. Scan all packages and compute hashes
    pkgHashes, err := b.scanPackageHashes(ctx)
    if err != nil {
        return BuildResult{}, err
    }
    
    // 2. Get stale packages from store
    stale, err := b.store.GetStalePackages(ctx, pkgHashes)
    if err != nil {
        return BuildResult{}, err
    }
    
    if len(stale) == 0 {
        return BuildResult{}, nil // Nothing to do
    }
    
    // 3. Rebuild only stale packages
    var result BuildResult
    for _, pkg := range stale {
        nodes, edges, err := b.buildPackageGraph(ctx, pkg)
        if err != nil {
            return result, err
        }
        
        err = b.store.ReplacePackageGraph(ctx, pkg, nodes, edges, pkgHashes[pkg])
        if err != nil {
            return result, err
        }
        
        result.Packages++
        result.Nodes += len(nodes)
        result.Edges += len(edges)
    }
    
    return result, nil
}
```

**Dangling edge cleanup:**
```go
// CleanupDanglingEdges removes edges pointing to non-existent nodes
func (s *Store) CleanupDanglingEdges(ctx context.Context) (int64, error) {
    result, err := s.db.ExecContext(ctx, `
        DELETE FROM edges 
        WHERE repo_key = ?
        AND (
            src NOT IN (SELECT id FROM nodes WHERE repo_key = ?)
            OR dst NOT IN (SELECT id FROM nodes WHERE repo_key = ?)
        )
    `, s.repoKey, s.repoKey, s.repoKey)
    if err != nil {
        return 0, err
    }
    return result.RowsAffected()
}
```

### Testing Strategy

1. **Hash stability tests**:
   - Same files → same hash
   - Reordered files → same hash
   - Changed content → different hash

2. **Incremental rebuild tests**:
   - Initial build indexes all packages
   - No changes → no packages rebuilt
   - Single file change → only affected package rebuilt
   - New package → only new package indexed

3. **Edge cleanup tests**:
   - Edges to deleted packages are removed
   - Valid edges remain

### Acceptance Criteria

- [ ] `ComputeFilesHash` produces stable, deterministic hashes
- [ ] `ReplacePackageGraph` atomically replaces package subgraph
- [ ] `GetStalePackages` correctly identifies changed packages
- [ ] `BuildIncremental` only rebuilds stale packages
- [ ] Cross-package edges (IMPORTS) handled correctly
- [ ] Dangling edge cleanup works after package removal
- [ ] Rebuild time scales with changed packages, not total

---

## Integration Notes

### Database Location

The repo graph database lives at:
```
~/.agentctl/storage/repoindex.db
```

Multiple workspaces are supported via `repo_key` partitioning (same pattern as memory.db).

### Relation to Existing Stores

| Store | Purpose | Relation to Repo Graph |
|-------|---------|------------------------|
| `symbol.Store` | Embeddings + summaries | Source of symbol summaries |
| `memory.Store` | Gotchas, notes | Could link to concept nodes |
| `repoindex.Store` | Graph structure | New, owns relationships |

### CLI Integration

```bash
# Build index
agentctl repoindex build --workspace . --go --typescript

# Check status
agentctl repoindex status --workspace .

# Search by text
agentctl repoindex search "authentication" --limit 10

# Expand from node
agentctl repoindex expand "<repo_key>::pkg:github.com/user/repo/internal/auth" \
  --edge CONTAINS --edge IMPORTS --depth 2
```

### Future: Agent Tools

Phase 7 will wrap these as agent tools:
- `repo_index_search` - Find starting points
- `repo_index_expand` - Navigate relationships
- `repo_index_open` - Get node details

---

## Success Metrics

1. **Index size**: <100MB for typical monorepo
2. **Build time**: <30s for initial build, <5s for incremental
3. **Query latency**: <50ms for search, <100ms for 2-hop expand
4. **Coverage**: All Go/TS files indexed with CONTAINS edges

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Large repos blow up DB size | Storage costs | Add node/edge limits per workspace |
| Cross-package edges complex | Dangling references | Foreign key cascades + cleanup job |
| FTS5 index grows large | Query slowdown | Make FTS optional, profile first |
| go/packages slow for monorepos | Build time | Package-level caching + incremental |

---

## Appendix: Full Schema DDL

See PR 4.1 Schema Definition section above. The complete migration script will be in `migrations.go`.
