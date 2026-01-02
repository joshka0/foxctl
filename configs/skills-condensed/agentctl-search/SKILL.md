---
name: agentctl Search
description: Fast code search with agentctl using ripgrep and grep skills. Find patterns, files, and code.
---

# Code Search

High-performance search via ripgrep and grep skills.

## Ripgrep

```bash
agentctl run text/ripgrep --input '{"pattern": "func.*Handler", "path": ".", "file_type": "go"}'
```

| Param | Description |
|-------|-------------|
| `pattern` | Regex pattern |
| `path` | Directory or file |
| `file_type` | go, js, py, etc. |
| `case_sensitive` | Boolean |
| `context_lines` | Lines around matches |

## Find Files

```bash
agentctl run fs/find --input '{"path": ".", "pattern": "*.test.go", "type": "file"}'
```

## Directory Tree

```bash
agentctl run fs/tree --input '{"path": "src/", "max_depth": 3}'
```

## Context-Aware

```bash
agentctl run code/context_ripgrep --input '{"query": "auth", "expand_functions": true}'
```

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-search/Skill.md`
