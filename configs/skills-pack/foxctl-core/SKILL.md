---
name: foxctl-core
description: "Core foxctl workflow: run skills, read/write files safely, and do fast repo search."
---

## What I do
- Give you a small, reliable “toolbelt” for everyday work (fs + search + run).
- Prefer structured tools over ad-hoc shell.

## When to use me
- You want to inspect or change files safely.
- You need to find where something lives, fast.
- You want to run an foxctl skill but aren’t sure which one.

## Quick start

### 1) Run any skill
```bash
foxctl run <skill> --input '<json>'
```

### 2) File operations
```bash
foxctl run fs/ls   --input '{"path": ".", "all": true, "long": true}'
foxctl run fs/read --input '{"path": "README.md"}'
foxctl run fs/write --input '{"path": "out.txt", "content": "...", "mode": "overwrite"}'
foxctl run fs/tree --input '{"path": ".", "max_depth": 3, "gitignore": true}'
foxctl run fs/find --input '{"path": ".", "pattern": "*.go", "type": "file"}'
```

### 3) Search
Use `text/grep` for fast literal search and `code/context_grep` when you want whole functions/blocks.

```bash
foxctl run text/grep --input '{"pattern": "PathValidator", "path": "."}'
foxctl run code/context_grep --input '{"pattern": "PathValidator", "path": ".", "expand_functions": true}'
```

### 4) JSON plumbing
```bash
foxctl run data/jq --input '{"query": ".data", "input": "<json>"}'
foxctl run json/transform --input '{"operation": "extract", "path": ".data", "input": "<json>"}'
```
