# Room Milestone Exit Enforcement

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Add an opt-in enforcement layer on top of the derived milestone exit policy so coordinators can choose whether `milestone review pass` should reject writes when milestone exit prerequisites are not satisfied |
| Related | [room-milestone-exit-policy.md](./room-milestone-exit-policy.md), [room-milestone-evidence-policy.md](./room-milestone-evidence-policy.md), [room-milestone-synthesis.md](./room-milestone-synthesis.md), [room-epic-health-pulse.md](./room-epic-health-pulse.md) |

## Why this slice

The room-agile model now exposes a shared derived `exit_policy` for milestones.

That means the system can already say:

- whether a milestone is blocked
- whether it is ready for review
- whether it is ready for summary
- whether it is ready to exit

But write-path behavior is still fully permissive:

- `milestone review pass` can still be recorded even when exit prerequisites are missing

That is the right default while the model stabilizes, but some rooms will want stricter coordinator discipline once the read model is trusted.

## Goals

1. Allow milestone review pass enforcement to be enabled explicitly.
2. Reuse the existing derived `exit_policy` helper rather than duplicating checks.
3. Keep blocking behavior narrow: only `milestone review pass`, not every room write.
4. Preserve a soft, guidance-only default for existing rooms.
5. Return actionable error envelopes when enforcement blocks a review write.

## Non-goals

1. Enforcing policy for all rooms by default.
2. Enforcing milestone summary writes in the first slice.
3. Creating a new board-message kind.
4. Reintroducing keyword- or note-based readiness checks.
5. Requiring optional evidence lanes.

## Proposed model

Add an opt-in enforcement switch on the milestone contract layer:

- `enforce_exit_policy`

Example:

```bash
agentctl room milestone contract <room-id> <milestone-id> \
  --enforce-exit-policy
```

Behavior:

- default: `false`
- when `false`, current behavior remains unchanged
- when `true`, `milestone review ... pass ...` should reject unless the milestone `exit_policy.status` is `ready_for_review` or `ready_to_exit`

## First implementation slice

### 1. Contract flag

Expose:

- `--enforce-exit-policy`

on:

- `milestone start`
- `milestone contract`

### 2. Read model

Expose:

- `enforce_exit_policy` on milestone contract/read model

### 3. Write-path enforcement

Only gate:

- `agentctl room milestone review <room-id> <milestone-id> pass ...`

When blocked, return:

- `EARG`
- clear `hint`
- the current `exit_policy.status`
- the missing `exit_policy.reasons`

### 4. Guidance stays aligned

`epic next` and `epic health` should keep using the same `exit_policy` helper.

## Allowed pass statuses under enforcement

Under `enforce_exit_policy=true`, pass review is allowed when:

- `exit_policy.status == ready_for_review`
- `exit_policy.status == ready_to_exit`

`ready_to_exit` remains allowed so a coordinator can re-record or restate a pass review without first mutating the milestone away from an already-exitable state.

Blocked otherwise:

- `blocked`
- `not_ready`
- `ready_for_summary`

Rationale:

- if summary is the missing step, a pass review already exists and a second pass review should not be the primary next action

## Behavior boundaries

### What changes now

- rooms can opt into hard review-pass discipline
- enforcement uses existing typed policy signals

### What does not change yet

- no default-on enforcement
- no enforcement for summary writes
- no enforcement for block review writes

## Risks

1. Over-correcting too early
   - keep it opt-in
2. Diverging read/write semantics
   - use the same shared `exit_policy` helper
3. Confusing rooms about why pass review was rejected
   - return explicit reasons and the current policy status in the envelope

## Definition of done

1. milestone contract supports `enforce_exit_policy`
2. milestone read model exposes the flag
3. pass review is rejected only when enforcement is enabled and `exit_policy` is not review-ready
4. review block writes remain allowed
5. focused tests cover:
   - enforcement off
   - enforcement on with `not_ready`
   - enforcement on with `ready_for_review`
   - enforcement on with `ready_for_summary`
6. review confirms the slice stays narrow and does not expand enforcement beyond pass-review writes
