# Foxterm

Greenfield OpenTUI terminal for foxctl developer workflows.

This package intentionally does not depend on the archived TUI packages. It is
built around the backend facades used by OpenTUI:

- `GET /api/v2/runs`
- `GET /api/v2/runs/{run_id}`
- `GET /api/v2/events/stream`
- job, room, CAS, skill, and MCP facade routes

Run locally after installing workspace dependencies:

```bash
bun run dev:server
bun run --cwd packages/foxterm dev
```

By default foxterm connects to `http://127.0.0.1:8090`, which matches
`foxctl web serve` and the root `dev:server` script. Set `FOXCTL_API_URL` when
the web server uses another host or port:

```bash
FOXCTL_API_URL=http://127.0.0.1:3000 bun run --cwd packages/foxterm dev
```
