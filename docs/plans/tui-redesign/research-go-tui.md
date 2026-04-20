# go-tui v0.11.0 Reference for LLM Authors

> This document is a concise API and pattern reference for
> `github.com/grindlemire/go-tui` v0.11.0, written so that Codex / Claude /
> future maintainers can produce correct foxctl TUI code without reading the
> framework source.
>
> The foxctl TUI pins `github.com/grindlemire/go-tui v0.11.0` in `go.mod`.
> All symbol references and code examples below target that version.

---

## (a) Core Types

### `tui.App`

The central runtime. Owns the terminal, event loop, focus manager, and
double-buffered renderer.

- **`tui.NewApp(opts ...AppOption) (*App, error)`** — creates an App with real
  terminal I/O (`os.Stdout`, `os.Stdin`). Enters raw mode and alternate screen
  by default.
- **`tui.NewAppWithReader(reader EventReader, opts ...AppOption) (*App, error)`**
  — same but with a custom event reader (useful for testing).
- Key options: `WithRootComponent`, `WithRootView`, `WithMouse`, `WithInlineHeight`.

Reference: <https://pkg.go.dev/github.com/grindlemire/go-tui#NewApp>

### `tui.Component`

The interface every struct component must implement:

```go
type Component interface {
    Render(app *App) *Element
}
```

Optional interfaces a component may also implement:

| Interface           | Method                     | Purpose                                         |
| ------------------- | -------------------------- | ----------------------------------------------- |
| `KeyListener`       | `KeyMap() KeyMap`          | Keyboard bindings (re-evaluated every frame)    |
| `MouseListener`     | `HandleMouse(MouseEvent)`  | Mouse event handling                            |
| `WatcherProvider`   | `Watchers() []Watcher`     | Timer / channel watchers (started after mount)  |
| `Initializer`       | `Init() func()`            | One-time mount setup; returned func is cleanup  |
| `AppBinder`         | `BindApp(app *App)`        | Bind `State` / `Events` fields to an App       |
| `AppUnbinder`       | `UnbindApp()`              | Detach app-bound resources on unmount           |

Reference: <https://github.com/grindlemire/go-tui/blob/main/component.go>

### `tui.Element`

The node type in the rendered tree. Created by `.gsx` template compilation or
by hand via `tui.NewElement`. Has children, styles (Tailwind classes), layout
properties (`flexGrow`, `width`, `height`), watchers, and focus state.

### `tui.State[T]`

Generic reactive state. Wraps a value of type `T` and notifies bound callbacks
on change.

- **`tui.NewState[T any](initial T) *State[T]`** — constructor.
- **`s.Get() T`** — thread-safe read from any goroutine.
- **`s.Set(v T)`** — write + mark dirty + fire bindings. Must be called from
  the main loop; for background updates use `App.QueueUpdate` or channel
  watchers.
- **`s.Update(fn func(T) T)`** — read-modify-write convenience: reads current,
  applies `fn`, calls `Set` with the result.
- **`s.Bind(fn func(T)) Unbind`** — register a callback; returns an `Unbind`
  handle.

The current foxctl shell uses `tui.State[ShellState]` for the full
presentation snapshot and `tui.State[bool]` per focus pane.

Reference: <https://pkg.go.dev/github.com/grindlemire/go-tui#State>

### Events

The framework dispatches typed events through the event loop:

| Type            | Trigger                              |
| --------------- | ------------------------------------ |
| `KeyEvent`      | Keyboard input (key, rune, modifier) |
| `MouseEvent`    | Mouse press / release / drag / wheel |
| `ResizeEvent`   | Terminal resize (SIGWINCH)           |
| `UpdateEvent`   | Closure queued via `QueueUpdate`     |

`KeyEvent.App()` returns the owning `*App` so handlers can call `Stop()`,
`QueueUpdate()`, etc.

Reference: <https://github.com/grindlemire/go-tui/blob/main/event.go>

### `tui.KeyMap`

An ordered slice of key bindings. Each binding maps a key spec to a handler:

```go
type KeyMap []keyBinding
```

