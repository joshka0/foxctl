# 2025-11-30 – Phase 1-A: Review Operations for todo/manage

## Summary

Implemented `review_request` and `review_status` operations for the `todo/manage`
skill as part of Phase 1 (Review Gate v1) of the Universal SWE Grep & Agents plan.

## Changes

### Skills

- **`skills/todo/main.go`**:
  - Added `reviewRequestReq` and `reviewStatusReq` input types.
  - Extended `input` struct with `ReviewRequest` and `ReviewStatus` fields.
  - Added `operation="review_request"` and `"review_status"` branches in `run()`.
  - Implemented `handleReviewRequest`:
    - Validates task is in `in_progress` or `ready_for_review` status.
    - Rejects requests for `pending`, `completed`, `blocked`, `canceled` tasks.
    - Sets `Status` → `ready_for_review`, `LastReviewStatus` → `pending`.
    - Generates a ULID for `LastReviewID`.
  - Implemented `handleReviewStatus`:
    - Returns `last_review_status`, `last_review_at`, `last_review_id` for a task.
  - Added `formatTime` helper for safe *time.Time formatting.
  - Extended `taskOutput` struct with `LastReviewStatus`, `LastReviewAt`,
    `LastReviewID` fields.
  - Updated `toOutput` to include review fields in envelope output.

- **`skills/todo/main_test.go`**:
  - Added `setTaskStatus` helper method for test setup.
  - Added tests:
    - `TestTodoReviewRequest_InProgressTask` – happy path.
    - `TestTodoReviewRequest_PendingTask_Rejected` – rejection for pending.
    - `TestTodoReviewRequest_CompletedTask_Rejected` – rejection for completed.
    - `TestTodoReviewStatus_ReturnsFields` – review status probe.

### Documentation

- **`docs/impl_plan/universal_swe_grep_and_agents_specs_phase1_review_gate_todo.md`**:
  - Created Phase 1 review gate todo spec with sections A, B, C.
  - Marked section A (review operations) as completed.

## Spec Alignment

This implementation aligns with:

- `docs/spec/review_gate.md` §todo/manage.review_request and §todo/manage.review_status.
- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase1_review_gate_todo.md` section A.

## Next Steps

- **B**: Upgrade `todo/manage.complete` to enforce review gate semantics.
- **C**: Implement review artifact storage + CAS wiring.
