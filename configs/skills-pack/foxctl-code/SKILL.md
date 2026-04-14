---
name: foxctl-code
description: "Code intelligence and analysis: symbols, diffs, complexity, imports, security scan, semantic search, and repo graph DAG grep."
---

## What I do
- Help you answer “where is X?” and “how does X work?” with structure-first tooling.

## When to use me
- You’re about to edit non-trivial code and want to understand the surface area.
- You need to find related code (conceptual search), not just exact strings.

## Common commands

### Symbols / structure
```bash
foxctl run code/symbols --input '{"path": "internal/agent/daemon/daemon.go"}'
```

### Diffs
```bash
foxctl run code/diff --input '{"staged": true}'
foxctl run code/diff --input '{"base": "origin/main", "head": "HEAD"}'
```

### Imports / dependency graph
```bash
foxctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

### Security scan
```bash
foxctl run code/security --input '{"path": ".", "recursive": true}'
```

### Semantic search
```bash
foxctl run code/semantic_search --input '{"query": "repoindex", "format": "tree", "limit": 25}'
```

### Repo graph index
```bash
# Build the repo graph index (dry-run first; this writes to the repoindex DB).
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
foxctl index repo build --dry-run --workspace . --go --typescript --elixir
foxctl index repo build --workspace . --go --typescript --elixir

foxctl index repo search --workspace . --query "repoindex"
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO
```

### DAG grep (repo graph explanation subgraph)
Use this when you want a small explanation subgraph in one call (similar to `code/context_grep`, but for repoindex).

```bash
foxctl run code/dag_grep --input '{
  "query": "repoindex built",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80,
  "k": 5
}'
```

Notes:
- TypeScript adds heuristic `CALLS` edges; Elixir adds heuristic `REFERS_TO` edges. These are best-effort (no type-checking) and conservative (ambiguous targets are skipped).
