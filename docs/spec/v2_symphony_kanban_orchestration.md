# V2 Symphony Ingress and Kanban Orchestration

Status: Draft  
Version: 0.1  
Last Updated: 2026-03-05

## Related Specs

- `docs/spec/v2_greenfield_bootstrap.md`
- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/spec/agent_hierarchy.md`
- `docs/spec/overseer_profile.md`
- `docs/general/runtime-orchestration.md`

## 1. Purpose

Define how a Symphony-style issue-driven workflow fits into `foxctl` v2 without introducing a second orchestration runtime, and define the canonical Kanban read model for the UI.

This spec reframes Symphony as an ingress and scheduling layer over existing `foxctl` runtime boundaries.

## 2. Core Decision

Symphony is implemented as a pull-based ingress loop (adapter pattern), not as a parallel top-level orchestrator.

The system keeps one authority per concern:

1. Scheduler/ingress owns intake, eligibility, retries, reconciliation timing.
2. Overseer owns spawn and plan authority.
3. Hooks own policy checks at event boundaries.
4. v2 runner/event pipeline owns execution, persistence, and async enrichment.

## 3. Scope and Non-Goals

### In Scope

1. Tracker polling and issue normalization.
2. Eligibility and retry/backoff scheduling.
3. Scheduler -> overseer handoff contract.
4. Kanban board projection and lane definitions.
5. Runtime event/projection updates for board state.

### Out of Scope

1. Replacing `internal/runtime/orchestration/workflow/*` DAG skill workflows.
2. Bypassing overseer with direct scheduler spawn.
3. Defining provider-specific Codex JSON-RPC details.
4. Full multi-tenant control plane.

## 4. Architecture Placement

### 4.1 Control Plane

New v2 orchestration component:

1. Polls configured tracker source.
2. Computes candidate eligibility.
3. Schedules dispatch/retry/reconcile operations.
4. Emits orchestration events for projection and UI.

### 4.2 Execution Plane

Execution remains in current v2 pathways:

1. `RunService`/`LongLivedRunService` for turn execution.
2. Runtime supervisor components implementing `Run(ctx) error`.
3. Event append + event bus + enrichers.

### 4.3 Policy Plane

Policy enforcement remains where it is:

1. Overseer validates spawn depth/roles/policies.
2. Hooks enforce pre/post tool policy and action mediation.

Scheduler must never own spawn authorization or global plan mutation.

## 5. Ownership Boundaries

| Concern | Owner | Forbidden |
|---|---|---|
| Issue polling cadence, candidate ordering, retry timing | Scheduler | Direct session creation bypassing overseer |
| Spawn approval, depth limits, role constraints | Overseer | Scheduler-side alternate approval logic |
| Tool gating, input rewrite, block/approve decisions | Hooks | Hidden scheduler policy side effects |
| Turn execution and event append | v2 runner/services | Ad hoc side channels for execution |
| UI lane rendering | Projection/read model | UI mutating orchestration state directly |

## 6. Scheduler to Overseer Contract

Scheduler handoff must use the single canonical spawn path.

### Request

Use `spawn.Request` as the v2 service input, plus overseer context metadata:

1. `request_id` (required for idempotency)
2. `role`, `prompt`, `exec_mode`
3. `correlation_id`, `causation_id`
4. Optional runtime linkage (`run_id`, `agent_id`, `actor_id`)
5. Overseer spawn context fields (`caller_actor_id`, `caller_depth`, `caller_max_depth`, `caller_local_max_depth`, `epic_id`, optional `parent_plan_node_id`) carried in orchestration metadata and mapped into overseer spawn envelope at adapter boundary

Mapping note: in v2 architecture terms, `spawn.Request` is the concrete `SpawnIntent`, and `spawn.Response` is the concrete `SpawnDecision`.

### Decision

Invoke orchestration and spawn services once:

1. `OrchestrationService.DispatchIssue(ctx, issueID, requestID)` owns dispatch command handling.
2. Dispatch uses `SpawnService.Spawn(ctx, req)` as the only spawn entrypoint.
3. `SpawnService` routes through overseer/mail-based spawn evaluation (`agent.spawn` contract) and returns `spawn.Response`.
4. Scheduler and orchestration components must not call direct session creation APIs.

### Response Handling

Use `spawn.Response` as authoritative for service outcome:

1. Accept spawned agents only from successful `spawn.Response`.
2. Surface overseer denial details through orchestration events/projection card fields (`last_outcome`, `policy_status`, `denial_reason`, `suggestion`) without mutation.
3. Never fallback to direct spawn on denial.

## 7. Workflow Contract Strategy

`WORKFLOW.md` front matter is added as an orchestration config contract and does not replace existing YAML DAG workflows.

### 7.1 Coexistence Rules

1. `foxctl workflow run` continues to support existing `apiVersion: foxctl/v1` workflow YAML unchanged.
2. New orchestration config loader parses `WORKFLOW.md` front matter for scheduler settings.
3. These are separate engines with separate concerns.

### 7.2 Suggested Package Layout

1. `internal/runtime/orchestration/workflow/frontmatter` for `WORKFLOW.md` parsing/validation.
2. `internal/v2/core/orchestration` for canonical orchestration types/state machine.
3. `internal/v2/services/orchestration_service.go` for orchestration command boundary.
4. `internal/v2/runtime/orchestration/*` for long-lived dispatcher/reconciler components.
5. `internal/v2/adapters/libsql/orchestration/*` for durable projections.

## 8. State Model and Kanban Lanes

Kanban lanes are a projection over orchestration state.

### 8.1 Canonical Internal States

1. `Unclaimed`
2. `Claimed`
3. `Running`
4. `RetryQueued`
5. `Released`

### 8.2 UI Lanes

1. `Todo` -> `Unclaimed` and eligible.
2. `Claimed` -> reserved but not yet active.
3. `Running` -> active execution.
4. `RetryQueued` -> backoff pending.
5. `Blocked` -> denied by policy/overseer/hard validation.
6. `Review` -> handoff state (`Human Review`, etc.).
7. `Done` -> terminal tracker states.

`Blocked`, `Review`, and `Done` are read-model views derived from orchestration state + tracker state + policy outcomes.

### 8.3 Deterministic Lane Mapping (Precedence Ordered)

Projection must evaluate in this order and stop at first match:

| Precedence | Condition | Lane |
|---|---|---|
| 1 | `tracker_state` in configured terminal states | `Done` |
| 2 | `tracker_state` in configured handoff/review states | `Review` |
| 3 | `policy_status` in `{denied, blocked, validation_failed}` or `last_outcome` in `{spawn_denied, policy_denied, preflight_failed}` | `Blocked` |
| 4 | `orchestration_state == Running` | `Running` |
| 5 | `orchestration_state == RetryQueued` | `RetryQueued` |
| 6 | `orchestration_state == Claimed` | `Claimed` |
| 7 | `orchestration_state` in `{Unclaimed, Released}` and `eligibility == eligible` | `Todo` |
| 8 | `orchestration_state` in `{Unclaimed, Released}` and `eligibility != eligible` | `Blocked` |

Lane mapping must be total: every card record must satisfy exactly one row after precedence evaluation.

## 9. Kanban Projection Contract

The board is read-only projection data.

### 9.1 Board Shape

```json
{
  "version": 1,
  "status": "ok",
  "command": "orchestration/board-get",
  "data": {
    "generated_at": "2026-03-05T12:00:00Z",
    "counts": {
      "todo": 0,
      "claimed": 0,
      "running": 0,
      "retry_queued": 0,
      "blocked": 0,
      "review": 0,
      "done": 0
    },
    "lanes": [
      {
        "id": "running",
        "title": "Running",
        "cards": [
          {
            "issue_id": "abc123",
            "issue_identifier": "ABC-123",
            "title": "Implement X",
            "state": "Running",
            "run_id": "run-001",
            "actor_id": "actor:system:overseer",
            "attempt": 2,
            "retry_due_at": null,
            "last_event_type": "run.started",
            "last_event_at": "2026-03-05T11:59:30Z",
            "policy_status": "ok"
          }
        ]
      }
    ]
  },
  "meta": {
    "ts": "2026-03-05T12:00:00Z"
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

### 9.2 Board API Surface

1. `orchestration/board-get` returns envelope-wrapped board projection.
2. `orchestration/board-card-get` returns one card with current lane and recent events.
3. `orchestration/refresh` enqueues immediate reconcile/poll and returns envelope ack.

### 9.3 Bounded Output and Artifact Rules

1. Board responses must support bounded reads (`limit`, `cursor`, optional lane filter) to avoid unbounded payloads; default `limit` is 50 cards and max `limit` is 200 cards per request.
2. If serialized `data` exceeds the project large-output threshold (64KB), response must return `data.summary` + `data.artifact` (CAS digest) instead of full inline board payload.
3. Artifactized responses must preserve the same `command` and include retrieval instructions in `data.hint` when applicable.

### 9.4 Card Requirements

Each card should include:

1. Ticket identity (`issue_id`, `issue_identifier`, `title`).
2. Runtime identity (`run_id`, `actor_id`).
3. Scheduling state (`state`, `attempt`, `retry_due_at`).
4. Operability context (`last_event_type`, `last_event_at`, `policy_status`).
5. Projection context (`lane`, `eligibility`, `last_outcome`).

### 9.5 Projection Contract Notes

1. `state` is the internal orchestration state.
2. `lane` is derived using section 8.3 precedence rules.
3. `policy_status` and `last_outcome` are required for blocked/review explainability.

## 10. UI Interaction Rules

The Kanban board is command-driven, not state-authoritative.

### 10.1 Allowed UI Actions

1. Queue immediate reconcile/refresh.
2. Request claim/release.
3. Request retry now.
4. Request move to review/handoff when policy permits.

### 10.2 Disallowed UI Actions

1. Direct DB mutations.
2. Direct spawn/session creation.
3. Direct policy overrides.

All actions must route through orchestration services and keep overseer/hook checks in path.

### 10.3 Idempotency Requirements

1. All state-changing orchestration commands (`orchestration/refresh`, claim/release, retry-now, handoff requests) must require `request_id`.
2. Command handlers must be idempotent on `(command, scope_id, request_id)`, where `scope_id` is `issue_id` for issue-scoped actions and `workspace_id` for global actions such as `orchestration/refresh`.
3. Duplicate requests must return the prior terminal outcome envelope with explicit idempotency marker in `data` (for example `idempotent: true`).

## 11. Event and Projection Requirements

### 11.1 Event Requirements

Scheduler emits typed orchestration events at minimum:

1. `issue.discovered`
2. `issue.claimed`
3. `issue.dispatched`
4. `issue.retry_queued`
5. `issue.released`
6. `issue.blocked`
7. `issue.handoff`
8. `issue.done`

### 11.2 Projection Requirements

1. Projection is append-derived only.
2. Projection must be idempotent by event ID and request ID.
3. Projection must preserve last failure cause and denial reason for board cards.

### 11.3 SSE/Wide-Event Requirements

1. Orchestration command handlers emit wide events for:
   - `web.orchestration.board_get`
   - `web.orchestration.board_card_get`
   - `web.orchestration.refresh`
2. SSE forwarding must preserve orchestration metadata used by Runtime UI:
   - always: top-level `trace_id`
   - when present on source event data: `data.request_id`, `data.lane`, `data.last_outcome`
3. Operation-specific minimums:
   - `web.orchestration.board_card_get`: `data.request_id`, `data.lane`, `data.last_outcome`
   - `web.orchestration.board_get`: `data.request_id` plus board summary metadata (`data.card_count`, `data.lane_filter`)
   - `web.orchestration.refresh`: `data.request_id` plus refresh metadata (`data.queued`, `data.coalesced`, `data.idempotent`)
4. Additional optional fields may be present (`issue_id`, `issue_identifier`, `policy_status`, `eligibility`) but must not replace the operation-specific minimums above.

## 12. Failure and Safety Rules

1. Invalid `WORKFLOW.md` reload must keep last known good effective config.
2. Poll/dispatch errors must not crash long-lived runtime host.
3. Scheduler must continue reconciliation even when dispatch preflight fails.
4. Policy-denied runs must be visible in `Blocked` lane with reason.
5. No hidden fallback path that bypasses overseer or hooks.

## 13. Migration Plan (High-Level)

1. Introduce front matter parsing and typed orchestration config.
2. Introduce orchestration runtime component under v2 supervisor host.
3. Add orchestration events and projection store.
4. Add Kanban board API/read model.
5. Add UI board surface against projection API.
6. Keep existing `workflow` CLI behavior stable for YAML workflows.

## 14. Acceptance Criteria

1. One orchestration path per operation is preserved.
2. No direct scheduler spawn bypass exists.
3. Kanban board lanes map deterministically from projection state.
4. Board actions route through service contracts and respect overseer/hooks.
5. Event and projection tests cover retry, deny, handoff, and terminal transitions.
