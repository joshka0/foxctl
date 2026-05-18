# Foxctl Plugin for Hermes Agent

Deep integration between foxctl's intelligence layer and hermes-agent, providing 26 tools spanning repo index search, code analysis, room-agile lifecycle, memory, and filesystem access.

## Tool Categories

### Intelligence Layer (deep code understanding)

| Tool | Description |
|---|---|
| `foxctl_repo_search` | Search the repo index for symbols, files, packages by natural-language query |
| `foxctl_repo_dag` | Get an explanation dependency DAG for a code concept |
| `foxctl_repo_expand` | Expand the repo index graph from seed nodes to discover neighbors |
| `foxctl_repo_open` | Open a repo index node by ID for full metadata |
| `foxctl_code_grep` | Search code patterns and return surrounding function/class blocks |
| `foxctl_semantic_search` | Unified semantic search across symbols, sessions, memory, codemaps |
| `foxctl_code_symbols` | Extract symbols (functions, types, interfaces) from a file |
| `foxctl_text_grep` | Fast regex search across the workspace |
| `foxctl_fs_read` | Read file contents through CAS-backed storage |
| `foxctl_fs_find` | Find files by name, path, or glob pattern |
| `foxctl_codemap_list` | List available codemaps |
| `foxctl_codemap_get` | Get a codemap by ID with full content |

### Memory & Session Layer (cross-agent knowledge)

| Tool | Description |
|---|---|
| `foxctl_memory_search` | Search foxctl's vector-indexed knowledge base |
| `foxctl_session_recall` | Recall context from past sessions |
| `foxctl_context` | Gather workspace overview (rooms, tasks, health) |

### Room Communication

| Tool | Description |
|---|---|
| `foxctl_room_send` | Send a message to the room |
| `foxctl_room_inbox` | Check your room inbox |
| `foxctl_room_messages` | Read recent room messages |
| `foxctl_room_message_ack` | Acknowledge a room message |

### Room-Agile Lifecycle

| Tool | Description |
|---|---|
| `foxctl_epic_show` | Show epic details |
| `foxctl_epic_resume` | Get epic state summary |
| `foxctl_epic_health` | Check epic health |
| `foxctl_epic_next` | Get recommended next action |
| `foxctl_milestone_show` | Show milestone details |
| `foxctl_story_show` | Show story details |
| `foxctl_story_start` | Move story to in_progress |
| `foxctl_story_review` | Move story to in_review |
| `foxctl_story_validate` | Validate a story (pass/fail/waived) |

### System

| Tool | Description |
|---|---|
| `foxctl_health` | Check foxctl daemon status |

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

## Usage Patterns

### Code exploration
```
You: how does the room agile story lifecycle work?
Hermes: [calls foxctl_repo_search "room agile story lifecycle"]
        [calls foxctl_code_grep "storyState" mode=ripgrep]
        Found: StoryState in internal/domain/agent/board.go, transitions in cmd/room.go...
```

### Cross-agent memory
```
You: search foxctl memory for the auth module design
Hermes: [calls foxctl_memory_search query="auth module design"]
        Found 3 records: auth-architecture (knowledge), ...
```

### Room-agile workflow
```
You: check the room inbox
Hermes: [calls foxctl_room_inbox]
        [calls foxctl_epic_next]
        Next action: start story "Add read-only agile endpoint"
        Want me to start it?

You: yes
Hermes: [calls foxctl_story_start story_id=01KRS5AXG5...]
        Story started. I'll pick up the implementation now.
```

### Deep code understanding
```
You: what does the TursoStore vector search implementation look like?
Hermes: [calls foxctl_repo_search "vector search turso"]
        [calls foxctl_code_grep "SearchChunks" path="internal/storage/sessions"]
        Found TursoStore.SearchChunks in turso_store.go:1183-1245...
        [calls foxctl_fs_read "internal/storage/sessions/turso_store.go"]
```

## Architecture

```
hermes agent
  └── plugin: foxctl
       ├── tools.py      → 26 registered tools (4 categories)
       ├── client.py     → HTTP client with skill envelope unwrapping
       ├── config.py     → reads from config.yaml + env
       └── __init__.py   → plugin entry + lifecycle hooks

foxctl daemon (localhost:8090)
  └── Skill API (/api/skills/*)
       ├── repo/index_search, repo/index_dag_grep, repo/index_expand
       ├── code/context_grep, code/semantic_search, code/symbols
       ├── text/grep, fs/read, fs/find
       ├── codemap/list, codemap/get
       ├── memory/query, session/recall
       └── REST API
            ├── /api/rooms/{id}/messages, inbox, agile
            └── /api/health, /api/context/overview

herdr (terminal multiplexer)
  └── room loop relay → pane delivery
```

## Deep Intelligence Integration

The plugin exposes foxctl's full intelligence layer to hermes through 62 foxctl skills mapped to 12 dedicated intelligence tools:

- **RepoIndex** — 33,714 nodes with 150,818 edges covering symbols, files, packages. Semantic search with Qwen3-Embedding-8B vectors. Graph traversal for dependency analysis.
- **Code Search** — Multi-mode (ripgrep/AST/line) with function/class block expansion. Smart search combines repo index + ripgrep + semantic ranking.
- **Memory** — Named records with vector embeddings, BM25 fallback. Cross-agent knowledge base.
- **Codemaps** — Semantic code relationship maps generated by AI agents.
- **Session Recall** — Past conversation search with embedding similarity.

All skill responses are unwrapped from the foxctl skill envelope (`output.data`) automatically by the client's `_unwrap_skill()` method, so hermes receives clean data without needing to understand the CAS/artifact system.

## Zero Dependencies

The plugin uses only Python stdlib (`urllib`, `json`, `os`, `logging`) for maximum portability — no requests, no httpx, no external packages needed.
