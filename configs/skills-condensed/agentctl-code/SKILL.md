---
name: agentctl Code Analysis
description: Analyze code with agentctl skills - symbols, complexity, diffs, security scanning, and imports.
---

# Code Analysis

Extract insights from codebases using specialized analysis skills.

## Skills

```bash
# Symbols (functions, types, methods)
agentctl run code/symbols --input '{"path": "src/main.go"}'

# Complexity metrics
agentctl run code/complexity --input '{"path": "src/", "recursive": true}'

# Unified diffs
agentctl run code/diff --input '{"old": "file.bak", "new": "file.go"}'

# Security scanning
agentctl run code/security --input '{"path": ".", "recursive": true}'

# Import analysis
agentctl run code/imports --input '{"path": "internal/", "recursive": true}'

# Smart code search
agentctl run code/smart_search --input '{"query": "error handling", "files": ["handler.go"]}'

# Repo graph index
# For TS/Elixir-only repos, add `--go=false` (otherwise Go indexing may fail).
agentctl index repo build --dry-run --workspace . --go --typescript --elixir
agentctl index repo build --workspace . --go --typescript --elixir
agentctl index repo search --workspace . --query "Supervisor"
agentctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO

# DAG grep (explanation subgraph from repoindex)
agentctl run code/dag_grep --input '{"query":"repoindex builder","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'

# Git operations
agentctl run code/git --input '{"action": "log", "count": 10}'
agentctl run code/git --input '{"action": "blame", "path": "main.go"}'
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-code/Skill.md`
