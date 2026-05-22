# Code Quality Cleanup Audit

This backlog comes from the May 2026 multi-agent cleanup audit. The first slice
is in progress on `feat/code-quality-cleanup`; the remaining items are grouped
by cleanup value and implementation risk.

## Completed In Current Slice

- [x] Move the shared clock interface out of `skilltest` and into
  `internal/platform/timeutil`.
- [x] Remove production imports of `internal/adapters/skillslib/skilltest`.
- [x] Alias `session/save` and `session/restore` snapshot types to
  `internal/context/sessionkit/snapshot`.
- [x] Remove the deprecated TUI `StubAgent` / `SetStubAgents` path.
- [x] Remove confirmed dead helpers/constants in touched Go packages and tests.
- [x] Make malformed flow edge conditions fail flow start instead of silently
  becoming unconditional edges.
- [x] Update Kubernetes base/local CAS env vars to the runtime key names.

## Highest Priority Remaining Work

- [ ] Make schema migrations fail honestly.
  - Scope: `internal/storage/tasks/store.go`,
    `internal/storage/sessions/turso_store.go`,
    `internal/storage/memory/turso_store.go`,
    `internal/storage/mailbox/store.go`,
    `internal/runtime/actor/watcher.go`.
  - Problem: broad DDL error swallowing can leave partially migrated stores.
  - Target: suppress only known idempotent duplicate/already-exists cases and
    return every other migration error with table/index context.
  - Tests: corrupt/bad DDL fixtures, duplicate-column idempotency, startup
    failure when required indexes/triggers cannot be created.

- [ ] Stop swallowing controlled storage decode corruption.
  - Scope: trajectory, sessions, pattern store, and testwatch storage decode
    paths.
  - Problem: foxctl-owned JSON/timestamp fields decode to empty values on
    corruption, hiding durable contract drift.
  - Target: preserve explicit empty optional fields, but return wrapped errors
    for invalid persisted JSON or timestamps.
  - Tests: corrupt-row fixtures and legacy empty-field fixtures.

- [ ] Split runtime observability into a neutral contract package.
  - Scope: `internal/runtime/observability` imports from storage, intelligence,
    and context packages.
  - Problem: lower layers depend upward on runtime and an SSE bridge package.
  - Target: move event types/emitter interfaces to a neutral platform/domain
    package; keep SSE/runtime adapters in runtime/interface layers.
  - Tests: affected storage/intelligence/context packages plus web SSE tests.

- [ ] Remove `internal/storage/jobs/executor` runtime ownership.
  - Problem: a storage package imports runtime execution and constructs runtime
    executors.
  - Target: keep storage jobs as persistence/types only; move executor wiring to
    a runtime job package.
  - Tests: jobs, runtime execution, and daemon job startup.

## Type And Contract Consolidation

- [ ] Consolidate frontend API DTOs into `@foxctl/data`.
  - Scope: `packages/data`, `packages/gui-agent`, `packages/foxterm`.
  - Problem: `gui-agent` keeps local copies of envelopes, orchestration,
    mailbox, room, blackboard, and agent types that drift from `@foxctl/data`.
  - Target: add `@foxctl/data` to `gui-agent`; move shared DTOs and make
    UI-only types explicit extensions.
  - Tests: data typecheck, gui-agent build, foxterm typecheck.

- [ ] Add canonical room loop and delivery binding DTOs.
  - Scope: backend room-control responses plus both TS clients.
  - Target: centralize `RoomLoop`, delivery trace/result, health snapshot, and
    room delivery binding shapes.

- [x] Replace query-plan wrapper structs with a deliberate shared contract.
  - Scope: `internal/intelligence/searchquery`,
    `internal/intelligence/codecontext`, `internal/intelligence/retrieval/v2`.
  - Target: use aliases to the central `searchquery` types unless a stable
    facade is genuinely needed.

- [x] Consolidate OpenAI-compatible SSE stream DTOs.
  - Scope: console app and runtime engine streaming chunks.
  - Target: move chunk/delta/tool-call stream wire structs to a neutral provider
    compatibility package.

- [x] Remove duplicated path/file read helpers in indexers.
  - Scope: semantic and symbol indexers.
  - Target: shared `ReadLimited` helper with traversal, symlink, directory, and
    size-limit tests.

## Weak Types And Boundary Cleanup

- [ ] Type orchestration board payload normalization.
  - Scope: `packages/data`, `packages/gui-agent`, `packages/foxterm`.
  - Target: `OrchestrationBoardPayload` union with guards for board vs artifact
    reference.

- [ ] Remove frontend double-casts around known API shapes.
  - Scope: Flow canvas details, room timeline events, and agent chat SSE.
  - Target: explicit `FlowDetail`, typed timeline event input, `SSEEnvelope<T>`,
    and stream event guards.

- [ ] Replace Go path extraction map blobs with typed input structs.
  - Scope: `internal/platform/pathutil` and hook pathutil tests.

