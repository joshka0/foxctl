# V2 Symphony + Kanban Implementation Plan

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-03-05

## Objective

Implement `docs/spec/v2_symphony_kanban_orchestration.md` as a v2-native execution slice:

1. Symphony-style issue intake as ingress/scheduler (not a second orchestrator).
2. Single spawn path through existing v2 service + overseer policy path.
3. Deterministic Kanban read model backed by append-derived projection.
4. UI board that is operationally useful for runtime triage.

## Non-Goals

1. No reintroduction of v1 fallback/shadow logic for supported v2 command surfaces.
2. No scheduler-owned policy decisions.
3. No direct DB mutation from UI.
4. No replacement of existing YAML DAG workflow execution (`foxctl workflow run`).

## Locked Constraints

1. Envelope contract remains Protocol v1 (`version/status/command/data/meta/error`).
2. Command names use `namespace/verb` hyphen format (for example `orchestration/board-get`).
3. Mutating orchestration commands require `request_id` and idempotency on `(command, scope_id, request_id)`.
4. Large payload rule applies (artifactize above threshold with `data.summary` + `data.artifact`).
5. Runtime components must use `Run(ctx)` + bounded queues + supervisor host wiring.

## Workstream Breakdown (PR Sequence)

## PR-42: Workflow Frontmatter + Typed Orchestration Config

Goal:

1. Add `WORKFLOW.md` frontmatter parser/validator for orchestration settings without touching DAG workflow executor behavior.

Primary files:

1. `internal/runtime/orchestration/workflow/frontmatter/types.go` (new)
2. `internal/runtime/orchestration/workflow/frontmatter/parser.go` (new)
3. `internal/runtime/orchestration/workflow/frontmatter/parser_test.go` (new)
4. `internal/runtime/orchestration/workflow/frontmatter/validate.go` (new)
5. `internal/runtime/orchestration/workflow/frontmatter/validate_test.go` (new)
6. `internal/runtime/orchestration/workflow/loader.go` (wire coexistence rule only, no DAG behavior break)
7. `docs/spec/v2_symphony_kanban_orchestration.md` (if minor clarifications needed)

Acceptance:

1. Parser accepts frontmatter + markdown body split.
2. Invalid reload keeps last known good config.
3. Existing `foxctl workflow run` YAML tests stay green.

Tests:

1. `go test ./internal/runtime/orchestration/workflow/...`

## PR-43: Core Orchestration Domain + Service Contract

Goal:

1. Introduce canonical orchestration domain model and service boundary for dispatch/retry/reconcile commands.

Primary files:

1. `internal/v2/core/orchestration/types.go` (new)
2. `internal/v2/core/orchestration/state_machine.go` (new)
3. `internal/v2/core/orchestration/state_machine_test.go` (new)
4. `internal/v2/core/orchestration/lanes.go` (new; precedence mapping)
5. `internal/v2/core/orchestration/lanes_test.go` (new)
6. `internal/v2/core/services/interfaces.go` (add orchestration service interface)
7. `internal/v2/services/orchestration_service.go` (new)
8. `internal/v2/services/orchestration_service_test.go` (new)

Acceptance:

1. Internal states and lane mapping are total and deterministic.
2. Spawn handoff uses `SpawnService.Spawn` only.
3. Denial reasons remain explicit (`last_outcome`, `policy_status`, `denial_reason`).

Tests:

1. `go test ./internal/v2/core/orchestration ./internal/v2/services`

## PR-44: Runtime Orchestration Component (Scheduler + Reconcile Loop)

Goal:

1. Add long-lived orchestration runtime component that runs under v2 supervisor host and emits orchestration events.

Primary files:

1. `internal/v2/runtime/orchestration/component.go` (new, `Run(ctx)` loop)
2. `internal/v2/runtime/orchestration/scheduler.go` (new)
3. `internal/v2/runtime/orchestration/reconcile.go` (new)
4. `internal/v2/runtime/orchestration/retry_queue.go` (new)
5. `internal/v2/runtime/orchestration/component_test.go` (new)
6. `internal/v2/runtime/orchestration/reconcile_test.go` (new)
7. `internal/v2/services/long_lived_run_service.go` (register orchestration component in specs)
8. `internal/v2/services/long_lived_run_service_test.go` (update expectations)

