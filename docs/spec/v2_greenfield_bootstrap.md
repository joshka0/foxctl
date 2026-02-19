# Agentctl V2 Greenfield Bootstrap Plan

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-02-18

## Purpose

Define a practical bootstrap sequence for a clean v2 runtime with:

- One execution loop
- One tool system
- One spawn/ask orchestration path
- Thin transports (CLI/Web/Daemon)
- Deterministic, test-first behavior
- Go-native non-blocking control-plane components

## Version Boundary

This spec is `v2`-only and maps to `internal/v2/*`.
v1 behavior remains unchanged unless a command is explicitly routed through
`AGENTCTL_V2_COMMANDS`.

## Related Docs

- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/plans/v2-greenfield-bootstrap.md`
- `docs/general/runtime-orchestration.md`
- `docs/general/memory.md`
- `docs/general/companion-memory.md`
- `docs/general/context-and-observability.md`
- `docs/architecture/system-architecture.md`
- `docs/designs/hierarchical-memory-retrieval.md`
- `docs/designs/progressive-memory-system.md`

## V2 Design Principles

1. One way to do each core operation.
2. Domain logic in services/core, never in handlers.
3. Append-first persistence plus projections for reads.
4. Tool contracts are typed and policy-aware.
5. Runtime state changes emit typed events.
6. Safety uses feature-flag routing, idempotency, and append-only persistence (no command-level `--dry-run` requirement).
7. Turn execution stays synchronous; maintenance runs in background components.
8. Concurrency is explicit: bounded channels, context-driven shutdown, immutable snapshots for hot reads.

## Scope (MVP)

In scope:

- `spawn`, `run`, `ask`, `list`, `kill`
- mailbox + filesystem/code tools
- hierarchy depth limits
- event stream + SQLite projections

Out of scope for MVP:

- presence generation
- advanced hybrid memory extraction pipeline
- optimization/reflection automation
- multi-provider strategy matrix beyond one primary provider

## Target Package Layout

```text
internal/v2/
  core/
    agent/
    run/
    spawn/
    events/
    policy/
  runtime/
    runner/
    events/
    snapshots/
    supervisor/
    profiles/
    tools/
    hooks/
  services/
    spawn_service.go
    ask_service.go
    run_service.go
  adapters/
    sqlite/
    mailbox/
    llm/
    telemetry/
  ports/
    cli/
    api/
    daemon/
```

Dependency direction:

```text
ports -> services -> core
services -> adapters (through interfaces)
runtime -> core
runtime -> adapters (through interfaces)
core imports no adapters
```

## Core Interfaces (V2)

```go
type RunEngine interface {
    RunTurn(ctx context.Context, in TurnInput) (TurnOutput, error)
}

type ToolCatalog interface {
    ForProfile(profile ProcessProfile) []ToolDef
}

