# Information Architecture — TUI Operator Cockpit

This document specifies the information architecture for the redesigned foxctl
TUI operator cockpit. It covers layout responsibilities, primary and secondary
flows, navigation, focus, keybindings, screen inventory, state mappings,
progressive reveal, and the three-plane memory model.

It satisfies VAL-DOCS-004, VAL-DOCS-011, and VAL-DOCS-013.

Cross-references:
- `DESIGN.md` (repo root) — product shape, UX priorities, visual interaction rules
- `docs/plans/go-tui-agent-shell.md` — canonical prior plan (four-region shell)
- `audit-current-tui.md` — audit of the current TUI with file:line citations
- `architecture.md` — architecture decisions and rationale (forthcoming M1 deliverable)

---

## (a) Three-Lane Layout Responsibilities

The redesigned cockpit uses a three-lane layout per DESIGN.md principle 2
("Main Lane, Detail Lane, Evidence Lane"). The layout replaces the current
four-region shell (top bar / transcript / composer / right rail) defined in
[go-tui-agent-shell.md](../go-tui-agent-shell.md) with an entity-centric,
agent-first model.

### Main Lane (left, ~40% width)

**Canonical responsibility:** primary operational surface — live agent
inventory, entity selection, and fast triage.

The Main lane answers DESIGN.md's first question: *"What is running?"*

Contents:

- **Agent inventory** — a scrollable list of typed `Agent` entities, each row
  showing short agent ID, role, status badge, workspace label, parent link
  (`—` if root), and last-activity timestamp.
- **Empty state** — when zero agents exist, a CTA explaining how to spawn one
  (e.g., `foxctl agent spawn --role researcher`).
- **Loading state** — rendered within 500ms of launch; shows "Connecting to
  daemon at <url>…" with a spinner.
- **Error state** — when the daemon is unreachable after the configured
  timeout (≤5s), shows the URL, the error, and a retry hint.
- **Rooms list** (secondary screen, navigated to via `2` key) — room
  directory with membership and latest message summary.

