# foxctl Viewer Applications

foxctl includes viewer applications for monitoring agents, activity,
mailbox/blackboard state, and companion conversations.

## Current Packages

- `packages/gui-agent` — Web GUI (React + Vite, port `5174` in dev)
- `cmd/foxctl_tui` — Canonical Go terminal agent shell
- `internal/interfaces/tui` — Go TUI rendering and state model
- `packages/data` — Shared TypeScript data/client types

Archived surfaces:

- `docs/archive/source/packages/tui-agent` — archived TypeScript terminal control plane
- `docs/archive/source/cmd/foxctl_viewer` — archived legacy Go viewer TUI

## Runtime Topology

```
GUI Agent (Vite :5174) ─┐
Go TUI                  ├──> foxctl API server (:8090)
CLI                     ┘
```

The GUI and TUI both consume backend APIs served by:

```bash
foxctl web serve --dev-cors
```

## Quick Start

From repository root:

```bash
bun install

# API + GUI (recommended)
make gui-agent

# GUI only (if API server already running)
bun run dev:gui

# API only
bun run dev:server

# Go TUI with a local foxctl agent
make go-tui-agent

# Build the Go TUI binary only
make go-tui-build
```

## Make Targets

- `gui-agent` — build Go backend, start API server, start Vite GUI
- `gui-agent-vite` — GUI only (Vite dev server)
- `gui-agent-build` — frontend build only
- `go-tui-agent` — build/start API, spawn a foxctl agent, launch the Go TUI
- `go-tui-build` — build `bin/foxctl-tui`

## Troubleshooting

- GUI fails to load data:
  - Ensure API server is reachable at `http://localhost:8090`.
  - Check Vite proxy settings in `packages/gui-agent/vite.config.ts`.
- Port conflicts:
  - API uses `8090`, Vite uses `5174` by default.
