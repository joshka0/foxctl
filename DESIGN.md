# DESIGN.md

## Scope

This file is the visual and interaction source of truth for `agentctl` UI work.

The primary target is:

- `packages/gui-agent/`

This document complements `AGENTS.md`:

- `AGENTS.md` defines engineering, runtime, and process rules
- `DESIGN.md` defines product shape, UX priorities, and visual interaction rules

## Product Intent

`gui-agent` is not a generic AI chat app.
It is an operator control plane for a multi-agent system.

It should feel:

- operational, not toy-like
- information-dense, but not noisy
- trustworthy under load
- explicit about ownership, scope, and evidence
- fast to scan when something is broken

The user should be able to answer, within seconds:

1. What is running?
2. What is blocked or failing?
3. Which agent / room / conversation owns this work?
4. What can I do next?

## Primary UX Model

The GUI should be designed around entities, not around disconnected tabs.

Primary entities:

- workspace
- agent
- room
- conversation
- trace / evidence

Navigation should always preserve the active entity context where possible.
Avoid “blank surface first, choose context later” workflows.

## Core Design Principles

### 1. Runtime First

The default experience should prioritize live operations over passive browsing.

Good:

- active agents visible immediately
- errors and stalled work surfaced early
- one-click actions for chat, inspect, stop, resume, room handoff

Bad:

- explorer-first information architecture
- equal visual weight for mature and placeholder surfaces

### 2. Main Lane, Detail Lane, Evidence Lane

Use a three-lane mental model:

- Main lane: current operational surface
- Detail lane: selected entity details and actions
- Evidence lane: logs, turns, artifacts, traces, context refs

Do not flatten all three into peer top-level surfaces.
Evidence should support runtime and companion work, not compete with it.

### 3. Main Message, Threaded Detail

Borrow from `pi-mono/packages/mom`: keep the main interaction surface readable and push verbose execution detail into subordinate views.

In `gui-agent`, that means:

- primary cards and lists show concise status
- tool-call floods belong in drawers, inspectors, expandable sections, or trace panels
- the default operator view should not resemble a raw event firehose

### 4. Summary First, Raw Second

Borrow from `pi-mono/packages/web-ui`: message and session UIs work because the default rendering is concise and stable.

Apply the same rule to `gui-agent`:

- show summaries first
- make raw payloads explicit and opt-in
- never require payload reading to understand basic state

### 5. Multi-Agent Work Is Coordinated, Not Collapsed

The UI should present a multi-agent system as a graph of cooperating actors, not as one flat list of processes.

Important distinctions must stay visible:

- parent vs child agent
- conversational agent vs worker
- room-owned vs standalone activity
- session history vs live activity stream

### 6. Honest Surfaces

If a surface is projection-based or heuristic, the UI must say so through structure and copy.
Do not present inferred traces as if they are canonical records.

This is especially important for:

- turns
- context
- artifacts
- event-derived drilldowns

## Visual Direction

The visual language should feel like a serious systems console with strong editorial hierarchy.

### Tone

- dark-first is acceptable here because this is an operator console
- avoid neon sci-fi styling
- avoid soft marketing gradients and decorative fluff
- prefer quiet contrast, layered panels, disciplined accent use

### Hierarchy

- status color should be meaningful and sparse
- typography should differentiate label, state, title, and evidence
- layout should create obvious scanning zones

### Density

- compact by default
- dense lists are fine when grouping and labels are strong
- never trade away legibility for density

### Interaction

- keyboard-friendly where possible
- drawers and side panels preferred over full route jumps for inspection
- preserve context during drilldown

## Surface Ownership

### Runtime

Canonical home for:

- live agent inventory
- state transitions
- direct lifecycle actions
- fast triage

Should not duplicate full room management or conversation browsing.

### Companion

Canonical home for:

- working conversations
- agent chat follow-up
- linked agent/conversation context
- context-aware human steering

Should feel like a workbench, not a generic chat clone.

### Rooms

Canonical home for:

- room directory
- room membership
- room transcript and shared coordination stream

Room controls scattered into runtime cards or detail panels should be minimized.

### Orchestration

Canonical home for:

- issue flow
- board state
- execution grouping
- workspace-level coordination

This should evolve toward a board-plus-runtime-summary model, not just another list.

### Events

Canonical home for:

- debugging
- forensics
- operational audit

Should default to curated signal, not raw volume.

### Turns / Context / Artifacts

These are evidence surfaces.
They are valuable when reached from runtime, conversation, or events.
They should not dominate primary navigation when empty or weakly grounded.

