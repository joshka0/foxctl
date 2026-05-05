# GUI Agent V2 Rearchitecture Plan

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-03-04

## Goal

Ship a v2-native GUI that is operationally useful now, and scales into turn/context/artifact-first workflows without legacy UI debt.

## Assumptions

1. We are doing a hard v2 cut in GUI behavior (no v1 UX fallback path in primary nav).
2. Existing backend APIs remain available while we add narrower v2 read models.
3. The initial v2 user journey should prioritize live runtime operations and companion work, not empty explorer pages.

## Findings From Live Review (5174)

Observed in live UI pass:

1. `Turns`, `Context`, `Artifacts` are present in top nav but mostly empty-state shells.
2. Agent information is duplicated between sidebar and main Runtime panel.
3. `Events` has high volume and low signal-to-noise for runtime triage.
4. `Companion` includes duplicated actor names/options and identity ambiguity.

Root issue: information architecture is conceptually v2, but execution surface is still split between placeholder explorers and legacy-heavy operational views.

## IA Decision (Locked)

Primary surfaces for v2 phase-in:

1. `Runtime` (default landing)
2. `Companion` (second primary)
3. `Events` (secondary, debugging/forensics)
4. `Turns`, `Context`, `Artifacts` remain available only when they have data, otherwise they route to Runtime/Companion guided CTAs.

This keeps the product honest: no dead-end top-level tabs.

## UX Flows (Must Work)

### Flow A: Operator triage

1. Open GUI on Runtime.
2. See active/error agents first.
3. Open details or chat in one click.
4. Resolve stuck/error agent quickly.

### Flow B: Evidence-first context debugging

1. Start in Runtime or Companion.
2. Jump to related event trace.
3. From trace, open turn/slice/episode refs.
4. Return back to originating conversation/agent.

### Flow C: Companion as working console

1. Select conversation with unambiguous identity.
2. Inspect linked agent/runtime config.
3. Ask follow-up and view cited context/memory.

## Screen-Level Spec and Acceptance Criteria

### Runtime

Intent: control plane for active work.

Acceptance criteria:

1. Runtime renders active/error agents first, stopped hidden by default.
2. Sidebar does not duplicate full runtime cards (summary-only in sidebar).
3. Actions (`Chat`, `Details`, `Stop/Resume`) are label-clear and keyboard-focusable.
4. Runtime empty state has one obvious CTA: `Spawn agent`.

### Companion

Intent: conversational workbench linked to runtime identities.

Acceptance criteria:

1. Agent dropdown/options are deduplicated by stable identity (`agent.id`).
2. Conversation rows show disambiguation metadata (`name`, short `id`, status, recency).
3. “No conversation selected” state offers guided next steps (new conversation or link existing agent).
4. Companion panel can deep-link into Runtime agent detail and back.

### Events

Intent: incident/debug timeline, not a raw firehose.

Acceptance criteria:

1. Default event mode is summary-first (recent errors, high-latency ops, active traces).
2. Raw payload view is behind explicit expand actions.
3. Filters are deterministic and persist for session (`component`, `workspace`, `errors-only`, search).
4. Event row can navigate to related runtime/session/turn when refs exist.

### Turns / Context / Artifacts

Intent: evidence explorers.

Acceptance criteria:

1. If no relevant data exists, show guided CTA with explicit next action and source surface.
2. If data exists, each row includes stable refs and source trace/session metadata.
3. Click-through from ref opens concrete evidence target (turn/slice/artifact) or explicit “not found”.
4. No generic placeholder copy like “coming soon”.

## Component-Level Change List

### Keep (with small updates)

1. `packages/gui-agent/src/components/layout/AppShell.tsx`
2. `packages/gui-agent/src/components/layout/AgentSidebar.tsx`
3. `packages/gui-agent/src/components/agents/AgentList.tsx`
4. `packages/gui-agent/src/components/conversations/ConversationsList.tsx`
5. `packages/gui-agent/src/components/actions/LogsViewer.tsx`

### Refactor

1. `packages/gui-agent/src/App.tsx`
   - Add surface gating logic and no-dead-end routing behavior.
2. `packages/gui-agent/src/stores/viewStore.ts`
   - Remove legacy alias behavior from primary flow; keep strict v2 views.
3. `packages/gui-agent/src/components/layout/AgentSidebar.tsx`
   - Convert runtime agent list to compact summary entries (not full duplicate nav burden).
