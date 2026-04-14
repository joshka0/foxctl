# LEANN vs Memvid vs foxctl: Vector Storage Architecture Comparison

**Status**: Research
**Date**: 2026-01-07
**Purpose**: Compare three approaches to vector storage for AI memory systems

---

## Executive Summary

| System | Core Innovation | Storage Model | Best For |
|--------|-----------------|---------------|----------|
| **LEANN** | Selective recomputation | Graph-only (no embedding storage) | Edge devices, massive scale |
| **Memvid** | Single-file portability | Append-only frames + embedded indices | Sharing, archival, offline |
| **foxctl** | Scope-based flexibility | Multi-database + pluggable providers | Live operations, cross-workspace |

**Key Finding**: LEANN's selective recomputation approach could dramatically reduce foxctl's storage footprint (97% savings) while maintaining search quality. This is particularly relevant for:
1. **Symbols scope**: 1024d embeddings for millions of symbols
2. **Cross-workspace search**: Global embedding storage at Turso
3. **Session archives**: Historical session embeddings

---

## 1. LEANN: Graph-Based Selective Recomputation

### Core Concept

LEANN (Low-Storage Efficient Approximate Nearest Neighbor) eliminates embedding storage entirely by:

1. **Building a proximity graph** (HNSW) during indexing
2. **Discarding embeddings** after graph construction
3. **Recomputing embeddings on-demand** during search
4. **Pruning the graph** to preserve high-degree hub nodes

```
Traditional Vector DB:
┌─────────────────────────────────────────────┐
│ Document 1 → Embedding (4KB) → Store        │
│ Document 2 → Embedding (4KB) → Store        │
│ ...                                         │
│ Document 1M → Embedding (4KB) → Store = 4GB │
└─────────────────────────────────────────────┘

LEANN:
┌─────────────────────────────────────────────┐
│ Document 1 → Embedding → Build Graph → Discard │
│ Document 2 → Embedding → Build Graph → Discard │
│ ...                                            │
│ Store: Pruned Graph (200MB) + Text (varies)    │
│ Search: Recompute embeddings for traversed nodes│
└────────────────────────────────────────────────┘
```

### Architecture Details

```
LEANN Index Structure:
┌─────────────────────────────────────────────────┐
│ Pruned Proximity Graph (CSR format)              │
│  - High-degree hub nodes preserved               │
│  - Redundant edges pruned                        │
│  - 50x smaller than embedding storage            │
├─────────────────────────────────────────────────┤
│ Raw Documents (text/code/etc)                    │
│  - Original content for recomputation            │
│  - Compressed storage                            │
├─────────────────────────────────────────────────┤
│ Optional: PQ Codes (for DiskANN backend)         │
│  - Approximate distances for graph traversal     │
│  - Real-time reranking with true embeddings      │
└─────────────────────────────────────────────────┘
```

### Performance Characteristics

| Metric | Traditional (FAISS) | LEANN HNSW | LEANN DiskANN |
|--------|---------------------|------------|---------------|
| Storage (60M docs) | 201 GB | 6 GB | ~15 GB |
| Storage savings | - | **97%** | ~92% |
| Search latency (warm) | 10ms | 200-500ms | 50-100ms |
| Search latency (cold) | 50ms | 200-500ms | 100-200ms |
| Top-3 Recall | 95% | 90% | 93% |
| Index build time | 1x | 1x (then discard) | 1x |

**Key Tradeoff**: LEANN trades **search latency** (embedding recomputation) for **storage efficiency** (97% reduction).

### When LEANN Makes Sense

- **Personal devices**: Limited storage (laptops, phones)
- **Massive scale**: Millions of documents where embedding storage is prohibitive
- **Infrequent search**: Batch processing, not real-time
- **Cost-sensitive**: GPU compute cheaper than storage (for some workloads)

---

## 2. Memvid: Single-File Portable Memory

### Core Concept

Memvid packages all data and indices into a single `.mv2` file:

```
.mv2 File Structure:
┌─────────────────────────────────────────────────┐
│ Header (4KB)                                     │
│  - Magic bytes, version, pointers                │
├─────────────────────────────────────────────────┤
│ Write-Ahead Log (1-64MB)                         │
│  - Recovery, checkpointing                       │
├─────────────────────────────────────────────────┤
│ Data Segments                                    │
│  - Append-only frames (payload + metadata)       │
│  - Zstandard/LZ4 compression                     │
│  - SHA-256 checksums                             │
├─────────────────────────────────────────────────┤
│ Search Indices                                   │
│  - Tantivy (BM25 lexical search)                │
│  - HNSW (384d vector similarity)                │
├─────────────────────────────────────────────────┤
│ Time Index + TOC                                 │
│  - Temporal ordering, checksums                  │
└─────────────────────────────────────────────────┘
```

### Key Characteristics

