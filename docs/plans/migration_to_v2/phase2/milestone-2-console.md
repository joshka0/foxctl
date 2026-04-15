# Milestone 2: Interactive Actor Console

**Status:** ~85% Complete

## Overview

Consoles are first-class, attachable, and use mailbox transport. Users can interact with multiple actors live, cancel mid-turn, and watch streaming progress.

---

## PR 2.1 — Console Session Registry + Mailbox Protocol

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Console store | `internal/storage/console/store.go` | ✅ Done |
| Console CLI | `cmd/foxctl/cmd/console.go` | ✅ Done |
| Mailbox skill | `skills/mailbox/` | ✅ Done |

### Schema

```sql
-- console_sessions table (implemented)
CREATE TABLE console_sessions (
    console_id TEXT PRIMARY KEY,
    workspace_id TEXT,
    actor_id TEXT,
    session_id TEXT,
    created_at TEXT,
    last_attached_at TEXT,
    meta_json TEXT
);
```

### Mailbox Message Payload (v1)

```jsonc
{
  "type": "ask" | "reply" | "event" | "cmd",
  "actor_id": "...",
  "console_id": "...",
  "correlation_id": "...",
  "content": "...",
  "metadata": { "partial": true, "mime": "text/markdown", "progress": {...} },
  "cmd": { "name": "cancel" }
}
```

### CLI Commands

```bash
# Attach to actor console
foxctl console attach --actor my-coder

# Attach to specific console
foxctl console attach --actor my-coder --console 01JFXYZ...

# List consoles
foxctl console list

# Remove console
foxctl console rm <console-id>
```

---

## PR 2.2 — foxctl_viewer: Tabs + Streaming + Cancel

**Status:** ✅ Mostly Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| TUI main | `cmd/foxctl_viewer/tui.go` | ✅ Done (66k lines) |
| Tabs by console | `cmd/foxctl_viewer/views.go` | ✅ Done |
| Actions (send/cancel) | `cmd/foxctl_viewer/actions.go` | ✅ Done |

### Backpressure Rule

- `max_inflight_correlations_per_console = 1` (hard default)
- Viewer blocks new asks while one is running (unless `--force`)

### Remaining Work

- [ ] Verify streaming `event` chunks render correctly
- [ ] Verify Ctrl+C sends `cmd.cancel` for active correlation

---

## PR 2.3 — Actor Streaming Integration

**Status:** ⚠️ Partial

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Event bus | `internal/runtime/actor/event_bus.go` | ✅ Done |
| Agent actor hooks | `internal/runtime/actor/agent_actor.go` | ✅ Done |
| AgentProgress event | Hook dispatch | ⚠️ Needs verification |

### Expected Behavior

When inbound mailbox message has `console_id`:
- Emit `event` with progress after each tool call
- Emit `reply` with final assistant output on stop

### Remaining Work

- [ ] Verify `AgentProgress` hook event is emitted
- [ ] Wire progress events to console via mailbox
- [ ] Test long-running tasks show progress without LLM prompt spam

---

## PR 2.4 — Trajectory Logging for Console Sessions

**Status:** ✅ Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Trajectory persister | `internal/runtime/actor/trajectory_persister.go` | ✅ Done |
| Trajectory store | `internal/storage/trajectory/` | ✅ Done |
| Export skill | `skills/trajectory_export/` | ✅ Done |

### Event Types Persisted

- `console.ask`
- `console.event`
- `console.reply`
- `console.cancel`

### CLI

```bash
# View console interactions
foxctl trajectory tail --actor <id>
```

---

## Acceptance Criteria

- [x] `foxctl console attach --actor <id>` creates/attaches console session
- [x] Console ask/reply works through mailbox
- [ ] Streaming events render in viewer
- [ ] Cancel interrupts in-progress turn
- [x] Trajectory captures console interactions
