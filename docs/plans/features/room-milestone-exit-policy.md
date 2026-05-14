# Room Milestone Exit Policy

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Add explicit milestone exit-policy status on top of milestone contract, evidence lanes, review, and summary so coordinators can see whether a milestone is actually ready to exit and, in a later slice, optionally enforce that policy |
| Related | [room-milestone-contract.md](../../archive/plans/features/room-milestone-contract.md), [room-milestone-evidence-policy.md](../../archive/plans/features/room-milestone-evidence-policy.md), [room-milestone-synthesis.md](../../archive/plans/features/room-milestone-synthesis.md), [room-evidence-lanes.md](../../archive/plans/features/room-evidence-lanes.md), [room-epic-health-pulse.md](../../archive/plans/features/room-epic-health-pulse.md) |

## Why this slice

The room-agile model now has:

- milestone contracts
- evidence lanes
- required evidence lane policy
- milestone review
- milestone summary

But milestone exit is still split across several separate signals:

- accepted story coverage
- required evidence lanes
- review existence
- summary existence
- blocked or failed evidence

That means the system can describe the component parts of readiness, but it still does not expose one explicit milestone-level exit-policy read model.

## Goals

1. Define an explicit derived milestone exit-policy status.
2. Keep the first slice read-model only.
3. Make `milestone show`, `epic resume`, `epic next`, and `epic health` able to reason about exit readiness using one shared policy function.
4. Preserve current review write semantics in v1.
5. Keep the policy based on typed signals that already exist in room state.

## Non-goals

1. Auto-rejecting `milestone review pass` in the first slice.
2. Creating a new board-message kind.
3. Replacing milestone summary or evidence-lane rollups.
4. Requiring all optional lanes.
5. Using heuristic keyword matching against summary text or notes.

## Proposed model

Add a derived milestone exit-policy object to the milestone read model:

```json
{
  "exit_policy": {
    "status": "not_ready|ready_for_review|ready_for_summary|ready_to_exit|blocked",
    "reasons": ["missing_required_lane", "missing_review"],
    "checks": {
      "accepted_stories_covered": true,
      "required_lanes_satisfied": false,
      "has_blocking_story": false,
      "has_failed_validation": false,
      "has_review": false,
      "has_summary": false
    }
  }
}
```

## Status meanings

- `blocked`
  - there is a blocked story or failing validation evidence that should prevent normal exit
- `not_ready`
  - milestone is still missing required coverage or required lane policy
- `ready_for_review`
  - accepted stories are covered, required lanes are satisfied, and there is no review yet
- `ready_for_summary`
  - review exists but summary does not
- `ready_to_exit`
  - review and summary exist, no blocking signals remain

This status is distinct from the existing milestone `status` field, which still reflects review verdict history.

## First implementation slice

### 1. Derived milestone exit policy

Expose:

- `exit_policy.status`
- `exit_policy.reasons`
- `exit_policy.checks`

at least through:

- `milestone show`
- `epic resume`
- `epic health`

### 2. Epic guidance alignment

Use the derived exit policy to simplify and sharpen guidance:

- `epic next` should prefer the next missing exit step using the policy status
- `epic health` should report milestone exit problems using the same status/reason model

### 3. No enforcement yet

Do not hard-reject milestone review or summary writes in this slice.

## Behavior boundaries

### What changes now

- milestone read models gain one explicit exit-policy object
- epic guidance can target the next exit step more coherently
- operators no longer need to reconstruct readiness from scattered counters

### What does not change yet

- no mutation-path rejection
- no new protocol object
- no markdown-as-source-of-truth behavior

## Risks

1. Duplicating existing health logic instead of centralizing it
   - use one shared milestone exit-policy helper
2. Conflating review verdict history with exit readiness
   - keep `status` and `exit_policy.status` separate
3. Making the first slice too strict
   - stay read-model/guidance only

## Definition of done

1. milestone read model exposes a derived exit-policy object
2. `epic next` and `epic health` use that policy consistently
3. current review write semantics remain unchanged
4. focused tests cover:
   - blocked
   - missing required lane or missing validation
   - ready for review
   - ready for summary
   - ready to exit
5. review confirms the slice stays additive and does not prematurely enforce policy