| Feature | Memvid | Notes |
|---------|--------|-------|
| File format | Single .mv2 | Portable, shareable |
| Vector dimensions | 384d (fixed) | Smaller than Voyage/Gemini |
| Vector index | HNSW (embedded) | Fast local search |
| Lexical search | Tantivy (embedded) | Superior to SQLite FTS5 |
| Storage model | Append-only frames | Immutable, audit-friendly |
| Temporal queries | Yes (time-travel) | Navigate to past states |
| Offline support | Full | No external dependencies |

### When Memvid Makes Sense

- **Portability**: Share complete sessions/knowledge bases
- **Archival**: Read-only historical data
- **Offline-first**: No API keys or network required
- **Temporal debugging**: Navigate session history

---

## 3. foxctl: Multi-Database + Pluggable Providers

### Current Architecture

```
~/.foxctl/
├── storage/
│   ├── memory.db      # Named memories (1024d Voyage)
│   ├── tasks.db       # Task management
│   ├── sessions.db    # Session lineage (1024d Voyage)
│   ├── trajectory.db  # Audit trail
│   └── agents.db      # Agent registry
├── cache/
│   ├── embedding_queue.db  # Background jobs
│   └── cache.db       # TTL cache
└── cas/
    └── sha256/<digest>  # Immutable artifacts
```

### Key Characteristics

| Feature | foxctl | Notes |
|---------|----------|-------|
| Database | Multi-SQLite | Separation of concerns |
| Vector dimensions | 1024d (Voyage), 768d (Gemini) | Flexible per-scope |
| Vector providers | Voyage, Gemini, Mistral | Pluggable |
| Lexical search | SQLite FTS5 | Functional, not best-in-class |
| Storage model | CRUD (update in place) | Flexible |
| Cross-workspace | Turso sync | Global search |
| Offline support | Partial (FTS fallback) | Degraded mode |

### Scope-Based Embedding Strategy

```
Scope        → Model         → Dimensions → Cost/1M
─────────────────────────────────────────────────────
symbols      → voyage-code-3 → 1024d      → $0.18
memory       → voyage-3.5    → 1024d      → $0.06
tasks        → voyage-3.5    → 1024d      → $0.06
sessions     → voyage-3.5    → 1024d      → $0.06
codemaps     → voyage-3.5    → 1024d      → $0.06
```

---

## 4. Side-by-Side Comparison

### Storage Efficiency

| Scenario | foxctl | Memvid | LEANN |
|----------|----------|--------|-------|
| 100K symbols (1024d) | 400 MB | 150 MB (384d) | 12 MB |
| 10K sessions (1024d) | 40 MB | 15 MB (384d) | 1.2 MB |
| 1M memories (1024d) | 4 GB | 1.5 GB (384d) | 120 MB |

**LEANN advantage**: 97% storage reduction vs foxctl

### Search Quality

| Metric | foxctl (Voyage 1024d) | Memvid (384d) | LEANN (any dims) |
|--------|-------------------------|---------------|------------------|
| Code retrieval | Excellent (voyage-code-3) | Good | Excellent (same model) |
| Text retrieval | Excellent (voyage-3.5) | Good | Excellent (same model) |
| Cross-lingual | Good | Limited | Same as source model |

**Key insight**: LEANN doesn't constrain embedding dimensions—it can use any model, just recomputes on-demand.

### Search Latency

| Scenario | foxctl | Memvid | LEANN HNSW | LEANN DiskANN |
|----------|----------|--------|------------|---------------|
| Warm cache | 50-100ms (API) | 10-20ms | 200-500ms | 50-100ms |
| Cold cache | 50-100ms (API) | 50ms | 200-500ms | 100-200ms |
| Batch (100 queries) | 5-10s | 1-2s | 20-50s | 5-10s |

**LEANN tradeoff**: Higher latency due to embedding recomputation

### Flexibility

| Capability | foxctl | Memvid | LEANN |
|------------|----------|--------|-------|
| Multi-scope (different models) | ✅ | ❌ (single file) | ✅ (per-index) |
| Cross-workspace search | ✅ (Turso) | ❌ | ✅ (if centralized) |
| Live updates | ✅ | ⚠️ (append-only) | ✅ |
| Portability | ❌ | ✅ | ❌ |
| Offline | ⚠️ (FTS fallback) | ✅ | ✅ |

---

## 5. Integration Strategies

### Strategy A: LEANN for Symbol Indexing

**Problem**: Symbol embeddings (1024d × millions) consume significant storage.

**Solution**: Replace symbol embedding storage with LEANN's selective recomputation.

```go
// Current: Store embeddings
type Symbol struct {
    ID        string
    Body      string
    Embedding []float32  // 1024 × 4 bytes = 4KB per symbol
}

// LEANN: Store graph only
type SymbolIndex struct {
    Graph     LeannGraph    // CSR format, ~50x smaller
    Documents map[string]string  // ID → body for recomputation
}

// Search: Recompute embeddings for traversed nodes
func (idx *SymbolIndex) Search(query string, k int) []Symbol {
    queryEmb := embed(query)  // Generate query embedding
    candidates := idx.Graph.TraverseWithRecompute(queryEmb, k)
    return candidates
}
```

