# Embedding Quality & Repo Graph Index Roadmap

> Comprehensive plan for improving code retrieval quality through doc-enriched embeddings and a queryable repo graph index.

## Overview

This roadmap transforms agentctl's code understanding from "keyword search" to "semantic navigation" through:

1. **Better embeddings** - Query-optimized vectors + doc-enriched symbol text
2. **Repo graph index** - SQLite DAG store with containment, imports, and call edges
3. **Comment-driven expansion** - `Index:` blocks in GoDoc create soft edges for navigation
4. **Tooling** - Agent tools and GUI panels for "Manus for code" experience

## Non-negotiables

- Node IDs are repo-scoped (namespaced IDs or composite PK) to prevent collisions.
- Index block syntax is canonical and parseable (structured fields).
- Embedding text normalization is centralized in one package.
- Embedding inputs are secret-scanned (comments included).
- Each phase ships with a measurable eval artifact.

## Phases

| Phase | Focus | PRs | Dependencies |
|-------|-------|-----|--------------|
| [Phase -1](./phase--1-eval-harness.md) | Evaluation harness | 1 | None |
| [Phase 0](./phase-0-standards.md) | Standards + scaffolding | 3 | None |
| [Phase 1](./phase-1-query-embeddings.md) | Query embedding quality | 1 | None |
| [Phase 2](./phase-2-symbol-embeddings.md) | Doc-enriched symbol embeddings | 3 | Phase 0.2 |
| [Phase 3](./phase-3-file-intent.md) | File-level intent (TOC) | 2 | Phase 0 |
| [Phase 4](./phase-4-repo-graph.md) | Repo Graph Index v1 | 4 | None |
| [Phase 5](./phase-5-comment-edges.md) | Comment-driven expansion | 4 | Phase 4 |
| [Phase 6](./phase-6-doc-sweep.md) | Repo-wide doc sweep | 3+ | Phase 0.1 |
| [Phase 7](./phase-7-gui-integration.md) | Tool + GUI integration | 2 | Phase 4, 5 |
| [Phase X](./phase-x-secret-safety.md) | Embedding input safety | 1 | Phase 0 |

## Dependency Graph

```
Phase -1 (eval harness) ────────────────────────────────────┐
                                                            ▼
Phase 0.1 (doc standards) ──────────────────────────────────┐
                                                            ▼
Phase 0.2 (embeddingtext pkg) ──► Phase 2 (symbol embeds) ──► Phase 6 (doc sweep)
                                                            │
Phase 0.3 (feature flags) ──────────────────────────────────┤
                                                            │
Phase 1 (query embeds) ─────────────────────────────────────┤
                                                            │
Phase 3 (file intent) ──────────────────────────────────────┤
                                                            │
Phase 4 (repo graph) ──► Phase 5 (comment edges) ───────────┼──► Phase 7 (GUI)
                                                            │
Phase X (secret safety) ────────────────────────────────────┤
                                                            ▼
                                               [Full semantic navigation]
```

## Quick Start

**Immediate wins (ship now):**
- Phase 1: Use `EmbedQuery` in retrieval (small change, measurable improvement)

**Core improvements (next sprint):**
- Phase 0 + Phase 2: Doc-enriched embeddings transform search quality

**Infrastructure (parallel track):**
- Phase 4: Repo graph store enables relationship navigation

## Feature Flags

The roadmap uses feature flags to gate behavior and enable gradual rollout:

| Flag | Values | Default | Purpose |
|------|--------|---------|---------|
| `EMBED_QUERY_MODE` | `auto\|embed\|embed_query` | `auto` | Control query embedding strategy |
| `EMBED_SYMBOL_TEXT_MODE` | `raw\|doc_enriched` | `raw` | Symbol embedding text format |
| `EMBED_FILE_TEXT_MODE` | `raw\|intent` | `raw` | File embedding content (optional) |

## Key Files by Phase

### Phase 0-2 (Embeddings)
- `internal/intelligence/indexing/embeddingtext/` - New utility package
- `internal/intelligence/retrieval/semantic_search.go` - Query embedding changes
- `internal/intelligence/indexing/symbol/` - Symbol embedding enqueue

### Phase 4-5 (Repo Graph)
- `internal/intelligence/indexing/repoindex/store/` - New SQLite DAG store
- `internal/intelligence/indexing/repoindex/builder/` - Graph construction
- `internal/intelligence/indexing/repoindex/query/` - Search/expand engine

### Phase 7 (Tools)
- `skills/repo_index_search/` - Search tool
- `skills/repo_index_expand/` - Expand tool
- `packages/gui-agent/` - Graph visualization panels

## Related Documents

- [Design Notes](./design-notes.md) - Two-tier approach: when to use Index blocks vs simple GoDoc
- [Doc Comment Snippets](./doc-comment-snippets.md) - Copy/paste examples for GoDoc + Index blocks
- [Phase Details](./phase-0-standards.md) - Start with Phase 0

## Success Metrics

1. **Search relevance**: "find auth handler" returns auth code, not just files mentioning "auth"
2. **Navigation depth**: From any symbol, reach related symbols in 1-2 hops
3. **Doc coverage**: 80%+ exported symbols have GoDoc with Index blocks
4. **Query latency**: Sub-100ms for graph traversals
