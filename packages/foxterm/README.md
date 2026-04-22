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
- `POST /api/rooms`
- `GET /api/rooms/{room_id}/messages`
- `POST /api/rooms/{room_id}/messages`
- `GET /api/rooms/{room_id}/loop`
- `POST /api/agents/spawn`
- `GET /api/atcp/sessions`
- `POST /api/atcp/foxctl-rooms/{room_id}/spawn-cli`
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
- `c` composes a follow-up turn on the selected run stream.
- `Enter` submits the composed prompt, opens the selected run detail, or opens a card's linked run.
- `x` opens a confirmation prompt for killing the selected active run.
- `+` or `Shift+n` creates a room from the Rooms view.
- `m` writes a message to the selected room.
- `s` spawns a foxctl daemon agent into the selected room.
- `Shift+s` spawns an ATCP-backed CLI session into the selected room.
- `/` filters the active worklist.
- `a` cycles activity scopes.
- `r` refreshes the active worklist.

Foxterm starts runs asynchronously, renders the selected run transcript, and
follows `/api/v2/events/stream` for live activity. The synchronous
`POST /api/v2/runs` path remains available for non-interactive clients.

In Rooms, `+` creates a room in the active workspace, `m` writes a room
message, and `n` seeds a new run prompt with the selected room/task context.
Foxterm identifies room operations as `FOXTERM_ACTOR_ID`/`FOXCTL_ACTOR_ID`, or
`dev-local-user` by default. In Cards, `n` seeds a board/card context prompt,
and `Enter` jumps to the card's linked run when one exists.

Room messages require the room loop to be active. Foxterm shows loop heartbeat
and delivery-owner status in the room detail panel, plus a `./bin/foxctl room
loop ...` start command when the loop is not ready.

Room agent spawning uses `POST /api/agents/spawn` with the selected room ID.
The spawn composer accepts `role` or `role: prompt`; when the prompt is omitted
the backend uses its default room-aware prompt for that role.
Foxterm also reads `/api/agents` and shows room member agent state/model in the
room detail panel.

CLI-backed agents such as Codex, Claude, Droid, and other terminal tools should
use the separate ATCP-aware facade. The `s` action is for foxctl daemon agents;
`Shift+s` creates an ATCP session and joins it to the room-linked ATCP room.
The CLI composer accepts `agent@adapter: command args`, for example
`codex-a@codex: codex --no-alt-screen`.
