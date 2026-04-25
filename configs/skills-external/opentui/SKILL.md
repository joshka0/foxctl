---
name: opentui-tui-builder
description: "Build or review OpenTUI terminal interfaces, component patterns, theming, layout, keyboard flows, and polished TUI interactions."
---

# OpenTUI TUI Builder

Build terminal interfaces that feel fast, legible, trustworthy, keyboard-native, and respectful of the terminal.

A good TUI is not a web app squeezed into monospace. It is a command environment with visual memory, predictable focus, recoverable actions, excellent keyboard flow, and careful use of the terminal’s limited space.

Use OpenTUI’s rendering power to clarify workflows, not to decorate the terminal into noise.

## Core Mental Model

A terminal UI has different constraints from a graphical UI:

- The screen is made of cells, not pixels.
- Width and height are volatile.
- Users expect keyboard control.
- Mouse support is helpful but secondary.
- Color support and theme detection vary.
- Logs, stdout, stderr, and the app UI can collide.
- A broken cleanup path can leave the terminal in a bad state.
- Dense information is acceptable only when hierarchy remains obvious.
- The user must always know where focus is, what keys work, and how to escape.

The best TUI feels like a sharp instrument: minimal ceremony, immediate feedback, no ambiguity.

## Quick Reference

| Area | Use When |
| --- | --- |
| Renderer | Choosing `alternate-screen`, `main-screen`, or `split-footer`; cleanup; render loop |
| Layout | Panels, sidebars, responsive terminal sizing, status bars |
| Keyboard | Shortcuts, focus routing, forms, command palettes |
| Components | `Box`, `Text`, `Input`, `Select`, `ScrollBox`, `Code`, `Diff`, `Markdown` |
| Visual Design | Borders, color, hierarchy, density, icons, text attributes |
| Interaction | Focus states, help overlays, escape routes, async actions |
| Performance | Large scroll regions, live rendering, animations, log streams |
| Reliability | Ctrl+C, signals, errors, terminal restore, external output |

## Critical Rules

### 1. Choose the screen mode based on the job

Do not pick a renderer mode casually.

| Mode | Use For | Avoid When |
| --- | --- | --- |
| `alternate-screen` | Full-screen TUI apps, dashboards, editors, file pickers, durable interactive tools | The user needs previous scrollback visible |
| `split-footer` | Assistants, prompts, inline controls, progress panels below normal command output | The UI needs the full terminal |
| `main-screen` | Short-lived tools, demos, tests, simple inline interactions | You need scrollback-native rendering; OpenTUI still owns a region |

Default to `alternate-screen` for full applications.

Use `split-footer` when the TUI is attached to ongoing CLI output and must not overwrite logs.

```ts
const renderer = await createCliRenderer({
  screenMode: "alternate-screen",
  exitOnCtrlC: true,
  targetFps: 30,
})
````

For assistant-like tools:

```ts
const renderer = await createCliRenderer({
  screenMode: "split-footer",
  footerHeight: 14,
  externalOutputMode: "capture-stdout",
  exitOnCtrlC: true,
})
```

Never let normal `stdout.write` overlap the TUI accidentally. In split-footer mode, prefer captured stdout unless passthrough is explicitly intended.

### 2. Always restore the terminal

A TUI that leaves the terminal broken is not polished, no matter how good it looks.

Always call `renderer.destroy()` on clean exit, errors, and controlled shutdown paths.

```ts
const renderer = await createCliRenderer({ exitOnCtrlC: true })

process.on("uncaughtException", (error) => {
  console.error(error)
  renderer.destroy()
  process.exit(1)
})

