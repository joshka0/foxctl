---
title: Search and index
description: Choose semantic search, smart search, context grep, or repo graph search for code retrieval.
---

foxctl exposes several retrieval surfaces. Each one answers a different kind of
question. Choose by the shape of the question, not by implementation detail.

## Which search to use

| Question shape | Start with | Why |
|---------------|------------|-----|
| "Where is this concept implemented?" | `code/semantic_search` | Ranked results across symbols, sessions, memory, and codemaps |
| "Find snippets that answer this question" | `code/smart_search` | Combines candidate generation with snippet extraction in one call |
| "Find exact text or function shapes" | `code/context_grep` | Regex or literal pattern matching with expanded code blocks |
| "Show graph neighbors and call paths" | `repoindex` or `code/dag_grep` | Structural relationships — calls, references, imports |
| "Recall named project memory" | `memory/query` or session recall | Persistent named knowledge, not code search |
| "Extract code from known files" | `code/snippet_extract` | You already have candidate files and want evidence extraction |

## Semantic search

`code/semantic_search` is the primary retrieval tool for finding code by
meaning rather than exact text. It queries across multiple scopes and returns
ranked candidates.

### Basic search

```bash
foxctl run code/semantic_search --input '{"query":"auth middleware","limit":25}'
```

### Tree mode for orientation

Use tree mode when exploring an unfamiliar area of the codebase. It produces a
hierarchical directory structure with relevance scores and summaries:

```bash
foxctl run code/semantic_search --input '{"query":"storage","format":"tree","limit":25}'
```

Tree options:
- `tree_depth: 1` — root level only
- `tree_depth: 2` — two levels (default)
- `tree_depth: 0` — unlimited depth (special case: 0 means "no limit")
- `tree_max_children: 50` — cap items per directory
- `tree_include_summaries: false` — disable summaries for faster output

This smart table-of-contents shape is the same retrieval instinct behind
repoindex. For the PageIndex comparison, see
[repoindex and PageIndex](/retrieval/repoindex-pageindex/).

### Scope filtering

Narrow results to specific source types:

```bash
foxctl run code/semantic_search --input '{
  "query": "auth",
  "scope": ["symbols"],
  "limit": 10
}'
```

Valid scope values: `symbols`, `sessions`, `memories`, `tasks`, `codemaps`.

### Input contract

| Field | Type | Notes |
|-------|------|-------|
| `query` | string | Required unless `format:"tree"` |
| `scope` | string[] | Valid values: `symbols`, `sessions`, `memories`, `tasks`, `codemaps` |
| `limit` | int | Default `20` |
| `min_similarity` | float | Default `0.3` |
| `format` | string | `json` (default) or `tree` |
| `repo_index_mode` | string | `auto` (default), `search`, `dag`, or `off` |

### Repo index mode

When `scope` includes code scopes, the `repo_index_mode` field controls how the
repo graph index contributes:

| Mode | Behavior |
|------|----------|
| `auto` | Precise default. Uses repo-index search first, falls back to DAG when search returns no hits |
| `search` | Best for structural queries when you want the most direct repo-index entrypoints |
| `dag` | Broader graph exploration with neighborhood context |
| `off` | Disables repo-index contribution entirely |

For exact symbol lookup, the exact lane usually dominates regardless of mode.
For structural phrases, `search` is the precise mode and `dag` is the
exploratory mode.

### Cross-workspace search

Search across all workspaces when the remote Turso database is configured:

```bash
foxctl run code/semantic_search --input '{"query":"auth","remote":true,"global":true}'
```

### Reranking

Enable optional reranking for improved relevance (requires a rerank endpoint):

```bash
foxctl run code/semantic_search --input '{
  "query": "error handling",
  "rerank_enabled": true,
  "rerank_top_k": 20
}'
```

Rerank configuration is controlled by environment variables:

| Env var | Purpose |
|---------|---------|
| `FOXCTL_RERANK_ENABLED` | Enable reranking |
| `FOXCTL_RERANK_PROVIDER` | Provider: `qwen` or OpenAI-compatible local endpoint |
| `FOXCTL_RERANK_BASE_URL` | Rerank endpoint base URL |
| `FOXCTL_RERANK_API_KEY` | Optional bearer token |
| `FOXCTL_RERANK_MODEL` | Model name (default `Qwen/Qwen3-Reranker-0.6B`) |

### Memory decay

`memory_decay_enabled` applies a local recency/access rerank to memory-scope
candidates. This is separate from model-based reranking. Old strong matches
remain visible; weak recent memories do not become authoritative just because
they are fresh.

```bash
foxctl run code/semantic_search --input '{
  "query": "deployment gotchas",
  "scope": ["memories"],
  "memory_decay_enabled": true
}'
```

## Smart search

`code/smart_search` combines candidate generation with snippet extraction in a
single call. Use it when you don't already have specific files and need both
discovery and evidence.

### Query mode

```bash
foxctl run code/smart_search --input '{"query":"how does session restore work"}'
```

### Task-oriented mode

The newer task-oriented input shape produces more focused extraction:

