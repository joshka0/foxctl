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
- Hard-cut room member binding updates to the canonical `delivery_binding`
  request shape.
- Hard-cut room delivery relay and trace output to the canonical
  `delivery_binding` runtime contract, deleting live fallback relay behavior.
- Isolated v2 Turso stores from the raw `*sql.DB` compatibility opener.
- Added or tightened tests around the risky cleanup areas.

Integration note: current `main` was merged into this branch on 2026-05-23, and
the full verification set has been rerun after the merge.

Gap-fix note: after the initial MR 43 readiness pass, the branch also closed
the stale Pi/Hermes installed-plugin evidence gap, made `pi-extension` room
participants first-class viewer/inbox participants, added local no-auth
CoVe/RLM model configuration tests, and added a real Chrome-based GUI browser
smoke via `bun run smoke:gui-browser`.

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
- Legacy stdlib `*sql.DB` compatibility is now isolated behind
  `dbutil.OpenStoreDB` for non-v2 named stores. Dead `OpenDBCompat` and
  `sqliteutil.OpenDBWithDriver` pass-throughs were deleted, while local
  SQLite-only callers use `dbutil.OpenSQLiteDBShared`.

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
- Room member binding updates now return a named `RoomMemberBindingResponse`
  DTO instead of a fixed anonymous response map.
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
- Frontend exported-symbol checks now have a repo-local dependency graph gate:
  `bun run dead:frontend`. It uses the TypeScript compiler API over active
  frontend packages and fails on targeted externally unused exports.
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
- `@foxctl/data/client` was hard-cut to the active orchestration board UI
  workflow. The root `@foxctl/data` entrypoint no longer re-exports client
  helpers; callers use `@foxctl/data/client` explicitly for the narrow
  orchestration board API.
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
- The canonical member binding route now accepts `delivery_binding` or
  explicit `unbound` updates and no longer maps legacy top-level transport
  fields into the binding.
- Auto room relay now uses canonical participant delivery directly. The legacy
  mux fallback relay path, fallback merge/deduplication helpers, fallback
  policy field, fallback-attempt trace flag, and `legacy_mux`/`legacy_bound`
  statuses were removed from live runtime/API/frontend surfaces.
- Pi and Hermes room binding integrations now submit canonical
  `delivery_binding` data instead of parallel top-level transport fields.
- Persisted room-member mux/transport columns remain only as the SQLite storage
  encoding for `delivery_binding`; the domain `RoomMember` type no longer has
  top-level transport mirror fields. The old `delivery_fallback_policy` column
  is schema-only and inert.
- Pane transport registration now uses the canonical surgical
  `UpdateRoomMemberBinding` path, preserving existing mux presentation fields
  while updating the pane-socket delivery binding. The narrower
  `UpdateRoomMemberTransport` storage helper and its implementation-detail
  tests were deleted.
- Room transport docs and skill-pack guidance were refreshed so future agents
  do not rebuild against deleted legacy response fields.
- Dead foxterm `getRun` / `RunDetail` API exports were removed after caller
  search showed the UI uses `getRuns` plus `getRunTranscript`.
- Dead GUI standalone `CompanionChat`, its private `chatStore`, its barrel
  export, and the unused `ActivityFeed` component were removed. The activity
  store, activity focus store, and activity types remain because they are live
  in logs, sidebars, agent lists, and v2 explorer surfaces.
- Dead GUI API wrappers were removed after caller search confirmed no live
  frontend imports: orchestration cleanup, agent memory compression, mailbox
  list/send, room patch/delete/member patch, blackboard list/post/delete,
  health, console-session settings, console-session list/delete/messages,
  persisted-session get, and flow status/log fetch. Similar live GUI/data
  wrappers stay separate where route, params, response shape, or GUI auth
  behavior differ.
- Dead `@foxctl/data` orchestration exports were removed:
  `dispatchOrchestrationIssue`, `OrchestrationDispatchResult`, and the unused
  `isOrchestrationBoardPayloadBoard` /
  `isOrchestrationBoardPayloadArtifact` predicates.
- Dead frontend export surface was tightened further: unused GUI-local response
  and helper types in `packages/gui-agent/src/api/client.ts` are no longer
  exported, the internal orchestration board shape guards are package-private,
  and the unused `APIResponse<T>` type was deleted in favor of canonical
  `ApiEnvelope<T>`.
- The broad unused `@foxctl/data/client` endpoint helper surface was deleted
  after active caller and dead-export graph evidence showed only orchestration
  board helpers were imported by current UIs.

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
- `bun run dead:frontend`
- `make check-doc-links`
- `git diff --check`
- commit-hook static analysis, `gofumpt`, `golangci-lint`, large-file guard,
  and tech-debt marker scan

Final main-sync verification on 2026-05-23:

- `make build`
- focused coverage reproductions for the agent daemon reply path and console
  no-events smoke path
