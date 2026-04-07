# Room Story Lifecycle

| Field | Value |
|-------|--------|
| Status | Draft for review |
| Scope | explicit story execution lifecycle for `agentctl room` agile workflow |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

Stories currently encode:

- proposed vs accepted
- owner
- description
- validation state

That is enough for scoping and proof, but not enough for durable execution
tracking across long-running work.

Factory-style missions had stronger feature-level delivery discipline. The next
slice is to add an explicit story lifecycle without replacing tasks, reviews, or
validations.

## Goals

1. Give stories explicit execution state beyond proposed/accepted.
2. Make in-progress, blocked, review, waived, done, and deferred status durable.
3. Keep story validation as proof, not as the only way to infer progress.
4. Keep the lifecycle transport-independent and room-native.
5. Keep the slice narrow enough to implement and review cleanly.

## Non-goals

1. Replacing room tasks with stories.
2. Redesigning story validation in this slice.
3. Collapsing review, summary, and lifecycle into one object.
4. Adding estimates, points, or Scrum-specific planning fields.
5. Introducing automatic state transitions from every message kind.

## Proposed states

Recommended story states:

- `proposed`
- `accepted`
- `in_progress`
- `in_review`
- `validated`
- `blocked`
- `waived`
- `done`
- `deferred`

State intent:

- `proposed`
  - candidate scope, not committed
- `accepted`
  - committed scope, not started
- `in_progress`
  - active implementation or investigation
- `in_review`
  - awaiting review or verification
- `validated`
  - required proof exists, but final milestone closure may still be pending
- `blocked`
  - cannot proceed without external resolution
- `waived`
  - accepted exception path; proof intentionally incomplete
- `done`
  - fully completed and no longer active
- `deferred`
  - intentionally not active in the current milestone

## Proposed model

Keep the story root as the canonical story identity.

Add append-only `story_state` updates that reference the story root and carry:

- `state`
- `reason`
- `blocked_by`
- `reviewer`
- `updated_by`

The current story view should derive its effective state from:

1. the story root (`accepted` by default after acceptance)
2. the latest `story_state` update
3. story validation only when it is needed to derive `validated` or `waived`

This keeps lifecycle explicit, but still lets proof inform state when
appropriate.

## Command surface

### Add a story state command

```bash
agentctl room story state <room-id> <story-id> <state> \
  --reason "Implementation is underway" \
  --blocked-by "Needs clarification on validator expectations" \
  --reviewer human-a
```

Examples:

```bash
agentctl room story state <room-id> <story-id> in_progress \
  --reason "Started implementation"

agentctl room story state <room-id> <story-id> in_review \
  --reason "Ready for coordinator review" \
  --reviewer human-a

agentctl room story state <room-id> <story-id> blocked \
  --reason "Waiting on cross-story integration decision" \
  --blocked-by "milestone-summary"

agentctl room story state <room-id> <story-id> deferred \
  --reason "Move to next milestone after current tranche closes"
```

## Read-model shape

`story show` should expose:

- `state`
- `state_reason`
- `blocked_by`
- `reviewer`
- `state_history`
- `state_update_count`

`milestone show` should expose story state rollups:

- `in_progress_story_count`
- `in_review_story_count`
- `blocked_story_count`
- `validated_story_count`
- `done_story_count`
- `deferred_story_count`

Note:

- `validated_story_count` remains proof/coverage-based for now *(accepted stories with validation coverage)*, not strictly `state == validated`
- lifecycle state counts and proof counts intentionally coexist

## Invariants

1. `story_state` is append-only; history remains visible.
2. `proposed` stories do not accept `story state` transitions until accepted.
3. `validated` and `waived` may be set explicitly, but they should remain aligned with the latest effective validation state.
4. If an explicit `story_state` update to `validated` or `waived` contradicts the latest `story_validation`, reject the write instead of allowing two truths.
5. `done` should only be allowed when the story is either validated or explicitly waived.
6. `blocked` requires a non-empty `reason`.
7. `deferred` requires a non-empty `reason`.
8. State values are normalized and validated.
9. The latest state update is authoritative for the current story state.

## Relationship to existing fields

- `story proposal`
  - creates `proposed`
- `story accept`
  - creates `accepted`
- `story state`
  - tracks execution lifecycle
- `story validation`
  - carries proof
- `milestone summary`
  - synthesizes, but does not replace story state

## Work-pack integration

The story work-pack mirror should expose lifecycle in:

- `story.md`
- `meta.json`

Recommended additions:

- Current State
- State Reason
- Blocked By
- Reviewer
- State History

## MCP exposure

Extend `room_agile` with:

- `story_state`

Arguments:

- `room_id`
- `story_id`
- `state`
- `workspace`
- `sender`
- optional `reason`
- optional `blocked_by`
- optional `reviewer`

## Skill implications

Update `agentctl-room-agile` to teach:

- story lifecycle is execution state, not proof
- use `story state in_progress` when work starts
- use `story state in_review` when asking for review
- use `story state blocked` or `deferred` explicitly instead of letting work silently stall

## Definition of done

This slice is done when:

1. `story state` exists and validates state transitions
2. `story show` exposes effective state and state history
3. `milestone show` exposes story-state counts
4. work-pack `story.md` and `meta.json` include current state
5. `room_agile` exposes `story_state`
6. focused tests cover:
   - accepted -> in_progress -> in_review
   - blocked requires reason
   - done requires validated or waived
   - deferred state persistence

## Open questions

1. Should `validated` and `waived` ever be set only by validation-derived state, with no explicit `story state` path?
2. Should `done` be automatic once a story is validated and the milestone summary lands, or remain explicit?
3. Should story state eventually synchronize with room tasks when a story is linked to a task?
