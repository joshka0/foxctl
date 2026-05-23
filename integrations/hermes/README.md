# Foxctl Plugin for Hermes Agent

Deep integration between foxctl's intelligence, coordination, and flow orchestration layers and hermes-agent, providing 73 tools spanning 11 categories.

## Tool Categories

### Flow Orchestration (multi-agent DAG workflows)

| Tool | Description |
|---|---|
| `foxctl_flow_create` | Create a new flow graph |
| `foxctl_flow_show` | Show flow detail: nodes, edges, state |
| `foxctl_flow_list` | List all flows in the workspace |
| `foxctl_flow_add_node` | Add an execution node (agent, skill, PTY, HTTP, transform) |
| `foxctl_flow_add_edge` | Connect nodes with typed edges and transforms |
| `foxctl_flow_start` | Start executing a flow |
| `foxctl_flow_status` | Get runtime status of a running flow |
| `foxctl_flow_logs` | Get execution logs for a flow run |
| `foxctl_flow_stop` | Stop a running flow |
| `foxctl_flow_output` | Push structured output to a running flow node |
| `foxctl_flow_build_pipeline` | Build a linear agent pipeline from stage definitions |
| `foxctl_flow_build_fan_out` | Build a fan-out: one source broadcasts to parallel sinks |
| `foxctl_flow_delete` | Delete a flow and all its data |

### Multi-Agent Coordination

| Tool | Description |
|---|---|
| `foxctl_agent_list` | List agents in the room |
| `foxctl_room_task_list` | List room tasks with optional status filter |
| `foxctl_room_task_add` | Create a new task in the room |
| `foxctl_room_task_claim` | Claim a pending task |
| `foxctl_room_task_complete` | Mark a task as completed |
| `foxctl_room_task_block` | Block a task with a reason |
| `foxctl_room_task_abandon` | Abandon a claimed task |
| `foxctl_room_status` | Get room delivery/member status |
| `foxctl_publish_context` | Broadcast agent state to the room |

### Pipe Protocol (agent-to-agent data flow)

| Tool | Description |
|---|---|
| `foxctl_pipe_emit` | Emit structured data through a named pipe to other agents |
| `foxctl_pipe_receive` | Consume pipe messages from the room inbox |

### ContextWiki (control + knowledge planes)

| Tool | Description |
|---|---|
| `foxctl_context_show` | Read the top-of-mind context bundle |
| `foxctl_context_report` | Generate a structured context report |
| `foxctl_context_observe` | Record an observation to the control plane |
| `foxctl_context_tension` | Record a tension (architectural concern) |
| `foxctl_context_capture` | Capture a handoff note for session continuity |
| `foxctl_context_infer` | Infer context from recent activity |
| `foxctl_context_handoffs` | List recorded handoff notes |
| `foxctl_context_dispatch` | Get context-aware task dispatch |
| `foxctl_context_next` | Get recommended next action |
| `foxctl_context_observations` | List all observations |
| `foxctl_context_tensions` | List all tensions |
| `foxctl_context_overview` | Get full context plane overview |

### Vault / Knowledge Plane

| Tool | Description |
|---|---|
| `foxctl_vault_search` | Search the Obsidian vault for notes |
| `foxctl_vault_stats` | Get vault statistics |
| `foxctl_vault_promote` | Promote a draft note to evergreen |
| `foxctl_vault_append` | Append content to a vault note |
| `foxctl_vault_bridge` | Bridge a foxctl doc to vault |
| `foxctl_vault_graph` | Get vault link graph |
| `foxctl_vault_index_build` | Rebuild the vault search index |

### Intelligence Layer (deep code understanding)

| Tool | Description |
|---|---|
| `foxctl_repo_search` | Search the repo index for symbols, files, packages |
| `foxctl_repo_dag` | Get an explanation dependency DAG for a code concept |
| `foxctl_repo_expand` | Expand the repo index graph from seed nodes |
| `foxctl_repo_open` | Open a repo index node by ID for full metadata |
| `foxctl_code_grep` | Search code patterns with function/class block expansion |
| `foxctl_semantic_search` | Unified semantic search across symbols, sessions, memory |
| `foxctl_branch_impact` | Inspect branch blast radius before review or editing via `code/branch_impact` |
| `foxctl_code_symbols` | Extract symbols from a file |
| `foxctl_text_grep` | Fast regex search across the workspace |
| `foxctl_fs_read` | Read file contents through CAS-backed storage |
| `foxctl_fs_find` | Find files by name, path, or glob pattern |
| `foxctl_codemap_list` | List available codemaps |
| `foxctl_codemap_get` | Get a codemap by ID |

### Memory & Session Layer

| Tool | Description |
|---|---|
| `foxctl_memory_search` | Search foxctl's vector-indexed knowledge base |
| `foxctl_memory_put` | Store a knowledge record (CLI → Turso) |
| `foxctl_session_recall` | Recall context from past sessions |

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

### Context Curation

| Tool | Description |
|---|---|
| `foxctl_context_curator` | Unified curator report: memory, observations, tensions, handoffs, vault drafts |
| `foxctl_context_memory_drafts` | Plan or write Obsidian inbox memory drafts from contextengine retrieval feedback |

### System

| Tool | Description |
|---|---|
| `foxctl_health` | Check foxctl daemon status |

## Install

```bash
# Symlink the plugin directory into hermes plugins
ln -sf /path/to/foxctl/integrations/hermes ~/.hermes/plugins/foxctl
```

To verify or relink both Hermes and Pi to the current foxctl checkout, run:

```bash
scripts/doctor-pi-hermes-integrations.sh
scripts/doctor-pi-hermes-integrations.sh --apply
```

