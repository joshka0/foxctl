---
title: API server
description: foxctl HTTP API surface, route groups, probes, response conventions, and auth integration.
---

The foxctl API server is the HTTP surface behind `foxctl web serve`. It exposes runtime state, agent management, room coordination, orchestration, companion chat, and retrieval endpoints under the base path `/api`.

## Base path and probes

The live server uses `/api` as the base path. The older `/api/v1/*` prefix has been removed and now returns `EDEPRECATED`.

| Path | Purpose |
|---|---|
| `GET /api/health` | Health check |
| `GET /healthz` | Kubernetes liveness probe |
| `GET /readyz` | Kubernetes readiness probe |
| `WS /ws/console/{id}` | WebSocket console session |

## Response conventions

There are two main response patterns:

1. **Protocol-style command envelopes** for command-oriented handlers. These follow the [Protocol v1](/reference/protocol-v1/) envelope shape with `version`, `status`, `command`, `data`, `meta`, and `error` fields.
2. **JSON resource responses** for list/detail style endpoints.

Large payloads may be artifactized through CAS, especially in orchestration and other high-volume responses. When a response exceeds the CAS threshold, the envelope contains `data.summary` plus a `data.artifact` pointer instead of the full payload inline.

## Route groups

### System and transport

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Health status |
| `GET` | `/api/events` | Global invalidation/event feed (SSE) |
| `GET` | `/api/openapi.json` | OpenAPI schema |
| `GET` | `/api/swagger` | Swagger UI |
| `GET` | `/healthz` | K8s liveness |
| `GET` | `/readyz` | K8s readiness |
| `WS` | `/ws/console/{id}` | Console WebSocket |

### OAuth and platform webhooks

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/oauth/callback` | OAuth callback handler (currently stubbed) |
| `POST` | `/api/teams/messages` | Teams platform webhook |

### Jobs, CAS, and workspaces

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/jobs` | List jobs |
| `GET` | `/api/jobs/{id}` | Job state |
| `GET/HEAD/POST/DELETE` | `/api/cas/{digest...}` | CAS artifact access |
| `GET` | `/api/workspaces` | Workspace inventory |
| `POST` | `/api/workspaces/switch` | Switch active workspace |

### Skills

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/skills` | Skill inventory |
| `GET` | `/api/skills/schema` | Skill schema metadata |
| `POST` | `/api/skills/run` | Run a skill via API |
| `GET/POST/PUT/DELETE` | `/api/skills/{skill...}` | Skill CRUD |

### Console sessions

| Method | Path | Purpose |
|---|---|---|
| `GET/POST` | `/api/console/sessions` | List or create console sessions |
| `GET/POST/DELETE/PATCH` | `/api/console/sessions/{id...}` | Console session lifecycle |

### Core state surfaces

| Method | Path | Purpose |
|---|---|---|
| `GET/POST` | `/api/tasks` | Task list and creation |
| `GET/PATCH/POST` | `/api/tasks/{id...}` | Task detail and updates |
| `GET` | `/api/sessions` | Session list |
| `GET` | `/api/sessions/{id...}` | Session detail |
| `GET` | `/api/agents` | Agent inventory |
| `POST/GET/PATCH/DELETE` | `/api/agents/{id...}` | Agent CRUD |
| `POST` | `/api/agents/spawn` | Spawn a new agent |
| `POST` | `/api/agents/{id}/ask` | Send a question to an agent |
| `POST` | `/api/agents/{id}/daemon/start` | Start/resume agent daemon |
| `POST` | `/api/agents/{id}/daemon/kill` | Stop agent daemon |
| `GET` | `/api/agents/{id}/daemon/sessions` | List daemon sessions |

### Stats and collaboration

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/stats` | Runtime statistics |
| `GET` | `/api/insights` | Insights and analytics |
| `GET/POST/PATCH` | `/api/mailbox` | Mailbox operations |
| `GET/POST` | `/api/rooms` | Room list and creation |
| `GET/POST/PATCH` | `/api/rooms/{id...}` | Room state and updates |
| `GET` | `/api/rooms/{id}/events` | Room-scoped SSE events |
| `GET/POST/DELETE` | `/api/reservations` | Reservations |
| `GET/POST` | `/api/blackboard` | Blackboard operations |
| `GET/POST/PATCH/DELETE` | `/api/blackboard/{id...}` | Blackboard item lifecycle |

#### Room-specific notes

