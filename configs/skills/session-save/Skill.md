---
name: Session Save
description: Save current session context to foxctl memory before compaction
---

# Session Save

Captures session state before context compaction for continuity.

## Usage

This skill runs automatically via the PreCompact hook, but can be invoked manually:

```bash
foxctl run session/save --input '{"trigger": "manual", "summary": "Working on feature X"}'
```

## What Gets Captured

- **Active task**: Currently in-progress task with description
- **Pending todos**: Tasks in pending or in_progress status
- **Active plan**: Most recently modified plan from `~/.claude/plans/`
- **Gotchas**: Learnings and gotchas from all relevant tasks
- **Decisions**: Key decisions made during the session
- **Summary**: User-provided or auto-generated session summary

## Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `trigger` | string | Source: `pre_compact`, `manual`, `session_end` |
| `workspace` | string | Project workspace path (default: cwd) |
| `session_id` | string | Optional session ID for tracking |
| `summary` | string | Optional user-provided summary |

## Output

```json
{
  "snapshot_id": "snap-1234567890",
  "items_captured": {
    "active_task": 1,
    "active_plan": 1,
    "pending_todos": 3,
    "gotchas": 2
  },
  "message": "Session snapshot saved"
}
```

## Hook Integration

The `session-save.sh` hook runs on `PreCompact` events:

```json
{
  "matcher": "auto|manual",
  "hooks": [{
    "type": "command",
    "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-save.sh"
  }]
}
```
