# Memvid Integration Analysis for agentctl

**Status**: Research\
**Date**: 2026-01-07\
**Purpose**: Evaluate how memvid (MV2 file format) could fit into agentctl's architecture as a potential vector search provider or storage backend

## Executive Summary

Memvid represents a **single-file memory infrastructure** with embedded search capabilities, designed for portable, offline-first AI memory. agentctl uses a **multi-database architecture** with pluggable vector providers. This analysis explores:

1. **Architectural compatibility**: How memvid's approach aligns with agentctl's design
2. **Integration strategies**: Replace, augment, or hybrid approaches
3. **Tradeoffs**: Portability vs flexibility, embedded vs pluggable
4. **Recommendations**: Where memvid adds value and where existing architecture is stronger

**Key Finding**: Memvid is best suited as an **optional portable export format** or **offline-first alternative** rather than replacing agentctl's core architecture. The multi-database approach with pluggable providers offers superior flexibility for agentctl's use cases.

---

## 1. Architecture Comparison

### Memvid (MV2) Architecture

```
┌─────────────────────────────────────────────────────────┐
│                    .mv2 File                            │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Header (4 KB)                                    │   │
│  │  - Magic bytes, version, pointers               │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Write-Ahead Log (1-64 MB)                       │   │
│  │  - Recovery mechanism, checkpointing            │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Data Segments (Variable)                        │   │
│  │  - Frames with payload, timestamps, tags        │   │
│  │  - Zstandard/LZ4 compression                    │   │
│  │  - SHA-256 checksums per frame                  │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Search Indices (Embedded)                       │   │
│  │  - Tantivy (BM25 lexical search)               │   │
│  │  - HNSW (384d vector similarity)               │   │
│  └─────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────┐   │
│  │ Time Index + TOC                                │   │
│  │  - Temporal ordering, checksums                 │   │
│  └─────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

**Key Characteristics:**
- **Single file**: All data, indices, WAL in one `.mv2` file
- **Append-only**: Frames never modified in place
- **Embedded search**: Both lexical (Tantivy) and vector (HNSW) built-in
- **Temporal queries**: Time-travel to past states
- **Offline-first**: No external dependencies
- **384 dimensions**: Fixed vector size for HNSW

### agentctl Architecture

```
~/.agentctl/
├── storage/              # Persistent data
│   ├── memory.db         # Named memories (gotchas, decisions, symbols, codemaps)
│   │   └── memories (with optional embeddings: 1024d Voyage/Gemini/Mistral)
│   ├── tasks.db          # Task management with dependencies
│   ├── sessions.db       # Session lineage, summaries, embeddings
│   │   ├── sessions (with embeddings: Gemini 768d)
│   │   ├── session_turns (per-turn tracking)
│   │   ├── session_chunks (for deep retrieval)
│   │   └── session_context_windows (compaction boundaries)
│   ├── trajectory.db     # Agent execution audit trail
│   └── agents.db         # Agent registry
├── cache/                # Ephemeral/regenerable
│   ├── embedding_queue.db
│   └── cache.db
├── cas/                  # Content-addressed storage
│   └── sha256/<digest>   # Immutable artifacts
└── sessions/archives/    # Compressed JSONL files
    └── <session-id>.jsonl.gz
