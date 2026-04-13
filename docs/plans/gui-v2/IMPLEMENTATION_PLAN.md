# GUI v2 Implementation Plan

> Comprehensive plan for replacing the Bun/Express server with a Go backend + optionally migrating to Svelte SPA, including "Console" functionality as a Claude Code alternative.

---

## Continuation Notes

**Current Progress:** Implementing Phases 1-5 (Go backend foundation + Skills/Tools)

**After Phase 5, pick up:**
- **Phases 6-10:** Console functionality (WebSocket, REST/SSE, streaming, sessions, GUI page)
- **Phases 11-12:** Replace Bun server, optional Svelte SPA migration

**Full implementation plan location:** `docs/plans/gui-v2/IMPLEMENTATION_PLAN.md`

---

## Svelte SPA Migration Progress (Phase 12)

**Completed:**
- Phase A: Foundation (scaffold, UI components, layout, routing, simple pages)
- Phase B: Detail pages (JobDetailPage, TaskDetailPage, SessionsPage, AgentsPage, StatsPage)
- Phase C: Advanced pages (SearchPage, SQLitePage, InsightsPage, CodemapsPage)

**In Progress:**
- Phase D: Console page (wired to `/api/console/sessions/*`)

**Pending:**
- Console polish (tool result rendering, metrics, retry UX)
- Workspace selection in header (global vs per-page)

---

## Executive Summary

**Goal:** Create a native "agentctl Studio" experience that:
1. Replaces the Express server with a **Go HTTP+SSE/WS** backend
2. Powers existing dashboard pages (jobs/tasks/agents/mailbox/etc.)
3. Adds a native **Console** page for interactive chat with streaming tool calls
4. Optionally migrates from React to Svelte SPA

**Key Deliverables:**
- Go web server at `cmd/agentctl_web`
- Console WebSocket/SSE endpoints for real-time streaming
- Skill tool registry with JSON Schema generation
- Tool executor that routes to daemon or fallback execution
- (Optional) Svelte SPA at `packages/gui-svelte`

