# Implementation Plan: V2 Greenfield Bootstrap

Status: Approved (A+)
Iterations: 4
Source Spec: `docs/spec/v2_greenfield_bootstrap.md`
Working Tracker: `docs/plans/v2-implementation-todo.md`

## Companion and General References

- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/general/runtime-orchestration.md`
- `docs/general/agent-daemon.md`
- `docs/general/memory.md`
- `docs/general/companion-memory.md`
- `docs/general/context-and-observability.md`
- `docs/architecture/system-architecture.md`
- `docs/designs/hierarchical-memory-retrieval.md`
- `docs/designs/progressive-memory-system.md`

## Problem Statement

The current v1 agentctl runtime has accumulated dual tool systems (Registry for MCP, Runtime executor for daemon agents), duplicate spawn/ask logic across CLI/API/daemon transports, and no unified event stream. This plan bootstraps a clean v2 runtime with:

- One execution loop (staged pipeline)
- One tool system (catalog + executor)
- One spawn/ask orchestration path (services layer)
- Thin transports (CLI/Web/Daemon as ports)
- Deterministic, test-first behavior (golden tests + fakes)
- Go-native non-blocking control-plane components for background work
- Turn intelligence pipeline for embeddings/annotations/classification over immutable turns

## Architecture Decision

**Approach**: Strangler Fig pattern — v2 lives in `internal/v2/` alongside v1. A per-command feature flag (`AGENTCTL_V2_COMMANDS=spawn,ask,run,list,kill`) routes specific commands to v2 while v1 remains the default behavior.

**Why**: Allows incremental migration with low risk to v1 stability. Each command can be validated independently before cutting over. Rollback is trivial (remove command from flag).

**Safety model**: No command-level `--dry-run` gate is required for v2 rollout. Safety comes from per-command feature flags, idempotency keys (`request_id`), append-only events, and scoped rollback.

**Go-native runtime model**: Keep turn execution request-scoped and direct (`services -> runner -> store`), and run cross-cutting/background workloads in supervisor-managed `Run(ctx)` components with bounded channels and immutable snapshot reads.

**Alternatives rejected**:
- Big-bang rewrite: too risky for a solo maintainer
- In-place refactor: v1's dual tool systems and scattered orchestration make clean separation impractical

**Dependency direction**:
```
ports → services → core
services → adapters (through interfaces)
runtime → core
runtime → adapters (through interfaces)
core imports no adapters
```

### V2 Scope Guardrails

- All behaviors in this plan apply to `internal/v2/*` paths.
- v1 command behavior does not change unless that command is listed in `AGENTCTL_V2_COMMANDS`.
- Non-blocking/background guarantees in this plan are acceptance criteria for v2-routed commands only.

## Design Patterns

| Pattern | Location | Rationale |
|---------|----------|-----------|
| Command | `internal/v2/services/{spawn,ask,run,list,kill}_service.go` | Isolate operation behavior behind request/response DTOs |
| Pipeline / Chain of Responsibility | `internal/v2/runtime/runner/*` | Deterministic stage execution, composable and testable |
| Repository | `internal/v2/core/events` interfaces + `internal/v2/adapters/libsql/*` | Append-only persistence with projection reads |
| Adapter | `internal/v2/adapters/v1bridge/*` | Call v1 runtime/hooks/tools without duplicate logic |
| Supervisor | `internal/v2/runtime/supervisor/*` | Standard `Run(ctx)` lifecycle for background components |
| Single Writer | `internal/v2/runtime/*` state loops | Clear ownership of mutable state, fewer race conditions |
| Snapshot Cache | `internal/v2/runtime/snapshots/*` | Lock-light reads for hot paths and status surfaces |
| Strangler Fig | `internal/v2/ports/{cli,api,daemon}` + command flag gate | Incremental migration with per-command opt-in |

## Error Contract

**File**: `internal/v2/core/errors/errors.go`

```go
package errors

type ErrorKind string

const (
    ErrNotFound        ErrorKind = "not_found"
    ErrPolicyViolation ErrorKind = "policy_violation"
    ErrTimeout         ErrorKind = "timeout"
    ErrToolFailed      ErrorKind = "tool_failed"
    ErrStageFailed     ErrorKind = "stage_failed"
    ErrInternal        ErrorKind = "internal"
    ErrValidation      ErrorKind = "validation"
    ErrDependency      ErrorKind = "dependency"
)

type EventContext struct {
    StreamID      string
    StreamType    events.StreamType
    Command       string
    CorrelationID string
    CausationID   string
    ActorID       string
    RequestID     string
}

type V2Error struct {
    Kind      ErrorKind
    Message   string
    Cause     error
    Fatal     bool
    Retryable bool
    Details   map[string]any
}

func (e *V2Error) Error() string
func (e *V2Error) HTTPStatus() int
func (e *V2Error) IsFatal() bool
func (e *V2Error) ToEvent(ctx EventContext, evtType events.EventType) events.Event
```

**HTTP mapping**:
- `ErrNotFound` → 404
- `ErrPolicyViolation` → 403
- `ErrTimeout` → 408
- `ErrToolFailed` → 502
- `ErrStageFailed` → 500 if fatal; degraded output if non-fatal
- `ErrValidation` → 400
- `ErrInternal` / `ErrDependency` → 500

## Turn Model + Trace Placement

Turn data model is hierarchical and immutable:

1. `Turn` (request lifecycle root)
2. `Iteration` (one model cycle)
3. `ToolCall` (zero or more per iteration)

Trace placement:

1. Turn: `trace_id`, `root_span_id`, `correlation_id`, `causation_id`
2. Iteration: child `span_id`, `parent_span_id`, `trace_id`
3. ToolCall: child `span_id`, `parent_span_id`, `trace_id`

Reference forms for context building:

1. whole turn: `turn/{turn_id}`
2. iteration: `turn/{turn_id}/iter/{index}`
3. tool call: `turn/{turn_id}/iter/{index}/tool/{call_id}`
4. span slice: `turn/{turn_id}#msg:{msg_id}:{start}-{end}`

Rule: enrichment jobs (embedding/annotation/classification) are asynchronous and idempotent by `(turn_id, artifact_type, artifact_version)` and must not block turn completion.

## Non-Blocking Contract (V2 Routes)

1. Turn write path must not wait on enrichers.
2. Enrichers consume `turn.recorded` events asynchronously via bounded queues.
3. Missing artifacts degrade context quality only; they do not fail turn completion.
4. Enricher failures emit `artifact.failed` events and retry policy metadata without retroactively failing a completed turn.

## Completion Review Gate

For every PR slice in this plan, completion requires a subagent second-pass review
note (reviewer, scope, findings, decision) before marking the slice done.
Use the protocol in `docs/spec/v2_repo_rules_and_skills.md`.

## Kickoff Plan (Start Now)

The first implementation slice should lock contracts and test determinism before
feature migration.

### Kickoff Batch A: Contracts + Compile-Safe Skeleton (PR-01A)

Goal: create `internal/v2` package tree, core interfaces/types, and no-op
adapters with zero behavior change.

Tasks:
- [ ] create `internal/v2/core/{events,run,spawn,ask,list,kill,tool,policy,services}`
- [ ] create `internal/v2/runtime/{runner,supervisor,snapshots}`
- [ ] create `internal/v2/ports/config/v2flags.go`
- [ ] add compile-only tests for package shape and imports

Exit checks:
- [ ] `go test ./internal/v2/...` passes
- [ ] `go test ./...` passes

### Kickoff Batch B: Error/Event Contracts + Golden Harness (PR-01B)

Goal: define canonical v2 error/event structures and deterministic fixture
harness before implementing behavior.

Tasks:
- [ ] implement `internal/v2/core/errors/errors.go`
- [ ] implement `internal/v2/core/events/{types,payloads}.go`
- [ ] add deterministic fakes (`fake_clock`, `fake_uuid`, `fake_event_store`)
- [ ] add first golden fixture and comparator utility for JSONL events

Exit checks:
- [ ] `TestV2Error_HTTPStatusAndToEvent` passes
- [ ] `TestEnvelopeContract_V2Output_V1Shape` passes
- [ ] golden fixtures are byte-stable in CI

### Kickoff Batch C: Event Store Vertical Slice (PR-02A)

Goal: wire append-only events and projection replay for one command path
(`spawn`) behind tests, without routing live traffic yet.

Tasks:
- [ ] add libsql-first schema/store for v2 events with monotonic stream version
- [ ] add minimal projection tables (`agent_state`, `run_state`)
- [ ] implement replay utility + idmap primitives
- [ ] add integration tests for append/replay/idmap roundtrip

Exit checks:
- [ ] event append/replay tests pass
- [ ] idmap tests pass
- [ ] no v1 command path changes

## File Changes

### PR-01: V2 Skeleton + Contracts

**Scope**: Publish canonical v2 package shape, types, error/event contracts, policy constraints, and v2 flag parser.

| File | Status |
|------|--------|
| `docs/spec/v2_greenfield_bootstrap.md` | modified |
| `docs/spec/v2_repo_rules_and_skills.md` | new |
| `internal/v2/core/errors/errors.go` | new |
| `internal/v2/core/events/types.go` | new |
| `internal/v2/core/events/payloads.go` | new |
| `internal/v2/core/run/types.go` | new |
| `internal/v2/core/run/context.go` | new |
| `internal/v2/core/spawn/types.go` | new |
| `internal/v2/core/ask/types.go` | new |
| `internal/v2/core/list/types.go` | new |
| `internal/v2/core/kill/types.go` | new |
| `internal/v2/core/tool/types.go` | new |
| `internal/v2/core/policy/constraints.go` | new |
| `internal/v2/core/services/interfaces.go` | new |
| `internal/v2/runtime/runner/stage.go` | new |
| `internal/v2/runtime/runner/pipeline.go` | new |
| `internal/v2/ports/config/v2flags.go` | new |

**Key types**:
- `run.StageFailure` — required element of `TurnOutput` for degraded turns
- `policy/constraints.go` — pure validators for max depth, max iterations, tool-invocation cap, timeout bounds, profile capability checks
- `v2flags.go` — parses `AGENTCTL_V2_COMMANDS` with command set normalization
- `Component` (`Run(ctx)`) and snapshot interfaces — foundation for non-blocking control-plane services
- turn hierarchy structs with trace lineage fields (`Turn`/`Iteration`/`ToolCall`)

**Tests** (6):
1. `TestV2Flags_Parse_DefaultOff` — empty flag routes nothing to v2
2. `TestV2Flags_Parse_CommandSetNormalization` — spacing, duplicates, invalid command rejection
3. `TestConstraints_MaxDepth_RejectsNegativeOrExcessive` — hard bound checks
4. `TestV2Error_HTTPStatusAndToEvent` — status mapping and event population
5. `TestStageFailure_StructureRoundTrip` — JSON compatibility in TurnOutput
6. `TestEnvelopeContract_V2Output_V1Shape` — validates `version`, `status`, `meta.ts`, and `error` shape compatibility

**Definition of done**: `go test ./...` passes, zero behavior change for v1 paths.

---

### PR-02: Event Store + Projections

**Scope**: Append-only event persistence, projection read models, v1↔v2 ID mapping.

| File | Status |
|------|--------|
| `internal/v2/core/events/repository.go` | new |
| `internal/v2/adapters/libsql/events/schema.go` | new |
| `internal/v2/adapters/libsql/events/store.go` | new |
| `internal/v2/adapters/libsql/events/replay.go` | new |
| `internal/v2/adapters/libsql/projections/schema.go` | new |
| `internal/v2/adapters/libsql/projections/store.go` | new |
| `internal/v2/adapters/libsql/projections/replay.go` | new |
| `internal/v2/adapters/libsql/idmap/schema.go` | new |
| `internal/v2/adapters/libsql/idmap/store.go` | new |

**Key implementation points**:
- Enforce monotonic `stream_version` per `(stream_id, stream_type)` as single ordering source
- Projection-upsert flow materializes `agent_state`/`run_state` from canonical events
- v1↔v2 ID mapping for `kill`/`list` interoperability
- `replay` utilities are idempotent and safe for partial recovery

**Tests** (5):
1. `TestEventAppend_EnforcesMonotonicVersion` — version conflicts rejected
2. `TestEventReplay_RebuildsProjection` — deterministic projection state after replay
3. `TestProjectionStore_LegacyEntityLookup` — ID mapping for v1 input
4. `TestIdMap_WriteRead_RoundTrip` — unique mapping and immutability
5. `TestReplay_Failure_RetriedAsInternalRetryable` — error classification

**Definition of done**: Event append tests + projection replay tests pass.

---

### PR-03: Runner Pipeline (No Transport)

**Scope**: Ordered runner stages, turn lifecycle orchestration, no-rollback degraded-stage behavior.

| File | Status |
|------|--------|
| `internal/v2/runtime/runner/pipeline.go` | modified |
| `internal/v2/runtime/runner/init_context.go` | new |
| `internal/v2/runtime/runner/resolve_dependencies.go` | new |
| `internal/v2/runtime/runner/pre_hooks.go` | new |
| `internal/v2/runtime/runner/build_toolset.go` | new |
| `internal/v2/runtime/runner/model_call.go` | new |
| `internal/v2/runtime/runner/post_hooks.go` | new |
| `internal/v2/runtime/runner/persist_turn.go` | new |
| `internal/v2/runtime/runner/emit_events.go` | new |
| `internal/v2/testkit/fakes/fake_clock.go` | new |
| `internal/v2/testkit/fakes/fake_uuid.go` | new |
| `internal/v2/testkit/fakes/fake_model.go` | new |
| `internal/v2/testkit/fakes/fake_tool_executor.go` | new |
| `internal/v2/testkit/fakes/fake_event_store.go` | new |

**Canonical pipeline stage order**:
1. `InitContext`
2. `ResolveDependencies`
3. `ApplyPreHooks`
4. `BuildToolset`
5. `ModelCall`
6. `ApplyPostHooks`
7. `PersistTurn`
8. `EmitEvents`

**Stage failure policy**: Record-and-continue (no rollback). Non-fatal stage errors append `StageFailure` to `TurnOutput` and continue as degraded turn. Fatal errors terminate and publish terminal failure event.

**Golden test format**:
- Location: `internal/v2/runtime/runner/testdata/golden_events/*.jsonl`
- Format: one JSON event per line (JSONL), each object matching `events.Event`
- Required deterministic fields: `id`, `stream_id`, `stream_type`, `stream_version`, `sequence`, `event_type`, `occurred_at`, `correlation_id`, `request_id`, `payload`
- Comparison: byte-exact on marshaled canonical JSONL after deterministic execution with `fake_uuid` and `fake_clock`
- Rule: fail test on key/field omissions; no semantic-only differences allowed
- Example fixture: `run_worker_toolflow_success.jsonl` validating exact sequence: `run.started` → `tool.invoked` → `tool.responded` → `turn.recorded` → `run.completed`
- Validates `sequence` increments by 1 (no gaps) and `stream_version` progression equals persisted count

**Tests** (5):
1. `TestPipeline_HappyPath_OrderedExecution` — full stage chain and event order
2. `TestPipeline_MaxIterations_StopsAndMarksDegraded` — bounded loop behavior
3. `TestPipeline_StageFailure_NonFatalContinues` — degraded turn output and event continuity
4. `TestPipeline_ContextCancel_StopsBeforePersist` — cancellation with no further writes
5. `TestPipeline_Golden_EventOrder_StableOutput` — exact JSONL fixture match

**Definition of done**: Pipeline unit tests by stage + golden tests for event emission order pass.

---

### PR-04: Unified Tool Stack

**Scope**: Single catalog/executor path, profile-aware allowlist, v1 tool bridge.

| File | Status |
|------|--------|
| `internal/v2/core/tool/types.go` | modified |
| `internal/v2/runtime/tools/types.go` | new |
| `internal/v2/runtime/tools/catalog.go` | new |
| `internal/v2/runtime/tools/executor.go` | new |
| `internal/v2/runtime/profiles/profiles.go` | new |
| `internal/v2/adapters/v1bridge/tool_bridge.go` | new |
| `internal/v2/testkit/fakes/fake_tool_executor.go` | modified |

**Key implementation points**:
- Parse typed tool args/results from struct payloads
- Resolve tool profiles from `profiles` package; deny by default
- Wrap v1 tool executor through `v1bridge` adapter with v2 event/error semantics

**Tests** (5):
1. `TestToolCatalog_AllowsOnlyProfileTools` — strict profile allowlist
2. `TestToolExecutor_ArgBindingRejectsInvalidSchema` — typed args validation
3. `TestToolExecutor_PassesThroughSuccessPayload` — successful v1 tool adaptation
4. `TestToolExecutor_MapsToolFailureToErrToolFailed` — error mapping
5. `TestExecutor_UnknownToolReturnsPolicyViolation` — defensive error for unregistered tool

**Definition of done**: All v2 tools executed through one path, schema/validation tests per tool pass.

---

### PR-05: Unified Spawn/Ask/Run/List/Kill Services

**Scope**: Full v2 service layer, shared validation/orchestration, v1 interop via ID mapping.

| File | Status |
|------|--------|
| `internal/v2/services/spawn_service.go` | new |
| `internal/v2/services/ask_service.go` | new |
| `internal/v2/services/run_service.go` | new |
| `internal/v2/services/list_service.go` | new |
| `internal/v2/services/kill_service.go` | new |
| `internal/v2/services/dependencies.go` | new |
| `internal/v2/adapters/v1bridge/spawn_bridge.go` | new |
| `internal/v2/adapters/v1bridge/ask_bridge.go` | new |
| `internal/v2/adapters/v1bridge/kill_bridge.go` | new |

**Key implementation points**:
- Centralize request validation and policy checks in services
- Use `RunEngine` for turn execution, projection-backed repository reads for list
- Map request IDs to idempotency where supported
- Cross-version kill/list via idmap repository

**Tests** (5):
1. `TestSpawnService_ValidInputCreatesV2Record` — spawn output and run/agent IDs
2. `TestSpawnService_DuplicateRequestIDIdempotent` — deduping behavior
3. `TestAskService_PolicyViolationReturns403Mapping` — policy gate behavior
4. `TestKillService_V1IDMappedToV2` — id-map fallback path
5. `TestListService_UsesProjectionAndFilters` — stable v2 list contract

**Definition of done**: CLI/API/daemon integration tests hit same service methods, no duplicate spawn logic in v2 ports.

---

### PR-06: V2 Ports + Feature-Flagged Strangler Routing

**Scope**: Wire CLI/API/daemon to service layer, per-command opt-in, v1 default.

| File | Status |
|------|--------|
| `internal/v2/ports/config/v2flags.go` | modified |
| `internal/v2/ports/config/v2flags_test.go` | new |
| `internal/v2/ports/cli/router.go` | new |
| `internal/v2/ports/cli/spawn.go` | new |
| `internal/v2/ports/cli/ask.go` | new |
| `internal/v2/ports/cli/run.go` | new |
| `internal/v2/ports/cli/list.go` | new |
| `internal/v2/ports/cli/kill.go` | new |
| `internal/v2/ports/api/router.go` | new |
| `internal/v2/ports/api/mappers.go` | new |
| `internal/v2/ports/daemon/router.go` | new |
| `cmd/agentctl/cmd/agent.go` | modified |
| `internal/web/api/agents.go` | modified |
| `internal/actor/agent_actor.go` | modified |
| `internal/agent/runtime/runtime.go` | modified |

**Key implementation points**:
- Parse `AGENTCTL_V2_COMMANDS` and route only matching commands to v2
- All unmatched commands route to existing v1 handlers unchanged
- Keep v1 execution logic untouched; PR-06 changes are routing and wiring only
- Inject observability fields on every dispatch (`command`, `decision=v1|v2`, `correlation`)
- Maintain API payload compatibility unless v2 adds extras

**Tests** (6):
1. `TestRouter_DefaultV1WhenFlagUnset` — strict fallback behavior
2. `TestRouter_SingleCommandOptInToV2` — only requested command uses v2
3. `TestCLI_API_ParityShellTest_Ask` — command shape compatibility across ports
4. `TestDaemonDispatch_SpawnAskKillRouting` — actor runtime path selection
5. `TestKillCommandRollbackToV1WhenNotEnabled` — per-command opt-out behavior
6. `TestRouter_EnvelopeContract_ParityV1V2` — verifies `version`, `status`, `meta.ts`, and `error` contract parity

**Definition of done**: Side-by-side run capability (v1 default, v2 opt-in), smoke tests for spawn/list/ask/kill pass, and disabled commands preserve v1 behavior.

---

### PR-07: Supervisor + Runtime Event Bus

**Scope**: Add Go-native background component lifecycle and bounded runtime event fanout.

| File | Status |
|------|--------|
| `internal/v2/runtime/supervisor/component.go` | new |
| `internal/v2/runtime/supervisor/host.go` | new |
| `internal/v2/runtime/supervisor/host_test.go` | new |
| `internal/v2/runtime/events/bus.go` | new |
| `internal/v2/runtime/events/bus_test.go` | new |

**Key implementation points**:
- Components implement `Run(ctx context.Context) error`
- Host uses `errgroup` for start/stop and cancellation propagation
- Event bus is bounded with explicit overflow policy (drop/backpressure) and telemetry

**Tests** (5):
1. `TestSupervisor_StartsAndStopsAllComponents` — full lifecycle
2. `TestSupervisor_ContextCancel_StopsComponents` — cancellation propagation
3. `TestSupervisor_ComponentError_FailsHost` — error handling policy
4. `TestEventBus_BoundedQueue_OverflowPolicy` — explicit overflow behavior
5. `TestEventBus_NoSubscriberDeadlock` — fanout safety

**Definition of done**: Background components can run/stop independently without blocking turn execution.

---

### PR-08: Snapshots + Non-Blocking Maintenance

**Scope**: Add immutable runtime snapshots and first maintenance component under supervisor.

| File | Status |
|------|--------|
| `internal/v2/runtime/snapshots/store.go` | new |
| `internal/v2/runtime/snapshots/store_test.go` | new |
| `internal/v2/runtime/maintenance/digest_component.go` | new |
| `internal/v2/runtime/maintenance/digest_component_test.go` | new |

**Key implementation points**:
- Snapshot store uses immutable replace/load semantics (atomic swap model)
- Maintenance components publish summary state into snapshots
- Turn path reads snapshots only; no synchronous dependency on maintenance latency

**Tests** (5):
1. `TestSnapshotStore_LoadStoreAtomic` — deterministic snapshot semantics
2. `TestSnapshotStore_ConcurrentReaders_NoContentionRegression` — hot-read behavior
3. `TestMaintenanceComponent_PublishesSnapshot` — maintenance output path
4. `TestMaintenanceFailure_DoesNotBlockRunEngine` — non-blocking guarantee
5. `TestSnapshotProjection_Parity` — snapshot/read-model consistency

**Definition of done**: Maintenance workloads are non-blocking, and snapshot reads are stable and race-safe.

---

### PR-09: Turn Intelligence + Context Builder

**Scope**: Persist turn hierarchy with trace lineage and add non-blocking derived-artifact/context retrieval.

| File | Status |
|------|--------|
| `internal/v2/core/run/turn_record.go` | new |
| `internal/v2/core/run/iteration_record.go` | new |
| `internal/v2/core/run/tool_call_record.go` | new |
| `internal/v2/runtime/enrichers/queue.go` | new |
| `internal/v2/runtime/enrichers/worker.go` | new |
| `internal/v2/runtime/contextbuilder/builder.go` | new |
| `internal/v2/runtime/contextbuilder/ref_parser.go` | new |

**Key implementation points**:
- Persist `Turn -> Iteration -> ToolCall` with trace/correlation/causation metadata
- Queue enrichers asynchronously (embedding/annotation/classification) with idempotent keys
- Resolve context references for whole-turn and partial-turn retrieval

**Tests** (6):
1. `TestTurnRecord_PersistsIterationAndToolCallLineage` — hierarchy persistence
2. `TestTraceLineage_ParentSpanRelationships` — trace parent/child structure
3. `TestEnricherQueue_IdempotentByArtifactVersion` — dedupe semantics
4. `TestEnricherFailure_DoesNotBlockTurnCompletion` — non-blocking guarantee
5. `TestContextBuilder_ResolveWholeTurnRef` — `turn/{id}`
6. `TestContextBuilder_ResolveSliceRef` — `turn/{id}#msg:{id}:{start}-{end}`

**Definition of done**: turn lineage is durable, enrichment is asynchronous, and context references are deterministic.

## Testing Strategy

### Unit Tests
- Each PR includes 5-6 focused tests covering happy path, edge cases, and error paths
- All tests use deterministic fakes (clock, uuid, model, tool executor, event store)
- Fakes live in `internal/v2/testkit/fakes/`

### Golden Tests
- JSONL format, one event per line
- Byte-exact comparison against fixtures in `testdata/golden_events/`
- Deterministic via fake clock (fixed timestamps) and fake UUID (sequential)
- Validates event sequence, field completeness, version progression

### Integration Tests (PR-05 to PR-09)
- CLI/API/daemon hit same service methods
- Feature flag routing validated end-to-end
- v1 interop via ID mapping tested
- supervisor lifecycle and maintenance non-blocking guarantees validated
- turn-reference context retrieval and enrichment pipeline validated

## Migration Notes (Strangler Strategy)

### A) Rollback Procedure
1. Remove command from `AGENTCTL_V2_COMMANDS` (scoped rollback) or set empty (full rollback)
2. Restart service boundaries (CLI wrapper, web API, daemon)
3. Verify v2 event producers/consumers are quiescent for that command
4. v2 writes are isolated — v1 path resumes without schema rollback

### B) Parity Verification
1. Start with `ask` as the first shadow-validation command before expanding to `spawn`, `run`, `list`, and `kill`
2. Enable shadow mode with `AGENTCTL_V2_SHADOW_COMMANDS=ask` while keeping primary routing unchanged
3. Run mirrored command fixtures through both paths in shadow mode
4. Compare: envelope contract (`version`, `status`, `meta.ts`, `error.code`, `error.message`), return text ordering, tool-call count/IDs/results, error class and HTTP status, correlation_id, and timing metadata. Enforce error mapping parity: malformed input must map to `ErrValidation`/400; policy denies must map to `ErrPolicyViolation`/403.
5. Alert on divergence in fatal/terminal outcomes; allow drift only in v2-only extensions

### C) v1 Path Deletion Criteria
1. Command-specific diff below tolerance for sustained validation window
2. No production incidents attributable to that command in v2
3. Stable stage and event behavior in v2 telemetry
4. Coverage across daemon/CLI/API routing, service tests, and replay/projection validation
5. Follow-up migration playbook exists for ID mapping and tooling references
6. Control-plane (`Run(ctx)` components) is stable under cancellation and restart
7. Turn intelligence enrichers remain non-blocking and idempotent under retries

### D) Data Migration
- v2 uses independent IDs and authoritative append-only event model; starts from newly created entities
- No bulk import required for MVP
- v1 agents/sessions remain readable from v1; v2 `list`/`kill` support v1-visible IDs through ID mapping adapter
- Optional offline projection seeding deferred — separate maintenance tool, not part of initial rollout

## Implementation Order

1. **PR-01**: V2 Skeleton + Contracts
2. **PR-02**: Event Store + Projections
3. **PR-03**: Runner Pipeline (No Transport)
4. **PR-04**: Unified Tool Stack
5. **PR-05**: Unified Spawn/Ask/Run/List/Kill Services
6. **PR-06**: V2 Ports + Feature-Flagged Strangler Routing
7. **PR-07**: Supervisor + Runtime Event Bus
8. **PR-08**: Snapshots + Non-Blocking Maintenance
9. **PR-09**: Turn Intelligence + Context Builder
10. **PR-10**: Ask Shadow Validation Plumbing

Note: PR-10 is a transitional Wave 1 -> Wave 2 slice focused on shadow parity plumbing for `ask`; detailed rollout/expansion behavior is captured in the Wave 2 sections below.

Each PR is independently testable. PRs 1-5 have no v1 business-logic changes. PR-06 wires routing with opt-in flag and minimal touch points in existing command dispatch files. PRs 7-8 add Go-native control-plane capabilities without coupling them to the turn execution critical path. PR-09 adds turn lineage and derived context intelligence without introducing a second execution path. PR-10 establishes shadow validation plumbing for parity verification before Wave 2 cutover.

## Wave 2: Productionization + Dynamic Context (V2-Only)

Wave 1 established the v2 runtime skeleton and contracts. Wave 2 focuses on
real cutover, persistent turn intelligence, and dynamic context assembly that
reduces dependence on a single large prompt window.

### Wave 2 Goals

1. Route live command paths through v2 services/ports (without breaking v1 fallback).
2. Persist turn/iteration/tool-call intelligence in a libsql-first model, including
   retrieval-friendly artifact metadata.
3. Feed an asynchronous event pipeline for enrichment, annotation, and context materialization.
4. Build layered, budgeted context assembly (L2 -> L1 -> L0) with temporal drill-down
   (`hours -> days -> weeks -> months`) and stable references.
5. Define hard decommission gates for v1 command paths.

### Areas That Need Consideration and Fleshing Out

1. **Live routing integration**
   - Ensure CLI/API/daemon entrypoints actually dispatch to `internal/v2/ports/*`
     when `AGENTCTL_V2_COMMANDS` is enabled.
   - Keep rollback one-step (`unset`/remove command from flag) with no schema rollback.
2. **Turn persistence and retrieval**
   - Add concrete `TurnRecorder` and `TurnReader` adapters backed by libsql.
   - Persist iteration/tool-call detail and reference slices to support partial recall.
3. **Artifact schema and vector-ready storage**
   - Use libsql-first artifact tables for embeddings/annotations/classifications/learnings.
   - Prefer libsql vector search/index capabilities as the primary retrieval backend in v2.
   - Keep versioned idempotency keys `(turn_id, artifact_type, artifact_version)`.
4. **Dynamic event pipeline**
   - Wire runner emits (`turn.recorded`) to enricher producers via bounded queues.
   - Enforce non-blocking semantics; degraded/missing artifacts must not fail turn completion.
5. **Context builder evolution**
   - Expose deterministic reference resolution for whole turn, iteration, tool-call, and slices.
   - Support hierarchical retrieval with drill-down metadata (`expandable_dates`) and
     temporal pyramids aligned with:
     - `docs/general/companion-memory.md`
     - `docs/designs/hierarchical-memory-retrieval.md`
     - `docs/designs/progressive-memory-system.md`
6. **Parity and observability**
   - Expand shadow parity beyond `ask` to `spawn`, `run`, `list`, `kill`.
   - Track divergence, queue lag, drop counts, and context quality metrics by command.
7. **v1 retirement path**
   - Define command-by-command deletion criteria and cleanup order once parity windows pass.

### Wave 2 PR Slices (Proposed)

1. **PR-11: Command Surface Cutover**
   - Wire real CLI/API/daemon handlers to v2 routers for enabled commands.
   - DoD: v2 routing is exercised in live command tests; disabled commands still use v1.
2. **PR-12: Libsql Turn + Artifact Stores**
   - Implement production `TurnRecorder`/`TurnReader` and artifact persistence.
   - DoD: turn lineage and artifact metadata are queryable by stable refs.
3. **PR-13: Enrichment Pipeline Wiring**
   - Connect runtime event bus to enricher queue/worker producers.
   - DoD: `turn.recorded` triggers async jobs; failures emit events without blocking turn success.
4. **PR-14: Hierarchical Context Builder**
   - Add temporal pyramid retrieval and drill-down APIs (`hours/days/weeks/months`).
   - DoD: context assembly can return coarse summaries with explicit drill targets.
5. **PR-15: Companion Memory Integration**
   - Feed context artifacts into layered companion memory assembly (L2 -> L1 -> L0 budgets).
   - DoD: context builder can mix recent turn refs + companion summaries deterministically.
6. **PR-16: v1 Decommission Gates**
   - Enforce parity windows, remove duplicate v1 paths command-by-command.
   - DoD: command is v2-primary with documented rollback and no orphan transport logic.

### PR-16 Readiness Gates (Operational Policy)

1. **Shadow parity coverage**
   - `AGENTCTL_V2_SHADOW_COMMANDS` supports `spawn,run,list,kill` in addition to `ask`.
   - Mutating command shadows (`spawn`,`run`,`kill`) are blocked by default and require
     explicit opt-in with `AGENTCTL_V2_SHADOW_MUTATING=true`.
   - Non-mutating shadows (`ask`,`list`) can run by default.
2. **Sustained parity window**
   - Require a rolling 7-day window per command before promoting to v2-primary.
   - Require at least 200 shadow samples per command in that window.
   - Require `match_rate >= 99.0%` and no severity-1 incidents attributable to routing divergence.
3. **Incident-free promotion checks**
   - No unresolved `shadow_error` spikes for the command during the parity window.
   - No queue/backpressure regressions in non-blocking paths attributable to shadow runs.
4. **Command-by-command v1 freeze/removal order**
   - Stage A: Freeze v1 path for the command (compatibility boundary only; no new behavior work).
   - Stage B: Switch command default to v2 (flagless v2 primary, rollback flag retained).
   - Stage C: Remove v1 command path after one additional incident-free window.
   - Recommended order: `list` -> `ask` -> `run` -> `spawn` -> `kill`.

### Full v2 Exit Criteria (From v1)

1. `spawn`, `ask`, `run`, `list`, and `kill` are v2-primary in CLI, API, and daemon ports.
2. Turn lineage plus derived artifacts are persisted and retrievable through stable references.
3. Enrichment/event/context pipelines are non-blocking under load with bounded backpressure.
4. Shadow parity reports are stable across all migrated commands for a sustained window.
5. v1 command handlers are either removed or explicitly frozen behind compatibility boundaries.

## Wave 3: Retrieval + Dynamic Context Intelligence (PR-17+)

Wave 2 established persistence and non-blocking enrichment pipelines. Wave 3
focuses on retrieval surfaces that let context assembly use turn artifacts
directly instead of relying only on chronological windows.

### Wave 3 Goals

1. Add a production retrieval surface for artifact embeddings in v2 turns storage.
2. Keep retrieval deterministic and bounded across both libsql vector and SQLite fallback paths.
3. Add runtime-facing interfaces so context assembly can blend:
   - temporal lineage (`hours/days/weeks/months`)
   - artifact-semantic matches
   - companion-memory layers (L2 -> L1 -> L0)
4. Add observability for retrieval path quality (vector hit path vs fallback path).

### PR-17: Libsql-First Artifact Semantic Retrieval

#### Scope

1. Add `SearchArtifactsByEmbedding` on the v2 libsql turns adapter.
2. Use vector-first SQL retrieval (`vector_distance_cos`) when supported.
3. Fall back to deterministic in-process cosine scoring when vector SQL is unavailable.
4. Support retrieval filters:
   - `session_id`
   - `artifact_type` (`embedding`, `annotation`, `classification`, `learning`)
5. Cap retrieval result limits to prevent unbounded scans.

#### Acceptance Criteria

1. Retrieval returns stable ordering (similarity first, deterministic tie-breakers).
2. Fallback mode behavior is deterministic and tested.
3. Invalid artifact filters fail fast with explicit typed errors.
4. Existing turn completion non-blocking guarantees remain unchanged.
5. Context-builder contract includes deterministic merge metadata:
   - `artifact_search_path` (`vector|fallback|disabled|error`)
   - `artifact_hit_count`
   - dedup by `ref`
   - merge ordering: temporal blocks first, then semantic refs by similarity desc + ref asc

#### Interface Proposal (Documented Contract)

1. Define a core-facing retrieval port in `internal/v2/core/run`:
   - `ArtifactSemanticRetriever`
   - `ArtifactSearchOptions`
   - `ScoredArtifact`
2. Keep libsql adapter ownership of concrete SQL/search behavior while exposing a
   transport-agnostic interface to runtime/context components.
3. Extend context-builder request/response contracts to support optional semantic
   artifact queries and explicit result metadata (`artifact_search_path`,
   `artifact_hit_count`).
4. Keep semantic retrieval optional and non-fatal so baseline temporal assembly
   remains available when retrieval is degraded/unavailable.

Reference: `docs/spec/v2_greenfield_bootstrap.md` ("Artifact Semantic Retrieval Contract (Wave 3)" and "Context Builder Integration Contract (PR-17 Proposal)").

#### Known Risk (Current)

- CI currently validates fallback retrieval behavior, but does not yet run an
  end-to-end native libsql vector-query execution path. This is tracked in
  `docs/plans/v2-implementation-todo.md` as an open question.

### PR-18 Focus: Vector Path Confidence + Retrieval Quality

PR-17 completed the core retrieval interface, context-builder integration, and
path observability/tracing. PR-18 should focus on confidence and quality gates
for production use of vector-backed retrieval.

1. Add deterministic coverage for native libsql vector path execution
   (`vector_distance_cos`) in a CI-friendly test setup.
2. Expose explicit runtime capability status for retrieval path selection
   (vector-enabled vs fallback-only) in observability output.
3. Expand retrieval quality telemetry with path-scoped latency and hit-rate
   buckets so degraded behavior is measurable.
4. Codify fallback guardrails and rollout criteria:
   - fallback is acceptable for continuity, but must be marked degraded when
     vector capability is expected
   - fallback-only runs must not break temporal/context assembly correctness
