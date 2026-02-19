# V2 Implementation Todo Tracker

Status: Active  
Owner: Solo maintainer  
Last Updated: 2026-02-19

## Objective

Implement the v2 runtime incrementally with:

- one orchestration path per command
- one tool execution path
- append-only events + projections
- non-blocking enrichment/context assembly
- strict v1/v2 boundary via `AGENTCTL_V2_COMMANDS`
- shadow validation via `AGENTCTL_V2_SHADOW_COMMANDS`

Primary specs/plans:

- `docs/spec/v2_greenfield_bootstrap.md`
- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/plans/v2-greenfield-bootstrap.md`

## Current State

- [x] Wave 1 foundation complete (PR-01 through PR-10) with tests and review notes
- [x] v2 docs aligned to companion/general references and non-blocking v2 scope rules
- [x] Wave 2 production cutover batch 1 complete (PR-11 through PR-13)
- [x] Wave 2 dynamic context + decommission gate batch complete (PR-14 through PR-16)
- [x] Wave 3 kickoff complete for PR-17 (artifact retrieval + semantic observability/tracing)

## Wave 1 Completed (Reference)

- [x] PR-01 to PR-06: skeleton/contracts through feature-flag routing
- [x] PR-07 to PR-09: supervisor/events/snapshots + turn intelligence foundation
- [x] PR-10: `ask` shadow validation plumbing and parity telemetry

## Now (Wave 3 Active)

Wave 2 rationale, DoD expectations, and exit criteria live in:
`docs/plans/v2-greenfield-bootstrap.md` ("Wave 2: Productionization + Dynamic Context (V2-Only)").

Wave 3 retrieval goals and PR-17+ scope live in:
`docs/plans/v2-greenfield-bootstrap.md` ("Wave 3: Retrieval + Dynamic Context Intelligence (PR-17+)").

- [x] PR-11: Live command-surface cutover (v2 routing in real CLI/API/daemon handlers)
  - [x] wire daemon `agent.spawn`/`agent.list`/`agent.kill` request handling through `internal/v2/ports/daemon` with env-flag routing and safe fallback
  - [x] wire CLI `spawn/run/list/kill` entrypoints through `internal/v2/ports/cli` in real command handlers
  - [x] wire API agent spawn/daemon action handlers through `internal/v2/ports/api` in real HTTP handlers
  - [x] keep v1 fallback behavior unchanged when flags are unset
  - [x] add integration tests proving real handler routing (not router-only unit tests)
- [x] PR-12: Libsql turn/artifact stores (production `TurnRecorder` + `TurnReader`)
  - [x] persist `Turn -> Iteration -> ToolCall` lineage in libsql-backed stores
  - [x] add artifact tables for embeddings/annotations/classifications/learnings
  - [x] keep idempotency key contract `(turn_id, artifact_type, artifact_version)`
- [x] PR-13: Event-to-enricher wiring
  - [x] emit `turn.recorded` from runtime pipeline into enricher producer
  - [x] consume via bounded queue/worker with non-blocking guarantees
  - [x] emit failure/retry telemetry events without failing completed turns
- [x] PR-14: Hierarchical context builder + temporal pyramid retrieval
  - [x] support `hours -> days -> weeks -> months` summaries with drill-down refs
  - [x] expose expandable-date style metadata for selective deepening
- [x] PR-15: Companion memory layered assembly integration
  - [x] wire L2 -> L1 -> L0 budgeted context composition
  - [x] blend turn refs + companion summaries deterministically
  - [x] validate referenceability of whole turns and partial slices in assembled context

## Next (Wave 3 Active)

- [x] PR-17: libsql-first artifact semantic retrieval surfaces
  - [x] add `SearchArtifactsByEmbedding` in `internal/v2/adapters/libsql/turns/store.go` with vector-first query + safe cosine fallback
  - [x] add deterministic retrieval tests (ranking, filtering, fallback-disable behavior)
  - [x] document core-facing retrieval interface + context builder integration contract (`docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-greenfield-bootstrap.md`)
  - [x] define core-facing artifact semantic retrieval interface for runtime consumers
  - [x] wire optional semantic artifact layer into context builder assembly path
  - [x] enforce context-builder metadata and merge guarantees (`artifact_search_path`, `artifact_hit_count`, dedup-by-ref, deterministic merge ordering)
  - [x] add observability counters for vector-path vs fallback-path query usage
  - [x] add optional wide-event emission for `context.semantic_artifact_search`
  - [x] propagate tracing context to semantic events (`trace_id` + `parent_id` when span context exists)

- [x] PR-16: v1 decommission readiness gates
  - [x] expand shadow parity past `ask` to `spawn/run/list/kill`
  - [x] define sustained parity window + incident-free thresholds
  - [x] remove/freeze superseded v1 handlers command-by-command

- [ ] PR-18: libsql vector-path validation + retrieval quality hardening
  - [ ] add deterministic integration tests that exercise native libsql vector SQL path (`vector_distance_cos`) in CI-friendly setup
  - [ ] add explicit runtime capability signal (vector enabled/disabled) to retrieval observability output and docs
  - [ ] add latency + hit-rate telemetry buckets for semantic artifact search path quality
  - [ ] document fallback guardrails (when fallback is expected vs treated as degraded) in `docs/spec/v2_greenfield_bootstrap.md`
  - [ ] define rollout gate: fallback-only behavior must not regress baseline context assembly correctness

## Decisions (Locked)

- [x] v2 remains opt-in per command through `AGENTCTL_V2_COMMANDS`
- [x] no command-level `--dry-run` requirement for v2 rollout
- [x] turn completion must never block on enrichers/maintenance
- [x] turn lineage is `Turn -> Iteration -> ToolCall` with trace metadata
- [x] context retrieval uses stable refs (`turn/*`, slice refs)
- [x] first shadow-validation command is `ask` (lowest rollout risk, high parity signal)

## Open Questions

- [ ] libsql embedding storage format and vector indexing policy for artifact tables
- [ ] context budget policy across L2/L1/L0 (global vs per-command vs per-role)
- [ ] whether and how to backfill selected v1 turns into v2 retrieval surfaces
- [ ] native libsql/Turso vector-path CI coverage for `SearchArtifactsByEmbedding` (current tests cover deterministic fallback path only)

## Completion Gate (Required Before Marking Done)

Before checking off any PR slice, run a subagent review and add a short note.

Required note fields:

1. `reviewer`: subagent id/handle
2. `scope`: files or PR slice reviewed
3. `findings`: summary or `none`
4. `decision`: `approved` | `approved-with-known-risks`

Template:

```text
Subagent Review
- reviewer: <id>
- scope: <files/slice>
- findings: <summary|none>
- decision: <approved|approved-with-known-risks>
```

## Resume Protocol (After Context Loss)

1. Open this tracker and continue from the first unchecked item under `Now`.
2. Open `docs/plans/v2-greenfield-bootstrap.md` for acceptance tests and file list.
3. Keep changes scoped to one PR slice only.
4. Before stopping, update:
   - `Last Updated` date
   - checkboxes
   - subagent review note for the completed slice
   - short entry in `Progress Log`

## Progress Log

- 2026-02-19:
  Subagent Review
  - reviewer: `019c750b-7ecd-7980-b862-6ac80123efdb`
  - scope: `PR-17 tracing propagation slice` (`internal/observability/{span.go,span_test.go}`, `internal/v2/runtime/contextbuilder/{layered.go,layered_observability_test.go}`, `docs/observability/wide-events.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Completed tracing propagation for semantic retrieval wide events.
  - `StartSpan` now attaches generated `span_id` to context in `internal/observability/span.go`.
  - Semantic retrieval events now inherit `trace_id` from context and set `parent_id` from current span when present.
  - Added regression assertions for:
    - context `span_id` propagation in `internal/observability/span_test.go`
    - `trace_id` and `parent_id` linkage in `internal/v2/runtime/contextbuilder/layered_observability_test.go`
- 2026-02-19:
  Subagent Review
  - reviewer: `019c7501-d05a-7352-902e-ca988f39b71e`
  - scope: `PR-17 wide-event emission slice` (`internal/v2/runtime/contextbuilder/{layered.go,layered_observability_test.go}`, `internal/observability/wide_event.go`, `docs/observability/wide-events.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none` (vector/error/disabled path coverage confirmed)
  - decision: `approved`
- 2026-02-19: Implemented PR-17 retrieval counters + semantic wide-event emission.
  - Added atomic semantic retrieval counters to `internal/v2/runtime/contextbuilder/Builder` with snapshot API (`ArtifactStats`):
    - calls: total/vector/fallback/disabled/error
    - hits: total/vector/fallback
  - Wired counter increments in semantic retrieval resolution path in `internal/v2/runtime/contextbuilder/layered.go`.
  - Added optional wide-event emission in `internal/v2/runtime/contextbuilder/layered.go`:
    - operation: `context.semantic_artifact_search`
    - component: `contextbuilder`
    - data: `search_path`, `hit_count`, `session_id`, `query_dims`, and optional filters
    - error path emits `status=error` while context assembly remains non-blocking
  - Added regression coverage in `internal/v2/runtime/contextbuilder/layered_observability_test.go`.
  - Added test assertions for counter behavior in `internal/v2/runtime/contextbuilder/layered_test.go`.
  - Updated `docs/observability/wide-events.md` to mark semantic retrieval wide-event mapping as implemented.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c74e7-34d1-7741-ad85-4c6ebfdeffca`
  - scope: `PR-17 implementation slice` (`internal/v2/core/run/artifact_search.go`, `internal/v2/adapters/libsql/turns/{store,store_test}.go`, `internal/v2/runtime/contextbuilder/{builder,layered,layered_test}.go`, `docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Implemented PR-17 interface and context-builder semantic merge contract.
  - Added new core run retrieval contract in `internal/v2/core/run/artifact_search.go`:
    - `ArtifactSearchOptions`
    - `ScoredArtifact`
    - `ArtifactSearchResult`
    - `ArtifactSemanticRetriever`
  - Updated libsql turns adapter to implement the core retriever:
    - `SearchArtifactsByEmbedding(...) (run.ArtifactSearchResult, error)`
    - supports `ArtifactTypes[]` and `MinSimilarity`
    - returns explicit search path metadata (`vector`/`fallback`/`disabled`)
  - Wired optional semantic retrieval into `internal/v2/runtime/contextbuilder/layered.go`:
    - `LayeredRequest.Semantic`
    - `Builder.SetArtifactRetriever(...)`
    - non-fatal degraded behavior (`artifact_search_path=error`, turn assembly continues)
    - deterministic semantic merge and dedup-by-ref
  - Added tests in:
    - `internal/v2/adapters/libsql/turns/store_test.go`
    - `internal/v2/runtime/contextbuilder/layered_test.go`
  - Validation:
    - `go test ./internal/v2/adapters/libsql/turns`
    - `go test ./internal/v2/runtime/contextbuilder`
    - `go test ./internal/v2/...`
- 2026-02-19: Documented concrete PR-17 interface proposal (core/run + context builder).
  - Added proposed core retrieval interfaces (`ArtifactSemanticRetriever`, `ArtifactSearchOptions`, `ScoredArtifact`) in `docs/spec/v2_greenfield_bootstrap.md`.
  - Added context-builder integration contract with optional semantic query shape, deterministic merge order, and degraded-mode behavior.
  - Added Wave 3 plan-level interface proposal section and cross-reference in `docs/plans/v2-greenfield-bootstrap.md`.
- 2026-02-19: Refined PR-17 interface docs after subagent review.
  - Aligned retrieval option contract to include `ArtifactTypes[]` and `MinSimilarity` in `docs/spec/v2_greenfield_bootstrap.md`.
  - Added explicit PR-17 acceptance criteria for context-builder metadata + deterministic semantic merge behavior in `docs/plans/v2-greenfield-bootstrap.md`.
  - Added explicit tracker work item for metadata/merge guarantees in this file.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c74da-2c68-7b83-8639-4d6631991652`
  - scope: `PR-17 interface proposal docs` (`docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `resolved` — aligned retrieval options with context contract and added explicit metadata/merge acceptance items
  - decision: `approved-with-known-risks`
- 2026-02-19: Expanded PR-17 documentation across planning/spec/general docs.
  - Added Wave 3/PR-17 scope, goals, acceptance criteria, and risk notes in `docs/plans/v2-greenfield-bootstrap.md`.
  - Aligned tracker phase wording to Wave 3 and added direct Wave 3 cross-reference in this file.
  - Added companion-memory bridge note for PR-17 semantic artifact retrieval in `docs/general/companion-memory.md`.
  - Added artifact semantic retrieval contract language to `docs/spec/v2_greenfield_bootstrap.md`.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c74d3-1ca0-7020-8a20-9689f1c2c922`
  - scope: `PR-17 docs alignment slice` (`docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`, `docs/general/companion-memory.md`, `docs/spec/v2_greenfield_bootstrap.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Started PR-17 (Wave 3 kickoff) with libsql-first artifact semantic retrieval.
  - Added `SearchArtifactsByEmbedding` + supporting option/result types in `internal/v2/adapters/libsql/turns/store.go`.
  - Retrieval prefers native `vector_distance_cos(...)` when enabled and downgrades to in-process cosine scoring when vector SQL is unavailable.
  - Added filter support for `session_id` and `artifact_type` with deterministic ranking and bounded limits.
  - Added tests in `internal/v2/adapters/libsql/turns/store_test.go` covering fallback behavior, ordering, and filter correctness.
  - Validation: `go test ./internal/v2/adapters/libsql/turns` and `go test ./internal/v2/...`.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c74cd-b1c3-7592-876a-f17c1b22d19d`
  - scope: `PR-17 kickoff retrieval slice` (`internal/v2/adapters/libsql/turns/{store,store_test}.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `low` — native libsql vector-path branch lacks direct CI coverage; fallback path is covered
  - decision: `approved-with-known-risks`
- 2026-02-18: Documented Wave 2 planning and reset tracker to active Wave 2 execution.
  - Updated `docs/plans/v2-greenfield-bootstrap.md` with Wave 2 goals, consideration areas, proposed PR-11..PR-16 slices, and full-v2 exit criteria.
  - Updated this tracker to mark Wave 1 complete and set Wave 2 live cutover + dynamic context work as current priorities.
- 2026-02-18: PR-11 partial implementation landed for daemon command surface.
  - Routed `agent.spawn`/`agent.list`/`agent.kill` through v2 daemon ports in `internal/daemon/service.go` (`dispatchAgent*` + flag/shadow router) while preserving existing behavior by delegating v2 handlers to current logic.
  - Disabled daemon shadow execution for these mutating RPC methods to avoid duplicate side effects during routing rollout.
  - Added daemon routing tests in `internal/daemon/service_v2_routing_test.go` to assert v1/v2 decision switching via `AGENTCTL_V2_COMMANDS`.
- 2026-02-18: PR-11 completed for CLI/API/daemon command surfaces.
  - Routed real CLI handlers (`agent spawn/run/list/kill`) through `internal/v2/ports/cli` in `cmd/agentctl/cmd/agent.go` with v1 fallback and v2 command opt-in via `AGENTCTL_V2_COMMANDS`.
  - Routed API agent spawn and daemon action handlers through `internal/v2/ports/api` in `internal/web/api/agents.go` while preserving current behavior in v2 delegates.
  - Added handler-level routing tests:
    - `cmd/agentctl/cmd/agent_v2_routing_test.go`
    - `internal/web/api/agents_v2_routing_test.go`
    - extended daemon fallback coverage in `internal/daemon/service_v2_routing_test.go`
- 2026-02-18: Completed PR-12 libsql turn/artifact persistence slice.
  - Added new v2 libsql turns adapter package: `internal/v2/adapters/libsql/turns`.
  - Added production `run.TurnRecorder`/`run.TurnReader` implementation with hierarchical persistence for `Turn -> Iteration -> ToolCall`.
  - Added artifact persistence with stable refs (`turn/{turn_id}/artifact/{artifact_type}/{artifact_version}`), vector-ready schema (`F32_BLOB`) for libsql, and idempotent upsert keyed by `(turn_id, artifact_type, artifact_version)`.
  - Added coverage for lineage roundtrip, lineage replacement on upsert, artifact idempotency, stable-ref lookup, and invalid artifact type handling.
- 2026-02-18: Completed PR-13 event-to-enricher wiring.
  - Added runtime enricher producer component in `internal/v2/runtime/enrichers/producer.go` to subscribe to runtime events and enqueue configured artifact jobs on `turn.recorded`.
  - Wired runner event fanout via optional best-effort bus publish in `internal/v2/runtime/runner/{types,pipeline}.go` with non-fatal `OnEventError` handling.
  - Added integration coverage in `internal/v2/runtime/runner/enricher_wiring_test.go` proving:
    - bus publish failures do not fail turn completion,
    - `turn.recorded` triggers async enrichment jobs and `artifact.failed` events while turns still complete successfully.
  - Hardened queue dedupe lifecycle by releasing keys after job processing (`internal/v2/runtime/enrichers/{queue,worker}.go`) and added retry-friendly release test coverage.
- 2026-02-18: Completed PR-14 hierarchical context builder + temporal pyramid retrieval.
  - Added `run.TurnListOptions` + `run.TurnTimelineReader` and production list support in `internal/v2/adapters/libsql/turns/store.go` with session/time filtering and deterministic ordering.
  - Added context-builder temporal retrieval in `internal/v2/runtime/contextbuilder/builder.go` for `hours`, `days`, `weeks`, and `months` with coarse summaries.
  - Added drill-down metadata outputs (`expandable_dates`, `expandable_refs`) including `day:*`, `hour:*`, `week:*`, and stable `turn/*` refs.
  - Added tests:
    - `internal/v2/adapters/libsql/turns/store_test.go` (`TestTurnStore_ListTurns_BySessionAndTime`)
    - `internal/v2/runtime/contextbuilder/builder_test.go` (`BuildTemporal*` cases)
  - Validated with `go test ./internal/v2/...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72fa-5465-7563-93df-36870fabd2a5`
  - scope: `PR-14 hierarchical context builder slice` (`internal/v2/core/run/turn_record.go`, `internal/v2/adapters/libsql/turns/{schema,store,store_test}.go`, `internal/v2/runtime/contextbuilder/{builder,builder_test}.go`)
  - findings: `none` (review output `overall=warn` was doc/check-status oriented; code-level findings list was empty)
  - decision: `approved-with-known-risks`
- 2026-02-18: Completed PR-15 companion memory layered assembly integration.
  - Added layered context API in `internal/v2/runtime/contextbuilder/layered.go`:
    - `CompanionProvider` + `SetCompanionProvider`
    - `BuildLayered` for deterministic `L2 -> L1 -> L0` assembly
  - Layered output blends companion summaries with temporal turn buckets and stable refs.
  - Added derived slice refs (`turn/{id}#msg:{msg_id}:0-{n}`) from recent turns so assembled context can reference both whole turns and partial slices.
  - Added deterministic integration test in `internal/v2/runtime/contextbuilder/layered_test.go`.
  - Validated with `go test ./internal/v2/runtime/contextbuilder` and `go test ./internal/v2/...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72fe-9fcd-76e0-9550-e0a7700e4d41`
  - scope: `PR-15 layered context slice` (`internal/v2/runtime/contextbuilder/{builder,layered,layered_test}.go`)
  - findings: `none`
  - decision: `approved`
- 2026-02-18: PR-16 partial decommission readiness implementation.
  - Added shared shadow sanitization + mutating opt-in:
    - `AGENTCTL_V2_SHADOW_MUTATING` (default false)
    - mutating shadow commands (`spawn`,`run`,`kill`) blocked unless explicitly enabled
  - Wired CLI/API/daemon command dispatchers to `NewRouterWithShadow(...)` with sanitized shadow flags.
  - Added parity routing tests for non-mutating shadow execution and mutating opt-in behavior:
    - `cmd/agentctl/cmd/agent_v2_routing_test.go`
    - `internal/web/api/agents_v2_routing_test.go`
    - `internal/daemon/service_v2_routing_test.go`
  - Added explicit PR-16 parity-window and promotion thresholds in `docs/plans/v2-greenfield-bootstrap.md`.
- 2026-02-18: Completed PR-16 v1 decommission readiness gates.
  - Added v1-freeze command set parsing via `AGENTCTL_V2_FREEZE_V1_COMMANDS` in `internal/v2/ports/config/v2flags.go` (+ tests).
  - Added freeze enforcement in `internal/v2/ports/router.go` so frozen commands fail fast with `ErrPolicyViolation` before either runner executes.
  - Wired CLI/API/daemon routers and dispatchers to pass freeze flags with shadow flags via `NewRouterWithShadowAndFreeze(...)`.
  - Added freeze-routing coverage in:
    - `internal/v2/ports/router_shadow_test.go`
    - `cmd/agentctl/cmd/agent_v2_routing_test.go`
    - `internal/web/api/agents_v2_routing_test.go`
    - `internal/daemon/service_v2_routing_test.go`
  - Validated with:
    - `go test ./internal/v2/ports/config ./internal/v2/ports ./cmd/agentctl/cmd ./internal/web/api ./internal/daemon`
    - `go test ./internal/v2/...`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c7315-8761-7543-9035-f8ad55dab480`
  - scope: `PR-16 freeze-gate completion slice` (`internal/v2/ports/{config/v2flags.go,router.go,router_shadow_test.go,cli/router.go,api/router.go,daemon/router.go}`, `cmd/agentctl/cmd/{agent.go,agent_v2_routing_test.go}`, `internal/web/api/{agents.go,agents_v2_routing_test.go}`, `internal/daemon/{service.go,service_v2_routing_test.go}`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c730f-e7ef-7a01-af53-4f912ff57574`
  - scope: `PR-16 partial decommission readiness slice` (`internal/v2/ports/config/{v2flags,v2flags_test}.go`, `cmd/agentctl/cmd/{agent.go,agent_v2_routing_test.go}`, `internal/web/api/{agents.go,agents_v2_routing_test.go}`, `internal/daemon/{service.go,service_v2_routing_test.go}`, `docs/plans/{v2-greenfield-bootstrap,v2-implementation-todo}.md`)
  - findings: `none` (overall `pass`; note: reviewer requested a full-suite run before merge, completed in local verification step)
  - decision: `approved-with-known-risks`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72be-2275-7aa2-bfd2-7904f4cbeafb`
  - scope: `PR-11 daemon routing slice` (`internal/daemon/service.go`, `internal/daemon/service_v2_routing_test.go`)
  - findings: `none` (approved: safe fallback behavior and stable routing tests)
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72ca-4cd5-74a0-b5bd-548700eaee61`
  - scope: `PR-11 expanded routing slice` (`cmd/agentctl/cmd/agent.go`, `cmd/agentctl/cmd/agent_v2_routing_test.go`, `internal/web/api/agents.go`, `internal/web/api/agents_v2_routing_test.go`, `internal/daemon/service.go`, `internal/daemon/service_v2_routing_test.go`)
  - findings: `none` (non-blocking note addressed by adding daemon invalid-env fallback test coverage)
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72c1-d634-7a21-85b2-5dd546d8dd11`
  - scope: `PR-11 daemon routing slice (post-shadow-safety fix)` (`internal/daemon/service.go`, `internal/daemon/service_v2_routing_test.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none` (approved; daemon shadow disabled for mutating routes and tests remain coherent)
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72b7-b0a7-7a13-ba4b-a50ebc5c6eb0`
  - scope: `Wave 2 doc alignment` (`docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none` (optional improvement applied: added direct Wave 2 cross-reference in tracker)
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72da-b7c0-7c53-a1d7-8c2e27f12649`
  - scope: `PR-12 libsql turns/artifacts slice` (`internal/v2/adapters/libsql/turns/schema.go`, `internal/v2/adapters/libsql/turns/store.go`, `internal/v2/adapters/libsql/turns/store_test.go`)
  - findings: `none` (initial low-risk vector fallback note addressed by atomic disable-after-unsupported behavior)
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c72e1-080c-7451-9b1a-424c35c55ef2`
  - scope: `PR-13 event-to-enricher slice` (`internal/v2/runtime/runner/{types,pipeline}.go`, `internal/v2/runtime/enrichers/{producer,queue,worker}.go`, `internal/v2/runtime/enrichers/producer_test.go`, `internal/v2/runtime/runner/enricher_wiring_test.go`, `internal/v2/runtime/enrichers/worker_test.go`)
  - findings: `none` (low-risk dedupe-map growth note resolved by key release on worker completion; residual risk: queue-full drops remain telemetry-only)
  - decision: `approved`
- 2026-02-18: Created v2 tracker; linked v2 docs to companion/general references; added kickoff batches in main v2 plan.
- 2026-02-18: Completed PR-01A scaffold (`internal/v2/*` package tree), added `v2flags` parser + tests, added core import-boundary guard test, and validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c7088-6e8c-7e80-871d-371c266a6728`
  - scope: `PR-01A` scaffold (`internal/v2/**`)
  - findings: `none` (no blocking issues). Residual risks:
    1) `internal/v2/scaffold_test.go` is compile-only; add behavioral tests as runtime logic lands.
    2) `internal/v2/ports/config/v2flags.go` command map must stay in sync with actual routed v2 commands.
  - decision: `approved-with-known-risks`
- 2026-02-18: Completed PR-01B error/event contracts and deterministic test harness.
  - Added `internal/v2/core/errors/errors.go` with HTTP status mapping + `ToEvent` conversion.
  - Added `internal/v2/core/events/{types,payloads}.go` and deterministic testkit fakes/comparator.
  - Added first JSONL fixture at `internal/v2/runtime/runner/testdata/golden_events/pr01b_event_contract.jsonl`.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c7091-4f82-7612-a4dd-c42bb6040469`
  - scope: `PR-01B` contracts/harness (`internal/v2/core/errors`, `internal/v2/core/events`, `internal/v2/testkit/*`, fixture + scaffold import update)
  - findings: `none`
  - decision: `approved`
- 2026-02-18: Completed PR-02A as a libsql-first storage slice.
  - Added `internal/v2/core/events/repository.go` contracts for stream listing/replay.
  - Added libsql adapters under `internal/v2/adapters/libsql/{events,projections,idmap}` with schema/store/replay.
  - Added tests for monotonic append, replay rebuild, legacy lookup, and idmap roundtrip.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c70a0-2f79-7952-b1e7-dabccc0b23f7`
  - scope: `PR-02A` libsql adapters (`internal/v2/adapters/libsql/**`) + `internal/v2/core/events/repository.go`
  - findings: `none`
  - decision: `approved`
- 2026-02-18: Completed PR-03 runner pipeline (no transport).
  - Added canonical stage pipeline under `internal/v2/runtime/runner/*`:
    `InitContext -> ResolveDependencies -> ApplyPreHooks -> BuildToolset -> ModelCall -> ApplyPostHooks -> PersistTurn -> EmitEvents`.
  - Added v2 run domain types in `internal/v2/core/run/types.go`.
  - Added deterministic runner fakes: `fake_model`, `fake_tool_executor`.
  - Added runner tests for happy path, max-iteration degrade, non-fatal stage failure continuation, cancellation-before-persist, model no-tool/done handling, tool failure, post-hook failure, and golden JSONL stability.
  - Added golden fixture: `internal/v2/runtime/runner/testdata/golden_events/run_worker_toolflow_success.jsonl`.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c70bc-a5c4-73a2-a811-6cbbc3d9574a`
  - scope: `PR-03` runner pipeline (`internal/v2/core/run/types.go`, `internal/v2/runtime/runner/*`, `internal/v2/testkit/fakes/{fake_model,fake_tool_executor}.go`, golden fixture)
  - findings: `none`
  - decision: `approved`
- 2026-02-18: Completed PR-04 unified tool stack.
  - Added core tool contracts in `internal/v2/core/tool/types.go` (`ProcessProfile`, `ToolDef`, `ToolPolicy`, `ToolCatalog`).
  - Added runtime profile allowlists in `internal/v2/runtime/profiles/profiles.go`.
  - Added unified runtime catalog/executor in `internal/v2/runtime/tools/{types,catalog,executor}.go` with deny-by-default profile filtering and schema-based arg validation.
  - Added v1 bridge adapter in `internal/v2/adapters/v1bridge/tool_bridge.go` to map legacy execution to v2 tool result/error semantics.
  - Extended deterministic fake tool executor (`internal/v2/testkit/fakes/fake_tool_executor.go`) with per-tool call counting.
  - Added PR-04 acceptance tests in `internal/v2/runtime/tools/executor_test.go` and bridge tests in `internal/v2/adapters/v1bridge/tool_bridge_test.go`.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c70cd-a3b7-7d41-bb05-197f51ca95c4`
  - scope: `PR-04` unified tools (`internal/v2/core/tool/types.go`, `internal/v2/runtime/profiles/profiles.go`, `internal/v2/runtime/tools/*`, `internal/v2/adapters/v1bridge/tool_bridge.go`, `internal/v2/testkit/fakes/fake_tool_executor.go`)
  - findings: `none`
  - decision: `approved`
- 2026-02-18: Completed PR-05 unified service layer.
  - Added v2 command DTOs for spawn/ask/list/kill and service interfaces in `internal/v2/core/{spawn,ask,list,kill,services}`.
  - Added service implementations in `internal/v2/services/{dependencies,run_service,spawn_service,ask_service,list_service,kill_service}.go`.
  - Added v1 interop bridges in `internal/v2/adapters/v1bridge/{spawn_bridge,ask_bridge,kill_bridge}.go`.
  - Added PR-05 acceptance tests in `internal/v2/services/services_test.go` for spawn create/idempotency, ask policy gating, kill idmap fallback, and projection-filtered list behavior.
  - Fixed reviewer findings by making default ID generation concurrency-safe and resolving idmap in kill flow even without projections.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c70dd-1d42-7e93-8ee2-0876814d592a`
  - scope: `PR-05` services + v1 bridges (`internal/v2/core/{spawn,ask,list,kill}/types.go`, `internal/v2/core/services/interfaces.go`, `internal/v2/services/*`, `internal/v2/adapters/v1bridge/{spawn_bridge,ask_bridge,kill_bridge}.go`)
  - findings: `none` (initial findings fixed: default ID generator race, kill idmap fallback when projections are nil)
  - decision: `approved`
- 2026-02-18: Completed PR-06 v2 ports + feature-flag routing.
  - Added shared routing dispatch core in `internal/v2/ports/router.go` with command-level v1/v2 decisioning and observability hook support.
  - Added CLI port routing adapters in `internal/v2/ports/cli/{router,spawn,ask,run,list,kill}.go`.
  - Added API port routing + envelope mappers in `internal/v2/ports/api/{router,mappers}.go`.
  - Added daemon method routing in `internal/v2/ports/daemon/router.go` with `agent.*` method to command mapping and unknown-method v1 fallback.
  - Added PR-06 acceptance-style tests:
    - `TestRouter_DefaultV1WhenFlagUnset`
    - `TestRouter_SingleCommandOptInToV2`
    - `TestCLI_API_ParityShellTest_Ask`
    - `TestDaemonDispatch_SpawnAskKillRouting`
    - `TestKillCommandRollbackToV1WhenNotEnabled`
    - `TestRouter_EnvelopeContract_ParityV1V2`
  - Added regression tests for wrapped v2 error mapping and unknown daemon method observability.
  - Validated with `go test ./internal/v2/...` and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c716b-6c9c-70d2-a6db-4342780e9f4a`
  - scope: `PR-06` routing (`internal/v2/ports/router.go`, `internal/v2/ports/cli/*`, `internal/v2/ports/api/*`, `internal/v2/ports/daemon/router.go`, associated tests)
  - findings: `none` (initial findings fixed: wrapped `V2Error` unwrapping in API mapper, unknown-method observability in daemon router)
  - decision: `approved`
- 2026-02-18: Completed PR-07 supervisor + runtime event bus.
  - Added supervisor lifecycle host and tests in `internal/v2/runtime/supervisor/{component,host,host_test}.go`.
  - Added bounded runtime event bus with explicit overflow policies and telemetry counters in `internal/v2/runtime/events/{doc,bus,bus_test}.go`.
  - Added PR-07 tests for bounded overflow behavior and slow-subscriber non-deadlock fanout.
  - Validated with `go test ./internal/v2/runtime/supervisor ./internal/v2/runtime/events` and `go test ./internal/v2/...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c717c-b8c2-7421-a2ff-9b1ef21290fa`
  - scope: `PR-07` supervisor + runtime bus (`internal/v2/runtime/supervisor/*`, `internal/v2/runtime/events/*`)
  - findings: `approved with low-risk note` (publish currently holds read lock during blocking overflow waits; acceptable for now)
  - decision: `approved`
- 2026-02-18: Completed PR-08 snapshots + non-blocking maintenance.
  - Added immutable snapshot store with deep-clone load/store semantics in `internal/v2/runtime/snapshots/store.go`.
  - Added first maintenance projector component in `internal/v2/runtime/maintenance/digest_component.go` that consumes runtime events and publishes digest snapshots without blocking turn execution.
  - Added PR-08 tests:
    - `TestSnapshotStore_LoadStoreAtomic`
    - `TestSnapshotStore_ConcurrentReaders_NoContentionRegression`
    - `TestMaintenanceComponent_PublishesSnapshot`
    - `TestMaintenanceFailure_DoesNotBlockRunEngine`
    - `TestSnapshotProjection_Parity`
  - Validated with `go test ./internal/v2/runtime/snapshots ./internal/v2/runtime/maintenance`, `go test ./internal/v2/runtime/...`, `go test ./internal/v2/...`, and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c718b-43e1-7d91-a157-679f98ff08e2`
  - scope: `PR-08` snapshots + maintenance (`internal/v2/runtime/snapshots/*`, `internal/v2/runtime/maintenance/*`)
  - findings: `none` (initial observability note addressed by default projector error logging)
  - decision: `approved`
- 2026-02-18: Completed PR-09 turn intelligence + context builder.
  - Added canonical lineage records in `internal/v2/core/run/{turn_record,iteration_record,tool_call_record}.go`.
  - Wired runner lineage persistence via optional `TurnRecorder` in `internal/v2/runtime/runner/{types,init_context,model_call,persist_turn}.go`.
  - Added async enrichers with idempotent queue keys `(turn_id, artifact_type, artifact_version)` in `internal/v2/runtime/enrichers/{queue,worker}.go`.
  - Added deterministic context reference parsing/building in `internal/v2/runtime/contextbuilder/{ref_parser,builder}.go`.
  - Added PR-09 tests:
    - `TestTurnRecord_PersistsIterationAndToolCallLineage`
    - `TestTraceLineage_ParentSpanRelationships`
    - `TestEnricherQueue_IdempotentByArtifactVersion`
    - `TestEnricherFailure_DoesNotBlockTurnCompletion`
    - `TestContextBuilder_ResolveWholeTurnRef`
    - `TestContextBuilder_ResolveSliceRef`
  - Validated with `go test ./internal/v2/runtime/runner ./internal/v2/runtime/enrichers ./internal/v2/runtime/contextbuilder`, `go test ./internal/v2/...`, and `go test -tags=libsqlite3 ./...`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c71a3-e2c6-7161-bd36-6d5ba86b134a`
  - scope: `PR-09` lineage/enrichers/contextbuilder (`internal/v2/core/run/*`, `internal/v2/runtime/{runner,enrichers,contextbuilder}/*`, `internal/v2/scaffold_test.go`)
  - findings: `none` (initial queue close/send race in `enrichers/queue.go` fixed and re-reviewed)
  - decision: `approved`
- 2026-02-18: Completed PR-01A residual-risk follow-up and shadow-route decision.
  - Added `SupportedCommands()` export in `internal/v2/ports/config/v2flags.go` and canonical-set test coverage in `internal/v2/ports/config/v2flags_test.go`.
  - Added command-surface drift guards:
    - `internal/v2/ports/daemon/command_surface_test.go`
    - `internal/v2/ports/cli/command_surface_test.go`
    - `internal/v2/ports/api/router_test.go` (`TestRouter_AllSupportedCommandsCanRouteV2`)
  - Locked first shadow-validation command to `ask` and updated parity-verification sequencing in `docs/plans/v2-greenfield-bootstrap.md`.
  - Validated with `go test ./internal/v2/ports/config ./internal/v2/ports/cli ./internal/v2/ports/api ./internal/v2/ports/daemon`, `go test ./internal/v2/...`, `go test -tags=libsqlite3 ./...`, and `make check-doc-links`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c71ae-23eb-7d01-80fb-3ae209caf6de`
  - scope: `PR-01A residual follow-up` (`internal/v2/ports/{config,cli,api,daemon}/*`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-18: Completed PR-10 ask shadow validation plumbing.
  - Added shadow command parsing via `AGENTCTL_V2_SHADOW_COMMANDS` in `internal/v2/ports/config/v2flags.go` (+ tests).
  - Added non-blocking-capable shadow execution contract in `internal/v2/ports/router.go` with `DispatchWithShadow`, `ShadowReport`, comparator support, and parity tests.
  - Wired CLI/API/daemon v2 routers to pass shadow configuration (`internal/v2/ports/{cli,api,daemon}/router.go`).
  - Added real CLI `agent ask` shadow validation hook in `cmd/agentctl/cmd/agent_ask_shadow.go` and called it from `runAgentAsk` after v1 ack/write.
  - Shadow run is side-effect safe (no second mailbox send) by using v2 ask service with an in-memory dispatcher and emits `agent.ask.shadow` observability events.
  - Updated parity verification docs to include `AGENTCTL_V2_SHADOW_COMMANDS=ask` bootstrap.
  - Validated with `go test ./cmd/agentctl/cmd ./internal/v2/ports/... ./internal/v2/...`, `go test -tags=libsqlite3 ./...`, and `make check-doc-links`.
- 2026-02-18:
  Subagent Review
  - reviewer: `019c71bb-e52a-7a52-a5a9-53e57fd0268b`
  - scope: `PR-10 ask shadow plumbing` (`cmd/agentctl/cmd/{agent.go,agent_ask_shadow.go,agent_ask_shadow_test.go}`, `internal/v2/ports/{config,router,cli,api,daemon}/*`, plan docs)
  - findings: `none`
  - decision: `approved`