Construction helpers:

- **`tui.On(key KeySpec, handler func(KeyEvent))`** — creates a binding.
- **`tui.Rune(r rune)`** — key spec for a printable character.
- **`tui.Rune(r).Ctrl()`** — key spec for Ctrl+character.
- **`tui.AnyRune`** — matches any printable character (used for text input).

A component's `KeyMap()` method is called every frame; it can return different
bindings based on current state.

Reference: <https://go-tui.dev/docs/reference/events>

### Focus

Focus is managed by an internal `focusManager` that tracks registered
`Focusable` elements (any `*Element` with `tabStop=true`).

Key APIs on `*App`:

- **`FocusNext()`** / **`FocusPrev()`** — Tab / Shift+Tab navigation.
- **`Focused() Focusable`** — current focused element.
- **`SetFocus(elem Focusable)`** — focus a specific element.
- **`BlurFocused()`** — unfocus everything.

**`tui.FocusGroup`** is a convenience struct that owns multiple `*State[bool]`
values and automatically sets exactly one to `true` at a time. The foxctl
shell uses `tui.MustNewFocusGroup(...)` for its four focus panes.

Reference: <https://go-tui.dev/docs/reference/focus>

### Watchers

Watchers are side-effectful goroutines started by the framework when a
component enters the tree.

| Constructor              | Fires when                                    |
| ------------------------ | --------------------------------------------- |
| `tui.OnTimer(d)`         | Every `d` duration                            |
| `tui.OnChange(s, fn)`    | State `s` changes                             |
| `tui.Watch(ch, fn)`      | Value received on channel `ch`                |

The foxctl shell uses `tui.Watch` to bridge background goroutines
(stream pump, ask runtime, cancel runtime) into the main render loop.

Reference: <https://go-tui.dev/docs/reference/watchers>

### Refs

Refs are not a first-class type in v0.11.0. Instead, the framework provides
`Element` pointers discovered via `WalkFocusables`, `WalkWatchers`, and
`ElementAt`. For click handling, elements accept `WithOnClick` and
`WithOnActivate` options.

Reference: <https://go-tui.dev/docs/reference/refs>

---

## (b) Composition Model (.gsx + Generated Code)

`.gsx` files are templ-inspired templates that compile to plain Go source
(`*_gsx.go`) via the `tui generate` CLI. The generated code creates `*Element`
trees at runtime.

### Compilation pipeline

```
.gsx files  →  tui generate  →  *_gsx.go  →  go build  →  widget tree
```

### Template types

1. **Standalone templates** — free functions returning elements:

   ```gsx
   templ Badge(label string, color string) {
       <div class="flex gap-1">
           <span class="font-bold {color}">{label}</span>
       </div>
   }
   ```

2. **Method templates** — tied to a struct component:

   ```gsx
   templ (c *MyComponent) Render() {
       <div class="flex-col h-full">
           @Badge("hello", "text-cyan")
       </div>
   }
   ```

3. **Children slot** — `{children...}` inside a template accepts nested content.

### Layout & styling

Elements use Tailwind-style utility classes that compile directly to Go option
functions:

- Layout: `flex`, `flex-col`, `gap-1`, `grow`, `shrink-0`, `p-1`, `border-rounded`.
- Alignment: `justify-between`, `justify-center`.
- Sizing: `width={44}`, `height={5}`, `flexGrow={1.0}`.
- Typography: `font-bold`, `font-dim`, `text-cyan`, `text-green`, `text-red`.

### Control flow in .gsx

Go `if`, `for`, and `:=` expressions are legal inline:

```gsx
if state.ActiveRail == RailMemory {
    @MemoryRail(state.Memory)
} else if state.ActiveRail == RailWorkers {
    @WorkersRail(state.Workers)
}
```

Reference: <https://go-tui.dev/docs/guides/02-gsx-syntax>

---

## (c) Event Loop

### `tui.App.Run()` — full framework loop

The simplest lifecycle. Blocks until `Stop()` is called.

```go
app, _ := tui.NewApp(tui.WithRootComponent(myComponent))
defer app.Close()
app.Run() // blocks
```

