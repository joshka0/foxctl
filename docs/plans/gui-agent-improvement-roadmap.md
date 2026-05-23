# GUI Agent Improvement Roadmap

Status: Draft  
Owner: Solo maintainer  
Last Updated: 2026-03-12

## Goal

Turn `packages/gui-agent` into a coherent operator control plane instead of a collection of overlapping runtime tools.

This document captures the converged findings from:

- local code review
- independent subagent reviews on control-plane IA and navigation
- independent subagent review on conversation/runtime model overlap

It is intentionally more product- and surface-focused than `docs/plans/gui-agent-v2-rearchitecture.md`, which already describes the desired v2 direction at a higher level.

## Scope

In scope:

- information architecture and surface ownership
- entity-aware navigation
- Runtime / Companion / Events / Rooms / Orchestration workflow coherence
- conversation/session/agent-link model cleanup
- event/evidence trust model
- frontend contract normalization and test coverage

Out of scope:

- full UI rewrite
- backend protocol redesign beyond narrow cleanup needed to support a stable UI model
- replacing `gui-agent` with a third-party control plane

## Why This Exists

`gui-agent` has a clear product ambition, but the current implementation still carries multiple competing models:

- top-level surfaces with equal weight even when they are not equally mature
- duplicated control ownership across sidebar, Runtime, Agent Detail, and Rooms
- multiple conversation/session identities exposed as if they were one concept
- event-derived explorer surfaces presented as if they were canonical evidence browsers

The result is a product that is usable for a maintainer who knows the internals, but harder than necessary for an operator who is trying to answer:

1. What is running?
2. What is broken?
3. What conversation or room owns this work?
4. Where do I go to act on it?

## Converged Diagnosis

### 1. Surface Hierarchy Is Still Ambiguous

Primary and secondary surfaces are visually and behaviorally too similar.

Relevant files:

- [`packages/gui-agent/src/App.tsx`](../../packages/gui-agent/src/App.tsx)
- [`packages/gui-agent/src/components/layout/AppShell.tsx`](../../packages/gui-agent/src/components/layout/AppShell.tsx)
- [`packages/gui-agent/src/components/layout/AgentSidebar.tsx`](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)
- [`docs/plans/gui-agent-v2-rearchitecture.md`](gui-agent-v2-rearchitecture.md)

Symptoms:

- `Runtime`, `Rooms`, `Orchestration`, `Turns`, `Context`, `Artifacts`, `Events`, and `Companion` all appear as peer destinations.
- The app shell header only reflects the active surface, not the active entity or incident context.
- The sidebar still behaves like a second runtime surface instead of a summary navigator.

Consequence:

- Operators must understand internal concepts before the UI starts helping them.
- The product reads as “many tools in one shell” instead of “one control plane with drill-downs.”

### 2. Runtime Ownership Is Split Across Multiple Surfaces

Runtime control is duplicated between:

- `AgentSidebar`
- `AgentList`
- `AgentDetailView`
- room-related panels inside Runtime

Relevant files:

- [`packages/gui-agent/src/components/layout/AgentSidebar.tsx`](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)
- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx`](../../packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx)

Symptoms:

- Sidebar exposes conversational agent lists, worker sections, batch cleanup, and spawn actions.
- Runtime repeats the same inventory with richer cards.
- Agent Detail expands into a three-column workbench that partially reimplements Companion, Rooms, and orchestration behaviors.

Consequence:

- There is no single authoritative place for runtime action ownership.
- The same concept appears with different affordances and different levels of detail.

### 3. Navigation Is View-Based, Not Entity-Based

The app remembers the active surface, but selected agent / room / conversation / trace are mostly in-memory selections or storage hacks.

Relevant files:

- [`packages/gui-agent/src/stores/viewStore.ts`](../../packages/gui-agent/src/stores/viewStore.ts)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/conversations/ConversationsList.tsx`](../../packages/gui-agent/src/components/conversations/ConversationsList.tsx)

