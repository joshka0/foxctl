---
title: Repo navigation
description: Find and explore code using semantic search, smart search, repoindex, and DAG grep.
---

When an agent or user needs to find code, explain relationships, or prepare a compact evidence bundle before editing, foxctl provides a layered navigation system. This page covers each search surface, when to use it, and practical examples.

## Search surfaces at a glance

| Surface | Command | Best use |
|---------|---------|----------|
| Semantic search | `foxctl run code/semantic_search` | Cross-source retrieval (symbols, sessions, memories, tasks, codemaps) |
| Smart search | `foxctl run code/smart_search` | Code-file retrieval plus evidence extraction in one step |
| Snippet extract | `foxctl run code/snippet_extract` | Anchor-aware code evidence extraction from known candidate files |
| Context grep | `foxctl run code/context_grep` | Pattern search returning full function/block bodies |
| Repo graph index | `foxctl index repo build/search/expand` | Structural relationships (calls, refs, imports) |
| DAG grep | `foxctl run code/dag_grep` | Compact explanation subgraph in one call |

## Choosing the right surface

```
Need to find code?
├── Topic is fuzzy or you need orientation?
│   → Semantic search (tree mode)
├── Need snippets and candidates in one step?
│   → Smart search
├── Already have candidate files?
│   → Snippet extract
├── Searching for a specific pattern?
│   → Context grep
└── Need to understand relationships?
    ├── Build the repo index first
    └── Use expand or DAG grep
```

## Semantic search

Semantic search finds code by meaning, not just text matching. Use it first when the topic is fuzzy, when you need a tree-shaped orientation of the codebase, or when searching across multiple source types.

### Basic search

```bash
foxctl run code/semantic_search --input '{"query": "auth middleware"}'
```

### Tree orientation

```bash
foxctl run code/semantic_search --input '{"query": "storage", "format": "tree", "limit": 25}'
```

Tree output includes:
- Hierarchical directory structure with relevance scores
- LLM-generated summaries for each file
- Root summary synthesizing the codebase area

Tree options:

| Option | Default | Description |
|--------|---------|-------------|
| `tree_depth` | `2` | Depth levels (`0` = unlimited) |
| `tree_max_children` | `50` | Maximum items per directory |
| `tree_include_summaries` | `true` | Include file summaries |

### Scoped search

Filter by source type using the `scope` parameter:

```bash
foxctl run code/semantic_search --input '{"query": "error handling", "scope": ["symbols", "memories"]}'
```

Valid scope values: `symbols`, `sessions`, `memories`, `tasks`, `codemaps`.

### Input parameters

| Field | Type | Notes |
|-------|------|-------|
| `query` | string | Required unless `format:"tree"` |
| `scope` | string[] | Source types to search |
| `limit` | int | Default `20` |
| `min_similarity` | float | Default `0.3` |
| `format` | string | `json` (default) or `tree` |
| `repo_index_mode` | string | `auto`, `search`, `dag`, `off` (symbol/code scopes only) |
| `remote` / `global` | bool | Cross-workspace remote search |
| `rerank_enabled` | bool | Enable model-based reranking |

### Repo index mode

When searching symbol or code scopes, `repo_index_mode` controls how the repo graph index contributes:

| Mode | Behavior |
|------|----------|
| `auto` (default) | Repo-index search first, DAG fallback when no hits |
| `search` | Precise structural queries, most direct entrypoints |
| `dag` | Broader graph exploration, neighborhood context |
| `off` | Disables repo-index contribution |

## Smart search

Smart search auto-generates candidates from indexes and then extracts relevant snippets. Use it when you need code snippets in one step without separately managing candidate lists.

```bash
foxctl run code/smart_search --input '{"query": "how does session restore work"}'
```

Smart search combines semantic search (the "find" step) and snippet extraction (the "extract" step) into a single call.

### With file filter

```bash
foxctl run code/smart_search --input '{"query": "error types", "glob": "*.go"}'
```

Output includes extracted snippets with full function bodies and surrounding code context.

## Snippet extraction

Use snippet extraction when you already have candidate files (from semantic search or a manual list) and need anchor-aware evidence extraction.

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

| Mode | Description |
|------|-------------|
| `snippets` (default) | Full code snippets with context |
| `masked` | Structure only, implementation hidden |
| `flow` | Control flow extraction |

## Context grep

Context grep searches for patterns and returns complete surrounding blocks (functions, methods, classes) rather than just matching lines:

```bash
foxctl run code/context_grep --input '{"pattern": "func.*Handle", "path": "."}'
```

### With file type filter

```bash
foxctl run code/context_grep --input '{
  "pattern": "class.*Service",
  "path": "src/",
  "glob": "*.py"
}'
```

Context grep supports ripgrep, ast-grep (structural), and line expansion modes:

| Mode | Trigger | Use for |
|------|---------|---------|
| `ripgrep` (default) | Regex pattern | Text pattern matching |
| `ast` | `ast_pattern` parameter | Structural code patterns |
| `line` | `file_path` + line range | Expanding specific line ranges |

## Repo graph index (repoindex)

The repo graph index is a local, per-repo SQLite database for navigating code by structural relationships. It powers call graphs, import trees, and reference lookups.

### What it stores

**Nodes:** package, file, symbol, concept

**Edges:** `CONTAINS`, `IMPORTS`, `REFERS_TO`, `CALLS`

**Language coverage:**

