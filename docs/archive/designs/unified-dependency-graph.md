# Unified Dependency Graph for Semantic Search

## Problem Statement

Our semantic search (`code/semantic_search`) currently ranks results by:
1. **Embedding similarity** - cosine distance to query
2. **RRF (Reciprocal Rank Fusion)** - combines rankings from multiple sources

What's missing: **Authority/importance signals**. A function called by 50 other functions should rank higher than one called by none, even if similarity scores are equal.

## Current State

### Existing Graph Infrastructure

| Component | Location | What It Tracks |
|-----------|----------|----------------|
| **Task Graph** | `internal/intelligence/analysis/tasksgraph/` | Task → Task (depends_on) |
| **Symbol Call Graph** | `internal/intelligence/indexing/symbol/` | Symbol → Symbol (calls) |
| **Import Graph** | `skills/code_imports/` | File → File (imports) |
| **Workflow DAG** | `internal/workflow/` | Step → Step (depends_on) |
| **Overseer Scoring** | `internal/intelligence/analysis/overseer/` | Combines PageRank + CriticalPath + Mailbox |

### Gap Analysis

```
Current (Siloed):
  Tasks ──depends_on──► Tasks
  Symbols ──calls──► Symbols
  Files ──imports──► Files

Needed (Unified):
  Session ──touched──► Symbol
  Task ──modified──► Symbol
  Memory ──about──► Symbol
  Session ──worked_on──► Task
  Symbol ──mentions──► Memory
```

## Proposed Architecture

### 1. Stable Node Identity Strategy

**Problem:** Path-based IDs like `symbol:src/auth.go:Login` break on file moves/renames.

**Solution:** Use stable IDs with path alias resolution:

| Node Type | Stable ID Format | Example |
|-----------|------------------|---------|
| Task | `task:<ulid>` | `task:01HXYZ...` |
| Session | `session:<ulid>` | `session:01HABC...` |
| Memory | `memory:<name>` | `memory:gotcha-auth-middleware` |
| Symbol | `symbol:<content_hash>:<name>` | `symbol:a1b2c3:Login` |
| File | `file:<cas_digest>` | `file:sha256:abc123...` |

**Path Alias Table:** Maps stable IDs to current paths for resolution.

### 2. Unified Graph Schema

```sql
-- Central node index (fast authority queries, avoid expensive joins)
CREATE TABLE graph_nodes (
    workspace TEXT NOT NULL,
    node_id TEXT NOT NULL,           -- Stable ID (e.g., "task:01HXYZ", "symbol:a1b2c3:Login")
    node_type TEXT NOT NULL,         -- "session" | "task" | "symbol" | "memory" | "file"
    title TEXT,                      -- Display name
    current_path TEXT,               -- Current file path (for symbols/files, updated on moves)
    pagerank REAL DEFAULT 0.0,       -- Pre-computed PageRank score
    in_degree INTEGER DEFAULT 0,     -- Cached edge counts
    out_degree INTEGER DEFAULT 0,
    last_seen TEXT NOT NULL,         -- For stale node detection
    metadata TEXT,                   -- JSON for type-specific data

    PRIMARY KEY (workspace, node_id)
);

CREATE INDEX idx_nodes_type ON graph_nodes(workspace, node_type);
CREATE INDEX idx_nodes_pagerank ON graph_nodes(workspace, pagerank DESC);
CREATE INDEX idx_nodes_path ON graph_nodes(workspace, current_path);

-- Edge table (relationships between nodes)
CREATE TABLE graph_edges (
    id TEXT PRIMARY KEY,             -- ULID
    workspace TEXT NOT NULL,
    from_id TEXT NOT NULL,           -- Stable node ID
    from_type TEXT NOT NULL,
    to_id TEXT NOT NULL,             -- Stable node ID
    to_type TEXT NOT NULL,
    edge_type TEXT NOT NULL,         -- Enum: see below
    weight REAL DEFAULT 1.0,         -- For weighted PageRank
    created_at TEXT NOT NULL,
    updated_at TEXT,
    ttl_days INTEGER,                -- NULL = no expiry, sessions default 90
    metadata TEXT,                   -- JSON for additional context

    UNIQUE(workspace, from_id, to_id, edge_type),
    FOREIGN KEY (workspace, from_id) REFERENCES graph_nodes(workspace, node_id),
    FOREIGN KEY (workspace, to_id) REFERENCES graph_nodes(workspace, node_id)
);

CREATE INDEX idx_edges_from ON graph_edges(workspace, from_id);
CREATE INDEX idx_edges_to ON graph_edges(workspace, to_id);
CREATE INDEX idx_edges_type ON graph_edges(workspace, edge_type);
CREATE INDEX idx_edges_ttl ON graph_edges(ttl_days) WHERE ttl_days IS NOT NULL;
```

### 2. Edge Types & Semantics