Acceptance:

1. Tick loop runs without blocking run pipeline.
2. Reconciliation still runs when dispatch preflight fails.
3. Backoff behavior is deterministic and bounded.
4. `internal/v2/runtime/orchestration/*` does not spawn sessions or write projections directly; scheduler/reconciler only call `OrchestrationService`.

Tests:

1. `go test ./internal/v2/runtime/orchestration ./internal/v2/services`

## PR-45: LibSQL Orchestration Projection + Board Read Model

Goal:

1. Add append-derived orchestration projection storage and board query model in libsql.

Primary files:

1. `internal/v2/adapters/libsql/orchestration/schema.go` (new)
2. `internal/v2/adapters/libsql/orchestration/store.go` (new)
3. `internal/v2/adapters/libsql/orchestration/replay.go` (new)
4. `internal/v2/adapters/libsql/orchestration/store_test.go` (new)
5. `internal/v2/adapters/libsql/orchestration/replay_test.go` (new)
6. `internal/v2/adapters/libsql/projections/replay.go` (delegate orchestration event apply path to orchestration projector)

Acceptance:

1. Board lanes computed by precedence table in spec section 8.3.
2. Projection idempotent by event id and request id.
3. Query supports bounded reads (`limit`, `cursor`, lane filter).
4. One projector apply path is preserved: `projections/replay` delegates to orchestration projector and does not duplicate lane logic.

Tests:

1. `go test ./internal/v2/adapters/libsql/orchestration ./internal/v2/adapters/libsql/projections`

## PR-46: Orchestration API/Command Surface

Goal:

1. Expose orchestration board and control commands with strict envelope output and idempotency requirements.

Primary files:

1. `internal/interfaces/web/api/orchestration.go` (new handlers)
2. `internal/interfaces/web/server.go` (route registration)
3. `internal/v2/services/orchestration_service.go` (command methods)
4. `internal/v2/services/orchestration_service_test.go` (idempotency + error cases)

Endpoints/commands:

1. `orchestration/board-get`
2. `orchestration/board-card-get`
3. `orchestration/refresh`

Acceptance:

1. `status: ok` responses use `error.code=null` + `error.message=null`.
2. Large board payloads artifactize correctly.
3. Mutating actions reject missing `request_id`.
4. Web handlers call v2 orchestration service only and do not perform domain orchestration directly.

Tests:

1. `go test ./internal/interfaces/web/... ./internal/v2/services`

## PR-47: GUI Kanban Surface (Runtime-First Integration)

Goal:

1. Replace placeholder-heavy runtime list behavior with actionable orchestration board lanes and card operations.

Primary files:

1. `packages/gui-agent/src/api/types.ts` (orchestration board types)
2. `packages/gui-agent/src/api/client.ts` (board API client)
3. `packages/gui-agent/src/stores/orchestrationBoardStore.ts` (new)
4. `packages/gui-agent/src/components/v2/RuntimeSummaryPanel.tsx` (or new Kanban panel)
5. `packages/gui-agent/src/components/agents/AgentList.tsx` (integrate board cards/actions)
6. `packages/gui-agent/src/App.tsx` (route/surface wiring)
7. `packages/gui-agent/src/stores/viewStore.ts` (surface behavior updates)

Acceptance:

1. Runtime default view shows non-empty, deterministic lanes.
2. Card actions trigger orchestration commands (no direct mutations).
3. Navigation still supports deep-link to events/turn context.

Tests:

1. `pnpm -C packages/gui-agent test` (or project-standard GUI test command)

## PR-48: Observability + Quality Gate + E2E Hardening

Goal:

1. Finalize tracing/telemetry and verify full orchestration path with deterministic tests and manual smoke checks.

Primary files:

1. `internal/runtime/observability/sse_bridge.go` (orchestration operation pass-through + curated fields)
2. `docs/observability/wide-events.md` (orchestration event fields)
3. `docs/spec/v2_symphony_kanban_orchestration.md` (final contract alignment)
4. `docs/plans/v2-implementation-todo.md` (close checklist items)

Acceptance:

