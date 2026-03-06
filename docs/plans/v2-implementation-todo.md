# V2 Implementation Todo Tracker (Active)

Status: Active
Owner: Solo maintainer
Last Updated: 2026-03-05

## History Archive

Historical migration logs and pre-hard-cut planning moved to:

- `docs/plans/archive/v2-implementation-todo-archive-2026-03-02.md`
- `docs/plans/archive/v2-greenfield-bootstrap-archive-2026-03-02.md`

This active tracker keeps only current v2 execution items.

## Objective

Maintain and extend the v2 runtime while finishing remaining hybrid cutover work:

- one command orchestration path per surface
- one tool execution path
- append-only event sourcing + projections
- non-blocking enrichers and context assembly
- deterministic, referenceable context outputs

Primary docs:

- `docs/spec/v2_greenfield_bootstrap.md`
- `docs/spec/v2_repo_rules_and_skills.md`
- `docs/spec/v2_symphony_kanban_orchestration.md`
- `docs/plans/v2-greenfield-bootstrap.md`
- `docs/plans/gui-agent-v2-rearchitecture.md`
- `docs/plans/v2-symphony-kanban-implementation.md`

## Current State

- [x] Wave 1 foundations complete
- [x] Wave 2 production cutover complete
- [x] Wave 3 retrieval + resynthesis complete
- [x] Wave 4 episode/working-context/narrative complete
- [ ] Remaining legacy command-routing cleanup still exists for some agent CLI surfaces

## Completed Milestones

- [x] PR-11: initial direct v2 command-surface cutover work (CLI/API/daemon handlers)
- [x] PR-12: libsql turn/artifact stores
- [x] PR-13: event-to-enricher wiring
- [x] PR-14: hierarchical temporal context retrieval
- [x] PR-15: companion layered context integration
- [x] PR-16: decommission cleanup gates
- [x] PR-17: semantic artifact retrieval contract + implementation
- [x] PR-18: vector-path validation and quality guardrails
- [x] PR-19: source conversation resynthesis
- [x] PR-25: episode layer + compiler enricher
- [x] PR-26: WorkingContext retrieval gate
- [x] PR-27: evidence-cited narrative artifacts
- [x] PR-32: embedding stack unification

## Active Next Batch (Wave 5)

- [ ] PR-33: quality gate unification for v2 command + retrieval outputs
  - [ ] lock a single Definition of Done contract for CLI/API/daemon parity, context refs, and event metadata
  - [ ] wire deterministic committed-loop checks (`quality-gate` + code review gate)
  - [ ] add fail-fast CI checks for missing provenance fields on derived artifacts
- [ ] PR-34: gui-agent/runtime contract hardening
  - [ ] depends on PR-33 (single DoD + provenance baseline)
  - [ ] align context bundle/ref metadata with GUI activity/event types
  - [ ] verify SSE forwarding scope only includes intended v2 runtime/context events
  - [ ] add regression coverage for stable ref rendering and trace correlation fields
- [ ] PR-35: retrieval policy and observability tightening
  - [ ] enforce retrieval SLO alerts (latency, hit rate, fallback ratio)
  - [ ] add deterministic drift tests for semantic ranking against fixed corpora
  - [ ] document production thresholds and incident playbook in observability docs

## Planned Following Batch (Wave 6: GUI v2 hard-cut)

- [ ] PR-36: GUI information architecture hard-cut to v2 runtime surfaces
  - [ ] depends on PR-34 (backend contract baseline)
  - [ ] replace placeholder-heavy view routing with Runtime/Turns/Context/Artifacts/Events/Companion flows
  - [ ] remove primary dependence on legacy operations-page navigation patterns (no demote-only path)
- [ ] PR-37: GUI runtime/events contract hardening
  - [ ] depends on PR-33 + PR-36
  - [ ] frontend contract ownership only (PR-34 keeps backend SSE/provenance ownership)
  - [ ] align SSE payload projection with trace/session/turn-first navigation
  - [ ] stabilize ref metadata usage (`turn_refs`, `slice_refs`, `episode_refs`, `artifact_refs`) in UI models
- [ ] PR-38: Turn/Iteration/ToolCall inspector
  - [ ] depends on PR-37
  - [ ] add deterministic lineage drill-down for turn -> iteration -> tool call
  - [ ] expose trace/span metadata in inspector and tests
- [ ] PR-39: Context and artifact explorer
  - [ ] depends on PR-37
  - [ ] render layered context bundle metadata (search path, vector capability, ref counts)
  - [ ] add ref click-through from semantic artifact hits into evidence slices
