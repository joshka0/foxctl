---
name: agentctl-code
description: "Code intelligence and analysis: symbols, diffs, complexity, imports, security scan, and semantic search."
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
agentctl run code/semantic_search --input '{"query": "where is task guard implemented", "format": "tree", "limit": 25}'
```