process.on("unhandledRejection", (reason) => {
  console.error(reason)
  renderer.destroy()
  process.exit(1)
})
```

Rules:

* Do not rely on `process.exit()` alone.
* Do not leave raw mode, mouse tracking, alternate screen, or cursor state dirty.
* If handling Ctrl+C manually, disable the internal behavior intentionally and still destroy the renderer.
* Make `Esc`, `q`, or `Ctrl+C` behavior clear in the footer/help text.

### 3. Keyboard first, mouse second

Every important action must be reachable by keyboard.

Mouse support is an enhancement. Keyboard is the contract.

Default key conventions:

| Key                 | Expected Meaning                                            |
| ------------------- | ----------------------------------------------------------- |
| `Tab` / `Shift+Tab` | Move focus between regions or fields                        |
| `Up/Down`           | Move within vertical lists                                  |
| `Left/Right`        | Move across tabs, segmented controls, or horizontal choices |
| `j/k`               | Optional Vim-style vertical movement in lists               |
| `Enter`             | Confirm, submit, open, select                               |
| `Space`             | Toggle, mark, multi-select                                  |
| `Esc`               | Close overlay, cancel edit, go back one layer               |
| `?`                 | Show help                                                   |
| `/`                 | Search or filter                                            |
| `Ctrl+C`            | Exit or interrupt current operation                         |
| `Ctrl+L`            | Clear or refresh, if applicable                             |
| `Ctrl+R`            | Refresh/reload, if applicable                               |

Do not invent surprising shortcuts for common actions.

If a shortcut is destructive, require confirmation unless it is easily reversible.

### 4. Make focus impossible to miss

At every moment, the user must know:

1. which region is active
2. which item or field is selected
3. what pressing Enter will do
4. how to leave the current state

Good focus treatments:

* selected row background
* active panel border color
* cursor visible in focused input
* title or footer changes for active mode
* dimmed inactive regions
* explicit focus marker such as `›`, `●`, or `▸`

Bad focus treatments:

* only changing text color slightly
* using color with no shape/text fallback
* showing multiple “selected” states that compete
* hiding focus inside nested panels

Pattern:

```ts
Box(
  {
    border: true,
    borderStyle: "rounded",
    borderColor: isFocused ? theme.focus : theme.border,
    title: isFocused ? " Files — active " : " Files ",
    padding: 1,
  },
  ...
)
```

### 5. Use a small number of durable layout regions

Most good TUIs have a simple shell:

```text
┌ Header / title / context ───────────────────────────┐
│ Main content                                        │
│                                                     │
│                                                     │
├ Optional secondary/status region ───────────────────┤
│ Footer: keys, mode, status, errors                  │
└─────────────────────────────────────────────────────┘
```

Common layouts:

| Layout                   | Good For                               |
| ------------------------ | -------------------------------------- |
| Single column            | Forms, wizards, focused workflows      |
| Sidebar + detail         | File browsers, model pickers, settings |
| Top tabs + content       | Sectioned workflows                    |
| Split main + inspector   | Code, diffs, logs, previews            |
| Scrollbox + fixed footer | Chat, logs, command output             |
| Command palette overlay  | Global navigation and commands         |

Avoid more than three major regions unless the terminal is wide enough.

### 6. Design responsively for terminal size

Terminals are resized constantly. Treat layout as fluid.

Practical breakpoints:

|         Width | Layout Guidance                                           |
| ------------: | --------------------------------------------------------- |
|   `< 60 cols` | Single-column fallback; hide descriptions; shorten labels |
|  `60–89 cols` | Compact two-region layout only if necessary               |
| `90–119 cols` | Sidebar + main works well                                 |
|   `120+ cols` | Sidebar + main + inspector can work                       |
|   `< 20 rows` | Hide nonessential panels; keep footer minimal             |

React pattern:

```tsx
import { useTerminalDimensions } from "@opentui/react"

function App() {
  const { width, height } = useTerminalDimensions()
  const compact = width < 80 || height < 24

  return (
    <box
      style={{
        width: "100%",
        height: "100%",
        flexDirection: compact ? "column" : "row",
        gap: 1,
      }}
    >
      {!compact && <Sidebar />}
      <Main compact={compact} />
    </box>
  )
}
```

Never let a layout become unreadable just because it technically fits.

### 7. Use borders as structure, not decoration

Borders are expensive in terminal space. A border consumes visual attention and usually one cell on every side.

Use borders for:

* primary panels
* modal overlays
* focused regions
* grouped forms
* code/diff viewers
* high-stakes confirmation dialogs

Avoid borders for:

* every row
* every small button
* nested panels inside nested panels
* purely decorative emphasis
* content that whitespace can separate

Border style guidance:

| Style     | Use                                         |
| --------- | ------------------------------------------- |
| `single`  | Neutral panels, utility areas, dense apps   |
| `rounded` | Primary cards, modals, friendly tools       |
| `double`  | Rare; high-emphasis modal or app frame      |
| `heavy`   | Very rare; active/focused emphasis or alert |

If the screen feels noisy, remove borders before removing content.

### 8. Prefer whitespace and alignment over boxes

In terminals, good spacing often beats visual chrome.

Use:

* `padding: 1` for readable panels
* `gap: 1` between stacked controls
* aligned labels
* consistent left edges
* stable footer position
* fixed-width metadata columns

Bad:

```text
┌────┐ ┌────┐ ┌────┐
│ A  │ │ B  │ │ C  │
└────┘ └────┘ └────┘
```

Better:

```text
  A    B    C