```

**Key Characteristics:**
- **Multi-database**: Separate SQLite DBs for different concerns
- **Pluggable vector providers**: Voyage, Gemini, Mistral (1024d/768d)
- **Scope-based embedding**: Different models for symbols, memories, tasks, sessions
- **CAS for artifacts**: Separate content-addressed storage
- **Progressive memory**: L0 (raw) → L1 (summaries) → L2 (distilled) → archive
- **Cross-workspace sync**: Optional Turso for global search (CGO)

---

## 2. Vector Search Comparison

| Feature | Memvid | agentctl Current |
|---------|--------|-----------------|
| **Embedding dimensions** | 384 (fixed) | 1024 (Voyage), 768 (Gemini), 3072 (Gemini large) |
| **Vector index** | HNSW (embedded in .mv2) | External providers (Voyage/Gemini/Mistral APIs) |
| **Lexical search** | Tantivy (BM25, Boolean operators) | SQLite FTS5 (limited) |
| **Distance metric** | Cosine similarity | Cosine similarity |
| **Offline support** | Yes (fully embedded) | No (requires API keys) |
| **Multi-scope** | No (single index per file) | Yes (symbols, memory, tasks, sessions, codemaps) |
| **Cross-workspace** | Limited (need to open multiple files) | Yes (Turso sync for global search) |
| **Configuration** | M=16, ef_construction=200 | Provider-dependent |
| **Cost** | Free (local compute) | $0.06-0.18 per 1M tokens (Voyage) |

**Observations:**
1. Memvid's **384d** is smaller than agentctl's **1024d** (Voyage) — potential quality tradeoff
2. Memvid's **embedded HNSW** eliminates API costs and latency but increases file size
3. agentctl's **multi-scope** approach (different models for different content types) is more flexible
4. Memvid's **Tantivy** offers superior lexical search vs agentctl's SQLite FTS5

---

## 3. Storage Model Comparison

### Memvid: Append-Only Frames

```
Frame Format:
- Unique identifier (URI: mv2://[track/][path/]name)
- Timestamps and encoding metadata
- Compressed payload (Zstandard/LZ4)
- SHA-256 verification
- User-defined tags and lifecycle status
```

**Strengths:**
- **Immutable by design**: Frames never modified, excellent for audit trails
- **Self-contained**: All metadata travels with the frame
- **Hierarchical organization**: URI-based paths enable structure
- **Compression built-in**: Automatic codec selection

**Weaknesses:**
- **Fixed schema**: Hard to extend frame structure without versioning
- **Single file grows**: No natural sharding/partitioning
- **Lock contention**: All operations on one file (WAL mitigates this)

### agentctl: Multi-Database + CAS

```
Separation of Concerns:
- memory.db: Named memories with optional embeddings
- tasks.db: Task management with graph dependencies
- sessions.db: Session lineage and progressive memory
- CAS: Immutable artifacts (referenced by digest)
```

**Strengths:**
- **Flexible schema**: Each DB can evolve independently
- **Natural partitioning**: Different concerns in different files
- **Concurrent access**: Multiple DBs reduce lock contention
- **Standard tooling**: SQLite is ubiquitous, well-understood

**Weaknesses:**
- **Complexity**: Multiple files to manage, backup, sync
- **Not portable**: Can't easily share "one file" with full context
- **Cross-DB queries**: Harder to query across memory + tasks + sessions

---

## 4. Progressive Memory: Fit Analysis

### agentctl's Progressive Memory (Actor System)

```
L0: Raw Turns (8K tokens)
  ├─ Full fidelity, recent interactions
  └─ Triggers summarization when buffer fills

L1: Recent Summaries (6K tokens)
  ├─ Batch summaries of L0 turns
  └─ Triggers distillation when threshold reached

L2: Distilled History (4K tokens)
  ├─ Compressed session trajectory
  └─ Re-distills or archives when exceeds budget

Archive: sessions.db + .jsonl.gz
  ├─ Full turn history (searchable)
  └─ Chunk-level embeddings for deep retrieval
```

**Storage:**
- L0, L1, L2 artifacts: CAS
- Metadata/cursors: `actor_memory_state` table in sessions.db
- Archived turns: sessions.db + compressed JSONL

### How Memvid Could Fit

**Option 1: Replace sessions.db + JSONL archives**
- Store each actor's turns as frames in an `.mv2` file
- Use memvid's temporal queries for time-travel
- Use embedded HNSW for chunk-level retrieval

**Tradeoffs:**
- ✅ **Portability**: Single file per actor/session
- ✅ **Temporal queries**: Built-in time-travel
- ✅ **Offline search**: No API keys needed
- ❌ **Schema flexibility**: Harder to evolve turn metadata
- ❌ **Cursor management**: Need to map memvid frames to L0/L1/L2 cursors
- ❌ **Dimension mismatch**: 384d vs agentctl's 1024d (Voyage)

**Option 2: Export format for portability**
- Keep sessions.db for live operations
- Export completed sessions to `.mv2` for sharing/archival
- Use memvid for offline/portable session replay

**Tradeoffs:**
- ✅ **Best of both**: SQLite for live, memvid for portability
- ✅ **No lock-in**: Keep existing architecture
- ✅ **Selective export**: Only finalized sessions to .mv2
- ❌ **Duplicate storage**: Both SQLite and .mv2 (but .mv2 is optional)

---

## 5. Integration Strategies

### Strategy A: Replace Vector Provider (Embedded HNSW)

**What**: Use memvid's embedded HNSW instead of Voyage/Gemini APIs

**How**:
1. Create per-scope `.mv2` files:
   - `~/.agentctl/vectors/symbols.mv2`
   - `~/.agentctl/vectors/memory.mv2`
   - `~/.agentctl/vectors/sessions.mv2`
   - `~/.agentctl/vectors/tasks.mv2`
2. Implement `MemvidVectorStore` as alternative to `VoyageEmbedder`
3. Store embeddings in memvid, metadata in SQLite

**Pros**:
- ✅ **Offline-first**: No API keys required
- ✅ **No API costs**: Free embeddings (but need local model)
- ✅ **Superior lexical search**: Tantivy vs SQLite FTS5

**Cons**:
- ❌ **Dimension mismatch**: 384d vs 1024d (need to re-embed all existing vectors)
- ❌ **Quality tradeoff**: 384d may be lower quality than Voyage 1024d
- ❌ **Local compute**: Need local embedding model (Ollama, etc.)
- ❌ **File management**: Multiple .mv2 files vs single memory.db
- ❌ **Cross-scope search**: Harder to search across all scopes simultaneously

**Recommendation**: ❌ **Not recommended** — agentctl's pluggable provider approach is more flexible

---

### Strategy B: Session Archive Format

**What**: Export completed sessions to `.mv2` for portability/sharing

**How**:
1. Add `agentctl sessions export --format mv2 --output session.mv2`
2. Convert session_turns + chunks + context_windows to memvid frames
3. Embed full JSONL content as frames with temporal ordering
4. Use memvid's time-travel for session replay

**Pros**:
- ✅ **Portability**: Single file contains entire session
- ✅ **Shareability**: Easy to share sessions with team members
- ✅ **Offline replay**: No database needed to view session
- ✅ **Time-travel**: Navigate to any point in session history
- ✅ **Backward compatible**: Keep existing sessions.db for live operations

**Cons**:
- ⚠️ **Duplicate storage**: Both SQLite and .mv2 (but .mv2 is optional)
- ⚠️ **Conversion overhead**: Need to transform data to frame format
- ⚠️ **Dimension mismatch**: Re-embed chunks at 384d or skip embeddings

**Recommendation**: ✅ **Recommended** — Best fit for memvid's strengths (portability, shareability)

---

### Strategy C: Knowledge Base Export

**What**: Export named memories, gotchas, decisions to `.mv2` for offline knowledge base

**How**:
1. Add `agentctl memory export --workspace . --output kb.mv2`
2. Convert memories, codemaps, symbols to frames
3. Use memvid's hierarchical URIs: `mv2://workspace/memories/gotcha-auth`
4. Leverage Tantivy for full-text search over knowledge base

**Pros**:
- ✅ **Offline knowledge base**: No agentctl needed to query memories
- ✅ **Superior search**: Tantivy > SQLite FTS5 for full-text
- ✅ **Portable**: Share team knowledge in single file
- ✅ **Hierarchical**: URI structure maps well to memory organization

**Cons**:
- ⚠️ **Read-only export**: .mv2 is for sharing, not live editing
- ⚠️ **Dimension mismatch**: Re-embed at 384d or skip embeddings
- ⚠️ **Sync complexity**: Keep memory.db and .mv2 in sync if live

**Recommendation**: ✅ **Recommended** — Good fit for knowledge sharing use case

---

### Strategy D: Hybrid Architecture (Live + Archive)

**What**: Use SQLite for live operations, memvid for archival/portability

**How**:
```
Live Operations (Current):
  memory.db    → Named memories, embeddings (1024d Voyage)
  tasks.db     → Task management
  sessions.db  → Session lineage

Archive/Export (New):
  memory-export.mv2   → Snapshot of memories at time T
  session-abc123.mv2  → Archived session with time-travel
  workspace-kb.mv2    → Full knowledge base export
```

**Pros**:
- ✅ **Best of both worlds**: SQLite flexibility + memvid portability
- ✅ **No lock-in**: Keep existing architecture
- ✅ **Selective adoption**: Use .mv2 only where it adds value
- ✅ **Incremental migration**: Add memvid support gradually

**Cons**:
- ⚠️ **Complexity**: Maintain two formats
- ⚠️ **Sync burden**: Keep exports up-to-date (but can be lazy)
- ⚠️ **Dual tooling**: Need both SQLite and memvid libraries

**Recommendation**: ✅ **Recommended** — Pragmatic approach that preserves existing strengths

---

## 6. Technical Considerations

### Vector Dimension Compatibility

| Content Type | agentctl Current | Memvid | Compatibility |
|--------------|------------------|--------|---------------|
| Code symbols | 1024d (Voyage Code-3) | 384d | ⚠️ Re-embed required |
| Memories | 1024d (Voyage 3.5) | 384d | ⚠️ Re-embed required |
| Sessions | 768d (Gemini) | 384d | ⚠️ Re-embed required |
| Tasks | 1024d (Voyage 3.5) | 384d | ⚠️ Re-embed required |

**Impact**: All existing embeddings need re-generation at 384d to use memvid's HNSW

**Mitigation**:
- For **exports**: Re-embed during export process (one-time cost)
- For **live replacement**: Requires full re-indexing (expensive)

### Performance Implications

| Operation | SQLite + Voyage | Memvid .mv2 | Notes |
|-----------|-----------------|-------------|-------|
| **Vector search (warm)** | ~50-100ms (API call) | ~10-20ms (local HNSW) | Memvid faster |
| **Vector search (cold)** | ~50-100ms | ~50ms (disk seek + HNSW) | Similar |
| **Lexical search** | ~5-10ms (FTS5) | ~2-5ms (Tantivy) | Memvid faster |
| **Write throughput** | ~10K writes/sec | ~1K writes/sec (WAL) | SQLite faster |
| **File size growth** | Incremental (pages) | Append-only (frames) | Similar |

**Key Findings**:
- Memvid's **local HNSW** eliminates API latency (good for offline)
- SQLite's **write throughput** is superior (better for live operations)
- Memvid's **Tantivy** is faster than FTS5 (good for search-heavy workloads)

### Concurrency Model

**SQLite (agentctl current)**:
- **WAL mode**: Multiple readers + single writer
- **Lock granularity**: Database-level (but separate DBs reduce contention)
- **Concurrent access**: Excellent for read-heavy workloads

**Memvid**:
- **Embedded WAL**: Checkpointing at 75% occupancy or 1,000 transactions
- **Lock granularity**: File-level (all operations on one .mv2 file)
- **Concurrent access**: Good for append-heavy, decent for reads

**Impact**: Memvid's single-file design could bottleneck high-write scenarios (e.g., real-time symbol indexing)

---

## 7. Use Case Analysis

### Use Case 1: Symbol Indexing (Phase 4 of SWE Grep Plan)

**Current agentctl approach**:
- Extract symbols (Go AST, tree-sitter)
- Store in `memory.db` with type `symbol`
- Embed symbol bodies with Voyage Code-3 (1024d)
- Incremental updates per-symbol (check body_digest)

**Memvid alternative**:
- Store symbols as frames in `symbols.mv2`
- Use hierarchical URIs: `mv2://workspace/symbols/func/AuthHandler`
- Embed at 384d with local model

**Verdict**: ❌ **Not recommended**
- Reason: Symbol indexing is **write-heavy** (incremental updates). SQLite's superior write throughput and fine-grained locking (per-symbol) outperform memvid's append-only model.
- Tradeoff: Voyage Code-3 (1024d) likely outperforms 384d embeddings for code search quality.

---

### Use Case 2: Session Archive & Deep Retrieval (Phase 4 of Progressive Memory)

**Current agentctl approach**:
- Capture sessions to `sessions.db`
- Archive JSONL to `.jsonl.gz`
- Extract chunks with embeddings (Gemini 768d)
- Tier 1 (session-level) → Tier 1.5 (context windows) → Tier 2 (chunks) → Tier 3 (JSONL)

**Memvid alternative**:
- Export session to `session-abc123.mv2`
- Store turns as frames with temporal ordering
- Use memvid's time-travel to navigate context windows
- Embed chunks at 384d

**Verdict**: ✅ **Recommended** (as export format)
- Reason: **Portability** is key for session sharing. A single `.mv2` file is easier to share with team members than "sessions.db + .jsonl.gz + embedding schema".
- Tradeoff: Dimension mismatch (768d → 384d) acceptable for archived sessions (not used for live retrieval).
- Implementation: Add `agentctl sessions export --format mv2`

---

### Use Case 3: Named Memories & Gotchas

**Current agentctl approach**:
- Store in `memory.db` with type (gotcha, decision, pattern, etc.)
- Optional embeddings (Voyage 3.5, 1024d)
- Workspace-scoped + cross-workspace sync (Turso)

**Memvid alternative**:
- Export to `workspace-kb.mv2`
- Use URIs: `mv2://workspace/gotchas/auth-race-condition`
- Leverage Tantivy for full-text search

**Verdict**: ✅ **Recommended** (as export format)
- Reason: **Offline knowledge base** is valuable for team sharing. Tantivy's superior full-text search is a bonus.
- Tradeoff: Read-only export (not for live editing). Keep `memory.db` as source of truth.
- Implementation: Add `agentctl memory export --workspace . --output kb.mv2`

---

### Use Case 4: Cross-Workspace Search (Turso Sync)

**Current agentctl approach**:
- Push embeddings to Turso (remote libSQL)
- Query across all workspaces with `--global` flag
- Voyage Rerank-2.5 for improved relevance

**Memvid alternative**:
- Create one `.mv2` file per workspace
- Query by opening multiple .mv2 files
- Use memvid's search across all files

**Verdict**: ❌ **Not recommended**
- Reason: Turso's **centralized database** is superior for cross-workspace search. Opening multiple .mv2 files sequentially is slower than a single query to Turso.
- Tradeoff: Memvid's offline capability is useful for *exporting* global knowledge, but not for *live* cross-workspace search.

---

## 8. Implementation Roadmap

### Phase 1: Export Format (MVP)

**Goal**: Add memvid export for portability/sharing without changing core architecture

**Tasks**:
1. **Add memvid Go library dependency**
   - Evaluate existing Rust crates with Go bindings vs pure Go implementation
   - Prefer pure Go to avoid CGO complexity (agentctl is CGO-optional)

2. **Implement session export**
   ```bash
   agentctl sessions export --session-id <id> --format mv2 --output session.mv2
   ```
   - Convert session_turns to frames
   - Embed chunk previews at 384d (using local model or skip embeddings)
   - Include context_windows as frame metadata
   - Preserve JSONL content in frame payloads

3. **Implement memory export**
   ```bash
   agentctl memory export --workspace . --output kb.mv2
   ```
   - Convert memories to frames with hierarchical URIs
   - Include gotchas, decisions, patterns, codemaps
   - Use Tantivy for full-text search indexing

4. **Add import for verification**
   ```bash
   agentctl sessions import --file session.mv2 --verify
   ```
   - Parse .mv2 frames back to internal format
   - Verify checksums and temporal ordering
   - Display session replay UI (TUI or Web GUI)

**Success Criteria**:
- ✅ Exported .mv2 files are self-contained and portable
- ✅ Can share sessions with team members (no agentctl DB needed to view)
- ✅ Tantivy search provides better UX than SQLite FTS5

---

### Phase 2: Offline Embedding Model (Optional)

**Goal**: Enable local embeddings for offline .mv2 creation

**Tasks**:
1. **Integrate Ollama or local model**
   - Use `nomic-embed-text` (768d) or similar
   - Fallback to 384d if memvid requires fixed dimensions

2. **Add embedding config**
   ```yaml
   # ~/.agentctl/config.yaml
   memvid:
     embedding:
       provider: ollama
       model: nomic-embed-text
       url: http://localhost:11434
       dimensions: 768  # Or 384 if memvid enforces
   ```

3. **Batch embedding during export**
   - Embed all frames during export process
   - Show progress bar for large exports

**Success Criteria**:
- ✅ Can create .mv2 files with embeddings offline (no API keys)
- ✅ Search quality acceptable for archived sessions

---

### Phase 3: Read-Only Viewer (Optional)

**Goal**: Standalone viewer for .mv2 files (no agentctl installation needed)

**Tasks**:
1. **Build minimal viewer binary**
   - Read .mv2 file
   - Display frames in chronological order
   - Leverage Tantivy for search
   - Use HNSW for semantic queries

2. **TUI or web UI**
   - Session timeline view
   - Jump to context windows
   - Search by keyword or vector similarity

3. **Distribute as standalone**
   - `agentctl-viewer` binary
   - Shareable with non-agentctl users

**Success Criteria**:
- ✅ Non-technical users can view/search exported sessions
- ✅ Useful for knowledge sharing in teams

---

## 9. Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| **Dimension mismatch (384d vs 1024d)** | High | Re-embed during export; accept quality tradeoff for portability |
| **Pure Go memvid unavailable** | Medium | Use Rust crate with CGO bindings (but requires CGO build) |
| **Dual format complexity** | Medium | Make .mv2 export optional; keep SQLite as primary |
| **Sync burden** | Low | Exports are lazy (on-demand, not automatic) |
| **File size growth** | Low | Compression built into memvid; monitor disk usage |

---

## 10. Recommendations

### ✅ **DO: Use memvid as export format**

**Why**:
- Portability is memvid's core strength
- Single-file format ideal for sharing sessions and knowledge bases
- Tantivy's superior lexical search adds value
- Time-travel and temporal queries useful for session replay
- No disruption to existing architecture

**How**:
- Implement `agentctl sessions export --format mv2`
- Implement `agentctl memory export --workspace . --output kb.mv2`
- Add standalone viewer for .mv2 files (optional)

---

### ❌ **DON'T: Replace core storage with memvid**

**Why**:
- SQLite's multi-database approach offers better separation of concerns
- Superior write throughput for live operations (symbol indexing, task updates)
- Pluggable vector providers (Voyage, Gemini) more flexible than fixed 384d
- Dimension mismatch (1024d → 384d) requires full re-embedding (expensive)
- Cross-workspace Turso sync superior to multi-file approach

**Exception**: If **offline-first** becomes a hard requirement (e.g., air-gapped deployments), reconsider memvid as primary store with local embedding model.

---

### ⚠️ **MAYBE: Hybrid approach for specific scopes**

**Consider memvid for**:
- **Codemaps** (already finalized artifacts, rarely updated)
- **Archived sessions** (read-only after completion)
- **Named memories** (if offline knowledge base is critical)

**Keep SQLite for**:
- **Symbols** (high write volume, incremental updates)
- **Tasks** (dependency graph, frequent status changes)
- **Live sessions** (progressive memory, cursor-based compaction)

---

## 11. Conclusion

Memvid represents a well-designed **single-file memory infrastructure** optimized for **portability and offline-first operation**. However, agentctl's current **multi-database architecture** with **pluggable vector providers** is better suited for the system's core use cases:

1. **High write throughput** (symbol indexing, task updates)
2. **Flexible embedding models** (Voyage Code-3 for symbols, Gemini for sessions)
3. **Cross-workspace search** (Turso sync)
4. **Separation of concerns** (memory, tasks, sessions in separate DBs)

**Best integration strategy**: Use memvid as an **optional export format** for:
- Sharing completed sessions with team members
- Creating portable knowledge bases (memories, gotchas, decisions)
- Offline session replay and time-travel debugging

This approach preserves agentctl's architectural strengths while leveraging memvid's portability and superior lexical search (Tantivy) where it adds clear value.

---

## 12. Next Steps

1. **Prototype session export** (Phase 1.2 from roadmap)
   - Add memvid dependency (evaluate pure Go vs CGO)
   - Implement `agentctl sessions export --format mv2`
   - Test with real session data

2. **Evaluate embedding options** (Phase 2)
   - Test Ollama nomic-embed-text for local embeddings
   - Compare 384d vs 768d vs 1024d quality on sample queries

3. **User feedback** (before Phase 3)
   - Share prototype .mv2 exports with users
   - Gather feedback on portability value vs complexity tradeoff
   - Decide if standalone viewer is worth building

4. **Document integration** (if prototype succeeds)
   - Update `docs/impl_plan/universal_swe_grep_and_agents.md` with memvid export strategy
   - Add memvid export to `docs/designs/progressive-memory-system.md`
   - Create `docs/guides/exporting-sessions-to-memvid.md`

---

## Appendix A: Memvid API Surface (Hypothetical)

If agentctl integrates memvid, the API might look like:

```go
// Export session to .mv2
func ExportSessionToMemvid(
    ctx context.Context,
    sessionID string,
    outputPath string,
    opts MemvidExportOptions,
) error

type MemvidExportOptions struct {
    IncludeEmbeddings bool        // Generate 384d embeddings
    EmbeddingModel    string      // "ollama/nomic-embed-text"
    CompressPayloads  bool        // Use Zstandard compression
    IncludeJSONL      bool        // Embed full JSONL as frames
}

// Open .mv2 file for read-only access
func OpenMemvidSession(path string) (*MemvidSession, error)

type MemvidSession struct {
    // Query frames by time range
    QueryByTime(start, end time.Time) ([]Frame, error)

    // Full-text search via Tantivy
    Search(query string) ([]Frame, error)

    // Vector similarity search via HNSW
    SemanticSearch(embedding []float32, limit int) ([]Frame, error)

    // Time-travel to specific point
    RewindTo(timestamp time.Time) (*MemvidSession, error)
}
```

---

## Appendix B: References

- Memvid MV2 Spec: https://github.com/memvid/memvid/blob/main/MV2_SPEC.md
- Memvid README: https://github.com/memvid/memvid/blob/main/README.md
- agentctl Implementation Plan: `docs/impl_plan/universal_swe_grep_and_agents.md`
- agentctl Unified Agents: `docs/spec/unified_agents.md`
- agentctl Progressive Memory: `docs/designs/progressive-memory-system.md`
- agentctl Actor Progressive Memory: `docs/designs/actor-progressive-memory.md`
