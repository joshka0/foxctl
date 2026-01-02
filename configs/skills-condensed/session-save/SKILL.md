---
name: Session Save
description: Save current session context to agentctl memory before compaction
---

# Session Save

Captures session state before compaction via PreCompact hook.

## Usage

```bash
agentctl run session/save --input '{"trigger": "manual", "summary": "Working on X"}'
```

## What Gets Captured

- Active task and todos
- Active plan from `~/.claude/plans/`
- Gotchas and learnings
- Key decisions
- Session summary

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `trigger` | string | `pre_compact`, `manual`, `session_end` |
| `summary` | string | Optional user summary |

Full docs: `~/repos/personal/agentctl/configs/skills/session-save/Skill.md`
