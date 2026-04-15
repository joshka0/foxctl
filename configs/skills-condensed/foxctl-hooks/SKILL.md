---
name: foxctl Hooks
description: Claude Code hooks for task guard, knowledge routing, mail routing, test feedback, and file guard.
---

# Hooks System

Intercept Claude Code operations for coordination.

## PreToolUse Hooks

| Hook | Triggers | Purpose |
|------|----------|---------|
| `task-guard` | Edit, Write, MultiEdit | Ensures active task exists |
| `knowledge-router` | All | Surfaces relevant knowledge packs |
| `mail-router` | All | Surfaces unread mailbox messages |
| `file-guard` | Edit, Write, MultiEdit | Checks file reservations |

## PostToolUse Hooks

| Hook | Triggers | Purpose |
|------|----------|---------|
| `test-feedback` | Edit, Write, MultiEdit | Runs tests after changes |

## Decisions

- `approve` - Allow operation
- `block` - Prevent with reason
- `none` - Advisory only

## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `FOXCTL_TASK_GUARD_MODE` | `auto` | `auto` or `strict` |
| `FOXCTL_FILE_GUARD_MODE` | `advisory` | `advisory` or `strict` |

Full docs: `~/.foxctl/share/configs/skills/foxctl-hooks/Skill.md`
