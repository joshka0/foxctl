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
- Hard-cut several API compatibility responses to canonical typed DTOs.
- Isolated v2 Turso stores from the raw `*sql.DB` compatibility opener.
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
- v2 Turso store constructors now depend on a narrow `StoreDB` capability
  interface, and v2 openers use `dbdriver.OpenDB` directly instead of
  `OpenDBCompatWithCloser`.

### Type And Contract Cleanup

- Shared frontend API DTOs were consolidated into `@foxctl/data`.
- The `gui-agent` `@/api/types` pass-through facade was removed; shared GUI
  DTO imports now point directly at `@foxctl/data/types`, while GUI-only view
  models live in local modules.
- Room loop, delivery binding, mailbox, blackboard, room task, orchestration,
  envelope, and agent DTOs now share a first-class package boundary.
- Orchestration board responses now parse through an
  `OrchestrationBoardPayload` union for inline board vs CAS artifact payloads.
- Frontend event stream boundaries now use typed SSE guards for room messages,
  room timeline events, agent chat stream events, and flow status events.
- Residual `LogsViewer` event metadata casts were replaced by explicit
  activity-log conversion helpers, and `@foxctl/data/client` now reads
  `import.meta.env` through a typed Vite metadata contract instead of `any`.
- Query-plan wrapper structs were replaced by a shared search query contract.
- OpenAI-compatible SSE stream DTOs moved into a neutral provider
  compatibility package.
- Indexer file reads now use a shared bounded reader with traversal, symlink,
  directory, and size-limit coverage.
- v2 tool JSON schema parsing now uses typed schema subset structs.
- Codemap tool handlers now decode through typed per-tool argument models.
- Semantic envelope metadata is typed at the boundary.
- Backend agent list/detail/patch responses use one canonical `AgentResponse`
  conversion path.
- Fixed agent-daemon, companion personality, skills/MCP, and room-agile
  responses now use named DTOs instead of anonymous `map[string]any` wrappers.
- Room-control fixed response envelopes now use named DTOs, including status,
  control-snapshot, inbox, tasks, loop, coordinator handoff, message actions,
  and task actions.
- `agent.CompactRoomSummaryForInbox` now returns a typed compact room summary
  instead of a map-shaped JSON blob.
- The GUI orchestration board store now imports canonical
  `@foxctl/data/client` wrappers for board load, runtime load, card action,
  and refresh. The shared board client supports `archived_only`, and the
  unused GUI-only `getOrchestrationBoardCard` wrapper was deleted.
- Frontend unused-code checks now have a repeatable gate:
  `bun run unused:frontend`. The shared data, gui-agent, foxterm, and GUI auth
  gateway TypeScript configs enforce unused locals/parameters where applicable,
  and GitLab runs the gate in a Bun-based `typescript-frontend` job.
- Existing `gui-agent` ESLint React hook findings were cleaned up, and
  `bun run --cwd packages/gui-agent lint` now participates in
  `bun run check:frontend`.

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
- Room member/status responses no longer emit legacy top-level transport
  mirrors; clients use canonical `delivery_binding` and computed
  `transport`/status fields.
- The legacy HTTP `PUT /api/rooms/{room}/members/{actor}/transport` route was
  removed after GUI and foxterm moved to the canonical member binding route.
- Room transport docs and skill-pack guidance were refreshed so future agents
  do not rebuild against deleted legacy response fields.
- Dead foxterm `getRun` / `RunDetail` API exports were removed after caller
  search showed the UI uses `getRuns` plus `getRunTranscript`.
- Dead GUI standalone `CompanionChat`, its private `chatStore`, its barrel
  export, and the unused `ActivityFeed` component were removed. The activity
  store, activity focus store, and activity types remain because they are live
  in logs, sidebars, agent lists, and v2 explorer surfaces.

### Honesty Fixes

- Indexing `FanoutModeJobs` was made honest.
- Experimental Eino adapter support is now labeled explicitly.
- Empty or malformed CoVe LLM output is rejected instead of accepted.
- RLM split failures are recorded or returned.
- New symbol embedding jobs require canonical symbol memory identity.
- OpenCode direct tool failures now surface structured foxctl error details
  instead of presenting failed runs as empty search results.
- Hermes memory writes now report failure when both CLI and companion import
  paths fail, with attempted errors preserved for diagnosis.

### Verification Used During Slices

Verification has been run slice-by-slice, including:

- `go test ./internal/interfaces/web/api`
- `go test ./internal/rlm/...`
- `go test` for affected storage, runtime, indexing, and provider packages
- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/gui-agent build`
- `bun run --cwd packages/gui-agent lint`
- `bun run --cwd packages/foxterm typecheck`
- `bun run check:frontend`
- `bun run unused:frontend`
- `make check-doc-links`
- `git diff --check`
- commit-hook static analysis, `gofumpt`, `golangci-lint`, large-file guard,
  and tech-debt marker scan

## Outstanding Items

### Compatibility Layers

- Decide whether the internal `UpdateRoomMemberTransport` pane registration
  path should move to an explicit binding update helper.
- Migrate or isolate older non-v2 `OpenDBCompat*` callers in the legacy storage
  lane without widening the v2 Turso cleanup.
- Similar remaining GUI/data-client wrapper names currently differ by route,
  params, response shape, or GUI auth behavior; keep them separate until the
  shared client owns an actually matching contract.

### Weak Types

- No current high-priority weak frontend cast remains from the tracked audit
  slice. Continue treating new external event/API boundaries as unknown input
  with explicit guards.

### Hidden Fallbacks

- No known high-priority hidden fallback honesty item remains from the current
  audit. Keep this lane open only for new caller-evidenced failures.

### Dead Frontend Code

- Delete or replace remaining unused GUI API wrappers and unused utility exports
  if they are confirmed unused.
- `bun run unused:frontend` is now the repeatable compiler/linter gate for
  unused frontend imports, locals, and parameters. It does not replace caller
  search or a future dead-export dependency graph pass.

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

- Completed: dependable frontend unused-code and verification commands are wired
  locally and in GitLab CI via `bun run check:frontend` and
  `bun run unused:frontend`.
- Completed: `bun run --cwd packages/gui-agent lint` is clean and included in
  `bun run check:frontend`.
- Follow-up: add a project-level dead-export/dependency graph pass only after
  the current compiler and linter gates are consistently clean.

## Recommended Next Slices

1. Continue structural consolidation follow-ups, starting with one repeated
   helper family that has clear caller evidence.