The doctor checks that Hermes has lifecycle hooks, the automatic memory draft
tool, ContextWiki/vault tools, memory curator tools, and flow tools installed.
Override the default Hermes target with
`HERMES_FOXCTL_PLUGIN_PATH=/path/to/hermes/plugins/foxctl`.

## Configure

In `~/.hermes/config.yaml`:

```yaml
plugins:
  enabled:
    - foxctl

foxctl:
  url: "http://localhost:8090"
  workspace: "."
  room: "alpha"
  actor: "actor:hermes:local"
  vault_path: ".foxctl/templates/obsidian-vault"
  auto_bind: false
  memory_context: true
  epic_context: true
  memory_drafts_auto: false
  memory_drafts_apply: true
  memory_drafts_dry_run: false
  memory_drafts_interval_seconds: 900
  memory_drafts_lookback: "24h"
  memory_drafts_limit: 20
```

Environment variable overrides: `FOXCTL_URL`, `FOXCTL_WORKSPACE`, `FOXCTL_ROOM`, `FOXCTL_EPIC_ID`, `FOXCTL_ACTOR`, `FOXCTL_SESSION`, `FOXCTL_AUTO_BIND`, `FOXCTL_VAULT_PATH`, `FOXCTL_MEMORY_DRAFTS_AUTO`, `FOXCTL_MEMORY_DRAFTS_APPLY`, `FOXCTL_MEMORY_DRAFTS_DRY_RUN`, `FOXCTL_MEMORY_DRAFTS_INTERVAL_SECONDS`, `FOXCTL_MEMORY_DRAFTS_LOOKBACK`, `FOXCTL_MEMORY_DRAFTS_LIMIT`.

Set `memory_drafts_auto: true` to let Hermes lifecycle hooks create Obsidian inbox memory drafts in the background from contextengine retrieval feedback. These writes remain draft-only and review-gated; canonical note merges still require the ContextWiki proposal review path.

## Architecture

```
hermes agent
  └── plugin: foxctl (73 tools, 11 categories)
       ├── tools.py      → tool registrations + schemas
       ├── client.py     → HTTP + CLI client with envelope unwrapping
       ├── config.py     → config.yaml + env var reading
       └── __init__.py   → plugin entry + lifecycle hooks

foxctl daemon (localhost:8090)
  ├── Skill API (/api/skills/run) — 150 skills
  ├── REST API (/api/rooms, /api/context, /api/companion)
  └── Flow Engine (DAG executor)
       ├── NodeAgent   — spawns foxctl agents, captures output
       ├── NodeSkill   — executes foxctl skill subprocesses
       ├── NodePTY     — foxprox terminal sessions
       ├── NodeHTTP    — outbound HTTP requests
       ├── NodeTransform — pure data transforms
       └── Edges       — typed pipes with transforms:
             passthrough, regex_extract, template,
             jq_filter, split_lines, map_fields, file_write

foxprox broker
  ├── Room router — message fan-out to participants
  ├── Talkback rules — terminal output → room message routing
  └── Foxprox spawner — CLI agent lifecycle (droid, claude)

herdr (terminal multiplexer)
  └── room loop relay → pane delivery
```

### Coordination Stack

```
Layer 5: Flow DAG        — multi-agent orchestration (n8n-like)
           ┌──────────┐  output_ready  ┌──────────┐
           │ analyzer │ ──────────────► │ coder    │
           │ (agent)  │                 │ (agent)  │
           └──────────┘                 └──────────┘
               │
               │ fan-out
          ┌────┼────┐
          ▼    ▼    ▼
       go    py   rust
       (agent)(agent)(agent)

Layer 4: Pipe Protocol   — structured agent→agent data flow
Layer 3: Room Tasks      — claim/complete/block work items
Layer 2: Room Messages   — durable inbox + relay to terminals
Layer 1: Room Relay      — herdr terminal delivery to panes
```

## Usage Patterns

### Multi-agent pipeline

```
You: build a code review pipeline — analyze, implement, review

Hermes: [calls foxctl_flow_build_pipeline with 3 agent stages]
        Created flow "code-review-pipeline" (3 nodes, 2 edges)
        analyzer → implementer → reviewer
        [calls foxctl_flow_start]
        Flow started. Analyzer is running...

        [calls foxctl_flow_status]
        analyzer: completed (12s)
        implementer: running...
        reviewer: idle

        [calls foxctl_flow_logs node=reviewer]
        Review complete. 3 issues found, 2 fixed.
```

### Fan-out parallel work

```
You: have 3 agents implement in parallel — Go, Python, Rust

Hermes: [calls foxctl_flow_build_fan_out]
        source: analyst → sinks: [go-coder, python-coder, rust-coder]
        [calls foxctl_flow_start]
        Analyst running... output will fan out to all 3 coders.
```

### Agent coordination via room tasks

```
You: assign the embedding optimization task to hermes

Hermes: [calls foxctl_room_task_list status=pending]
        Found task: "optimize embedding worker batch size"
        [calls foxctl_room_task_claim task_id=...]
        Task claimed. Starting implementation...
        [implements the change]
        [calls foxctl_room_task_complete task_id=... notes="Batch size increased from 20 to 50"]
```

### Code exploration

```
You: how does the flow engine execute agent nodes?

Hermes: [calls foxctl_repo_search "flow engine agent executor"]
        [calls foxctl_code_grep "AgentExecutor" path="internal/runtime/flow"]
        Found AgentExecutor in executors.go:218 — spawns agents via
        AgentSpawner interface, supports prompt/ask input modes...
```

## Zero Dependencies

The plugin uses only Python stdlib (`urllib`, `json`, `os`, `subprocess`, `logging`) for maximum portability — no requests, no httpx, no external packages.
