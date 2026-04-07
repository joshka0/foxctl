# agentctl Viewer Applications

agentctl includes TypeScript viewer applications for monitoring agents, activity,
mailbox/blackboard state, and companion conversations.

## Current Packages

- `packages/gui-agent` — Web GUI (React + Vite, port `5174` in dev)
- `packages/tui-agent` — Terminal-first operator control plane (`pi-tui`)
- `packages/data` — Shared TypeScript data/client types

## Runtime Topology

```
GUI Agent (Vite :5174) ─┐
TUI                     ├──> agentctl API server (:8090)
CLI                     ┘
```

The GUI and TUI both consume backend APIs served by:

```bash
agentctl web serve --dev-cors
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

# TUI agent shell (requires API server)
bun run dev:tui
```

## Make Targets

- `gui-agent` — build Go backend, start API server, start Vite GUI
- `gui-agent-vite` — GUI only (Vite dev server)
- `gui-agent-build` — frontend build only
- `ts-dev-tui` — TUI agent dev mode

## Troubleshooting

- GUI fails to load data:
  - Ensure API server is reachable at `http://localhost:8090`.
  - Check Vite proxy settings in `packages/gui-agent/vite.config.ts`.
- Port conflicts:
  - API uses `8090`, Vite uses `5174` by default.