1. SSE includes usable orchestration card metadata (`lane`, `last_outcome`, `request_id`, `trace_id`).
2. E2E flow passes: issue eligible -> claimed -> running/retry/review/done projection updates.
3. Quality gate/coder review loops return no unresolved high-severity findings.

Tests and checks:

1. `go test ./...`
2. `make check-doc-links`
3. project quality gate + committed review loop

## PR-49: GUI V2 Surface Activation + Focused Event Drilldown

Goal:

1. Make Runtime/Companion/Events immediately useful while activating Turns/Context/Artifacts with prebuilt trace views from real activity data.

Primary files:

1. `packages/gui-agent/src/App.tsx` (strict v2 surface routing)
2. `packages/gui-agent/src/stores/viewStore.ts` (v2 view normalization + hash behavior)
3. `packages/gui-agent/src/components/layout/AgentSidebar.tsx` (summary-first surfaces + runtime-focused agent controls)
4. `packages/gui-agent/src/components/actions/LogsViewer.tsx` (focused trace/session filtering + return navigation)
5. `packages/gui-agent/src/components/v2/V2Explorers.tsx` (prebuilt turn/context/artifact traces)
6. `packages/gui-agent/src/stores/activityFocusStore.ts` (cross-surface activity focus state)
7. `packages/gui-agent/src/hooks/useActivityStream.ts` (non-blocking snapshot bootstrap merged with live SSE)
8. `packages/gui-agent/src/components/conversations/ConversationsList.tsx` (companion identity dedupe/labeling)
9. `packages/gui-agent/src/components/agents/AgentDetailView.tsx` and `packages/gui-agent/src/components/layout/SpawnAgentPanel.tsx` (companion route alignment)
10. `internal/providers/llm/defaults.go`, `internal/providers/llm/providers.go`, `packages/gui-agent/src/components/agents/spawnFormConstants.ts` (OpenRouter default model update)

Acceptance:

1. Primary surfaces are `runtime`, `turns`, `context`, `artifacts`, `events`, `companion` with deterministic hash mapping.
2. Turns/Context/Artifacts render non-placeholder, activity-backed trace summaries when events exist.
3. Explorer-to-Events drilldown uses trace/session focus and can navigate back to source surface.
4. Activity stream bootstrap does not clobber live SSE events during initial load.
5. OpenRouter default model is `google/gemini-3.1-flash-lite-preview` while retaining other model options.

Tests:

1. `pnpm -C packages/gui-agent build`
2. `go test ./cmd/foxctl/cmd ./internal/providers/llm ./internal/runtime/actor`

## PR-50: Events Signal Quality + Session-Persisted Filters

Goal:

1. Make Events operational by default with summary-first triage, persisted filters, and direct navigation from events back into runtime/evidence surfaces.

Primary files:

1. `packages/gui-agent/src/stores/eventProjectionStore.ts` (session-persisted event filter/projection state)
2. `packages/gui-agent/src/components/actions/LogsViewer.tsx` (summary-first view, active-trace/slow-op/recent-error cards, raw-row toggle, event navigation actions)
3. `packages/gui-agent/src/stores/activityFocusStore.ts` (allow `events` as focus source surface)

Acceptance:

1. Events opens in summary-first mode and raw rows are an explicit opt-in toggle.
2. Filters (`errorsOnly`, hidden commands, component, workspace, search) persist for browser session.
3. Event rows expose direct navigation actions to Runtime and inferred evidence surfaces (Turns/Context/Artifacts).
4. Focus state can round-trip between Events and source surfaces.

Tests:

1. `pnpm -C packages/gui-agent build`

## PR-51: Event Trace Drawer + Ref Drilldown Panels

Goal:

1. Add deeper trace/ref debugging from Events without leaving the v2 runtime workflow.

Primary files:

1. `packages/gui-agent/src/components/v2/EventTraceDrawer.tsx` (trace-scoped event timeline panel)
2. `packages/gui-agent/src/components/v2/RefDrilldownPanel.tsx` (structured refs with direct surface routing)
3. `packages/gui-agent/src/components/actions/LogsViewer.tsx` (selection wiring and panel integration)

Acceptance:

