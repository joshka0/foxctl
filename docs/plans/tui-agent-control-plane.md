# TUI Agent Control Plane Plan

Status: Archived
Owner: Solo maintainer  
Last Updated: 2026-04-18

Archive note: the TypeScript TUI package described here now lives under
`archive/packages/tui-agent/`. Current terminal work targets the Go TUI in
`cmd/foxctl_tui/` and `internal/interfaces/tui/`.

## Goal

Build a new terminal-first operator control plane for `foxctl` that complements `packages/gui-agent` without creating a second agent runtime.

The TUI should be fast for day-to-day operator work:

1. inspect running agents and rooms
2. triage failures and blocked work
3. send bounded follow-ups
4. review orchestration state and evidence
5. stay usable inside tmux and multi-agent terminal workflows

## Decision

Create a new `packages/tui-agent/` package and treat `foxctl` as the only backend/runtime authority.

Do **not** treat `pi-mono` as a drop-in application shell. Use it as a design and interaction reference for:

1. TUI rendering patterns
2. overlays and modal workflows
3. terminal input ergonomics
4. chat/session presentation

The repo's existing `packages/tui/` package is considered legacy and should be archived rather than evolved in place.

## Why This Direction

`packages/gui-agent` already owns the modern product direction for the operator control plane:

- [`packages/gui-agent/src/App.tsx`](../../packages/gui-agent/src/App.tsx)
- [`packages/gui-agent/src/components/v2/OrchestrationBoardScreen.tsx`](../../packages/gui-agent/src/components/v2/OrchestrationBoardScreen.tsx)
- [`docs/plans/gui-agent-v2-rearchitecture.md`](gui-agent-v2-rearchitecture.md)
- [`docs/plans/gui-agent-improvement-roadmap.md`](gui-agent-improvement-roadmap.md)

Those files show a clear runtime / companion / events / orchestration model. Replacing that with pi's own runtime/session model would duplicate authority and make the product harder to reason about.

`pi-mono` is still useful as a basis for interaction design:

- terminal primitives from `pi-tui`
- coding-agent UX patterns from `pi-coding-agent`
- clean session/message UX from `pi-web-ui`

But `foxctl` should remain the owner of:

1. agent lifecycle
2. orchestration data
3. room/mailbox semantics
4. event/evidence contracts
5. persistence and identity

## Constraints

1. No dependency installation or vendoring work is required for the planning phase.
2. The new TUI should compose over existing `foxctl` APIs and event streams where possible.
3. The TUI must not invent parallel session, room, or agent identifiers.
4. The TUI must remain tmux-friendly and work cleanly in a terminal-only workflow.
5. Legacy `packages/tui/` should be archived instead of quietly continuing as an implied active surface.

## Non-Goals

Out of scope for the first implementation:

1. replacing `packages/gui-agent`
2. embedding pi's own runtime or provider stack as the primary backend
3. full parity with every historical TUI diagnostics screen
4. direct dependency on external `pi-mono` packages before the interaction seams are proven

## Product Shape

The new TUI should be an operator control plane, not a generic coding chat.

Primary surfaces:

1. `Runtime`
2. `Orchestration`
3. `Rooms`
4. `Activity`
5. `Agent Detail`
6. `Artifacts / Evidence`

Secondary overlays:

1. spawn agent
2. ask/send follow-up
3. room dispatch
4. logs/raw event detail
5. keyboard help

This mirrors the intent already captured in [`DESIGN.md`](../../DESIGN.md): summary-first, entity-first, raw detail on demand.

## Architecture

### 1. Backend Ownership

`foxctl` remains the system of record.

The TUI consumes:

1. typed HTTP/JSON read models via shared frontend client code
2. websocket/SSE activity streams where available
3. stable envelope/event payloads for evidence drill-down

### 2. Adapter Layer

Create a thin adapter inside `packages/tui-agent` that converts `foxctl` transport payloads into TUI view models.

Responsibilities:

1. workspace resolution
2. typed fetch and refresh
3. event projection for lists/detail panes
4. optimistic action states for spawn/stop/send/dispatch

Non-responsibilities:

1. business rules for orchestration
2. room ownership semantics
3. backend-derived identity generation

### 3. UI Layer

The TUI shell should be intentionally terminal-native:

1. pane-oriented layout
2. keyboard-first navigation
3. overlays for forms and drill-down
4. compact rows with expandable detail

Borrow from `pi-mono`:

1. overlay/dialog ergonomics
2. message queue and input semantics
3. terminal key handling expectations, especially tmux

Do not borrow:

1. pi's runtime state model
2. pi's session storage as source of truth
3. pi's tool/provider execution model

## Package Layout

Target package:

`packages/tui-agent/`

Suggested structure:

