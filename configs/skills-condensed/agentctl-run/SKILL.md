---
name: agentctl Run
description: Execute agentctl skills with JSON input. Use for file ops, code analysis, text search, and structured workflows.
---

# Skill Runner

Execute any agentctl skill with JSON I/O.

## Usage

```bash
agentctl run <skill-name> --input '<json>'
```

## Skills

| Category | Skills |
|----------|--------|
| **File System** | `fs/ls`, `fs/read`, `fs/write`, `fs/tree`, `fs/find` |
| **Code** | `code/diff`, `code/symbols`, `code/complexity`, `code/imports`, `code/security`, `code/smart_search` |
| **Text** | `text/grep`, `text/ripgrep`, `text/replace` |
| **Git** | `git/status`, `code/git` |
| **Data** | `data/jq`, `json/transform` |
| **Tasks** | `todo/manage` |

## Output

```json
{"version": 1, "status": "ok", "command": "<skill>", "data": {...}}
```

Large outputs stored in CAS with `data.artifact` digest.

Full docs: `~/.agentctl/share/configs/skills/agentctl-run/Skill.md`
