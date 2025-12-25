---
name: Plan Sync
description: Sync Claude Code plans and optionally import as tasks
---

# Plan Sync

Syncs Claude Code plans from `~/.claude/plans/` and optionally imports plan steps as agentctl tasks.

## Usage

```bash
# Dry run - preview what would change
agentctl run plan/sync --input '{"dry_run": true}'

# Sync plans (detect changes)
agentctl run plan/sync --input '{}'

# Sync and import steps as tasks
agentctl run plan/sync --input '{"import_tasks": true}'

# Force re-sync even if unchanged
agentctl run plan/sync --input '{"force": true}'

# Sync specific plan file
agentctl run plan/sync --input '{"plan_file": "~/.claude/plans/my-plan.md"}'
```

## Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `workspace` | string | Project workspace path (default: cwd) |
| `plan_file` | string | Specific plan to sync (optional) |
| `import_tasks` | boolean | Create tasks from plan steps |
| `dry_run` | boolean | Preview changes without applying |
| `force` | boolean | Re-sync even if hash unchanged |

## Output

```json
{
  "plans_processed": 4,
  "plans_changed": 2,
  "tasks_created": 15,
  "dry_run": false,
  "results": [
    {
      "plan_file": "~/.claude/plans/my-plan.md",
      "plan_title": "Feature Implementation",
      "content_hash": "sha256:abc...",
      "status": "synced",
      "tasks_created": 8,
      "steps": [...]
    }
  ],
  "message": "Synced 4 plans, 2 changed, 15 tasks created"
}
```

## Status Values

| Status | Description |
|--------|-------------|
| `created` | First time syncing this plan |
| `synced` | Plan changed since last sync |
| `unchanged` | Plan hash matches, no changes |
| `error` | Failed to process plan |

## Hook Integration

The `plan-sync.sh` hook runs on `Stop` events:

```json
{
  "matcher": "",
  "hooks": [{
    "type": "command",
    "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/plan-sync.sh"
  }]
}
```

## Task Import

When `import_tasks: true`, each plan step becomes a task with:

- **title**: Step title from plan
- **description**: Step content
- **plan_file**: Path to source plan
- **plan_section**: Section hierarchy (e.g., "Phase 1 > Step 1.1")
- **depends_on**: Dependencies if specified in plan

Existing tasks (matched by plan_section) are skipped to avoid duplicates.
