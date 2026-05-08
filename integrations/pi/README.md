# foxctl-pi-extension

Bridge your [foxctl](https://github.com/joshka/foxctl) Go daemon with
[pi](https://github.com/badlogic/pi-mono)'s extension system.

## What it does

This extension exposes foxctl's HTTP API as pi tools and slash commands,
so you can query skills, agents, rooms, tasks, jobs, memory, and more
directly from the pi TUI.

## Source of Truth

The tracked foxctl source lives at `integrations/pi/foxctl.ts`.

For local dogfooding with the sibling Pi checkout, point Pi's project extension
slot at this tracked file:

```bash
ln -sfn /Users/joshka/repos/personal/foxctl/integrations/pi/foxctl.ts \
  /Users/joshka/repos/githubs/pi-mono/.pi/extensions/foxctl.ts
```

This keeps Pi reloads pointed at the same file that foxctl agents review,
index, and commit.

## Installation

### Option 1: Auto-discovery

Copy the tracked extension to Pi's extensions directory:

```bash
mkdir -p ~/.pi/extensions
cp integrations/pi/foxctl.ts ~/.pi/extensions/
```

### Option 2: Per-project

Copy into your project's `.pi/extensions/`:

```bash
mkdir -p .pi/extensions
cp /path/to/foxctl/integrations/pi/foxctl.ts .pi/extensions/foxctl.ts
```

### Option 3: CLI flag

Load it explicitly:

```bash
pi --extension /path/to/foxctl.ts \
  --foxctl-url http://localhost:8090 \
  --foxctl-gateway-url http://localhost:8765
```

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `--foxctl-url` | `http://localhost:8090` | URL of the foxctl daemon |
| `--foxctl-gateway-url` | `http://localhost:8765` | URL of the foxctl terminal gateway for compatibility room terminals |
| `--foxctl-workspace` | `.` | Workspace root used by filesystem, code, repoindex, memory, room, and task tools |
| `--foxctl-context` | `false` | Inject foxctl workspace/task/room context before each Pi agent turn |
| `--foxctl-memory-context` | `true` | Include prompt-keyed `memory/query` and `session/recall` evidence in `--foxctl-context` hook injection |

## Available Tools

### Status & Health
- `foxctl_status` — Daemon status and version
- `foxctl_health` — Health/readiness checks

### Skills
- `foxctl_skills_list` — List all skills
- `foxctl_skill_detail` — Get skill schema/info
- `foxctl_skill_run` — Run a skill with JSON input
- `foxctl_tool_run` — Run an OpenAPI-enabled foxctl skill through its direct skill endpoint

### Filesystem & Text
- `foxctl_fs_list` — List workspace files or directories through `fs/ls`
- `foxctl_fs_read` — Read a workspace file through `fs/read`
- `foxctl_filesystem_read` — Alias for `foxctl_fs_read`
- `foxctl_fs_find` — Find workspace files through `fs/find`
- `foxctl_text_grep` — Search workspace text through `text/grep`

### Code & Repoindex
- `foxctl_code_search` — Smart code search with repoindex-aware candidates
- `foxctl_code_semantic_search` — Semantic search across code and context scopes
- `foxctl_code_context_grep` — Search code and return surrounding functions/classes
- `foxctl_repoindex_search` — Search repoindex nodes and semantic anchors
- `foxctl_repoindex_dag` — Build a compact repoindex explanation graph
- `foxctl_repoindex_expand` — Expand repoindex graph edges from seed node IDs
- `foxctl_repoindex_open` — Open a repoindex node by ID

### Refactor, Memory & Sessions
- `foxctl_refactor_scout` — Read-only deterministic refactor scout
- `foxctl_refactor_plan` — Entry-point-oriented alias for refactor planning
- `foxctl_memory_query` — Query canonical foxctl memory records
- `foxctl_memory_search` — Alias for `foxctl_memory_query`
- `foxctl_session_recall` — Recall relevant prior sessions

### Agents
- `foxctl_agents_list` — List agents
- `foxctl_agent_detail` — Agent info
- `foxctl_agent_spawn` — Spawn an agent
- `foxctl_agent_ask` — Send a message to an agent

### Rooms
- `foxctl_rooms_list` — List rooms
- `foxctl_room_detail` — Room info
- `foxctl_room_messages` — Get room messages
- `foxctl_room_send` — Send a message to a room
- `foxctl_room_terminal_links` — Get local/tailnet room terminal dogfood links
- `foxctl_room_terminal_register` — Register a room with the terminal gateway and return dogfood links

### Tasks
- `foxctl_tasks_list` — List tasks (filter by status)
- `foxctl_task_detail` — Task info
- `foxctl_task_complete` — Mark task completed

### Search & Memory
- `foxctl_search` — Semantic memory search
- `foxctl_blackboard_post` — Post to blackboard
- `foxctl_blackboard_list` — List blackboard entries

### Codemaps
- `foxctl_codemaps_list` — List codemaps
- `foxctl_codemap_detail` — Codemap info

### Orchestration
- `foxctl_board` — Get orchestration board
- `foxctl_board_dispatch` — Dispatch a board card

### Jobs
- `foxctl_jobs_list` — List jobs
- `foxctl_job_detail` — Job info
- `foxctl_job_cancel` — Cancel a job

### Workspaces
- `foxctl_workspaces_list` — List workspaces
- `foxctl_workspace_switch` — Switch workspace

### Sessions
- `foxctl_sessions_list` — List sessions
- `foxctl_session_detail` — Session info

### Companion
- `foxctl_companion_chat` — Chat with companion
- `foxctl_companion_memory` — Manage companion memory

### Context Plane
- `foxctl_context` — Get context overview
- `foxctl_control_inbox` — List coordinator control proposals (`/api/context/control-proposals`)
- `foxctl_control_inspect` — Inspect one control proposal (`/api/context/control-proposals/{id}`)
- `foxctl_control_decide` — Append typed coordinator decisions (`/api/context/control-proposals/{id}/decisions`)

### Mux (tmux/zellij)
- `foxctl_mux_list` — List panes
- `foxctl_mux_read` — Read pane output

### Mailbox
- `foxctl_mailbox_send` — Send mailbox message
- `foxctl_mailbox_list` — List mailbox messages

### CAS
- `foxctl_cas_list` — List CAS objects
- `foxctl_cas_get` — Get CAS object

### Console
- `foxctl_console_list` — List console sessions
- `foxctl_console_ask` — Send message to console

### Stats & Logs
- `foxctl_stats` — Job statistics
- `foxctl_logs` — Observability logs

### Reservations
- `foxctl_reservations_list` — File reservations

### SQLite
- `foxctl_sqlite_query` — Run SQLite query

### V2 Runtime
- `foxctl_v2_runs_list` — List v2 runs
- `foxctl_v2_run_detail` — Run details

### Foxprox
- `foxctl_foxprox_rooms` — List foxprox rooms
- `foxctl_foxprox_spawn` — Spawn via foxprox

### MCP
- `foxctl_mcp_status` — MCP status
- `foxctl_mcp_tools` — MCP tools list

### OpenAPI
- `foxctl_openapi` — Get OpenAPI spec

## Slash Commands

| Command | Description |
|---------|-------------|
| `/foxctl-status` | Show daemon status |
| `/foxctl-skills` | List skills |
| `/foxctl-tools` | List focused filesystem/code/repoindex/refactor/memory wrappers |
| `/foxctl-agents` | List agents |
| `/foxctl-rooms` | List rooms |
| `/foxctl-terminal` | Register and show the configured room's compatibility browser terminal |
| `/foxctl-tasks` | List tasks |
| `/foxctl-board` | Show board |
| `/foxctl-stats` | Show stats |
| `/foxctl-context` | Show context plane |
| `/foxctl-mcp` | Show MCP status |
| `/foxctl-workspaces` | List workspaces |

## Requirements

- [pi](https://github.com/badlogic/pi-mono) interactive mode
- foxctl daemon running (`foxctl web serve`)
- foxctl terminal gateway running for room terminal dogfood (`foxctl gateway --dev`)
- repoindex built for `foxctl_repoindex_*` tools (`foxctl index repo build --workspace . --go --typescript --elixir`)

## Development

The extension is a single TypeScript file that uses pi's `ExtensionAPI` to
register tools, commands, and event hooks. It communicates with foxctl via
standard `fetch()` calls to the daemon's HTTP API.

Typecheck through Pi's local toolchain:

```bash
cd /Users/joshka/repos/githubs/pi-mono
./node_modules/.bin/tsgo --ignoreConfig --noEmit \
  --module NodeNext --moduleResolution NodeNext \
  --target ES2022 --strict --skipLibCheck \
  .pi/extensions/foxctl.ts
```

Coordinator cockpit dogfood path:
- Use `foxctl_control_inbox` to triage proposals.
- Use `foxctl_control_inspect` to inspect one proposal before deciding.
- Use `foxctl_control_decide` to persist each approve/reject/clarification/override decision as a backend `CoordinatorDecision`.
- `/reload` may not expand Pi's initial tool allowlist; restart Pi if these tools are missing from the current session.

After source or docs changes, index the tracked integration from the foxctl
workspace with TypeScript enabled:

```bash
cd /Users/joshka/repos/personal/foxctl
./bin/foxctl index repo build --workspace . --go=false --typescript --elixir=false --semantic-anchors
```

If Pi/web repoindex tools report a repoindex schema downgrade or reset, rebuild
the affected compiled skill artifact before rebuilding the graph:

```bash
make skill SKILL=repo_index_search
./bin/foxctl index repo build --workspace . --go --typescript --elixir --semantic-anchors --include-tests --incremental=false
```
