# Room Coordinator Pulse

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Add a room-wide coordinator mission-control surface that summarizes all epics in one room using existing epic read models rather than creating another source of truth |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md), [room-epic-health-pulse.md](./room-epic-health-pulse.md), [room-epic-resume-and-next.md](./room-epic-resume-and-next.md), [room-epic-checkpoint.md](./room-epic-checkpoint.md), [room-milestone-exit-policy.md](./room-milestone-exit-policy.md) |

## Why this slice

The backend already has strong per-epic coordinator surfaces:

- `epic resume`
- `epic next`
- `epic health`
- `epic checkpoint`

But the coordinator still lacks one room-wide answer to:

- which epics need attention first
- which epics are blocked
- which epics are still in intake
- which milestones are ready for review or summary
- which epics are missing checkpoints or delivery-log continuity

This is the largest remaining Factory-style backend gap.

## Goals

1. Add one room-wide coordinator pulse derived from existing epic read models.
2. Summarize all epics in one room in priority order.
3. Reuse current epic health, exit policy, and continuity helpers where
   possible.
4. Keep the pulse read-only in v1.
5. Make the pulse stable enough for coordinator use, room loops, and future UI
   surfaces.

## Non-goals

1. Replacing `room status`.
2. Replacing `epic health`.
3. Adding new enforcement behavior.
4. Introducing new board-message kinds in v1.
5. Solving all multi-epic scoping issues in the same slice.

This pulse is distinct from the existing room/task cleanup pulse concepts:

- existing `room status` / coordinator task pulse remains the room/message/task lane
- `room pulse` is the new read-only epic aggregate across the room
- v1 does not emit `coordinator_pulse` board messages

## Proposed command surface

Add:

- `agentctl room pulse <room-id>`

Optional inputs:

- `--workspace <path>`
- `--actor <actor-id>`
- `--limit <n>`
- `--only <filter>`

Suggested `--only` filters:

- `all`
- `blocked`
- `intake`
- `review`
- `stale`
- `ready`

## Output model

The command should return:

- `room`
- `summary`
- `epic_count`
- `epics`
- `top_items`

### `summary`

Room-wide counters such as:

- `blocked_epic_count`
- `intake_epic_count`
- `review_epic_count`
- `execution_epic_count`
- `shaping_epic_count`
- `completed_epic_count`
- `missing_checkpoint_count`
- `stale_checkpoint_count`
- `stale_summary_count`
- `missing_log_count`

These counters are computed across all epics in the room, even when `--only`
filters the returned `epics`, `epic_count`, and `top_items`.

### `epics`

One compact row per epic with:

- `epic_id`
- `title`
- `phase`
- `health_status`
- `priority`
- `current_milestone_id`
- `current_milestone_title`
- `exit_policy_status`
- `checkpoint_status`
- `latest_checkpoint_at`
- `latest_log_label`
- `latest_log_at`
- `top_issue_types`
- `top_next_action`

### `top_items`

Room-wide prioritized coordinator actions with:

- `type`
- `priority`
- `epic_id`
- `target_id`
- `title`
- `reason`
- `command_hint`

This is the room-wide analogue of `epic next`, not a replacement for it.

## Priority model

Default room-wide priority should sort epics roughly as:

1. blocked
2. intake not finalized
3. ready_for_review / ready_for_summary
4. stale checkpoint or missing delivery log
5. active execution
6. completed

Within the same class:

- use the existing epic health severity
- then epic id for deterministic ordering

## First implementation slice

### 1. Shared derivation helper

Add a room-wide helper that:

- enumerates epics in the room
- derives `resume`
- derives `health`
- reads current milestone `exit_policy`
- derives compact room-wide priority

### 2. CLI command

Add `room pulse` using the shared helper.

### 3. MCP surface

Expose:

- `room_pulse`

This should stay separate from `room_agile` because it is room-wide rather than
one-epic scoped.

### 4. Read-only only

No writes in this slice:

- no pulse persistence
- no coordinator pulse room messages
- no loop-triggered broadcasts

Those can follow later once the read model is good.

## Signal definitions

### `checkpoint_status`

For v1:

- `not_needed` when no checkpoint exists and the epic has not yet crossed the v1
  activity threshold
- `missing` when no checkpoint exists after real epic activity
- `fresh` when a checkpoint exists and the epic is not obviously ahead of it
- `stale` when later activity exists after the latest checkpoint

V1 activity threshold:

- epic finalized, or
- milestone count > 0, or
- story count > 0, or
- log count > 0, or
- guidance update count > 0

Later slices can refine this with explicit material-change markers.

### `top_issue_types`

Derived from existing epic health issues, for example:

- `epic_not_finalized`
- `milestone_blocked`
- `milestone_needs_review`
- `milestone_needs_summary`
- `story_missing_validation`
- `epic_has_no_log`
- `epic_checkpoint_missing`
- `epic_checkpoint_stale`

### `top_next_action`

Derived from the first stable action after merging:

1. synthetic room-pulse items such as `checkpoint_epic`
2. the existing `epic next` items

So `top_next_action` may be synthetic in v1 when the room-wide pulse wants to
surface a checkpoint refresh before the epic-local next item list.

### `exit_policy_status`

Reuse the existing milestone `exit_policy.status` strings when a current
milestone exists:

- `not_ready`
- `ready_for_review`
- `ready_for_summary`
- `ready_to_exit`
- `blocked`

Do not introduce a second taxonomy in this slice.

## Risks

1. Double-counting room-wide issues
   - keep room pulse room-wide, but clearly scope per-epic counters
2. Repeating already-available detail
   - keep epic rows compact and point to `epic health` / `epic next`
3. Multi-epic scoping leaks
   - call out known room-wide interview caveats in the first slice
4. Overreaching into orchestration writes
   - keep v1 read-only

## CLI notes

- keep `--actor` as the actor-specific shaping input, matching `epic next` and
  `epic health`
- do not add `--sender`; this is a read-only command
- document that v1 may still inherit the current room-wide interview caveat
  until the multi-epic scoping slice lands

## Definition of done

1. `agentctl room pulse <room-id>` returns a stable room-wide epic summary.
2. the pulse is derived from existing epic helpers rather than a parallel model.
3. room-wide counters and epic ordering are deterministic.
4. `top_items` expose actionable room-wide coordinator work.
5. MCP exposes the same read model.
6. focused tests cover:
   - blocked epic ordering
   - intake epic visibility
   - review-ready epic visibility
   - checkpoint missing / stale signal
   - deterministic empty room shape
7. review confirms the slice stays read-only and does not duplicate existing
   epic logic unnecessarily.
