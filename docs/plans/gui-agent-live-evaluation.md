# gui-agent Live Evaluation

Date: 2026-03-14
Status: Active evaluation / planning input
Related:
- [gui-agent-improvement-roadmap.md](gui-agent-improvement-roadmap.md)
- [gui-agent-v2-rearchitecture.md](gui-agent-v2-rearchitecture.md)

## Scope

This document captures a live product pass over the current `gui-agent`
implementation after the recent control-plane, Companion, and workbench
refactors merged to `main`.

The evaluation was based on:
- a live browser walk using `agent-browser`
- desktop checks across `Runtime`, `Companion`, `Events`, `Rooms`,
  `Orchestration`, `Turns`, `Context`, and `Artifacts`
- a mobile-width spot check of `Runtime`
- console/runtime observation during navigation

This is not a code-review document. It is a product and interaction assessment:
what is working, what is not working, and which follow-up slices should come
next.

## Executive Read

`gui-agent` is now recognizably a control plane rather than a pile of adjacent
tools. The strongest surfaces are `Companion`, `Events`, and `Rooms`. The
weakest surface is still `Runtime`, which remains too list-heavy and
action-heavy under real operator load. The biggest unresolved regression is
mobile layout: the desktop sidebar still consumes the viewport on narrow
screens and crushes the main content.

The evidence surfaces (`Turns`, `Context`, `Artifacts`) now fit the overall IA
better, but they still feel more heuristic than canonical. During the live pass
`Turns` emitted a duplicate-key warning for `context.layered_bundle`, which
matches the current trust gap in those views.

## Working

| Rank | Area | Severity | What Is Working | Why It Works | Keep / Extend |
| --- | --- | --- | --- | --- | --- |
| 1 | Companion | strong | Agent-grouped mailbox is clear and materially easier to scan than the old mixed conversation feed. | Ownership is now the primary organizing principle, and the sidebar density is high enough to support many companion threads. | Keep agent-grouped default. Extend with better per-agent urgency cues only after Runtime is fixed. |
| 2 | Events | strong | Summary-first `Events` gives a credible "what is broken right now?" first impression. | Error cards, latency cards, and active traces are much more actionable than dumping raw rows first. | Preserve this shape. Improve trace trust and raw-event drilldown, not the top-level framing. |
| 3 | Rooms | strong | `Rooms` reads like a canonical workflow surface instead of a duplicate editor. | Workspace selector, room list, metadata editor, and message area all belong together. | Keep room ownership centralized here. Avoid reintroducing room editing elsewhere. |
| 4 | Agent Detail / Workbench | good | `AgentDetailView` feels more focused than before and no longer behaves like a second application. | Moving summary-only content into the support rail reduced concept sprawl. | Continue treating it as a runtime/workbench surface, not a settings archive. |
| 5 | Shell / IA | good | The top-level shell now communicates `Primary` vs `Evidence` and feels more deliberate. | Navigation hierarchy is clearer and the top bar supports control-plane orientation. | Keep the current top-level surface set. Avoid adding more primary surfaces casually. |
| 6 | Orchestration | promising | The board concept fits well as its own operator surface. | It is easier to understand as a dedicated board than as embedded runtime clutter. | Continue hardening it as a primary surface. |
| 7 | Bundle behavior | good | Surface-level lazy loading removed the Vite chunk-size warning. | The app no longer ships every heavy screen in the initial bundle. | Keep lazy loading. Polish first-load fallbacks later. |

## Not Working

| Rank | Area | Severity | Problem | Evidence From Live Pass | Likely Ownership |
| --- | --- | --- | --- | --- | --- |
| 1 | Responsive layout | critical | Mobile layout is effectively broken. The desktop sidebar still occupies the left side on narrow screens and the main panel gets crushed into a thin strip. | `390x844` Runtime pass showed sidebar + content squeezed side-by-side instead of adapting to a mobile navigation pattern. | [AppShell.tsx](../../packages/gui-agent/src/components/layout/AppShell.tsx), [AgentSidebar.tsx](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx) |
| 2 | Runtime density | high | `Runtime` still collapses into a long, repetitive action-heavy inventory once the top summary ends. | Live pass on ~100 agents showed a wall of repeated `Workbench`, `Start`, `Stop`, and `Open Workbench` actions with weak row differentiation. | [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx) |
| 3 | Runtime action clarity | high | Actions are too generic and not uniquely targetable. | Browser automation had to fall back to refs because repeated action labels made specific targets hard to distinguish. This is also a human scanability problem. | [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx) |
| 4 | Evidence trust | medium | `Turns` / `Context` / `Artifacts` still feel heuristic, not canonical, and at least one real rendering issue remains. | Console warning during `Turns`: duplicate children with key `context.layered_bundle`; duplicated turn signal chips reinforced the "derived projection" feel. | [V2Explorers.tsx](../../packages/gui-agent/src/components/v2/V2Explorers.tsx) |
| 5 | Lazy-load cold start | medium | Several non-runtime surfaces show the loading fallback long enough to feel "cold" rather than immediate. | `Rooms`, `Orchestration`, and `Turns` all visibly hit the `Loading ...` fallback before rendering. | [App.tsx](../../packages/gui-agent/src/App.tsx) |
| 6 | Runtime/operator priority | medium | Runtime still over-emphasizes inventory management relative to incident triage. | The top summary is good, but the main body still rewards browsing and clicking through agents more than quick operator decisions. | [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx) |
| 7 | Evidence surface weight | low | `Turns`, `Context`, and `Artifacts` still look slightly too "first-class" relative to their confidence level. | The shell hierarchy is better, but the views themselves still present strong object framing for derived evidence. | [AppShell.tsx](../../packages/gui-agent/src/components/layout/AppShell.tsx), [AgentSidebar.tsx](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx), [V2Explorers.tsx](../../packages/gui-agent/src/components/v2/V2Explorers.tsx) |

