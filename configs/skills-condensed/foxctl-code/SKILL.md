---
name: foxctl Code Analysis
description: Analyze code with foxctl skills - symbols, complexity, diffs, security scanning, and imports.
---

# Code Analysis

Extract insights from codebases using specialized analysis skills.

## Skills

```bash
# Symbols (functions, types, methods)
foxctl run code/symbols --input '{"path": "src/main.go"}'

# Complexity metrics
foxctl run code/complexity --input '{"path": "src/", "recursive": true}'

# Unified diffs
foxctl run code/diff --input '{"old": "file.bak", "new": "file.go"}'

# Security scanning
foxctl run code/security --input '{"path": ".", "recursive": true}'

# Import analysis
foxctl run code/imports --input '{"path": "internal/", "recursive": true}'

# Smart code search
foxctl run code/smart_search --input '{"query": "error handling", "files": ["handler.go"]}'

# Repo graph index
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
foxctl index repo build --dry-run --workspace . --go --typescript --elixir
foxctl index repo build --workspace . --go --typescript --elixir
foxctl index repo search --workspace . --query "Supervisor"
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO

# DAG grep (explanation subgraph from repoindex)
foxctl run code/dag_grep --input '{"query":"repoindex builder","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'

# Git operations
foxctl run code/git --input '{"action": "log", "count": 10}'
foxctl run code/git --input '{"action": "blame", "path": "main.go"}'
```

Full docs: `~/.foxctl/share/configs/skills/foxctl-code/Skill.md`
