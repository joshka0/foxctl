# Room Milestone Synthesis

| Field | Value |
|-------|--------|
| Status | Draft for review |
| Scope | structured milestone summary / synthesis for `foxctl room` agile workflow |
| Related | [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

`milestone summary` currently stores only a free-form note body.

That is useful, but it is weaker than the milestone synthesis artifacts that made
Factory-style milestones durable and reviewable.

The next narrow slice is to turn milestone summary into a structured synthesis
artifact without making it a second source of truth.

## Goals

1. Make `milestone summary` a richer, reviewable synthesis object.
2. Keep story validation as the canonical proof layer.
3. Keep milestone summary as a rollup, not a duplicate evidence store.
4. Make milestone synthesis visible in room show outputs and work-pack files.
5. Keep the slice narrow enough to review and implement cleanly.

## Non-goals

1. Redesigning story lifecycle in the same slice.
2. Adding separate milestone-level validation records.
3. Replacing milestone review or acceptance criteria support.
4. Adding user-testing as a separate protocol kind yet.
5. Turning summary into a mutable dashboard outside room state.

## Current problem

Today milestone summary is just:

- one summary message
- free-form body text

What is missing:

- explicit passed criteria
- explicit failed criteria
- waived validation ids
- blocking validation ids
- notable decisions
- systemic findings
- recommended next milestone / next focus
- follow-up guidance

Without structure, the summary is less useful for resumption and less aligned
with the Factory milestone synthesis pattern.

## Proposed model

Keep `milestone review` as the verdict.

Keep story validations as the primary proof objects.

Make `milestone summary` a structured synthesis body with these fields:

- `summary`
- `passed_criteria`
- `failed_criteria`
- `waived_validation_ids`
- `blocking_validation_ids`
- `notable_decisions`
- `systemic_findings`
- `recommended_next`
- `guidance_updates`

This makes `milestone summary` the milestone-level synthesis artifact.

## Command surface

### Extend milestone summary

Add flags to enrich the synthesis:

```bash
foxctl room milestone summary <room-id> <milestone-id> \
  --summary "Foundation passed with one waived audit and no blocking validations." \
  --passed-criterion "Epic hierarchy is visible" \
  --passed-criterion "Accepted stories are validated" \
  --waived-validation <validation-id> \
  --decision "Keep contract and summary distinct" \
  --finding "Review follow-ups should be room-acked when no reply is needed" \
  --next "Start the story lifecycle slice" \
  --guidance "Use milestone summary as synthesis, not proof"
```

To preserve a compact CLI, keep the current positional notes form as a shorthand:

```bash
foxctl room milestone summary <room-id> <milestone-id> "Review synthesis..."
```

That shorthand should populate `summary` only.
If both positional notes and `--summary` are provided, `--summary` wins.

## Read-model shape

`milestone show` should expose:

- `summaries`
- `latest_summary`
- `summary_count`
- `summary_meta`

Where `summary_meta` includes the structured synthesis fields.

It should also surface quick counts for coordinator scanning:

- `passed_criteria_count`
- `failed_criteria_count`
- `waived_validation_count`
- `blocking_validation_count`
- `decision_count`
- `finding_count`
- `guidance_update_count`

## Invariants

1. Summary is a rollup layer, not a second evidence store.
2. `blocking_validation_ids` and `waived_validation_ids` must reference validations already attached to stories in the same milestone.
3. `blocking_validation_ids` may be a curated subset of current story-level blocking validations, but every referenced id must still be valid at summary time.
4. `failed_criteria` and `passed_criteria` are milestone-level synthesis statements, not new acceptance-criteria roots.
5. Synthesis fields should summarize or reference proof; they must not inline large artifacts, CAS blobs, or full validation bodies.
6. Lists are normalized:
   - trim whitespace
   - dedupe
   - stable-sort ids where order is not semantically meaningful
7. Summary remains coordinator-owned.
8. The latest summary is authoritative for milestone synthesis, but prior summaries remain in the ledger.

## Relationship to existing fields

- `milestone review`
  - verdict (`pass` / `block`)
- `story validation`
  - canonical proof
- `milestone summary`
  - synthesis over review + validation + criteria state
- `delivery log`
  - epic-level continuity, not milestone synthesis; `recommended_next` in summary should stay milestone-scoped and not replace epic-level next-focus logging

## Work-pack integration

The milestone work-pack mirror should render structured synthesis into:

- `summary.md`
- `meta.json`

Recommended markdown sections:

- Summary
- Passed Criteria
- Failed Criteria
- Waived Validations
- Blocking Validations
- Notable Decisions
- Systemic Findings
- Recommended Next
- Guidance Updates

## MCP exposure

Extend `room_agile` summary support so `milestone_summary` accepts:

- `summary`
- repeatable `passed_criterion`
- repeatable `failed_criterion`
- repeatable `waived_validation`
- repeatable `blocking_validation`
- repeatable `decision`
- repeatable `finding`
- repeatable `next`
- repeatable `guidance`

## Skill implications

Update `foxctl-room-agile` to teach:

- `milestone review` is verdict
- `milestone summary` is synthesis
- story validations still carry proof
- milestone summary should link findings and next steps, not recreate proof details

## Definition of done

This slice is done when:

1. `milestone summary` accepts structured synthesis fields
2. `milestone show` exposes the latest structured synthesis cleanly
3. work-pack `summary.md` and `meta.json` include the synthesis fields
4. `room_agile` exposes the structured summary arguments
5. focused tests cover:
   - shorthand summary-only flow
   - structured synthesis flow
   - validation id reference checks
   - coordinator-only mutation

## Open questions

1. Should `passed_criteria` / `failed_criteria` later reference criterion ids instead of free-form text?
2. Should `guidance_updates` eventually promote into a separate retro/guidance protocol kind?
3. Do we want `recommended_next` to stay narrative, or later tie directly to milestone/story proposals?