1. Events can open a trace drawer from active-trace summary rows.
2. Selecting an event row exposes a ref drilldown panel with grouped refs.
3. Ref drilldown actions route directly to `turns`, `context`, or `artifacts` via existing focus bridge.

Tests:

1. `pnpm -C packages/gui-agent build`

## PR-52: Guided Empty-State Routing for Companion and Explorers

Goal:

1. Remove remaining dead-end empty states by adding guided transitions to the primary v2 surfaces.

Primary files:

1. `packages/gui-agent/src/components/v2/V2Explorers.tsx` (guided CTAs for empty Turns/Context/Artifacts)
2. `packages/gui-agent/src/components/conversations/ConversationsList.tsx` (Companion empty-state route to Runtime)

Acceptance:

1. Turns/Context/Artifacts empty states offer explicit next-action buttons to Runtime/Companion.
2. Companion empty state offers both “New Conversation” and “Open Runtime”.
3. No primary surface leaves user on a dead-end placeholder without a clear route.

Tests:

1. `pnpm -C packages/gui-agent build`

## PR-53: View Routing Hard-Cut Cleanup

Goal:

1. Remove remaining legacy hash aliases from GUI routing so navigation is strictly v2-native.

Primary files:

1. `packages/gui-agent/src/stores/viewStore.ts` (strict view normalization; invalid hash -> runtime)

Acceptance:

1. At PR-53 completion, only `runtime`, `turns`, `context`, `artifacts`, `events`, `companion` were valid view hashes (later extended by PR-54 to include `orchestration`).
2. Unknown or legacy hashes no longer map to hidden aliases and fall back to `runtime`.
3. Existing v2 route behavior remains stable.

Tests:

1. `pnpm -C packages/gui-agent build`

## PR-54: Dedicated Orchestration Screen

Goal:

1. Move the orchestration board out of Runtime list density and into its own top-level screen.

Primary files:

1. `packages/gui-agent/src/components/v2/OrchestrationBoardScreen.tsx`
2. `packages/gui-agent/src/components/agents/AgentList.tsx` (remove embedded board panel)
3. `packages/gui-agent/src/stores/viewStore.ts` (add `orchestration` view)
4. `packages/gui-agent/src/components/layout/AgentSidebar.tsx` (add nav entry)
5. `packages/gui-agent/src/components/layout/AppShell.tsx` and `packages/gui-agent/src/App.tsx` (routing/title wiring)

Acceptance:

1. Runtime focuses on agent triage only.
2. Orchestration board is available as a separate screen from sidebar.
3. Existing board functionality is unchanged, only relocated.

Tests:

1. `pnpm -C packages/gui-agent build`

## Cross-PR Contract Checklist

1. Single orchestration path preserved for spawn/dispatch decisions.
2. No direct session spawn from scheduler/UI.
3. Projection and API are append-derived and idempotent.
4. Lane mapping behavior remains deterministic across replay and live events.
5. UI actions remain command-driven with request IDs.

## Rollout Order and Risk Controls

1. Ship backend contract first (PR-42 through PR-46), then GUI (PR-47, after GUI hard-cut baseline PR-41), then hardening (PR-48).
2. Keep orchestration loop feature-flagged until projection and API tests are green.
3. Use bounded queue configs for orchestration events from day one.
4. Block merge if envelope contract, idempotency semantics, or lane determinism tests regress.

## Manual Smoke Script (Post PR-47)

1. Start runtime with orchestration enabled.
2. Trigger one eligible issue and verify `Todo -> Claimed -> Running` lane transitions.
3. Force one failure and verify `RetryQueued` with due time and reason.
4. Trigger policy denial and verify `Blocked` with denial reason.
5. Move issue to handoff/terminal state and verify `Review`/`Done` projection.
6. Validate GUI card actions use command path and include request IDs.

## Definition of Done

1. `docs/spec/v2_symphony_kanban_orchestration.md` is fully implemented by code paths in v2 runtime/services/adapters.
2. Kanban board is not placeholder data; it is projection-backed and actionable.
3. All touched package tests are green.
4. Docs links are green.
5. Subagent review note is recorded in `docs/plans/v2-implementation-todo.md`.
