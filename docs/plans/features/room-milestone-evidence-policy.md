# Room Milestone Evidence Policy

| Field | Value |
|-------|--------|
| Status | Draft |
| Scope | Add explicit milestone-level evidence expectations on top of evidence lanes so coordinators can declare which lanes are required for milestone exit without changing the append-only validation write path |
| Related | [room-evidence-lanes.md](./room-evidence-lanes.md), [room-milestone-contract.md](./room-milestone-contract.md), [room-milestone-synthesis.md](./room-milestone-synthesis.md), [room-epic-factory-mission-parity.md](./room-epic-factory-mission-parity.md) |

## Why this slice

The room-agile model now has:

- story validation
- evidence lanes
- milestone contracts
- milestone review and summary

What is still missing is the link between:

- what evidence a milestone says it expects
- what evidence its stories actually provide

Today:

- a milestone can list validators in the contract
- a story can expose lane-level proof
- milestone rollups can show lane presence

But the system still does not answer:

- which lanes are actually required for this milestone to exit
- whether missing lane coverage is advisory or blocking
- whether a milestone is ready for review under its own explicit evidence policy

That means coordinators still have to infer too much from raw rollups.

## Goals

1. Let milestones declare required evidence lanes explicitly.
2. Keep the story validation ledger append-only and unchanged.
3. Derive milestone evidence policy status from the existing lane summaries.
4. Make `milestone show`, `epic next`, and health views able to reason about missing required lanes.
5. Keep the first slice additive and coordinator-controlled.

## Non-goals

1. Replacing the existing milestone contract.
2. Creating a new top-level evidence object separate from `story_validation`.
3. Auto-failing milestone review writes based on policy in the first slice.
4. Changing story coverage semantics for all rooms at once.
5. Requiring all known lanes for every milestone.

## Proposed model

Treat milestone evidence policy as part of the milestone contract layer.

### New contract fields

Extend milestone contract with:

- `required_evidence_lanes`
- `optional_evidence_lanes`

Example:

```bash
foxctl room milestone contract <room-id> <milestone-id> \
  --validator review \
  --validator integration \
  --required-lane review \
  --required-lane integration \
  --optional-lane user_test
```

Interpretation:

- `validators_expected` remains the broader contract hint
- `required_evidence_lanes` is the explicit exit policy for v1
- `optional_evidence_lanes` is informative only
- `validators_expected` and `required_evidence_lanes` may intentionally diverge; for example, a validator may be listed for process context even when its lane is not required for milestone exit

If `required_evidence_lanes` is empty, current behavior remains unchanged.

## Derived status

Each milestone should expose:

- `required_lane_status`
- `required_lane_missing`
- `required_lane_covered`

Example:

```json
{
  "required_lane_status": "satisfied|missing|not_configured",
  "required_lane_missing": ["integration"],
  "required_lane_covered": ["review"]
}
```

Rules:

- `not_configured` when no required lanes are declared
- `satisfied` when every required lane has at least one accepted story with covered evidence in that lane
- `missing` otherwise

This is milestone-level policy status, not a change to raw story coverage.

## First implementation slice

### 1. Contract extension

Add milestone contract support for:

- `--required-lane`
- `--optional-lane`

These should be cumulative/deduped like the other contract list fields. They should use the same fixed lane key set as evidence lanes (`review`, `test`, `integration`, `user_test`, `manual_check`, `audit`), and optional lanes should stay disjoint from required lanes after merge.

### 2. Milestone read-model status

Expose:

- `required_evidence_lanes`
- `optional_evidence_lanes`
- `required_lane_status`
- `required_lane_missing`
- `required_lane_covered`

### 3. Epic next / health integration

Use required lane policy for coordinator guidance only:

- `epic next` may emit a “validate required lane” action
- `epic health` may emit a warn issue when required lanes are missing

This slice should not hard-block milestone review commands yet.

### 4. Work-pack rendering

Add the policy to:

- `milestone.md`
- `summary.md`

so resumptions do not require reconstructing expectations from chat.

## Behavior boundaries

### What changes now

- coordinators can declare required evidence lanes
- room views can show whether those lanes are satisfied
- summaries and health can reference missing required lanes

### What does not change yet

- `story validate` write path
- milestone review pass/block command semantics
- global story `covered` semantics
- automatic requirement of all lanes

## Risks

1. Confusing `validators_expected` with `required_evidence_lanes`
   - keep both fields visible and documented
2. Making policy enforcement too strict too early
   - first slice should warn/guide, not hard-reject review writes
3. Over-scoping at the story level
   - required lanes are milestone-level in v1, not per-story

## Definition of done

This slice is done when:

1. milestone contract supports required/optional evidence lanes
2. milestone read models expose derived required-lane status
3. epic next / health can reference missing required lanes
4. work-pack milestone rendering includes the policy
5. focused tests cover:
   - not configured
   - partially satisfied
   - fully satisfied
6. Cursor review confirms the slice is scoped tightly enough before implementation
