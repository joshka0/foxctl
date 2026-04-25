---
name: foxctl-epic-grader
description: "Grade a foxctl room-agile epic as a planning artifact: score readiness, surface blockers, and prescribe upgrade actions."
---

# foxctl-epic-grader

Use this skill when the goal is to evaluate a foxctl epic as a planning artifact.

This skill is for `epic creation` grading, not `epic implementation` grading.

Read [references/grading-rubric.md](./references/grading-rubric.md) before scoring.

## Core rule

Do not reward implementation progress when grading epic creation quality.

An epic with real code changes can still score poorly if:

- stories are too coarse
- validator lanes are missing
- milestone criteria are weak
- intake traceability is lost
- evidence expectations are unclear
- the execution boundary is fuzzy

Likewise, an unimplemented epic can still score very well if the planning surface
is structurally ready for execution.

## Inputs to review

Use these surfaces together:

- `foxctl room epic show <room-id> <epic-id>`
- `foxctl room show <room-id>`
- milestone views when needed
- story views when needed
- intake-source artifacts when grading imported or authored epics

For Factory-origin epics, compare against:

- `mission.md`
- `features.json`
- `validation-contract.md`
- `validation-state.json`
- `progress_log.jsonl`
- `handoffs/`
- `worker-transcripts.jsonl`

For authored epics, compare against the intake source when available:

- epic intake questions and answers
- drafting brief or spec
- shaping notes or guidance updates

## What to score

Score the epic against the rubric categories in the reference file.

The total must be exactly `100`.

Do not hand-wave the score. Every deduction should correspond to a concrete
planning weakness visible in the epic.

## Decision boundary

After scoring, emit one of:

- `READY`
- `NOT READY`

`READY` means the epic is suitable to serve as the implementation plan.

`NOT READY` means planning work is still required before implementation should begin.

## Report format

Always output a compact report with these sections, in this order:

```markdown
# Epic Grade: <epic title>

- Decision: READY|NOT READY
- Total: <n>/100

## Category Scores
- Brief & Boundaries: <n>/<max>
- Milestone Architecture: <n>/<max>
- Story Shape: <n>/<max>
- Validation & Evidence: <n>/<max>
- Intake Traceability: <n>/<max>
- Implementation-Plan Readiness: <n>/<max>

## Blockers
- <only planning blockers that keep the epic out of READY>

## Upgrade Actions
- <highest-leverage action 1>
- <highest-leverage action 2>
- <highest-leverage action 3>

## Notes
- <important nuance, especially import-vs-source differences>
```

## Scoring behavior

- Be strict.
- Prefer concrete deductions over vague praise.
- Cap praise.
- If a category is weak, say exactly what would raise it.
- Do not list implementation tasks as upgrade actions unless they are really plan-shaping tasks.

Upgrade actions should be framed as epic-creation improvements, for example:

- split one oversized imported story into three executable stories
- add `review` and `user_test` lanes to milestone contract
- attach handoff artifacts to validator stories
- add milestone criteria for exit conditions
- reconcile imported state that overstates source progress

Avoid actions like:

- implement feature X
- write code for Y
- run the app and test manually

Those belong to implementation, not grading.

## When imported from Factory

If the epic was imported from Factory, explicitly grade:

- how much source structure survived
- whether validator phases were preserved or reconstructed
- whether evidence artifacts are represented
- whether foxctl improved the mission enough to function as the implementation plan

An imported epic can score highly only if foxctl has already closed the planning gaps that Factory left open.

If the epic was authored directly in foxctl, explicitly grade:

- whether milestones and stories trace back to the intake brief
- whether assumptions were made explicit instead of buried in prose
- whether open intake questions were closed or preserved visibly
- whether the epic reads like a canonical implementation plan rather than a rough brainstorm
