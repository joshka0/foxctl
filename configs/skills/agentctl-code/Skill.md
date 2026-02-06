---
name: agentctl Code Analysis
description: Analyze code with agentctl skills - symbols, complexity, diffs, security scanning, and imports.
---

# Code Analysis with agentctl

Extract insights from codebases using specialized analysis skills.

## Symbols Extraction

Extract functions, classes, types, and their signatures:

```bash
agentctl run code/symbols --input '{"path": "src/main.go"}'
```

Output includes:

- Function names and signatures
- Class/struct definitions
- Type declarations
- Method receivers

## Code Complexity

Analyze cyclomatic complexity and other metrics:

```bash
agentctl run code/complexity --input '{"path": "src/", "recursive": true}'
```

## Unified Diffs

Generate diffs between files or versions:

```bash
agentctl run code/diff --input '{"old": "file.go.bak", "new": "file.go", "format": "unified"}'
```

## Security Scanning

Detect potential security issues:

```bash
agentctl run code/security --input '{"path": ".", "recursive": true}'
```

Checks for:

- Hardcoded secrets
- SQL injection patterns
- Unsafe function calls
- Path traversal risks

## Import Analysis

Map dependencies and import relationships:

```bash
agentctl run code/imports --input '{"path": "internal/", "recursive": true}'
```

## SWE Grep

Extract high-signal code snippets from candidates:

```bash
agentctl run code/smart_search --input '{"query": "error handling", "files": ["handler.go", "service.go"]}'
```

## Repo Graph Index

Navigate code by relationships (calls, references, imports):

```bash
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
agentctl index repo build --dry-run --workspace . --go --typescript --elixir
agentctl index repo build --workspace . --go --typescript --elixir
agentctl index repo search --workspace . --query "Supervisor"
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO
```

## DAG Grep (Repo Graph Explanation)

Use `code/dag_grep` when you want a small explanation subgraph in one call (similar to `code/context_grep`, but for repoindex):

```bash
agentctl run code/dag_grep --input '{
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
agentctl run code/git --input '{"action": "log", "count": 10}'

# File blame
agentctl run code/git --input '{"action": "blame", "path": "main.go"}'

# Diff against branch
agentctl run code/git --input '{"action": "diff", "ref": "main"}'
```