────  ────  ────
```

### 9. Build a restrained semantic color system

Do not scatter raw colors through the app. Define a small theme.

```ts
const theme = {
  bg: "transparent",
  panel: "#1f2335",
  panelAlt: "#24283b",
  border: "#414868",
  borderMuted: "#303443",
  text: "#c0caf5",
  muted: "#7f849c",
  subtle: "#565f89",
  focus: "#7aa2f7",
  success: "#9ece6a",
  warning: "#e0af68",
  danger: "#f7768e",
  accent: "#bb9af7",
}
```

Rules:

* Color should reinforce hierarchy, not create rainbow noise.
* Never use color as the only signal.
* Use dim text for secondary information.
* Use strong colors only for focus, status, errors, and primary action.
* Avoid pure red/green reliance; include text or glyphs.
* Avoid blinking text except for rare terminal-native warnings. Usually do not use it.

### 10. Text attributes should be rare and meaningful

Available text styling can include bold, dim, italic, underline, inverse, hidden, strikethrough, and blink.

Use them sparingly:

| Attribute     | Good Use                                     |
| ------------- | -------------------------------------------- |
| Bold          | Active title, primary label, important value |
| Dim           | Metadata, inactive shortcut, placeholder     |
| Underline     | Link-like text only                          |
| Inverse       | Selection/focus in very dense views          |
| Strikethrough | Deleted item, diff removal, completed task   |
| Italic        | Rare; terminal support varies                |
| Blink         | Almost never                                 |

Bad TUI text is visually busy. Good TUI text has hierarchy.

### 11. Make copy/select behavior intentional

OpenTUI text can be selectable. Decide what should be copyable.

Usually selectable:

* logs
* code
* diffs
* output
* generated text
* file paths
* error messages

Usually not selectable:

* button labels
* decorative titles
* separators
* repeated row chrome

For labels and controls, disable selection when it interferes with interaction.

```ts
Text({
  content: "Submit",
  selectable: false,
})
```

### 12. Use labels, hints, and footers instead of making users guess

A good TUI teaches itself.

Every screen should answer:

* Where am I?
* What is selected?
* What can I do now?
* What keys matter here?
* How do I go back?
* Is work running, done, failed, or waiting?

Footer pattern:

```text
↑↓ move   Enter select   / search   ? help   Esc back   Ctrl+C quit
```

Do not show twenty shortcuts at once. Show the three to six that matter in the current state.

### 13. Help overlays should be contextual

A `?` help panel is one of the highest-value TUI features.

Good help overlay:

* grouped by current mode
* short labels
* visible exit instruction
* includes global shortcuts
* does not obscure critical state permanently

Example:

```text
┌ Help ───────────────────────────────┐
│ Navigation                          │
│  ↑/↓, j/k     Move selection         │
│  Enter        Open selected item     │
│  Esc          Back / close panel     │
│                                      │
│ Actions                             │
│  /            Filter list            │
│  r            Refresh                │
│  d            Delete selected item   │
│                                      │
│ Global                              │
│  ?            Toggle help            │
│  Ctrl+C       Quit                   │
└──────────────────────────────────────┘
```

### 14. Forms need explicit flow

Terminal forms become frustrating when focus and submission are unclear.

Rules:

* Show focused field clearly.
* `Tab` and `Shift+Tab` move between fields.
* `Enter` submits only when that is obvious.
* `Esc` cancels or exits edit mode.
* Validation errors appear near the relevant field.
* Required fields are marked textually, not only with color.
* Preserve typed values when validation fails.
* Do not log secrets.

Core construct pattern:

```ts
function LabeledInput(props: {
  id: string
  label: string
  placeholder: string
  width?: number
}) {
  return delegate(
    { focus: `${props.id}-input`, value: `${props.id}-input` },
    Box(
      { flexDirection: "row", gap: 1 },
      Text({
        content: props.label.padEnd(14),
        fg: theme.muted,
        selectable: false,
      }),
      Input({
        id: `${props.id}-input`,
        width: props.width ?? 30,
        placeholder: props.placeholder,
        backgroundColor: theme.panel,
        focusedBackgroundColor: theme.panelAlt,
        textColor: theme.text,
        cursorColor: theme.focus,
      }),
    ),
  )
}
```

### 15. Inputs should distinguish live input from committed changes

Use input semantics carefully:

| Event             | Meaning                                  |
| ----------------- | ---------------------------------------- |
| input / `onInput` | Every keystroke; filtering, live preview |
| change            | Committed value after blur or Enter      |
| enter / submit    | User intentionally submitted             |

Do not run expensive work on every keystroke unless it is debounced or cheap.

Good:

* live filter list on `onInput`
* validate or submit on Enter
* save committed config on blur/change

Bad:

* network request on every character
* destructive command triggered by accidental Enter
* hidden validation with no field-level message

### 16. Select lists need context, not just options

A good selector includes:

* title or prompt
* current selection
* optional descriptions
* scroll indicator if clipped
* filter/search for long lists
* preview pane for complex choices
* clear empty state

Use `Select` for vertical choices. It already supports common navigation such as Up/Down, `j/k`, fast scroll, and Enter selection.

Good option text:

```ts
{
  name: "Deploy production",
  description: "Build, upload, and promote the latest release"
}
```

Bad option text:

```ts
{ name: "prod", description: "" }
```

### 17. Scroll regions must reveal that they scroll

A scrollable area without scroll affordance feels broken.

Use a `ScrollBox` when content exceeds visible space.

Rules:

* Give scroll regions explicit height.
* Show a scrollbar or textual position when content is clipped.
* Use sticky scroll for logs and chat.
* Pause sticky behavior when the user scrolls away.
* Use viewport culling for large lists.
* Keep the footer outside the scroll region.

Log/chat pattern:

```ts
ScrollBox(
  {
    id: "logs",
    width: "100%",
    height: "100%",
    stickyScroll: true,
    stickyStart: "bottom",
    viewportCulling: true,
    verticalScrollbarOptions: {
      trackOptions: { backgroundColor: theme.borderMuted },
    },
  },
  ...logLines.map((line) =>
    Text({
      content: line,
      fg: theme.text,
      selectable: true,
    }),
  ),
)
```

### 18. Separate app UI from logs and external output

Do not let logging fight rendering.

Rules:

* Use the OpenTUI console overlay for debugging.
* In split-footer mode, capture stdout when output needs to appear above the footer.
* Do not use `console.log` as a substitute for app UI.
* Do not print progress with ordinary stdout while OpenTUI owns the same terminal region.
* Route debug logs away from production UI unless the user opens a console/debug panel.

### 19. Status bars should be boring and excellent

The status bar is not decoration. It is ambient state.

Good status bar includes:

* current mode
* current scope/path/context
* running operation
* error/success summary
* essential shortcuts

Pattern:

```ts
const statusBar = Box(
  {
    position: "absolute",
    bottom: 0,
    width: "100%",
    height: 1,
    backgroundColor: theme.panel,
    flexDirection: "row",
    justifyContent: "space-between",
    paddingLeft: 1,
    paddingRight: 1,
  },
  Text({ content: ` ${mode} `, fg: theme.focus, selectable: false }),
  Text({ content: `? help  Esc back  Ctrl+C quit`, fg: theme.muted, selectable: false }),
)
```

Keep status text short. If it wraps, it has failed.

### 20. Errors should be recoverable

A good TUI error explains:

1. what failed
2. whether data is safe
3. what the user can do next
4. which key retries or dismisses

Bad:

```text
Error: failed
```

Good:

```text
Connection failed. Your changes were not sent.
[r] Retry   [e] Edit config   [Esc] Back
```

Use red for danger, but pair it with text and action.

### 21. Async states should preserve layout

Loading should not cause the whole screen to jump.

Good async states:

* reserve the output area
* show a spinner only for short waits
* show progress/logs for long waits
* allow cancellation when appropriate
* keep the user’s current context visible
* show completion clearly

Avoid:

* blank screens
* fake progress bars
* constantly changing spinners in multiple regions
* hiding errors behind overlays

### 22. Animation should be almost invisible

Terminals are not great animation canvases. A small amount of motion can help, but constant animation can feel cheap or distracting.

Use animation for:

* progress bars
* short reveal of a panel
* active status pulse
* smooth width/number changes
* temporary emphasis after an action

Avoid animation for:

* normal navigation
* every focus move
* looping decorative effects
* large full-screen transitions
* anything that reduces readability

OpenTUI render-loop rule:

* Let automatic rendering handle static UI.
* Use continuous rendering only when necessary.
* For animations, call/request live rendering only for the duration of the animation, then release it.
* Keep `targetFps` modest, usually around `30`.

### 23. Use the timeline API for stateful motion, not ad-hoc timers

When animating numeric properties, prefer OpenTUI’s timeline hooks/APIs over scattered `setInterval`.

React pattern:

```tsx
import { useTimeline } from "@opentui/react"
import { useEffect, useState } from "react"

