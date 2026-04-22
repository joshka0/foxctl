# Foxterm

Greenfield OpenTUI terminal for foxctl developer workflows.

This package intentionally does not depend on the archived TUI packages. It is
built around the backend facades used by OpenTUI:

- `GET /api/v2/runs`
- `POST /api/v2/runs?async=true`
- `GET /api/v2/runs/{run_id}`
- `GET /api/v2/runs/{run_id}/transcript`
- `POST /api/v2/runs/{run_id}/kill`
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

Runs view shortcuts:

- `n` opens the prompt composer.
- `Enter` submits the composed prompt or opens the selected run detail.
- `x` opens a confirmation prompt for killing the selected active run.
- `/` filters the active worklist.
- `a` cycles activity scopes.
- `r` refreshes the active worklist.

Foxterm starts runs asynchronously, renders the selected run transcript, and
follows `/api/v2/events/stream` for live activity. The synchronous
`POST /api/v2/runs` path remains available for non-interactive clients.
