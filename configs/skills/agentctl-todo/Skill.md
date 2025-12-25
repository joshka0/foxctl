---
name: agentctl Todo
description: Manage tasks with agentctl todo commands. Create, list, complete tasks for tracking work.
---

# Task Management with agentctl

Structured task tracking with dependencies and completion notes.

## Commands

### Add a Task

```bash
agentctl todo add --title "Task title" --description "Details"
```

### Add Task with Dependencies

Use `--depends-on` to block a task until dependencies complete:

```bash
agentctl todo add --title "Deploy app" --depends-on 01ABC123 --depends-on 01DEF456
```

Use `--parent` to create subtasks:

```bash
agentctl todo add --title "Write tests" --parent 01ABC123
```

### List Tasks

```bash
agentctl todo list
agentctl todo list --status pending
agentctl todo list --status completed
```

### Show Active Task

```bash
agentctl todo active
```

### Complete a Task

```bash
agentctl todo complete --id <task-id> --notes "What was done"
```

Tasks with incomplete dependencies cannot be completed until their dependencies
are done.

### Update a Task

```bash
agentctl todo update --id <task-id> --title "New title"
```

## Workflow

1. **Before starting work**: Create a task to track the change
2. **During work**: The task-guard hook ensures tasks exist for edits
3. **After completion**: Mark task complete with notes

## Example Session

```bash
# Start a feature
agentctl todo add --title "Implement user auth" --description "Add JWT-based authentication"

# Check what's active
agentctl todo active

# Complete when done
agentctl todo complete --id 01ABC... --notes "Added JWT middleware, login/logout endpoints"
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
agentctl run plan/sync --input '{"import_tasks": true}'

# List tasks from a specific plan
agentctl todo list  # Tasks show plan_file if linked
```

When `plan/sync` imports tasks, each task includes:
- `plan_file`: Path to the source plan
- `plan_section`: Section hierarchy (e.g., "Phase 1 > Step 1.1")
