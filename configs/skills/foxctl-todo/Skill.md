---
name: foxctl Todo
description: Manage tasks with foxctl todo commands. Create, list, complete tasks for tracking work.
---

# Task Management with foxctl

Structured task tracking with dependencies and completion notes.

## Commands

### Add a Task

```bash
foxctl todo add --title "Task title" --description "Details"
```

### Add Task with Dependencies

Use `--depends-on` to block a task until dependencies complete:

```bash
foxctl todo add --title "Deploy app" --depends-on 01ABC123 --depends-on 01DEF456
```

Use `--parent` to create subtasks:

```bash
foxctl todo add --title "Write tests" --parent 01ABC123
```

### List Tasks

```bash
foxctl todo list
foxctl todo list --status pending
foxctl todo list --status completed
```

### Show Active Task

```bash
foxctl todo active
```

### Complete a Task

```bash
foxctl todo complete --id <task-id> --notes "What was done"
```

Tasks with incomplete dependencies cannot be completed until their dependencies
are done.

### Update a Task

```bash
foxctl todo update --id <task-id> --title "New title"
```

## Workflow

1. **Before starting work**: Create a task to track the change
2. **During work**: The task-guard hook ensures tasks exist for edits
3. **After completion**: Mark task complete with notes

## Example Session

```bash
# Start a feature
foxctl todo add --title "Implement user auth" --description "Add JWT-based authentication"

# Check what's active
foxctl todo active

# Complete when done
foxctl todo complete --id 01ABC... --notes "Added JWT middleware, login/logout endpoints"
```

## Integration

The PreToolUse `task-guard` hook automatically:

- Creates tasks for edit operations (auto mode)
- Blocks edits without tasks (strict mode)

Set mode: `export AGENTCTL_TASK_GUARD_MODE=strict`

## Plan Integration

Tasks can be linked to Claude Code plans from `~/.claude/plans/`:

```bash
# Import plan steps as tasks
foxctl run plan/sync --input '{"import_tasks": true}'

# List tasks from a specific plan
foxctl todo list  # Tasks show plan_file if linked
```

When `plan/sync` imports tasks, each task includes:
- `plan_file`: Path to the source plan
- `plan_section`: Section hierarchy (e.g., "Phase 1 > Step 1.1")
