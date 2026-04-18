# Component Spec — TUI Operator Cockpit

Per-widget contract for every M2 widget. Each widget section specifies 10
required elements: (1) props, (2) state, (3) variants, (4) keyboard behavior
as an explicit key table, (5) focus behavior, (6) testing expectations
(MockTerminal + tuistory), (7) visible focus indicator requirement, (8)
empty-state copy guidance, (9) loading-state copy guidance, (10) error-state
behavior.

This document satisfies **VAL-DOCS-005**.

Cross-references:
- [information-architecture.md](information-architecture.md) — layout, navigation, keybindings, focus model
- [research-go-tui.md](research-go-tui.md) — go-tui API reference (Component, State, Element, KeyMap, MockTerminal)
- [audit-current-tui.md](audit-current-tui.md) — audit findings driving widget design
- [DESIGN.md](../../../DESIGN.md) — product shape and visual interaction rules

---

## EntityList

A scrollable, focusable, selectable list of typed entities (agents, rooms,
events). This is the primary interactive surface in the Main lane. It replaces
the current flat transcript list with an entity-centric model per DESIGN.md
principle 5 ("Multi-Agent Work Is Coordinated, Not Collapsed").

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Items` | `[]Entity` | yes | — | Typed entity rows to render. Each `Entity` has `ID`, `Label`, `SubLabel`, `StatusBadge`, `Detail` fields. |
| `Width` | `int` | yes | — | Available width in cells. |
| `Height` | `int` | yes | — | Available height in cells. |
| `SelectedIndex` | `int` | no | `-1` | Index of the currently selected row. `-1` means no selection. |
| `Focused` | `bool` | no | `false` | Whether the list has focus. |
| `Loading` | `bool` | no | `false` | If true, renders LoadingState instead of list rows. |
| `Empty` | `bool` | no | `false` | If true and `Loading` is false and `Items` is empty, renders EmptyState. |
| `EmptyMessage` | `string` | no | (see §8) | Override empty-state copy. |
| `ErrorMessage` | `string` | no | `""` | If non-empty, renders error state instead of list. |
| `WrapAround` | `bool` | no | `true` | Whether navigation wraps from last to first and vice versa. |
| `OnSelect` | `func(index int)` | no | `nil` | Callback fired when the selected index changes. |
| `OnActivate` | `func(index int)` | no | `nil` | Callback fired when the user activates (Enter) a row. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `selectedIndex` | `int` | EntityList | Currently highlighted row index. Updated by keyboard navigation and `OnSelect`. |
| `scrollOffset` | `int` | EntityList | First visible row index. Adjusted to keep `selectedIndex` in view. |
| `focused` | `bool` | Parent | Focus state managed externally; EntityList reads it from props. |

EntityList holds **no entity data** — it receives `Items` as a prop and renders them. This follows the single-writer state ownership invariant from the architecture decisions doc (`docs/plans/tui-redesign/architecture.md`, forthcoming).

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Default** | `Loading=false`, `Empty=false`, `ErrorMessage=""` | Standard scrollable list. |
| **Loading** | `Loading=true` | Renders [LoadingState](#loadingstate) inline. List rows hidden. |
| **Empty** | `Loading=false`, `len(Items)==0`, `ErrorMessage=""` | Renders [EmptyState](#emptystate) inline. |
| **Error** | `ErrorMessage!=""` | Renders error message with retry hint. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| ↓ / `j` | Move selection down one row. If `WrapAround=true` and at bottom, wrap to index 0. | `Focused=true`, `Loading=false`, `Empty=false`, `ErrorMessage=""` |
| ↑ / `k` | Move selection up one row. If `WrapAround=true` and at top, wrap to last index. | Same |
| Home / `g` | Jump to first row (index 0). | Same |
| End / `G` | Jump to last row (index `len(Items)-1`). | Same |
| PageDown / Ctrl+D | Move selection down by `min(Height, len(Items)-selectedIndex)` rows. Clamp to last row. | Same |
| PageUp / Ctrl+U | Move selection up by `min(Height, selectedIndex)` rows. Clamp to first row. | Same |
| Enter | Fire `OnActivate(selectedIndex)`. If `selectedIndex==-1`, no-op. | Same |
| `e` | Open evidence drawer for the selected row. | Same |

**Wrap-around behavior:** when `WrapAround=true`, pressing ↓ on the last row selects index 0, and pressing ↑ on the first row selects the last index. When `WrapAround=false`, navigation clamps at boundaries (no movement).

### (5) Focus Behavior

- When `Focused=true`, the list accepts keyboard input for navigation.
- When `Focused=false`, all keyboard events are ignored (no navigation, no selection change).
- Focus state is externally managed (parent lane controls focus cycling).
- On focus gain, the `selectedIndex` is preserved — no auto-jump.
- The selected row is always kept visible: if `selectedIndex < scrollOffset`, `scrollOffset` is set to `selectedIndex`; if `selectedIndex >= scrollOffset + Height`, `scrollOffset` is set to `selectedIndex - Height + 1`.

### (6) Testing Expectations

**MockTerminal tests (table-driven):**

| Test case | Asserts |
|-----------|---------|
| `TestNavigateDown` | ↓ moves selection from index 0 → 1 → 2. |
| `TestNavigateUp` | ↑ moves selection from index 2 → 1 → 0. |
| `TestWrapAround` | At last item, ↓ wraps to 0; at first item, ↑ wraps to last. |
| `TestNoWrapAround` | `WrapAround=false`: at last item, ↓ stays; at first, ↑ stays. |
| `TestHomeEnd` | Home jumps to 0; End jumps to last. |
| `TestPageDownPageUp` | PageDown advances by Height rows; PageUp retreats by Height. |
| `TestFocusIndicator` | Raw cell buffer shows colored left border or inverse background on focused list (not just font-weight). |
| `TestUnfocusedIgnoresKeys` | When `Focused=false`, ↓ does not change `selectedIndex`. |
| `TestScrollKeepsSelectionVisible` | After jumping to a row outside the viewport, `scrollOffset` adjusts. |
| `TestEmptyRendersEmptyState` | With 0 items, renders EmptyState. |
| `TestLoadingRendersLoadingState` | With `Loading=true`, renders LoadingState. |
| `TestErrorRendersError` | With `ErrorMessage="fail"`, renders error text. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `entitylist-focused` | Focused list with 5 items, index 2 selected. Visible focus indicator (colored left border). |
| `entitylist-unfocused` | Same list, unfocused. No colored border; selected row has subtle background. |
| `entitylist-empty` | Empty list with EmptyState visible. |

### (7) Visible Focus Indicator Requirement

The focused EntityList MUST display a visible focus indicator that is **NOT
font-weight only**. Acceptable indicators:

- **Colored left border** (2-cell-wide colored bar on the left edge of each visible row).
- **Inverse background** on the selected row (foreground/background swap).
- **Colored background** (e.g., theme token `ColorSurfaceSelected`) on the selected row.

Unacceptable: bold text alone, underline alone on a single row, or any
indicator that would be indistinguishable at a glance from an unfocused list
on common terminal themes (dark background, no custom styling).

### (8) Empty-State Copy Guidance

When the entity list has zero items:

- **Agent inventory:** "No agents running." + CTA: "Spawn one: `foxctl agent spawn --role <role>`" + list of available roles (researcher, coder, planner, reviewer, overseer).
- **Rooms directory:** "No rooms found." + CTA: "Create a room with `foxctl room create`."
- **Generic:** "No items." + CTA from parent context.

The empty-state copy must always include a next-action CTA per DESIGN.md
"Accessibility And Quality Bar" ("empty states with action").

### (9) Loading-State Copy Guidance

- **Agent inventory:** "Connecting to daemon at `<url>`…" with a spinner character (e.g., ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏).
- **Rooms directory:** "Loading rooms…"
- **Generic:** "Loading…" with a spinner.

Loading copy must explain what is being prepared (not just "Loading…") per
[information-architecture.md](information-architecture.md) §(h) state design
rules.

### (10) Error-State Behavior

When `ErrorMessage` is non-empty:

1. Render the error message in place of the list rows.
2. Error text includes: the error message, and a retry hint ("Press `r` to retry").
3. The list does **not** clear `selectedIndex` or `Items` — error state is
   overlaid, not destructive. On retry/fresh data, the previous state is
   restored if still valid.
4. ESC continues to work (back/quit).
5. The error state uses a distinct color (theme token `ColorStatusError`).

---

## DetailPane

A scrollable detail view for the selected entity (agent, room, event). Renders
a header with title and status badge, sectioned body content, and integrates
EmptyState when no entity is selected. This is the primary surface in the
Detail lane per DESIGN.md principle 2 ("Main Lane, Detail Lane, Evidence Lane").

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Title` | `string` | yes | — | Header title. Truncated with ellipsis at width boundary. |
| `Status` | `StatusVariant` | no | `StatusNone` | Status badge variant for the header. |
| `Sections` | `[]Section` | yes | — | Body sections. Each `Section` has `Title` and `Lines []string`. |
| `Width` | `int` | yes | — | Available width in cells. |
| `Height` | `int` | yes | — | Available height in cells (header + body). |
| `Focused` | `bool` | no | `false` | Whether the pane has focus. |
| `HasEntity` | `bool` | no | `true` | If false, renders empty state instead of content. |
| `ScrollOffset` | `int` | no | `0` | Vertical scroll position for the body. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `scrollOffset` | `int` | DetailPane | Vertical scroll position of the body area. Header is always visible. |
| `focused` | `bool` | Parent | Focus state managed externally. |

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Populated** | `HasEntity=true`, `len(Sections)>0` | Full header + scrollable sections. |
| **Empty entity** | `HasEntity=true`, `len(Sections)==0` | Header visible, body shows "No details available." |
| **No entity** | `HasEntity=false` | Renders [EmptyState](#emptystate) with guidance copy. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| ↓ / `j` | Scroll body down 1 line. | `Focused=true`, `HasEntity=true`, content overflows viewport |
| ↑ / `k` | Scroll body up 1 line. | Same |
| PageDown / Ctrl+D | Scroll body down by `bodyHeight` lines. | Same |
| PageUp / Ctrl+U | Scroll body up by `bodyHeight` lines. | Same |
| Home / `g` | Scroll to top of body (`scrollOffset=0`). | Same |
| End / `G` | Scroll to bottom of body. | Same |
| `e` | Open evidence drawer for the current context. | `HasEntity=true` |

Scrolling only activates when body content exceeds the visible area. When
content fits within the viewport, scroll keys are no-ops.

### (5) Focus Behavior

- When `Focused=true`, scroll keys are active and a visible focus indicator
  appears (see §7).
- When `Focused=false`, no scroll keys are processed.
- Focus does not auto-scroll or change `scrollOffset` on gain.
- Scroll position is clamped: `0 ≤ scrollOffset ≤ max(0, totalContentHeight - bodyHeight)`.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestHeaderRender` | Header shows title and status badge. |
| `TestTitleTruncation` | Title longer than `Width` is truncated with `…`. |
| `TestSectionSeparators` | Sections are separated by visible horizontal rules or spacing. |
| `TestBodyScroll` | Content exceeding viewport height scrolls via ↓/↑. |
| `TestScrollIndicator` | When scrolled, a scroll indicator (e.g., `▾` or `│` scrollbar marker) is visible. |
| `TestEmptyNoEntity` | `HasEntity=false` renders EmptyState. |
| `TestEmptyNoSections` | `HasEntity=true`, `Sections=[]` renders "No details available." |
| `TestFocusIndicator` | Focused pane shows visible border indicator. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `detailpane-populated` | DetailPane with header, 3 sections, content visible. |
| `detailpane-empty` | No entity selected; EmptyState visible. |
| `detailpane-scrolled` | Content scrolled; scroll indicator visible. |
| `detailpane-truncated-title` | Title exceeds width; ellipsis visible. |

### (7) Visible Focus Indicator Requirement

The focused DetailPane MUST display a visible focus indicator that is **NOT
font-weight only**. Acceptable indicators:

- **Colored top border** (1-cell-high colored line above the header).
- **Inverse header background** (foreground/background swap on the header row).
- **Colored left border** (2-cell-wide colored bar along the left edge).

The indicator must be distinct from the unfocused state at a glance. Bold text
alone on the title is insufficient.

### (8) Empty-State Copy Guidance

When `HasEntity=false`:

- **Agent detail:** "Select an agent from the inventory or spawn one." + "Use ↓/↑ to navigate, Enter to open chat."
- **Room detail:** "Select a room to view details."
- **Generic:** "Select an item to view details."

Per [information-architecture.md](information-architecture.md) §(h), the
empty-state copy includes keyboard guidance so the operator knows what to do
next.

### (9) Loading-State Copy Guidance

- "Loading `<entity-type>` details…" with a spinner character.
- Loading state for agent detail: "Loading agent details…"
- Loading state for room detail: "Loading room details…"

Loading state is rare for DetailPane (data typically arrives with the entity
selection), but must be supported for slow API calls (e.g., agent runtime
snapshot via `GET /api/agents/{id}`).

### (10) Error-State Behavior

When the entity detail fetch fails:

1. The header still renders (with the entity title if known).
2. The body shows: "Failed to load details: `<error>`." + "Press `r` to retry."
3. Error state does not clear `Title` or `Sections` — previous content is
   preserved behind the error overlay if available.
4. The error text uses theme token `ColorStatusError`.
5. ESC and other navigation keys continue to work.

---

## Tabs

A tabbed header that allows switching between named views. Used within the
Evidence drawer for sub-tabs (Memory, Continuity) and for other grouped
content. Per DESIGN.md "Interaction" guidance: keyboard-friendly navigation
is required.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Labels` | `[]string` | yes | — | Tab labels. At least 1 label required. |
| `ActiveIndex` | `int` | no | `0` | Currently active tab index. Clamped to `[0, len(Labels))`. |
| `Width` | `int` | yes | — | Available width in cells. |
| `Focused` | `bool` | no | `false` | Whether the tabs bar has focus. |
| `OnChange` | `func(index int)` | no | `nil` | Callback fired when the active tab changes. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `activeIndex` | `int` | Tabs | Currently active tab. Updated by keyboard or `OnChange`. |
| `focused` | `bool` | Parent | Focus state managed externally. |

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Single tab** | `len(Labels)==1` | Active indicator shown; navigation keys are no-ops. |
| **Multiple tabs** | `len(Labels)>1` | Full tab bar with active indicator. Navigation active. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| → / `l` | Move to next tab. If at last tab and `WrapAround=true`, wrap to first. | `Focused=true`, `len(Labels)>1` |
| ← / `h` | Move to previous tab. If at first tab and `WrapAround=true`, wrap to last. | Same |
| Tab | Move to next tab (forward cycle). | Same |
| Shift+Tab | Move to previous tab (reverse cycle). | Same |
| Home / `g` | Jump to first tab. | Same |
| End / `G` | Jump to last tab. | Same |

**Wrap-around behavior:** Tab navigation wraps by default. At the last tab,
→ wraps to the first. At the first tab, ← wraps to the last.

### (5) Focus Behavior

- When `Focused=true`, tab navigation keys are active.
- When `Focused=false`, all keyboard events pass through (no tab changes).
- Focus is indicated by the active tab indicator becoming more prominent (see §7).

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestForwardNavigation` | → moves active index 0 → 1 → 2. |
| `TestBackwardNavigation` | ← moves active index 2 → 1 → 0. |
| `TestWrapForward` | At last tab, → wraps to index 0. |
| `TestWrapBackward` | At first tab, ← wraps to last index. |
| `TestShiftTab` | Shift+Tab moves backward. |
| `TestHomeEnd` | Home jumps to first; End jumps to last. |
| `TestSingleTabNoop` | With 1 tab, → is a no-op. |
| `TestActiveIndicatorVisible` | Raw cell buffer shows active tab with distinct indicator beyond bold. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `tabs-active-focused` | Tabs bar focused; active tab has underline + colored background. Inactive tabs are plain. |
| `tabs-active-unfocused` | Same content, unfocused; active tab still indicated but less prominent. |

### (7) Visible Focus Indicator Requirement

The active tab MUST have a visible indicator that is **NOT font-weight only**.
Acceptable indicators:

- **Colored underline** (1-cell-high colored line below the active tab label).
- **Colored background** (theme token `ColorSurfaceActive` on the active tab).
- **Inverse video** (foreground/background swap on the active tab label).

The indicator must be distinguishable in the raw cell buffer beyond just bold
text. Inactive tabs must be clearly less prominent than the active tab.

When `Focused=true`, the active tab indicator may become more prominent (e.g.,
brighter color or thicker underline) compared to the unfocused state.

### (8) Empty-State Copy Guidance

Tabs widget does not have an empty state in the conventional sense. If
`Labels` is empty, the widget renders nothing (zero height). This is the
correct behavior — an empty Tabs bar adds no value.

### (9) Loading-State Copy Guidance

Tabs has no loading state. Tab labels are always synchronously available from
props. Loading behavior belongs to the content displayed below the tab bar,
not the tabs themselves.

### (10) Error-State Behavior

Tabs has no error state. Errors in the tab content are handled by the content
panels, not the tab bar. The tab bar always renders its labels.

---

## Drawer

A slide-over panel that opens on top of the right portion of the screen (Evidence
lane area). Used for raw payload inspection, memory surfaces, and evidence
viewing. Per DESIGN.md principle 3 ("Main Message, Threaded Detail"), the
drawer defaults to closed and opens on demand.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Open` | `bool` | no | `false` | Whether the drawer is currently open. |
| `Title` | `string` | yes | — | Drawer title shown in the header. |
| `Content` | `[]string` | yes | — | Lines of content to render in the scrollable body. |
| `Width` | `int` | no | `30` | Drawer width in cells when open. |
| `Height` | `int` | yes | — | Available height in cells. |
| `OnClose` | `func()` | no | `nil` | Callback fired when the drawer closes. Fires exactly once per open→close cycle. |
| `PreviouslyFocusedRef` | `*Element` | no | `nil` | Reference to the element that had focus before the drawer opened. Focus returns here on close. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `open` | `bool` | Drawer | Open/close state. Toggled by API or ESC. |
| `scrollOffset` | `int` | Drawer | Vertical scroll position for the body content. |
| `previouslyFocused` | `Element` | Drawer | Element to restore focus to on close. |

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Closed** | `Open=false` | Drawer is not rendered. No space consumed. |
| **Open** | `Open=true` | Drawer slides over the right portion. Content is scrollable. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| ESC | Close the drawer. Restore focus to `PreviouslyFocusedRef`. | `Open=true` |
| ↓ / `j` | Scroll content down 1 line. | `Open=true`, content overflows |
| ↑ / `k` | Scroll content up 1 line. | Same |
| PageDown / Ctrl+D | Scroll content down by visible height. | Same |
| PageUp / Ctrl+U | Scroll content up by visible height. | Same |
| Home / `g` | Scroll to top. | Same |
| End / `G` | Scroll to bottom. | Same |
| Tab | Cycle focus forward inside the drawer (no focus escape). | `Open=true` |
| Shift+Tab | Cycle focus backward inside the drawer (no focus escape). | `Open=true` |

**Focus trap:** When the drawer is open, Tab and Shift+Tab cycle **inside**
the drawer only. They do not move focus to lanes behind the drawer. This is a
focus trap per accessibility best practices.

### (5) Focus Behavior

- When the drawer opens, focus moves to the drawer body.
- When the drawer closes (ESC or API), focus returns to `PreviouslyFocusedRef`.
- If `PreviouslyFocusedRef` is nil, focus returns to the last focused lane.
- **Double-close safety:** calling `OnClose` or setting `Open=false` on an
  already-closed drawer is a no-op (no panic, no double-fire of `OnClose`).
- `OnClose` fires **exactly once** per open→close cycle.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestOpen` | Setting `Open=true` renders drawer content. |
| `TestCloseViaESC` | ESC on an open drawer calls `OnClose` and clears `Open`. |
| `TestFocusTrap` | Tab cycles inside the drawer; focus does not escape to the parent. |
| `TestFocusRestore` | After close, the previously-focused element receives focus. |
| `TestDoubleClose` | Closing an already-closed drawer is a no-op; `OnClose` not double-fired. |
| `TestScrollContent` | ↓/↑ scroll the body when content overflows. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `drawer-open` | Drawer open with content visible; title in header; close hint shown. |

### (7) Visible Focus Indicator Requirement

When the drawer is open:

- The drawer border (left edge) uses a **colored border** (theme token
  `ColorBorderFocus`) to indicate it is the focused surface.
- The drawer header shows a close hint ("ESC to close").
- The focused/active state of the drawer is visible through the border color,
  not through font-weight alone.

### (8) Empty-State Copy Guidance

- "No payload available for this row." — when the drawer is opened for a row
  that has no raw payload.
- "No data." — for generic empty drawer content.

### (9) Loading-State Copy Guidance

- "Loading raw payload…" with a spinner — when the drawer is opened for a row
  whose raw payload is being fetched asynchronously.

### (10) Error-State Behavior

When the drawer content fails to load:

1. The drawer header still renders (with the row title if known).
2. The body shows: "Failed to load payload: `<error>`."
3. The error text uses theme token `ColorStatusError`.
4. ESC still closes the drawer. Other navigation keys continue to work.
5. The drawer does not auto-close on error — the user must explicitly dismiss.

---

## StreamViewer

A scrollable, append-only content viewer for streaming text (token replies,
event streams). Implements sticky-bottom (follow-tail) behavior with automatic
disengage on scroll-up. This is used in the Detail lane for transcript
rendering and ask-stream token display.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Lines` | `[]string` | yes | — | Content lines to display. New lines are appended. |
| `Width` | `int` | yes | — | Available width in cells. |
| `Height` | `int` | yes | — | Available height in cells. |
| `Focused` | `bool` | no | `false` | Whether the viewer has focus. |
| `FollowTail` | `bool` | no | `true` | Whether auto-scroll to bottom is engaged. |
| `MaxLines` | `int` | no | `10000` | Maximum retained lines. Older lines are dropped on overflow. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `scrollOffset` | `int` | StreamViewer | First visible line index. |
| `followTail` | `bool` | StreamViewer | Whether follow-tail is engaged. |
| `lines` | `[]string` | Parent via props | Content lines. StreamViewer renders but does not own the data. |

**"At bottom" definition:** `followTail` is engaged when
`scrollOffset + Height >= len(Lines)`. The viewer is "at bottom" when the
last visible line is the last content line.

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Follow engaged** | `followTail=true` | New lines auto-scroll into view. Status indicator: "⋮" or "↓" at bottom. |
| **Follow disengaged** | `followTail=false` | Scroll position is fixed; new lines are appended but not scrolled to. Status indicator: "↑" or scroll position. |
| **Empty** | `len(Lines)==0` | Renders empty message. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| ↓ / `j` | Scroll down 1 line. If at bottom, no-op. | `Focused=true`, `len(Lines)>0` |
| ↑ / `k` | Scroll up 1 line. **Disengages follow-tail.** | Same |
| PageDown / Ctrl+D | Scroll down by `Height` lines. | Same |
| PageUp / Ctrl+U | Scroll up by `Height` lines. **Disengages follow-tail.** | Same |
| Home / `g` | Scroll to top. **Disengages follow-tail.** | Same |
| End / `G` | Scroll to bottom. **Re-engages follow-tail.** | Same |

**Follow-tail behavior:**

1. When follow-tail is engaged and new lines are appended, the viewer
   auto-scrolls so the last line is always visible.
2. When the user scrolls up by ≥1 line (via ↑, PageUp, Home), follow-tail
   **disengages**. The absolute scroll anchor (line index) is preserved across
   new item arrivals — the user's reading position does not jump.
3. When the user scrolls to the bottom (End, `G`), follow-tail **re-engages**.

### (5) Focus Behavior

- When `Focused=true`, scroll keys are active.
- When `Focused=false`, scroll keys are ignored; follow-tail state is
  preserved but no user-initiated scroll changes occur.
- Focus does not change scroll position on gain.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestFollowTailEngaged` | Start at bottom; append 10 lines; verify last line is visible. |
| `TestScrollUpDisengages` | Scroll up 1 line; verify `followTail=false`; append lines; verify scroll position unchanged. |
| `TestScrollAnchorPreserved` | Scroll to line 5; append 100 lines; verify line 5 is still the top visible line. |
| `TestEndReEngages` | Scroll up, then press End; verify `followTail=true` and bottom is visible. |
| `TestPageUpPageDown` | PageUp/PageDown move by Height lines. |
| `TestBurstNoDrops` | Append 1000 lines in a tight loop; verify final `Lines` slice has all 1000 items in order. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `streamviewer-follow-engaged` | Viewer at bottom; follow indicator visible; last line visible. |
| `streamviewer-scrolled-up` | Viewer scrolled up; follow disengaged; scroll position preserved. |

### (7) Visible Focus Indicator Requirement

The focused StreamViewer MUST display a visible focus indicator that is **NOT
font-weight only**. Acceptable indicators:

- **Colored left border** (2-cell-wide bar along the left edge).
- **Colored top/bottom border** of the viewer area.
- **Follow-tail status indicator** that changes color when focused vs unfocused.

### (8) Empty-State Copy Guidance

- "Waiting for output…" — when the viewer is active but no lines have arrived yet.
- "No content." — when the stream has ended with zero output.

### (9) Loading-State Copy Guidance

- "Connecting to stream…" with a spinner — when the stream connection is being
  established (e.g., during `POST /api/agents/{id}/ask-stream`).

### (10) Error-State Behavior

When the stream fails:

1. The viewer preserves all lines received so far.
2. An error indicator row is appended: "⚠ Stream error: `<error>`."
3. Follow-tail is force-engaged so the error row is visible.
4. The error row uses theme token `ColorStatusError`.
5. Scroll and navigation continue to work on the existing content.
6. Subsequent well-formed frames (if the stream recovers) render normally after
   the error indicator.

---

## EmptyState

A small, centered informational widget that renders when a list or panel has no
content. Used by EntityList, DetailPane, and other containers. Per DESIGN.md
"Accessibility And Quality Bar": empty states must include an action.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Message` | `string` | yes | — | Primary message (e.g., "No agents running."). |
| `CTA` | `string` | no | `""` | Call-to-action text (e.g., "Spawn one: `foxctl agent spawn --role <role>`"). |
| `Width` | `int` | yes | — | Available width in cells. |
| `Height` | `int` | yes | — | Available height in cells. |
| `Icon` | `string` | no | `""` | Optional icon character (e.g., "📋", "∅"). |

### (2) State

EmptyState is **stateless**. It renders entirely from props with no internal
state. This makes it trivially testable and deterministic.

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **With CTA** | `CTA != ""` | Message + CTA on next line. |
| **Without CTA** | `CTA == ""` | Message only. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| (none) | EmptyState does not handle any keys. It is a passive display widget. | Always |

EmptyState is not focusable. It is rendered inline by parent widgets
(EntityList, DetailPane) which handle keyboard routing.

### (5) Focus Behavior

EmptyState is **not focusable**. It is a leaf display widget embedded within
focusable parents. The parent widget retains focus even when displaying the
EmptyState.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestRenderMessage` | Message text appears centered in the widget area. |
| `TestRenderCTA` | CTA text appears below the message. |
| `TestRenderWithoutCTA` | Only message rendered; no extra blank line. |
| `TestWidthRespect` | Message wraps at `Width` boundary; no cell overflow. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `emptystate-with-cta` | EmptyState with message "No agents running." and CTA "Spawn one: foxctl agent spawn --role researcher". |

### (7) Visible Focus Indicator Requirement

Not applicable — EmptyState is not focusable and does not display a focus
indicator.

### (8) Empty-State Copy Guidance

This widget **is** the empty state. Copy guidelines for its `Message` and `CTA`
props:

- `Message`: factual statement about the absence (e.g., "No agents running.",
  "No rooms found.", "No payload available.").
- `CTA`: concrete next action (e.g., "Spawn one: `foxctl agent spawn --role <role>`",
  "Create a room with `foxctl room create`."). Must always include a
  next-action step per DESIGN.md.
- Avoid vague messages like "Nothing here" or "No data" without a CTA.

### (9) Loading-State Copy Guidance

Not applicable — EmptyState is not used during loading. Use [LoadingState](#loadingstate)
instead.

### (10) Error-State Behavior

Not applicable — EmptyState is not used during errors. Error states are handled
by the parent widget with error-specific rendering.

---

## LoadingState

A small, centered informational widget that renders during async operations.
Displays a spinner and explanatory message. Per [information-architecture.md](information-architecture.md) §(h): loading states must explain what is being prepared.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Message` | `string` | yes | — | Explanatory message (e.g., "Connecting to daemon at `http://localhost:8090`…"). |
| `Width` | `int` | yes | — | Available width in cells. |
| `Height` | `int` | yes | — | Available height in cells. |
| `SpinnerFrames` | `[]string` | no | `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` | Spinner character sequence. |

### (2) State

| State field | Type | Owner | Description |
|-------------|------|-------|-------------|
| `spinnerIndex` | `int` | LoadingState | Current frame index in `SpinnerFrames`. Advanced by a watcher timer. |

The spinner animation is driven by a go-tui `Watcher` (interval timer) that
increments `spinnerIndex` on each tick. The timer is started in the component's
`Init()` and cleaned up on unmount.

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Default** | Normal | Spinner + message, centered. |
| **Compact** | `Height <= 3` | Spinner + message on a single line; no vertical centering. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| (none) | LoadingState does not handle any keys. It is a passive display widget. | Always |

LoadingState is not focusable. The parent widget retains focus even when
displaying the LoadingState. This ensures ESC/quit keys remain functional
during loading (per VAL-SKEL-002).

### (5) Focus Behavior

LoadingState is **not focusable**. It is a leaf display widget embedded within
focusable parents. The parent widget retains focus and continues to handle ESC
and quit keys while the loading state is displayed.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestRenderMessage` | Message text appears centered. |
| `TestSpinnerFrame` | At `spinnerIndex=0`, first spinner frame character is rendered. |
| `TestSpinnerAdvances` | After watcher tick, `spinnerIndex` increments (wraps to 0). |
| `TestCompactMode` | When `Height <= 3`, content fits on one line. |
| `TestWidthRespect` | Message wraps at `Width` boundary; no cell overflow. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `loadingstate-default` | LoadingState with message "Connecting to daemon at http://localhost:8090…" and spinner. |

### (7) Visible Focus Indicator Requirement

Not applicable — LoadingState is not focusable and does not display a focus
indicator.

### (8) Empty-State Copy Guidance

Not applicable — LoadingState is not used for empty states. Use
[EmptyState](#emptystate) instead.

### (9) Loading-State Copy Guidance

This widget **is** the loading state. Copy guidelines for its `Message` prop:

- Must explain what is being prepared (e.g., "Connecting to daemon at `<url>`…",
  "Loading agents…", "Loading room details…").
- Must NOT use vague text like just "Loading…" without context.
- Should include the target URL or entity name when available.
- Per [information-architecture.md](information-architecture.md) §(h): loading
  states explain what is being prepared, not just that loading is happening.

### (10) Error-State Behavior

Not applicable — LoadingState does not render errors. When loading fails, the
parent widget transitions from displaying LoadingState to displaying an error
state. LoadingState itself has no error behavior.

---

## StatusBadge

A small inline badge that renders a status indicator with a distinct visual
style per variant. Used in EntityList rows, DetailPane headers, and the status
footer. Per DESIGN.md "Hierarchy": status color should be meaningful and sparse.

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Variant` | `StatusVariant` | yes | — | One of: `StatusOK`, `StatusWarn`, `StatusError`, `StatusPending`, `StatusNone`. |
| `Label` | `string` | yes | — | Text label (e.g., "running", "error", "pending"). |
| `Width` | `int` | no | `0` | If > 0, pad/truncate the badge to this width. If 0, auto-size. |

### (2) State

StatusBadge is **stateless**. It renders entirely from props with no internal
state.

### (3) Variants

| Variant | Color | Icon | Use case |
|---------|-------|------|----------|
| `StatusOK` | Green (`ColorStatusOK`) | `●` | Agent running, connection healthy, operation succeeded. |
| `StatusWarn` | Yellow (`ColorStatusWarn`) | `◐` | Agent idle, connection degraded, slow operation. |
| `StatusError` | Red (`ColorStatusError`) | `○` or `✗` | Agent error, connection failed, operation failed. |
| `StatusPending` | Blue/gray (`ColorStatusPending`) | `⏳` or `…` | Agent starting, loading, operation pending. |
| `StatusNone` | Default foreground | (none) | No status to display. Renders label only, no icon. |

Each variant MUST render with a **distinct ANSI color sequence** (not just
bold-vs-not-bold). The colors are defined as theme tokens in the theme package
(see the theme tokens decision in `docs/plans/tui-redesign/architecture.md`, forthcoming).

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| (none) | StatusBadge does not handle any keys. It is a passive display widget. | Always |

StatusBadge is not focusable and not interactive. It is an inline display
element within other widgets.

### (5) Focus Behavior

StatusBadge is **not focusable**. It is an inline element rendered within
EntityList rows, DetailPane headers, and the status footer.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestOK` | `StatusOK` renders green icon + label. Raw buffer has distinct ANSI green sequence. |
| `TestWarn` | `StatusWarn` renders yellow icon + label. Raw buffer has distinct ANSI yellow sequence. |
| `TestError` | `StatusError` renders red icon + label. Raw buffer has distinct ANSI red sequence. |
| `TestPending` | `StatusPending` renders blue/gray icon + label. Raw buffer has distinct ANSI blue/gray sequence. |
| `TestNone` | `StatusNone` renders label only, no icon. |
| `TestVariantsDistinct` | All four colored variants produce different ANSI sequences in the raw buffer. |
| `TestWidthPadding` | When `Width > 0`, badge is padded to width. |
| `TestWidthTruncation` | When label exceeds `Width`, truncated with `…`. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `statusbadge-ok` | Green `● running` badge. |
| `statusbadge-warn` | Yellow `◐ idle` badge. |
| `statusbadge-error` | Red `○ error` badge. |
| `statusbadge-pending` | Blue/gray `⏳ starting` badge. |

### (7) Visible Focus Indicator Requirement

Not applicable — StatusBadge is not focusable and does not display a focus
indicator.

### (8) Empty-State Copy Guidance

Not applicable — StatusBadge is not used for empty states.

### (9) Loading-State Copy Guidance

Not applicable — StatusBadge is not used for loading states.

### (10) Error-State Behavior

Not applicable — StatusBadge renders the `StatusError` variant for error
states, but does not have its own error behavior. It always renders from props.

---

## KeybindHint

A compact inline display of a keybinding hint (key + description). Used in the
status footer strip and as inline hints in empty states and help overlays. Per
DESIGN.md "Accessibility And Quality Bar": keybindings must be discoverable
(always-visible hint footer).

### (1) Props

| Prop | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `Key` | `string` | yes | — | Key label (e.g., "Tab", "↑↓", "Enter", "Ctrl+X"). |
| `Description` | `string` | yes | — | Brief action description (e.g., "focus", "nav", "submit"). |
| `Separator` | `string` | no | `":"` | Separator between key and description. |
| `Compact` | `bool` | no | `true` | If true, render as `Key:Desc` with minimal spacing. |

### (2) State

KeybindHint is **stateless**. It renders entirely from props with no internal
state.

### (3) Variants

| Variant | When | Visual difference |
|---------|------|-------------------|
| **Compact** | `Compact=true` | Single-line `Key:Desc` with minimal spacing. Used in footer strip. |
| **Expanded** | `Compact=false` | `Key` on one line, `Description` on the next. Used in help overlays. |

### (4) Keyboard Behavior

| Key | Action | When |
|-----|--------|------|
| (none) | KeybindHint does not handle any keys. It is a passive display widget. | Always |

KeybindHint is not focusable and not interactive. It is a display-only element.

### (5) Focus Behavior

KeybindHint is **not focusable**. It is a display-only element rendered in the
status footer and inline help.

### (6) Testing Expectations

**MockTerminal tests:**

| Test case | Asserts |
|-----------|---------|
| `TestCompactRender` | Renders `Tab:focus` with separator and compact spacing. |
| `TestExpandedRender` | Renders `Tab` on line 1, `focus` on line 2. |
| `TestCustomSeparator` | `Separator="→"` renders `Tab→focus`. |
| `TestWidthRespect` | Total rendered width does not exceed `len(Key)+len(Separator)+len(Description)`. |

**Tuistory snapshots:**

| Snapshot | Shows |
|----------|-------|
| `keybindhint-compact` | Compact hint strip with 3+ hints: `Tab:focus  ↑↓:nav  Enter:submit`. |

### (7) Visible Focus Indicator Requirement

Not applicable — KeybindHint is not focusable and does not display a focus
indicator.

### (8) Empty-State Copy Guidance

Not applicable — KeybindHint is not used for empty states.

### (9) Loading-State Copy Guidance

Not applicable — KeybindHint is not used for loading states.

### (10) Error-State Behavior

Not applicable — KeybindHint does not have error behavior. It always renders
from props.

---

## Cross-Cutting Rules

### Theme Tokens

All widgets MUST reference color values through the theme tokens package
(declared in `docs/plans/tui-redesign/architecture.md`, forthcoming). Raw color literals
(`tui.Cyan`, `tui.Red`, `"#..."`, etc.) MUST NOT appear in widget
implementation files. This ensures visual consistency and makes palette changes
single-point edits.

### Focus Indicator Rules

1. **Focusable widgets** (EntityList, DetailPane, Tabs, Drawer, StreamViewer)
   MUST display a visible focus indicator when focused.
2. **Non-focusable widgets** (EmptyState, LoadingState, StatusBadge, KeybindHint)
   are passive display elements and do not display focus indicators.
3. **Font-weight only is forbidden** as the sole focus indicator for any
   focusable widget. Acceptable: colored borders, inverse backgrounds, colored
   backgrounds, underlines. Unacceptable: bold text alone, subtle weight
   changes, or any indicator that would be ambiguous on common terminal themes.

### Testing Coverage

Every widget must have:

1. **MockTerminal unit tests** — table-driven, covering all keyboard behaviors,
   focus states, variants, and boundary conditions.
2. **Tuistory snapshot tests** — at least the snapshots listed in each widget's
   §6. Snapshots capture rendered output for visual regression.
3. **Raw cell buffer assertions** — for focus indicators and StatusBadge variant
   colors, tests must inspect the raw cell buffer to confirm the expected ANSI
   sequences are present (not just that the widget "didn't crash").

### Determinism

No widget implementation may call `time.Now()`, `rand.*`, or `os.Getenv` inside
render or state-reducer functions. Dependencies (clock, UUID, config) are
injected via constructor parameters. This follows the determinism invariant
from `docs/plans/tui-redesign/architecture.md` (forthcoming) and repo-root AGENTS.md.

### Unicode Width

EntityList, DetailPane, StreamViewer, and StatusBadge must correctly handle
wide characters (CJK, combining marks, ZWJ emoji, RTL) without panic, cell
overlap, or width miscalculation. See VAL-CMP-012 for the full unicode test
matrix.
