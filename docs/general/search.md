# Search and Embeddings

Machine-friendly reference for semantic retrieval, reranking, and repo graph search.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `skills/code_semantic_search`, `skills/code_smart_search`, `skills/code_snippet_extract`, `internal/searchquery`, `internal/searchindex`, `internal/intelligence/retrieval/v2`, `internal/intelligence/codecontext`, `internal/indexing/semantic`, `internal/indexing/rerank`, `internal/indexing/repoindex`, `internal/storage/dbdriver/search.go` |
| Last reviewed | 2026-03-07 |

## Search Surfaces

| Surface | Command | Best use |
|--------|---------|----------|
| Unified semantic search | `agentctl run code/semantic_search --input ...` | Cross-source retrieval (`symbols`, `sessions`, `memories`, `tasks`, `codemaps`) |
| Smart code search | `agentctl run code/smart_search --input ...` | Code-file retrieval plus evidence extraction |
| Snippet extraction | `agentctl run code/snippet_extract --input ...` | Anchor-aware code evidence extraction from known candidates |
| Repo graph index | `agentctl index repo <build|search|expand|open|ask>` | Structural relationships (calls/refers/imports) |

## Code Search Stack

Current code-search flow is layered as:

- `internal/searchquery`
  Shared query parsing (`terms`, `phrases`, `identifiers`, `path hints`)
- `internal/searchindex`
  Typed lexical/vector recall over code documents
- `internal/intelligence/retrieval/v2`
  Fusion and grouped file hits with anchors
- `internal/intelligence/codecontext`
  Anchor-aware snippet extraction and output preparation

`internal/retrieval` is no longer the active code-search engine. It remains only for tree helpers and summary-generation utilities still used elsewhere.

## `code/semantic_search` Input Contract

Source of truth: `skills/code_semantic_search/main.go`.

| Field | Type | Notes |
|------|------|-------|
| `query` | string | Required unless `format:"tree"` |
| `scope` | string[] | Valid values: `symbols`, `sessions`, `memories`, `tasks`, `codemaps` |
| `limit` | int | Default `20` |
| `min_similarity` | float | Default `0.3` |
| `rerank_enabled` | bool | Optional Voyage reranking |
| `rerank_top_k` | int | Candidate size before rerank |
| `rerank_model` | string | Default `rerank-2.5` |
| `remote` / `global` / `workspaces` | bool/string[] | Cross-workspace remote search mode |
| `format` | string | `json` or `tree` |
| `repo_index_mode` | string | Symbol/code scopes only: `auto`, `search`, `dag`, `off` |

`repo_index_mode` guidance:

- `auto`
  Precise default. Uses repo-index search first and falls back to DAG only when search returns no repo-index hits.
- `search`
  Best for structural queries when you want the most direct repo-index entrypoints.
- `dag`
  Best for broader graph exploration when you want neighborhood context.
- `off`
  Disables repo-index contribution.

For exact symbol lookup, the exact lane should usually dominate regardless of repo-index mode. For structural phrases like `repo index dag grep`, `search` is the preferred precise mode and `dag` is the exploratory mode.

## Embedding Model Selection

Source of truth: `internal/indexing/semantic/provider.go`.

| Scope category | Default model |
|---------------|---------------|
| `symbols` and file summaries | `voyage-code-3` |
| `memory`, `tasks`, `sessions`, `codemaps` | `voyage-3.5` |

Override order:

1. `AGENTCTL_EMBEDDING_MODEL_<SCOPE>` (for example `AGENTCTL_EMBEDDING_MODEL_SYMBOLS`)
2. `AGENTCTL_EMBEDDING_MODEL_CODE` (code scopes) / `AGENTCTL_EMBEDDING_MODEL_TEXT` (text scopes)
3. Built-in defaults above

## Rerank Configuration

Source of truth: `internal/indexing/rerank/config.go`.

| Env var | Purpose |
|--------|---------|
| `AGENTCTL_RERANK_ENABLED` | Enable reranking |
| `AGENTCTL_RERANK_TOP_K` | Candidates passed to reranker |
| `AGENTCTL_RERANK_FINAL_K` | Final result count |
| `AGENTCTL_RERANK_MODEL` | Reranker model (default `rerank-2.5`) |
| `AGENTCTL_RERANK_SCORE_BLEND` | Blend original and rerank score |
| `AGENTCTL_RERANK_TIMEOUT` | Request timeout |
| `AGENTCTL_RERANK_RATE_LIMIT` | Requests/minute (`0` disables) |
| `AGENTCTL_RERANK_RATE_LIMIT_WAIT` | Wait vs fail when throttled |

## Hybrid Scoring (BM25 + Vector)

Source of truth: `internal/storage/dbdriver/search.go`.

| Parameter | Meaning |
|----------|---------|
| `alpha=1.0` | BM25-only weight |
| `alpha=0.5` | Balanced lexical/semantic weighting (default) |
| `alpha=0.0` | Vector-only weight |

Formula: `score = alpha * bm25_scaled + (1 - alpha) * vector_scaled`

## Repo Graph Index Flow

```bash
agentctl index repo build --dry-run --workspace . --go --typescript --elixir
agentctl index repo build --workspace . --go --typescript --elixir
agentctl index repo search --workspace . --query "Supervisor" --limit 10
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
```

## Common Failure Modes

| Symptom | Likely cause |
|--------|---------------|
| Empty semantic matches for vector-only scopes | Missing embedding provider key (`VOYAGE_API_KEY` / `GEMINI_API_KEY`) |
| Dimension mismatch | Model/store dimension drift; rebuild the affected scope using [docs/general/embedding-rebuilds.md](embedding-rebuilds.md) |
| Rerank unavailable | Missing key or `AGENTCTL_RERANK_ENABLED` not enabled |
| Remote/global search unavailable | Missing Turso remote configuration |

## Related Docs

- [docs/general/repoindex.md](repoindex.md)
- [docs/general/storage.md](storage.md)
- [docs/general/skills.md](skills.md)
- [docs/general/gotchas.md](gotchas.md)
