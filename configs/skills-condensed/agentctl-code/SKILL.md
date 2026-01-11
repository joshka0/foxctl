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
agentctl run code/swe_grep --input '{"query": "error handling", "files": ["handler.go"]}'

# Git operations
agentctl run code/git --input '{"action": "log", "count": 10}'
agentctl run code/git --input '{"action": "blame", "path": "main.go"}'
```

Full docs: `~/.agentctl/share/configs/skills/agentctl-code/Skill.md`
