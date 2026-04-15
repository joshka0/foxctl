---
name: Session Restore
description: Restore context from a saved session to continue work
---

# Session Restore

Restores session state after context compaction with context injection.

## Usage

This skill runs automatically via the SessionStart hook, but can be invoked
manually:

```bash
foxctl run session/restore --input '{"trigger": "compact"}'
```

## What Gets Restored

The skill injects a markdown context block containing:

- **Active Plan**: Title and section headers from `~/.claude/plans/`
- **Active Task**: Current task with ID and description
- **Pending Work**: List of pending/in-progress todos
- **Gotchas & Learnings**: Important notes from tasks
- **Key Decisions**: Decisions made in previous session
- **Session Summary**: Overview of previous work

## Parameters

| Parameter   | Type   | Description                            |
| ----------- | ------ | -------------------------------------- |
| `trigger`   | string | Source: `compact`, `resume`, `startup` |
| `workspace` | string | Project workspace path (default: cwd)  |

## Output

```json
{
  "hook_output": {
    "decision": "approve",
    "reason": "Restored session snapshot from 5m ago",
    "context": "## Session Continuity Context\n...",
    "env": {
      "FOXCTL_SESSION_RESTORED": "true",
      "FOXCTL_SNAPSHOT_ID": "snap-1234567890"
    }
  },
  "snapshot_id": "snap-1234567890",
  "snapshot_age": "5m",
  "items_restored": 4
}
```

## Hook Integration

The `session-restore.sh` hook runs on `SessionStart` events:

```json
{
  "matcher": "compact|resume",
  "hooks": [{
    "type": "command",
    "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/session-restore.sh"
  }]
}
```

### Lineage & Identity Notes

- Single active session per workspace/agent; start/resume/fork refuse if another
  is active unless `--force`.
- Active is determined by status (e.g., `running`); terminal statuses set
  `ended_at`, non-terminal clear it on reopen.
- Identity fallback file: `~/.foxctl/sessions/active/<workspace_hash>.json`
  (stores `session_id`, `agent_id`, lineage) for hooks without env access.
- Env to skills: `FOXCTL_SESSION_ID`, `FOXCTL_AGENT_ID` + fallbacks
  (`CLAUDE_SESSION_ID`, `OPENCODE_SESSION_ID`, `CURSOR_SESSION_ID`,
  `TERM_SESSION_ID`) are forwarded by exec/WASI runners so restored sessions
  attribute correctly.
- Lineage inspection: `foxctl sessions chain --session <id>`; trajectories
  store `session_id` for joins.

## Context Format

The injected context follows this structure:

```markdown
## Session Continuity Context

_Restored after compact (snapshot from 5m ago)_

### Active Plan

**Plan Title** (`plan-file.md`) Sections:

- Section 1
- Section 2

### Active Task

**Task Title** (ID: 01ABC...) Task description here

### Pending Work

- :hourglass: Pending task
- :arrows_counterclockwise: In-progress task

### Gotchas & Learnings

- **Task**: Important learning

---

_Continue where you left off._
```