| Edge Type | From | To | Created By | Weight |
|-----------|------|-----|------------|--------|
| `touched` | Session | Symbol | `session-capture` hook | 1.0 |
| `modified` | Task | Symbol | `task-guard` + Edit hooks | 1.5 (stronger) |
| `calls` | Symbol | Symbol | `code/imports` or LSP | 1.0 |
| `imports` | Symbol | Symbol | `live-index` hook | 0.8 |
| `depends_on` | Task | Task | `todo/manage` | 2.0 (explicit) |
| `worked_on` | Session | Task | `task-guard` tracking | 1.0 |
| `about` | Memory | Symbol | Memory creation with file context | 1.0 |
| `mentions` | Session | Memory | NLP extraction or explicit tags | 0.5 |
| `parent_of` | Task | Task | `todo/manage` hierarchy | 1.0 |

### 3. Edge Creation Points (Hooks)

```
┌─────────────────────────────────────────────────────────────────┐
│                        Hook Integration                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  PostToolUse(Edit/Write)                                        │
│    └─► live-index.sh                                            │
│         └─► graph/add_edge: symbol → symbol (imports/calls)     │
│    └─► task-guard tracking                                      │
│         └─► graph/add_edge: task → symbol (modified)            │
│                                                                  │
│  Stop (session end)                                             │
│    └─► session-capture.sh                                       │
│         └─► graph/add_edge: session → symbol (touched)          │
│         └─► graph/add_edge: session → task (worked_on)          │
│                                                                  │
│  todo/manage (add/complete)                                     │
│    └─► graph/add_edge: task → task (depends_on, parent_of)      │
│                                                                  │
│  memory/put                                                     │
│    └─► graph/add_edge: memory → symbol (about)                  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 4. PageRank Integration

#### Option A: Query-Time Personalized PageRank
```
Query: "authentication"
  │
  ├─► Semantic search returns: [symbol:auth.go:Login, session:abc, task:123]
  │
  └─► Personalized PageRank from these seeds
       │
       └─► Walk graph, accumulate scores on connected nodes
            │
            └─► Boost highly-connected nodes in results
```

**Pros:** Always fresh, considers query context
**Cons:** Slower for large graphs

#### Option B: Pre-computed PageRank with Periodic Refresh
```
Background job (hourly or on significant changes):
  │
  └─► Load full graph for workspace
       │
       └─► Run PageRank
            │
            └─► Store rank as field on each node in named_memory/sessions/tasks
```

**Pros:** Fast at query time
**Cons:** Can be stale

#### Recommendation: Hybrid
- **Pre-compute** global PageRank periodically (stored as `pagerank_score` on nodes)
- **Query-time** boost based on immediate neighbors of matched nodes

### 5. Search Integration

Modified `code/semantic_search` scoring:

```go
type ScoredResult struct {
    // Existing
    Source     string
    ID         string
    Similarity float64  // Embedding cosine similarity

    // New
    PageRank       float64  // Pre-computed authority score
    ConnectionBoost float64  // Query-time neighbor bonus
    FinalScore     float64  // Combined score
}

// Scoring formula (tunable weights)
func computeFinalScore(r ScoredResult) float64 {
    const (
        wSimilarity  = 0.50  // Semantic relevance
        wPageRank    = 0.30  // Global importance
        wConnection  = 0.20  // Query-specific context
    )
    return (wSimilarity * r.Similarity) +
           (wPageRank * r.PageRank) +
           (wConnection * r.ConnectionBoost)
}
```

### 6. Session ID Integration

The user mentioned working on session_id tracking. This fits naturally:

```sql
-- Existing named_memory gains session_id
ALTER TABLE named_memory ADD COLUMN session_id TEXT;