- [ ] PR-40: Companion temporal pyramid UI
  - [ ] depends on PR-39
  - [ ] surface hours/days/weeks/months views with episode/narrative summaries
  - [ ] preserve evidence-linked navigation from summary to raw turns
- [ ] PR-41: GUI v2 hard-cut cleanup and quality gate
  - [ ] depends on PR-38 + PR-39 + PR-40
  - [ ] remove deprecated v1-only GUI code paths
  - [ ] add end-to-end smoke checks for spawn/ask/events/context drill-down

## Planned Next Batch (Wave 7: Symphony Ingress + Kanban Board)

- [x] PR-42: workflow frontmatter loader + typed orchestration config
  - [x] parse/validate `WORKFLOW.md` frontmatter without breaking DAG YAML workflows
  - [x] enforce last-known-good config retention on invalid reload
- [x] PR-43: v2 orchestration core + service contract
  - [x] add deterministic state machine + lane precedence mapper
  - [x] ensure single spawn path through `SpawnService.Spawn`
- [x] PR-44: runtime orchestration component under supervisor host
  - [x] add scheduler/reconcile/retry loop components with `Run(ctx)` lifecycle
  - [x] keep turn execution non-blocking relative to orchestration loop
- [x] PR-45: libsql orchestration projection + board read model
  - [x] add append-derived schema/store/replay path
  - [x] implement bounded board queries + deterministic lane projection
- [x] PR-46: orchestration command/API surface
  - [x] expose `orchestration/board-get`, `orchestration/board-card-get`, `orchestration/refresh`
  - [x] enforce request-id idempotency and artifact fallback for large board payloads
- [ ] PR-47: GUI Kanban runtime integration
  - [ ] depends on PR-41 (GUI v2 hard-cut baseline)
  - [x] render projection-backed lanes/cards in Runtime
  - [x] route card actions through orchestration commands only
- [x] PR-48: observability + quality gate hardening
  - [x] align SSE/wide-events with orchestration metadata
  - [x] close with end-to-end replay/projection/UI smoke coverage
- [x] PR-49: GUI v2 surface activation + focused events bridge
  - [x] activate strict v2 surface routing (`runtime/turns/context/artifacts/events/companion`)
  - [x] render activity-backed prebuilt traces in Turns/Context/Artifacts
  - [x] add explorer->events focus bridge and safe focus filtering
  - [x] merge initial logs snapshot with live SSE stream on bootstrap
  - [x] default OpenRouter model to `google/gemini-3.1-flash-lite-preview` while retaining other options
- [x] PR-50: Events signal quality + persisted filter projection
  - [x] add session-persisted events filter store for deterministic triage state
  - [x] switch Events to summary-first mode with explicit raw-events toggle
  - [x] add recent-errors/high-latency/active-traces summary cards
  - [x] add event-row navigation actions to Runtime and inferred evidence surfaces
- [x] PR-51: Events deep-dive panels
  - [x] add trace-focused EventTraceDrawer panel
  - [x] add structured RefDrilldownPanel with grouped refs
  - [x] wire event selection and trace selection from LogsViewer
- [x] PR-52: Guided empty-state routing
  - [x] add Runtime/Companion CTAs for empty Turns/Context/Artifacts explorers
  - [x] add Companion empty-state “Open Runtime” path (with optional agent preselect)
  - [x] remove dead-end empty-state flow from active primary surfaces
- [x] PR-53: View routing hard-cut cleanup
  - [x] remove legacy hash alias mapping from `viewStore`
  - [x] keep strict v2 hash set at PR-53 completion (`runtime|turns|context|artifacts|events|companion`)
  - [x] default invalid hashes to `runtime`
- [x] PR-54: Dedicated orchestration screen
  - [x] move orchestration board panel to its own view
  - [x] remove embedded board panel from Runtime agent list
  - [x] wire sidebar/app-shell routing for `orchestration` screen (valid set now includes `orchestration`)

## Decisions (Locked)

- [x] orchestration and v2 event/projection flows are the preferred direction
- [x] legacy command routing should be removed deliberately, not left ambiguous
- [x] turn completion never blocks on enrichers/maintenance
- [x] lineage shape is `Turn -> Iteration -> ToolCall` with trace metadata
- [x] context retrieval is stable-ref-first

## Open Questions

- [ ] should narrative refresh cadence be adaptive by session activity level?
- [ ] should WorkingContext fallback ladder be configurable per profile?
- [ ] how strict should vector/fallback overlap gates be for low-volume projects?

