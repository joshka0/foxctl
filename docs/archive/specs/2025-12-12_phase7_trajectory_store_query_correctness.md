---
description: Phase 7 Trajectory Store Query Correctness Fixes (PR1 follow-up)
status: Approved
owner: jkatigbak
---

# Phase 7 – Trajectory Store Query Correctness Fixes

## Goal

Fix correctness and safety issues in the SQLite-backed trajectory store:

- Exact task id filtering (avoid substring matches like `task1` matching
  `task10`).
- Safe trace id filtering (avoid brittle `LIKE` matching on JSON).
- Ensure list APIs return non-nil empty slices (`[]`) rather than `null` when
  marshaled.

## Non-goals

- No envelope / `meta.*` contract changes.
- No new CLI commands, jobs, or export logic.
- No new cross-database behavior (SQLite is the authoritative implementation for
  this store).

## Background / Problem Statement

Current implementation issues:

- `task_ids_json LIKE '%<taskID>%'` can match substrings and does not enforce
  element boundaries.
- `meta_json LIKE '%"trace_id":"<traceID>"%'` is unsafe (false
  positives/negatives, sensitive to whitespace/ordering) and not robust to JSON
  encoding.
- JSON columns sometimes store `""` (empty string) for “no value”, which makes
  SQLite JSON functions error (`malformed JSON`).
- Several list methods may return `nil` slices, which encode as JSON `null`.

## Proposed Change (v1)

### 1) Exact JSON membership for task id

Replace substring matching with SQLite JSON table-valued functions:

- Filter predicate:

`EXISTS (SELECT 1 FROM json_each(trajectories.task_ids_json) WHERE json_each.value = ?)`

- Parameter passed is the raw `filter.TaskID`.

### 2) Safe trace id filtering

Replace `LIKE` matching with JSON extraction:

- Filter predicate:

`json_extract(trajectory_events.meta_json, '$.trace_id') = ?`

- Parameter passed is the raw `traceID`.

### 3) Store `NULL` for absent JSON

For JSON-typed columns, store SQL `NULL` (not empty string) when no value is
present:

- `trajectories.task_ids_json`
- `user_requests.command_context_json`
- `user_requests.task_hints_json`
- `trajectory_events.data_inline_json`
- `trajectory_events.meta_json`

This guarantees JSON functions operate safely.

### 4) Return empty slices

Ensure list functions return `[]T{}` (non-nil) when empty:

- `ListTrajectories`
- `ListUserRequests`
- `ListEvents`
- `GetEventsByTraceID`

### 5) Indexing

Add an expression index for trace lookup performance:

`CREATE INDEX IF NOT EXISTS idx_events_trace_id ON trajectory_events(workspace_id, json_extract(meta_json, '$.trace_id'));`

## Data Model / Migration

No new tables.

Migration changes:

- Add `idx_events_trace_id` expression index.

## Design Diagram (data flow)

```mermaid
graph TD
  A[Trajectory store caller] --> B[ListTrajectories(filter.TaskID)]
  B --> C[(SQLite trajectories)]
  C --> D[json_each(task_ids_json) exact equality]

  A --> E[GetEventsByTraceID(workspaceID, traceID)]
  E --> F[(SQLite trajectory_events)]
  F --> G[json_extract(meta_json,'$.trace_id') = traceID]
```

## Rollout Plan

| Step | Action                                              | Risk    | Validation                                                |
| ---- | --------------------------------------------------- | ------- | --------------------------------------------------------- |
| 1    | Implement JSON-safe storage of NULL vs empty string | Low     | Unit tests cover inserts + JSON_EXTRACT/json_each queries |
| 2    | Update task filter + trace filter SQL               | Low/Med | Unit tests for exact match and trace lookup               |
| 3    | Add expression index for trace_id                   | Low     | Migration idempotency test; `make lint`                   |

## Rollback Plan

- Revert commit.
- SQLite schema is forward compatible with extra indexes; removing the index is
  optional.

## Test Plan

- Unit tests:
  - `ListTrajectories` task filter: `task1` must not match `task10`.
  - `GetEventsByTraceID` uses JSON_EXTRACT and returns correct events only.
  - Empty results return non-nil empty slices.
  - Regression: JSON columns stored as NULL do not break scanning.

## Approval

To proceed with implementation, change `status:` above to `Approved`.
