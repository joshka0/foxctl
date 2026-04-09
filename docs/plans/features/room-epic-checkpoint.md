# Room Epic Checkpoint

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Add a durable `epic checkpoint` snapshot so coordinators can persist a resumable epic handoff without changing the canonical room ledger |
| Related | [room-epic-resume-and-next.md](./room-epic-resume-and-next.md), [room-epic-health-pulse.md](./room-epic-health-pulse.md), [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-workpack-templates.md](./room-workpack-templates.md), [room-agile-provenance-backlinks.md](./room-agile-provenance-backlinks.md) |

## Why this slice

`epic resume` and `epic next` already derive the operational state needed to
restart an epic, but they are still transient reads.

The room lacks a durable “checkpoint” object that says:

- this is the current resumable epic snapshot
- this was the coordinator interpretation at a point in time
- this is what the next operator should pick up from

Factory-style missions benefited from this kind of continuation artifact because
it reduced the amount of room replay and reconstruction needed after long gaps.

## Goals

1. Add a durable `epic_checkpoint` room artifact.
2. Make checkpoint content derive primarily from existing `epic resume` and
   `epic next` output rather than inventing a new second model.
3. Keep `room` canonical and treat checkpoints as historical snapshots.
4. Add a human-readable checkpoint work-pack artifact under the epic.
5. Expose the latest checkpoint on epic read models for fast orientation.

## Non-goals

1. Replacing `epic resume` or `epic next`.
2. Making checkpoints the source of truth for epic state.
3. Automatically creating checkpoints from delivery logs, milestone reviews, or
   summaries.
4. Introducing heuristic text synthesis from arbitrary room messages.
5. Turning checkpoints into enforcement inputs for epic health in this slice.

## Proposed command surface

Add:

- `agentctl room epic checkpoint <room-id> <epic-id>`

Optional inputs:

- `--label "<text>"`
- `--note "<text>"`
- `--actor "<actor>"`
- `--limit <n>`

Rules:

- `--actor` defaults to the coordinator lane for deriving next actions.
- `--limit` controls how many `epic next` items are embedded in the checkpoint.
- if `--label` is omitted, generate a deterministic label from:
  - epic phase
  - current milestone title when present
  - current date

## Stored model

Introduce a new append-only room kind:

- `epic_checkpoint`

Checkpoint data should include:

- `checkpoint_id`
- `room_id`
- `epic_id`
- `actor`
- `label`
- `phase`
- `summary`
- `current_milestone_id`
- `current_milestone_title`
- `next_items`
- `reason`
- `note`
- `created_at`
- `created_by`

### Source-of-truth rule

The checkpoint body should be built from:

1. `epic resume`
2. `epic next`
3. optional coordinator-supplied `label` / `note`

This keeps the checkpoint a durable projection of the room, not a parallel
state system.

## Read-model behavior

`epic show` should expose:

- `checkpoint_count`
- `latest_checkpoint`

`epic resume` should expose:

- `latest_checkpoint_id`
- `latest_checkpoint_label`
- `latest_checkpoint_at`

The read model should not merge checkpoint content back into live state
derivation. It is a pointer and convenience snapshot only.

## Work-pack projection

Add a checkpoint directory under the epic work-pack:

- `~/.agentctl/epics/<epic-id>/checkpoints/<checkpoint-id>.md`
- `~/.agentctl/epics/<epic-id>/checkpoints/<checkpoint-id>.json`

The markdown artifact should include:

- title / label
- timestamp
- phase
- current milestone
- summary
- next actions
- optional coordinator note
- provenance block

The JSON artifact should include the canonical checkpoint envelope plus the
standard provenance fields.

## Rendering rules

### Label generation

When the caller omits `--label`, generate a stable label such as:

- `Execution checkpoint — Foundation — 2026-04-07`
- `Review checkpoint — Foundation — 2026-04-07`
- `Completed checkpoint — 2026-04-07`

### Next items ordering

Embed the `epic next` items in the exact deterministic order returned by
`epic next`.

### Empty state

If there are no next items:

- store `next_items: []`
- render an explicit markdown line such as:
  - `No open next actions.`

## MCP surface

Add:

- `room_agile epic_checkpoint`

Inputs should mirror CLI:

- `room_id`
- `epic_id`
- `label`
- `note`
- `actor`
- `limit`

## Risks

1. Snapshot drift
   - checkpoints are historical snapshots; do not treat them as current truth
2. Overwriting live reasoning
   - derive from `resume`/`next` instead of allowing arbitrary freeform bodies
3. Checkpoint spam
   - keep the command explicit and coordinator-owned in v1
4. Multi-epic room ambiguity
   - inherit existing `epic resume` caveats rather than hiding them

## Definition of done

1. `agentctl room epic checkpoint` writes a durable checkpoint artifact.
2. checkpoint content is derived from existing `epic resume` and `epic next`
   logic plus optional note/label.
3. epic read models expose latest-checkpoint metadata.
4. work-pack checkpoint markdown and JSON files are materialized with
   provenance.
5. MCP mirrors the same operation.
6. focused tests cover:
   - generated label when omitted
   - embedded next items order
   - empty next-items shape
   - latest checkpoint on epic read models
7. review confirms the slice remains additive and does not create a second
   source of truth.
