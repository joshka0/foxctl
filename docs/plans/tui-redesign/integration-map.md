# Integration Map — TUI Operator Cockpit

This document lists every API endpoint and streaming surface the TUI operator
cockpit consumes, along with the recommended adapter struct, key methods,
lifecycle semantics, and backpressure policy for each.

The TUI communicates exclusively with `foxctl web serve` over HTTP/SSE.
There is no direct database access, no shared-memory IPC, and no JSON-RPC
socket from the TUI process. All streaming uses Server-Sent Events (SSE)
over long-lived HTTP connections.

## Reference Conventions

- **Handler locations** cite `internal/interfaces/web/api/<file>.go:<line>`.
- **Adapter locations** cite `internal/interfaces/tui/<file>.go:<line>`.
- **Struct names** are the Go types that wrap each endpoint.
- "Streaming" rows deliver events incrementally; "request/response" rows are
  single round-trips.

---

## Endpoint Map

| # | Endpoint | Method | Streaming | Handler Location | Adapter Struct | Key Methods | Lifecycle (start / stop / cancel) | Backpressure Policy |
|---|----------|--------|-----------|------------------|----------------|-------------|-----------------------------------|---------------------|
| 1 | `GET /api/agents` | GET | No | `api/agents.go:327` (`AgentsListHandler`) | `AgentAdapter` (`agent_adapter.go:14`) | `ListAgents(ctx, limit)`, `GetAgent(ctx, id)` | **Start**: per-call HTTP round-trip. **Stop**: response received. **Cancel**: `ctx` cancellation aborts the underlying `*http.Request` via `http.NewRequestWithContext`. | N/A — single request/response. No buffering. Caller controls concurrency by gating serial or parallel calls. |
| 2 | `GET /api/agents/{id}` | GET | No | `api/agents.go:426` (`AgentDetailHandler`) | `AgentAdapter` (`agent_adapter.go:14`) | `GetAgent(ctx, id)`, `ListAgents(ctx, limit)` | **Start**: per-call HTTP round-trip. **Stop**: response received. **Cancel**: `ctx` cancellation aborts the request. | N/A — single request/response. |
| 3 | `POST /api/agents/{id}/ask-stream` | POST | **Yes** (SSE via hub relay) | `api/agents.go:1331` (`handleAgentAskStream`) | `ConsoleAskRuntime` (`console_ask_runtime.go:96`) + SSE subscription via `/api/events` topic filter | `Enqueue(ctx, req)`, `Updates() <-chan ConsoleAskUpdate` | **Start**: `POST` returns `202 Accepted` immediately with a `correlation_id`. Server publishes `started`/`delta`/`tool_call`/`tool_result`/`completed`/`error`/`cancelled` events to the SSE hub, keyed by `correlation_id`. The TUI subscribes to `/api/events?topic=agent_chat` to receive these frames. **Stop**: stream ends when server publishes `completed`, `error`, or `cancelled` phase. The `activeAgentStreams` registry (`api/agents.go:266`) deletes the correlation entry on goroutine exit. **Cancel**: `POST /api/agents/{id}/ask-stream/cancel` calls `activeAgentStreams.Cancel(agentID, correlationID)` which invokes the stored `context.CancelFunc`, causing the companion streaming goroutine to exit and emit a `cancelled` phase event. | **Client-side**: `ConsoleAskRuntime` uses a bounded `requests` channel (default cap 16) and a bounded `updates` channel (default cap 16). When the updates channel is full, `sendUpdate()` blocks until the consumer drains or the runtime context is cancelled — this is the primary backpressure signal. The TUI event loop must drain `Updates()` promptly to avoid stalling the runtime goroutine. **Server-side**: `agentStreamRegistry` has no built-in per-agent concurrency limit — multiple concurrent asks to the same agent are allowed. The SSE hub's per-client `Send` channel (cap 64) drops events when full (best-effort broadcast). The TUI adapter must tolerate missed SSE frames gracefully. |
| 4 | `POST /api/agents/{id}/ask-stream/cancel` | POST | No | `api/agents.go:1498` (`handleAgentAskStreamCancel`) | `ConsoleCancelRuntime` (`console_cancel_runtime.go:72`) | `Enqueue(ctx, req)`, `Updates() <-chan ConsoleCancelUpdate` | **Start**: per-call HTTP round-trip. Returns `200 OK` with `cancelled` count (0 or 1). **Stop**: response received. **Cancel**: `ctx` cancellation on the cancel request itself (separate from the ask-stream being cancelled). | **Client-side**: `ConsoleCancelRuntime` uses bounded `requests` (cap 16) and `updates` (cap 16) channels with the same blocking-backpressure model as `ConsoleAskRuntime`. **Server-side**: `activeAgentStreams.Cancel` is synchronous and O(1) per correlation — no backpressure concern. |
| 5 | `GET /api/events` (topic filtered) | GET | **Yes** (SSE long-poll) | `sse/handler.go:11` (`Handler`) / `sse/hub.go:182` (`TopicHandler`) | New dedicated adapter (recommended: `EventsSubscriber`) consuming `sse.Hub.TopicHandler` | `Subscribe(ctx, topics ...string)`, `Events() <-chan SSEEvent` (proposed) | **Start**: HTTP GET opens a long-lived SSE connection. Server sends `connected` event with `client_id`, then streams `data: {json}\n\n` frames. Topic filtering is server-side: only clients subscribed to matching topics receive the event. **Stop**: client disconnects (closes TCP), or `r.Context()` is cancelled (e.g., TUI shutdown). Hub unregisters the client and closes its `Send` channel. **Cancel**: `ctx.Done()` closes the HTTP connection. The hub's `Run()` goroutine detects the unregister and removes the client. | **Server-side**: each `Client.Send` channel has capacity 64. When full, the hub skips the event for that client (silent drop). This prevents a slow consumer from blocking the broadcast path. **Client-side (TUI)**: the proposed `EventsSubscriber` adapter should use a bounded `chan SSEEvent` (recommended cap 128–256) and a dedicated goroutine that reads from the SSE response body and forwards events. If the TUI's event channel is full, the adapter should either drop the event with a log or apply back-pressure by blocking the read, depending on the criticality of the event. For the cockpit, dropping stale events is acceptable for non-critical event types (e.g., repeated `heartbeat`), but `invalidate` events must not be dropped (they trigger UI re-fetches). Recommended strategy: **ring-buffer with overwrite for heartbeat, blocking for invalidate/agent_chat**. |
| 6 | Agent hierarchy via `GET /api/agents/{id}/runtime?depth=N` | GET | No | `api/agents.go:1522` (`handleAgentRuntimeGet`) | `AgentAdapter` (`agent_adapter.go:14`) — extend with `GetAgentRuntime(ctx, id, depth)` | `GetAgent(ctx, id)` (existing), `GetAgentRuntime(ctx, id, depth)` (proposed) | **Start**: per-call HTTP round-trip. Returns a `runtime` tree with `agent_id`, `worker_id`, `status`, `children[]` up to the requested depth (default 2, max 5). **Stop**: response received. **Cancel**: `ctx` cancellation aborts the request. | N/A — single request/response. The tree depth is bounded by `maxRuntimeTreeDepth` (5). |
| 7 | `GET /api/rooms` | GET | No | `api/rooms.go:294` (`RoomsListHandler`) | New adapter (recommended: `RoomAdapter`) | `ListRooms(ctx, workspaceID, limit)` (proposed), `GetRoom(ctx, roomID)` (proposed) | **Start**: per-call HTTP round-trip. **Stop**: response received. **Cancel**: `ctx` cancellation aborts the request. | N/A — single request/response. |
| 8 | `GET /api/rooms/{id}/events` | GET | **Yes** (SSE via topic-filtered hub) | `api/rooms.go:628` (`handleRoomEventsGet`) | New adapter (recommended: `RoomEventsSubscriber`) | `Subscribe(ctx, workspaceID, roomID)` (proposed), `Events() <-chan RoomMessage` (proposed) | **Start**: HTTP GET opens a long-lived SSE connection. Server delegates to `Hub.TopicHandler(roomEventTopic(workspaceID, roomID))`, which only delivers events scoped to that room. **Stop**: client disconnects or `ctx` cancellation. Hub unregisters the client. **Cancel**: `ctx.Done()` closes the connection. | **Server-side**: identical to `/api/events` — per-client `Send` channel (cap 64) with silent drop on full. **Client-side (TUI)**: same pattern as `EventsSubscriber` but scoped to a single room. The proposed `RoomEventsSubscriber` should use a bounded channel (recommended cap 64–128). Room events are generally lower volume than the global event feed, but message bursts can occur when multiple agents reply concurrently. The adapter should buffer messages and the TUI should drain promptly to avoid skip. |

