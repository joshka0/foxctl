---
title: Chat Platforms
description: Connect Discord, Telegram, and Teams to foxctl sessions, skills, and agent workflows via the chat adapter layer.
---

Chat platform adapters connect external messaging events (Discord, Telegram,
Microsoft Teams) to foxctl skills, agent endpoints, and console sessions. They
run as part of the web server process and normalize platform-specific messaging
into a shared command model.

This page covers the adapter architecture, platform-specific behavior, session
model, and operational characteristics.

## Adapter Architecture

### Starting a Chat Adapter

Enable one inbound chat adapter when starting the web server:

```bash
foxctl web serve --chat discord
foxctl web serve --chat telegram
foxctl web serve --chat teams
```

Only one adapter runs per `foxctl web serve` process. The startup path lives in
`internal/interfaces/web/server.go`.

### Startup Sequence

1. The server builds a shared `chatadapter.Bridge`
2. The selected adapter is created and connected
3. Slash/text command handlers are wired into the bridge
4. Natural-language message handlers are wired into a `SessionBridge` backed by
   the shared console session manager and websocket transport

If credentials are missing or platform validation fails, that adapter does not
start. The rest of the web server continues running normally.

### Runtime Flow

```
Platform (Discord / Telegram / Teams)
  → chatadapter driver
    → chatadapter.Bridge (slash commands)
      → SkillRunner (skill execution)
      → /api/agents/* (agent HTTP endpoints)
    → chatadapter.SessionBridge (natural language)
      → Console SessionManager
        → Console WebSocket transport
          → Companion + context builder
```

Slash commands route through the `Bridge` to either skill execution or agent
HTTP endpoints. Natural-language messages route through the `SessionBridge` to
the console session manager, which connects to the companion and context builder.

## Command Model

The shared bridge routes MVP command intents to either skill execution through
`api.SkillRunner` or agent HTTP endpoints under `/api/agents/*`.

### Shared Commands

| Command | Routes to |
|---------|-----------|
| `/search` | Skill execution |
| `/todo` | Skill execution |
| `/memory` | Skill execution |
| `/logs` | Skill execution |
| `/agent-spawn` | `/api/agents` |
| `/agent-list` | `/api/agents` |

### Platform Naming Differences

| Platform | Slash command style | Example |
|----------|-------------------|---------|
| Discord | Hyphenated | `/agent-spawn` |
| Telegram | Underscore | `/agent_spawn` (normalized to `agent-spawn`) |
| Teams | Text parsing | `agent-spawn` parsed from message content |

Telegram normalizes its platform-specific command names back into the shared
bridge command names internally.

## Platform Behavior

### Discord

- Uses explicit command registration from `discord.MVPCommands()`
- Supports commands, interactions, and natural-language message mode
- Binds a Discord-specific session bridge over the shared console session
  manager

### Telegram

- Uses explicit command registration from `telegram.MVPCommands()`
- Supports commands, callback interactions, and natural-language messaging
- Uses a Telegram-specific session bridge over the shared console session
  manager
- **Note:** Telegram support has known dependency-maintenance risk in current
  docs; treat it as operational but not risk-free

### Microsoft Teams

- Uses Bot Framework webhook ingestion at `POST /api/teams/messages`
- Wires command, interaction, and message handlers in the web server
- Uses conversation-reference persistence plus a shared generic session bridge
- Does not rely on slash-command registration in the same way as Discord and
  Telegram

## Session and Concurrency Model

### Natural-Language Chat

Natural-language chat is routed through managed console sessions, not directly
through the slash-command bridge. The `chatadapter.SessionBridge` maps a
platform conversation to a console session and streams edits and messages back
to the platform.

### Concurrency

- Turn-level concurrency is serialized through `companion.Locker`
- For PostgreSQL-backed deployments, the lock backend can use PostgreSQL to
  avoid duplicate active turns across pods
- Without PostgreSQL, the lock falls back to in-memory locking (single-process
  only)

## API Dependencies

The chat layer depends on current web-server routes rather than a separate
daemon API surface:

| Endpoint | Purpose |
|----------|---------|
| `/api/agents` | List agents |
| `/api/agents/{id}` | Agent details |
| `/api/agents/{id}/ask` | Ask agent a question |
| `/api/agents/{id}/daemon/{start\|kill\|sessions}` | Agent lifecycle |
| `/api/events` | Event streaming |
| `/api/teams/messages` | Teams webhook ingestion |
| `/ws/console/{id}` | WebSocket console transport |

## Extension Boundary

Chat adapters are designed to stay conservative at the platform ingress layer.
If chat adapters are later used for domain-specific workflows (SRE
investigations, email handling, document review), the boundary should remain:

1. **Platform-native webhook parsing and auth** stay in the Go adapter layer
2. **The adapter normalizes** inbound messages or events into canonical Go-side
   commands or domain requests
3. **Only after normalization** may the runtime forward canonical signals into
  orchestration or workflow engines

For example, a Teams channel for investigations can continue to use
`SessionBridge` for generic natural-language chat, while a bound investigation
controller routes into a dedicated service — all without teaching the chat
adapter about domain-specific logic.

## Jido and v2 Notes

Chat adapters do not talk to Jido directly. They indirectly benefit from
v2/Jido-backed behavior through:

- `/api/agents/*` handlers
- Companion context building
- Orchestration and event surfaces exposed elsewhere in the web server

## Operational Characteristics

- Adapter startup is platform-specific; only one adapter is selected per
  `foxctl web serve` process
- SSE hooks are available to adapters that render live agent state updates
- Teams JWT verification can be skipped only in dev mode and requires
  `--dev-cors`
- Keep platform tokens and webhook secrets out of docs and logs
- Session bridge behavior should link back to sessions and agent lifecycle

## Cross-References

- [Agents Lifecycle](/agents/lifecycle) — agent commands and execution modes
- [Skills Runtime](/skills/runtime-and-install) — skill execution paths
- [API Server](/architecture/api-server) — route groups and auth
- [Rooms](/collaboration/rooms) — durable coordination surfaces
