# Room Evidence Lanes

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Formalize story evidence lanes on top of the existing `story_validation` protocol so milestone synthesis, ACA promotion, and future retrieval can reason about proof by lane instead of only by flattened validation status |
| Related | [room-agile-workpacks-and-validation.md](./room-agile-workpacks-and-validation.md), [room-milestone-synthesis.md](./room-milestone-synthesis.md), [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md), [room-agile-aca-promotion.md](./room-agile-aca-promotion.md) |

## Why this slice

The current room-agile backend already has:

- `story_validation`
- milestone rollups
- milestone synthesis
- ACA draft promotion
- work-pack mirrors

That is enough to track proof, but it is still too flat.

Right now a story can have:

- `review`
- `test`
- `integration`
- `user_test`
- `manual_check`
- `audit`

as `validator_type`, but the model still treats those mostly as one generic
validation stream with one coverage bit and one latest status.

That is the next parity gap with the stronger Factory-style model:

- different evidence lanes should stay distinguishable
- milestone synthesis should be able to say which lane passed or failed
- ACA promotion should be able to target specific evidence classes
- future retrieval should be able to answer “integration checks usually fail here” instead of only “a validation failed”

## Goals

1. Keep lane identity explicit on every story validation.
2. Make room read models expose lane-oriented evidence summaries.
3. Make milestone synthesis and rendering expose evidence by lane rather than only by flattened coverage.
4. Keep the first slice additive and compatible with the current `story_validation` protocol.
5. Avoid keyword heuristics; lane semantics should come from explicit typed fields.

## Non-goals

1. Replacing `story_validation` with a completely new protocol kind.
2. Auto-generating validations from task activity.
3. Turning every lane into a new top-level command noun.
4. Reworking milestone review/synthesis semantics in the same slice.
5. Introducing learned retrieval or reranking logic here.

## Current problem

The model already tracks:

- latest validation status
- coverage
- blockers
- waived validations

But it does not yet make lane semantics first-class in the derived views.

Examples of questions that are awkward today:

- did code review pass but integration fail?
- is the story only covered by `manual_check`, or do we also have `test` evidence?
- are milestone blockers mostly from `user_test` or `integration`?
- did a waived lane replace another stronger lane, or is it the only proof?

## Proposed model

Keep `story_validation` as the canonical evidence record.

Add a stronger explicit lane model on top of it.

### Lane identity

Use `validator_type` as the lane key for now, but document it as an evidence lane:

- `review`
- `test`
- `integration`
- `user_test`
- `manual_check`
- `audit`

This means:

- each validation record belongs to exactly one lane
- supersession still happens within the same lane
- rollups are computed both:
  - overall
  - per lane

Forward-compatibility rule:

- if a future validator type appears outside the current fixed enum, keep it as a distinct explicit lane key instead of collapsing it into another lane
- do not invent heuristic buckets
- the first slice should preserve unknown typed lanes in the derived read model even if the CLI continues to validate known values on write

### Lane summary per story

Each accepted story should expose a lane summary object like:

```json
{
  "evidence_lanes": {
    "review": {
      "count": 2,
      "latest_status": "pass",
      "latest_validation_id": "01VAL...",
      "covered": true,
      "waived": false,
      "blocking": false
    },
    "integration": {
      "count": 1,
      "latest_status": "blocked",
      "latest_validation_id": "01VAL...",
      "covered": false,
      "waived": false,
      "blocking": true
    }
  }
}
```

This is derived state only. The source of truth remains the append-only
validation ledger.

### Lane rollup per milestone

Each milestone should expose:

- `lane_counts`
- `lane_coverage`
- `lane_blockers`
- `lane_waivers`

Example:

```json
{
  "lane_counts": {
    "review": 3,
    "test": 2,
    "integration": 1
  },
  "lane_coverage": {
    "review": 2,
    "test": 1,
    "integration": 0
  },
  "lane_blockers": {
    "integration": ["01VAL..."]
  }
}
```

The first slice can stay compact; the main requirement is that the lane
identity stays queryable.

`lane_counts` in this first slice should mean:

- number of accepted stories that currently have effective evidence in that lane

It should **not** mean raw total validation-record count across all history.

## Coverage rules

Keep the current overall story coverage semantics.

For lane-specific coverage:

- a lane is `covered` when its latest effective validation status is `pass` or `waived`
- a lane is `blocking` when its latest effective validation status is `fail` or `blocked`
- superseded validations do not count toward lane coverage

Overall story `covered` can remain:

- true if at least one effective lane gives acceptable proof

This first slice should not force a stricter “all required lanes must pass”
policy yet.

That stricter policy belongs in a later milestone exit/coverage rule slice.

## First implementation slice

### 1. Story read-model lane summary

Extend `story show` and internal story views with:

- `evidence_lanes`
- `covered_lanes`
- `blocking_lanes`
- `waived_lanes`

### 2. Milestone read-model lane rollup

Extend milestone views with:

- `lane_counts`
- `lane_coverage`
- `lane_blockers`
- `lane_waivers`

### 3. Work-pack rendering

Add lane summaries to:

- `story.md`
- validation markdown/json where useful
- milestone summary rendering

Do not create a new separate lane file per story in the first slice.

### 4. ACA promotion alignment

ACA promotion does not need a new command in this slice, but it should become
able to rely on explicit lane data already exposed in the read models.

That means lane fields should be available for:

- validation promotion
- milestone promotion
- later retrieval

## Command surface

No new top-level command is required in the first slice.

Continue using:

```bash
foxctl room story validate <room-id> <story-id> <validator_type> <status> "<summary>"
```

The change is in:

- the read models
- the work-pack mirror
- the synthesis/ACA consumers

## Work-pack impact

### story.md

Add a section like:

```md
## Evidence Lanes

- `review`: pass
- `test`: pass
- `integration`: blocked
```

### milestone summary

Allow milestone summary rendering to surface lane-oriented synthesis, for
example:

- blocking lanes
- waived lanes
- lanes still missing proof

This stays derived from the current validation records.

This slice does **not** change milestone pass/fail rules.

## Risks

1. Lane semantics becoming stricter than intended too early
   - do not force all lanes to pass in the first slice
2. Confusing lane identity with required-ness
   - a lane may exist without being required by the milestone contract
3. Duplicating evidence state in summary
   - lane rollups must stay derived
4. Adding too much ceremony to `story validate`
   - keep the write path stable for now

## Definition of done

This slice is done when:

1. story read models expose lane summaries
2. milestone read models expose aggregated lane rollups
3. work-pack story/milestone rendering shows lane-oriented proof state
4. focused tests cover at least:
   - mixed lanes on one story
   - supersession inside one lane
   - milestone lane blocker rollup
5. Cursor review confirms no major gaps before implementation