Symptoms:

- hash routing only captures the surface
- Runtime to Companion handoff writes `localStorage` and waits for Companion to auto-select
- room and trace context are not durable route state

Consequence:

- refresh/back-forward behavior is fragile
- deep links are weak
- cross-surface handoffs are easy to break

### 4. The Conversation / Session / Agent-Link Model Is Confused

The UI currently exposes multiple partially overlapping concepts:

- companion conversations
- console sessions
- persisted sessions
- agent ask-stream conversations
- linked-agent companion conversations

Relevant files:

- [`packages/gui-agent/src/components/conversations/ConversationsList.tsx`](../../packages/gui-agent/src/components/conversations/ConversationsList.tsx)
- [`packages/gui-agent/src/components/layout/SpawnAgentPanel.tsx`](../../packages/gui-agent/src/components/layout/SpawnAgentPanel.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/api/client.ts`](../../packages/gui-agent/src/api/client.ts)

Note: the old standalone `CompanionChat` component and private `chatStore`
were later deleted after caller evidence showed current conversation surfaces no
longer used them.

Specific problems:

1. ask-stream can drift from the server-returned canonical `conversation_id`
2. linked-agent identity is inconsistent between Runtime and Companion
3. `localStorage` and `sessionStorage` are used with unrelated keys and semantics
4. persisted sessions are shown but not truly resumable
5. unlinked new conversations use synthetic IDs derived from console sessions

Consequence:

- operators cannot easily tell what kind of conversation they are looking at
- state restoration is brittle
- the model leaks transport details into product behavior

### 5. Rooms Are Fragmented Across Surfaces

Relevant files:

- [`packages/gui-agent/src/components/rooms/RoomsView.tsx`](../../packages/gui-agent/src/components/rooms/RoomsView.tsx)
- [`packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx`](../../packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)

Symptoms:

- Runtime has room control
- agent cards show room badges and room shortcuts
- Agent Detail has control-room creation, affiliated-room listings, and room messaging
- RoomsView is both room directory and room editor

Consequence:

- “where do I manage rooms?” has multiple valid answers
- room actions are duplicated with slightly different semantics

### 6. Evidence Surfaces Are Still Heuristic

`Turns`, `Context`, and `Artifacts` are currently event-derived projections over bounded recent activity rather than canonical durable evidence browsers.

Relevant files:

- [`packages/gui-agent/src/components/v2/V2Explorers.tsx`](../../packages/gui-agent/src/components/v2/V2Explorers.tsx)
- [`packages/gui-agent/src/hooks/useActivityStream.ts`](../../packages/gui-agent/src/hooks/useActivityStream.ts)
- [`packages/gui-agent/src/stores/activityStore.ts`](../../packages/gui-agent/src/stores/activityStore.ts)

Symptoms:

- traces are “prebuilt” by segmenting activity events
- surfaces can be empty even when durable data exists elsewhere
- the UI implies canonicality that the backend contract does not yet provide

Consequence:

- operator trust is weaker
- these surfaces create conceptual noise in top-level nav

### 7. Events Is Better, But Still Needs a Stronger Trust Model

Relevant files:

- [`packages/gui-agent/src/components/actions/LogsViewer.tsx`](../../packages/gui-agent/src/components/actions/LogsViewer.tsx)
- [`packages/gui-agent/src/stores/eventProjectionStore.ts`](../../packages/gui-agent/src/stores/eventProjectionStore.ts)

Symptoms:

- default hidden commands are persisted quietly
- raw event visibility is a toggle, not a mode with clear operator semantics
- row actions infer destinations from payload shape

Consequence:

- two operators can be looking at different filtered views without noticing
- “what evidence am I actually seeing?” is not always obvious

### 8. Frontend Contract Normalization Is Too Weak

Relevant files:

- [`packages/gui-agent/src/api/client.ts`](../../packages/gui-agent/src/api/client.ts)
- [`packages/data/src/types.ts`](../../packages/data/src/types.ts)
- [`internal/interfaces/web/server.go`](../../internal/interfaces/web/server.go)

Symptoms:

- some endpoints return raw JSON
- some return canonical envelopes
- orchestration can return inline payloads or artifact pointers
- client components frequently compensate for transport variation directly

Consequence:

- component code becomes transport-aware
- UI cleanup is slower because every screen also acts as a protocol adapter

### 9. State Architecture Is Fragmented

Relevant files:

- [`packages/gui-agent/src/stores/viewStore.ts`](../../packages/gui-agent/src/stores/viewStore.ts)
- [`packages/gui-agent/src/stores/activityStore.ts`](../../packages/gui-agent/src/stores/activityStore.ts)
- [`packages/gui-agent/src/stores/eventProjectionStore.ts`](../../packages/gui-agent/src/stores/eventProjectionStore.ts)
- [`packages/gui-agent/src/stores/orchestrationBoardStore.ts`](../../packages/gui-agent/src/stores/orchestrationBoardStore.ts)

Symptoms:

- stores are small and local, but product concepts cut across them
- surface transitions rely on side effects across multiple stores
- persistence rules are distributed and inconsistent

Consequence:

- cross-surface bugs are likely
- refactors require touching many implicit coordination points

### 10. UX Feedback and Test Coverage Are Below the Risk Level of the App

Relevant files:

- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)
- [`packages/gui-agent/src/components/layout/AgentSidebar.tsx`](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)
- [`packages/gui-agent/package.json`](../../packages/gui-agent/package.json)

Symptoms:

- `window.confirm`, `alert`, and `console.error` are still used in key flows
- no dedicated GUI tests were found
- large stateful components carry high regression risk without coverage

Consequence:

- UX feels less intentional than the product ambition
- cleanup work will regress easily without a safety net

## Product Decisions To Lock Before Refactoring

These decisions should be made explicit before large implementation work:

1. **Primary surfaces**
   - Recommended: `Runtime`, `Companion`, `Events`, `Rooms`, `Orchestration`

2. **Secondary / contextual surfaces**
   - Recommended: `Turns`, `Context`, `Artifacts`
   - Expose via drill-down or only when durable evidence exists

3. **Canonical room ownership**
   - Recommended: `RoomsView` is the single canonical room surface

4. **Canonical conversation model**
   - Recommended: define one source of truth for linked conversation identity and one explicit distinction between:
     - companion conversation
     - console session
     - persisted session

5. **Navigation model**
   - Recommended: entity-aware routes or route-like state

## Recommended Roadmap

### Slice 1: Control-Plane IA Hard-Cut

Goal:

- make the app read as one control plane

Scope:

- simplify top-level surface hierarchy
- reduce sidebar ownership to summary/navigation
- make Runtime the canonical action-heavy surface
- improve header context

Likely files:

- [`packages/gui-agent/src/App.tsx`](../../packages/gui-agent/src/App.tsx)
- [`packages/gui-agent/src/components/layout/AppShell.tsx`](../../packages/gui-agent/src/components/layout/AppShell.tsx)
- [`packages/gui-agent/src/components/layout/AgentSidebar.tsx`](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)
- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)
- [`docs/plans/gui-agent-v2-rearchitecture.md`](gui-agent-v2-rearchitecture.md)

Acceptance:

- clear primary vs secondary surfaces
- sidebar no longer duplicates Runtime management
- shell header shows current entity/incident context, not just surface title

### Slice 2: Entity-Aware Navigation Foundation

Goal:

- replace hash-only surface routing with entity-aware navigation

Scope:

- selected agent / room / conversation / trace become routable state
- remove `localStorage`-driven auto-select handoffs

Likely files:

- [`packages/gui-agent/src/stores/viewStore.ts`](../../packages/gui-agent/src/stores/viewStore.ts)
- [`packages/gui-agent/src/App.tsx`](../../packages/gui-agent/src/App.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/conversations/ConversationsList.tsx`](../../packages/gui-agent/src/components/conversations/ConversationsList.tsx)
- [`packages/gui-agent/src/components/rooms/RoomsView.tsx`](../../packages/gui-agent/src/components/rooms/RoomsView.tsx)
- [`packages/gui-agent/src/components/actions/LogsViewer.tsx`](../../packages/gui-agent/src/components/actions/LogsViewer.tsx)

