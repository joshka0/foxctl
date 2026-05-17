# Foxctl Plugin for Hermes Agent

Bridges the foxctl daemon with hermes-agent, providing room-agile tools, memory search, and context injection.

## What it gives hermes

| Capability | Tool | Description |
|---|---|---|
| Memory search | `foxctl_memory_search` | Search foxctl's vector-indexed knowledge base |
| Session recall | `foxctl_session_recall` | Recall context from past sessions |
| Context | `foxctl_context` | Gather workspace overview (rooms, tasks, health) |
| Room messaging | `foxctl_room_send` | Send a message to the room |
| Room inbox | `foxctl_room_inbox` | Check your room inbox |
| Room messages | `foxctl_room_messages` | Read recent room messages |
| Message ack | `foxctl_room_message_ack` | Acknowledge a room message |
| Epic state | `foxctl_epic_show/resume/health/next` | Read epic state |
| Milestones | `foxctl_milestone_show` | Read milestone details |
| Stories | `foxctl_story_show` | Read story details |
| Story lifecycle | `foxctl_story_start/review/validate` | Mutate story state |
| Health | `foxctl_health` | Check foxctl daemon status |

## Install

```bash
# Symlink the plugin directory into hermes plugins
ln -sf /path/to/foxctl/integrations/hermes ~/.hermes/plugins/foxctl
```

## Configure

In `~/.hermes/config.yaml`:

```yaml
plugins:
  enabled:
    - foxctl

foxctl:
  url: "http://localhost:8090"        # foxctl daemon URL
  workspace: "."                      # workspace path
  room: "alpha"                       # room ID to bind to
  actor: "actor:hermes:local"         # actor identity
  auto_bind: false                    # auto-register as room participant on session start
  memory_context: true                # include foxctl memory in context
  epic_context: true                  # include epic state in context
```

Environment variable overrides: `FOXCTL_URL`, `FOXCTL_WORKSPACE`, `FOXCTL_ROOM`, `FOXCTL_EPIC_ID`, `FOXCTL_ACTOR`, `FOXCTL_SESSION`, `FOXCTL_AUTO_BIND`.

## Usage in hermes

Once installed and enabled, hermes can call foxctl tools naturally:

```
You: search foxctl memory for the auth module design
Hermes: [calls foxctl_memory_search with query="auth module design"]

You: check the room inbox
Hermes: [calls foxctl_room_inbox]

You: start story 01KRS3YCDDV7CDX8MS314X4MQW
Hermes: [calls foxctl_story_start]

You: tell the room I'm done with the auth module
Hermes: [calls foxctl_room_send]
```

## Architecture

```
hermes agent
  └── plugin: foxctl
       ├── tools.py      → 19 registered tools
       ├── client.py     → HTTP client for foxctl API
       ├── config.py     → reads from config.yaml + env
       └── __init__.py   → plugin entry + lifecycle hooks

foxctl daemon (localhost:8090)
  └── REST API
       ├── /api/health
       ├── /api/memory/query
       ├── /api/session/recall
       ├── /api/context/overview
       ├── /api/rooms/{id}/messages
       ├── /api/rooms/{id}/inbox
       └── /api/rooms/{id}/agile (epic/milestone/story CRUD)

herdr (terminal multiplexer)
  └── room loop relay → pane delivery
```

## Memory Layer

The key integration point: foxctl's memory store acts as a shared knowledge base accessible to all agents in the room. Hermes can:

1. **Search** — `foxctl_memory_search` queries the vector store for relevant context
2. **Recall** — `foxctl_session_recall` retrieves past session insights
3. **Context** — `foxctl_context` gets the full workspace overview

This provides a cross-agent memory layer where Pi, Hermes, and other room participants share context through foxctl's indexed knowledge base.
