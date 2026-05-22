# Code Quality Cleanup Results

Date: 2026-05-22
Branch: `feat/code-quality-cleanup`
Merge request: `!43`

This document summarizes the code quality cleanup audit results so far and the
remaining work that still needs to be sliced down.

## Summary

The cleanup has focused on removing stale compatibility layers, strengthening
type boundaries, making hidden failures visible, and consolidating duplicated
runtime/storage helpers. The branch is now a broad maintainability pass rather
than a single feature change.

Current shape:

- Consolidated shared DTOs into `@foxctl/data`.
- Removed several legacy wrappers and dead runtime paths.
- Made storage migration/decode failures more honest.
- Moved shared infrastructure contracts out of runtime-owned packages.
- Added or tightened tests around the risky cleanup areas.

Integration note: `main` has advanced while this branch has been open. Before
merge, rebase or merge current `main` and rerun the full verification set.

## Completed Results

### Storage And Runtime Boundaries

- Storage migrations now fail on unexpected DDL errors instead of swallowing
  broad migration failures.
- Controlled storage decode corruption now returns wrapped errors for invalid
  persisted JSON/timestamps.
- Runtime observability event contracts moved to a neutral
  `internal/platform/observability` package.
- The skill job executor moved out of storage ownership and into runtime job
  wiring.
- Workspace repair logic was consolidated across memory, graph, sessions,
  tasks, and trajectory stores.
- v2 Turso store opening boilerplate was consolidated behind shared helpers.

### Type And Contract Cleanup

- Shared frontend API DTOs were consolidated into `@foxctl/data`.
- Room loop, delivery binding, mailbox, blackboard, room task, orchestration,
  envelope, and agent DTOs now share a first-class package boundary.
- Query-plan wrapper structs were replaced by a shared search query contract.
- OpenAI-compatible SSE stream DTOs moved into a neutral provider
  compatibility package.
- Indexer file reads now use a shared bounded reader with traversal, symlink,
  directory, and size-limit coverage.
- v2 tool JSON schema parsing now uses typed schema subset structs.
- Codemap tool handlers now decode through typed per-tool argument models.
- Semantic envelope metadata is typed at the boundary.

### Legacy And Compatibility Removal

- Deprecated `skillslib/runner` was removed.
- Deprecated TUI `StubAgent` / `SetStubAgents` paths were removed.
- RLM `ErrNotImplemented` runtime scaffolding and confirmed unused RLM helpers
  were removed.
- Companion live demo binaries moved to `examples/manual-smoke/companion/*`
  behind the `manualsmoke` build tag.
- `@foxctl/data/client` now uses canonical agent spawn/ask and mailbox routes.
- Room status now emits canonical `actionable_backlog`; the GUI client no
  longer translates legacy `action_required` payloads.
- foxterm now imports shared room DTOs directly from `@foxctl/data/types`.
- GUI console model requests now send canonical
  `story_gather_model` / `story_dialogue_model` / `llm_model` fields instead
  of deprecated `tool_model` / `response_model` shims.

### Honesty Fixes

- Indexing `FanoutModeJobs` was made honest.
- Experimental Eino adapter support is now labeled explicitly.
- Empty or malformed CoVe LLM output is rejected instead of accepted.
- RLM split failures are recorded or returned.
- New symbol embedding jobs require canonical symbol memory identity.

### Verification Used During Slices

Verification has been run slice-by-slice, including:

- `go test ./internal/interfaces/web/api`
- `go test ./internal/rlm/...`
- `go test` for affected storage, runtime, indexing, and provider packages
- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/gui-agent build`
- `bun run --cwd packages/foxterm typecheck`
- `make check-doc-links`
- `git diff --check`
- commit-hook static analysis, `gofumpt`, `golangci-lint`, large-file guard,
  and tech-debt marker scan

## Outstanding Items

### Compatibility Layers

- Remove the remaining `gui-agent` `@/api/types` DTO alias facade.
- Migrate shared GUI DTO imports directly to `@foxctl/data/types`.
- Move GUI-only view models and helpers into local GUI modules.
- Avoid duplicate `gui-agent` copies of shared `@foxctl/data/client` functions.
- Consolidate duplicated backend `AgentResponse` conversion.
- Replace map-shaped daemon fallback responses with typed response structs.
- Choose and enforce the canonical room transport model:
  `delivery_binding` plus explicit transport fields, or isolate legacy
  `backend/session/pane_id` compatibility.
- Decide whether v2 Turso `OpenDBCompatWithCloser` is transitional debt or
  should move to `dbdriver.DB`.

### Weak Types

- Type orchestration board payload normalization with an
  `OrchestrationBoardPayload` union and guards for board vs artifact reference.
- Remove frontend double-casts around Flow details, room timeline events, and
  agent chat SSE events.
- Add explicit `FlowDetail`, typed timeline event input, `SSEEnvelope<T>`, and
  stream event guards.

### Hidden Fallbacks

- Surface OpenCode hook skill failures instead of presenting empty results.
- Make Hermes memory fallback failures honest when both CLI and HTTP paths fail.

### Dead Frontend Code

- Delete or replace old `CompanionChat`, `chatStore`, unused activity feed,
  unused GUI API wrappers, unused utility exports, foxterm `getRun`, and
  foxterm `RunDetail` if they are confirmed unused.

### Structural Consolidation

- Consolidate skill inline output and workspace resolution helpers across
  repo-index/search/codemap skills.
- Consolidate OpenAPI plugin stdio/envelope harness code.
- Consolidate chat adapter command parsing and SSE activity decoding across
  Teams, Telegram, and Discord adapters.
- Extract shared Jido/goruntime orchestration reconciliation helpers, starting
  with pure retry delay, append/project, payload builder, and terminal event
  helpers.

### Tooling

- Add dependable unused-code tooling to the local/CI workflow.
- Document or install reliable TypeScript verification commands for
  `gui-agent`, `foxterm`, and `@foxctl/data`.

## Recommended Next Slices

1. Remove the `gui-agent` `@/api/types` alias facade in sub-slices.
2. Type orchestration board payload normalization.
3. Remove frontend double-casts around known API shapes.
4. Make Hermes fallback failures honest.
5. Add reliable unused-code tooling and frontend verification docs.