function ProgressBar({ target }: { target: number }) {
  const [width, setWidth] = useState(0)
  const timeline = useTimeline({ duration: 250, loop: false })

  useEffect(() => {
    timeline.add(
      { width },
      {
        width: target,
        duration: 250,
        ease: "linear",
        onUpdate: (animation) => {
          setWidth(animation.targets[0].width)
        },
      },
    )
  }, [target])

  return <box style={{ width, height: 1, backgroundColor: "#7aa2f7" }} />
}
```

Do not animate if a static update would be clearer.

### 24. Use React for app-like TUIs, Core for low-level control

Choose the implementation style intentionally.

| Approach               | Use When                                                                         |
| ---------------------- | -------------------------------------------------------------------------------- |
| `@opentui/react`       | App-like interfaces, stateful flows, familiar component structure                |
| Core constructs        | Small to medium apps, declarative composition without React                      |
| Imperative renderables | Low-level custom components, lifecycle-sensitive code, performance-critical code |
| Mixed                  | Most real apps: high-level declarative shell with low-level custom pieces        |

React is usually best for product-like TUIs with screens, state, hooks, and reusable components.

Core renderables are best when you need immediate access to methods, custom drawing, or fine lifecycle control.

### 25. Do not overuse absolute positioning

Absolute positioning is useful for:

* status bars
* overlays
* modals
* floating command palette
* fixed footer
* cursor-like indicators

Avoid it for normal layout. Prefer flexbox so the UI responds to terminal resize.

### 26. Keep command palettes simple

A command palette is often the fastest path through a TUI.

Good palette:

* opens with a predictable shortcut, often `Ctrl+P` or `:`
* filters as the user types
* supports Up/Down and Enter
* shows descriptions or shortcuts
* closes with Esc
* does not destroy the previous context
* can execute without mouse

Pattern:

```text
┌ Command ─────────────────────────────┐
│ > deploy                              │
│                                      │
│ › Deploy production      Ctrl+D       │
│   Deploy preview         Ctrl+Shift+D │
│   Show deploy logs                    │
└──────────────────────────────────────┘
```

### 27. Code and diffs deserve specialized views

For code, diffs, markdown, and diagnostics, use dedicated OpenTUI components where possible.

Rules:

* Keep code copyable.
* Preserve line numbers where helpful.
* Do not wrap code unless the mode clearly says so.
* Use horizontal scrolling for code panes when needed.
* Keep diagnostics near the relevant line.
* For diffs, color is helpful but signs are required: `+`, `-`, `~`.

### 28. Tables must degrade gracefully

Terminal tables often collapse poorly.

Rules:

* Prefer key-value lists on narrow screens.
* Truncate middle of long paths when needed.
* Right-align numbers.
* Keep units visible.
* Do not use too many vertical separators.
* Use dim text for secondary columns.
* Let users open a row for details instead of showing every field inline.

Good:

```text
NAME              STATUS     LATENCY   UPDATED
api               healthy       42ms   2m ago
worker           warning      180ms   8m ago
billing-sync     failed          —     12m ago
```

Bad:

```text
| name | status | latency | updated | region | version | pid | hash | ... |
```

### 29. Icons and Unicode must have fallbacks

Unicode can make a TUI feel elegant, but it can also break alignment.

Use cautiously:

* `›`, `▸`, `●`, `○`, `✓`, `✕`, `•`, `…`
* simple box drawing
* plain ASCII fallback if width/capability issues appear

Avoid:

* emoji in aligned tables
* ambiguous-width glyphs in dense layouts
* powerline glyphs unless the app explicitly targets patched fonts
* icons as the only label

Good:

```text
✓ Build completed
! Warning: config missing optional field
✕ Deploy failed
```

Better when compatibility matters:

```text
[ok] Build completed
[!] Warning: config missing optional field
[x] Deploy failed
```

### 30. Terminal theme support is uncertain

OpenTUI can detect terminal theme mode only when the terminal supports it. Design with fallback.

Rules:

* Always provide a default theme.
* Test dark and light terminals.
* Do not assume transparency looks good everywhere.
* Avoid low-contrast muted text.
* Keep semantic colors in a theme object.
* Let users override colors if the app is long-lived or professional.

### 31. Mouse support should never be required

Mouse can improve discoverability, but terminal users expect keyboard control.

Good mouse support:

* click to focus
* click to select
* hover only as a bonus
* pointer cursor where supported
* no hidden hover-only actions

Bad mouse support:

* actions only visible on hover
* drag interactions with no keyboard equivalent
* tiny clickable text with no focus state
* required mouse for navigation

### 32. Preserve user trust with secrets

Never leak secrets into visible text, logs, console overlays, stdout, snapshots, or crash traces.

Rules:

* Mask secrets if collecting them.
* Do not store secret input in app-level debug logs.
* Do not print environment variables wholesale.
* Do not copy secrets into error messages.
* If masking is not supported by the component you are using, implement or verify a safe masked input before collecting secrets.

### 33. Design empty states

Empty states in terminal apps are often neglected.

Good empty state:

```text
No deployments yet.