```text
packages/tui-agent/
  package.json
  tsconfig.json
  src/
    index.tsx
    App.tsx
    shell/
      AppFrame.tsx
      StatusBar.tsx
      CommandPalette.tsx
      HelpOverlay.tsx
    api/
      client.ts
      queries.ts
      events.ts
      projections.ts
    state/
      navigation.ts
      selection.ts
      workspaces.ts
      activity.ts
    views/
      RuntimeView.tsx
      OrchestrationView.tsx
      RoomsView.tsx
      ActivityView.tsx
      AgentDetailView.tsx
      EvidenceView.tsx
    overlays/
      SpawnAgentOverlay.tsx
      SendMessageOverlay.tsx
      EventDetailOverlay.tsx
      ArtifactOverlay.tsx
```

## Legacy Archive Plan

The current `packages/tui/` package should be archived before active development begins on the new control plane.

Archive steps:

1. capture any still-useful view ideas or API helpers from `packages/tui/`
2. move the package out of the active workspace path
3. mark it historical with a short archive note pointing to the new `tui-agent` plan
4. remove active root scripts that imply it is the preferred TUI surface

Recommended destination:

1. `archive/packages/tui-legacy/` if code should remain restorable
2. or a slimmer `docs/archive/` note if the code is not worth carrying forward

## Delivery Phases

### Phase 0: Archive Legacy TUI and Lock Scope

Deliverables:

1. archive `packages/tui/`
2. add a short rationale doc for the archive
3. create `packages/tui-agent/` skeleton
4. wire root workspace scripts for the new package

Definition of done:

1. there is one active terminal control-plane package
2. no root scripts or docs point new work toward the legacy TUI

### Phase 1: Shared Client and Shell

Deliverables:

1. shared typed API client for TUI reads/actions
2. shell layout with persistent nav, status bar, and overlay support
3. workspace selector
4. keyboard map and tmux-safe defaults

Key seams to reuse:

1. frontend contract shapes from [`packages/gui-agent/src/api/types.ts`](../../packages/gui-agent/src/api/types.ts)
2. request/envelope handling patterns from [`packages/gui-agent/src/api/client.ts`](../../packages/gui-agent/src/api/client.ts)

Definition of done:

1. operator can launch the TUI, switch workspace, and navigate stable top-level surfaces

### Phase 2: Runtime MVP

Deliverables:

1. runtime agent list
2. selected agent detail
3. spawn / stop / ask actions
4. basic room and conversation linkage display

Definition of done:

1. operator can answer "what is running?" and act on a selected agent without leaving the terminal

### Phase 3: Orchestration and Rooms

Deliverables:

1. orchestration board list/detail workflow
2. room directory and room message preview
3. dispatch/delegate actions
4. stable links between board card, room, and owning agent

Definition of done:

1. operator can answer "what is blocked?" and route follow-up work from the TUI

### Phase 4: Activity and Evidence

Deliverables:

1. summary-first activity view
2. event detail overlay with raw payload only on demand
3. ref-aware artifact/evidence panel
4. navigation between runtime entity and evidence view

Definition of done:

1. operator can answer "why is this broken?" from the terminal without dropping into raw logs first

### Phase 5: Polish and Convergence

Deliverables:

1. align terminology and actions with `gui-agent`
2. document keyboard workflow and tmux guidance
3. add targeted tests for projections, selection, and navigation
4. decide whether any pi-inspired abstractions are worth formalizing into shared local primitives

Definition of done:

1. `gui-agent` and `tui-agent` present the same control-plane concepts with different interaction styles, not different product models

## Verification Plan

For each phase:

1. `bun run --cwd packages/tui-agent typecheck`
2. add focused unit tests for projection/state helpers
3. manual tmux smoke test for navigation and overlays
4. confirm workspace switching and action routing hit the expected backend contract

Manual acceptance checklist for MVP:

1. launch TUI in a repo workspace
2. see runtime agent inventory
3. inspect one agent
4. send one bounded follow-up
5. inspect orchestration state
6. open one evidence or event detail panel

## Risks

1. Carrying over the legacy TUI's diagnostics-first information architecture would dilute the new control-plane focus.
2. Importing pi runtime assumptions too early would create a second authority for sessions and agent behavior.
3. Reusing GUI contracts naively without a TUI-specific projection layer will produce a cramped, low-signal interface.
4. tmux input behavior will regress unless keyboard handling is tested explicitly.

## Recommended First Slice

Implement the smallest useful path first:

1. archive legacy `packages/tui/`
2. scaffold `packages/tui-agent/`
3. build shell + workspace selector
4. ship `RuntimeView`
5. ship `AgentDetailView`
6. add spawn / stop / ask overlay

That slice is enough to validate whether the new TUI is a real operator surface rather than another diagnostics browser.
