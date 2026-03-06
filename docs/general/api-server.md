# API Server

Current HTTP surface for `agentctl web serve`.

## Current status

This is the live `/api` server surface registered in `internal/web/server.go`.
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
- `GET|POST|DELETE /api/reservations`
- `GET|POST /api/blackboard`
- `GET|POST|PATCH|DELETE /api/blackboard/{id...}`

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
