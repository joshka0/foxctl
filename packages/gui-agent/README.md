# gui-agent

Web UI for foxctl runtime operations (agents, activity, chat, mailbox, logs).

## Stack

- React + TypeScript + Vite
- Tailwind CSS
- TanStack Query
- Zustand

## Development

From repo root:

```bash
bun install

# API + GUI
make gui-agent

# GUI only (API must already be running on :8090)
bun run --cwd packages/gui-agent dev
```

Open: `http://localhost:5174`

## Build

```bash
bun run --cwd packages/gui-agent build
```

## API Integration

The frontend calls `/api/*` and `/ws` paths through Vite proxy configured in:

- `packages/gui-agent/vite.config.ts`

Default proxy target: `http://localhost:8090`.