- [ ] Replace v2 tool schema `map[string]any` parsing with typed JSON-schema
  subset structs.
  - Scope: `internal/v2/runtime/tools`.

- [ ] Type known semantic envelope metadata.
  - Scope: `internal/intelligence/searchindex`.
  - Target: `SemanticEnvelopeMetadata` with backward-compatible legacy map
    parsing.

- [ ] Convert codemap tool handler args one handler at a time.
  - Scope: `internal/intelligence/codemap/tools`.
  - Progress: `read_file` and `search_pattern` now decode through typed args;
    remaining handlers still use map assertions.

## Defensive Programming And Hidden Fallbacks

- [ ] Surface OpenCode hook skill failures instead of presenting empty results.
  - Scope: `configs/opencode-hooks/index.ts`.
  - Target: preserve structured skill errors in injected context; fail closed or
    warn for large-file stat failures.

- [ ] Make Hermes memory fallback failures honest.
  - Scope: `integrations/hermes/client.py` and config loading.
  - Target: catch expected exceptions only, accumulate errors, and return
    `ok:false` or raise when both CLI and HTTP paths fail.

- [ ] Reject empty/malformed LLM verification output.
  - Scope: verification LLM client and CoVe refiner.
  - Target: return explicit errors for empty provider content and malformed
    refiner contracts.

- [ ] Record or return solver split failures.
  - Scope: RLM braid executor split path.

- [ ] Require canonical symbol memory identity for new embedding jobs.
  - Scope: embedding worker and embedding store.
  - Target: require `memory_name` or `package_id + symbol_key`; migrate or skip
    incomplete old queue rows explicitly.

## Unused, Deprecated, And Legacy Code

- [ ] Remove staticcheck-confirmed unused Go code in a focused RLM slice.
  - Scope: unused semantic bundle helpers, old braid router-split helpers,
    unused scaffold/factory helpers, unused RLM tool helpers.
  - Tests: staticcheck `U1000`, `go test ./internal/rlm/...`.

- [ ] Delete or replace dead frontend slices.
  - Scope: old `CompanionChat`, `chatStore`, unused activity feed, unused GUI
    API wrappers, unused utility exports, foxterm `getRun` / `RunDetail`.
  - Tests: gui-agent build/test and foxterm typecheck after dependencies are
    installed.

- [ ] Remove deprecated `skillslib/runner`.
  - Scope: test-only imports from old skill runner helpers.
  - Target: move tests to `skillmain` / `skilltest`, then delete the package.

- [x] Update Kubernetes CAS env names.
  - Scope: base/local manifests.
  - Target: replace legacy `FOXCTL_CAS_BACKEND` / `FOXCTL_CAS_BUCKET` with
    current CAS driver/path/S3 bucket variables.
  - Tests: `kubectl kustomize` or manifest snapshot plus config-load coverage.

- [ ] Decide the fate of demo/test binaries under `cmd/companion_*_test`.
  - Target: delete them or move them behind explicit examples/manual-smoke
    boundaries and replace useful coverage with `_test.go` tests.

## Structural Simplification Candidates

- [ ] Consolidate workspace repair helpers across storage packages.
  - Scope: memory, graph, sessions, tasks, and trajectory workspace repair.
  - Target: shared pure path repair plus table/column collection helpers while
    keeping store-specific migrations local.

- [ ] Consolidate skill inline output and workspace resolution helpers.
  - Scope: repo-index/search/codemap skills.

- [ ] Consolidate OpenAPI plugin stdio/envelope harness.
  - Scope: plugin command mains.

- [ ] Consolidate chat adapter command parsing and SSE activity decoding.
  - Scope: Teams, Telegram, Discord chat adapters.

- [ ] Consolidate v2 Turso store opening boilerplate.
  - Scope: projections, turn requests, effects, and workers stores.

- [ ] Extract shared Jido/goruntime orchestration reconciliation helpers.
  - Target: start with pure helpers such as retry delay, append/project, payload
    builders, and terminal event construction before attempting a larger move.

## Stubs And Honesty Fixes

- [ ] Make indexing `FanoutModeJobs` honest.
  - Scope: indexing handler/types and overseer post-review stubs.
  - Target: either implement real job enqueueing plus file propagation, or remove
    the jobs mode until it exists.

- [ ] Clarify Eino adapter support level.
  - Target: either make it a supported adapter with explicit stream/tool
    behavior, or isolate it as experimental and stop presenting spike behavior
    as a normal backend.

- [ ] Remove `internal/rlm` `ErrNotImplemented` runtime path.
  - Target: split validation from execution or require a real executor at
    construction.

## Tooling Follow-Up

- [ ] Add dependable unused-code tooling to local/CI workflow.
  - Current audit used `staticcheck U1000` successfully. `knip` was useful but
    blocked by missing frontend dependencies/config resolution.

- [ ] Add TypeScript dependency installation or CI command docs for reliable
  `gui-agent`, `foxterm`, and `@foxctl/data` verification.