-- Edges automatically created
-- When session creates/modifies a memory:
INSERT INTO graph_edges (from_id, to_id, edge_type, ...)
VALUES ('session:abc', 'memory:gotcha-auth', 'created', ...);
```

This enables:
- "What memories were created in session X?"
- "What sessions reference this memory?"
- PageRank flows from active sessions to their artifacts

## Implementation Phases (Refined)

### Phase 1: Storage + Migration Foundation
**Goal:** Unified tables, migrate existing task dependencies

1. Add `graph_nodes` and `graph_edges` tables to memory.db
2. Migrate `tasks.parent_id` → `graph_edges` (edge_type: `parent_of`)
3. Migrate `tasks.depends_on[]` → `graph_edges` (edge_type: `depends_on`)
4. Create skills:
   - `graph/add_edge` - UPSERT by (workspace, from_id, to_id, edge_type)
   - `graph/query` - List edges by node/type, traverse neighbors
   - `graph/sync_nodes` - Populate graph_nodes from source tables
5. Ensure idempotent migrations (run on db open or via CLI)

**Deliverables:**
- `internal/storage/graph/` package
- Migration in `internal/storage/dbdriver/migrations/`
- Insights view consumes single edge table

### Phase 2: Ingestion Hooks
**Goal:** Automatic edge creation from existing hooks

| Hook | Edge Created | Details |
|------|--------------|---------|
| `live-index.sh` | symbol → symbol | calls, imports (use existing symbol.CallEdge) |
| `session-capture.sh` | session → symbol | touched (files read/edited in session) |
| `session-capture.sh` | session → task | worked_on (if task was active) |
| `task-guard` / Edit hooks | task → symbol | modified (edits under active task) |
| `todo/manage` | task → task | depends_on, parent_of (on write) |
| `memory/put` | memory → symbol | about (when file context provided) |

**Implementation:**
- Each hook calls `graph/add_edge` skill
- Use stable IDs (task ULID, session ULID, symbol content hash)
- Default TTL: 90 days for session edges, NULL for others

### Phase 3: PageRank Computation
**Goal:** Authority scores stored on nodes

1. Create `graph/pagerank` skill using gonum (reuse `tasksgraph` patterns)
2. Store `pagerank` in `graph_nodes` after computation
3. Update `in_degree`, `out_degree` counts
4. Trigger options:
   - Periodic background job (hourly)
   - On-demand via CLI: `agentctl graph pagerank --workspace .`
   - After significant edge changes (batch threshold)

**Algorithm:**
- Damping: 0.85, tolerance: 1e-6 (match existing tasksgraph)
- Per-workspace computation (no global graph)
- Weight edges by `edge_type` default weights

### Phase 4: Search Integration
**Goal:** PageRank influences semantic search ranking

1. Modify `code/semantic_search`:
   - Fetch `pagerank` from `graph_nodes` by result ID
   - Compute `ConnectionBoost` from query-matched seed neighbors
   - Apply combined scoring formula

2. Scoring weights (configurable, learnable later):
   ```
   wSimilarity  = 0.50  // Semantic relevance
   wPageRank    = 0.30  // Global authority
   wConnection  = 0.20  // Query-specific context
   ```

3. Cache pagerank in memory for active workspace (avoid repeated lookups)

### Phase 5: Maintenance & Repair
**Goal:** Graph hygiene and resilience

1. **Edge TTL cleanup:** Scheduled job deletes edges where `created_at + ttl_days < now`
2. **Dangling edge cleanup:** Remove edges pointing to non-existent nodes
3. **Path alias repair:** When `live-index` sees file move:
   - Update `graph_nodes.current_path` for affected symbol nodes
   - Edges remain on stable ID (no breakage)
4. **Repair command:** `agentctl graph repair --workspace .`
   - Re-sync path aliases
   - Delete dead edges
   - Recalculate degree counts

### Phase 6: CLI & Observability
**Goal:** Developer tooling and debugging

1. CLI commands:
   - `agentctl graph stats` - Edge counts by type, node counts by type
   - `agentctl graph top --type symbol --limit 10` - Top nodes by PageRank
   - `agentctl graph edges --node <id>` - List edges for a node
   - `agentctl graph export --format dot` - Graphviz DOT export

2. Doctor checks (in `agentctl doctor`):
   - Coverage: % of symbols with incoming edges
   - Dangling: edges to missing nodes
   - Expired: edges pending TTL cleanup
   - Degree distribution

3. Smoke tests:
   - PageRank on fixture graph matches expected values
   - Edge UPSERT idempotency

## Design Decisions

| Question | Decision | Rationale |
|----------|----------|-----------|
| Graph scope | Per-workspace | Avoid cross-project pollution, simpler PageRank |
| Edge TTL | 90 days for sessions, NULL for others | Sessions are ephemeral, code relationships are stable |
| Deletion cascading | Via maintenance job, not FK cascade | SQLite FK limitations with cross-table refs |
| Weight tuning | Learnable (Phase 2+) | Reuse `optimization/weights` pattern from overseer |
| Node sync | On PageRank compute | Avoid real-time sync overhead |

## Relationship to Existing Systems

```
┌─────────────────────────────────────────────────────────────────┐
│                     Unified Graph System                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐       │
│  │ tasksgraph   │    │ symbol index │    │ import graph │       │
│  │ (PageRank)   │    │ (CallEdge)   │    │ (deps)       │       │
│  └──────┬───────┘    └──────┬───────┘    └──────┬───────┘       │
│         │                   │                   │                │
│         └───────────────────┴───────────────────┘                │
│                             │                                    │
│                             ▼                                    │
│                    ┌────────────────┐                            │
│                    │  graph_edges   │  ◄── Unified storage       │
│                    │  (all types)   │                            │
│                    └────────────────┘                            │
│                             │                                    │
│                             ▼                                    │
│                    ┌────────────────┐                            │
│                    │ semantic_search│  ◄── Queries graph for    │
│                    │ + PageRank     │      authority signals     │
│                    └────────────────┘                            │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Success Metrics

1. **Precision improvement**: High-authority symbols rank higher for relevant queries
2. **Discovery**: Related items surfaced via graph connections
3. **Performance**: Query latency stays under 500ms with graph lookups
4. **Coverage**: >80% of symbols have at least one incoming edge

## References

- Existing PageRank: `internal/intelligence/analysis/tasksgraph/graph.go`
- Overseer scoring: `internal/intelligence/analysis/overseer/scoring.go`
- Symbol indexing: `internal/intelligence/indexing/symbol/`
- Semantic search: `skills/code_semantic_search/main.go`