Acceptance:

- refresh/back-forward preserves active entity
- direct linking to agent/room/conversation/trace is possible
- no `gui-agent-auto-select-conversation` flow remains

### Slice 3: Conversation / Session Model Cleanup

Goal:

- make conversation identity explicit and reliable

Scope:

- adopt server-returned `conversation_id`
- unify linked-agent identity contract
- define explicit behavior for companion conversation vs console session vs persisted session
- centralize persistence schema

Likely files:

- [`packages/gui-agent/src/components/conversations/ConversationsList.tsx`](../../packages/gui-agent/src/components/conversations/ConversationsList.tsx)
- [`packages/gui-agent/src/components/layout/SpawnAgentPanel.tsx`](../../packages/gui-agent/src/components/layout/SpawnAgentPanel.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/api/client.ts`](../../packages/gui-agent/src/api/client.ts)

Acceptance:

- one canonical linked conversation contract
- no synthetic conversation identity drift
- persisted sessions can be resumed deliberately, not just viewed
- storage keys are centralized and namespaced

### Slice 4: Agent Detail Decomposition

Goal:

- turn Agent Detail back into a detail surface

Scope:

- keep overview, runtime tree, status, and a few high-value actions
- move full conversation behavior to Companion
- move room authoring and control-room editor behavior to Rooms
- move advanced memory/session tools behind secondary UI

Likely files:

- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)

Acceptance:

- `AgentDetailView` no longer acts like a second application
- each major workflow has one owning surface

### Slice 5: Room Ownership Consolidation

Goal:

- make `RoomsView` canonical

Scope:

- remove duplicate room creation/editor behaviors from Runtime and Agent Detail
- keep lightweight room shortcuts only
- clarify “control room” as a typed room shortcut, not a separate model

Likely files:

- [`packages/gui-agent/src/components/rooms/RoomsView.tsx`](../../packages/gui-agent/src/components/rooms/RoomsView.tsx)
- [`packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx`](../../packages/gui-agent/src/components/rooms/RuntimeRoomPanel.tsx)
- [`packages/gui-agent/src/components/agents/AgentDetailView.tsx`](../../packages/gui-agent/src/components/agents/AgentDetailView.tsx)
- [`packages/gui-agent/src/components/agents/AgentList.tsx`](../../packages/gui-agent/src/components/agents/AgentList.tsx)

Acceptance:

- one answer to “where do I manage rooms?”
- room shortcuts deep-link into the canonical room surface

### Slice 6: Events / Forensics Trust Pass

Goal:

- make Events the default place to answer “what is broken now?”

Scope:

- explicit filter visibility
- one-click forensics mode
- trace/session-first drill-down
- clearer raw vs summarized evidence modes

Likely files:

- [`packages/gui-agent/src/components/actions/LogsViewer.tsx`](../../packages/gui-agent/src/components/actions/LogsViewer.tsx)
- [`packages/gui-agent/src/stores/eventProjectionStore.ts`](../../packages/gui-agent/src/stores/eventProjectionStore.ts)
- [`packages/gui-agent/src/hooks/useActivityStream.ts`](../../packages/gui-agent/src/hooks/useActivityStream.ts)

Acceptance:

- active filters are obvious
- raw evidence is one click away
- trace/session is treated as a first-class investigation object

### Slice 7: Evidence Surface Reframe

Goal:

- make `Turns`, `Context`, and `Artifacts` honest and useful

Scope:

- either demote them to contextual/event drill-downs
- or back them with stronger server-side read models and durable refs

Likely files:

- [`packages/gui-agent/src/components/v2/V2Explorers.tsx`](../../packages/gui-agent/src/components/v2/V2Explorers.tsx)
- related API/read-model code as needed

Acceptance:

- no top-level evidence surface claims more canonicality than the backend provides
- empty states are replaced by explicit ownership and guided drill-down

### Slice 8: Frontend Contract and State Cleanup

Goal:

- reduce protocol leakage and cross-store coupling

Scope:

- introduce stable UI-facing view models
- narrow `api/client.ts`
- reorganize stores around product domains

Likely files:

- [`packages/gui-agent/src/api/client.ts`](../../packages/gui-agent/src/api/client.ts)
- [`packages/data/src/types.ts`](../../packages/data/src/types.ts)
- [`packages/gui-agent/src/stores/*`](../../packages/gui-agent/src/stores)

Acceptance:

- components consume normalized view models
- fewer ad hoc transport compensations inside UI surfaces
- fewer side-effect-driven cross-store transitions

### Slice 9: Feedback and Regression Coverage

Goal:

- make cleanup durable

Scope:

- replace `window.confirm` / `alert` with in-app feedback patterns
- add tests for:
  - entity-aware routing
  - conversation identity reconciliation
  - ask-stream conversation adoption
  - event filter / forensics mode
  - room ownership flows

Likely files:

- GUI components and stores touched above
- `packages/gui-agent/package.json`

Acceptance:

- destructive and failure flows are handled in-app
- critical operator workflows have regression coverage

## Suggested Execution Order

1. Slice 1: Control-plane IA hard-cut
2. Slice 2: Entity-aware navigation foundation
3. Slice 3: Conversation / session model cleanup
4. Slice 4: Agent Detail decomposition
5. Slice 5: Room ownership consolidation
6. Slice 6: Events / forensics trust pass
7. Slice 7: Evidence surface reframe
8. Slice 8: Frontend contract and state cleanup
9. Slice 9: Feedback and regression coverage

Rationale:

- surface ownership and navigation should be stabilized before deep UI cleanup
- conversation/session identity issues are the riskiest state bugs and should be resolved early
- room and evidence ownership are easier to simplify once navigation and primary surface hierarchy are settled

## Immediate Next Step

If only one slice is started now, start with:

**Slice 1 + Slice 2 planning together, but implementation in order.**

That gives the project:

- a cleaner control-plane hierarchy
- less duplicated UI ownership
- a navigation model that can support the later conversation/room/evidence cleanup work without more storage hacks

## Subagent Review Notes

```text
Subagent Review
- reviewer: 019cdefb-b517-7e70-970b-1c4764cd3c2e
- scope: AppShell, AgentSidebar, AgentList, LogsViewer, gui-agent-v2-rearchitecture.md
- findings: confirmed fragmented control-plane hierarchy, equal-weight nav, flat visual hierarchy, and runtime/sidebar duplication
- decision: converged with local review

Subagent Review
- reviewer: 019cdef9-58ff-7b22-b38c-8aa4bb6b3acf
- scope: IA/workflow across AgentDetailView, RoomsView, Runtime, Events, V2 explorers
- findings: confirmed AgentDetailView sprawl, room ownership duplication, weak entity-aware navigation, and heuristic evidence surfaces
- decision: converged with local review

Subagent Review
- reviewer: 019cdf01-ca35-73e2-89a6-8542cb72f77d
- scope: conversation/runtime overlap across ConversationsList, CompanionChat, SpawnAgentPanel, AgentDetailView, chatStore, api/client
- findings: confirmed conversation identity drift, inconsistent linked-agent model, fragmented storage handoffs, weak persisted-session integration, and synthetic conversation IDs
- decision: converged with local review
```
