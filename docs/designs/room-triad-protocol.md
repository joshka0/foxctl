# Room Triad Protocol

| Field | Value |
|-------|-------|
| Status | Design proposal |
| Scope | Layer an implementer, verifier/tester, and judge/coordinator workflow on top of existing room agile primitives |
| Related | [foxctl room skill](../../configs/skills-pack/foxctl-room/SKILL.md), [foxctl room agile skill](../../configs/skills-pack/foxctl-room-agile/SKILL.md), [room evidence lanes](../archive/plans/features/room-evidence-lanes.md), [room milestone evidence policy](../archive/plans/features/room-milestone-evidence-policy.md), [room runtime delivery contract](../archive/plans/features/room-runtime-delivery-contract.md), [LLM-as-a-Verifier](https://llm-as-a-verifier.notion.site/) |

## Summary

The room triad protocol is a role protocol for delivery rooms where a story is
not complete just because the implementer says it is complete. A story becomes
complete only after:

1. an implementer records the implementation state,
2. a verifier or tester records explicit validation evidence, and
3. the judge or coordinator accepts, blocks, or waives the result.

This should be built as a room-native workflow on top of the existing durable
room log, room tasks, room agile epics/milestones/stories, `story state`,
`story validate`, milestone evidence lanes, and coordinator authority. It should
not introduce a second coordination system.

## Goals

1. Make independent validation part of the default room workflow.
2. Keep final acceptance coordinator-owned.
3. Use explicit structured state instead of free-form message inference.
4. Reuse existing evidence lanes before adding new command or storage concepts.
5. Make the workflow suitable for humans, CLI agents, MCP agents, and pane-backed
   room participants.

## Non-goals

1. Replacing the existing room loop, room task model, or room agile model.
2. Adding another coordinator or delivery owner beside the current room loop.
3. Inferring verdicts from strings such as "LGTM" in chat messages.
4. Requiring every room to use this protocol.
5. Adding first-class verification storage before the protocol is proven with
   current room primitives.

## Existing Building Blocks

The current repo already has most of the substrate needed for this protocol:

- Durable rooms, room members, inboxes, and messages live in the blackboard room
  store.
- Room members already carry a role string.
- `coordinator` already has special meaning for high-privilege room operations.
- `room loop` is already the lease-owned delivery and reminder runtime.
- `room task` supports assignment, claim, heartbeat, block, unblock, reclaim, and
  completion.
- `room epic`, `room milestone`, and `room story` provide durable agile
  structure.
- `room story validate` records typed validation evidence with lanes:
  `review`, `test`, `integration`, `user_test`, `manual_check`, and `audit`.
- Milestone contracts can declare required and optional evidence lanes.
- Milestone contracts can opt into `--enforce-exit-policy`.
- `room redgreen` already provides a brokered hidden-suite workflow with separate
  red and green worktrees.

The triad protocol should start by composing these pieces rather than creating a
new parallel protocol stack.

## Roles

### Coordinator / Judge

The coordinator is the final authority. For the first version, `judge` should be
treated as a protocol label for the room coordinator rather than a separate
privileged role.

The coordinator may:

- start and shape epics,
- start milestones,
- set milestone contracts and required lanes,
- assign or reassign work,
- resolve stale room traffic,
- waive evidence with explicit notes,
- mark final story state,
- pass or block milestone review,
- write milestone summaries,
- transfer coordinator ownership.

The coordinator must not pass a milestone with enforced missing required lanes.

### Implementer

The implementer owns the change. The implementer may:

- claim assigned implementation tasks,
- move a story to `in_progress`,
- record implementation notes,
- request verification,
- move a story to `in_review` with the verifier as reviewer.

The implementer must not mark their own work accepted without independent
validation evidence.

### Verifier / Tester / Grader

The verifier owns independent proof. `tester` and `grader` are acceptable room
role labels for humans or agents performing this verifier responsibility.

The verifier may:

- define the verification rubric before implementation or review begins,
- run tests and checks,
- record `story validate` evidence,
- attach command, artifact, and notes metadata,
- block a lane with concrete findings,
- request fixes from the implementer,
- recommend pass/block to the coordinator.

The verifier should use existing evidence lanes:

- `test` for unit or focused test proof,
- `integration` for end-to-end or multi-component proof,
- `review` for code/protocol review proof,
- `manual_check` for explicit human or manual verification,
- `audit` for security, policy, or governance checks,
- `user_test` for user-facing acceptance checks.

### Observer

Observers may read room state and contribute context. They should not own story
state, validation evidence, or final verdicts unless the coordinator explicitly
assigns them a role.

## Actor Naming

Actor ids should be scoped and distinctive. Avoid generic ids such as
`coordinator`, `reviewer`, or `tester` when multiple rooms or workstreams may
coexist.

Prefer ids like:

```text
room-runtime-judge-a
room-runtime-impl-a
room-runtime-verifier-a
evolve-impl-a
evolve-verifier-a
```

## Canonical Story Flow

The conceptual state machine is:

```text
proposed
  -> accepted
  -> rubric_ready
  -> in_progress
  -> in_review
  -> verified_pass | verified_block
  -> done | blocked | waived
```

The current implementation does not need all of these as low-level story states.
The first version should map the conceptual stages onto existing primitives:

| Protocol stage | Existing primitive |
|----------------|--------------------|
| Story accepted | `foxctl room story accept` |
| Verification rubric posted | directed `room send` or future `room verify rubric` |
| Implementer starts | `foxctl room story state ... in_progress` |
| Ready for verification | `foxctl room story state ... in_review --reviewer <verifier>` |
| Verifier runs checks | external command plus `story validate` |
| Verifier passes lane | `foxctl room story validate ... <lane> pass` |
| Verifier blocks lane | `foxctl room story validate ... <lane> blocked` |
| Coordinator accepts story | `foxctl room story state ... done` |
| Coordinator blocks story | `foxctl room story state ... blocked --reason ...` |
| Coordinator waives story | `foxctl room story validate ... <lane> waived --notes ...` plus story state |
| Milestone accepted | `foxctl room milestone review ... pass` |
| Durable synthesis | `foxctl room milestone summary` |

## Evidence Policy

Triad rooms should declare milestone evidence expectations explicitly. A typical
milestone contract should require at least `test` and `review` lanes:

```bash
foxctl room milestone contract <room-id> <milestone-id> \
  --validator test \
  --validator review \
  --required-lane test \
  --required-lane review \
  --exit "accepted stories have verifier evidence and coordinator verdict" \
  --enforce-exit-policy
```

Some milestones should also require `integration`, `manual_check`, or `audit`:

```bash
foxctl room milestone contract <room-id> <milestone-id> \
  --required-lane integration \
  --optional-lane manual_check \
  --optional-lane audit
```

Important boundary: current milestone evidence policy is milestone-level. It
does not automatically mean every story has every required lane. If triad rooms
need per-story required lane completeness before `done`, that is an additional
enforcement rule.

## Verifier Model

The verifier should produce structured evidence and recommendations, not final
authority. This follows the useful parts of the LLM-as-a-Verifier pattern:
decomposed criteria, repeated verification, fine-grained scores, and pairwise
comparison when multiple candidate trajectories exist.

The practical rule is:

- deterministic checks come first,
- verifier scoring explains and synthesizes those checks,
- judge/coordinator applies room policy to the evidence.

### Criteria Decomposition

Do not ask a verifier for one coarse "good/bad" answer. A useful verifier report
scores separate criteria such as:

- correctness,
- test coverage,
- integration behavior,
- scope control,
- safety and policy,
- evidence quality,
- maintainability,
- regression risk.

Each criterion should include:

- a stable criterion id,
- pass/block/waive status,
- score,
- explanation,
- evidence references,
- blocking and non-blocking findings.

### Repeated Verification

For important stories, run more than one verification pass. Repetition can be:

- the same verifier model run multiple times,
- multiple verifier agents,
- multiple criteria-specific prompts,
- deterministic checks plus LLM synthesis.

The report should preserve repetition count and confidence instead of hiding
disagreement behind a single score.

### Fine-Grained Scoring

The paper uses finer score-token granularity to reduce ties between long-horizon
agent trajectories. Foxctl does not need to require provider logprobs in the MVP,
but it should preserve room-native fields that can support the same direction:

- `score_0_100`,
- `confidence`,
- `criteria_scores`,
- `score_method`,
- `repetitions`,
- `model`,
- `prompt_version`.

Binary pass/block remains useful for policy, but the richer score explains how
strong the recommendation is.

### Pairwise and Tournament Verification

Pairwise verification is only necessary when there are multiple candidate
implementations or trajectories for the same story. In that mode, the verifier
compares candidates criterion-by-criterion and the room can derive a tournament
winner.

This is useful for:

- multiple workers racing on the same story,
- alternative fix strategies,
- benchmark-style agent evaluation,
- selecting the best patch before judge acceptance.

It is not required for a normal single-candidate implementation.

### Verifier Boundary

Verifier output is a recommendation. It can say:

- `recommendation: accept`,
- `recommendation: request_changes`,
- `recommendation: waive`,
- `recommendation: needs_human_judge`.

The coordinator still owns the actual verdict. A triad room should never infer
acceptance directly from a verifier recommendation.

## Rubric First

The verifier should define pass/fail criteria before implementation is
submitted for verification. In the MVP this can be a directed room message:

```bash
foxctl room send <room-id> \
  --sender room-runtime-verifier-a \
  --to room-runtime-impl-a \
  --reply-expected \
  "Verification rubric for <story-id>:
  - Tests pass
  - Docs describe role authority
  - No keyword heuristics
  - Coordinator-only final verdict
  - Evidence attached through story validate"
```

Later this can become a first-class command and typed message kind.

## Example Flow

Start a milestone with explicit evidence gates:

```bash
foxctl room milestone start <room-id> <epic-id> \
  --objective "Ship triad protocol MVP" \
  --validator test \
  --validator review \
  --required-lane test \
  --required-lane review \
  --exit "accepted stories have verifier evidence and coordinator verdict" \
  --enforce-exit-policy
```

Move a story into implementation:

```bash
foxctl room story state <room-id> <story-id> in_progress \
  --sender room-runtime-impl-a \
  --reason "Implementation started"
```

Submit for verification:

```bash
foxctl room story state <room-id> <story-id> in_review \
  --sender room-runtime-impl-a \
  --reviewer room-runtime-verifier-a \
  --reason "Ready for independent verification"
```

Record passing test evidence:

```bash
foxctl room story validate <room-id> <story-id> test pass \
  "Focused command passed." \
  --sender room-runtime-verifier-a \
  --command "go test ./cmd/foxctl/cmd" \
  --artifact-path docs/reviews/story-123-test.md
```

Record review evidence:

```bash
foxctl room story validate <room-id> <story-id> review pass \
  "Protocol keeps coordinator authority and uses explicit evidence lanes." \
  --sender room-runtime-verifier-a \
  --artifact-path docs/reviews/story-123-verify.md
```

Accept the story:

```bash
foxctl room story state <room-id> <story-id> done \
  --sender room-runtime-judge-a \
  --reason "Accepted after verifier pass evidence"
```

Block the story when evidence is insufficient:

```bash
foxctl room story validate <room-id> <story-id> test blocked \
  "Missing regression coverage for the verifier-block path." \
  --sender room-runtime-verifier-a \
  --notes "Add a failing verifier-block scenario before judge acceptance."

foxctl room send <room-id> \
  --sender room-runtime-judge-a \
  --to room-runtime-impl-a \
  --reply-expected \
  "Fix requested: verifier blocked the test lane. Add regression coverage and resubmit."
```

## Red/Green Variant

For hidden-suite workflows, the protocol should reuse `room redgreen`:

- the red actor owns hidden tests,
- the green actor owns implementation,
- the coordinator can inspect hidden metadata,
- non-red viewers get hidden paths redacted,
- checks run through the broker rather than exposing hidden tests.

This is a specialized verifier/tester flow. It should remain brokered and
append-only: hidden test contents are not pasted into the room, but summarized
results and command evidence are posted back into the durable room timeline.

## Authority Rules

The protocol is only useful if authority boundaries are explicit:

- Implementers may not accept their own work.
- Verifier pass evidence is not the final verdict.
- Coordinator acceptance without evidence must be an explicit waiver.
- Final milestone pass remains coordinator-owned.
- Role binding changes remain coordinator/high-privilege actions.
- Verdicts must not be inferred from free-form message text.
- Large reports should be written as artifacts and referenced from summaries.

## MVP Design

The MVP should be mostly documentation and prompt/protocol alignment:

1. Add this design document.
2. Add a `verifier`, `tester`, or `tester_grader` onboarding branch to
   room-aware prompts.
3. Update room skills with a triad validation mode.
4. Use existing `story state`, `story validate`, required lanes, and enforced
   milestone exit policy.
5. Use directed room messages for rubrics until first-class commands exist.

This MVP is convention-backed, not fully role-enforced. It improves workflow
clarity without introducing schema churn.

## Later First-Class Commands

If the workflow proves useful, add small command wrappers that emit typed,
append-only room messages:

```bash
foxctl room protocol start <room-id> triad \
  --judge <actor-id> \
  --implementer <actor-id> \
  --verifier <actor-id> \
  --required-lane test \
  --required-lane review

foxctl room verify rubric <room-id> <story-id> \
  --sender <verifier> \
  --criterion "Tests pass" \
  --blocking-criterion "Coordinator-only final verdict"

foxctl room verify report <room-id> <story-id> \
  --sender <verifier> \
  --recommendation accept \
  --score 92 \
  --artifact-path <path> \
  --command "go test ./..." \
  --finding "No blocking issues"

foxctl room judge verdict <room-id> <story-id> accept \
  --sender <coordinator> \
  --reason "Accepted after verifier pass evidence"
```

These commands should initially be wrappers around existing story validation,
story state, and milestone review primitives where possible.

## Possible Message Kinds

If typed messages become necessary, add focused kinds rather than overloading
free-form info messages:

```go
BoardMessageKindProtocol     BoardMessageKind = "protocol"
BoardMessageKindVerifyRubric BoardMessageKind = "verify_rubric"
BoardMessageKindVerifyReport BoardMessageKind = "verify_report"
BoardMessageKindJudgeVerdict BoardMessageKind = "judge_verdict"
```

These should remain append-only records. Read models can derive current
verification state from the latest relevant typed messages.

## Possible Verification Report Shape

Verification reports should use explicit fields:

```yaml
story_id: story-123
verifier: room-runtime-verifier-a
mode: single_candidate
recommendation: accept
aggregate_score: 85
confidence: medium
repetitions: 3
criteria:
  - id: correctness
    status: pass
    score: 94
    evidence: "go test ./cmd/foxctl/cmd passed"
  - id: scope_control
    status: pass
    score: 88
  - id: safety
    status: pass
    score: 82
  - id: evidence_quality
    status: pass
    score: 76
lanes:
  test: pass
  review: pass
commands:
  - go test ./cmd/foxctl/cmd
  - bash tests/regression/run.sh
findings:
  blocking: []
  non_blocking:
    - Consider adding GUI surfacing later.
artifacts:
  - path: docs/reviews/story-123-verify.md
    digest: sha256:...
```

Do not route or classify verification outcomes with keyword heuristics. Use
typed fields such as `recommendation`, `aggregate_score`, `criteria`, `lane`,
`story_id`, `artifact_path`, and `artifact_digest`.

## Possible Judge Verdict Shape

Judge verdicts should also be explicit and separate from verification reports:

```yaml
story_id: story-123
judge: room-runtime-judge-a
verdict: accept
reason: "Required lanes passed and verifier report has no blocking findings."
accepted_report_ids:
  - verify-report-456
waivers: []
```

This separation makes it clear which record is probabilistic verification and
which record is room authority.

## Enforcement Roadmap

### Phase 1: Convention

- Design doc and skill updates.
- Verifier/tester room onboarding.
- Required lanes on milestones.
- Explicit examples for story state and validation.

### Phase 2: Lightweight Enforcement

- Add prompt/onboarding tests.
- Add role checks for verifier/tester evidence lanes.
- Add coordinator-only final story acceptance in triad rooms.
- Add per-story required-lane checks before `done` if the protocol requires it.

### Phase 3: First-Class Commands

- Add `room protocol start triad`.
- Add `room verify rubric`.
- Add `room verify report`.
- Add `room judge verdict`.
- Keep commands append-only and backed by current room primitives where possible.

### Phase 4: Automation

The room loop can later detect explicit states and typed messages:

- story in review without verifier response,
- verifier block without implementer follow-up,
- verifier pass without judge verdict,
- required evidence lanes missing near milestone review,
- pending judge verdict while milestone appears ready.

Automation must use typed room state and message kinds, not substring matching.

## Open Questions

1. Should `judge` remain only an alias for `coordinator`, or become a separate
   advisory role?
2. Should triad rooms require per-story lane completeness before `done`, or is
   milestone-level evidence policy enough?
3. Should verifier rubrics be plain room messages in v1, or should the first
   code slice add a typed `verify_rubric` message?
4. Should red/green hidden-suite rooms be a triad profile, or stay as a separate
   specialized protocol?
5. What should the minimum score policy be if `verify report` supports numeric
   scoring?
6. Which verifier modes need provider logprobs, and which can work with normal
   structured model output plus deterministic checks?