## Completion Gate (Required Before Marking Done)

For each completed slice:

1. Add a subagent review note.
2. Run tests for touched packages.
3. Run docs link checks if markdown changed.
4. Update this tracker date and checkboxes.

Subagent review template:

```text
Subagent Review
- reviewer: <id>
- scope: <files/slice>
- findings: <summary|none>
- decision: <approved|approved-with-known-risks>
```

## Resume Protocol

1. Start from the first unchecked item in `Active Next Batch`.
2. Keep scope to one PR slice at a time.
3. Before pausing, update:
   - `Last Updated`
   - completed checkboxes
   - one subagent review note (with scope/findings/decision)

## Subagent Review Notes

```text
Subagent Review
- reviewer: gui-v2-plan-review-027693
- scope: docs/plans/gui-agent-v2-rearchitecture.md, docs/plans/v2-implementation-todo.md
- findings: sequencing/ownership wording gaps identified and patched
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: docs/plans/v2-symphony-kanban-implementation.md, docs/plans/v2-implementation-todo.md, docs/plans/README.md
- findings: sequencing and single-path ownership gaps identified and patched
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/v2/core/orchestration/*, internal/v2/core/services/interfaces.go, internal/v2/services/orchestration_service.go, internal/v2/services/orchestration_service_test.go
- findings: initial precedence and metadata issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/v2/runtime/orchestration/*, internal/v2/services/long_lived_run_service.go, internal/v2/services/long_lived_run_service_test.go
- findings: initial lifecycle/retry queue issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/v2/adapters/libsql/orchestration/*, internal/v2/adapters/libsql/projections/replay.go, internal/v2/adapters/libsql/projections/replay_test.go
- findings: initial idempotency/merge/lane-authority/event-time issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/web/api/orchestration.go, internal/web/api/orchestration_test.go, internal/web/server.go
- findings: initial refresh no-op and coalescing race identified; patched with replay-backed refresh and concurrency-safe queue semantics
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/api/types.ts, packages/gui-agent/src/api/client.ts, packages/gui-agent/src/stores/orchestrationBoardStore.ts, packages/gui-agent/src/components/v2/RuntimeSummaryPanel.tsx, packages/gui-agent/src/components/agents/AgentList.tsx
- findings: initial Runtime panel retry loop and non-2xx envelope parsing issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/observability/sse_bridge.go, internal/observability/sse_bridge_test.go, internal/web/api/orchestration.go, internal/web/api/orchestration_test.go, docs/observability/wide-events.md, docs/spec/v2_symphony_kanban_orchestration.md
- findings: initial docs contract ambiguity and missing board-get/refresh SSE regression coverage identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: internal/web/api/orchestration_test.go (end-to-end replay/projection lane transitions)
- findings: none
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/{App.tsx,components/layout/AppShell.tsx,components/layout/AgentSidebar.tsx,components/actions/LogsViewer.tsx,components/v2/V2Explorers.tsx,components/conversations/ConversationsList.tsx,components/agents/AgentDetailView.tsx,components/layout/SpawnAgentPanel.tsx,stores/{viewStore.ts,activityFocusStore.ts},hooks/useActivityStream.ts}, internal/providers/llm/{defaults.go,providers.go}, cmd/agentctl/cmd/web.go
- findings: initial startup event-loss, focus-filter guard, and dev-cors message mismatch identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/components/actions/LogsViewer.tsx, packages/gui-agent/src/stores/{eventProjectionStore.ts,activityFocusStore.ts}
- findings: initial summary/filter consistency, storage write safety, and focus selector guard issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/components/v2/{EventTraceDrawer.tsx,RefDrilldownPanel.tsx}, packages/gui-agent/src/components/actions/LogsViewer.tsx
- findings: initial selection-sync, trace-source consistency, and ref-navigation gating issues identified; patched and re-reviewed with no remaining substantive findings
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/components/v2/V2Explorers.tsx, packages/gui-agent/src/components/conversations/ConversationsList.tsx, docs/plans/{v2-symphony-kanban-implementation.md,v2-implementation-todo.md}
- findings: none
- decision: approved

Subagent Review
- reviewer: 019cbae8-dfb7-7af3-885c-f8ac07504966
- scope: packages/gui-agent/src/stores/viewStore.ts, docs/plans/{v2-symphony-kanban-implementation.md,v2-implementation-todo.md}
- findings: none
- decision: approved
```
