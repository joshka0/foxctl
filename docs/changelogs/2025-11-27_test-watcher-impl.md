# Test Watcher Implementation

**Date:** 2025-11-27\
**Spec:** `docs/spec/test_watch_feedback.md`

## Summary

Implemented the test watcher and feedback hook feature per the spec. This
enables foxctl to run test commands in watch mode and surface failing test
results to Claude via a PostToolUse hook.

## New Components

### Storage Layer (`internal/storage/testwatch/`)

- **`store.go`** — SQLite-backed store for test status per (workspace, watcher)
- **`config.go`** — YAML config parsing for `.foxctl/test-watch.yaml`
- **`store_test.go`** — Unit tests for store and config

### Runtime (`internal/tooling/testwatch/`)

- **`runner.go`** — Test watcher runtime with:
  - File change detection via fsnotify
  - Debouncing and min-interval throttling
  - Test output parsing for Go, pytest, and jest
  - Status persistence to SQLite

### CLI Commands

- **`foxctl test-watch list`** — List configured watchers
- **`foxctl test-watch add`** — Add or update a watcher
- **`foxctl test-watch remove`** — Remove a watcher
- **`foxctl watch tests`** — Run watchers (watch mode, --once, --status-only)

### Hook Skill (`skills/hooks_test_feedback/`)

- **`main.go`** — PostToolUse hook that surfaces failing tests
- **`skill.yaml`** — Skill manifest

### Hook Integration

- **`.claude/hooks/test-feedback.sh`** — Bash wrapper for the hook
- **`.claude/settings.json`** — Added PostToolUse hook configuration

## Configuration

Per-workspace config lives in `.foxctl/test-watch.yaml`:

```yaml
debounce: 2s
watchers:
  - id: go
    command: go test ./...
    include:
      - "**/*.go"
    debounce: 2s
    min_interval: 20s
```

## Usage

```bash
# Configure a Go watcher
foxctl test-watch add --id go --command "go test ./..." --include "**/*.go"

# Start watching
foxctl watch tests

# Run once and exit
foxctl watch tests --once

# Check status only
foxctl watch tests --status-only
```

## Database

Test status stored in `~/.foxctl/storage/test_watch.db`:

| Column        | Type | Description                     |
| ------------- | ---- | ------------------------------- |
| workspace_id  | TEXT | SHA256-derived workspace ID     |
| watcher_id    | TEXT | Watcher identifier (e.g., "go") |
| status        | TEXT | pass/fail/error/running         |
| command       | TEXT | Test command                    |
| started_at    | TEXT | ISO timestamp                   |
| finished_at   | TEXT | ISO timestamp                   |
| summary       | TEXT | Human-readable summary          |
| failures_json | TEXT | JSON array of failures          |
| raw_tail      | TEXT | Last 16KB of output             |

## Future Work

- [ ] Embedding-based test relevance scoring
- [ ] Test file ↔ source file mapping
- [ ] Integration with CI status
