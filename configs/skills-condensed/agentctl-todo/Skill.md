---
name: agentctl Todo
description: Manage tasks with agentctl todo commands. Create, list, complete tasks for tracking work.
---

# Task Management

Track work with dependencies and completion notes.

## Commands

```bash
# Add task
agentctl todo add --title "Task" --description "Details"

# Add with dependency
agentctl todo add --title "Deploy" --depends-on 01ABC123

# Add subtask
agentctl todo add --title "Write tests" --parent 01ABC123

# List tasks
agentctl todo list [--status pending|completed]

# Show active
agentctl todo active

# Complete task
agentctl todo complete --id <id> --notes "What was done"

# Update task
agentctl todo update --id <id> --title "New title"
```

## Integration

`hooks/task_guard` ensures tasks exist for edits.

Set mode: `AGENTCTL_TASK_GUARD_MODE=strict`

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-todo/Skill.md`
