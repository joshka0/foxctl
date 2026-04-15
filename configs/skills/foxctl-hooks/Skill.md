---
name: foxctl Hooks
description: Claude Code hooks for task guard, knowledge routing, mail routing, test feedback, and file guard.
---

# foxctl Hooks System

Hooks intercept Claude Code tool operations for coordination and automation.

## Active Hooks

### PreToolUse Hooks

**task-guard** (`hooks/task_guard`)

- Triggers on: Edit, Write, MultiEdit, NotebookEdit
- Purpose: Ensures active task exists before writes
- Modes:
  - `auto` (default) - Auto-creates task if none exists
  - `strict` - Blocks writes without active task

**knowledge-router** (`hooks/knowledge_router`)

- Triggers on: All operations
- Purpose: Surfaces relevant knowledge packs based on prompt keywords
- Output: Injects context hints for matching knowledge

**mail-router** (`hooks/mail_router`)

- Triggers on: All operations
- Purpose: Surfaces unread mailbox messages
- Priority: Admin > Overseer > Peer messages

**file-guard** (`hooks/file_guard`)

- Triggers on: Edit, Write, MultiEdit, NotebookEdit
- Purpose: Checks file reservations before edits
- Modes:
  - `advisory` - Warns on conflicts
  - `strict` - Blocks conflicting edits

### PostToolUse Hooks

**test-feedback** (`hooks/test_feedback`)

- Triggers on: Edit, Write, MultiEdit, NotebookEdit
- Purpose: Runs tests after code changes
- Output: Test results injected into context

### PreCompact Hooks

**session-save** (`session/save`)

- Triggers on: auto, manual compaction
- Purpose: Captures session state before context compaction
- Captures:
  - Active task and pending todos
  - Active plan from `~/.claude/plans/`
  - Gotchas and learnings from tasks
  - Session summary

### SessionStart Hooks

**session-restore** (`session/restore`)

- Triggers on: compact, resume events
- Purpose: Restores context after compaction
- Output:
  - Injects markdown context with plan, tasks, gotchas
  - Sets `FOXCTL_SESSION_RESTORED=true` env var
  - Sets `FOXCTL_SNAPSHOT_ID` env var

### Stop Hooks

**plan-sync** (`plan/sync`)

- Triggers on: session end
- Purpose: Syncs Claude Code plans from `~/.claude/plans/`
- Features:
  - Detects plan changes via content hash
  - Tracks sync state in memory store
  - Can import plan steps as tasks (when `import_tasks: true`)

## Hook Input Format

```json
{
	"event": "PreToolUse",
	"workspace_root": "/path/to/project",
	"session_id": "session-123",
	"tool_name": "Edit",
	"tool_input": {
		"file_path": "src/main.go",
		"old_string": "...",
		"new_string": "..."
	}
}
```

## Hook Output Format

```json
{
	"decision": "approve|block",
	"reason": "Explanation",
	"context": "Additional context for Claude",
	"meta": {
		"task_id": "...",
		"created": true
	}
}
```

## Decision Types

- `approve` - Allow operation to proceed
- `block` - Prevent operation with reason
- `none` - No action (advisory only)

## Environment Variables

| Variable                   | Description        | Default  |
| -------------------------- | ------------------ | -------- |
| `FOXCTL_TASK_GUARD_MODE` | auto or strict     | auto     |
| `FOXCTL_FILE_GUARD_MODE` | advisory or strict | advisory |
| `FOXCTL_BIN`             | Path to foxctl   | foxctl |

## Running Hooks Manually

```bash
# Test task guard
echo '{"tool_name":"Edit","tool_input":{"file_path":"test.go"}}' | \
  foxctl run hooks/task_guard --input-file -

# Test knowledge router
echo '{"tool_name":"Edit","prompt":"authentication"}' | \
  foxctl run hooks/knowledge_router --input-file -
```

## Hook Configuration

Hooks are configured in `.claude/settings.json`:

```json
{
	"hooks": {
		"PreToolUse": [
			{
				"matcher": "Edit|Write|MultiEdit",
				"hooks": [
					{
						"type": "command",
						"command": ".claude/hooks/task-guard.sh"
					}
				]
			}
		]
	}
}
```

## Task Status Demotion

When editing a task in `ready_for_review` or `completed` status:

1. Task guard detects reviewed status
2. Demotes task back to `in_progress`
3. Marks previous review as stale
