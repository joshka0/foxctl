# Room Epic Health Pulse

| Field | Value |
|-------|--------|
| Status | Reviewed, implementing |
| Scope | coordinator-facing epic health read model for `agentctl room` agile workflow |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

The room agile backend now has:

- epic intake and resumability
- milestone contracts
- story lifecycle
- story validation
- milestone synthesis
- retro guidance
- delivery log
- work-pack mirrors

What is still missing from the Factory-style mission model is the higher-level
coordinator pulse that says whether an epic is healthy, drifting, or blocked
without requiring the operator to manually inspect several read models.

`epic resume` and `epic next` are useful, but they are still oriented around:

- continuity for one actor
- next-step extraction

They are not yet the same thing as:

- coordinator-facing mission health
- coverage/tranche risk
- stale or missing milestone closure

## Goals

1. Give coordinators one compact health view for an epic.
2. Surface operational gaps that threaten long-running mission flow.
3. Reuse existing room/agile data instead of inventing a second planning system.
4. Keep the slice read-only.
5. Make the output deterministic and machine-readable.

## Non-goals

1. Replacing `epic resume` or `epic next`.
2. Auto-mutating room state based on health findings.
3. Adding weighted scoring or opaque health heuristics.
4. Creating a separate workflow outside `room`.
5. Building a GUI in the same slice.

## Proposed command surface

Add:

```bash
agentctl room epic health <room-id> <epic-id>
```

Optional:

- `--actor <participant>`
  - only to tailor actor-specific interview/task context when useful
- `--limit <n>`
  - maximum room messages inspected

This should remain distinct from:

- `epic resume`
- `epic next`
- room-wide `status`

`epic health` is specifically the epic-level coordinator pulse.

## Proposed output shape

Top-level fields:

- `epic_id`
- `title`
- `health`
  - `healthy`
  - `needs_attention`
  - `blocked`
  - `closing`
  - `complete`
- `phase`
- `current_milestone_id`
- `current_milestone_title`
- `summary`

Operational counters:

- `open_intake_questions`
- `open_interview_items`
- `milestone_count`
- `active_milestone_count`
- `stories_missing_validation`
- `blocked_story_count`
- `stale_milestone_summary_count`
- `milestones_missing_contract_count`
- `guidance_update_count`

Structured issue lanes:

- `issues`
  - typed, deterministic, sorted
- `recent_guidance_updates`
  - latest few only
- `recent_delivery_logs`
  - latest few only

## Issue types

Use explicit issue types rather than prose-only warnings:

- `intake_open`
- `milestone_missing_contract`
- `milestone_missing_criteria`
- `story_missing_validation`
- `story_blocked`
- `milestone_needs_review`
- `milestone_needs_summary`
- `stale_summary`
- `interview_unresolved`
- `epic_has_no_log`

Each issue should include:

- `type`
- `severity`
  - `info`
  - `warn`
  - `block`
- `target_id`
- `title`
- `reason`
- `command_hint`

Sorting:

- severity rank:
  - `block`
  - `warn`
  - `info`
- then `type`
- then `target_id`

## Health derivation

Keep health derivation explicit and deterministic:

1. `blocked`
   - if current milestone is blocked
   - or blocked story count > 0
2. `needs_attention`
   - if intake/interview remains open
   - or accepted stories lack validation
   - or milestone contract/criteria are missing
   - or summary/review is stale or missing
3. `closing`
   - if the current milestone has passed review and only synthesis/log closure remains
4. `complete`
   - if epic work is fully closed and no open issues remain
5. `healthy`
   - if execution is active and none of the above issue classes apply

Clarification:

- `needs_attention` wins over `closing`
- for example, an epic with a stale summary is not `closing` even if the current milestone already passed review

## Stale summary rule

This slice should make the stale-summary rule explicit instead of hiding it in
coordinator intuition.

Initial rule:

- a milestone is `stale_summary` when:
  - it has accepted stories
  - and its latest material change is newer than its latest milestone summary
  - and it is not already blocked

Material changes include:

- story acceptance
- story state updates
- story validations
- milestone review

Timestamp source:

- use the latest `(created_at, message_id)` marker among those material-change messages
- compare it against the latest milestone-summary `(created_at, message_id)` marker
- this avoids false negatives when multiple writes land in the same timestamp bucket

## Work-pack implications

No new write surface is required in this slice.

Optional read-model additions only:

- expose `retro.md`
- expose `delivery-log.md`
- expose milestone summary paths in the health output when helpful

## MCP exposure

Extend `room_agile` with:

- `epic_health`

Arguments:

- `room_id`
- `epic_id`
- `workspace`
- optional `actor`
- optional `limit`

## Definition of done

This slice is done when:

1. `epic health` returns a deterministic epic-level health view.
2. output is clearly separate from `epic resume` and `epic next`.
3. issue types are typed and stable.
4. health derivation follows an explicit precedence order.
5. focused tests cover:
   - blocked epic
   - missing validation
   - stale summary
   - healthy execution
   - complete epic

## Open questions

1. Should actor-specific room tasks eventually appear in `epic health`, or stay in `epic next` only?
2. Should stale-summary detection later use explicit checkpoint ids instead of timestamp comparison?
3. Should `epic health` later feed the room loop/coordinator pulse directly, or remain a pull-only read model first?