## Multi-Agent Interaction Model

This is where `pi-mono` is a useful basis.

Patterns worth borrowing:

### A. Separate the clean narrative from the verbose machinery

From `packages/mom`:

- main output stays readable
- tool details are secondary
- system progress is inspectable without dominating the conversation

For `gui-agent`, use:

- concise runtime cards
- expandable trace drawers
- support rails for related entities
- explicit “show raw” actions

### B. Treat sessions as first-class, resumable operator objects

From `packages/web-ui`:

- sessions have metadata
- sessions are selectable
- state persists clearly

For `gui-agent`, conversations and sessions must stop feeling synthetic or transport-derived.
Selection, restoration, and deep-linking must be stable.

### C. Make subagent roles legible

From `packages/coding-agent/examples/extensions/subagent`:

- scout
- planner
- reviewer
- worker

For `gui-agent`, multi-agent views should visibly encode role and responsibility.
A worker swarm without role semantics is just a process list.

### D. Keep workspace-local skills and memory close to the active context

From `packages/mom`:

- workspace-scoped memory
- channel-local skills
- durable logs plus compact working context

For `gui-agent`, this suggests:

- workspace context should always be visible near orchestration and runtime
- memory and room surfaces should feel attached to work, not abstract knowledge stores
- artifact and context panels should show scope clearly

## Design Rules For New UI Work

When adding or revising a `gui-agent` surface:

1. Start from the operator question being answered.
2. Choose a canonical owner surface for the action.
3. Keep secondary evidence behind a progressive reveal.
4. Preserve entity context across navigation.
5. Prefer one strong path over many partially overlapping paths.
6. If a panel duplicates another surface, reduce it to summary form.
7. If a surface has no trustworthy data, route users to the owning surface with a guided CTA.

## Specific Guidance For Current GUI Work

### App shell

The shell should reinforce:

- active surface
- active entity
- workspace scope
- connection health

It should not become a second runtime surface.

### Sidebar

The sidebar should be:

- compact
- summary-oriented
- navigation-first

Not:

- a duplicate control plane inside the control plane

### Agent detail

Agent detail should function as a focused inspection and action workbench.
It should unify:

- role
- lineage
- runtime
- linked room
- linked conversation
- nearby evidence

It should not independently recreate all room and companion workflows.

### Rooms

Rooms should emphasize coordination state:

- who is in the room
- what the room is for
- latest important messages
- relationship to workspace and agents

### Events and traces

Events should be legible under pressure.

Defaults:

- errors first
- suspicious latency visible
- structured refs clearly surfaced
- raw payloads hidden until requested

## What To Avoid

- treating every internal concept as a top-level destination
- raw JSON as primary UI
- duplicated actions across Runtime, Sidebar, Agent Detail, and Rooms
- route state hidden in ad hoc storage without deep-link semantics
- decorative visual complexity not tied to meaning
- “AI dashboard” styling clichés

## Accessibility And Quality Bar

All major surfaces should support:

- clear focus states
- meaningful labels
- readable contrast
- empty states with action
- loading states that explain what is being prepared
- error states that preserve nearby context

## Working References

Current `agentctl` files that should stay aligned with this document:

- `packages/gui-agent/src/App.tsx`
- `packages/gui-agent/src/components/layout/AppShell.tsx`
- `packages/gui-agent/src/components/layout/AgentSidebar.tsx`
- `packages/gui-agent/src/components/agents/AgentDetailView.tsx`
- `packages/gui-agent/src/components/rooms/RoomsView.tsx`
- `packages/gui-agent/src/components/v2/OrchestrationBoardScreen.tsx`
- `docs/plans/gui-agent-improvement-roadmap.md`
- `docs/plans/gui-agent-v2-rearchitecture.md`

Reference material used for the multi-agent interaction direction:

- `~/repos/githubs/pi-mono/packages/web-ui/README.md`
- `~/repos/githubs/pi-mono/packages/web-ui/src/components/AgentInterface.ts`
- `~/repos/githubs/pi-mono/packages/web-ui/src/components/MessageList.ts`
- `~/repos/githubs/pi-mono/packages/web-ui/src/dialogs/SessionListDialog.ts`
- `~/repos/githubs/pi-mono/packages/mom/README.md`
- `~/repos/githubs/pi-mono/packages/mom/docs/new.md`
- `~/repos/githubs/pi-mono/packages/coding-agent/examples/extensions/subagent/README.md`