## Ranked PR Slices

### PR 1: Responsive Shell and Sidebar

Priority: P0
Goal: make `gui-agent` usable on mobile-width screens

Scope:
- [AppShell.tsx](../../packages/gui-agent/src/components/layout/AppShell.tsx)
- [AgentSidebar.tsx](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)
- any sidebar trigger / drawer state needed for narrow screens

Acceptance:
- on `390x844`, the main surface is readable and not crushed
- primary navigation can be opened and dismissed intentionally
- top bar and content stack correctly without horizontal clipping
- evidence and primary surfaces still preserve current hierarchy

### PR 2: Runtime Triage Compression

Priority: P1
Goal: make `Runtime` feel like an incident/operations surface instead of a
long control list

Scope:
- [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx)

Acceptance:
- default runtime view emphasizes active, errored, and recently changed agents
- row density is materially improved
- secondary row metadata is reduced or progressively disclosed
- top summary and quick actions remain, but the main list becomes easier to
  scan under high agent counts

### PR 3: Runtime Action Label Pass

Priority: P1
Goal: make row actions human- and automation-targetable

Scope:
- [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx)
- supporting button labels and accessible names

Acceptance:
- repeated row actions include specific target context, for example:
  `Open workbench for researcher #01K...`
- no more long runs of indistinguishable `Workbench`, `Start`, or `Stop`
  controls in accessibility snapshots
- hover/visible labels stay concise while accessible labels remain explicit

### PR 4: Evidence Integrity Pass

Priority: P1
Goal: make `Turns` / `Context` / `Artifacts` feel internally reliable

Scope:
- [V2Explorers.tsx](../../packages/gui-agent/src/components/v2/V2Explorers.tsx)

Acceptance:
- duplicate-key warning is removed
- repeated trace/ref badges are deduped or intentionally grouped
- terminology and empty states make the "derived evidence" status clearer
- one operator can tell which parts are trace candidates vs canonical refs

### PR 5: Lazy-Load Polish

Priority: P2
Goal: keep the bundle win without the cold-start feel

Scope:
- [App.tsx](../../packages/gui-agent/src/App.tsx)
- optionally route-specific skeletons or per-surface fallbacks

Acceptance:
- `Rooms`, `Orchestration`, and evidence surfaces feel intentional when loading
- fallback content is smaller, less jarring, and better matched to the target
  screen
- first-load transitions do not feel like blank page swaps

### PR 6: Runtime Priority Reframing

Priority: P2
Goal: clarify what Runtime is for once density is under control

Scope:
- [AgentList.tsx](../../packages/gui-agent/src/components/agents/AgentList.tsx)
- possibly [AgentSidebar.tsx](../../packages/gui-agent/src/components/layout/AgentSidebar.tsx)

Acceptance:
- Runtime answers "what needs attention?" before "what exists?"
- queue/backlog affordances are more obvious than raw inventory exploration
- quick actions align with triage, restart, inspect, and handoff workflows

## Preserve These Decisions

- Keep `Companion` agent-grouped by default.
- Keep `Rooms` as the canonical room-management surface.
- Keep `Events` summary-first rather than returning to a raw log viewer.
- Keep `AgentDetailView` reduced and summary-driven.
- Keep lazy-loaded heavy surfaces; polish the experience rather than removing
  the split.

## Open Questions

1. Should `Runtime` eventually gain an explicit "triage mode" vs "inventory
   mode", or should the default list simply become triage-first permanently?
2. Should `Turns`, `Context`, and `Artifacts` stay top-level in the sidebar, or
   should they become secondary destinations entered from `Events`?
3. Should `Orchestration` remain a full primary surface if its board is only
   used for a subset of operators, or is it still correct at the top level?

## Recommended Next Move

Implement `PR 1` and `PR 2` before any more Companion polish. The current
largest product gap is no longer the conversation model. It is the mismatch
between the improved shell and the still-too-dense Runtime surface, plus the
fact that narrow-screen layout is not yet usable.
