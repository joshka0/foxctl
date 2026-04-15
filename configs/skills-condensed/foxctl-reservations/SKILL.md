---
name: foxctl File Reservations
description: Advisory file locking for multi-agent coordination. Reserve files, check conflicts, prevent concurrent edits.
---

# File Reservations

Prevent concurrent edit conflicts via `foxctl run mailbox`.

## Usage

```bash
foxctl run mailbox --input '{"operation": "<op>", ...}'
```

## Operations

| Op | Params | Description |
|----|--------|-------------|
| `reserve` | `workspace_id`, `actor_id`, `paths[]`, `mode`, `task_id?`, `reason?`, `duration_minutes?` | Reserve files |
| `check_conflicts` | `workspace_id`, `actor_id`, `paths[]`, `mode` | Check for conflicts |
| `release` | `workspace_id`, `actor_id`, `paths[]` | Release reservations |
| `list_reservations` | `workspace_id` | List active reservations |

## Modes

- `exclusive` - Only holder can edit
- `shared` - Multiple readers allowed

## Integration

`hooks/file_guard` auto-checks conflicts before writes.

Set mode: `FOXCTL_FILE_GUARD_MODE=strict`

Full docs: `~/.foxctl/share/configs/skills/foxctl-reservations/Skill.md`
