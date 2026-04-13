# V2 Implementation Todo Tracker

Status: Active  
Owner: Solo maintainer  
Last Updated: 2026-03-02

## Objective

Implement the v2 runtime incrementally with:

- one orchestration path per command
- one tool execution path
- append-only events + projections
- non-blocking enrichment/context assembly
- hard-cut v2 command paths for supported agent surfaces (`spawn`, `ask`, `run`, `list`, `kill`)
- removal of transitional v1 fallback/shadow command routing for active surfaces

Primary specs/plans:

- `docs/spec/v2_greenfield_bootstrap.md`
- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/plans/v2-greenfield-bootstrap.md`

Note: historical entries in this tracker may reference transitional routing flags used during migration phases.
Note: historical entries may also reference deleted packages (`internal/v2/ports/*`, `internal/v2/adapters/v1bridge/*`) that are now archived.

## Current State

- [x] Wave 1 foundation complete (PR-01 through PR-10) with tests and review notes
- [x] v2 docs aligned to companion/general references and non-blocking v2 scope rules
- [x] Wave 2 production cutover batch 1 complete (PR-11 through PR-13)
- [x] Wave 2 dynamic context + decommission gate batch complete (PR-14 through PR-16)
- [x] Wave 3 kickoff complete for PR-17 (artifact retrieval + semantic observability/tracing)
- [x] Wave 3 source-resynthesis slice complete (PR-19: source conversations -> v2 turns/artifacts)
- [x] Wave 3 routing-default slice complete (PR-20: v2-primary command routing defaults)
- [x] Wave 4 planning documented (PR-25/PR-26/PR-27: episodes, WorkingContext gate, evidence-cited narrative)

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
  - [x] wire daemon `agent.spawn`/`agent.list`/`agent.kill` request handling to v2 command handlers in `internal/daemon/service.go`
  - [x] wire CLI `spawn/run/list/kill` entrypoints to v2 command handlers in `cmd/agentctl/cmd/agent.go`
  - [x] wire API agent spawn/daemon action handlers to v2 command handlers in `internal/interfaces/web/api/agents.go`
  - [x] remove transitional per-command v1 fallback/shadow routing from active command surfaces
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

- [x] PR-18: libsql vector-path validation + retrieval quality hardening
  - [x] add deterministic integration tests that exercise native libsql vector SQL path (`vector_distance_cos`) in CI-friendly setup
  - [x] add explicit runtime capability signal (vector enabled/disabled) to retrieval observability output and docs
  - [x] add latency + hit-rate telemetry buckets for semantic artifact search path quality
  - [x] document fallback guardrails (when fallback is expected vs treated as degraded) in `docs/spec/v2_greenfield_bootstrap.md`
  - [x] define rollout gate with measurable thresholds:
    - fallback-only corpus required invariants pass rate = `100%`
    - vector vs fallback semantic overlap@10 >= `90%` on validation corpus
    - fallback ratio <= `5%` over rolling 24h when vector capability is expected

- [x] PR-19: source conversation resynthesis for v2 artifact surfaces
  - [x] add source-import adapter for Claude/Codex JSONL -> canonical v2 turn lineage
  - [x] add deterministic artifact derivation (`annotation`, `classification`, `learning`, optional `embedding`)
  - [x] add `agentctl sessions resynthesize-v2` command for direct v2 backfill writes
  - [x] support optional Claude todo snapshot ingestion in artifact synthesis context
  - [x] enforce embedding dimension compatibility with turns store settings

- [x] PR-20: v2-primary command routing defaults
  - [x] make `ParseV2CommandsFromEnv()` default to all supported commands when env is unset/empty
  - [x] add explicit `none` value for `AGENTCTL_V2_COMMANDS` to force global v1 fallback
  - [x] update CLI/API/daemon routing tests to use `none` where v1-primary behavior is required
  - [x] add default-env routing assertions proving v2-primary behavior in command/API/daemon paths
  - [x] retired in hard-cut cleanup (runtime command-level routing flags removed from active command surfaces)
- [x] PR-21: command-surface de-legacy dedup (remove no-op v2 passthrough wrappers)
  - [x] point CLI/API v2 function pointers at canonical handlers directly
  - [x] remove no-op `*V2` wrappers in `cmd/agentctl/cmd/agent.go` and `internal/interfaces/web/api/agents.go`
  - [x] remove daemon `handleAgent*V2` passthrough methods and route both branches to canonical handlers
  - [x] reduced duplicated behavior paths during transition; finalized as hard-cut direct routing
- [x] PR-22: explicit v2 list command path (no v2->v1 aliasing)
  - [x] route daemon `agent.list` v2 branch through dedicated `handleAgentListV2(ctx)` path
  - [x] keep response parity by mapping shared `AgentListResult` builder for both list branches
  - [x] wire CLI/API list v2 function pointers to explicit v2 handlers (no alias pointers)
  - [x] transition tests completed; command now served by hard-cut v2 routing
- [x] PR-23: explicit v2 kill command path (v2 service + bridge)
  - [x] route daemon `agent.kill` v2 branch through dedicated `handleAgentKillV2(ctx, params)` path
  - [x] execute v2 kill branch through `v2/services.KillService` (legacy bridge removed in hard-cut cleanup)
  - [x] keep state-update side effects parity via shared post-kill state update helper
  - [x] wire CLI/API kill v2 function pointers to explicit v2 handlers (no alias pointers)
  - [x] transition tests completed; command now served by hard-cut v2 routing
- [x] PR-24: explicit v2 spawn command path (no v2->v1 alias pointers)
  - [x] route daemon `agent.spawn` v2 branch through dedicated `handleAgentSpawnV2(ctx, params)` path
  - [x] wire CLI/API spawn v2 function pointers to explicit v2 handlers (no alias pointers)
  - [x] keep behavior parity via shared spawn helper implementations
  - [x] transition routing cleanup completed; route ambiguity removed with hard-cut direct wiring
  - [x] keep targeted command-surface tests green

## Next (Wave 4 Planned)

Wave 4 scope and contracts live in:
`docs/plans/v2-greenfield-bootstrap.md` ("Wave 4: Identity-Guided Retrieval + Narrative Views (PR-25+)").

- [x] PR-25 (M3): episode layer + compiler enricher
  - [x] add `v2_episodes` schema with deterministic boundaries and landmark flagging
  - [x] wire asynchronous episode compiler from `turn.recorded` and source-resynthesis paths
    - [x] `turn.recorded` async compiler component skeleton added (`runtime/enrichers`)
    - [x] runtime default compiler wiring now compiles episodes from persisted timeline + artifacts when configured (`sourceimport.SessionEpisodeCompiler` + `EpisodeCompilerConfig{TurnTimeline, ArtifactReader}`)
    - [x] source-resynthesis trigger path for episode compilation (`sessions resynthesize-v2` -> `sourceimport.BuildEpisodes` -> `turnStore.SaveEpisode`)
  - [x] expose episode drill-down refs to turns/anchors in context builder
  - [x] add deterministic tests for episode ordering, idempotency, and non-blocking failure behavior

## Next Candidate Batch (Wave 5 Planning)

- [x] PR-28: explicit v2 `run` command path (remove `runAgentRunV2 = runAgentRunV1` aliasing)
- [x] PR-29: runtime component host wiring for enrichers/maintenance in long-lived v2 run service
  - [x] add `LongLivedRunService` lifecycle gate that serializes `Run` and `Close` semantics
  - [x] sanitize supervisor specs before host construction (skip invalid/empty specs)
  - [x] classify host dependency errors into deterministic retryable/fatal flags
  - [x] add regression tests for invalid-spec path, close-timeout retry, and in-flight run shutdown gating
- [x] PR-30: gui-agent route audit and v2 contract alignment (event stream + context bundle refs)
  - [x] remove remaining daemon-start v2 alias in web API route wiring (explicit v2 handler path)
  - [x] align SSE forwarding scope to v2 runtime/context contract (`context.*` exact ops + scoped `v2.runtime.*` prefixes)
  - [x] emit `context.layered_bundle` observability event with stable context ref metadata for GUI consumption
  - [x] expose context-ref metadata shape in gui-agent `ActivityEvent` types
- [x] PR-31: skill runtime parity audit (`code/*`, `memory/*`, `todo/*`) against v2 turn/artifact contracts
  - [x] add canonical alias normalization for v2 runtime tool names (slash/dot/underscore -> underscore canonical)
  - [x] normalize profile allowlists through canonicalizer and align defaults to include slash-form `code/search` + `memory/query`
  - [x] ensure v1 bridge executes strict canonical runtime names (`code/search` -> `code_search`) without fallback paths
  - [x] expose `todo/*` defaults in v2 profiles and validate execution-path canonicalization (`todo/query` -> `todo_query`)
  - Subagent Review
    - reviewer: Russell (explorer) `019c9f2f-0b6d-7752-9388-a1828291fdb2`
    - scope: `internal/v2/runtime/{toolnames,profiles,tools}`, `internal/v2/adapters/v1bridge/tool_bridge*`, tracker note
    - findings: duplicate canonical collision handling + alias test gaps were flagged and fixed
    - decision: approved-with-known-risks
  - Subagent Review
    - reviewer: James (explorer) `019c9f41-22a2-7ba2-8258-b405362874a7`
    - scope: `internal/v2/runtime/{toolnames,profiles,tools}`, `internal/v2/adapters/v1bridge/tool_bridge*`
    - findings: none
    - decision: approved
- [x] PR-32: embedding stack unification for local+remote providers (hash/lmstudio/voyage) across runtime + resynthesis paths
  - [x] add shared embedder resolver/factory (`ResolveEmbedderConfig`, `NewEmbedderFromResolvedConfig`) in `sourceimport`
  - [x] route `sessions resynthesize-v2` embedder setup through shared resolver/factory path (single resolve pass)
  - [x] add runtime artifact enricher path using shared artifact synthesis with runtime provenance metadata
  - [x] align runtime defaults to avoid provider mislabeling (`ProviderAuto` default) and enforce embedding gating behavior
  - [x] add deterministic tests for embedder resolver/factory, runtime artifact enricher behavior, and command helper wiring
  - Subagent Review
    - reviewer: Ampere (explorer) `019c9f4e-b7fc-7e40-9263-8f23d1bf411e`
    - scope: `cmd/agentctl/cmd/sessions.go`, `cmd/agentctl/cmd/sessions_embedder_test.go`, `internal/v2/adapters/sourceimport/{embedder_factory.go,embedder_factory_test.go,types.go,artifacts.go,importer_test.go}`, `internal/v2/runtime/enrichers/{artifact_enricher.go,artifact_enricher_test.go}`
    - findings: initial review found runtime provenance tagging, provider default attribution, and duplicate resolve issues; follow-up review found none
    - decision: approved
- [x] PR-26 (M4): WorkingContext gate on retrieval
  - [x] extend retrieval options with `WorkingContext` filters (`session/workspace/files/labels/min_salience`)
  - [x] apply hard SQL gating before vector ranking in libsql retrieval
  - [x] apply deterministic soft rerank (similarity + salience + label overlap)
  - [x] emit gate metadata in context output and wide events
- [x] PR-27 (M5): narrative artifact with evidence citations
  - [x] add `narrative` artifact type and session-scoped compiler
  - [x] enforce claim citation to turn anchors at validation/write boundaries
  - [x] integrate narrative blocks into layered assembly with provenance metadata + staleness metadata
  - [x] add rejection tests for uncited narrative claims

## Decisions (Locked)

- [x] supported command surfaces are hard-cut to v2 routing (no runtime command-level v1 fallback/shadow flags)
- [x] no command-level `--dry-run` requirement for v2 rollout
- [x] turn completion must never block on enrichers/maintenance
- [x] turn lineage is `Turn -> Iteration -> ToolCall` with trace metadata
- [x] context retrieval uses stable refs (`turn/*`, slice refs)
- [x] first shadow-validation command is `ask` (lowest rollout risk, high parity signal)

## Open Questions

- [x] libsql embedding storage format and vector indexing policy for artifact tables
- [x] context budget policy across L2/L1/L0 (resolved: global default split + per-command overrides via `LayerBudget`)
- [x] whether and how to backfill selected v1 turns into v2 retrieval surfaces (resolved via `agentctl sessions resynthesize-v2` source-log import/resynthesis path)
- [x] native libsql/Turso vector-path CI coverage for `SearchArtifactsByEmbedding` (CI now runs strict native-vector gate with `AGENTCTL_V2_REQUIRE_NATIVE_VECTOR_SQL=1`)
- [x] episode boundary heuristics and landmark thresholds for M3 (resolved: deterministic rule set with `cosine_distance>=0.30`, todo-completion boundaries, decision-transition boundaries, landmark criteria)
- [x] WorkingContext default strictness for M4 (resolved: fallback ladder `strict -> relax_labels -> relax_salience -> temporal_only` with explicit metadata)
- [x] narrative refresh cadence and trigger policy for M5 (resolved: hybrid trigger `12 turns` or `30 minutes`, plus manual refresh)

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

## Progress Log (Historical Migration Archive)

- 2026-03-02: Completed hard-cut cleanup for command routing migration artifacts.
  - Removed transitional routing packages under `internal/v2/ports/*`.
  - Removed unused legacy bridge package `internal/v2/adapters/v1bridge/*`.
  - Updated active Wave/Decision sections to reflect direct v2 command-surface wiring and retired runtime command-level fallback/shadow flags.
  - Validation: `go test ./internal/v2/... ./internal/daemon ./internal/interfaces/web/api ./cmd/agentctl/cmd` and `make check-doc-links` (pass).
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9f4e-b7fc-7e40-9263-8f23d1bf411e`
  - scope: `PR-32 embedding stack unification` (`cmd/agentctl/cmd/sessions.go`, `cmd/agentctl/cmd/sessions_embedder_test.go`, `internal/v2/adapters/sourceimport/{embedder_factory.go,embedder_factory_test.go,types.go,artifacts.go,importer_test.go}`, `internal/v2/runtime/enrichers/{artifact_enricher.go,artifact_enricher_test.go}`)
  - findings: `initial review found three issues (runtime provenance tag mismatch, provider default attribution, duplicate resolver pass); all fixed; final review found none`
  - decision: `approved`
- 2026-02-27: Completed PR-32 embedding stack unification (hash/lmstudio/voyage) across runtime + resynthesis.
  - Added shared embedder resolver/factory in `internal/v2/adapters/sourceimport/embedder_factory.go` with provider defaults, env fallback handling, and probe helpers.
  - Rewired `agentctl sessions resynthesize-v2` embedder setup to use one resolve pass + resolved constructor path.
  - Added runtime artifact enricher tests and runtime provenance override (`metadata.artifact_from=runtime`) while preserving source-import provenance defaults.
  - Changed runtime artifact enricher default provider to `ProviderAuto` to avoid codex mislabeling in mixed/unspecified runtime contexts.
  - Added command-level helper coverage for resynthesize embedder resolution (`cmd/agentctl/cmd/sessions_embedder_test.go`).
  - Validation: `CGO_ENABLED=0 go test ./internal/v2/adapters/sourceimport ./internal/v2/runtime/enrichers ./cmd/agentctl/cmd -count=1` (pass).
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9f1e-2ade-78c1-9b68-c00af252fdb1`
  - scope: `PR-30 gui-agent route audit + event/context alignment` (`internal/interfaces/web/api/agents.go`, `internal/observability/{wide_event.go,sse_bridge.go,sse_bridge_test.go}`, `internal/v2/runtime/contextbuilder/{layered.go,layered_internal_test.go,layered_observability_test.go}`, `packages/gui-agent/src/api/types.ts`)
  - findings: `none`
  - decision: `approved`
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9f17-3de5-7ac1-8093-1bcf4faf3022`
  - scope: `PR-30 gui-agent route audit + event/context alignment` (`internal/interfaces/web/api/agents.go`, `internal/observability/{wide_event.go,sse_bridge.go,sse_bridge_test.go}`, `internal/v2/runtime/contextbuilder/{layered.go,layered_observability_test.go}`, `packages/gui-agent/src/api/types.ts`)
  - findings: `SSE forwarding scope too broad, plus low test-hardening gaps`
  - decision: `approved-with-known-risks`
- 2026-02-27: Completed PR-30 gui-agent route audit + v2 contract alignment.
  - Replaced remaining web API daemon-start v2 alias with explicit `handleAgentDaemonStartV2` wiring through shared route-safe logic.
  - Added `context.layered_bundle` wide event emission from layered context assembly, including deterministic stable ref slices and ref-count metadata (`refs`, `turn_refs`, `slice_refs`, `episode_refs`, `narrative_refs`, `artifact_refs`).
  - Updated SSE bridge forwarding rules to include exact context operations and scoped v2 runtime prefixes, plus curated extraction of context-ref metadata for GUI activity stream payloads.
  - Added observability/contextbuilder regression tests (`sse_bridge_test.go`, `layered_internal_test.go`, `layered_observability_test.go`) and GUI type alignment for ref metadata (`packages/gui-agent/src/api/types.ts`).
  - Validation: `CGO_ENABLED=0 go test ./internal/observability ./internal/v2/runtime/contextbuilder ./internal/interfaces/web/api -count=1` and `pnpm -C packages/gui-agent exec tsc --noEmit` (pass).
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9f0f-0aba-7672-af97-284e564f9454`
  - scope: `PR-29 long-lived run service` (`internal/v2/services/{long_lived_run_service.go,long_lived_run_service_test.go}`)
  - findings: `none (low test-hardening suggestions only, addressed)`
  - decision: `approved`
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9f09-70ea-7483-a605-d2a847bd2d22`
  - scope: `PR-29 long-lived run service` (`internal/v2/services/{long_lived_run_service.go,long_lived_run_service_test.go}`)
  - findings: `host invalid-spec startup edge case, run/close lifecycle race, retryable classification mismatch`
  - decision: `approved-with-known-risks`
- 2026-02-27: Completed PR-29 long-lived runtime component host wiring.
  - Added `LongLivedRunService` and deterministic `BuildLongLivedRunSpecs` wiring in `internal/v2/services/long_lived_run_service.go`.
  - Added lifecycle hardening: run/close gate, invalid-spec host suppression, and non-retryable terminal dependency classification.
  - Added regression coverage in `internal/v2/services/long_lived_run_service_test.go` for host start-once, host failure surfacing, no-host behavior, post-close behavior, timeout-close retry, invalid specs, and in-flight run shutdown gating.
  - Validation: `go test ./internal/v2/services -count=1` and `go test ./internal/v2/runtime/... ./internal/v2/services/... ./cmd/agentctl/cmd -run 'AgentRun|V2Routing|LongLivedRunService' -count=1` (pass).
- 2026-02-27:
  Subagent Review
  - reviewer: `019c9efa-c142-7731-a9bf-75b1744da466`
  - scope: `PR-28 explicit v2 run command path` (`cmd/agentctl/cmd/agent.go`)
  - findings: `none`
  - decision: `approved`
- 2026-02-27: Completed PR-28 explicit v2 run command path.
  - Replaced `runAgentRunV2Fn = runAgentRunV1` alias with explicit `runAgentRunV2`.
  - Added shared helper `runAgentRunWithRoute(cmd, args)` and routed both v1/v2 run handlers through it for parity.
  - Validation: `go test ./cmd/agentctl/cmd` (pass).
- 2026-02-27:
  Subagent Review
  - reviewer: `019c8797-2d35-7802-bd3e-87b68a229994`
  - scope: `PR-25 runtime episode wiring` (`internal/v2/adapters/sourceimport/{episodes_runtime.go,episodes_runtime_test.go,types.go}`, `internal/v2/runtime/enrichers/{episode_compiler.go,episode_compiler_test.go}`)
  - findings: `none`
  - decision: `approved`
- 2026-02-27: Completed PR-25 runtime episode wiring path.
  - Added `sourceimport.SessionEpisodeCompiler` that derives episodes from persisted session timeline + artifacts (same deterministic `BuildEpisodes` path used by backfill).
  - Extended `EpisodeCompilerConfig` with `TurnTimeline` + `ArtifactReader` and auto-wired runtime default compiler when explicit compiler is not provided.
  - Added tests for:
    - live compiler behavior (`episodes_runtime_test.go`)
    - enrichers default wiring from `turn.recorded` to episode writes (`episode_compiler_test.go`)
  - Validation: `go test ./internal/v2/adapters/sourceimport ./internal/v2/runtime/enrichers` and `go test ./internal/v2/... ./internal/storage/dbdriver/...` (pass).
- 2026-02-22:
  Subagent Review
  - reviewer: `019c8797-2d35-7802-bd3e-87b68a229994`
  - scope: `PR-25 schema + compiler skeleton` (`internal/v2/core/run/episode_record.go`, `internal/v2/adapters/libsql/turns/{schema.go,store.go,store_test.go}`, `internal/v2/runtime/enrichers/{episode_compiler.go,episode_compiler_test.go}`)
  - findings: `none` (one prior review finding about auto-ID collisions was fixed with hashed IDs + regression coverage)
  - decision: `approved`
- 2026-02-22: Started PR-25 implementation (schema + compiler skeleton).
  - Added core episode contracts in `internal/v2/core/run/episode_record.go` (`EpisodeRecord`, `EpisodeWriter`, `EpisodeReader`, list options, not-found error).
  - Added `v2_episodes` schema and indexes in `internal/v2/adapters/libsql/turns/schema.go`.
  - Added episode persistence APIs in turns store:
    - `SaveEpisode`
    - `GetEpisode`
    - `ListEpisodes`
  - Added async episode compiler component skeleton in `internal/v2/runtime/enrichers/episode_compiler.go` subscribed to `turn.recorded`.
  - Added tests:
    - `internal/v2/adapters/libsql/turns/store_test.go` episode roundtrip/idempotency/not-found coverage
    - `internal/v2/runtime/enrichers/episode_compiler_test.go` non-blocking component behavior
  - Validation: `go test ./internal/v2/...` (pass).
- 2026-02-22:
  Subagent Review
  - reviewer: `019c877c-e1a0-7922-9326-09f83c7bee6c`
  - scope: `Wave 4 planning docs` (`docs/plans/psych-memory-m1-m2.md`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `three specification gaps found (episode heuristics, WorkingContext graceful fallback definition, narrative refresh cadence); all resolved in follow-up patch`
  - decision: `approved-with-known-risks` (initial), then resolved via explicit policy updates in same session
- 2026-02-22: Documented Wave 4 (M3/M4/M5) planning in v2 docs.
  - Added psych-memory consistency guardrails to keep immutable evidence + reconstructible derived views explicit.
  - Added Wave 4 section in greenfield plan with PR-25/PR-26/PR-27 scopes and acceptance criteria.
  - Added Wave 4 checklist items and then resolved initial design questions into explicit default policies.
- 2026-02-22:
  Subagent Review
  - reviewer: `019c8604-5703-7ec2-92f3-3d797ac316e7` (final), `019c8602-a782-7b21-8f1a-e611a302cb8d` (initial)
  - scope: `PR-24 explicit v2 spawn path` (`cmd/agentctl/cmd/agent.go`, `internal/interfaces/web/api/agents.go`, `internal/daemon/service.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `initial review found one medium issue (unused route flag in daemon helper), fixed; final re-review found none`
  - decision: `approved`
- 2026-02-22: Completed PR-24 explicit v2 spawn command path.
  - Added dedicated daemon `handleAgentSpawnV2(ctx, params)` route for v2 dispatch (no v2 branch alias in method dispatch).
  - Switched CLI/API spawn v2 function pointers to explicit v2 handlers while retaining existing behavior.
  - Consolidated spawn behavior in shared helpers to preserve parity and keep v1 fallback unchanged.
  - Verification: `go test ./cmd/agentctl/cmd ./internal/interfaces/web/api ./internal/daemon ./internal/v2/...` (pass).
- 2026-02-22:
  Subagent Review
  - reviewer: `019c85f4-8d2e-7731-ac25-934560901d63`, `019c85f4-8e14-7f20-b676-f7bd9124913d`, `019c85fb-d4a5-7650-a26a-291b252c4cf6` (post-review follow-up)
  - scope: `PR-23 explicit v2 kill path` (`internal/daemon/service.go`, `internal/interfaces/web/api/agents.go`, `cmd/agentctl/cmd/agent.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-22: Completed PR-23 explicit v2 kill command path.
  - Added dedicated daemon `handleAgentKillV2(ctx, params)` branch for v2 dispatch and kept v1 route unchanged.
  - Routed v2 kill execution through `v2/services.KillService` using `v2/adapters/v1bridge.KillBridge` to keep migration on v2 service contracts.
  - Preserved kill side effects (agent session map + agents.db stopped-state updates) via shared post-kill helper.
  - Switched CLI/API kill v2 function pointers to explicit v2 handlers while preserving payload behavior.
  - Post-review follow-up: threaded request `context.Context` through v1 kill route in daemon dispatch to preserve cancellation/timeout propagation.
  - Verification: `go test ./cmd/agentctl/cmd ./internal/interfaces/web/api ./internal/daemon ./internal/v2/...` (pass).
- 2026-02-22:
  Subagent Review
  - reviewer: `019c8583-da9f-7122-8f64-cca0d7544e33`, `019c8583-de8e-79a2-a6bd-fc225e5acd7c`
  - scope: `PR-22 explicit v2 list path` (`cmd/agentctl/cmd/agent.go`, `internal/interfaces/web/api/agents.go`, `internal/daemon/service.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none blocking`; one reviewer noted pending placeholders in this tracker entry, now resolved
  - decision: `approved`
- 2026-02-22: Completed PR-22 explicit v2 list command path.
  - Added dedicated daemon `handleAgentListV2(ctx)` route for v2 branch dispatch (no v2->v1 alias call).
  - Unified list result shaping through shared builder to preserve payload parity.
  - Switched CLI/API list v2 function pointers to explicit v2 handler functions while keeping behavior stable.
  - Verification: `go test ./cmd/agentctl/cmd ./internal/interfaces/web/api ./internal/daemon` and `go test ./internal/v2/...` (pass).
- 2026-02-21:
  Subagent Review
  - reviewer: `019c829e-2f38-7e31-9e78-b22e43ed600f`, `019c829e-305d-7e41-90f5-acdb7875d24b`
  - scope: `PR-21 command-surface de-legacy dedup` (`cmd/agentctl/cmd/agent.go`, `internal/interfaces/web/api/agents.go`, `internal/daemon/service.go`, targeted routing tests)
  - findings: `none blocking`; both reviewers flagged follow-up visibility for eventual v2-native replacement paths
  - decision: `approved-with-known-risks`
- 2026-02-21: Completed PR-21 command-surface de-legacy dedup.
  - Removed no-op v2 passthrough wrappers and directly aliased v2 branch defaults to canonical handlers in CLI/API.
  - Simplified daemon method dispatch to call canonical handlers from both v1/v2 branches and removed redundant `handleAgent*V2` methods.
  - Kept `AGENTCTL_V2_COMMANDS` command routing contract intact while reducing duplicate implementation paths.
  - Verification: `go test ./cmd/agentctl/cmd ./internal/interfaces/web/api ./internal/daemon ./internal/v2/...` (pass).
- 2026-02-20:
  Subagent Review
  - reviewer: `019c7a42-5bc2-7fa1-9945-68904fac8326`
  - scope: `PR-20 routing-default slice` (`internal/v2/ports/config/{v2flags.go,v2flags_test.go}`, `cmd/agentctl/cmd/agent_v2_routing_test.go`, `internal/interfaces/web/api/agents_v2_routing_test.go`, `internal/daemon/service_v2_routing_test.go`, `docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-20: Completed PR-20 v2-primary routing defaults.
  - `ParseV2CommandsFromEnv()` now enables all supported commands by default when `AGENTCTL_V2_COMMANDS` is unset/empty.
  - Added explicit global fallback token `AGENTCTL_V2_COMMANDS=none` to force v1 routing.
  - Updated CLI/API/daemon routing tests so intentional v1-primary cases use `none`.
  - Added default-env routing assertions that verify v2-primary behavior in command, API, and daemon dispatch paths.
  - Updated v2 spec/plan docs to reflect default-on routing semantics and rollback guidance.
- 2026-02-20:
  Subagent Review
  - reviewer: `019c7a17-f8a4-71a3-8a7a-3fa2dd05ca9c`
  - scope: `PR-19 source resynthesis slice` (`cmd/agentctl/cmd/sessions.go`, `internal/v2/adapters/sourceimport/*`, `internal/v2/adapters/libsql/turns/store.go`)
  - findings: `none`
  - decision: `approved`
- 2026-02-20: Completed PR-19 source conversation resynthesis into v2 turns/artifacts.
  - Added new source import adapter package at `internal/v2/adapters/sourceimport`:
    - provider detection + parsing for Claude/Codex JSONL sources
    - canonical lineage mapping (`Turn -> Iteration -> ToolCall`)
    - deterministic artifact derivation (`annotation`, `classification`, `learning`, optional `embedding`)
  - Added CLI surface `agentctl sessions resynthesize-v2` in `cmd/agentctl/cmd/sessions.go` with provider/source/session/workspace controls and `--dry-run`.
  - Added optional Claude todo ingestion and integration into synthesized artifact context (`--include-todos`).
  - Added `VectorDimensions()` accessor on turns store and switched embedder dimensions to store-configured values to avoid vector-dimension mismatch in environments with non-default settings.
  - Added parser/artifact derivation tests under `internal/v2/adapters/sourceimport/importer_test.go`.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c7825-d823-7f80-959c-561eb0c65319`
  - scope: `v2 layered budget policy slice` (`internal/v2/runtime/contextbuilder/{layered.go,layered_budget_test.go}`, `docs/spec/v2_greenfield_bootstrap.md`, `docs/general/companion-memory.md`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none` (prior semantic-budget reservation issue fixed and regression-covered)
  - decision: `approved`
- 2026-02-19: Implemented v2 layered context budget policy (L2/L1/L0 + semantic).
  - Added deterministic per-layer budget resolution under a single total cap:
    - defaults `L2=20%`, `L1=25%`, `L0=45%`, `Semantic=10%`
    - semantic budget reallocated to `L0` when semantic retrieval is absent
    - per-request overrides via `LayerBudget`
  - Added pure budget-policy tests in `internal/v2/runtime/contextbuilder/layered_budget_test.go`.
  - Updated v2 spec/plan and companion-memory docs to describe the resolved policy.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c781b-8e7b-73d1-9812-2f77532c3322`
  - scope: `v2 artifact embedding+index policy slice` (`internal/v2/adapters/libsql/turns/{store.go,schema.go,store_test.go,store_vector_cgo_test.go}`, `docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Implemented v2 artifact embedding storage + vector index policy.
  - Added explicit vector index policy constants and index-assisted vector candidate retrieval (`vector_top_k`) in turns adapter.
  - Added strict dimension policy for vector mode (`ErrInvalidEmbeddingDimensions`) on both artifact writes and semantic queries.
  - Added coverage for dimension-mismatch rejection and fallback-compatibility behavior.
  - Aligned cgo vector test config with explicit vector dimensions (`AGENTCTL_V2_TURNS_VECTOR_DIMS=4` / `AGENTCTL_VECTOR_DIMS=4`) for deterministic strict-gate runs.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c7812-a3d8-78f1-b3ff-300fb8c46c04`
  - scope: `PR-18 native vector CI gate slice` (`internal/v2/adapters/libsql/turns/store_vector_cgo_test.go`, `.github/workflows/ci.yml`, `docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Completed PR-18 native vector-path CI hard gate.
  - Added strict-mode behavior to `TestTurnStore_SearchArtifactsByEmbedding_VectorPathLibSQL` via `AGENTCTL_V2_REQUIRE_NATIVE_VECTOR_SQL=1`:
    - local/dev keeps skip behavior when vector SQL is unavailable
    - CI fails when native vector capability is expected but unavailable or downgraded
  - Added dedicated CI step in `.github/workflows/ci.yml` to run the strict native vector-path test.
  - Marked PR-18 and native vector CI coverage tracker items complete.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c7807-10fb-7ac0-ab59-41450b92fd67`
  - scope: `PR-18 rollout-gate no-vector-refs follow-up` (`internal/v2/runtime/contextbuilder/rollout_gate.go`, `internal/v2/runtime/contextbuilder/rollout_gate_test.go`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Hardened PR-18 rollout-gate overlap semantics for empty vector baseline cases.
  - Set per-case `overlapRate` default to `0.0` when `expected==0` so case metrics align with aggregate overlap behavior.
  - Added `TestEvaluateSemanticRolloutGate_NoComparableVectorRefsFailsOverlap` coverage for this path.
  - Guarded `CaseResults` indexing in the new test to avoid panics on empty result slices.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c77fd-dbb5-75f1-9e26-257938cb6f4e`
  - scope: `PR-18 rollout-gate evaluator slice` (`internal/v2/runtime/contextbuilder/rollout_gate.go`, `internal/v2/runtime/contextbuilder/rollout_gate_test.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Implemented PR-18 rollout-gate evaluator and corpus checks.
  - Added pure rollout gate evaluation API in `internal/v2/runtime/contextbuilder/rollout_gate.go`:
    - `SemanticRolloutGateThresholds`
    - `SemanticValidationCase`
    - `EvaluateSemanticRolloutGate(...)`
  - Enforced threshold checks:
    - fallback invariant pass rate (`100%`)
    - vector/fallback overlap@10 (`>= 90%`)
    - fallback ratio (`<= 5%`) when vector capability is expected
  - Added table-driven tests in `internal/v2/runtime/contextbuilder/rollout_gate_test.go` including edge cases:
    - no-sample fallback ratio behavior
    - overlap dedup and top-K limit behavior
- 2026-02-19:
  Subagent Review
  - reviewer: `019c77f9-268a-75f3-a041-483b6cbf45ed`
  - scope: `PR-18 fallback-guardrail docs slice` (`docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Documented PR-18 fallback guardrails in spec and marked tracker item complete.
  - Added required degraded-mode signaling and context invariants for fallback retrieval in `docs/spec/v2_greenfield_bootstrap.md`.
  - Added required telemetry dimensions for promotion readiness (`path`, `vector_capability`, `hit_bucket`, `latency_bucket`).
- 2026-02-19:
  Subagent Review
  - reviewer: `019c77f5-1ace-7d50-b597-f865fddce957`
  - scope: `PR-18 vector-path cgo test scaffold` (`internal/v2/adapters/libsql/turns/store_vector_cgo_test.go`, `docs/plans/v2-implementation-todo.md`)
  - findings: `low` — native vector coverage depends on CI runtime exposing `vector_distance_cos`; test currently skips otherwise
  - decision: `approved-with-known-risks`
- 2026-02-19: Added cgo integration test scaffold for native libsql vector retrieval.
  - Added `internal/v2/adapters/libsql/turns/store_vector_cgo_test.go` to exercise vector-path semantic search with local libsql files.
  - Test currently skips when native vector SQL is unavailable at runtime (store downgrades to fallback), preserving deterministic behavior across environments.
  - Tracker item remains open until CI confirms native vector-path execution coverage.
- 2026-02-19:
  Subagent Review
  - reviewer: `019c77e4-a3a3-7df2-983c-303bdb82af09`
  - scope: `PR-18 capability+telemetry slice` (`internal/v2/core/run/artifact_search.go`, `internal/v2/adapters/libsql/turns/{store.go,store_test.go}`, `internal/v2/runtime/contextbuilder/{builder.go,layered.go,layered_test.go,layered_observability_test.go}`, `docs/observability/wide-events.md`, `docs/spec/v2_greenfield_bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-19: Implemented PR-18 semantic retrieval capability and telemetry buckets.
  - Added explicit semantic retrieval vector capability signaling (`enabled|disabled|unknown`) in `run.ArtifactSearchResult` and libsql turns store responses.
  - Added context-builder artifact stats for:
    - vector/fallback path-scoped latency buckets (`<=10ms`, `<=50ms`, `<=100ms`, `>100ms`)
    - vector/fallback hit-rate buckets (`0`, `1-3`, `4-10`, `>10`)
    - vector capability call distribution (enabled/disabled/unknown)
  - Added layered context metadata and wide-event fields:
    - `artifact_vector_capability`
    - `hit_bucket`
    - `latency_bucket` (wide events)
  - Synced spec/docs contract updates in:
    - `docs/observability/wide-events.md`
    - `docs/spec/v2_greenfield_bootstrap.md`
- 2026-02-19:
  Subagent Review
  - reviewer: `019c75d0-9ede-70a2-8541-b07ab5237599`
  - scope: `PR-18 warning-fix docs slice` (`docs/plans/v2-greenfield-bootstrap.md`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none` (prior warnings resolved: PR-10 deterministic detail added; PR-18 measurable rollout gates added)
  - decision: `approved`
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
  - Routed API agent spawn and daemon action handlers through `internal/v2/ports/api` in `internal/interfaces/web/api/agents.go` while preserving current behavior in v2 delegates.
  - Added handler-level routing tests:
    - `cmd/agentctl/cmd/agent_v2_routing_test.go`
    - `internal/interfaces/web/api/agents_v2_routing_test.go`
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
    - `internal/interfaces/web/api/agents_v2_routing_test.go`
    - `internal/daemon/service_v2_routing_test.go`
  - Added explicit PR-16 parity-window and promotion thresholds in `docs/plans/v2-greenfield-bootstrap.md`.
- 2026-02-18: Completed PR-16 v1 decommission readiness gates.
  - Added v1-freeze command set parsing via `AGENTCTL_V2_FREEZE_V1_COMMANDS` in `internal/v2/ports/config/v2flags.go` (+ tests).
  - Added freeze enforcement in `internal/v2/ports/router.go` so frozen commands fail fast with `ErrPolicyViolation` before either runner executes.
  - Wired CLI/API/daemon routers and dispatchers to pass freeze flags with shadow flags via `NewRouterWithShadowAndFreeze(...)`.
  - Added freeze-routing coverage in:
    - `internal/v2/ports/router_shadow_test.go`
    - `cmd/agentctl/cmd/agent_v2_routing_test.go`
    - `internal/interfaces/web/api/agents_v2_routing_test.go`
    - `internal/daemon/service_v2_routing_test.go`
  - Validated with:
    - `go test ./internal/v2/ports/config ./internal/v2/ports ./cmd/agentctl/cmd ./internal/interfaces/web/api ./internal/daemon`
    - `go test ./internal/v2/...`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c7315-8761-7543-9035-f8ad55dab480`
  - scope: `PR-16 freeze-gate completion slice` (`internal/v2/ports/{config/v2flags.go,router.go,router_shadow_test.go,cli/router.go,api/router.go,daemon/router.go}`, `cmd/agentctl/cmd/{agent.go,agent_v2_routing_test.go}`, `internal/interfaces/web/api/{agents.go,agents_v2_routing_test.go}`, `internal/daemon/{service.go,service_v2_routing_test.go}`, `docs/plans/v2-implementation-todo.md`)
  - findings: `none`
  - decision: `approved`
- 2026-02-18:
  Subagent Review
  - reviewer: `019c730f-e7ef-7a01-af53-4f912ff57574`
  - scope: `PR-16 partial decommission readiness slice` (`internal/v2/ports/config/{v2flags,v2flags_test}.go`, `cmd/agentctl/cmd/{agent.go,agent_v2_routing_test.go}`, `internal/interfaces/web/api/{agents.go,agents_v2_routing_test.go}`, `internal/daemon/{service.go,service_v2_routing_test.go}`, `docs/plans/{v2-greenfield-bootstrap,v2-implementation-todo}.md`)
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
  - scope: `PR-11 expanded routing slice` (`cmd/agentctl/cmd/agent.go`, `cmd/agentctl/cmd/agent_v2_routing_test.go`, `internal/interfaces/web/api/agents.go`, `internal/interfaces/web/api/agents_v2_routing_test.go`, `internal/daemon/service.go`, `internal/daemon/service_v2_routing_test.go`)
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
- 2026-02-23: Completed PR-27 (M5) narrative artifact wiring.
  - Added session-scoped narrative compiler component with hybrid refresh policy (`12` new turns, `30m` max age, and explicit manual refresh) in `internal/v2/runtime/enrichers/narrative_compiler.go`.
  - Extended narrative contract + persistence for compiler progress metadata (`source_turn_id`, `source_turn_index`, `source_turn_count`) across `internal/v2/core/run/narrative_record.go` and `internal/v2/adapters/libsql/turns/store.go`.
  - Added validation + regression tests for uncited claims and session-scoped idempotent narrative upsert in `internal/v2/adapters/libsql/turns/store_test.go`.
  - Integrated narrative blocks and staleness metadata into layered context assembly (`narrative_stale`, `narrative_age_seconds`, `narrative_max_age_seconds`) with test coverage in `internal/v2/runtime/contextbuilder/layered_test.go`.
  - Added source-resynthesis manual narrative build/save path via `internal/v2/adapters/sourceimport/narrative.go` and `cmd/agentctl/cmd/sessions.go`.
  - Validation: `go test ./internal/v2/...`, `go test ./cmd/agentctl/cmd`, and `make check-doc-links`.
- 2026-02-23:
  Subagent Review
  - reviewer: `019c8a46-26b6-71f1-a004-67816b9d2652`
  - scope: `PR-27 narrative slice` (`internal/v2/core/run/narrative_record.go`, `internal/v2/adapters/libsql/turns/store.go`, `internal/v2/runtime/contextbuilder/{builder.go,layered.go,layered_test.go}`, `internal/v2/runtime/enrichers/narrative_compiler.go`, `internal/v2/adapters/sourceimport/narrative.go`, `cmd/agentctl/cmd/sessions.go`)
  - findings: `one determinism issue found initially (store SaveNarrative defaulted to time.Now instead of store clock); fixed by defaulting UpdatedAt from s.now before persistence, then re-reviewed clean`
  - decision: `approved`