---

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Browser (SPA)                            │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐ │
│  │ Dashboard   │  │ Console     │  │ Sessions/Jobs/Tasks/... │ │
│  │ Pages       │  │ (Chat UI)   │  │                         │ │
│  └──────┬──────┘  └──────┬──────┘  └───────────┬─────────────┘ │
└─────────┼────────────────┼─────────────────────┼───────────────┘
          │                │                     │
          ▼                ▼                     ▼
     /api/* REST      /ws/console/*         /api/events
                      (WebSocket)           (SSE)
          │                │                     │
┌─────────┴────────────────┴─────────────────────┴───────────────┐
│                    cmd/agentctl_web                            │
│  ┌────────────────────────────────────────────────────────────┐│
│  │                  internal/interfaces/web/                             ││
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────────────┐ ││
│  │  │ api/     │  │ consolews│  │ sse/                     │ ││
│  │  │ handlers │  │ hub.go   │  │ hub.go                   │ ││
│  │  └────┬─────┘  └────┬─────┘  └────────────┬─────────────┘ ││
│  └───────┼─────────────┼─────────────────────┼───────────────┘│
└──────────┼─────────────┼─────────────────────┼────────────────┘
           │             │                     │
           ▼             ▼                     ▼
    ┌──────────────────────────────────────────────────────────┐
    │              internal/storage/*                          │
    │  jobs.db │ cas/ │ mailbox.db │ sessions.db │ agents.db  │
    └──────────────────────────────────────────────────────────┘
```

---

## Phase 1: Go Web Server Skeleton + SSE Hub
**Estimated effort: 1-2 days**

### Deliverables
- `cmd/agentctl_web/main.go` - Server entrypoint
- `internal/interfaces/web/server.go` - HTTP handler wiring
- `internal/interfaces/web/options.go` - Configuration struct
- `internal/interfaces/web/sse/hub.go` - Global SSE pub/sub for invalidation
- `internal/interfaces/web/sse/handler.go` - `/api/events` SSE endpoint
- `internal/interfaces/web/api/status.go` - `/api/health` endpoint

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Server health + config info |
| GET | `/api/events` | SSE stream for UI invalidation |

### Acceptance Criteria
- [ ] `curl localhost:8090/api/health` returns JSON with `ok: true`
- [ ] `curl -N localhost:8090/api/events` connects and stays open
- [ ] SSE hub can `Publish(topic, payload)` and fanout to clients
- [ ] CORS enabled for local dev (`http://localhost:5173`)

### Key Code Patterns
```go
// SSE invalidation events (not full data)
// UI invalidates React Query keys on receipt
type InvalidateEvent struct {
    Keys []string `json:"keys"` // ["jobs", "tasks"]
}
```

---

## Phase 2: Jobs + CAS API
**Estimated effort: 2-3 days**

### Deliverables
- `internal/interfaces/web/api/jobs.go` - Jobs list/detail handlers
- `internal/interfaces/web/api/cas.go` - CAS metadata/content handlers
- `internal/interfaces/web/services/jobs_service.go` - Storage wrapper

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/jobs?state=&limit=&workspace=` | List jobs |
| GET | `/api/jobs/:id` | Job detail + artifacts + stderr |
| GET | `/api/jobs/:id/result` | Raw result.json |
| GET | `/api/jobs/:id/stderr` | Raw stderr.log |
| GET | `/api/cas/sha256/:hex/meta` | CAS object metadata |
| GET | `/api/cas/sha256/:hex/raw` | Stream CAS content |

### State Mapping Fix
Current GUI uses legacy states. Align to canonical:
| Legacy (GUI) | Canonical (storage) |
|--------------|---------------------|
| pending | queued |
| completed | ok |
| failed | error |
| cancelled | canceled |

### Acceptance Criteria
- [ ] Jobs page populates from Go backend
- [ ] Job detail shows result data, artifacts, stderr
- [ ] CAS artifacts clickable → download/preview
- [ ] State filters work with canonical values

---

## Phase 3: Board/Mailbox + Blackboard + Agents API
**Estimated effort: 2-3 days**

### Deliverables
- `internal/interfaces/web/api/mailbox.go` - BoardMessages endpoints
- `internal/interfaces/web/api/blackboard.go` - Blackboard CRUD
- `internal/interfaces/web/api/agents.go` - Agent list + daemon control
- `internal/interfaces/web/api/reservations.go` - File reservations

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/mailbox?workspace_id=&actor_id=&limit=` | List messages |
| POST | `/api/mailbox/mark_read` | Mark messages read |
| POST | `/api/mailbox/ack` | Acknowledge messages |
| GET | `/api/reservations?workspace_id=` | List file reservations |
| POST | `/api/reservations/reserve` | Create reservation |
| POST | `/api/reservations/release` | Release reservation |
| GET | `/api/blackboard?ns=&topic=&limit=` | List records |
| POST | `/api/blackboard/post` | Post record |
| GET | `/api/agents?limit=` | List agents |
| POST | `/api/agents/:id/start_daemon` | Start agent daemon |

### Acceptance Criteria
- [ ] MailboxPage displays real data
- [ ] ReservationsPage shows active file locks
- [ ] BlackboardPage shows key-value records
- [ ] AgentsPage can start/stop daemons

---

## Phase 4: Skills Registry + Tool Schema Generation
**Estimated effort: 2-3 days**

### Deliverables
- `internal/skills/names.go` - Tool name normalization (`code/symbols` → `code_symbols`)
- `internal/skills/schema.go` - `skill.Parameter` → JSON Schema conversion
- `internal/skills/registry.go` - Discover skills, build tool defs, mapping
- `internal/interfaces/web/api/skills.go` - `/api/skills` and `/api/tools` endpoints

### Tool Name Normalization
OpenAI-compatible function names can't contain `/`:
```go
func ToolName(skillName string) string {
    n := strings.ReplaceAll(skillName, "/", "_")
    n = strings.ReplaceAll(n, "-", "_")
    return n // "code/symbols" → "code_symbols"
}
```

### JSON Schema Generation
Convert `skill.Manifest.Signature.Parameters` to standard JSON Schema:
```json
{
  "type": "object",
  "properties": { "path": { "type": "string", "description": "..." } },
  "required": ["path"],
  "additionalProperties": false
}
```

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/skills` | List all skill manifests |
| GET | `/api/tools` | List tool defs with JSON schemas |

### Acceptance Criteria
- [ ] `/api/skills` returns manifests with name/version/description/tags
- [ ] `/api/tools` returns normalized tool names + JSON Schema
- [ ] No tool name collisions after normalization

---

## Phase 5: Skill Tool Executor
**Estimated effort: 2-3 days**

### Deliverables
- `internal/consoleapp/skill_tool_executor.go` - Execute skills as tools
- `internal/interfaces/web/tools/registry.go` - Tool registry wrapper
- `internal/interfaces/web/tools/executor.go` - Tool execution implementation
- `internal/interfaces/web/tools/profiles.go` - Agent profile allowlists

### Execution Paths
1. **Daemon path (preferred):** `daemon.Client.Run(skill, input, workspace, ephemeral=true)`
2. **Fallback path:** Direct execution via `execution.NewRunnerExecutor()`

### Agent Profiles
| Profile | Allowed Tools | Description |
|---------|---------------|-------------|
| explorer | code/symbols, code/complexity, code/semantic_search | Read-only exploration |
| reviewer | code/*, git/status | Code review + analysis |
| implementer | code/*, test/*, lsp/*, git/* | Full implementation |

### Acceptance Criteria
- [ ] `SkillToolExecutor.Execute("code_symbols", args)` returns envelope JSON
- [ ] Daemon path used when daemon running
- [ ] Fallback execution works when daemon not running
- [ ] Parameter defaults merged, required/enum validated

---

## Phase 6: Console WebSocket + Ask Loop
**Estimated effort: 3-4 days**

### Deliverables
- `internal/interfaces/web/consolews/hub.go` - WebSocket connection hub
- `internal/interfaces/web/consolews/session.go` - Console session state + ask loop
- `internal/consoleapp/runner.go` - LLM engine + tool runner integration
- `internal/consoleapp/stream.go` - Console payload streaming adapters

### WebSocket Protocol
```typescript
// Client → Server
type AskPayload = {
  type: "ask";
  actor_id: string;
  console_id: string;
  correlation_id: string;
  content: string;
};

type CmdPayload = {
  type: "cmd";
  actor_id: string;
  console_id: string;
  correlation_id: string;
  cmd: { name: "cancel"; correlation_id: string };
};

// Server → Client
type EventPayload = {
  type: "event";
  actor_id: string;
  console_id: string;
  correlation_id: string;
  content: string;
  metadata: { partial: boolean; tool?: string; cas_digest?: string };
};

type ReplyPayload = {
  type: "reply";
  actor_id: string;
  console_id: string;
  correlation_id: string;
  content: string;
};
```

### Execution Loop
1. Receive `ask` payload with user message
2. Build `EngineInput` from conversation history + user message
3. Run `LLMChatEngine.Run()` with tool defs
4. Emit `event` payloads for each tool call/result
5. Emit final `reply` payload with assistant response
6. Handle `cancel` command by canceling context

### Acceptance Criteria
- [ ] WebSocket connects at `/ws/console/:consoleID`
- [ ] User messages processed, LLM responds
- [ ] Tool calls emitted as events
- [ ] Tool results emitted with CAS digests
- [ ] Final reply delivered
- [ ] Cancel command stops in-flight request
- [ ] Conversation history maintained per session

---

## Phase 7: Console REST API + SSE Alternative
**Estimated effort: 2-3 days**

### Deliverables
- `internal/interfaces/web/handlers/console.go` - REST endpoints for console
- Console SSE endpoint as WebSocket alternative

### API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/console/sessions?workspace=` | List console sessions |
| POST | `/api/console/sessions` | Create console session |
| POST | `/api/console/sessions/:id/ask` | Send user message |
| POST | `/api/console/sessions/:id/cancel` | Cancel in-flight request |
| GET | `/api/console/sessions/:id/events` | SSE stream |

### SSE Format Options
Support `?format=payload` query param:
- **Default:** `{ type, data, ts }` wrapper
- **Payload:** Raw `ConsolePayload` JSON (cleaner for UI)

### Acceptance Criteria
- [ ] Sessions persist in `agents.db/console_sessions`
- [ ] SSE stream emits console payloads
- [ ] SSE has heartbeat (every 30s)
- [ ] Ask/cancel work via REST

---

## Phase 8: Streaming LLM Output
**Estimated effort: 2-3 days**

### Deliverables
- Extend `internal/engine/llmchat_engine.go` with streaming support
- `StreamConfig` with `OnDelta` callback

### Streaming Implementation
```go
type StreamConfig struct {
    Stream   bool
    OnDelta  func(StreamDelta)
}

type StreamDelta struct {
    ContentDelta   string
    ToolCallDelta  *ToolCallDelta
    FinishReason   string
}
```

### Parse SSE Stream
```go
// Parse "data: {...}" lines
// Stop on "data: [DONE]"
// Accumulate assistant content deltas
// Accumulate tool call deltas (function name + arguments)
// Emit deltas to console as events (partial=true)
```

### Acceptance Criteria
- [ ] Assistant text appears progressively (not only at end)
- [ ] Tool call arguments stream incrementally
- [ ] Works with OpenRouter/OpenAI/Groq providers

---

## Phase 9: Sessions Store Integration
**Estimated effort: 1-2 days**

### Deliverables
- Console sessions create corresponding `sessions.Session`
- Turns saved to `sessions.SessionTurn`
- Console sessions appear in SessionsPage

### Integration Points
```go
// On console session create
sessions.Store.Create(Session{
    Status: Running,
    AgentID: "agentctl",
    WorkspacePath: workspace,
})

// On each turn
sessions.Store.AddTurn(SessionTurn{
    Role: "user"/"assistant",
    Content: message,
    ToolCalls: toolCalls,
})

// On stop/cancel
sessions.Store.SetStatus(id, ok/error/canceled)
```

### Acceptance Criteria
- [ ] Console sessions visible in Sessions page
- [ ] Turn transcript preserved
- [ ] Tool calls recorded in trajectory

---

## Phase 10: GUI Console Page
**Estimated effort: 3-4 days**

### Deliverables (React)
- `packages/gui/src/pages/ConsolePage.tsx`
- `packages/gui/src/api/console.ts`
- `packages/gui/src/hooks/useConsoleSocket.ts`
- Route + sidebar nav item

### Deliverables (Svelte - Optional)
- `packages/gui-svelte/src/pages/ConsolePage.svelte`
- `packages/gui-svelte/src/lib/api/consoleApi.ts`
- `packages/gui-svelte/src/lib/api/consoleTypes.ts`

### UI Layout
```
┌─────────────────────────────────────────────────────────────────┐
│ agentctl                                    [Dark] [Profile ▼] │
├─────────────┬───────────────────────────────────────────────────┤
│             │                                                   │
│ Sessions    │  Transcript Timeline                              │
│ ──────────  │  ─────────────────────────                        │
│ [session-1] │  ┌───────────────────────────────────────┐        │
│  session-2  │  │ user • abc123                         │        │
│  session-3  │  │ How do I fix the auth bug?            │        │
│             │  └───────────────────────────────────────┘        │
│             │  ┌───────────────────────────────────────┐        │
│             │  │ tool • code_symbols • call           │        │
│             │  │ {"path": "src/auth/"}                 │        │
│             │  └───────────────────────────────────────┘        │
│             │  ┌───────────────────────────────────────┐        │
│             │  │ assistant • abc123 (streaming...)     │        │
│             │  │ I found the issue in login.ts:45...   │        │
│             │  └───────────────────────────────────────┘        │
│             ├───────────────────────────────────────────────────┤
│             │ [Type a message...                    ] [Send]    │
│             │                              [in-flight: xyz...] │
└─────────────┴───────────────────────────────────────────────────┘
```

### Acceptance Criteria
- [ ] Console page accessible at `/console`
- [ ] Can create/select console sessions
- [ ] User messages sent via WebSocket/REST
- [ ] Assistant tokens stream in real-time
- [ ] Tool calls displayed with args/results
- [ ] CAS digests linkable
- [ ] Cancel button stops in-flight request

---

## Phase 11: Replace Bun Server
**Estimated effort: 1-2 days**

### Deliverables
- Remove `packages/gui/server/*` dependency
- Update `package.json` scripts to use Go server
- Update vite proxy config

### Script Updates
```json
{
  "scripts": {
    "dev:server": "go run ./cmd/agentctl_web",
    "dev:gui": "vite",
    "dev:all": "concurrently \"bun run dev:server\" \"bun run dev:gui\""
  }
}
```

### Vite Config Updates
```typescript
export default defineConfig({
  server: {
    proxy: {
      "/api": { target: "http://localhost:8090", changeOrigin: true },
      "/ws": { target: "ws://localhost:8090", ws: true },
    },
  },
});
```

### Acceptance Criteria
- [ ] `bun run dev:all` starts Go backend + Vite
- [ ] All existing pages work with Go backend
- [ ] No more Bun/Express server code needed

---

## Phase 12: Svelte SPA (Optional)
**Estimated effort: 4-5 days**

### Deliverables
- `packages/gui-svelte/` - Complete Svelte SPA
- Reuses `@agentctl/data` for types + API calls
- TanStack Query (Svelte) for caching + invalidation
- SSE-based query invalidation

### Package Structure
```
packages/gui-svelte/
├── index.html
├── package.json
├── vite.config.ts
├── tailwind.config.cjs
├── src/
│   ├── main.ts
│   ├── app.css
│   ├── Root.svelte
│   ├── App.svelte
│   ├── routes.ts
│   ├── lib/
│   │   ├── api/
│   │   │   ├── queryClient.ts
│   │   │   ├── sse.ts
│   │   │   ├── client.ts
│   │   │   ├── consoleApi.ts
│   │   │   └── consoleTypes.ts
│   │   ├── components/
│   │   │   └── layout/
│   │   └── utils/
│   └── pages/
│       ├── JobsPage.svelte
│       ├── TasksPage.svelte
│       ├── ConsolePage.svelte
│       └── ...
```

### Port Order
1. Jobs + Tasks (baseline)
2. Sessions + Agents
3. Mailbox + Reservations
4. Search + SQLite + Insights
5. Console (with streaming)

### Acceptance Criteria
- [ ] Feature parity with React GUI
- [ ] SSE invalidation triggers query refresh
- [ ] Console page fully functional

---

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Streaming complexity | High | Start with non-streaming MVP, add streaming later |
| State sync between REST/WS | Medium | Use correlation IDs, single source of truth |
| Tool name collisions | Low | Registry validates uniqueness at startup |
| Provider compatibility | Medium | Test with OpenRouter, OpenAI, Groq |
| Migration disruption | Medium | Keep React GUI working during Svelte port |

---

## Dependencies

### Go Packages
```go
require (
    nhooyr.io/websocket v1.8.x      // WebSocket
    github.com/rs/zerolog           // Logging (existing)
    github.com/oklog/ulid/v2        // Correlation IDs (existing)
)
```

### TypeScript Packages (Svelte)
```json
{
  "dependencies": {
    "@tanstack/svelte-query": "^5.0.0",
    "svelte": "^4.2.0",
    "svelte-spa-router": "^4.0.0",
    "lucide-svelte": "^0.500.0"
  }
}
```

---

## Success Metrics

1. **API Response Time:** <100ms for list endpoints, <500ms for skill execution
2. **Streaming Latency:** First token within 200ms of API response
3. **WebSocket Reliability:** Automatic reconnection, no message loss
4. **Feature Parity:** All existing GUI features work on Go backend
5. **Console UX:** Comparable to Claude Code experience

---

## Implementation Order Summary

| Phase | Deliverable | Effort | Dependencies |
|-------|-------------|--------|--------------|
| 1 | Go server skeleton + SSE | 1-2 days | None |
| 2 | Jobs + CAS API | 2-3 days | Phase 1 |
| 3 | Board/Mailbox + Agents | 2-3 days | Phase 1 |
| 4 | Skills registry + schemas | 2-3 days | Phase 1 |
| 5 | Tool executor | 2-3 days | Phase 4 |
| 6 | Console WebSocket | 3-4 days | Phase 5 |
| 7 | Console REST + SSE | 2-3 days | Phase 6 |
| 8 | Streaming LLM | 2-3 days | Phase 6 |
| 9 | Sessions integration | 1-2 days | Phase 6 |
| 10 | GUI Console page | 3-4 days | Phase 7 |
| 11 | Replace Bun server | 1-2 days | Phase 2, 3 |
| 12 | Svelte SPA (optional) | 4-5 days | Phase 11 |

**Total Estimated Effort:** 24-35 days

---

## Quick Start Commands

```bash
# Phase 1: Create skeleton
mkdir -p cmd/agentctl_web internal/interfaces/web/{api,sse,consolews,tools}

# Run Go server
go run ./cmd/agentctl_web --addr=:8090 --dev-cors

# Run with existing GUI
cd packages/gui && bun run dev

# Test health
curl localhost:8090/api/health

# Test SSE
curl -N localhost:8090/api/events

# WebSocket test (wscat)
wscat -c "ws://localhost:8090/ws/console/test-session?profile=implementer"
```