Internally, `Run()`:

1. Calls `Open()` — registers signal handlers, starts input reader goroutine,
   performs initial render.
2. Loops: drain events for half the frame budget (default 16ms → 60 fps),
   call `Render()`, sleep for remaining time.
3. Exits when `Stop()` is called or SIGINT received.

### `tui.App.Open()` / `Events()` / `Dispatch()` / `Render()` / `Step()` — manual loop

For custom event processing (e.g., multiplexing with your own channels):

```go
app, _ := tui.NewApp(tui.WithRootComponent(myComponent))
defer app.Close()
app.Open()

for {
    select {
    case ev := <-app.Events():
        app.Dispatch(ev)
    case myMsg := <-myChannel:
        // handle custom event
    case <-app.StopCh():
        return
    }
    app.Render()
}
```

Key APIs:

- **`Open() error`** — start event loop (signal handlers, input reader). Returns
  `ErrAlreadyOpen` if called twice.
- **`Events() <-chan Event`** — merged event stream (input priority over
  background updates).
- **`Dispatch(ev Event)`** — route event to focused element / component keymap.
- **`Render()`** — dirty-check, re-render, diff-based terminal flush.
- **`Step() bool`** — convenience: `DispatchEvents()` + `Render()`.
- **`QueueUpdate(fn func())`** — enqueue a closure from any goroutine; executed
  on the main loop.
- **`Stop()`** — signal exit. Idempotent.
- **`Close() error`** — restore terminal, clean up goroutines.

Reference: <https://pkg.go.dev/github.com/grindlemire/go-tui#App.Run>

---

## (d) Focus and Keymap

### Focus management

The `focusManager` maintains an ordered list of `Focusable` elements. Tab
cycles through elements where `IsTabStop()` returns `true`. Focus state is
preserved across re-renders via index tracking (not element pointers).

Components track which pane is focused using `FocusGroup`, which owns
multiple `*State[bool]` values and exposes:

- **`KeyMap() KeyMap`** — returns Tab / Shift+Tab bindings.
- **`Current() int`** — index of the currently-focused state.
- **`Len() int`** — number of states.

### Keymap composition

KeyMaps are appendable slices. A typical component merges several sources:

```go
func (s *Shell) KeyMap() tui.KeyMap {
    keyMap := append(tui.KeyMap{}, s.focus.KeyMap()...)   // Tab navigation
    keyMap = append(keyMap, stopBindings()...)              // q/Esc/Ctrl+C
    keyMap = append(keyMap,                                 // app-specific
        tui.On(tui.Rune('m').Ctrl(), func(ke tui.KeyEvent) { s.setRail(RailMemory) }),
        // ...
    )
    return keyMap
}
```

Modal dialogs use `WithTrapFocus` and `WithModalKeyMap` to intercept all
keyboard input and scope focus to elements inside the modal.

Reference: <https://go-tui.dev/docs/reference/focus>

---

## (e) Built-in Widget Catalog

v0.11.0 provides these HTML-style elements in `.gsx` templates:

| Element      | Purpose                                      |
| ------------ | -------------------------------------------- |
| `div`        | Container (flex row by default, `flex-col` for column) |
| `span`       | Inline text                                  |
| `p`          | Paragraph (block-level text)                 |
| `button`     | Clickable button with `onClick`              |
| `input`      | Text input field                             |
| `textarea`   | Multi-line editable text area                |
| `ul` / `li`  | Unordered list / list item                   |
| `table`      | Grid of rows and columns                     |
| `progress`   | Progress bar                                 |
| `hr`         | Horizontal rule (separator line)             |
| `br`         | Line break                                   |

Common attributes on any element:

- `class="..."` — Tailwind-style utility classes.
- `width={N}` — fixed width in cells.
- `height={N}` — fixed height in rows.
- `flexGrow={F}` — flexbox grow factor.
- `autoFocus` — auto-focus this element on first render.
- `scrollable` — enable keyboard scrolling (arrow keys, PageUp/Down).
- `sticky` — stick to bottom edge (for streaming log viewers).
- `onClick` — callback for mouse clicks.