---

## Streaming Endpoints — Cancellation Detail

### `POST /api/agents/{id}/ask-stream`

| Aspect | Detail |
|--------|--------|
| **Cancel trigger** | `POST /api/agents/{id}/ask-stream/cancel` with `{ "correlation_id": "..." }` |
| **Cancel path (server)** | `activeAgentStreams.Cancel(agentID, correlationID)` invokes the stored `context.CancelFunc`. The companion streaming goroutine receives `context.Canceled`, emits a `cancelled` phase event, and exits. |
| **Cancel path (TUI)** | `ConsoleCancelRuntime.Enqueue(ctx, CancelConsoleSessionRequest{CorrelationID: corrID})` → HTTP POST → server cancels. The TUI also receives the `cancelled` SSE event on its `/api/events` subscription and updates the transcript row. |
| **Graceful teardown** | Server goroutine: `defer activeAgentStreams.Delete(agentID, correlationID)` + `defer cancel()` + `defer cleanup()`. All three run regardless of success/error/cancel. |
| **Timeout** | Default 30 min, configurable per-agent via `agent.Policy.Timeout`. |

### `GET /api/events` (topic filtered)

| Aspect | Detail |
|--------|--------|
| **Cancel trigger** | TUI closes the HTTP connection (calls `cancel()` on the subscription context). |
| **Cancel path (server)** | `r.Context()` done → handler returns → hub unregisters client → `client.Send` channel closed. |
| **Reconnection** | TUI should implement exponential backoff reconnection (1s, 2s, 4s, max 30s) with a `Last-Event-ID` header if supported. On reconnect, issue a full re-fetch of affected data to reconcile missed events. |
| **Heartbeat** | Server sends heartbeat every 30 seconds (`Hub.Run` ticker). TUI should treat 3 missed heartbeats as connection degraded. |

