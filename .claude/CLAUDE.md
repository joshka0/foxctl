# Claude Code Integration with agentctl

This project uses **agentctl** as the unified runtime for Claude Code hooks and
skills. All automation flows through agentctl's JSON envelope protocol,
providing:

- **Single binary** - No Python dependencies, ~5ms startup
- **Unified storage** - SQLite-backed tasks, CAS for artifacts, structured
  memory
- **Task-centric model** - All work is tracked via tasks with traceability

## Architecture

```
Claude Code → Hooks (bash wrappers) → agentctl skills → SQLite/CAS
                                           ↓
                                    JSON envelope output
```

## Active Hooks

### PreToolUse: Task Guard

**File:** `.claude/hooks/task-guard.sh`\
**Skill:** `hooks/task_guard`

Enforces task-centric model for write operations (Edit, Write, MultiEdit,
NotebookEdit):

- **Auto mode** (default): Auto-creates a task if none exists
- **Strict mode**: Blocks writes without an active task

Set mode via: `AGENTCTL_TASK_GUARD_MODE=strict`

## Available Skills

### Task Management (`todo/manage`)

```bash
# Add a task
agentctl todo add --title "Implement feature X" --description "Details..."

# List tasks
agentctl todo list

# Show active task
agentctl todo active

# Complete a task
agentctl todo complete --id <task-id> --notes "What was done"
```

### Skill Operations

```bash
# Run any skill directly
agentctl run <skill-name> --input '{"key": "value"}'

# List installed skills
agentctl skills list

# Describe a skill
agentctl skills describe <skill-name>
```

## Task Workflow

1. **Start work**: Create a task with `agentctl todo add --title "..."`
2. **Task guard activates**: PreToolUse hook ensures task exists before writes
3. **Work proceeds**: All edits are associated with the active task
4. **Complete task**: `agentctl todo complete --id <id> --notes "..."`

## Environment Variables

| Variable                   | Description             | Default       |
| -------------------------- | ----------------------- | ------------- |
| `AGENTCTL_TASK_GUARD_MODE` | `auto` or `strict`      | `auto`        |
| `AGENTCTL_HOME`            | Config/storage root     | `~/.agentctl` |
| `AGENTCTL_BIN`             | Path to agentctl binary | `agentctl`    |

## Storage Locations

- **Config**: `~/.agentctl/config.yaml`
- **Tasks DB**: `~/.agentctl/storage/tasks.db`
- **CAS**: `~/.agentctl/cas/sha256/<digest>`
- **Jobs**: `~/.agentctl/jobs/<ulid>/`

## Extending

### Adding a New Hook

1. Create skill in `skills/<name>/main.go`
2. Add `skill.yaml` manifest
3. Create bash wrapper in `.claude/hooks/<name>.sh`
4. Register in `.claude/settings.json`

### Hook Input/Output Contract

**Input** (stdin JSON):

```json
{
	"event": "PreToolUse",
	"workspace_root": "/path/to/project",
	"session_id": "...",
	"tool_name": "Edit",
	"tool_input": { "file_path": "..." }
}
```

**Output** (stdout JSON envelope):

```json
{
	"version": 1,
	"status": "ok",
	"command": "hooks/task_guard",
	"data": {
		"hook_output": {
			"decision": "approve",
			"reason": "task ensured",
			"meta": { "task_id": "...", "created": true }
		}
	}
}
```

## Development

```bash
# Build agentctl
make build

# Run tests
go test ./...

# Build skills
make skills-build
```
