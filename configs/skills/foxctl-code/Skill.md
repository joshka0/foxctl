---
name: foxctl Code Analysis
description: Analyze code with foxctl skills - symbols, complexity, diffs, security scanning, and imports.
---

# Code Analysis with foxctl

Extract insights from codebases using specialized analysis skills.

## Symbols Extraction

Extract functions, classes, types, and their signatures:

```bash
foxctl run code/symbols --input '{"path": "src/main.go"}'
```

Output includes:

- Function names and signatures
- Class/struct definitions
- Type declarations
- Method receivers

## Code Complexity

Analyze cyclomatic complexity and other metrics:

```bash
foxctl run code/complexity --input '{"path": "src/", "recursive": true}'
```

## Unified Diffs

Generate diffs between files or versions:

```bash
foxctl run code/diff --input '{"old": "file.go.bak", "new": "file.go", "format": "unified"}'
```

## Security Scanning

Detect potential security issues:

```bash
foxctl run code/security --input '{"path": ".", "recursive": true}'
```

Checks for:

- Hardcoded secrets
- SQL injection patterns
- Unsafe function calls
- Path traversal risks

## Import Analysis

Map dependencies and import relationships:

```bash
foxctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

## SWE Grep

Extract high-signal code snippets from candidates:

```bash
foxctl run code/smart_search --input '{"query": "error handling", "files": ["handler.go", "service.go"]}'
```

## Repo Graph Index

Navigate code by relationships (calls, references, imports):

```bash
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
foxctl index repo build --dry-run --workspace . --go --typescript --elixir
foxctl index repo build --workspace . --go --typescript --elixir
foxctl index repo search --workspace . --query "Supervisor"
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO
```

## DAG Grep (Repo Graph Explanation)

Use `code/dag_grep` when you want a small explanation subgraph in one call (similar to `code/context_grep`, but for repoindex):

```bash
foxctl run code/dag_grep --input '{
  "query": "repoindex builder",
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

## Git Operations

```bash
# Recent commits
foxctl run code/git --input '{"action": "log", "count": 10}'

# File blame
foxctl run code/git --input '{"action": "blame", "path": "main.go"}'

# Diff against branch
foxctl run code/git --input '{"action": "diff", "ref": "main"}'
```