### `GET /api/rooms/{id}/events`

| Aspect | Detail |
|--------|--------|
| **Cancel trigger** | TUI closes the HTTP connection (calls `cancel()` on the subscription context). Typically when navigating away from the room detail view. |
| **Cancel path (server)** | Identical to `/api/events` — same SSE hub, same `TopicHandler`, same client lifecycle. |
| **Reconnection** | Same strategy as `/api/events`. On reconnect, re-fetch recent messages via `GET /api/rooms/{id}/messages?limit=N` to reconcile. |

---

## Non-Streaming Endpoints — Lifecycle Summary

| Endpoint | Start | Response Shape | Error Handling |
|----------|-------|---------------|----------------|
| `GET /api/agents` | `APIClient.RequestJSON(ctx, GET, "/api/agents?limit=N", nil, &resp)` | `{ agents: AgentRecord[], total: int }` wrapped in envelope | `HTTPStatusError` with status code and body |
| `GET /api/agents/{id}` | `APIClient.RequestJSON(ctx, GET, "/api/agents/{id}", nil, &resp)` | `{ agent: AgentRecord }` wrapped in envelope | 404 if agent not found |
| `POST /api/agents/{id}/ask-stream/cancel` | `APIClient.RequestJSON(ctx, POST, "/api/agents/{id}/ask-stream/cancel", req, &resp)` | `{ ok: bool, agent_id: string, correlation_id: string, cancelled: int }` | 200 even if no active stream was found (`cancelled: 0`) |
| `GET /api/agents/{id}/runtime?depth=N` | `APIClient.RequestJSON(ctx, GET, "/api/agents/{id}/runtime?depth=N", nil, &resp)` | `{ runtime: { root: RuntimeTreeNode, error: string } }` | 404 if agent not found; `runtime.error` if tree traversal fails |
| `GET /api/rooms` | `APIClient.RequestJSON(ctx, GET, "/api/rooms?workspace_id=...&limit=N", nil, &resp)` | `{ rooms: RoomSummary[] }` wrapped in envelope | Standard envelope error |