```bash
foxctl run code/smart_search --input '{
  "question": "How does session restore work?",
  "limits": {"max_snippets": 20}
}'
```

Smart search auto-generates candidates from indexes (symbols, semantic,
ripgrep), then extracts relevant snippets from those candidates. It is the
end-to-end search tool when you want both "where" and "what."

## Context grep

`code/context_grep` searches for patterns and returns surrounding code blocks —
full function bodies, not just matching lines. Use it when exact text, function
signatures, or structured grep are more reliable than semantic ranking.

### Regex pattern

```bash
foxctl run code/context_grep --input '{"pattern":"Run\\(ctx","path":"internal"}'
```

### Expand to function body

By default, context grep returns the matching line with context. Use expansion
targets to get full enclosing blocks:

```bash
foxctl run code/context_grep --input '{
  "pattern": "func.*HandleRequest",
  "path": "internal/api",
  "expand_to": "function"
}'
```

Valid expansion targets: `function`, `block`, `class`.

### AST-based structural search

For Go, TypeScript, and other supported languages, use `ast_pattern` for
structural pattern matching instead of regex:

```bash
foxctl run code/context_grep --input '{
  "ast_pattern": "func $NAME($$$ARGS) $$$BODY",
  "language": "go",
  "path": "internal/runtime"
}'
```

### Filter options

| Option | Purpose |
|--------|---------|
| `glob` | Include file patterns (e.g., `["*.go"]`) |
| `glob_not` | Exclude file patterns (e.g., `["vendor/*"]`) |
| `expand_to` | Expansion target: `function`, `block`, or `class` |
| `max_blocks` | Maximum code blocks to return |
| `max_matches` | Maximum raw matches before expansion |

## Snippet extraction

`code/snippet_extract` extracts code snippets from candidate files you already
know about. Use it after `semantic_search` or `smart_search` when you have
specific files and want evidence extraction.

```bash
foxctl run code/snippet_extract --input '{
  "query": "authentication logic",
  "candidates": [
    {"path": "internal/auth/handler.go", "line": 45},
    {"path": "internal/auth/middleware.go", "line": 12}
  ]
}'
```

Extraction modes:
- `snippets` (default) — full code snippets with context
- `masked` — structure only, no implementation detail
- `flow` — control flow extraction

## Hybrid scoring

Under the hood, foxctl uses a hybrid BM25 + vector scoring system:

| Parameter | Meaning |
|-----------|---------|
| `alpha=1.0` | BM25-only (lexical) |
| `alpha=0.5` | Balanced lexical/semantic (default) |
| `alpha=0.0` | Vector-only (semantic) |

Formula: `score = alpha * bm25_scaled + (1 - alpha) * vector_scaled`

This blending happens automatically; you don't need to configure it for most
queries.

## Embedding model

The default embedding model is `text-embedding-qwen3-embedding-8b` for all
scopes. Override per-scope with environment variables:

1. `FOXCTL_EMBEDDING_MODEL_<SCOPE>` (e.g., `FOXCTL_EMBEDDING_MODEL_SYMBOLS`)
2. `FOXCTL_EMBEDDING_MODEL_CODE` (code scopes) / `FOXCTL_EMBEDDING_MODEL_TEXT` (text scopes)
3. Built-in defaults

If semantic search returns empty results for vector-only scopes, the likely
cause is a missing or unreachable OpenAI-compatible embedding endpoint.

## Common failure modes

| Symptom | Likely cause |
|---------|--------------|
| Empty semantic matches for vector-only scopes | Missing or unreachable embedding endpoint |
| Dimension mismatch | Model/store dimension drift; rebuild the affected scope |
| Rerank unavailable | Missing key or `FOXCTL_RERANK_ENABLED` not set |
| Remote/global search unavailable | Missing Turso remote configuration |

## Production guidance

1. **Use semantic search to orient.** Tree mode gives a fast structural
   overview before you dive into code.
2. **Re-open the canonical source file before editing.** Search results are
   pointers, not substitutes for reading the actual code.
3. **Use repoindex or DAG grep when relationships matter.** Text search finds
   occurrences; graph queries find connections.
4. **Use `snippet_extract` when you already have candidates; use
   `smart_search` when you need discovery and extraction together.**
5. **Treat generated codemaps as supporting artifacts, not canonical behavior.**

## Pipeline pattern

Chain search tools for comprehensive code understanding:

```
semantic_search → snippet_extract → review
     "where"         "what"          "meaning"
```

Or use `smart_search` for the first two steps combined:

```bash
# 1. Find candidates
foxctl run code/semantic_search --input '{"query":"error handling"}' > candidates.json

# 2. Extract snippets from those candidates
foxctl run code/snippet_extract --input "$(cat candidates.json | jq '{query: "error handling", candidates: .data.results}')"

# Or combine both steps:
foxctl run code/smart_search --input '{"query":"error handling"}'
```

## Cross-references

- [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) — graph-based retrieval
- [Repoindex model](/retrieval/repoindex-model/) — node and edge data model
- [Skills runtime](/skills/runtime-and-install/) — how to run search skills
- [Repo navigation workflow](/workflows/repo-navigation/) — end-to-end navigation guide
