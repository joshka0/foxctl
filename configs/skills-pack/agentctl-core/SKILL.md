---
name: agentctl-core
description: "Core agentctl workflow: run skills, read/write files safely, and do fast repo search."
---

## What I do
- Give you a small, reliable “toolbelt” for everyday work (fs + search + run).
- Prefer structured tools over ad-hoc shell.

## When to use me
- You want to inspect or change files safely.
- You need to find where something lives, fast.
- You want to run an agentctl skill but aren’t sure which one.

## Quick start

### 1) Run any skill
```bash
agentctl run <skill> --input '<json>'
```

### 2) File operations
```bash
agentctl run fs/ls   --input '{"path": ".", "all": true, "long": true}'
agentctl run fs/read --input '{"path": "README.md"}'
agentctl run fs/write --input '{"path": "out.txt", "content": "...", "mode": "overwrite"}'
agentctl run fs/tree --input '{"path": ".", "max_depth": 3, "gitignore": true}'
agentctl run fs/find --input '{"path": ".", "pattern": "*.go", "type": "file"}'
```

### 3) Search
Use `text/ripgrep` for fast literal search and `code/context_ripgrep` when you want whole functions/blocks.

```bash
agentctl run text/ripgrep --input '{"pattern": "PathValidator", "path": "."}'
agentctl run code/context_ripgrep --input '{"pattern": "PathValidator", "path": ".", "expand_functions": true}'
```

### 4) JSON plumbing
```bash
agentctl run data/jq --input '{"query": ".data", "input": "<json>"}'
agentctl run json/transform --input '{"operation": "extract", "path": ".data", "input": "<json>"}'
```
