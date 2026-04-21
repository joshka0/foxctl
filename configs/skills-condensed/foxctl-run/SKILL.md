---
name: foxctl Run
description: Execute foxctl skills with JSON input. Use for file ops, code analysis, text search, and structured workflows.
---

# Skill Runner

Execute any foxctl skill with JSON I/O.

## Usage

```bash
foxctl run <skill-name> --input '<json>'
```

Use `foxctl run` for job-tracked installed skills, `foxctl run --ephemeral` when
job storage is not writable, and `foxctl skills run` for direct parameter flags.
Use `--input-file -` for piped raw JSON and `--input stdin` to chain envelope
`data` between skills.

## Skills

| Category | Skills |
|----------|--------|
| **File System** | `fs/ls`, `fs/read`, `fs/write`, `fs/tree`, `fs/find` |
| **Code** | `code/diff`, `code/symbols`, `code/imports`, `code/security`, `code/smart_search`, `code/context_grep` |
| **Text** | `text/grep`, `text/replace` |
| **Git** | `git/status`, `code/git` |
| **Data** | `data/jq`, `json/transform` |
| **Tasks** | `todo/manage` |

## Output

```json
{"version": 1, "status": "ok", "command": "<skill>", "data": {...}}
```

Large outputs stored in CAS with `data.artifact` digest.

Full docs: `~/.foxctl/share/configs/skills/foxctl-run/Skill.md`