Run your first deployment:
  deploy production

[Enter] Create deploy   [?] Help   [Esc] Back
```

Bad empty state:

```text
[]
```

Empty states should be calm, useful, and action-oriented.

### 34. Use progressive disclosure

Do not put every option on the first screen.

Prefer:

* overview first
* details on selection
* advanced settings behind `a` or a separate panel
* command palette for infrequent actions
* help overlay for shortcuts

The terminal rewards density only after the user understands the structure.

## OpenTUI Implementation Patterns

### Basic React App Shell

```tsx
import { createCliRenderer } from "@opentui/core"
import { createRoot, useKeyboard, useTerminalDimensions } from "@opentui/react"
import { useState } from "react"

const theme = {
  bg: "transparent",
  panel: "#1f2335",
  panelAlt: "#24283b",
  border: "#414868",
  text: "#c0caf5",
  muted: "#7f849c",
  focus: "#7aa2f7",
  danger: "#f7768e",
}

function App() {
  const { width, height } = useTerminalDimensions()
  const compact = width < 80
  const [helpOpen, setHelpOpen] = useState(false)

  useKeyboard((key) => {
    if (key.name === "?") setHelpOpen((v) => !v)
    if (key.name === "escape" && helpOpen) setHelpOpen(false)
  })

  return (
    <box
      style={{
        width: "100%",
        height: "100%",
        flexDirection: "column",
        backgroundColor: theme.bg,
      }}
    >
      <Header compact={compact} />

      <box
        style={{
          flexGrow: 1,
          flexDirection: compact ? "column" : "row",
          gap: 1,
          padding: 1,
        }}
      >
        {!compact && <Sidebar />}
        <Main />
      </box>

      <Footer />

      {helpOpen && <HelpOverlay />}
    </box>
  )
}