4. `packages/gui-agent/src/components/agents/AgentList.tsx`
   - Promote triage actions and reduce card density.
5. `packages/gui-agent/src/components/conversations/ConversationsList.tsx`
   - Deduplicate linked-agent options by canonical ID.
   - Add runtime identity badges and clearer conversation grouping.
6. `packages/gui-agent/src/components/actions/LogsViewer.tsx`
   - Introduce summary-first mode with expandable raw events.
7. `packages/gui-agent/src/components/v2/V2Explorers.tsx`
   - Replace pure empty states with guided data-aware CTAs and real ref drilldowns.

### Add

1. `packages/gui-agent/src/stores/runtimeStore.ts`
   - Runtime-specific derived selectors and summary metrics.
2. `packages/gui-agent/src/stores/eventProjectionStore.ts`
   - Bounded ring + indexes (`trace_id`, `session_id`, `operation`, `ref`).
3. `packages/gui-agent/src/components/v2/RuntimeSummaryPanel.tsx`
4. `packages/gui-agent/src/components/v2/EventTraceDrawer.tsx`
5. `packages/gui-agent/src/components/v2/RefDrilldownPanel.tsx`

### Remove (after cutover)

1. Legacy hash aliases and dead routes in `viewStore` that imply old surfaces.
2. Redundant UI paths that expose same action in multiple inconsistent places.

## Data/Contract Requirements (UI-facing)

Required event payload stability:

1. `trace_id`, `session_id`, `operation`, `status`, `ts`
2. refs in `data`: `turn_refs`, `slice_refs`, `episode_refs`, `artifact_refs`
3. context metadata in `data` where present: `artifact_search_path`, `artifact_hit_count`, `artifact_search_error`

Contract rule:

1. UI must not parse free-form strings for core behavior when typed fields exist.

## Phased Delivery Plan

### Phase 1: Runtime and Companion cleanup (high leverage)

Scope:

1. Dedupe identities and options in Companion.
2. Reduce Runtime duplication between sidebar and main panel.
3. Improve action clarity and triage ordering.

Definition of done:

1. Runtime + Companion usable without visiting other tabs.
2. No duplicated agent entries in companion link selectors.

### Phase 2: Events signal quality

Scope:

1. Summary-first event mode.
2. Trace-centric drilldown and stable filter persistence.

Definition of done:

1. Operator can answer “what is broken now?” in less than 10 seconds from Events view.

### Phase 3: Explorer activation

Scope:

1. Turns/Context/Artifacts become evidence-driven explorers.
2. Empty states replaced with guided transitions and ref-aware controls.

Definition of done:

1. At least one end-to-end path exists: event -> trace -> turn/slice/artifact evidence.

### Phase 4: Hard cut and cleanup

Scope:

1. Remove obsolete routes/components and duplicate patterns.
2. Finalize docs/tests and acceptance checklist.

Definition of done:

1. Single v2 UX path only.
2. No dead-end primary navigation screens.

## Test Plan

1. Unit tests for store selectors and dedupe logic.
2. Component tests for Runtime/Companion/Events interaction paths.
3. Contract tests for event payload projection.
4. Manual smoke checklist:
   - Spawn agent
   - Open companion conversation
   - Trigger event
   - Navigate from event to evidence
5. `make check-doc-links` for docs updates.

## Risks and Mitigations

1. Risk: over-building explorers before data quality is ready.
   - Mitigation: gate explorers behind data-availability checks and guided fallback.
2. Risk: sidebar/runtime duplication returns via incremental edits.
   - Mitigation: define canonical ownership per interaction (sidebar summary, runtime detail).
3. Risk: event stream volume regresses UX.
   - Mitigation: bounded projection store + summary-first default mode.

## References

1. `docs/plans/v2-implementation-todo.md`
2. `docs/general/companion-memory.md`
3. `docs/general/foxcular-events.md`
4. `internal/runtime/observability/sse_bridge.go`
5. `packages/gui-agent/src/App.tsx`
6. `packages/gui-agent/src/stores/viewStore.ts`
7. `packages/gui-agent/src/components/layout/AgentSidebar.tsx`
8. `packages/gui-agent/src/components/agents/AgentList.tsx`
9. `packages/gui-agent/src/components/conversations/ConversationsList.tsx`
10. `packages/gui-agent/src/components/actions/LogsViewer.tsx`
11. `packages/gui-agent/src/components/v2/V2Explorers.tsx`