type ToolExecutor interface {
    Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

type SpawnService interface {
    Spawn(ctx context.Context, intent SpawnIntent) (SpawnDecision, error)
}

type AskService interface {
    Ask(ctx context.Context, req AskRequest) (AskResult, error)
}

type EventBus interface {
    Publish(ctx context.Context, evt Event) error
}

type Component interface {
    Name() string
    Run(ctx context.Context) error
}

type SnapshotStore interface {
    Load() RuntimeSnapshot
    Store(s RuntimeSnapshot)
}

type TurnArtifactEnricher interface {
    Enrich(ctx context.Context, turn TurnRecord) ([]TurnArtifact, error)
}

type ArtifactSearchOptions struct {
    SessionID     string
    ArtifactTypes []string
    Limit         int
    MinSimilarity float64
}

type ScoredArtifact struct {
    Ref             string
    TurnID          string
    ArtifactType    string
    ArtifactVersion string
    Similarity      float64
    Distance        float64
    Summary         string
    MetadataJSON    json.RawMessage
}

type ArtifactSearchResult struct {
    Hits             []ScoredArtifact
    SearchPath       string // vector | fallback | disabled | error
    VectorCapability string // enabled | disabled | unknown
}

type ArtifactSemanticRetriever interface {
    SearchArtifactsByEmbedding(ctx context.Context, queryEmbedding []float32, opts ArtifactSearchOptions) (ArtifactSearchResult, error)
}

type ContextBuilder interface {
    Build(ctx context.Context, req ContextRequest) (ContextBundle, error)
}
```

## Go-Native Runtime Model

Use a split runtime model:

1. Turn path (`services -> runner -> event append`) is direct and request-scoped.
2. Control plane (`runtime/supervisor`) runs long-lived components via `Run(ctx)` and `errgroup`.
3. Async boundaries (event fan-out, maintenance queues) are bounded channels with explicit overflow policy.
4. High-read shared state is exposed as immutable snapshots (atomic swap), not lock-heavy mutable structs.
5. Mutable runtime state has single owner goroutines where practical.

### Non-Blocking Guarantees (V2)

1. Turn completion is request-scoped and ends at `PersistTurn` + `EmitEvents`.
2. Enrichment/digest/indexing runs in supervisor-managed background components.
3. Backpressure is handled with bounded/drop-or-defer queue policies and events, never by stalling turn execution.
4. Context assembly reads persisted artifacts/snapshots and must not wait for in-flight enrichers.

## Turn Intelligence Model

In v2, turns are immutable source-of-truth records. Enrichment (embedding/classification/annotation/summarization) is derived asynchronously.

Hierarchy:

1. `Session`
2. `Turn` (one user request lifecycle)
3. `Iteration` (one model cycle inside the turn)
4. `ToolCall` (zero or more calls inside an iteration)

Minimal shape:

```go
type TurnRecord struct {
    ID            string
    SessionID     string
    TurnIndex     int
    TraceID       string
    RootSpanID    string
    CorrelationID string
    CausationID   string
    Iterations    []IterationRecord
    FinalOutput   MessageRef
}

type IterationRecord struct {
    TurnID         string
    IterationIndex int
    TraceID        string
    SpanID         string
    ParentSpanID   string
    ToolCalls      []ToolCallRecord
}

type ToolCallRecord struct {
    CallID        string
    IterationIndex int
    TraceID       string
    SpanID        string
    ParentSpanID  string
    Name          string
    ArgsJSON      json.RawMessage
    ResultRef     ArtifactRef
}
```

Reference forms for context retrieval:

1. Whole turn: `turn/{turn_id}`
2. Iteration scope: `turn/{turn_id}/iter/{index}`
3. Tool call scope: `turn/{turn_id}/iter/{index}/tool/{call_id}`
4. Message/span slice: `turn/{turn_id}#msg:{msg_id}:{start}-{end}`

Rules:

1. Turn writes must not block on enrichment.
2. Enrichment jobs are idempotent per `(turn_id, artifact_type, artifact_version)`.
3. Async enrichers link lineage through `correlation_id`/`causation_id`.
4. These rules apply only to commands currently routed to v2.

### Artifact Semantic Retrieval Contract (Wave 3)

Derived artifact retrieval should support semantic lookup over persisted
embeddings with deterministic behavior across driver modes.

Required properties:

1. Retrieval is libsql-vector first when vector SQL is available.
2. Retrieval falls back to in-process cosine ranking when vector SQL is unavailable.
3. Filter dimensions include:
   - `session_id` (optional)
   - `artifact_type` (optional; one of embedding/annotation/classification/learning)
4. Ordering is deterministic and bounded by explicit limits.
5. Retrieval failure/degradation must not affect turn completion guarantees.

Embedding storage and index policy:

1. Artifacts persist embeddings in two forms:
   - `embedding` (`F32_BLOB(N)`) for native libsql vector SQL
   - `embedding_json` (`TEXT`) for deterministic fallback cosine ranking and portability
2. Vector dimension policy:
   - canonical dimension `N` is configured by `AGENTCTL_VECTOR_DIMS` (and per-store
     overrides where available)
   - when vector mode is active, writes/queries with mismatched embedding dimensions
     fail fast with typed errors
3. Vector index policy:
   - maintain best-effort libsql index `idx_v2_turn_artifacts_embedding_vec` via
     `libsql_vector_idx(embedding)`
   - vector retrieval uses `vector_top_k(...)` candidate selection plus deterministic
     rerank (`vector_distance_cos`, then stable tie-breaks)
   - if vector SQL/index primitives are unavailable, retrieval downgrades to fallback
     cosine and must surface degraded path/capability metadata

Fallback guardrails (required for rollout readiness):

1. Fallback execution is allowed for continuity, but must be explicitly surfaced
   as degraded when vector capability is expected.
2. Context assembly invariants must hold in fallback mode:
   - deterministic ordering
   - stable references
   - required temporal blocks present
3. Runtime telemetry must expose retrieval path quality to support promotion
   decisions:
   - path (`vector|fallback|disabled|error`)
   - vector capability (`enabled|disabled|unknown`)
   - hit-count bucket (`zero|one_to_three|four_to_ten|gt_ten`)
   - latency bucket (`le_10ms|le_50ms|le_100ms|gt_100ms`)

### Context Builder Integration Contract (PR-17 Proposal)

Context assembly should treat artifact-semantic retrieval as an optional layer
that enriches, but never blocks, baseline temporal/context retrieval.

Proposed request/response extensions:

```go
type ArtifactSemanticQuery struct {
    QueryEmbedding []float32
    SessionID      string
    ArtifactTypes  []string
    Limit          int
    MinSimilarity  float64
    // Maps directly to ArtifactSearchOptions in the semantic retriever.
}

type LayerBudget struct {
    TotalChars    int // overall cap for rendered layered content
    L2Chars       int // optional override
    L1Chars       int // optional override
    L0Chars       int // optional override
    SemanticChars int // optional override
}

type ContextRequest struct {
    Ref       string
    Temporal  *TemporalRequest
    Semantic  *ArtifactSemanticQuery
    Budget    *LayerBudget
    // Semantic retrieval and layer rendering must honor resolved budget limits.
}

type ContextBundle struct {
    Ref          string
    Content      string
    ArtifactRefs []string
    Meta         map[string]any // includes artifact_search_path, artifact_vector_capability, artifact_hit_count
}
```

Integration rules:

1. If `Semantic == nil`, context builder behavior is unchanged.
2. If retriever is unavailable/fails, return context without semantic matches and
   set non-fatal metadata (`artifact_search_path=disabled|error`).
3. Semantic matches are filtered by `MinSimilarity`, deduplicated by `ref`, and
   bounded by explicit limits/budgets.
4. Deterministic merge order:
   - temporal/coarse context blocks first
   - semantic artifact refs sorted by similarity desc, then ref asc
5. Context builder must query persisted artifact state only and must not wait for
   in-flight enricher jobs.
6. Default layered budget policy uses one total cap with deterministic split:
   - `L2=20%`, `L1=25%`, `L0=45%`, `Semantic=10%`
   - if semantic retrieval is not requested, semantic budget is reallocated to `L0`
   - explicit per-layer overrides are allowed per request via `LayerBudget`

## Canonical Runtime Pipeline

Each turn runs the same staged flow:

1. `InitContext`
2. `ResolveDependencies`
3. `ApplyPreHooks`
4. `BuildToolset`
5. `ModelCall`
6. `ApplyPostHooks`
7. `PersistTurn`
8. `EmitEvents`

The loop is deterministic by default and testable with fake clock/uuid/model/tool adapters.

Canonical happy-path event order for golden tests:

`run.started` -> `tool.invoked` -> `tool.responded` -> `turn.recorded` -> `run.completed`

This order must stay stable unless the spec is updated and parity fixtures are regenerated.

## Error + Envelope Contract

Error mapping is deterministic:

- `ErrValidation` -> HTTP 400
- `ErrPolicyViolation` -> HTTP 403
- `ErrNotFound` -> HTTP 404
- `ErrTimeout` -> HTTP 408
- `ErrToolFailed` -> HTTP 502
- `ErrInternal` / `ErrDependency` / fatal `ErrStageFailed` -> HTTP 500
- non-fatal `ErrStageFailed` -> degraded turn output with recorded stage failure

V2 command responses must preserve v1 envelope compatibility for shared surfaces:

- `version`
- `status`
- `meta.ts`
- `error.code`
- `error.message`

## Process Profiles (Tool Exposure)

MVP profiles:

1. `overseer`
2. `worker`
3. `companion`

Each profile gets an explicit tool allowlist from `ToolCatalog`. No inline per-handler exceptions.

## Bootstrap PR Sequence

### PR-01: V2 Skeleton + Contracts

Deliverables:

- `internal/v2` package tree with compile-safe stubs
- core structs/interfaces for run/spawn/events
- `docs/spec/v2_repo_rules_and_skills.md` referenced from spec index

Definition of done:

- `go test ./...` passes
- zero behavior change for v1 paths
- envelope compatibility tests cover `version`, `status`, `meta.ts`, and `error` shape

### PR-02: Event Store + Projections

Deliverables:

- append-only event table for v2
- minimal projection tables (`agent_state`, `run_state`)
- adapter interfaces + sqlite implementation

Definition of done:

- event append tests
- projection replay tests

### PR-03: Runner Pipeline (No Transport)

Deliverables:

- staged runner implementation with fake model/tool executor
- profile-based tool exposure
- cancellation/timeouts + max-iteration enforcement

Definition of done:

- pipeline unit tests by stage
- golden tests for event emission order

### PR-04: Unified Tool Stack

Deliverables:

- v2 tool contract (`ToolDef`, `ToolPolicy`, `ToolResult`)
- single executor path via catalog + registry
- remove duplicated runtime-local tool switching from v2 path

Definition of done:

- all v2 tools executed through one path
- schema/validation tests per tool

### PR-05: Unified Spawn/Ask Services

Deliverables:

- `SpawnService` and `AskService` implementations
- depth/policy validation centralized
- mailbox request/response mapping centralized

Definition of done:

- CLI/API/daemon integration tests hit same service methods
- no duplicate spawn logic in v2 ports

### PR-06: V2 Ports + Feature Flag

Deliverables:

- `ports/cli`, `ports/api`, `ports/daemon` adapters for v2 services
- per-command feature flag (`AGENTCTL_V2_COMMANDS=spawn,ask,run,list,kill`) for routing
- migration shim that keeps v1 default

Definition of done:

- side-by-side run capability (v1 default, v2 opt-in)
- smoke tests for spawn/list/ask/kill
- envelope parity tests verify `version`, `status`, `meta.ts`, and `error` compatibility across v1/v2 routes

### PR-07: Supervisor + Runtime Event Bus

Deliverables:

- `internal/v2/runtime/supervisor` host with `Component` lifecycle (`Run(ctx)`)
- bounded `runtime/events` bus with explicit overflow strategy
- startup/shutdown wiring so components are context-cancelable and observable

Definition of done:

- start/stop tests show clean cancellation and component teardown
- bounded queue behavior is tested (drop/backpressure policy is explicit)
- no turn-path regression from supervisor startup

### PR-08: Snapshots + Non-Blocking Maintenance

Deliverables:

- `runtime/snapshots` immutable state cache for hot reads (status/digest/read models)
- first maintenance component (for example: digest/health projector) under supervisor
- turn execution fully decoupled from maintenance job latency

Definition of done:

- slow/failing maintenance component cannot block turn execution
- snapshot read/write semantics are deterministic and race-safe
- projection/snapshot parity tests pass

### PR-09: Turn Intelligence + Context Builder

Deliverables:

- turn/iteration/tool-call record model with trace lineage fields
- asynchronous enrichers for embeddings/annotations/classification (extensible pipeline)
- context builder that resolves whole-turn and partial-turn references

Definition of done:

- turn persistence includes iteration and tool-call lineage
- enrichment is idempotent and non-blocking to turn execution
- context builder resolves `turn/*` references deterministically

## Migration Strategy (Strangler Pattern)

1. Keep v1 as the default behavior.
2. Route specific commands to v2 behind explicit feature flag.
3. Migrate one command surface at a time (`spawn` -> `ask` -> `run`).
4. Keep v1 business logic untouched while introducing v2 routing/wiring changes.
5. Remove v1 duplicated paths only after parity tests are stable.
6. Keep control-plane components optional and composable during rollout.

Rollback model:

- remove command(s) from `AGENTCTL_V2_COMMANDS`
- restart relevant boundary process (CLI/API/daemon)
- confirm v2 event writers are quiescent for that command

## Acceptance Criteria for MVP

1. All v2 core commands use one orchestration path.
2. All v2 tools use one registry/executor path.
3. No transport layer opens stores and performs domain orchestration directly.
4. Event stream fully reconstructs current run/agent state via projection replay.
5. Test suite covers happy path plus cancellation, timeout, and retry boundaries.
6. Envelope contract parity is maintained for shared v1/v2 command surfaces.
7. Error mapping parity is enforced: malformed input maps to validation/400 and policy denies map to policy-violation/403.
8. Control-plane components use `Run(ctx)` lifecycle and cleanly stop on cancellation.
9. Bounded channels and immutable snapshots are used for background workloads.
10. Turn model persists iteration/tool-call lineage with trace metadata.
11. Context builder supports whole-turn and partial-turn references.

## First Scaffolding Work Items

1. Create `internal/v2/core/events/types.go` with typed event enums + payloads.
2. Create `internal/v2/services/spawn_service.go` with pure validation + intent mapping.
3. Create `internal/v2/runtime/runner/pipeline.go` with stage interfaces and no-op adapters.
4. Add `internal/v2/adapters/sqlite` test fixtures and replay tests.
5. Create `internal/v2/runtime/supervisor/host.go` with component registry and `errgroup` wiring.
6. Create `internal/v2/runtime/snapshots/store.go` with atomic snapshot swap/load.
7. Create `internal/v2/core/run/turn_record.go` with turn/iteration/tool-call types.
8. Create `internal/v2/runtime/contextbuilder/builder.go` with `turn/*` reference resolution.
