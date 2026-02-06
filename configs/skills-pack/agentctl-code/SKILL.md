---
name: agentctl-code
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
agentctl run code/symbols --input '{"path": "internal/agent/daemon/daemon.go"}'
```

### Diffs
```bash
agentctl run code/diff --input '{"staged": true}'
agentctl run code/diff --input '{"base": "origin/main", "head": "HEAD"}'
```

### Imports / dependency graph
```bash
agentctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

### Security scan
```bash
agentctl run code/security --input '{"path": ".", "recursive": true}'
```

### Semantic search
```bash
agentctl run code/semantic_search --input '{"query": "repoindex", "format": "tree", "limit": 25}'
```

### Repo graph index
```bash
# Build the repo graph index (dry-run first; this writes to the repoindex DB).
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
agentctl index repo build --dry-run --workspace . --go --typescript --elixir
agentctl index repo build --workspace . --go --typescript --elixir

agentctl index repo search --workspace . --query "repoindex"
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO
```

### DAG grep (repo graph explanation subgraph)
Use this when you want a small explanation subgraph in one call (similar to `code/context_grep`, but for repoindex).

```bash
agentctl run code/dag_grep --input '{
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