**Expected savings**:
- 1M symbols: 4GB → ~120MB (97% reduction)
- Tradeoff: Search latency 50ms → 200-500ms

**When to use**: Large codebases, offline-first, batch symbol search.

---

### Strategy B: LEANN for Session Archives

**Problem**: Archived sessions accumulate embeddings that are rarely searched.

**Solution**: Use LEANN for archival tier, standard storage for live sessions.

```
Live Sessions (sessions.db):
  - Recent sessions (last 30 days)
  - Full embeddings for fast search
  - Progressive memory (L0/L1/L2)

Archived Sessions (LEANN index):
  - Older sessions (>30 days)
  - Graph-only storage
  - Recompute on search (acceptable latency for archives)
```

**Expected savings**:
- 10K archived sessions: 40MB → 1.2MB
- Live session performance unchanged

---

### Strategy C: Hybrid foxctl + Memvid + LEANN

**Architecture**:

```
~/.foxctl/
├── storage/
│   ├── memory.db           # Live memories (standard)
│   ├── tasks.db            # Tasks (standard)
│   ├── sessions.db         # Live sessions (standard)
│   └── symbols/
│       └── leann.idx       # Symbol index (LEANN)
├── exports/
│   ├── session-abc.mv2     # Portable session (Memvid)
│   └── kb-workspace.mv2    # Knowledge base (Memvid)
└── archives/
    └── sessions.leann      # Archived sessions (LEANN)
```

**Use each where it excels**:
- **SQLite**: Live operations, CRUD, cross-workspace sync
- **LEANN**: Large-scale indices (symbols, archives)
- **Memvid**: Portability, sharing, offline viewing

---

## 6. Implementation Considerations

### LEANN Integration

**Pros**:
- 97% storage savings for embedding-heavy scopes
- Works with existing Voyage/Gemini models
- FAISS-based (established, well-tested)

**Cons**:
- Search latency increase (recomputation overhead)
- Python/C++ implementation (need Go bindings or subprocess)
- Index rebuild required on model change

**Implementation path**:
1. Add LEANN as optional backend for symbol scope
2. Fallback to standard SQLite for small indices
3. Auto-migrate to LEANN when index exceeds threshold

### Dimension Flexibility

**LEANN advantage**: Unlike Memvid (fixed 384d), LEANN works with any embedding model because it stores the graph structure, not the embeddings.

```
foxctl + LEANN:
  symbols  → voyage-code-3 (1024d) → LEANN graph (no embedding storage)
  memory   → voyage-3.5 (1024d)    → Standard SQLite (live updates)
  sessions → voyage-3.5 (1024d)    → LEANN for archives
```

---

## 7. Recommendations

### For foxctl

| Scope | Current | Recommended | Rationale |
|-------|---------|-------------|-----------|
| symbols | SQLite + embeddings | **LEANN** | High volume, 97% storage savings |
| memory | SQLite + embeddings | Keep current | Low volume, live updates |
| tasks | SQLite + embeddings | Keep current | Low volume, live updates |
| sessions (live) | SQLite + embeddings | Keep current | Fast search needed |
| sessions (archive) | SQLite + embeddings | **LEANN** | Rarely searched, 97% savings |
| exports | N/A | **Memvid** | Portability, sharing |

### Implementation Priority

1. **LEANN for symbols** (highest impact)
   - Symbol indexing is the largest embedding consumer
   - Acceptable latency for code search (200-500ms)
   - Start with optional flag: `AGENTCTL_SYMBOL_INDEX=leann`

2. **LEANN for session archives** (medium impact)
   - Archive sessions older than 30 days
   - Background migration during idle time
   - Preserve SQLite for live sessions

3. **Memvid for exports** (already planned)
   - Session export for sharing
   - Knowledge base export for offline use
   - No change to core architecture

---

## 8. Open Questions

1. **LEANN Go bindings**: Does LEANN have Go bindings, or do we need CGO/subprocess?
2. **Embedding cache**: Should we cache recently-computed embeddings during search?
3. **Index updates**: How does LEANN handle incremental updates (new symbols)?
4. **Hybrid search**: Can we combine LEANN vector search with SQLite FTS for hybrid retrieval?

---

## References

- [LEANN Paper (arXiv:2506.08276)](https://arxiv.org/abs/2506.08276)
- [LEANN GitHub](https://github.com/yichuan-w/LEANN)
- [Memvid Integration Analysis](./memvid-integration-analysis.md)
- [foxctl Progressive Memory](../designs/progressive-memory-system.md)

---

## Sources

- [LEANN: A Low-Storage Vector Index (arXiv)](https://arxiv.org/abs/2506.08276)
- [LEANN GitHub Repository](https://github.com/yichuan-w/LEANN)
- [LEANN: A Low-Storage Vector Index (HTML)](https://arxiv.org/html/2506.08276v1)
- [MarkTechPost: Meet LEANN](https://www.marktechpost.com/2025/08/12/meet-leann-the-tiniest-vector-database-that-democratizes-personal-ai-with-storage-efficient-approximate-nearest-neighbor-ann-search-index/)
