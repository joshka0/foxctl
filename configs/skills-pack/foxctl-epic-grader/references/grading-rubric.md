# Grading Rubric

This rubric grades `epic creation` quality only.

Total score: `100`

## 1. Brief & Boundaries — 15

Questions:

- Is the epic goal coherent and specific?
- Is the boundary between mission, milestones, and stories clear?
- Is the category split between `epic creation` and `epic implementation` explicit?
- Are major constraints, assumptions, and non-goals visible?

Typical deductions:

- vague epic brief
- no explicit constraints
- outcome mixed with implementation chatter
- no clear execution boundary

## 2. Milestone Architecture — 15

Questions:

- Do milestones partition the mission cleanly?
- Are milestone objectives and scopes coherent?
- Do milestone contracts and criteria support later review?
- Are dependencies and validator expectations visible where needed?

Typical deductions:

- milestones are arbitrary buckets
- milestone contracts are missing or too weak
- criteria are absent
- validator lanes are implied but not expressed

## 3. Story Shape — 20

Questions:

- Is each story executable as one coherent unit?
- Are oversized imported stories split or explicitly queued for split?
- Are validator stories separated from implementation stories when appropriate?
- Are dependencies and preconditions visible enough to execute safely?

Typical deductions:

- stories are too large
- stories mix implementation with validation
- imported source granularity was preserved even when it is unusable
- hidden sequencing dependencies

## 4. Validation & Evidence — 20

Questions:

- Are validation lanes typed clearly (`review`, `test`, `integration`, `user_test`, `audit`, etc.)?
- Do stories and milestones have clear evidence expectations?
- Are verification commands preserved where useful?
- Are artifacts or provenance pointers attached when they exist?

Typical deductions:

- everything collapsed into generic `audit`
- validator phases missing
- no evidence expectations
- source artifacts exist but are not represented

## 5. Intake Traceability — 10

Questions:

- Can an operator trace the epic back to its intake source?
- Are intake-time assumptions and shaping decisions visible?
- Does the canonical epic preserve source meaning conservatively?
- Are source contradictions surfaced instead of hidden?

Typical deductions:

- optimistic imported states or unjustified drafted claims
- missing provenance or missing brief traceability
- lost linkage to source assertions, handoffs, or intake answers
- drift between source intent and canonical story meaning

Mode-specific expectations:

- imported epics:
  - preserve source provenance, import fidelity, assertion linkage, and handoff evidence
- authored epics:
  - preserve brief traceability, explicit assumptions, and resolved versus open intake questions
- hybrid epics:
  - preserve both source provenance and foxctl-native shaping decisions

## 6. Implementation-Plan Readiness — 20

Questions:

- Could an implementation agent start from this epic without first redesigning it?
- Are the highest-risk planning gaps already closed?
- Are the remaining open questions small enough not to block execution?
- Does the epic function as the canonical implementation plan?

Typical deductions:

- another planning pass is obviously required
- unresolved blockers are structural
- validation policy is incomplete
- implementation agents would have to reinterpret the mission before starting

## Score bands

- `90-100`: strong implementation-plan candidate; only minor creation polish remains
- `75-89`: usable but still needs meaningful epic-creation upgrades
- `50-74`: partially shaped; not ready to serve as implementation plan
- `<50`: imported or drafted structure exists, but the epic is not operationally ready

## READY threshold

Recommended default:

- `READY` requires `90+`
- and no category below half of its max
- and no structural blockers in Story Shape, Validation & Evidence, or Implementation-Plan Readiness

Anything below that is `NOT READY`.