- `GET /api/events` remains the global invalidation/event feed used by broad UI surfaces.
- `GET /api/rooms/{id}/events?workspace_id=...` is the stable room-scoped SSE surface for room clients.
- Room mutation routes are actor-authorized: room patch and full member replacement require coordinator access.
- Member binding updates are self-service only for the target participant unless the caller has coordinator access.
- Role-changing member binding updates are coordinator-only even when the caller is updating their own binding.
- Room member payloads expose `delivery_binding` as the canonical binding/routing record. Legacy top-level fields (`backend`, `session`, `pane_id`, `transport_endpoint`, `transport_kind`) are not emitted in room member API responses.
- The legacy `/members/{actor_id}/transport` route has been removed; use `/members/{actor_id}/binding` with `delivery_binding`.
- Room status responses expose `last_delivery_trace` from the persisted room-loop row. This trace records the chosen binding, transport endpoint/kind, fallback attempt, outcome, and cursor movement for the latest delivery decision.
- Room reminder schedules are durable room-loop state. Acknowledging one emitted reminder does not cancel the schedule. Reminders can also stop automatically when linked `task_id`, `story_id`, or `milestone_id` work is satisfied.

### Search and inspection

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/logs` | Log access |
| `GET` | `/api/sqlite` | SQLite inspection |
| `GET` | `/api/sqlite/{...}` | SQLite query |
| `GET/POST` | `/api/search` | Search endpoint |
| `GET` | `/api/codemaps` | Codemap list |
| `GET` | `/api/codemaps/{id}` | Codemap detail |

### Orchestration

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/orchestration/dispatch-issue` | Dispatch an issue to the board |
| `POST` | `/api/orchestration/card-action` | Execute a card action |
| `GET` | `/api/orchestration/board-get` | Get board state |
| `GET` | `/api/orchestration/board-card-get` | Get card detail |
| `GET` | `/api/orchestration/board-card-runtime-get` | Get card runtime state |
| `POST` | `/api/orchestration/refresh` | Refresh board projections |
| `POST` | `/api/orchestration/seed-cards` | Seed cards onto the board |

These routes are the main HTTP view into the v2/Jido-backed orchestration board and projection state.

### Companion

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/companion/providers` | List LLM providers |
| `POST` | `/api/companion/chat` | Send a chat message |
| `GET` | `/api/companion/conversations` | List conversations |
| `DELETE/PATCH` | `/api/companion/conversations/{id}` | Conversation lifecycle |
| `GET/DELETE` | `/api/companion/conversations/{id}/messages` | Message access |
| `POST` | `/api/companion/conversations/{id}/compress` | Compress conversation |
| `GET/PATCH` | `/api/companion/conversations/{id}/personality` | Personality settings |
| `PATCH` | `/api/companion/conversations/{id}/personality/dimension` | Personality dimension |
| `GET/PATCH` | `/api/companion/conversations/{id}/settings` | Conversation settings |
| `POST` | `/api/companion/context` | Submit context |
| `GET/DELETE` | `/api/companion/context/{conversation_id...}` | Context lifecycle |
| `POST` | `/api/companion/characters` | Create character |
| `GET` | `/api/companion/characters/{conversation_id...}` | Character detail |
| `POST` | `/api/companion/characters/{conversation_id}/{character_id}/overlays` | Character overlays |
| `GET` | `/api/companion/memory/{conversation_id}/stats` | Memory stats |
| `GET` | `/api/companion/memory/{conversation_id}/context` | Memory context |
| `GET` | `/api/companion/memory/{conversation_id}/export` | Export memory |
| `GET` | `/api/companion/memory/{conversation_id}/search` | Search memory |
| `POST` | `/api/companion/memory/{conversation_id}/import` | Import memory |
| `DELETE` | `/api/companion/memory/{conversation_id}` | Clear memory |

## Architecture notes

The API server is hybrid: some handlers are classic storage/resource handlers, while orchestration, ask state, and runtime tree inspection now intersect with v2 projections and Jido-backed runtime data.

- `/api/agents/*` is a mix of local store operations, daemon-backed agent control, and newer runtime-state inspection helpers.
- `/api/orchestration/*` is the clearest current v2/Jido-backed HTTP surface.
- `/api/companion/*` provides the full companion chat and memory surface.

## Auth integration

All routes that modify state enforce principal propagation through the auth middleware. See [Auth and identity](/architecture/auth-and-identity/) for how principals, Casbin policies, and the OAuth broker work.

## Local checks

Start the server and inspect routes:

```bash
foxctl web serve --help
```

The MCP server can also be started locally:

```bash
foxctl mcp serve --daemon --skills
```

## OpenAPI spec

The canonical OpenAPI spec lives at `docs/general/api-server.openapi.yaml`. It defines request/response schemas, envelope shapes, and parameter types. Note that the spec still references `/api/v1` in server URLs for backward compatibility, but the live server serves `/api/*`.

## Related docs

- [Auth and identity](/architecture/auth-and-identity/) — principal propagation and Casbin
- [Runtime architecture](/architecture/runtime/) — v2/Jido-backed surfaces
- [Protocol v1](/reference/protocol-v1/) — envelope shape and status rules
- [Chat platforms](/integrations/chat-platforms/) — adapter runtime architecture
- [Kubernetes deployment](/deployment/kubernetes/) — probe configuration