All of these are composable; there is no separate "widget library" package.

Reference: <https://go-tui.dev/docs/reference/elements>

---

## (f) Idiomatic Patterns

### Pattern 1: Struct Component with Composition

A reusable component is a Go struct with `State` fields, a `Render()` method
template, and optional `KeyMap()` / `Watchers()` methods. Sub-templates compose
via `@`-calls.

```go
// agent_list.go
type AgentList struct {
    agents  *tui.State[[]Agent]
    focused *tui.State[bool]
}

func NewAgentList(agents []Agent) *AgentList {
    return &AgentList{
        agents:  tui.NewState(agents),
        focused: tui.NewState(false),
    }
}

func (al *AgentList) KeyMap() tui.KeyMap {
    return tui.KeyMap{
        tui.On(tui.KeyUp, func(ke tui.KeyEvent) {
            // move selection up
        }),
        tui.On(tui.KeyDown, func(ke tui.KeyEvent) {
            // move selection down
        }),
    }
}
```

```gsx
// agent_list.gsx
templ (al *AgentList) Render() {
    <div class="flex-col border-rounded p-1 gap-1 grow"
         deps={al.agents, al.focused}
         scrollable>
        for _, agent := range al.agents.Get() {
            <div class="flex border-single p-1 gap-1">
                <span class="text-cyan font-bold">{agent.ID}</span>
                <span class="font-dim">{agent.Role}</span>
                <span>{agent.Status}</span>
            </div>
        }
    </div>
}
```

The `deps` attribute lists which `State` values drive re-renders for this
element's subtree.

### Pattern 2: State + Events + Channel Watcher

This pattern bridges a background goroutine into the UI via a channel watcher.
The foxctl shell uses this extensively for stream pumps and ask runtimes.

```go
// stream_viewer.go
type StreamViewer struct {
    lines    *tui.State[[]string]
    buffer   *tui.State[int]
    updates  chan string
}

func NewStreamViewer(bufferSize int) *StreamViewer {
    return &StreamViewer{
        lines:   tui.NewState([]string{}),
        buffer:  tui.NewState(bufferSize),
        updates: make(chan string, bufferSize),
    }
}

// Enqueue adds a line from any goroutine. Bounded channel provides backpressure.
func (sv *StreamViewer) Enqueue(line string) error {
    select {
    case sv.updates <- line:
        return nil
    default:
        return fmt.Errorf("stream viewer buffer full (%d)", sv.buffer.Get())
    }
}

// Watchers bridges the channel into the main loop.
func (sv *StreamViewer) Watchers() []tui.Watcher {
    return []tui.Watcher{
        tui.Watch(sv.updates, func(line string) {
            sv.lines.Update(func(current []string) []string {
                return append(current, line)
            })
        }),
    }
}
```

```gsx
// stream_viewer.gsx
templ (sv *StreamViewer) Render() {
    <div class="flex-col border-rounded p-1 gap-1 grow"
         deps={sv.lines} scrollable sticky>
        <span class="font-bold text-cyan">Stream Output</span>
        <hr />
        for _, line := range sv.lines.Get() {
            <span>{line}</span>
        }
    </div>
}
```

Key points:
- `Enqueue` is safe to call from any goroutine (writes to a bounded channel).
- `tui.Watch` adapter reads from the channel on the main loop and calls the
  callback, which updates `State` and triggers a re-render.
- The bounded channel provides natural backpressure: when the channel is full,
  `Enqueue` returns an error instead of blocking.

### Pattern 3: Manual Event Loop with Open/Step/Events

For full control over event processing (e.g., multiplexing with a daemon
connection health check), use `Open()` instead of `Run()`:

