# Milestone 3: Blackboard + Task-Linked Multi-Agent

**Status:** ~70% Complete

## Overview

Tasks become the shared substrate for multi-agent coordination. Agents see each other through task-linked dialogue and artifacts via blackboard and context injection.

---

## PR 3.1 — Task Binding to Turns

**Status:** ⚠️ Partial

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| actor_turns table | `internal/storage/sessions/` | ⚠️ Needs task_id column |
| todo.ensure_active | `skills/todo/` | ✅ Done |
| TaskBinding event | Hook dispatch | ⚠️ Needs implementation |

### Schema Change Needed

```sql
-- Add to actor_turns
ALTER TABLE actor_turns ADD COLUMN task_id TEXT;
CREATE INDEX idx_actor_turns_task ON actor_turns(workspace_id, task_id, created_at);
```

### Toolkit Rule (Hard)

`todo.ensure_active` called at start of every `RunUntilStop` unless message marked `no_task=true`.

### Remaining Work

- [ ] Add `task_id` column to `actor_turns`
- [ ] Create index for task-based queries
- [ ] Emit `TaskBinding` hook event at turn start

---

## PR 3.2 — Blackboard Event Stream + Persistence

**Status:** ✅ Mostly Complete

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Blackboard store | `internal/storage/blackboard/store.go` | ✅ Done |
| Board store | `internal/storage/blackboard/board_store.go` | ✅ Done |
| BB tools | `internal/agent/tools/bb_tools.go` | ✅ Done |

### Event Types

| Event | Description | Status |
|-------|-------------|--------|
| `bb.posted` | New post created | ⚠️ Needs emission |
| `bb.updated` | Post updated | ⚠️ Needs emission |
| `bb.claimed` | Post claimed by actor | ⚠️ Needs emission |
| `bb.released` | Claim released | ⚠️ Needs emission |

### Remaining Work

- [ ] Emit EventBus events on bb operations
- [ ] Append compact entry to `actor_turns` on bb.post

---

## PR 3.3 — Context Router Hook

**Status:** ❌ Not Implemented

### Required Skill

`hooks_context_router` - Central intelligence for context injection.

### Triggers

- `bb.posted`
- `task.updated`
- `agent.turn.start`

### Output Actions

- `inject_context` to specific actors
- `send_mailbox` for interrupt messages

### Routing Rules (Hard, Deterministic)

1. If bb post has `task_id`:
   - Deliver to actors working on that task
   - Deliver to actors working on dependencies (once graph exists)

2. If bb post is from human/parent and `interrupt=true`:
   - Deliver as mailbox interrupt

3. Otherwise:
   - Deliver as inbox injection (top of next prompt)

### Remaining Work

- [ ] Create `skills/hooks_context_router/` skill
- [ ] Implement routing rules
- [ ] Wire to event bus triggers
- [ ] Add to `hooks.yaml`

---

## PR 3.4 — Task Dialogue Fetch Tool

**Status:** ❌ Not Implemented

### Required Tool

`task.dialogue` - LLM-friendly transcript for task history.

### Input

```json
{
  "task_id": "...",
  "limit_turns": 50,
  "since": "2024-01-01T00:00:00Z",
  "include_tools": false
}
```

### Output

Compact transcript (bounded), with paging token. Returns **text**, not raw DB rows.

### Remaining Work

- [ ] Create `skills/task_dialogue/` skill
- [ ] Implement bounded transcript generation
- [ ] Add paging support

---

## PR 3.5 — Overseer "Task Board" Actor

**Status:** ⚠️ Partial

### Implementation

| Component | Location | Status |
|-----------|----------|--------|
| Overseer scorer | `internal/analysis/overseer/scorer.go` | ✅ Done |
| Task graph analysis | `internal/analysis/tasksgraph/` | ✅ Done |

### Scheduling Rule (Deterministic)

Sort runnable tasks by:
1. Explicit priority field (if exists)
2. Due date (if exists)
3. created_at (FIFO)

### Assignment Output

```json
{
  "kind": "task.assignment",
  "task_id": "...",
  "actor_id": "...",
  "rationale": "..."
}
```

### Remaining Work

- [ ] Create `overseer` actor type in registry
- [ ] Wire deterministic scheduling to bb.post assignments
- [ ] Test coordination loop

---

## Acceptance Criteria

- [ ] Query "all dialogue for task X" returns complete, ordered results
- [ ] Blackboard posts create events and durable traces
- [ ] Actor A posting about task X → Actor B on task X sees it automatically
- [ ] Agents can call `task.dialogue` to see upstream/downstream dialogue
- [ ] Overseer assigns tasks and agents coordinate via bb/inbox
