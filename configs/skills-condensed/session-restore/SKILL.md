---
name: Session Restore
description: Restore context from a saved session to continue work
---

# Session Restore

Restores session state after compaction via SessionStart hook.

## Usage

```bash
foxctl run session/restore --input '{"trigger": "compact"}'
```

## What Gets Restored

- Active plan title and sections
- Active task with ID
- Pending todos
- Gotchas & learnings
- Session summary

## Output

Sets `FOXCTL_SESSION_RESTORED=true` and injects markdown context.

Full docs: `~/.foxctl/share/configs/skills/session-restore/Skill.md`