The Main lane is always visible except when the terminal is below minimum
size (see [Minimum Terminal Size](#minimum-terminal-size)).

### Detail Lane (center, ~35% width)

**Canonical responsibility:** selected entity details and actions.

The Detail lane answers DESIGN.md's questions *"Which agent / room /
conversation owns this work?"* and *"What can I do next?"*

Contents:

- **Agent detail** — selected agent's runtime snapshot, hierarchy context
  (parent/children summary), recent transcript preview, role, lineage,
  and linked room.
- **Ask/chat composer** — text input for sending messages to the selected
  agent via `POST /api/agents/{id}/ask-stream`. Submit streams token
  replies into the transcript preview.
- **Room detail** (secondary screen) — room membership, purpose, latest
  messages, relationship to workspace and agents.
- **Empty state** — when no entity is selected, shows "Select an agent
  from the inventory or spawn one." with keyboard guidance.

The Detail lane updates reactively when selection changes in the Main lane.
It is a focused inspection and action workbench per DESIGN.md "Agent detail"
guidance: it should unify role, lineage, runtime, linked room, linked
conversation, and nearby evidence without independently recreating all room
and companion workflows.

### Evidence Lane (right, ~25% width; collapsible drawer)

**Canonical responsibility:** raw payloads, tool calls, errors, traces,
context references.

The Evidence lane answers DESIGN.md's question *"What is blocked or
failing?"* by surfacing detail behind a progressive reveal.

Contents:

- **Evidence drawer** — opens over the right portion of the screen when the
  user presses `e` on a selected transcript row. Shows the raw payload for
  the selected row (text reply, tool call with args + result, or error row
  with error details).
- **Memory surfaces** — Companion Memory, Named Durable Memory, and ACA /
  Continuity panels (see [Three-Plane Memory Model](#three-plane-memory-model)).
- **Events log** — curated event stream filtered to errors and notable
  signals, not raw volume.

Per DESIGN.md principle 3 ("Main Message, Threaded Detail"), the Evidence
lane defaults to collapsed. It does not compete with the Main or Detail
lanes for visual weight. The default operator view should not resemble a raw
event firehose.

### Reconciliation with go-tui-agent-shell.md Four-Region Shell

The prior plan at [go-tui-agent-shell.md](../go-tui-agent-shell.md) defines
a four-region layout: top bar, center transcript, bottom composer, and right
rail with tabs (Memory, Continuity, Workers, Task).

The three-lane model **supersedes** the four-region shell for the agent-first
walking skeleton (M3) while **preserving** the conceptual coverage:

| Four-region element | Three-lane destination | Rationale |
|---------------------|----------------------|-----------|
| Top bar (assistant name, workspace, provider/model, in-flight status) | Status footer (connection + active entity + keybindings) | Top bar is split: operational status goes to the always-visible footer; entity identity is part of the Detail lane header. |
| Center transcript | Detail lane (transcript preview) | Transcript moves from a full-center surface to a component within the Detail lane, contextualized to the selected agent. |
| Bottom composer | Detail lane (composer sub-section) | Composer stays attached to the active entity context rather than being a global bottom bar. |
| Right rail (Memory, Continuity, Workers, Task tabs) | Evidence lane (memory surfaces) + Main lane (workers in inventory) | Memory and continuity become Evidence-lane drawers. Workers become first-class rows in the agent inventory (Main lane) rather than a separate tab. Task/board moves to a secondary screen accessible via navigation. |

This reconciliation ensures no conceptual surface from the canonical plan is
lost while restructuring the layout around entity-first navigation per
DESIGN.md principles 1 ("Runtime First") and 5 ("Multi-Agent Work Is
Coordinated, Not Collapsed").

---

## (b) Agent Inventory → Detail → Ask/Chat Primary Flow

This section describes the primary operator flow screen by screen.

### Screen 1: Loading → Agent Inventory

**Trigger:** launch `foxctl_tui -screen agents` (or documented invocation per
M1 architecture decision).

1. Within 500ms, the Main lane renders a loading state: "Connecting to
   daemon at `<url>`…" with a spinner.
2. Async boot fetches `GET /api/agents` in the background.
3. On success, the Main lane transitions to the agent inventory showing all
   agents.
4. On failure (timeout ≤5s), the Main lane shows an error state with the URL,
   error message, and retry guidance.
5. The Detail lane shows an empty state: "Select an agent from the inventory."
6. The status footer shows connection status, workspace, and keybinding hints.

**Keyboard actions available:** ESC to quit, Tab to cycle lanes.

### Screen 2: Selecting an Agent

**Trigger:** user presses ↓/↑ in the Main lane to highlight a different agent.

1. The highlighted agent row shows a visible selection indicator (colored left
   border or inverse background — not font-weight only).
2. The Detail lane updates to show the selected agent's:
   - Role and agent ID
   - Runtime snapshot (status, provider/model, execution mode)
   - Hierarchy context (parent agent if any, child count)
   - Recent transcript preview (last N entries)
3. The status footer updates the active entity label (e.g.,
   `agent: abc12345 (researcher)`).
4. Live refresh via `/api/events` (topic-filtered for agent events) updates
   the inventory as agents change externally.

**Keyboard actions available:** ↓/↑ to navigate, Enter to focus composer,
Tab to cycle lanes, `e` to open evidence drawer.

### Screen 3: Ask/Chat

**Trigger:** user presses Enter or navigates focus to the composer within the
Detail lane.

1. The composer becomes focused (visible cursor, highlighted border).
2. User types a message. Enter submits.
3. `POST /api/agents/{id}/ask-stream` is called. Tokens stream progressively
   into the transcript preview.
4. During streaming, the status footer shows "streaming…" and the cancel
   keybinding is highlighted.
5. On stream completion, the final transcript row shows a terminal status
   marker (✓ or "done").
6. On cancel (Ctrl+X during stream), the cancel endpoint is called, the
   transcript row shows a cancelled state, and a fresh submit is accepted
   within 100ms.

**Double-submit behavior:** pressing Enter while a stream is in flight
rejects the submission with a visible status bar message: "Stream in flight.
Press Ctrl+X to cancel first." No silent behavior; no orphan goroutine.

**Keyboard actions available:** Enter to submit, Ctrl+X to cancel,
ESC to close evidence drawer, Tab to cycle focus, `e` to open evidence
drawer on a transcript row.

### Screen 4: Evidence Inspection

**Trigger:** user presses `e` on a highlighted transcript row in the Detail
lane.

1. The Evidence drawer slides open over the right portion of the screen.
2. The drawer shows the raw payload for the selected row:
   - **Text reply:** full markdown/text content.
   - **Tool call:** tool name, arguments JSON, result JSON.
   - **Error row:** error code, message, stack trace if available.
3. ESC closes the drawer and returns focus to the previously focused element.
4. The drawer traps focus while open (Tab/Shift-Tab cycle inside the drawer).

---

## (c) Rooms Secondary Flow Outline

Rooms are a secondary flow, not the default screen. The primary flow centers
on agents per DESIGN.md principle 1 ("Runtime First").

### Rooms List

**Navigation:** press `2` from any screen to switch to the rooms inventory.

1. The Main lane replaces the agent inventory with a rooms directory.
2. Each row shows: room ID, title, member count, latest message summary,
   and relationship to the active workspace.
3. The Detail lane shows an empty state: "Select a room to view details."
4. Press `1` to return to the agent inventory.

### Room Detail

**Trigger:** user presses ↓/↑ to highlight a room, then the Detail lane
updates.

1. Detail lane shows: room purpose, membership list, latest messages,
   linked agents, and coordination state.
2. The composer is not active for rooms in M3 — rooms are read-only
   inspection surfaces in the walking skeleton.
3. Room events stream via `/api/rooms/{id}/events` (SSE) for live updates.

**Rooms design rationale:** per DESIGN.md "Rooms" section, rooms emphasize
coordination state (who is in the room, what the room is for, latest
messages, relationship to workspace and agents). Room controls scattered
into runtime cards or detail panels should be minimized.

---

## (d) Navigation Model

Navigation is lane-based and entity-first. There are no disconnected tabs.

### Global Navigation Keys

| Key | Action |
|-----|--------|
| `1` | Switch Main lane to agent inventory (primary screen) |
| `2` | Switch Main lane to rooms directory (secondary screen) |
| Tab | Cycle focus forward through lanes (Main → Detail → Evidence) |
| Shift+Tab | Cycle focus backward through lanes (Evidence → Detail → Main) |
| ESC | Close drawer / cancel modal / quit if nothing to close |
| `q` or Ctrl+C | Quit (hard exit, always available) |

### In-Lane Navigation

Within each lane, standard vim-adjacent keys apply:

| Key | Action |
|-----|--------|
| ↓ / `j` | Move selection down one row |
| ↑ / `k` | Move selection up one row |
| Home / `g` | Jump to first row |
| End / `G` | Jump to last row |
| PageDown / Ctrl+D | Scroll down one page |
| PageUp / Ctrl+U | Scroll up one page |
| Enter | Activate / submit / focus composer |
| `e` | Open evidence drawer for selected row |

### Back Navigation

ESC serves as the universal "back" key:

1. If a drawer is open → close drawer, restore focus.
2. If the composer is focused → blur composer, return focus to transcript.
3. If on a secondary screen (rooms) → return to agent inventory.
4. If nothing to close → quit.

---

## (e) Focus Model

Focus is visually indicated and always unambiguous.

### Focus Indicators

Per DESIGN.md "Accessibility And Quality Bar":

- **Focused lane** — highlighted border (colored left border or inverse
  background, not font-weight only).
- **Selected entity** — distinct background color on the selected row.
- **Focused input** — visible cursor and highlighted border in the composer.

### Focus Cycling

Focus cycles through three major lanes:

```
Main → Detail → Evidence → Main (wrap around)
```

Shift+Tab cycles in reverse. The cycle order matches left-to-right spatial
layout.

### Focus Within Lanes

- **Main lane:** focus is on the entity list; ↓/↑ change selection.
- **Detail lane:** focus alternates between transcript preview and composer.
  Tab within the Detail lane toggles between these sub-regions.
- **Evidence lane (drawer):** when open, focus is trapped inside. ESC closes
  and restores previous focus.

### Focus Preservation

- Selection state survives focus changes (Tab away and back preserves the
  selected agent).
- Selection state survives lane switches (`1` / `2` and back).
- Selection state survives terminal resizes.

---

## (f) Keybinding Table

The following table lists all proposed keybindings for the redesigned cockpit.
The "Conflicts" column cross-references `internal/interfaces/tui/keys.go` and
`internal/interfaces/tui/shell.gsx` (lines 269–302 in the `.gsx` source) for
conflicts with existing bindings. Every conflict is acknowledged.

| New Binding | Purpose | Conflicts with existing binding in keys.go or shell.gsx |
|-------------|---------|---------------------------------------------------------|
| ESC | Close drawer / cancel modal / back / quit | **Conflict:** `keys.go:38` maps ESC to `App().Stop()` (hard quit). **Resolution:** ESC in the new design is context-sensitive: it backs out of drawers/modals first, then quits. The hard quit is retained via `q` and Ctrl+C. |
| `q` | Quit (hard exit) | **Conflict:** `keys.go:39` maps `q` to `App().Stop()`. **Acknowledged:** same binding, same purpose. No change needed. |
| Ctrl+C | Quit (hard exit) | **Conflict:** `keys.go:40` maps Ctrl+C to `App().Stop()`. **Acknowledged:** same binding, same purpose. No change needed. |
| Tab | Cycle focus forward through lanes | **Conflict:** `shell.gsx:270` includes `s.focus.KeyMap()` which provides Tab for FocusGroup cycling. The current FocusGroup cycles through Transcript → Composer → Rail → Workers (4 panes). **Resolution:** Tab remains focus-cycle-forward; the FocusGroup is reduced from 4 panes to 3 lanes (Main, Detail, Evidence). |
| Shift+Tab | Cycle focus backward through lanes | **Conflict:** FocusGroup provides Shift-Tab for reverse cycling. **Acknowledged:** same binding, same purpose. Pane count changes. |
| ↓ / `j` | Move selection down | **No conflict.** Current TUI has no explicit ↓/`j` binding in keys.go or shell.gsx. |
| ↑ / `k` | Move selection up | **No conflict.** Current TUI has no explicit ↑/`k` binding. |
| Home / `g` | Jump to first row | **No conflict.** |
| End / `G` | Jump to last row | **No conflict.** |
| PageDown / Ctrl+D | Scroll down one page | **No conflict.** |
| PageUp / Ctrl+U | Scroll up one page | **No conflict.** |
| Enter | Submit composer / activate selected | **Conflict:** `shell.gsx:291` maps Enter to `submitComposer()` but only when `FocusComposer` is active. **Acknowledged:** same binding, same purpose. In the new design, Enter also activates the selected entity when not in the composer. |
| `e` | Open evidence drawer for selected row | **No conflict.** |
| `1` | Switch to agent inventory screen | **No conflict.** The rune `1` is not bound in keys.go or shell.gsx. |
| `2` | Switch to rooms screen | **No conflict.** The rune `2` is not bound. |
| Ctrl+M | Memory tab (in evidence drawer) | **Conflict:** `shell.gsx:273` maps Ctrl+M to `setRail(RailMemory)`. **Acknowledged:** same binding, same purpose. Ctrl+M opens the Memory surface, now inside the Evidence drawer rather than the right rail. |
| Ctrl+Y | Continuity tab (in evidence drawer) | **Conflict:** `shell.gsx:274` maps Ctrl+Y to `setRail(RailContinuity)`. **Acknowledged:** same binding, same purpose. Moves from rail tab to evidence drawer sub-tab. |
| Ctrl+W | Workers view (in main lane) | **Conflict:** `shell.gsx:275` maps Ctrl+W to `setRail(RailWorkers)`. **Resolution:** Workers are now first-class rows in the agent inventory (Main lane), not a rail tab. Ctrl+W filters/focuses the inventory to show workers. |
| Ctrl+B | Task/board screen | **Conflict:** `shell.gsx:276` maps Ctrl+B to `setRail(RailTask)`. **Resolution:** Task/board becomes a secondary screen reachable via `Ctrl+B`, not a rail tab. |
| Ctrl+X | Cancel in-flight ask | **Conflict:** `shell.gsx:299` conditionally maps Ctrl+X to `submitCancel()` when `enqueueCancel` is non-nil. **Acknowledged:** same binding, same purpose. |
| Ctrl+L | Focus transcript / main lane | **No conflict.** The prior plan at [go-tui-agent-shell.md](../go-tui-agent-shell.md) recommends Ctrl+L for transcript focus, but `keys.go` and `shell.gsx` do not bind it. |
| Ctrl+J | Focus composer / detail lane | **No conflict.** Recommended in [go-tui-agent-shell.md](../go-tui-agent-shell.md) but not bound in the current code. |

### Conflict Summary

All conflicts are **acknowledged** — none are unaccounted for:

1. **ESC** — shifts from hard quit to context-sensitive back (hard quit
   retained on `q`/Ctrl+C).
2. **Tab / Shift-Tab** — retained as focus-cycle; pane count reduces from 4
   to 3.
3. **Ctrl+M, Ctrl+Y** — retained; surface location moves from rail tab to
   evidence drawer sub-tab.
4. **Ctrl+W** — retained; semantic changes from rail tab to inventory filter.
5. **Ctrl+B** — retained; semantic changes from rail tab to secondary screen.
6. **Ctrl+X** — retained; same cancel purpose.
7. **Enter** — retained; extended to also activate selected entities.
8. **`q`, Ctrl+C** — retained identically.

---

## (g) Screen Inventory

| Screen | Main Lane | Detail Lane | Evidence Lane | Navigation |
|--------|-----------|-------------|---------------|------------|
| **Agent Inventory** (default) | Agent list with status, role, workspace | Empty state or selected agent detail + composer | Collapsed (drawer available via `e`) | `1` or default on launch |
| **Agent Detail** (inline) | Agent list (selection highlighted) | Selected agent: runtime, hierarchy, transcript, composer | Drawer for raw payloads | Auto-updates on selection change |
| **Ask/Chat** (inline) | Agent list | Streaming transcript + active composer | Drawer for stream evidence | Auto-enters when composer is submitted |
| **Rooms Directory** | Room list with membership summary | Empty state or selected room detail | Collapsed | `2` from any screen |
| **Loading** | Loading spinner + "Connecting to daemon at \<url\>…" | Empty | Collapsed | Auto-transitions on success/failure |
| **Connection Error** | Error message + URL + retry hint | Empty | Collapsed | Auto-transitions if daemon recovers |
| **Empty Daemon** | "No agents running. Spawn one: `foxctl agent spawn --role researcher`" | Empty state | Collapsed | Auto-updates on live refresh |
| **Too Small** | Full-screen guard: "Terminal too small — resize to ≥60×15" | (hidden) | (hidden) | ESC to quit |

---

## (h) Error / Loading / Empty State Mapping Per Screen

| Screen | Loading State | Empty State | Error State |
|--------|--------------|-------------|-------------|
| Agent Inventory | "Connecting to daemon at `<url>`…" with spinner. Rendered within 500ms. Transitions to inventory or error. | "No agents running." + CTA: `foxctl agent spawn --role <role>` + available roles. | "Cannot reach daemon at `<url>`." + last error + "Press `r` to retry or check that `foxctl web serve` is running." |
| Agent Detail | (hidden; detail loads with agent data from inventory) | "Select an agent from the inventory." + "Use ↓/↑ to navigate, Enter to open chat." | (rare; shown if agent detail fetch fails: "Failed to load agent detail: `<error>`. Press `r` to retry.") |
| Ask/Chat | "Sending question…" (inline in transcript, replaces composer briefly) | (not applicable; composer is always available) | "Ask failed: `<error>`." in transcript row. Retry by resubmitting. Cancelled state shown as "Cancelled" with distinct visual marker. |
| Rooms Directory | "Loading rooms…" | "No rooms found." + "Create a room with `foxctl room create`." | "Failed to load rooms: `<error>`. Press `r` to retry." |
| Evidence Drawer | "Loading raw payload…" | "No payload available for this row." | "Failed to load payload: `<error>`." |
| Status Footer | Connection status: "connecting…" (yellow) | Connection status: "connected" (green) | Connection status: "error" (red) |
| Too Small Guard | N/A | N/A | "Terminal too small — resize to ≥60×15" (full-screen guard). ESC exits. |

### State Design Rules

Per DESIGN.md "Accessibility And Quality Bar":

1. **Loading states** explain what is being prepared ("Connecting to daemon…",
   not just "Loading…").
2. **Empty states** include a next-action CTA.
3. **Error states** preserve nearby context (don't clear the inventory on a
   detail-fetch error).
4. **Recovery** — `r` to retry is available on error states; ESC to back out
   is always available.

---

## Progressive Reveal

Per DESIGN.md principle 4 ("Summary First, Raw Second"), the cockpit defaults
to concise summaries and makes raw payloads explicit and opt-in. This section
specifies the progressive reveal for four categories.

| Category | Summary Default (what the user sees without action) | User Action to Expose Raw | Raw View (what the user sees after action) |
|----------|------------------------------------------------------|---------------------------|---------------------------------------------|
| **(i) Transcript rows** | Single-line summary: speaker label, entry kind badge (ask/reply/tool/error/status), truncated text to fit width. Tool calls show tool name + "…" only. Errors show "⚠ <message>" truncated. | Press `e` on the selected row to open the Evidence drawer. | Full text content for replies; full JSON args + result for tool calls; full error code + message + stack trace for errors; complete metadata (correlation ID, timestamps). |
| **(ii) Tool activity** | Inline collapsed row in transcript: tool name + status badge (running ✓/✗/⏳) + one-line result summary. Arguments hidden. | Press `e` on the tool row, or press `→` to expand inline. | Full argument JSON (syntax-highlighted key/value pairs), full result JSON, duration, exit code. Scrollable within the drawer. |
| **(iii) Events** | Curated event list filtered to errors and notable signals (agent spawned, agent killed, connection state change). Each event is one line with timestamp + type + summary. | Press `e` on an event row, or navigate to Events in the Evidence drawer. | Full event JSON payload, including all metadata fields. Filterable by topic within the drawer. |
| **(iv) Errors** | Error badge in transcript rows and status footer: "⚠ <short message>". Error count shown in footer. | Press `e` on the error row, or open the Evidence drawer filtered to errors. | Full error: code, message, stack trace, HTTP status if applicable, request URL, correlation ID, suggested fix from `data.hint`. |

### Progressive Reveal Design Rules

1. **Never require raw payload reading** to understand basic state. The
   summary default must answer "what happened?" without opening the drawer.
2. **One-key access** — `e` on any row opens the evidence drawer. ESC closes.
3. **Drawer preserves context** — the drawer opens over the Evidence lane
   area, not replacing the Main or Detail lanes.
4. **No raw JSON as primary UI** — per DESIGN.md "What To Avoid": raw JSON is
   never the default rendering. It is always opt-in.

---

## Three-Plane Memory Model

Per [go-tui-agent-shell.md](../go-tui-agent-shell.md) section "Memory
Information Architecture", the shell must preserve separation between three
memory planes. These planes are not merged into one generic "memory" list.

### Companion Memory

**Definition:** per-agent conversation history — the reply-time layered
memory for one assistant or conversation. Used to inspect what the current
assistant remembers and what gets injected into the next turn.

**Cockpit surface:** exposed in the Evidence drawer under the "Memory" sub-tab
(accessible via Ctrl+M). Shows:
- **Injected Now:** the exact current layered memory context for the selected
  agent/session.
- **Search:** search persistent memory artifacts for the selected agent.
- **Stats:** turn count, hard-state count, episode count, evidence count,
  context token hint.

**Projection/heuristic labeling:** The "Injected Now" view is a
**projection** — it represents the companion service's assembled context at
a point in time, not a canonical record. Per DESIGN.md "Honest Surfaces"
principle, the UI labels this as *"Assembled context — what the model will
remember if you send a prompt right now."* The label makes explicit that this
is a point-in-time snapshot, not a durable store. Any heuristic ranking of
memory entries (e.g., relevance scoring) is shown with the score and labeled
*"Relevance score (heuristic)"* rather than presented as ground truth.

### Named Durable Memory

**Definition:** workspace-scoped long-term memory stored in `memory.db`. Used
for gotchas, decisions, summaries, and semantic lookup across prior durable
artifacts. Independent of any single agent or conversation.

**Cockpit surface:** exposed in the Evidence drawer under a "Durable Memory"
sub-section within the Memory tab. Shows:
- Search across all workspace memory entries.
- List of recent entries by type (gotcha, decision, summary, anti-pattern).
- Stats: total entries, types breakdown, oldest/newest.

**Projection/heuristic labeling:** Search results are ranked by semantic
similarity, which is a **heuristic**. Per DESIGN.md "Honest Surfaces," the
UI labels search results with *"Similarity: `<score>`"* and shows the match
type (semantic, exact, tag) explicitly. The UI does not present ranked
results as if they are an authoritative or canonical ordering. Each result
shows the entry's source session ID and creation timestamp so the operator
can assess reliability.

### ACA / Continuity

**Definition:** active-work continuity layer — task continuity, handoffs,
observations/tensions, top-of-mind summaries, and family-history summaries.
This is workspace-level continuity, not conversational memory.

**Cockpit surface:** exposed in the Evidence drawer under the "Continuity"
sub-tab (accessible via Ctrl+Y). Shows:
- Top-of-mind summary for the current workspace.
- Task-history summary.
- Latest handoff record.
- Open tensions and observations.
- Optional family-history summary for the repo/workstream.

**Projection/heuristic labeling:** The top-of-mind summary and
task-history summary are **projected artifacts** assembled by the ACA
(Agent Context Architecture) layer. Per DESIGN.md "Honest Surfaces," the
UI labels these as *"ACA summary — synthesized from session timeline and
memory artifacts"* to distinguish them from raw, canonical records. Handoff
records are canonical (they are stored artifacts), and the UI labels them
as *"Stored handoff"* without any projection qualifier. Tensions and
observations are labeled *"From ACA observations"* when they are
machine-extracted and *"Operator-logged"* when manually entered, ensuring
the provenance of each item is visible.

---

## Reconciliation

This section explicitly addresses how the three-lane model relates to the
prior four-region shell and how DESIGN.md principles map to concrete decisions
in this information architecture.

### Relationship to go-tui-agent-shell.md

The canonical plan at [go-tui-agent-shell.md](../go-tui-agent-shell.md)
defines a four-region shell: top bar (assistant metadata), center transcript,
bottom composer, and right rail (tabbed: Memory, Continuity, Workers, Task).
The recommended focus keys include Ctrl+L (transcript), Ctrl+J (composer),
Ctrl+M (memory), Ctrl+Y (continuity), Ctrl+W (workers), Ctrl+B (task).

The three-lane model **layers on top of** the four-region shell by:

1. **Adopting** the Memory and Continuity surfaces as Evidence-drawer sub-tabs,
   preserving the conceptual separation the canonical plan defines.
2. **Superseding** the spatial layout — transcript and composer move from
   global center/bottom to the Detail lane, contextualized to the selected
   entity. This addresses the audit finding that the current layout is
   foreground-assistant-centric rather than entity-centric.
3. **Layering** the top bar's operational status into a footer, which is
   always visible across all screens, and moving entity metadata into the
   Detail lane header.

The canonical plan's recommended keybindings (Ctrl+L/J/M/Y/W/B) are preserved
where possible. Ctrl+L and Ctrl+J are newly bound (they are not in keys.go
or shell.gsx today). Ctrl+M/Y/W/B are acknowledged conflicts that retain
their semantic purpose.

### DESIGN.md Principle Mapping

| DESIGN.md Principle | IA Decision |
|---------------------|-------------|
| **1. Runtime First** | Default screen is live agent inventory, not a blank surface or explorer. Loading state within 500ms. Errors surfaced early. |
| **2. Main Lane, Detail Lane, Evidence Lane** | Layout is structured as three lanes. Evidence is collapsed by default and does not compete with Main/Detail. |
| **3. Main Message, Threaded Detail** | Transcript rows show concise summaries. Tool-call floods are collapsed. Verbose detail lives in the evidence drawer, not inline. |
| **4. Summary First, Raw Second** | Progressive reveal section ensures all four categories (transcript, tool activity, events, errors) default to summaries. Raw payloads are opt-in via `e` key. |
| **5. Multi-Agent Work Is Coordinated, Not Collapsed** | Agent inventory shows parent/child relationships as explicit columns. Workers are first-class rows, not a separate tab. Hierarchy is visible in the Detail lane. |
| **6. Honest Surfaces** | Three-plane memory model labels projections as "Assembled context," similarity scores as "Relevance score (heuristic)," and ACA summaries as "synthesized." Canonical records (handoffs, operator-logged items) are labeled as such. |

---

## Minimum Terminal Size

The minimum supported terminal size is **60 columns × 15 rows**.

At sizes below this threshold:
- The entire screen shows: "Terminal too small — resize to ≥60×15"
- ESC exits with code 0.
- Normal rendering resumes when the terminal grows above the minimum.

At the minimum size (60×15):
- Main lane: ~24 columns (agent ID + role + status)
- Detail lane: ~21 columns (title + truncated detail)
- Evidence lane: collapsed (drawer overlays when opened)
- Status footer: 1 row

---

## Status Footer

The status footer is always visible (1 row at the bottom) and contains:

1. **Connection status** — icon + label: `● connected` (green) /
  `◐ connecting…` (yellow) / `○ error` (red).
2. **Active entity** — e.g., `agent: abc12345 (researcher)` or
  `room: my-room`.
3. **Keybinding hints** — compact strip showing 3+ current bindings:
  `Tab:focus  ↑↓:nav  Enter:submit  e:evidence  Ctrl+X:cancel`.

Per DESIGN.md "Interaction" guidance: keyboard-friendly, drawers preferred
over full route jumps, context preserved during drilldown.
