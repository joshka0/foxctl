# Room Milestone Contract

| Field | Value |
|-------|--------|
| Status | Draft for review |
| Scope | richer milestone contract for `agentctl room` agile workflow |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

The current room milestone model is still too thin for long-running mission-like
work.

Today a milestone has:

- title
- goal
- owner
- scope
- acceptance criteria
- review
- summary

That is enough for basic tracking, but not enough for the stronger contract
shape that Factory-style milestones had.

This slice adds a richer milestone contract without changing the core room model
or turning milestone summary into a second source of truth.

## Goals

1. Give milestones a durable contract, not just a title plus scope.
2. Make milestone intent and guardrails visible before implementation starts.
3. Keep the milestone contract distinct from milestone synthesis.
4. Keep `room` canonical and transport-independent.
5. Keep the first slice narrow enough to review and implement cleanly.

## Non-goals

1. Redesigning milestone review/summary in the same slice.
2. Adding a full story state machine in this slice.
3. Replacing current milestone criteria support.
4. Making work-pack templates more sophisticated yet.
5. Introducing a second milestone ledger outside `room`.

## Current problem

Milestones are missing several pieces that matter in practice:

- risks
- exclusions
- dependencies
- validators expected
- exit criteria

Without these, the coordinator still has to carry too much implicit context.

## Proposed model

Keep the existing milestone root, but enrich its body/meta with an explicit
contract section.

Recommended milestone contract fields:

- `objective`
- `scope`
- `risks`
- `exclusions`
- `dependencies`
- `validators_expected`
- `exit_criteria`

The current `goal` field can remain as the short intent line, while `objective`
becomes the richer contract narrative.

## Command surface

### Extend milestone start

Allow milestone start to set richer contract fields:

```bash
agentctl room milestone start <room-id> <epic-id> "Foundation" \
  --goal "Ship the first validation slice" \
  --objective "Make story validation and work-pack sync operational" \
  --owner human-a \
  --scope "story validation" \
  --scope "work-pack sync" \
  --risk "multi-epic rooms may overcount interview items" \
  --exclude "GUI changes" \
  --dependency "epic finalized" \
  --validator "review" \
  --validator "test" \
  --exit "accepted stories are validated" \
  --exit "milestone summary is written"
```

### New milestone contract update command

Add:

```bash
agentctl room milestone contract <room-id> <milestone-id> \
  --objective "..." \
  --risk "..." \
  --exclude "..." \
  --dependency "..." \
  --validator "..." \
  --exit "..."
```

Purpose:

- update the milestone contract after creation without rewriting review/summary
- keep contract updates explicit and coordinator-owned
- list fields on contract updates are cumulative for the fields you pass:
  - `risk`
  - `exclude`
  - `dependency`
  - `validator`
  - `exit`
- cumulative list fields are unioned, deduped, and stable-sorted
- omitted fields remain unchanged

## Read-model shape

`milestone show` should expose:

- existing milestone fields
- `contract`

Where `contract` includes:

- `objective`
- `scope`
- `risks`
- `exclusions`
- `dependencies`
- `validators_expected`
- `exit_criteria`

It should also expose contract presence counts for quick coordinator scanning:

- `risk_count`
- `dependency_count`
- `validator_count`
- `exit_criteria_count`

## Invariants

1. Milestone contract remains part of the milestone root/meta, not a separate ledger.
2. Contract changes are coordinator-only.
3. Contract fields are append-friendly but normalized:
   - trim whitespace
   - dedupe values
   - stable-sort repeated lists where order is not semantically meaningful
   - `milestone contract` adds to list fields you pass; it does not clear prior list entries unless a future explicit replace mode is added
4. `validators_expected` values should initially reuse the same validation vocabulary as story validation:
   - `review`
   - `test`
   - `integration`
   - `user_test`
   - `manual_check`
   - `audit`
5. `exit_criteria` are milestone-level success conditions, not story-level validation records.

## Relationship to existing fields

- `goal`
  - short milestone intent
- `objective`
  - richer narrative of what the milestone is trying to achieve
- `criteria`
  - specific acceptance statements already supported by `milestone criteria`
- `validators_expected`
  - which validation lanes should exist before the milestone is considered truly complete
- `summary`
  - review-time synthesis after the work is done

This keeps contract, criteria, validation, and summary separate.

## Work-pack integration

The milestone work-pack mirror should render these new fields into:

- `milestone.md`
- `meta.json`

Recommended markdown sections:

- Objective
- Scope
- Risks
- Exclusions
- Dependencies
- Validators Expected
- Exit Criteria

## MCP exposure

Extend `room_agile` with:

- `milestone_contract`

Arguments:

- `room_id`
- `milestone_id`
- `workspace`
- `sender`
- optional `objective`
- repeatable `risk`
- repeatable `exclude`
- repeatable `dependency`
- repeatable `validator`
- repeatable `exit`

## Skill implications

Update `agentctl-room-agile` to teach:

- define milestone contract before or immediately after implementation starts
- do not treat `milestone summary` as a substitute for milestone contract
- use contract updates when scope/risk/dependencies materially change

## Definition of done

This slice is done when:

1. milestone start accepts the richer contract fields
2. `agentctl room milestone contract` updates them explicitly
3. `milestone show` exposes the richer contract
4. work-pack rendering includes the new sections
5. `room_agile` exposes the new action
6. focused tests cover:
   - start with contract fields
   - contract update after creation
   - validator normalization/deduplication
   - coordinator-only mutation

## Open questions

1. Should `dependencies` be free-form strings in the first slice, or support structured ids later?
2. Should `exclusions` remain purely narrative, or later become a first-class negative-scope mechanism?
3. Should `validators_expected` be strictly normalized now, or allow custom values behind a later escape hatch?
