# API Server v1 (Draft)

Purpose: Provide a stable HTTP surface for subagents, memory, session recall, todos, multi-agent coordination (blackboard, mailbox, overseer), skills, and hooks.

## Goals
- HTTP API for GUI and external clients with explicit workspace scoping
- Lightweight JSON responses; skill endpoints return envelopes
- CAS-backed artifacts for large outputs

## Non-goals (v1)
- Remote auth, multi-tenant isolation, or public deployment
- Long-term compatibility guarantees beyond v1

## Versioning and Base Paths
- Primary: /api/v1
- Legacy alias: /api (until clients migrate)

## Response Envelope (non-skill endpoints)
```yaml
ok: true
data: {}
error:
  code: EARG
  message: workspace required
  hint: set ?workspace=...
meta:
  request_id: req_...
  ts: 2026-01-21T00:00:00Z
  duration_ms: 12
```

- Error codes reuse Protocol v1: EARG, EPOLICY, ENOTFOUND, ERUNTIME, ETIMEOUT.

## Workspace Scoping
- Required for storage-backed endpoints (tasks, memory, sessions, mailbox).
- Accept workspace query param or X-Workspace header.

## Large Outputs
- Inline summaries under data.summary with data.artifact (CAS digest) for large payloads.

## Local-only and Remote-ready
- Default bind: 127.0.0.1
- Remote-ready plan: bearer auth, TLS/mTLS, rate limits, audit logs

## Endpoint Map (Draft)
```yaml
base: /api/v1
system:
  - GET /health
  - GET /status
  - GET /stats
  - GET /insights
workspaces:
  - GET /workspaces
  - POST /workspaces/switch
skills:
  - GET /skills
  - GET /skills/{name}
  - GET /skills/schema
  - POST /skills/run
  - POST /skills/{name}/run
jobs:
  - GET /jobs
  - GET /jobs/{id}
  - POST /jobs
  - DELETE /jobs/{id}
cas:
  - POST /cas
  - GET /cas/{digest}
  - HEAD /cas/{digest}
  - POST /cas/{digest}/pin
  - DELETE /cas/{digest}/pin
agents:
  - GET /agents
  - POST /agents
  - GET /agents/{id}
  - PATCH /agents/{id}
  - POST /agents/{id}/start
  - POST /agents/{id}/stop
  - GET /agents/{id}/sessions
sessions:
  - GET /sessions
  - GET /sessions/{id}
  - GET /sessions/{id}/messages
  - GET /sessions/{id}/context-windows
  - POST /sessions/{id}/restore
  - POST /sessions/recall
memory:
  - GET /memories
  - GET /memories/{name}
  - POST /memories
  - PATCH /memories/{name}
  - DELETE /memories/{name}
  - POST /memories/search
  - GET /memories/stats
contextvars:
  - GET /contextvars
  - POST /contextvars
  - POST /contextvars/query
  - DELETE /contextvars/{key}
tasks:
  - GET /tasks
  - POST /tasks
  - GET /tasks/{id}
  - PATCH /tasks/{id}
  - POST /tasks/{id}/complete
  - POST /tasks/{id}/uncomplete
  - POST /tasks/{id}/dependencies
  - DELETE /tasks/{id}/dependencies/{dep}
  - GET /tasks/graph
overseer:
  - GET /overseer/recommendations
  - POST /overseer/run
  - GET /overseer/weights
blackboard:
  - GET /blackboard
  - POST /blackboard
  - POST /blackboard/{id}/lease
  - DELETE /blackboard/{id}
mailbox:
  - GET /mailbox
  - POST /mailbox
  - PATCH /mailbox/{id}
reservations:
  - GET /reservations
  - POST /reservations
  - DELETE /reservations/{id}
hooks:
  - GET /hooks
  - POST /hooks/reload
  - POST /hooks/validate
  - POST /hooks/run
search:
  - GET /search
  - POST /search
codemaps:
  - GET /codemaps
  - GET /codemaps/{id}
  - POST /codemaps/search
  - POST /codemaps/generate
companion:
  - POST /companion/chat
  - GET /companion/conversations
  - POST /companion/context
  - GET /companion/context/{conversation_id}
  - DELETE /companion/context/{conversation_id}
  - DELETE /companion/context/{conversation_id}/{key}
  - GET /companion/memory/{conversation_id}/stats
  - GET /companion/memory/{conversation_id}/context
  - DELETE /companion/memory/{conversation_id}
realtime:
  - GET /events
  - WS /ws/console/{id}
```

## OpenAPI Stub
- docs/general/api-server.openapi.yaml
