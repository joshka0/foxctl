# SQLite Task Store and Task Guard Hook

**Date:** 2025-11-26\
**Author:** AI Assistant\
**Branch:** feature/claude-harness

## Summary

Implemented a SQLite-backed task store for `agentctl`'s `todo/manage` skill and
created the `hooks/task_guard` skill for Claude Code integration. This
establishes the foundation for task-centric traceability in the agentctl
ecosystem.

## Changes

### 1. SQLite Task Store (`internal/storage/tasks`)

New package providing persistent task storage with:

- **Store interface** with full CRUD operations:
  - `Add`, `Update`, `Get`, `ListByWorkspace`
  - `GetActive`, `SetActive`, `ClearActive`, `EnsureActive`
- **Task struct** with fields: ID (ULID), WorkspaceID, Title, Description,
  ScopePath, ParentID, Children, DependsOn, Status, CreatedAt, CompletedAt,
  Notes, Gotchas
- **SQLite schema** with `tasks` and `active_tasks` tables
- **Workspace scoping** - tasks are scoped to workspace IDs (typically the
  worktree path)

### 2. Refactored `todo/manage` Skill

Migrated from JSON file storage to SQLite:

- **Breaking change**: Removed `store_path` parameter, added `workspace_id`
- **New operations**: `get_active`, `set_active`, `clear_active`,
  `ensure_active`
- **Preserved envelope shape** for backward compatibility
- **Version bump** to 0.2.0

### 3. Extended CLI (`agentctl todo`)

- Added `agentctl todo active` command to show the active task
- Updated all commands to use `--workspace` instead of `--store`
- Added `--scope` flag to `add` command

### 4. Hook Domain Types (`internal/domain/hook`)

New package defining standardized hook I/O:

```go
type Input struct {
    Event          string          `json:"event"`
    WorkspaceRoot  string          `json:"workspace_root"`
    SessionID      string          `json:"session_id"`
    ToolName       string          `json:"tool_name,omitempty"`
    ToolInput      json.RawMessage `json:"tool_input,omitempty"`
    ToolResponse   json.RawMessage `json:"tool_response,omitempty"`
}

type Output struct {
    Decision Decision       `json:"decision"` // "approve"|"block"|"none"
    Reason   string         `json:"reason,omitempty"`
    Context  string         `json:"context,omitempty"`
    Meta     map[string]any `json:"meta,omitempty"`
}
```

### 5. `hooks/task_guard` Skill

Claude Code PreToolUse hook that enforces task-centric model:

- **Auto mode** (default): Auto-creates tasks when none exist
- **Strict mode**: Blocks write operations without an active task
- **Gated tools**: Edit, Write, MultiEdit, NotebookEdit
- **Task title heuristic**: `<tool> <relative/path>`

### 6. Bash Wrapper (`scripts/hooks/task-guard.sh`)

Claude Code integration script that:

- Transforms Claude hook input to agentctl format
- Calls `agentctl run hooks/task_guard`
- Extracts and returns hook_output

## Usage

### CLI

```bash
# Add a task
agentctl todo add --title "Implement feature X"

# Show active task
agentctl todo active

# List all tasks
agentctl todo list
```

### Claude Code Integration

Add to `.claude/settings.json`:

```json
{
	"hooks": {
		"PreToolUse": [
			{
				"matcher": "Edit|Write|MultiEdit|NotebookEdit",
				"hooks": ["./scripts/hooks/task-guard.sh"]
			}
		]
	}
}
```

### Environment Variables

- `AGENTCTL_TASK_GUARD_MODE`: `auto` (default) or `strict`

## Testing

All tests pass:

- `internal/storage/tasks`: 6 tests
- `skills/todo`: 5 tests
- `skills/hooks_task_guard`: 5 tests

## Migration Notes

- **Breaking**: JSON task storage at `~/.agentctl/todo/tasks.json` is no longer
  used
- **New location**: SQLite database at `~/.agentctl/storage/tasks.db`
- No migration tool provided (app is unreleased)

## Next Steps

1. Implement `hooks/task_update` for PostToolUse context capture
2. Add memory integration for task-scoped context retrieval
3. Wire up additional hooks (Stop, SessionStart)