| Language | Nodes | Edges |
|----------|-------|-------|
| Go | Packages, files, symbols | `IMPORTS`, `REFERS_TO`, `CALLS` |
| TypeScript (`.ts`/`.tsx`) | Packages, files, symbols | `IMPORTS`, heuristic `CALLS` |
| Elixir (`.ex`/`.exs`) | Packages, files, symbols | Heuristic `REFERS_TO` |
| Terraform (`.tf`) | Packages, files, concepts | Resource/module/provider nodes |
| Kubernetes manifests | Packages, files, concepts | Resource nodes |
| Shell (`.sh`) | Packages, files, concepts | Command and env var nodes |

### Build the index

```bash
# Build with Go and TypeScript
foxctl index repo build --workspace . --go --typescript

# Include Elixir
foxctl index repo build --workspace . --go --typescript --elixir

# Infrastructure and script indexing
foxctl index repo build --workspace . --terraform --kubernetes --shell

# Force a full rebuild (incremental is default)
foxctl index repo build --workspace . --go --typescript --incremental=false
```

Optional build flags:

| Flag | Description |
|------|-------------|
| `--incremental=false` | Force a full rebuild |
| `--include-tests` | Index test files |
| `--go-pattern ./...` | Scope Go packages |
| `--include-semantic-anchors` | Include semantic anchor concept nodes |
| `--include-cochange` | Include empirical git co-change file edges |

### Enrich with summaries

Repo index build does not attach summaries. Generate and attach them separately:

```bash
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
foxctl index repo enrich summaries --workspace .
```

### Search the graph

```bash
# Full-text search
foxctl index repo search --workspace . --query "Supervisor" --limit 10

# Open a specific node by ID
foxctl index repo open --workspace . --id "<node-id>"

# Ask a natural language question (LLM tool loop over repoindex)
foxctl index repo ask --workspace . --question "Where is repoindex built?"
```

### Expand relationships

```bash
foxctl index repo expand --workspace . \
  --seed "<node-id>" \
  --edge CALLS --edge REFERS_TO \
  --direction out \
  --depth 2 \
  --budget 50
```

Expand parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `--seed` | — | Starting node ID (required) |
| `--edge` | All edges | Edge types to traverse (repeatable) |
| `--direction` | `out` | `out` or `in` |
| `--depth` | `1` | Traversal depth |
| `--budget` | `50` | Maximum nodes to return |

### Check index status

```bash
foxctl index repo status --workspace .
```

Index databases are stored at `~/.foxctl/storage/repoindex/<repo>-repoindex-<hash>.db`.

## DAG grep

DAG grep produces a compact explanation subgraph in one call. It is the better default when the question is about graph relationships rather than text snippets.

```bash
foxctl run code/dag_grep --input '{
  "query": "buildEvidencePack",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80,
  "k": 5
}'
```

DAG grep parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `query` | — | Search query for seed nodes |
| `workspace` | `.` | Workspace root |
| `render` | — | `tree` or `mermaid` output format |
| `edge_sets` | — | Edge sets to include (e.g., `["structural"]`) |
| `depth` | `2` | Traversal depth |
| `budget` | `80` | Maximum nodes to return |
| `k` | `10` | Number of seed nodes |
| `mode` | `hybrid` | Search mode: `fts`, `semantic`, or `hybrid` |

## Pipeline pattern

Chain tools for comprehensive code understanding:

```
semantic_search → snippet_extract → analysis
   "where"           "what"          "meaning"
```

```bash
# 1. Find candidates with semantic search
foxctl run code/semantic_search --input '{"query": "error handling"}'

# 2. Extract snippets from the candidates
foxctl run code/snippet_extract --input '{
  "query": "error handling patterns",
  "candidates": [
    {"path": "internal/errors/handler.go", "line": 45}
  ]
}'

# 3. Or use smart search for steps 1+2 combined
foxctl run code/smart_search --input '{"query": "error handling patterns"}'

# 4. For relationship questions, build the index and use DAG grep
foxctl index repo build --workspace . --go --typescript
foxctl run code/dag_grep --input '{"query": "HandleError", "render": "tree", "depth": 2}'
```

## Common failure modes

| Symptom | Likely cause |
|---------|--------------|
| Empty semantic matches for vector-only scopes | Missing or unreachable OpenAI-compatible embedding endpoint |
| Dimension mismatch | Model/store dimension drift; rebuild the affected scope |
| Rerank unavailable | Missing key or `FOXCTL_RERANK_ENABLED` not set |
| Empty repoindex results | Index not built or stale; rebuild with `foxctl index repo build` |
| Missing TypeScript edges | TypeScript adds heuristic `CALLS` edges; ambiguous targets are skipped |

## Terminology

When the documentation or agents say "index this," assume they mean making the material queryable to agents through repoindex and related semantic/vector stores.

- **Repoindex**: The per-workspace graph behind `foxctl index repo ...` and `repo_index_*` tools.
- **Symbol**: A named code declaration (function, type, method) that repoindex can navigate.
- **Node**: A graph record — commonly a package, file, symbol, or concept.
- **Edge**: A typed relationship between nodes (e.g., `CONTAINS`, `CALLS`, `REFERS_TO`).
- **`Index:` comment block**: Structured source metadata for discoverability and soft graph edges.
- **Semantic anchor**: A typed `[[type:slug]]` evidence marker near a strong owner — evidence for retrieval, not instructions.
