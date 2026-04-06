# Runtime Backend Contracts

This spec defines the minimum Go-owned contract for runtime backends in the
Go-native runtime migration. It exists to keep Jido optional in practice and to
make future backends or language workers pluggable behind one control-plane model.

## Purpose

The architecture and rollout docs now establish the dependency order:

1. Go owns runtime truth first.
2. Jido becomes an optional runtime adapter.
3. Engine pluggability comes later.

This spec fills in the missing implementation contract for that first step.

## Non-goals

- Replacing envelope v1 or v2 event-sourcing fundamentals.
- Defining model-provider or Eino integration details.
- Recreating BEAM/OTP semantics exactly inside Go.

## Contract principles

| Principle | Requirement |
|-----------|-------------|
| Go owns control-plane truth | Runtime state exposed to CLI/web/API must come from Go-owned stores/read models. |
| Adapters execute; Go records facts | Backends may launch and supervise workers, but canonical lifecycle facts must be normalized into Go contracts. |
| Replay-safe by default | Repeated lifecycle observations must be idempotent at the reconciler layer. |
| Runtime-neutral naming | Contracts should describe worker lifecycle and tree semantics, not Jido-specific transport details. |
| Stable read model | Runtime trees and status views need a deliberate compatibility story during migration. |

## Core entities

### Worker record

Represents one runtime-managed worker process or equivalent backend execution unit.

Required fields:

- `worker_id`
- `backend_kind`
- `backend_worker_ref`
- `agent_id`
- `run_id`
- `session_id`
- `parent_agent_id`
- `parent_worker_id`
- `workspace_id`
- `role`
- `status`
- `started_at`
- `updated_at`

Optional but strongly recommended:

- `request_id`
- `correlation_id`
- `causation_id`
- `stop_reason`
- `exit_code`
- `heartbeat_at`
- `backend_metadata`
- `tree_metadata`

### Identity mapping

The migration must support consistent lookup across these identities:

- runtime worker identity: `worker_id`
- backend-specific identity: `backend_worker_ref`
- v2 agent identity: `agent_id`
- v2 run identity: `run_id`
- session identity: `session_id`
- parent/child hierarchy identity: `parent_agent_id`, `parent_worker_id`
- transition mapping to legacy/runtime-specific ids where needed

Rule:

- every backend event must be resolvable to a stable `worker_id`
- UI/API/runtime-tree readers should not need backend-specific ids to function

## Required backend contract surface

The long-term seam is broader than `RuntimeSpawner`.

### Minimum backend interface set

Required responsibilities:

- spawn a child worker
- report lifecycle state changes
- accept cancellation or signal requests
- expose enough state for tree/status reconstruction
- survive repeated observation without duplicating control-plane effects

Suggested decomposition:

- `RuntimeSpawner`
  - `SpawnChild(...)`
- `RuntimeSignaler`
  - `SignalWorker(...)`
- `RuntimeStateReader`
  - `GetWorker(...)`
  - `ListChildren(...)`
  - `ListWorkersByRun(...)`
- `RuntimeReporter`
  - emits normalized lifecycle/status updates into Go-owned storage

The exact Go types can vary, but the contract surface needs all four concerns.

## Lifecycle state model

Backends must normalize worker state into a shared lifecycle vocabulary.

Minimum statuses:

- `pending`
- `starting`
- `running`
- `stopping`
- `completed`
- `failed`
- `cancelled`
- `unknown`

Allowed transitions:

- `pending -> starting`
- `starting -> running`
- `starting -> failed`
- `running -> stopping`
- `running -> completed`
- `running -> failed`
- `running -> cancelled`
- `stopping -> completed`
- `stopping -> failed`

Rules:

- terminal states are `completed`, `failed`, and `cancelled`
- transitions into terminal states must be monotonic
- repeated reports of the same terminal outcome must be idempotent
- `unknown` is for degraded observation, not a normal steady-state success path

## Lifecycle event schema

Backends must emit or translate worker observations into a normalized event schema.

