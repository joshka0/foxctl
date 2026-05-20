---
title: Pi and Hermes integrations
description: How Pi and Hermes integrate with foxctl for intelligence, memory, coordination, and multi-agent workflows.
---

Status: Current for Hermes plugin (71 tools) and Pi extension (100+ tools).

Foxctl ships two deep agent integrations — a TypeScript extension for [Pi](https://github.com/badlogic/pi-mono) and a Python plugin for [Hermes](https://github.com/joshka/hermes-agent). Both connect to the foxctl daemon's REST API and share the same intelligence, memory, and coordination layers.

## Pi extension

**Source**: `integrations/pi/foxctl.ts` (~1,800 lines)

Pi connects as a first-class foxctl participant with:

- **Room-agile lifecycle** — epic/milestone/story management with TUI widgets showing live epic state
- **Context injection** — memory matches, session recall, and epic context injected before each agent turn
- **Slash commands** — `/epic`, `/epic-next`, `/story-start`, `/context-show`, and more

### Installation

```bash
# Copy to Pi's extensions directory
mkdir -p ~/.pi/extensions
cp integrations/pi/foxctl.ts ~/.pi/extensions/
```

### Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--foxctl-url` | `http://localhost:8090` | Foxctl daemon URL |
| `--foxctl-workspace` | `.` | Workspace root |
| `--foxctl-context` | `false` | Inject foxctl context before each turn |
| `--foxctl-memory-context` | `true` | Include memory/session recall in context |
| `--foxctl-epic` | `""` | Active room-agile epic ID |
| `--foxctl-epic-context` | `true` | Inject epic state when epic is set |

### Key tools

| Category | Tools |
|----------|-------|
| Intelligence | `foxctl_code_search`, `foxctl_repoindex_search`, `foxctl_repoindex_dag`, `foxctl_code_context_grep` |
| Memory | `foxctl_memory_query`, `foxctl_memory_put`, `foxctl_session_recall` |
| ContextWiki | `foxctl_context_show`, `foxctl_context_capture`, `foxctl_context_observe`, `foxctl_context_tension` |
| Vault | `foxctl_vault_search`, `foxctl_vault_promote`, `foxctl_vault_graph` |
| Rooms | `foxctl_room_send`, `foxctl_room_messages`, `foxctl_room_agile` |
| Stories | `foxctl_story_start`, `foxctl_story_review`, `foxctl_story_validate` |

## Hermes plugin

**Source**: `integrations/hermes/` (~3,750 lines total)

Hermes connects as a foxctl plugin with 71 tools across 11 categories. Zero external dependencies — uses only Python stdlib (`urllib`, `json`, `os`, `subprocess`).

### Installation

```bash
# The plugin is installed as a symlink
mkdir -p ~/.hermes/plugins
ln -s /path/to/foxctl/integrations/hermes ~/.hermes/plugins/foxctl
```

### Configuration

```yaml
# ~/.hermes/config.yaml
foxctl:
  url: http://localhost:8091
  workspace: /path/to/project
  room: alpha
  actor: actor:hermes:local
  vault_path: /path/to/obsidian/vault
```

### Tool categories

| Category | Count | Key tools |
|----------|-------|-----------|
| Intelligence | 13 | `foxctl_repo_search`, `foxctl_code_search`, `foxctl_repoindex_dag`, `foxctl_codemap_detail` |
| ContextWiki | 12 | `foxctl_context_show`, `foxctl_context_observe`, `foxctl_context_tension`, `foxctl_context_capture` |
| Vault / Knowledge | 7 | `foxctl_vault_search`, `foxctl_vault_promote`, `foxctl_vault_graph`, `foxctl_vault_bridge` |
| Memory | 4 | `foxctl_memory_put`, `foxctl_memory_search`, `foxctl_memory_curator` |
| Rooms | 4 | `foxctl_room_send`, `foxctl_room_inbox`, `foxctl_room_messages` |
| Mailbox (direct) | 3 | `foxctl_mailbox_send`, `foxctl_mailbox_inbox`, `foxctl_mailbox_ack` |
| Flow orchestration | 13 | `foxctl_flow_create`, `foxctl_flow_start`, `foxctl_flow_build_pipeline`, `foxctl_flow_build_fan_out` |
| Multi-agent | 9 | `foxctl_agent_list`, `foxctl_room_task_add`, `foxctl_room_task_claim`, `foxctl_room_task_complete` |
| Pipe protocol | 2 | `foxctl_pipe_emit`, `foxctl_pipe_receive` |
| Embedding | 1 | `foxctl_embedding_flush` |

## How they coordinate

Pi and Hermes coordinate through three foxctl primitives:

### 1. ContextWiki (shared knowledge)

Both agents read and write observations, tensions, and handoffs:

```
Hermes: foxctl_context_observe(
  content="retry logic in store.go doesn't respect context cancellation"
)

Pi: foxctl_context_show()
  → sees Hermes's observation, takes it into account
```

### 2. Direct mailbox (1:1 messaging)

Agents send direct alerts and coordination messages:

```
Pi: foxctl_mailbox_send(
  recipient="actor:hermes:local",
  subject="File conflict on utils.go",
  body="I'm refactoring CreateUser(). The function is also called from cron/sync.go."
)

Hermes: foxctl_mailbox_inbox(only_unread=true)
  → sees Pi's message, adjusts its work
```

### 3. Write conflict detection (automatic)

The file guard hook detects conflicts and notifies both agents:

```
Pi writes shared/utils.go → auto-reserved
Hermes tries to write shared/utils.go → blocked
  → Both get mailbox alerts with reservation details
  → They coordinate via mailbox messages
```

## Running both agents

```bash
# Terminal 1: foxctl daemon
foxctl web serve --port 8091

# Terminal 2: room loop (herdr relay)
foxctl room loop alpha --workspace /path/to/project --backend herdr

# Terminal 3: Pi
pi --foxctl-url http://localhost:8091 \
   --foxctl-workspace /path/to/project \
   --foxctl-context \
   --foxctl-room alpha

# Terminal 4: Hermes
hermes chat -t foxctl
```

## Canonical sources

- [integrations/hermes/README.md](https://github.com/joshka0/foxctl/blob/main/integrations/hermes/README.md)
- [integrations/pi/README.md](https://github.com/joshka0/foxctl/blob/main/integrations/pi/README.md)
- [Multi-agent coordination](/collaboration/multi-agent-coordination)