- focused live TUI daemon fixture tests
- focused room API hard-cut tests for member binding and control snapshot
- `go test ./internal/interfaces/web/api`
- `python3 -m unittest integrations.hermes.test_client`
- `make check`
- `bun run unused:frontend`
- `make check-doc-links`
- `git diff --check`

Current room delivery hard-cut verification on 2026-05-23:

- `go test ./internal/domain/agent ./internal/storage/blackboard ./internal/storage/coordination ./internal/interfaces/web/api ./cmd/foxctl/cmd -count=1`
- `go test ./... -run 'TestNonExistent' -count=1`
- `bun run --cwd packages/data typecheck`
- `bun run --cwd packages/foxterm typecheck`
- `bun run --cwd integrations/pi check`
- `python3 -m unittest integrations.hermes.test_client`
- `bun run --cwd packages/gui-agent build`
- `make check-doc-links`
- `git diff --check`
- `foxctl index repo build` plus `code/dag_grep` over the `RoomMember` /
  `DeliveryBinding` relay path
- `make check`

## Outstanding Items

### Compatibility Layers

- Similar remaining GUI/data-client wrapper names currently differ by route,
  params, response shape, or GUI auth behavior; keep them separate until the
  shared client owns an actually matching contract.
- The room member binding route has been hard-cut to the canonical
  `delivery_binding` request shape, and room delivery relay now uses
  `delivery_binding` as the sole runtime/outward transport contract. Persisted
  mux/transport columns remain an internal storage model used behind the
  binding seam.

### Weak Types

- No current high-priority weak frontend cast remains from the tracked audit
  slice. Continue treating new external event/API boundaries as unknown input
  with explicit guards.

### Hidden Fallbacks

- No known high-priority hidden fallback honesty item remains from the current
  audit. Keep this lane open only for new caller-evidenced failures.

### Dead Frontend Code

- Completed: deleted the confirmed zero-caller GUI API wrappers and tiny dead
  `@foxctl/data` orchestration exports found by the current audit.
- Completed: added a broader dead-export/dependency graph gate for active
  frontend packages with `bun run dead:frontend`, and wired it into
  `bun run unused:frontend`.
- Completed: hard-cut the current broad `@foxctl/data/client` endpoint helper
  surface to only active UI callers.
- `bun run unused:frontend` is now the repeatable compiler/linter gate for
  unused frontend imports, locals, parameters, and targeted export graph
  findings. The strict export gate does not replace caller search before
  deletion.

### Structural Consolidation

- Completed: consolidated duplicated `inline_mode` parsing across code,
  repo-index, and codemap skills into
  `internal/adapters/skillslib/inlineutil`. Each skill still owns its specific
  preview/artifact output shape.
- Completed: consolidated duplicated repo-index skill workspace path
  resolution into `internal/adapters/skillslib/workspaceutil`. Query/open
  skills use required workspace resolution, while wrapper skills keep their
  existing current-directory fallback.
- Completed: consolidated OpenAPI plugin stdio/envelope harness code into
  `internal/interfaces/openapi/plugin` helpers for handshake writing, request
  payload decoding, typed data decoding, and response envelope writing. The
  auth and pagination plugins still own their command-specific behavior.
- Completed: consolidated duplicated chat adapter SSE activity decoding into
  `internal/interfaces/chatadapter.DecodeActivitySSEMessage`. Discord and
  Telegram keep their silent-skip behavior for malformed events, while Teams
  still emits `teams.sse_parse_failed` with the same parse-stage labels.
- Deferred: Teams and Telegram text command option parsing is duplicated, but
  the adapter entry points are not the same contract. Do not consolidate
  Discord command handling with those text parsers; Discord receives typed
  platform slash-command options.
- Completed: extracted runtime-neutral Jido/goruntime reconciliation helpers
  for append-then-project event writes and bounded exponential retry delay into
  `internal/v2/runtime/orchestration`.
- Deferred: dispatch payload builders and terminal event identity helpers stay
  adapter-local for now. Jido and Go-runtime have similar code there, but their
  request/stream IDs and payload details are not the same contract.

### Tooling

- Completed: dependable frontend unused-code and verification commands are wired
  locally and in GitLab CI via `bun run check:frontend` and
  `bun run unused:frontend`.
- Completed: `bun run --cwd packages/gui-agent lint` is clean and included in
  `bun run check:frontend`.
- Completed: project-level frontend dead-export gate `bun run dead:frontend`
  is wired without adding dependencies.
- Completed: added `bun run smoke:gui-browser`, a dependency-free browser smoke
  that starts an isolated foxctl backend, starts Vite against that backend, and
  verifies `/`, `/#rooms`, and `/#orchestration` in headless Chrome with route
  headings and browser runtime/console errors checked.

## Recommended Next Slices

1. Wait for remote CI on the current MR head before treating the cleanup as
   merge-ready.
