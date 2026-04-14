---
name: Plan Sync
description: Sync Claude Code plans and optionally import as tasks
---

# Plan Sync

Sync plans from `~/.claude/plans/` via `foxctl run plan/sync`.

## Usage

```bash
foxctl run plan/sync --input '{"dry_run": true}'
foxctl run plan/sync --input '{"import_tasks": true}'
foxctl run plan/sync --input '{"force": true}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `plan_file` | string | Specific plan to sync |
| `import_tasks` | bool | Create tasks from steps |
| `dry_run` | bool | Preview only |
| `force` | bool | Re-sync even if unchanged |

Runs on Stop hook automatically.

Full docs: `~/.foxctl/share/configs/skills/plan-sync/Skill.md`