Minimum event kinds:

- `worker_spawn_requested`
- `worker_spawned`
- `worker_started`
- `worker_heartbeat`
- `worker_state_changed`
- `worker_completed`
- `worker_failed`
- `worker_cancel_requested`
- `worker_cancelled`
- `worker_signal_sent`
- `worker_log_chunk`

Required event fields:

- `event_id`
- `event_kind`
- `observed_at`
- `worker_id`
- `backend_kind`
- `agent_id`
- `run_id`
- `status`

Recommended event fields:

- `parent_worker_id`
- `parent_agent_id`
- `session_id`
- `request_id`
- `correlation_id`
- `causation_id`
- `attempt`
- `stop_reason`
- `exit_code`
- `payload`

## Reconciler idempotency contract

The reconciler must treat backend observation as at-least-once input.

Idempotency rules:

1. A repeated observation of the same lifecycle fact must not append a semantically
   duplicate terminal event.
2. Status regression from a known terminal state is invalid unless explicitly modeled
   as a new worker attempt.
3. Heartbeats may update freshness timestamps without creating new semantic state
   transitions.
4. Log chunks are appendable, but they must not implicitly reopen terminal workers.
5. Retry attempts must be explicit in identity or attempt metadata rather than inferred
   from repeated spawn observations.

Suggested dedupe key shape:

- `worker_id`
- `event_kind`
- backend sequence number or backend event id when available
- fallback stable fingerprint over `(status, observed_at bucket, stop_reason, attempt)`

## Runtime tree read-model contract

CLI/web/API runtime tree views must be reconstructable from Go-owned state without
asking a backend-specific runtime API.

Minimum tree node fields:

- `agent_id`
- `worker_id`
- `parent_agent_id`
- `parent_worker_id`
- `status`
- `backend_kind`
- `role`
- `workspace_id`
- `started_at`
- `updated_at`

Recommended tree node fields:

- `run_id`
- `session_id`
- `stop_reason`
- `exit_code`
- `metadata`
- `children`

Compatibility rule:

- before removing Jido-backed tree reads, define whether the Go-native tree is
  intended to be shape-compatible with current API responses or whether specific
  differences are allowed

That decision must be explicit so GUI/API consumers do not absorb accidental
breaking changes.

## Supervisor guarantees for the default Go backend

The subprocess-backed Go runtime adapter must guarantee at least:

- bounded worker concurrency
- explicit start/stop ownership
- process-group-aware cancellation or equivalent cleanup
- non-blocking log capture
- durable worker registration before or at spawn handoff
- durable terminal-state recording
- restart-safe reattachment strategy or explicit unsupported-state policy

If process rediscovery after supervisor restart is not supported initially, the
system must document that limit and surface degraded state explicitly.

## Migration constraints

During the hybrid period:

- Jido-backed workers may still exist
- classic runtime paths may still coexist with v2 runtime paths
- some views may temporarily compare Go-native and Jido-backed state side by side

Required migration discipline:

- do not let backend-specific ids leak into canonical UI/API contracts
- do not split runtime truth across two equally authoritative read paths
- define which surfaces switch to Go-owned truth in each phase
- keep Jido support adapter-scoped while shrinking its role in default flows

## Phase-1 definition of done

Phase 1 is complete when:

1. Worker identity and lifecycle states are explicitly typed and documented.
2. A Go-owned registry can represent child spawn and lifecycle facts without Jido APIs.
3. Reconciler inputs and idempotency rules are specified.
4. Runtime tree readers have a defined minimum shape and compatibility story.
5. Supervisor guarantees and known non-goals are documented for the default Go backend.

## Related docs

- [../architecture/go-native-runtime-and-optional-jido.md](../architecture/go-native-runtime-and-optional-jido.md)
- [../plans/features/eino-go-native-runtime-plan.md](../plans/features/eino-go-native-runtime-plan.md)
- [../general/runtime-orchestration.md](../general/runtime-orchestration.md)
- [agent_hierarchy.md](agent_hierarchy.md)
