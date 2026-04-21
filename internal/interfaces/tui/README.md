# foxctl TUI Operator Cockpit

Terminal-based control plane for the foxctl multi-agent system. Built on
`github.com/grindlemire/go-tui`.

## Quick Start

```bash
go build -o bin/foxctl_tui ./cmd/foxctl_tui
./bin/foxctl_tui -screen agents -api-url http://localhost:8080
```

The `-screen` flag selects the initial surface (`agents`, `console`, `shell`).
`-api-url` points to a running `foxctl web serve` instance.

## Architecture

The cockpit uses a **three-lane layout** — Main, Detail, Evidence — matching
the operator model in `DESIGN.md`.

```
┌─────────────────────┬──────────────────┬─────────────────┐
│  Main (inventory)   │  Detail (agent)  │  Evidence       │
│  agent list / tree  │  status + attrs  │  drawer / logs  │
└─────────────────────┴──────────────────┴─────────────────┘
```

Key components:

- **CockpitScreen** — root component; owns the three-lane layout, agent
  inventory, ask-stream state, and evidence drawer. Phases: Loading → Ready
  (or Error).
- **BootManager** — async health check against the daemon API; transitions
  the cockpit phase without blocking the UI thread.
- **EventsSubscriber** — SSE subscription to `/api/events`; triggers debounced
  re-fetches of the agent inventory for live refresh.
- **runtime.Bounded[Req, Upd]** — generic bounded-queue runtime shared by all
  request-driven runtimes (ask-stream, cancel, console stream).

Data flow:

```
Daemon API → EventsSubscriber → CockpitScreen.SetAgents() → re-render
User key   → CockpitScreen.HandleKey()           → state change → re-render
Ask stream → AgentAskStreamRuntime.Enqueue()      → Bounded handler → Updates()
```

## Package Layout

| Package | Purpose |
|---------|---------|
| `components/` | Reusable widgets — EntityList, DetailPane, Tabs, Drawer, StreamViewer, StatusBadge, KeybindHint. See `components/README.md` for authoring guide. |
| `entities/` | Typed domain models — `Agent`, `AgentNode`, `Room`, `RoomMessage`, `EventRow`, `EntryKind`. |
| `runtime/` | `Bounded[Req, Upd]` — generic bounded goroutine lifecycle primitive. |
| `theme/` | Color palette (`theme.Colors`) and spacing tokens (`theme.Spacing`). Raw color literals are forbidden in M2 widgets. |
| `testfixture/` | Per-test daemon boot helper (`BootDaemon`) for integration tests. Boots `foxctl web serve -p 0` with a temp store, discovers the port, seeds agents, and tears down on `t.Cleanup`. |

## Entry Points

| Function | Purpose |
|----------|---------|
| `RunCockpit(apiURL)` | Cockpit mode — agent inventory, detail pane, evidence drawer. |
| `Run(ctx, opts)` | Shell mode — companion console with transcript, composer, rail. |

## Key Bindings

| Key | Action |
|-----|--------|
| `ESC` / `q` | Quit |
| `Tab` / `Shift+Tab` | Cycle focused lane |
| `↑` / `↓` | Navigate list |
| `Enter` | Select / open detail |
| `e` | Toggle evidence drawer |
| `Ctrl+X` | Cancel active stream |

## Testing

```bash
go test -race ./internal/interfaces/tui/...
```

- **Unit tests** use `gotui.MockTerminal` for deterministic render assertions.
- **Integration tests** use `testfixture.BootDaemon` to boot a real daemon and
  exercise the cockpit end-to-end.
- **Snapshot tests** golden-file the rendered buffer for visual regression.
