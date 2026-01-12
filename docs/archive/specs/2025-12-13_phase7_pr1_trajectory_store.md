---
description: "Phase 7 PR1: Trajectory Store (SQLite)"
status: Approved
owner: jkatigbak
---

# Phase 7 PR1 – Trajectory Store (SQLite)

## Goal

Implement the initial SQLite-backed trajectory store used by Phase 7 capture and
export flows.

The store persists the Phase 7 index records defined in
`docs/spec/dspy_trajectory_capture.md`:

- `Trajectory`
- `UserRequestCapture`
- `TrajectoryEvent`

## Non-goals

- No Protocol v1 envelope schema changes.
- No new CLI commands.
- No `trajectory.export` job/exporter logic.

## Storage Location

- The trajectory store lives under the configured storage root (same root used
  for other local stores).
- SQLite database file: `<storage_root>/trajectory.db`.

## Data Model

### Tables

- `trajectories`
  - `(workspace_id, id)` primary key
  - `root_request_id`, `task_ids_json`, `epic_id`, `agent_role`, `job_id`,
    `trace_id`, `status`, `summary`, `artifact_digest`
  - `created_at`, `updated_at`

- `user_requests`
  - `(workspace_id, id)` primary key
  - `actor`, `source`, `ts`, `text`
  - `command_context_json`, `task_hints_json`

- `trajectory_events`
  - `id` primary key (ULID)
  - `(workspace_id, trajectory_id)` foreign key referencing
    `trajectories(workspace_id, id)` with `ON DELETE CASCADE`
  - `ts`, `kind`, `actor`, `command`, `status`
  - `data_inline_json`, `data_artifact`, `meta_json`

### JSON columns

For JSON columns, the store must write SQL `NULL` (not empty strings) when no
value is present to avoid SQLite JSON function failures.

## Query Semantics (v1)

- Task filtering uses exact JSON membership (no substring matching):

`EXISTS (SELECT 1 FROM json_each(trajectories.task_ids_json) WHERE value = ?)`

- Trace ID filtering across events uses JSON extraction:

`json_extract(trajectory_events.meta_json, '$.trace_id') = ?`

- List APIs return non-nil empty slices on no results.

## API

The store exposes a Go interface with:

- `InsertTrajectory`, `GetTrajectory`, `UpdateTrajectory`, `ListTrajectories`,
  `DeleteTrajectory`
- `InsertUserRequest`, `GetUserRequest`, `ListUserRequests`
- `InsertEvent`, `InsertEvents`, `ListEvents`, `GetEventsByTraceID`

## Design Diagram

```mermaid
graph TD
  A[Capture Hooks] --> B[trajectory.Open(storage_root)]
  B --> C[(trajectory.db)]

  A --> D[InsertUserRequest]
  A --> E[InsertTrajectory]
  A --> F[InsertEvent(s)]

  G[Export/Query] --> H[ListTrajectories/ListEvents]
  H --> C
```

## Rollout Plan

| Step | Action                                     | Validation                                     |
| ---- | ------------------------------------------ | ---------------------------------------------- |
| 1    | Add store types + SQLite schema/migrations | `CGO_ENABLED=0 go test ./...`                  |
| 2    | Add query correctness filters and indexes  | Unit tests for exact task match + trace lookup |
| 3    | Run lint                                   | `make lint`                                    |

## Rollback Plan

- Revert the commit(s).
- SQLite schema changes are additive and idempotent; leaving the DB file is
  acceptable.

## Test Plan

- Unit tests for:
  - Migrations are idempotent.
  - CRUD for trajectories, user requests, events.
  - Task filter does not match substrings.
  - Trace lookup uses JSON extraction and returns correct results.
  - List methods return empty (non-nil) slices.
- `CGO_ENABLED=0 go test ./...`
- `make lint`

## Approval

To proceed with implementation and commit, change `status:` above to `Approved`.
