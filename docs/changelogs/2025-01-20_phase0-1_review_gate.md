# Phase 0-1 Implementation: Review Gate v1 (Kernel-Owned Dirtying)

**Date:** 2025-01-20

## Summary

Implemented Phase 0 (Pre-flight & Guardrails) and Phase 1 (Review Gate v1) of
the Universal SWE Grep & Agentic RAG integration plan. This establishes the
foundational review gate infrastructure with kernel-owned dirtying behavior.

## Changes

### Phase 0 – Pre-flight & Guardrails

- **Verified existing infrastructure:**
  - Jobs subsystem with state machine (`queued|running|ok|error|canceled`)
  - CAS store with put/get/head/gc operations
  - Named memory with optional vector support
  - `task_guard` and `file_guard` hooks
- **Confirmed all tests pass** across jobs, CAS, memory, and hooks

### Phase 1 – Review Gate v1

#### 1a. Extended Task struct with review fields

Added new status constants per `review_gate.md`:

- `StatusPending` (default)
- `StatusInProgress`
- `StatusReadyForReview`
- `StatusCompleted`
- `StatusBlocked`
- `StatusCanceled`

Added review status constants:

- `ReviewStatusOK`
- `ReviewStatusFailed`
- `ReviewStatusPending`
- `ReviewStatusStale`

Added review fields to Task:

- `LastReviewStatus` – current review outcome
- `LastReviewAt` – timestamp of last review
- `LastReviewID` – ID of most recent review artifact

#### 1b. Added migration for new task columns

- Extended SQLite schema with `last_review_status`, `last_review_at`,
  `last_review_id`
- Idempotent ALTER statements for existing databases
- Updated Add/Update/Get/ListByWorkspace/GetActive to handle new fields
- Updated scanTask/scanTaskRow to scan new columns

#### 1c. Implemented task_guard dirtying logic

Added `DirtyIfReviewed(ctx, taskID)` method to tasks.Store:

- Checks if task is in `ready_for_review` or `completed` status
- If so, demotes status to `in_progress`
- Marks any passing (`ok`) review as `stale`
- Returns `(task, dirtied, error)`

Updated `hooks/task_guard`:

- Calls `DirtyIfReviewed` on write operations
- Reports dirtying in output metadata (`dirtied`, `task_status`)
- Works in both `auto` and `strict` modes

#### 1d. Added comprehensive tests

Tasks store tests:

- `TestStore_DirtyIfReviewed_PendingTask` – no change
- `TestStore_DirtyIfReviewed_InProgressTask` – no change
- `TestStore_DirtyIfReviewed_ReadyForReviewTask` – demotes + marks stale
- `TestStore_DirtyIfReviewed_CompletedTask` – demotes + marks stale
- `TestStore_DirtyIfReviewed_FailedReviewNotMarkedStale` – demotes but keeps
  failed
- `TestStore_ReviewFields` – round-trip persistence

Task guard tests:

- `TestTaskGuard_AutoMode_DirtiesReadyForReviewTask`
- `TestTaskGuard_StrictMode_DirtiesCompletedTask`
- `TestTaskGuard_AutoMode_DoesNotDirtyInProgressTask`

## Files Modified

- `internal/storage/tasks/store.go` – Task struct, status/review constants,
  migration, CRUD, DirtyIfReviewed
- `internal/storage/tasks/store_test.go` – comprehensive dirtying + review field
  tests
- `skills/hooks_task_guard/main.go` – dirtying on write operations
- `skills/hooks_task_guard/main_test.go` – dirtying behavior tests

## Spec Alignment

This implementation aligns with:

- `docs/spec/review_gate.md` §5 (Dirtying Behavior)
- `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 0-1)
- `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 0-1 tests)

## Next Steps

- Phase 2: Semantic File Index v1
- Phase 3: Code Symbol Index v1
- Phase 4: SWE Grep Skill v1