---

## Adapter Inventory — Current vs Proposed

| Struct | File | Status | Consumes |
|--------|------|--------|----------|
| `APIClient` | `internal/interfaces/tui/api_client.go:19` | **Current** | Base HTTP transport for all adapters |
| `AgentAdapter` | `internal/interfaces/tui/agent_adapter.go:14` | **Current** | `GET /api/agents`, `GET /api/agents/{id}`, `POST /api/agents/{id}/ask` |
| `ConsoleAdapter` | `internal/interfaces/tui/console_adapter.go:15` | **Current** | `GET /api/console/sessions`, `POST .../ask`, `POST .../cancel` |
| `ConsoleAskRuntime` | `internal/interfaces/tui/console_ask_runtime.go:96` | **Current** | Wraps ask submission in a bounded goroutine |
| `ConsoleCancelRuntime` | `internal/interfaces/tui/console_cancel_runtime.go:72` | **Current** | Wraps cancel submission in a bounded goroutine |
| `ConsoleStreamPump` | `internal/interfaces/tui/console_stream_pump.go:51` | **Current** | Wraps SSE reading in a bounded goroutine |
| `EventsSubscriber` | _proposed_ | **Proposed** (M2/M3) | `GET /api/events` with topic filtering |
| `RoomAdapter` | _proposed_ | **Proposed** (M2/M3) | `GET /api/rooms`, `GET /api/rooms/{id}` |
| `RoomEventsSubscriber` | _proposed_ | **Proposed** (M2/M3) | `GET /api/rooms/{id}/events` |
| `AgentRuntimeAdapter` | _proposed_ (extend `AgentAdapter`) | **Proposed** (M3) | `GET /api/agents/{id}/runtime?depth=N` |

---

## SSE Event Types

The TUI must handle these SSE event types received from `/api/events`:

| Event Type | Source | TUI Action |
|------------|--------|------------|
| `connected` | `sse/handler.go:43` | Store `client_id`; log connection established. |
| `heartbeat` | `sse/hub.go:131` | Reset connection-health timer. |
| `invalidate` | `sse/hub.go:180` (`Invalidate`) | Re-fetch the named data keys (e.g., `agents` → call `ListAgents`). |
| `agent_chat` | `api/agents.go` (`publishAgentChatEvent`) | Render streaming tokens, tool calls, completion, error, or cancellation for the active agent conversation. |
| `room_message` | `api/rooms.go` (`publishRoomMessageEvent`) | Append to room transcript if room view is active. |

---

## Topic Filter Strategy

The SSE hub supports topic-scoped subscriptions via `Hub.TopicHandler(topics...)`.
The TUI should maintain **one global subscription** to `/api/events` (no topic
filter) for broad invalidation + heartbeat, and open **per-room scoped
subscriptions** to `/api/rooms/{id}/events` only when a room detail view is
active. This avoids duplicating the agent chat event stream (which flows
through the global hub without a topic filter).

When the TUI adds topic-aware subscription support, the recommended topic
subscriptions are:

| Subscription | Topics | Purpose |
|-------------|--------|---------|
| Global | _(none — receives all)_ | Heartbeat, invalidate, agent_chat |
| Room-scoped | `room:{workspaceID}:{roomID}` | Room-scoped messages and events |

---

## Connection Health Model

The TUI status footer should reflect connection health based on:

| State | Condition | Visual |
|-------|-----------|--------|
| **ok** | SSE connection active + heartbeat received within last 90s | Green indicator |
| **degraded** | SSE connected but heartbeat missed (3+ missed) | Yellow indicator |
| **error** | SSE disconnected or HTTP requests failing | Red indicator with URL label |

Reconnection strategy: exponential backoff (1s → 2s → 4s → 8s → 16s → 30s cap).
On reconnect, perform a full re-fetch of the agent inventory and any active room data.