```go
func runManualLoop(ctx context.Context, comp *MyRoot) error {
    app, err := tui.NewApp(tui.WithRootComponent(comp))
    if err != nil {
        return err
    }
    defer app.Close()

    if err := app.Open(); err != nil {
        return err
    }

    // Custom health ticker
    healthTicker := time.NewTicker(5 * time.Second)
    defer healthTicker.Stop()

    for {
        select {
        case <-ctx.Done():
            app.Stop()
            return ctx.Err()
        case <-healthTicker.C:
            // Check daemon health on the main loop
            comp.UpdateHealth(pingDaemon())
            app.Render()
        case <-app.Events():
            // Let the framework dispatch input events
            if !app.Step() {
                return nil // app.Stop() was called
            }
        case msg := <-comp.daemonEvents:
            // Bridge external events into state
            comp.state.Update(func(s State) State {
                s.LastEvent = msg
                return s
            })
            app.Render()
        }
    }
}
```

This pattern is useful when:
- You need to multiplex framework events with external event sources.
- You want to control frame timing explicitly.
- You need to perform periodic I/O (health checks, polling) without blocking
  the render loop.

---

## (g) Anti-patterns and Gotchas

### 1. Calling `State.Set()` from a background goroutine

`State.Set()` must be called from the main loop. From background goroutines,
use `App.QueueUpdate(fn)` or channel watchers (`tui.Watch`). Calling `Set()`
from another goroutine races with the render loop.

### 2. Mutating state outside `State.Update`

Never modify a `State[[]T]` or `State[map[K]V]` value in-place and expect
a re-render. Always replace the entire value:

```go
// WRONG: no re-render triggered
state.Get()[0] = newValue

// RIGHT: replace and trigger
state.Update(func(items []Item) []Item {
    items[0] = newValue
    return items
})
```

### 3. Blocking the main loop in a key handler

Key handlers run on the main loop. Never do I/O (HTTP calls, file reads)
inside a handler. Use `QueueUpdate` or a background goroutine with a channel
watcher instead.

### 4. Forgetting `deps` on the root element

In `.gsx` templates, the `deps` attribute lists which `State` values drive
re-renders for that subtree. Omitting `deps` means the framework will not
know to re-render when state changes.

### 5. Editing generated `*_gsx.go` files

Never hand-edit generated files. They are overwritten on the next `tui generate`
run. Only edit `.gsx` source files.

### 6. Not closing the App

Always `defer app.Close()` after creation. `Close()` restores the terminal
to its original state (exits raw mode, exits alternate screen, shows cursor).

### 7. Chaining constructors with ambient state

Avoid constructors that take other components and wire their channels
implicitly. Prefer explicit, flat constructors where all channels and
callbacks are passed in. The current foxctl shell's `NewShellWithRuntimes()`
has 9 parameters — this is a deliberate choice for clarity over convenience.

### 8. String-keyed state discriminators

Using `string` to discriminate state kinds (e.g., transcript entry kinds)
works but defeats exhaustive switch checking. Prefer a typed `int` enum with
named constants so the compiler catches missing cases.

---

## (h) Testing via MockTerminal

### `tui.MockTerminal`

A mock implementation of the `Terminal` interface that captures all rendering
operations in an in-memory cell buffer. Created with explicit dimensions:

```go
mock := tui.NewMockTerminal(80, 24) // 80 columns, 24 rows
```

Key test helpers:

| Method                | Purpose                                         |
| --------------------- | ----------------------------------------------- |
| `CellAt(x, y) Cell`  | Read a cell's rune and style at a position       |
| `String() string`     | Render the full buffer as a newline-joined string |
| `StringTrimmed()`     | Same, with trailing spaces stripped per line     |
| `Cursor() (x, y)`    | Current cursor position                         |
| `IsCursorHidden()`    | Whether the cursor is hidden                    |
| `Resize(w, h)`       | Simulate a terminal resize                      |
| `Reset()`             | Clear everything to defaults                    |

### Testing pattern

The foxctl TUI tests follow this structure:

1. **Create a component** with known initial state.
2. **Render into a MockTerminal** via `app.Render()`.
3. **Inspect the cell buffer** for expected text, styles, and layout.
4. **Simulate input** by calling key handlers directly (components expose
   `KeyMap()` which returns handler closures).
5. **Re-render and assert** that state changes propagated correctly.

Example from the foxctl codebase (`app_runtime_test.go`):

