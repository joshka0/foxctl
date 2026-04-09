# Room Epic Resume and Next

| Field | Value |
|-------|--------|
| Status | Draft for review |
| Scope | `agentctl room epic resume` and `agentctl room epic next` |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice first

The room agile backend already has durable `epic`, `milestone`, `story`,
validation, summary, and log objects.

What it still lacks is a strong resumability surface. After a long gap, the
coordinator or worker still has to reconstruct too much state manually from:

- `epic show`
- `milestone show`
- `story show`
- `log show`
- room chat/task history

This slice adds the minimum continuity layer that makes a room epic feel more
like a Factory-style long-running mission:

1. `epic resume` — summarize the current operational state of one epic
2. `epic next` — extract the next concrete actionable items from that state

This is deliberately narrower than “richer milestone contracts” or “retro”.

## Goals

1. Make epic resumability first-class.
2. Reduce the need to reread the whole room after a pause.
3. Surface the next actions for both coordinator and participants.
4. Reuse existing room state rather than inventing a second ledger.
5. Keep the commands transport-independent and room-native.

## Non-goals

1. Changing the canonical epic/milestone/story model.
2. Adding milestone contract fields in this slice.
3. Adding a new storage backend.
4. Making the GUI first.
5. Replacing `epic show` or `milestone show`.

## Command surface

### `agentctl room epic resume`

```bash
agentctl room epic resume <room-id> <epic-id> \
  --workspace /path/to/repo
```

Purpose:

- produce a resumable operational summary for one epic
- answer “what state is this epic in right now?”

This is intentionally different from `room status`:

- `room status` is room-wide and coordinator-oriented
- `epic resume` is epic-scoped and continuity-oriented
- `epic resume` should not duplicate generic coordinator pulse/task backlog output

### `agentctl room epic next`

```bash
agentctl room epic next <room-id> <epic-id> \
  --workspace /path/to/repo \
  --actor human-a
```

Purpose:

- return the next concrete actions for one actor against the epic
- if `--actor` is omitted, return coordinator-oriented actions

## Read-model shape

Both commands are derived from existing room state:

- `epic`
- epic intake (`epic_question`, `epic_answer`, `epic_finalize`)
- milestone proposals and milestones
- stories and validations
- milestone reviews and summaries
- delivery log
- room interview backlog
- room tasks when relevant

No new canonical event types are required for this slice.

## `epic resume` output

`epic resume` should return:

- `epic_id`
- `title`
- `status`
- `phase`
- `finalized`
- `current_milestone_id`
- `current_milestone_title`
- `milestone_count`
- `story_count`
- `accepted_story_count`
- `validated_story_count`
- `blocked_story_count`
- `open_intake_questions`
- `open_interview_items`
- `stories_missing_validation`
- `latest_log_label`
- `latest_log_notes`
- `workpack_root`
- `summary`

### Phase derivation

Suggested derived phases:

- `discovery`
  - epic exists but not finalized
- `shaping`
  - epic finalized but no active milestone yet
- `execution`
  - active milestone exists and work is in progress
- `review`
  - milestone review/summary work remains open
- `blocked`
  - active milestone has blocked validations or review block
- `completed`
  - all current milestones are passed and no active work remains

This is a derived field, not a new stored root field in this slice.

Phase precedence must be explicit:

1. `discovery`
   - epic not finalized
2. `blocked`
   - current milestone has blocked review state or blocked validations
3. `review`
   - current milestone is waiting on milestone review or summary closure
4. `execution`
   - current milestone exists and still has active scope/validation work
5. `shaping`
   - epic finalized but no active milestone exists
6. `completed`
   - all milestones are passed and no open epic work remains

Only one phase may be returned. The implementation should follow this ordered
decision table rather than ad hoc branching.

### Summary text

`summary` should be short and operational. It should be suitable for:

- restarting a session
- pasting into a room handoff
- feeding into a future `epic checkpoint` command

Example:

> Epic is in execution. Current milestone is “Foundation”. Two accepted stories
> exist, one still missing validation. No intake questions remain. Latest log
> says CLI hierarchy shipped and GUI surfacing is next.

## `epic next` output

`epic next` should return:

- `epic_id`
- `actor`
- `lane`
- `items`

Each item should be a structured action:

- `type`
- `priority`
- `target_id`
- `title`
- `reason`
- `command_hint`

The command must always return a stable shape:

- `epic_id`
- `actor`
- `lane`
- `items`
- `reason`

When there is no open work:

- `items` must be `[]`
- `reason` should be a short stable explanation such as `no open work`

### Action lanes

If `--actor` is omitted:

- default to the coordinator lane

If `--actor` is set:

- derive next actions specifically for that actor

### Possible `type` values

