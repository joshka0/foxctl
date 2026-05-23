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
- `GET /api/rooms/{room_id}/control-snapshot`
- `POST /api/agents/spawn`
- `GET /api/atcp/sessions`
- `GET /api/atcp/foxctl-rooms/{room_id}/sessions`
- `DELETE /api/atcp/foxctl-rooms/{room_id}/sessions/{session_id}`
- `POST /api/atcp/foxctl-rooms/{room_id}/spawn-cli`
- `POST /api/atcp/foxctl-rooms/{room_id}/messages`
- job, room, CAS, skill, and MCP facade routes

Run locally after installing workspace dependencies:

```bash
bun run dev:server
bun run --cwd packages/foxterm dev
```

The root `dev:server` script starts `foxctl web serve --atcp`, so foxterm's
ATCP-backed CLI sessions are served by the same foxctl process as the web API.
`bun run dev:atcpd` remains available when you intentionally want a standalone
ATCP daemon.

By default foxterm connects to `http://127.0.0.1:8090`, which matches
`foxctl web serve` and the root `dev:server` script. Set `FOXCTL_API_URL` when
the web server uses another host or port:

```bash
FOXCTL_API_URL=http://127.0.0.1:3000 bun run --cwd packages/foxterm dev
```

Verification from repo root:

```bash
bun run check:frontend
bun run unused:frontend
```

`check:frontend` includes `bun run --cwd packages/foxterm typecheck`.
`unused:frontend` also runs the targeted frontend dead-export report.

Runs view shortcuts:

- `:` or `Ctrl+p` opens the command palette for discoverable actions.
- `n` opens the prompt composer.
- `c` composes a follow-up turn on the selected run stream.
- `Enter` submits the composed prompt, opens the selected run detail, or opens a card's linked run.
- `x` opens a confirmation prompt for killing the selected active run.
- `+` or `Shift+n` creates a room from Rooms or an orchestration card from Cards.
- `m` writes a message to the selected room.
- `o` enables and starts or revives the selected room loop.
- `s` spawns a foxctl daemon agent into the selected room.
- `Shift+s` spawns an ATCP-backed CLI session into the selected room.
- `v` cycles the focused ATCP CLI session screen detail.
- `p` prompts the focused ATCP CLI session.
- In Cards, `v` opens the selected card runtime tree.
- In Cards, `d` marks the selected card done, `u` releases it, and `t` retries
  blocked or retry-queued cards after confirmation.
- `x` stops the focused ATCP CLI session in Rooms or kills a selected run in Runs.
- `/` filters the active worklist.
- `a` cycles activity scopes.
- `r` refreshes the active worklist.
- `b` hides or shows the Scope pane; `w` hides or shows the active worklist.
- `,` / `.` resize the focused Scope or worklist pane.
- `-` / `=` resize room agent panes.

Foxterm starts runs asynchronously, renders the selected run transcript, and
follows `/api/v2/events/stream` for live activity. The synchronous
`POST /api/v2/runs` path remains available for non-interactive clients.

In Rooms, `+` creates a room in the active workspace, `m` writes a room
message, and `n` seeds a new run prompt with the selected room/task context.
Foxterm identifies room operations as `FOXTERM_ACTOR_ID`/`FOXCTL_ACTOR_ID`, or
`dev-local-user` by default. In Cards, `n` seeds a board/card context prompt,
and `Enter` jumps to the card's linked run when one exists.
The Rooms composer shows the exact Enter target, such as the selected ATCP
participant, room message destination, daemon-agent spawn target, or CLI spawn
target, before sending.

Room messages require the room loop to be active. Foxterm shows loop heartbeat
and delivery-owner status in the room detail panel, plus a `./bin/foxctl room
loop ...` start command when the loop is not ready. Press `o` in Rooms to
enable the persisted loop policy and spawn `foxctl room loop <room-id>` as a
local background process for the selected room.
Room detail also reads the room control snapshot for the local actor. The
operator pulse shows local inbox obligations, loop health, active reminders,
task/card counts, latest delivery trace, and participant state as separate
membership, transport, runtime, and viewer signals.
Room detail also shows a compact orchestration strip with linked task cards and
active board cards so room work and coordinator-created board activity stay
visible without switching scopes. In Rooms detail focus, `v` cycles the
visible room card selection and `Enter` opens that card in Cards; when no room
card is selected, `Enter` opens the focused ATCP screen.

Room agent spawning uses `POST /api/agents/spawn` with the selected room ID.
The spawn composer accepts `role` or `role: prompt`; when the prompt is omitted
the backend uses its default room-aware prompt for that role.
Foxterm also reads `/api/agents` and shows room member agent state/model in the
room detail panel.

CLI-backed agents such as Codex, Claude, Droid, and other terminal tools should
use the separate ATCP-aware facade. The `s` action is for foxctl daemon agents;
`Shift+s` creates an ATCP session and joins it to the room-linked ATCP room.
The CLI flow opens a preset picker for Codex, Claude, Droid, Gemini, Shell, or
Custom. Selecting a preset fills editable `agent`, `adapter`, and `command`
fields; Custom keeps the raw `agent@adapter: command args` form, for example
`codex-a@codex: codex --no-alt-screen`.
The room detail panel shows attached ATCP sessions, command, adapter, and
readiness so CLI-backed agents are visible after they start. It also shows the
latest non-empty rendered screen line when ATCP screen snapshots are available.
Press `v` to cycle the focused ATCP session and show a larger screen excerpt.
Press `p` to send the bottom-composer prompt to that focused CLI participant
through the room-linked ATCP message facade.
Press `x` in Rooms to confirm and stop the focused ATCP session; this calls the
room-scoped delete facade so foxterm only stops a CLI attached to that room.
