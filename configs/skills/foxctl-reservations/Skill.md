---
name: foxctl File Reservations
description: Advisory file locking for multi-agent coordination. Reserve files, check conflicts, prevent concurrent edits.
---

# File Reservations with foxctl

Prevent concurrent edit conflicts between agents using advisory locks.

## Reserve Files

```bash
foxctl run mailbox --input '{
  "operation": "reserve",
  "reserve": {
    "workspace_id": "/path/to/workspace",
    "actor_id": "actor:coder:agent1",
    "paths": ["src/auth.go", "src/handler.go"],
    "mode": "exclusive",
    "task_id": "01ABC...",
    "reason": "Implementing auth feature",
    "duration_minutes": 30
  }
}'
```

Modes:

- `exclusive` - Only holder can edit (conflicts with all others)
- `shared` - Multiple readers allowed (only conflicts with exclusive)

## Check for Conflicts

Before editing, check if files are reserved:

```bash
foxctl run mailbox --input '{
  "operation": "check_conflicts",
  "check_conflicts": {
    "workspace_id": "/path/to/workspace",
    "actor_id": "actor:coder:agent2",
    "paths": ["src/auth.go"],
    "mode": "exclusive"
  }
}'
```

Response indicates any conflicting reservations.

## Release Reservations

```bash
foxctl run mailbox --input '{
  "operation": "release",
  "release": {
    "workspace_id": "/path/to/workspace",
    "actor_id": "actor:coder:agent1",
    "paths": ["src/auth.go"]
  }
}'
```

## List Active Reservations

```bash
foxctl run mailbox --input '{
  "operation": "list_reservations",
  "list_reservations": {
    "workspace_id": "/path/to/workspace"
  }
}'
```

## Automatic Expiration

Reservations expire after the specified duration (default: 10 minutes). Expired
reservations are automatically cleaned up on conflict checks.

## Hooks Integration

The `hooks/file_guard` hook automatically:

1. Checks for conflicts before write operations
2. Creates reservations for the editing agent
3. Blocks or warns based on mode (strict vs advisory)

Set mode: `export FOXCTL_FILE_GUARD_MODE=strict`