- `answer_intake_question`
- `finalize_epic`
- `shape_milestones`
- `start_milestone_from_proposal`
- `add_milestone_criteria`
- `propose_story`
- `accept_story`
- `validate_story`
- `review_milestone`
- `summarize_milestone`
- `append_delivery_log`
- `resolve_interview`
- `follow_up_blocker`

For documentation and future UI grouping, these action types fall into lanes:

- `intake`
  - `answer_intake_question`
  - `finalize_epic`
- `milestone`
  - `shape_milestones`
  - `start_milestone_from_proposal`
  - `add_milestone_criteria`
- `story`
  - `propose_story`
  - `accept_story`
  - `validate_story`
- `review`
  - `review_milestone`
  - `summarize_milestone`
- `log`
  - `append_delivery_log`
- `clarification`
  - `resolve_interview`
- `blocker`
  - `follow_up_blocker`

## Prioritization rules

`epic next` should prefer:

1. unresolved intake before execution
2. blocked work before new work
3. validation gaps before more scope expansion
4. missing milestone review/summary before starting the next milestone
5. stale interview or direct clarification items before speculative execution

Coordinator default order:

1. open intake questions
2. finalize epic if ready
3. shape/start milestone if none active
4. unblock current milestone
5. ensure stories have validation
6. write missing milestone summary
7. refresh delivery log if work changed materially

Participant default order:

1. direct actor-owned interview/intake obligations
2. assigned or owned stories missing validation
3. review actions explicitly aimed at that actor
4. story proposal/implementation work under the current milestone

Ordering rule:

- primary sort: `priority` ascending
- secondary sort: `type` lexicographically
- tertiary sort: `target_id` lexicographically

This ordering must be deterministic across CLI, MCP, and tests.

## “Current milestone” selection

This slice needs a deterministic current-milestone rule.

Recommended rule:

1. choose the most recent milestone whose status is neither `passed` nor fully complete
2. if all milestones are passed, choose the most recent milestone
3. if no milestone exists, return no current milestone

This keeps `epic resume` stable without introducing a new explicit pointer yet.

Tie-break:

- use milestone creation time descending
- if timestamps are equal, use milestone id lexicographically

## Validation-gap detection

For this slice, `stories_missing_validation` means:

- accepted stories in the current milestone
- where `covered != true`

This should align with the existing milestone rollup semantics.

## Work-pack integration

Both commands should return work-pack path hints:

- epic root
- current milestone dir if present
- story dirs for actionable validation gaps if present

This is for operator convenience only. Room state remains canonical.

## Command hints

`command_hint` should use concrete `agentctl` commands whenever the next step is
stable enough to express as a command.

Examples:

- `agentctl room epic finalize <room-id> <epic-id> "..."`
- `agentctl room milestone start <room-id> <epic-id> --proposal <proposal-id>`
- `agentctl room story validate <room-id> <story-id> review pass "..."`

Avoid prose-only hints when a deterministic command exists.

## MCP exposure

Extend `room_agile` with:

- `epic_resume`
- `epic_next`

Arguments:

- `room_id`
- `epic_id`
- `workspace`
- optional `actor`

## Skill implications

Update `agentctl-room-agile` to teach:

- use `epic resume` to restart work after a gap
- use `epic next` before inventing new scope or rereading the entire room

## Suggested implementation shape

Backend steps:

1. add `newRoomEpicResumeCommand`
2. add `newRoomEpicNextCommand`
3. build a reusable epic continuity read-model helper
4. derive phase/current milestone/validation gaps from existing views
5. expose the same through `room_agile`

Recommended helpers:

- `buildRoomEpicContinuity(...)`
- `findCurrentRoomMilestone(...)`
- `buildRoomEpicNextItems(...)`

## Definition of done

This slice is done when:

1. `agentctl room epic resume` returns a coherent operational summary
2. `agentctl room epic next` returns ordered actionable items
3. `room_agile` exposes `epic_resume` and `epic_next`
4. focused tests cover:
   - discovery epic
   - finalized epic with no milestone
   - active milestone with missing validation
   - blocked milestone
   - milestone pending summary/review
   - deterministic empty `epic next` response
5. skill docs mention the new commands

## Open questions

1. Should `epic next` include room-task assignments directly in this slice, or stay strictly within epic/milestone/story state?
2. Should “current milestone” remain a derived heuristic long-term, or eventually become an explicit epic pointer?
3. Do we want a future `epic checkpoint` command to persist the human-friendly summary returned by `epic resume`?
4. In this slice, `open_interview_items` is room-wide rather than epic-filtered because interview sessions do not yet carry explicit epic linkage. If multi-epic rooms become common, tighten this in a follow-up instead of broadening the current feature.