function Header({ compact }: { compact: boolean }) {
  return (
    <box
      style={{
        height: 3,
        border: true,
        borderStyle: "single",
        borderColor: theme.border,
        paddingLeft: 1,
        paddingRight: 1,
        justifyContent: "center",
      }}
    >
      <text fg={theme.text}>{compact ? "App" : "OpenTUI Application"}</text>
    </box>
  )
}

function Footer() {
  return (
    <box
      style={{
        height: 1,
        flexDirection: "row",
        justifyContent: "space-between",
        backgroundColor: theme.panel,
        paddingLeft: 1,
        paddingRight: 1,
      }}
    >
      <text fg={theme.focus}>normal</text>
      <text fg={theme.muted}>? help  Esc back  Ctrl+C quit</text>
    </box>
  )
}

const renderer = await createCliRenderer({
  screenMode: "alternate-screen",
  exitOnCtrlC: true,
  targetFps: 30,
})

createRoot(renderer).render(<App />)
```

### Core Construct Card

```ts
import { Box, Text, t, bold, fg } from "@opentui/core"

function Card(props: {
  title: string
  description: string
  focused?: boolean
}) {
  return Box(
    {
      border: true,
      borderStyle: "rounded",
      borderColor: props.focused ? theme.focus : theme.border,
      padding: 1,
      gap: 1,
      flexDirection: "column",
    },
    Text({
      content: t`${bold(fg(props.focused ? theme.focus : theme.text)(props.title))}`,
      selectable: false,
    }),
    Text({
      content: props.description,
      fg: theme.muted,
      selectable: true,
    }),
  )
}
```

### Select With Preview

```tsx
function Picker() {
  const [selected, setSelected] = useState(0)

  const options = [
    { name: "Local", description: "Run task on this machine" },
    { name: "Staging", description: "Deploy to staging environment" },
    { name: "Production", description: "Deploy to production" },
  ]

  return (
    <box style={{ flexDirection: "row", gap: 1, width: "100%", height: "100%" }}>
      <box title="Target" style={{ width: 34, border: true, borderStyle: "rounded" }}>
        <select
          width={32}
          height={10}
          options={options}
          selectedIndex={selected}
          showDescription
          showScrollIndicator
          onSelectionChanged={(index) => setSelected(index)}
        />
      </box>

      <box
        title="Preview"
        style={{
          flexGrow: 1,
          border: true,
          borderStyle: "rounded",
          borderColor: theme.border,
          padding: 1,
        }}
      >
        <text fg={theme.text}>{options[selected]?.name}</text>
        <text fg={theme.muted}>{options[selected]?.description}</text>
      </box>
    </box>
  )
}
```

### Scrollable Logs

```tsx
function Logs({ lines }: { lines: string[] }) {
  return (
    <scrollbox
      style={{
        width: "100%",
        height: "100%",
        border: true,
        borderStyle: "rounded",
        borderColor: theme.border,
      }}
      stickyScroll
      stickyStart="bottom"
      viewportCulling
    >
      {lines.map((line, index) => (
        <text key={index} fg={theme.text} selectable>
          {line}
        </text>
      ))}
    </scrollbox>
  )
}
```

### Confirmation Dialog

```tsx
function ConfirmDelete({ name, onConfirm, onCancel }: {
  name: string
  onConfirm: () => void
  onCancel: () => void
}) {
  useKeyboard((key) => {
    if (key.name === "escape") onCancel()
    if (key.name === "return") onConfirm()
  })

  return (
    <box
      style={{
        position: "absolute",
        width: 52,
        height: 9,
        left: "50%",
        top: "50%",
        border: true,
        borderStyle: "rounded",
        borderColor: theme.danger,
        backgroundColor: theme.panel,
        padding: 1,
        gap: 1,
      }}
    >
      <text fg={theme.danger}>Delete {name}?</text>
      <text fg={theme.text}>This action cannot be undone.</text>
      <text fg={theme.muted}>Enter confirm   Esc cancel</text>
    </box>
  )
}
```

## Review Output Format

When reviewing or improving an OpenTUI app, group concrete changes by principle. Use markdown tables with **Before**, **After**, and **Reason**.

Do not list abstract advice if no concrete change was made.

### Example

#### Keyboard flow

| Before                         | After                                         | Reason                              |
| ------------------------------ | --------------------------------------------- | ----------------------------------- |
| Only mouse click selected rows | Added Up/Down, `j/k`, Enter, and Esc handling | TUI must be fully keyboard-operable |
| Help shortcut was undocumented | Added `? help` to footer                      | Users need a visible discovery path |

#### Focus clarity

| Before                                                  | After                                                  | Reason                                                |
| ------------------------------------------------------- | ------------------------------------------------------ | ----------------------------------------------------- |
| Active panel changed from gray to slightly lighter gray | Added focused border color and title suffix `— active` | Focus must be visible without relying on subtle color |
| Form had two fields that both looked active             | Added single focus state and Tab order                 | Prevents ambiguous submission behavior                |

#### Layout

| Before                            | After                                                      | Reason                                        |
| --------------------------------- | ---------------------------------------------------------- | --------------------------------------------- |
| Three-column layout at 70 columns | Collapsed to single-column compact layout under 80 columns | Prevents clipped text and cramped panels      |
| Footer was inside the scrollbox   | Moved footer outside the scroll region                     | Keeps shortcuts visible while content scrolls |

#### Renderer reliability

| Before                                             | After                                                      | Reason                  |
| -------------------------------------------------- | ---------------------------------------------------------- | ----------------------- |
| App called `process.exit()` on errors              | Added `renderer.destroy()` in error and rejection handlers | Restores terminal state |
| Logs printed through `console.log` into active TUI | Routed debug output to console overlay / captured stdout   | Prevents UI corruption  |

#### Visual hierarchy

| Before                         | After                                                     | Reason                       |
| ------------------------------ | --------------------------------------------------------- | ---------------------------- |
| Every row had a rounded border | Removed row borders; used selected background and spacing | Reduces visual noise         |
| Error state was red text only  | Added `[error]` label and retry shortcut                  | Color is not the only signal |

## Review Checklist

### Renderer and lifecycle

* [ ] Correct screen mode selected: `alternate-screen`, `main-screen`, or `split-footer`
* [ ] `renderer.destroy()` is called on all exit/error paths
* [ ] Ctrl+C behavior is intentional
* [ ] External output cannot corrupt the TUI
* [ ] Console/debug output is separated from user-facing UI
* [ ] Renderer is not left in continuous mode unnecessarily

### Keyboard and focus

* [ ] Every important action works by keyboard
* [ ] Focus is visible in every mode
* [ ] Tab order is predictable
* [ ] Escape/back behavior is clear
* [ ] Enter behavior is obvious and safe
* [ ] Destructive shortcuts require confirmation or are reversible
* [ ] Help is available with `?` or another visible shortcut

### Layout and responsiveness

* [ ] Layout adapts below 80 columns
* [ ] Layout adapts to low-height terminals
* [ ] Footer/status remains visible
* [ ] Scroll regions have explicit size
* [ ] Content does not clip silently
* [ ] Main regions are limited and understandable
* [ ] Absolute positioning is used only for overlays/fixed elements

### Visual design

* [ ] Borders are structural, not decorative
* [ ] Color palette is semantic and restrained
* [ ] Color is not the only state indicator
* [ ] Text attributes are used sparingly
* [ ] Inactive information is dimmed, not hidden
* [ ] Unicode glyphs have reasonable fallbacks
* [ ] Dense tables remain readable

### Components

* [ ] `Select` is used for vertical choices
* [ ] `ScrollBox` is used for overflow content
* [ ] Sticky scroll is used for logs/chat when appropriate
* [ ] Viewport culling is enabled for large scrollable content
* [ ] Inputs distinguish live typing from committed submission
* [ ] Code/diff/markdown views use specialized components where helpful
* [ ] Text selection is intentional

### Interaction quality

* [ ] Loading, empty, error, and success states are designed
* [ ] Long-running operations can be canceled or interrupted where appropriate
* [ ] The user always knows current mode and available actions
* [ ] Command palette or help exists for non-obvious actions
* [ ] Mouse support is optional, never required
* [ ] Secrets are never logged or displayed accidentally

### Performance

* [ ] Large lists are culled or paginated
* [ ] Expensive updates are throttled/debounced
* [ ] Animation is brief and purposeful
* [ ] Live rendering is requested only while needed
* [ ] Target FPS is modest unless there is a real reason
* [ ] Logs do not trigger unnecessary full-screen churn