```go
func TestShellRendersTranscript(t *testing.T) {
    state := DefaultShellState(Options{Workspace: "."})
    shell := NewShell(state)

    app, err := tui.NewApp(tui.WithRootComponent(shell))
    require.NoError(t, err)
    defer app.Close()

    // Render into mock terminal
    mock := tui.NewMockTerminal(120, 40)
    app.Render()  // renders to internal buffer

    // Verify top bar contains workspace label
    output := mock.String()
    assert.Contains(t, output, "foxctl_tui")
}
```

Reference: <https://github.com/grindlemire/go-tui/blob/main/mock_terminal.go>

---

## (i) Gap Analysis for Cockpit Needs

The table below maps foxctl operator cockpit requirements to what go-tui v0.11.0
provides and the approach for each gap.

| Need                                             | Provided by go-tui                      | Approach                                           |
| ------------------------------------------------ | --------------------------------------- | -------------------------------------------------- |
| **Scrollable transcript with sticky-bottom**     | `scrollable` + `sticky` attributes     | Use directly. Add auto-scroll toggle on scroll-up. |
| **Live data from channels**                      | `tui.Watch(ch, fn)`                    | Use directly. Bridge background goroutines via bounded channels. |
| **Focus management (multi-pane)**                | `FocusGroup` + `focusManager`          | Use directly. Each lane is one focus state.        |
| **Tabbed side rail**                             | Manual: render tabs as styled spans     | Build a `Tabs` component wrapping `State[RailTab]`. |
| **Drawer / modal for evidence**                  | `Modal` with `trapFocus`               | Use directly with `WithTrapFocus(true)`.            |
| **Keybinding help footer**                       | No built-in widget                      | Build a `KeybindHint` component rendering a `<div>` with styled spans. |
| **Entity list with selection highlight**         | No built-in widget                      | Build `EntityList` component with `State[int]` for selected index and custom focus indicator. |
| **Detail pane with scrollable sections**         | `scrollable` container                 | Build `DetailPane` with header + section children. |
| **Loading / empty / error states**               | No built-in widget                      | Build `EmptyState`, `LoadingState`, `StatusBadge` as small leaf components. |
| **Theme tokens (consistent colors)**             | Tailwind classes (`text-cyan`, etc.)    | Extract named constants into a `theme` package; forbid raw color literals in widget code. |
| **Typed entity model**                           | N/A (not a framework concern)           | Define `Agent`, `Room`, `EventRow` structs with typed kind enums in a separate `entities` package. |
| **Generic bounded runtime**                      | N/A (not a framework concern)           | Implement `Bounded[Req, Upd]` with configurable buffer channels. |
| **Async boot (non-blocking HTTP)**               | N/A (not a framework concern)           | Use channel watchers + loading state; initial HTTP fetch runs in a goroutine. |
| **Stream token rendering**                       | `sticky` + channel watcher              | Use `tui.Watch` to bridge SSE token channel; append to `State[[]string]`. |
| **Connection health indicator**                  | No built-in widget                      | Build `StatusBadge` with ok/warn/error variants using distinct ANSI styles. |
| **CJK / Unicode width correctness**             | `Cell.IsContinuation()` handles wide runes | Test with CJK and emoji strings; verify no cell overlap in `MockTerminal`. |
| **SIGWINCH / resize handling**                   | Automatic via `ResizeEvent`             | Test with `MockTerminal.Resize()`. Ensure layout is flex-based (not absolute). |
| **`.gsx` toolchain for LLM authoring**          | `tui generate` CLI + LSP                | Document exact regeneration command and forbidden-edit globs. |

### Summary

go-tui v0.11.0 provides a strong foundation for the cockpit: reactive state,
flexbox layout, channel watchers, focus management, modal overlays, and a
MockTerminal for deterministic testing. The primary gaps are higher-level
widgets (entity lists, detail panes, status badges, tabs, drawers) and
application-level patterns (bounded runtimes, typed entities, theme tokens).
All of these are buildable on top of the framework's primitives, and this
mission's M2 milestone addresses them with a focused component library seed.
