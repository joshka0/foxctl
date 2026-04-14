# API Server

Current HTTP surface for `foxctl web serve`.

## Current status

This is the live `/api` server surface registered in `internal/interfaces/web/server.go`.
It is not the older `/api/v1` draft contract.

- Base path: `/api`
- Legacy `/api/v1/*`: removed and now returns `EDEPRECATED`
- WebSocket console path: `/ws/console/{id}`
- Kubernetes probe paths: `/healthz`, `/readyz`

## Current response styles

There are two main response patterns:

1. Protocol-style command envelopes for many command-oriented handlers.
2. JSON resource responses for list/detail style endpoints.

Large payloads may be artifactized through CAS, especially in orchestration and
other high-volume responses.

## Route groups

### System and transport

- `GET /api/health`
- `GET /api/events`
- `GET /api/openapi.json`
- `GET /api/swagger`
- `GET /api/swagger/`
- `GET /healthz`
- `GET /readyz`
- `WS /ws/console/{id}`

### OAuth and platform webhooks

- `GET /api/oauth/callback`
- `POST /api/teams/messages`

### Jobs, CAS, and workspaces

- `GET /api/jobs`
- `GET /api/jobs/{id}`
- `GET|HEAD|POST|DELETE /api/cas/{digest...}`
- `GET /api/workspaces`
- `POST /api/workspaces/switch`

### Skills

- `GET /api/skills`
- `GET /api/skills/schema`
- `POST /api/skills/run`
- `GET|POST|PUT|DELETE /api/skills/{skill...}`

### Console sessions

- `GET|POST /api/console/sessions`
- `GET|POST|DELETE|PATCH /api/console/sessions/{id...}`

### Core state surfaces

- `GET|POST /api/tasks`
- `GET|PATCH|POST /api/tasks/{id...}`
- `GET /api/sessions`
- `GET /api/sessions/{id...}`
- `GET /api/agents`
- `POST|GET|PATCH|DELETE /api/agents/{id...}`
- `POST /api/agents/spawn`
- `POST /api/agents/{id}/ask`
- `POST /api/agents/{id}/daemon/start`
- `POST /api/agents/{id}/daemon/kill`
- `GET /api/agents/{id}/daemon/sessions`

### Stats and collaboration

- `GET /api/stats`
- `GET /api/insights`
- `GET|POST|PATCH /api/mailbox`
- `GET|POST /api/rooms`
- `GET|POST|PATCH /api/rooms/{id...}`
- `GET /api/rooms/{id}/events`
- `GET|POST|DELETE /api/reservations`
- `GET|POST /api/blackboard`
- `GET|POST|PATCH|DELETE /api/blackboard/{id...}`

#### Room-specific notes

- `GET /api/events` remains the global invalidation/event feed used by broad UI surfaces.
- `GET /api/rooms/{id}/events?workspace_id=...` is the stable room-scoped SSE surface for room clients.
- Existing-room mutation routes are actor-authorized:
  room patch and full member replacement require coordinator access.
- Member transport and binding updates are self-service only for the target participant unless the caller has coordinator access.
- Role-changing member binding updates are coordinator-only even when the caller is updating their own binding.
- Room member payloads now expose `delivery_binding` as the canonical binding/routing record.
- Legacy top-level room member fields (`backend`, `session`, `pane_id`, `transport_endpoint`, `transport_kind`) are still mirrored for compatibility while clients migrate.
- Room status and loop/status responses expose `last_delivery_trace` from the persisted room-loop row.
- Treat `last_delivery_trace` as the canonical explanation surface for the latest delivery decision:
  it records the chosen binding, chosen transport endpoint/kind, fallback attempt, outcome, and cursor movement for the last observed delivery.
- Room reminder schedules are durable room-loop state:
  acknowledging one emitted reminder follow-up does not cancel the schedule.
- Reminder schedules can also stop automatically when linked `task_id`, `story_id`, or `milestone_id` work is satisfied, in addition to explicit cancel or `max_iterations`.
- For room-runtime verification, prefer `bash tests/regression/run.sh` as the canonical regression bundle instead of composing ad hoc command sets.
- If the symptom is "the message showed up in the pane but did not submit," follow that bundle with `AGENTCTL_INTEGRATION_TMUX=1 go test -tags='integration libsqlite3' ./cmd/foxctl/cmd -run 'TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux' -count=1 -v` to prove the target terminal process actually consumed the relayed line.

### Search and inspection

- `GET /api/logs`
- `GET /api/sqlite`
- `GET /api/sqlite/{...}`
- `GET|POST /api/search`
- `GET /api/codemaps`
- `GET /api/codemaps/{id}`

### Orchestration

- `POST /api/orchestration/dispatch-issue`
- `POST /api/orchestration/card-action`
- `GET /api/orchestration/board-get`
- `GET /api/orchestration/board-card-get`
- `GET /api/orchestration/board-card-runtime-get`
- `POST /api/orchestration/refresh`
- `POST /api/orchestration/seed-cards`

These routes are the main HTTP view into the v2/Jido-backed orchestration board
and projection state.

### Companion

- `GET /api/companion/providers`
- `POST /api/companion/chat`
- `GET /api/companion/conversations`
- `DELETE|PATCH /api/companion/conversations/{id}`
- `GET|DELETE /api/companion/conversations/{id}/messages`
- `POST /api/companion/conversations/{id}/compress`
- `GET|PATCH /api/companion/conversations/{id}/personality`
- `PATCH /api/companion/conversations/{id}/personality/dimension`
- `GET|PATCH /api/companion/conversations/{id}/settings`
- `POST /api/companion/context`
- `GET|DELETE /api/companion/context/{conversation_id...}`
- `POST /api/companion/characters`
- `GET /api/companion/characters/{conversation_id...}`
- `POST /api/companion/characters/{conversation_id}/{character_id}/overlays`
- `GET /api/companion/memory/{conversation_id}/stats`
- `GET /api/companion/memory/{conversation_id}/context`
- `GET /api/companion/memory/{conversation_id}/export`
- `GET /api/companion/memory/{conversation_id}/search`
- `POST /api/companion/memory/{conversation_id}/import`
- `DELETE /api/companion/memory/{conversation_id}`

## Current architecture notes

- The API server is hybrid: some handlers are classic storage/resource handlers,
  while orchestration, ask state, and runtime tree inspection now intersect with
  v2 projections and Jido-backed runtime data.
- `/api/agents/*` is a mix of local store operations, daemon-backed agent
  control, and newer runtime-state inspection helpers.
- `/api/orchestration/*` is the clearest current v2/Jido-backed HTTP surface.

## Versioning note

Earlier docs referred to `/api/v1` as the primary base path. That is no longer
current. The live server serves `/api/*`, and `/api/v1/*` now exists only as a
deprecation response path.

## Related docs

- `docs/architecture/system-architecture.md`
- `docs/general/runtime-orchestration.md`
- `docs/architecture/chat-platform-adapter.md`
- `docs/architecture/kubernetes-runtime.md`
- `docs/general/api-server.openapi.yaml`
