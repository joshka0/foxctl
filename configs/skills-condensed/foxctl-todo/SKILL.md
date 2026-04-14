---
name: foxctl Todo
description: Manage tasks with foxctl todo commands. Create, list, complete tasks for tracking work.
---

# Task Management

Track work with dependencies and completion notes.

## Commands

```bash
# Add task
foxctl todo add --title "Task" --description "Details"

# Add with dependency
foxctl todo add --title "Deploy" --depends-on 01ABC123

# Add subtask
foxctl todo add --title "Write tests" --parent 01ABC123

# List tasks
foxctl todo list [--status pending|completed]

# Show active
foxctl todo active

# Complete task
foxctl todo complete --id <id> --notes "What was done"

# Update task
foxctl todo update --id <id> --title "New title"
```

## Integration

`hooks/task_guard` ensures tasks exist for edits.

Set mode: `AGENTCTL_TASK_GUARD_MODE=strict`

Full docs: `~/.foxctl/share/configs/skills/foxctl-todo/Skill.md`
