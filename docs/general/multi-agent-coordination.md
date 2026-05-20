# Multi-Agent Coordination

Foxctl provides a complete multi-agent coordination stack — from intelligence and memory layers to real-time write conflict detection and direct agent messaging. This doc covers how agents like [Pi](https://github.com/badlogic/pi-mono) and [Hermes](https://github.com/joshka/hermes-agent) collaborate through foxctl.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         foxctl daemon                           │
│                                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │
│  │ REST API │  │ Room     │  │ Actor    │  │ Background    │  │
│  │ :8091    │  │ Protocol │  │ Mailbox  │  │ Workers       │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  │ ┌───────────┐ │  │
│       │              │              │        │ │ Curator   │ │  │
│       │              │              │        │ │ (5m/24h)  │ │  │
│       │              │              │        │ └───────────┘ │  │
│       │              │              │        │ ┌───────────┐ │  │
│  ┌────┴──────────────┴──────────────┴────┐   │ │ Summary   │ │  │
│  │            Storage Layer              │   │ │ Worker    │ │  │
│  │  ┌────────┐ ┌────────┐ ┌───────────┐ │   │ └───────────┘ │  │
│  │  │Turso   │ │Black-  │ │SQLite     │ │   └───────────────┘  │
│  │  │Vector  │ │board   │ │Mailbox    │ │                      │
│  │  │DB      │ │Store   │ │Store      │ │                      │
│  │  └────────┘ └────────┘ └───────────┘ │                      │
│  └──────────────────────────────────────┘                      │
│                                                                 │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Repo Index   │  │ ContextWiki  │  │ Flow Engine          │  │
│  │ (semantic    │  │ (observations│  │ (DAG-based workflows │  │
│  │  code graph) │  │  tensions,   │  │  with agent nodes)   │  │
│  │              │  │  handoffs)   │  │                      │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
          ▲                    ▲                    ▲
          │ HTTP API           │ herdr room         │ HTTP API
          │                    │ relay              │
    ┌─────┴──────┐       ┌─────┴──────┐       ┌─────┴──────┐
    │     Pi     │       │   herdr    │       │  Hermes    │
    │ (TypeScript│       │  (mux      │       │  (Python   │
    │  extension)│       │   bridge)  │       │   plugin)  │
    └────────────┘       └────────────┘       └────────────┘
```

## Agent Integrations

### Pi Extension (`integrations/pi/foxctl.ts`)

A TypeScript pi extension exposing 100+ tools across the full foxctl API surface:

- **Intelligence**: repoindex search/DAG, code search, filesystem, codemaps
- **Room Agile**: epic/milestone/story lifecycle with TUI widgets
- **ContextWiki**: read top-of-mind bundle, capture handoffs, record observations
- **Vault**: search, promote evergreens, build repo graphs
- **Memory**: query, store learnings, session recall
- **Coordination**: room messaging, task management, agent discovery

Pi gets context injected before each turn when `--foxctl-context` is enabled, including memory matches, session recall, and active epic state.

### Hermes Plugin (`integrations/hermes/`)

A zero-dependency Python plugin (stdlib `urllib` only) providing 71 tools across 11 categories:

| Category | Tools | Description |
|----------|-------|-------------|
| Health & Status | 2 | Daemon health, workspace status |
| Intelligence | 13 | Repoindex, code search, filesystem, codemaps |
| ContextWiki | 12 | Show, report, observe, tension, capture, infer, handoffs |
| Vault / Knowledge | 7 | Search, promote, append, bridge, graph, index, stats |
| Memory | 4 | Put, search, curator report, session learnings |
| Room | 4 | Send, inbox, messages, message ack |
| **Mailbox (direct)** | **3** | **Send, inbox, ack** |
| Flow Orchestration | 13 | Create, show, list, delete, add_node, add_edge, start, status, logs, stop, output, build_pipeline, build_fan_out |
| Multi-Agent Coordination | 9 | Agent list, room tasks (add/claim/complete/block/abandon), room status, publish context |
| Pipe Protocol | 2 | Emit, receive structured agent-to-agent data |
| Embedding | 1 | Flush queued embedding jobs |

## Intelligence Layer

### Repo Index

The repo index is a semantic code graph — symbols, files, packages, and concepts linked by structural edges (calls, implements, imports, contains). Agents use it for:

- **`repoindex_search`** — find symbols by natural language query
- **`repoindex_dag`** — build an explanation graph from seed nodes (e.g., "how does auth middleware chain?")
- **`repoindex_expand`** — traverse edges from specific nodes to understand dependencies

### Code Search

Multi-lane code search combining:

1. **Symbol search** — repo index symbol lookup
2. **ripgrep** — fast literal/regex pattern matching
3. **Semantic search** — vector similarity over code embeddings (4096d Qwen3-Embedding-8B)

Results are ranked and deduplicated across lanes.

### Embedding Pipeline

- Local GPU embedding server (Qwen3-Embedding-8B, 4096d, `localhost:8100`)
- Incremental indexing with SHA-256 unchanged-file skip
- Circuit breaker on embedding worker failures
- 12,924 items indexed, ~2.7h total drain time

## Memory Layer

### Memory Store (Turso-backed)

Named memory records with lifecycle states (active, stale, archived):

- **`memory_put`** — store learnings, decisions, gotchas, preferences, patterns
- **`memory_search`** — BM25 + vector hybrid search
- **Lifecycle management** — records age through active → stale → archived
- **Confidence scoring** — records carry confidence levels for retrieval ranking

### ContextWiki

A shared context plane that agents read and write:

- **Observations** — factual, confidence-scored, accumulate over time
- **Tensions** — contradictions, performance issues, complexity markers
- **Handoffs** — structured session handoffs (phase, outcome, next actions)
- **Top-of-mind bundle** — the current workspace context (objective, constraints, active tasks)

Agents use ContextWiki to share knowledge that isn't visible from code alone — design decisions, gotchas discovered during implementation, cross-cutting concerns.

### Obsidian Vault / Knowledge Plane

Bridge between foxctl's operational knowledge and long-form documentation:

- **`vault_promote`** — draft evergreen notes from ContextWiki observations
- **`vault_graph`** — generate structured notes from the repo index graph
- **`vault_bridge`** — reconcile repo docs with vault notes
- **`vault_search`** — full-text + semantic search over vault index

## Coordination Layer

### Room Protocol

Rooms are durable coordination spaces with:

- **Epic/Milestone/Story lifecycle** — room-agile project management
- **Task tracking** — add, claim, complete, block, abandon
- **Message delivery** — via herdr mux bridge to agent terminals
- **Coordinator role** — a designated agent manages task assignment

The room loop (`foxctl room loop`) connects to herdr for live delivery to agent panes.

### Direct Mailbox

Agents send direct messages without going through rooms:

```
POST /api/mailbox
{
  "workspace_id": "/home/dev/repos/foxctl",
  "sender": "actor:hermes:local",
  "recipient": "actor:pi:local",
  "subject": "File conflict on utils.go",
  "body": "...",
  "kind": "alert",
  "priority": 2
}
```

- **`foxctl_mailbox_send`** — send a direct message to another agent
- **`foxctl_mailbox_inbox`** — read your inbox (filter by unread)
- **`foxctl_mailbox_ack`** — acknowledge messages

### Write Conflict Detection

When agents edit the same codebase, the `hooks_file_guard` skill prevents conflicts:

1. Agent A writes to `shared/utils.go` → file guard **auto-reserves** the file
2. Agent B tries to write to `shared/utils.go` → file guard detects conflict
3. **Both agents get mailbox alerts**:
   - Agent B: "File reserved by Agent A. They are working on: [reason]. Send them a message to coordinate."
   - Agent A: "Agent B tried to write to your file. Coordinate if needed."

No upfront planning required — coordination happens just-in-time when conflicts arise.

### Flow Engine (DAG Workflows)

Structured multi-agent workflows with the `foxctl flow` engine:

- **7 node kinds**: Agent, Skill, PTY, HTTP, Transform, Condition, Timer
- **7 edge transforms**: map input/output between nodes
- **4 triggers**: manual, cron, webhook, event
- **Built-in patterns**: linear pipelines, fan-out/fan-in, conditional branching

Example — code review pipeline:
```bash
foxctl flow build_pipeline --name code-review \
  --stages 'analyzer→implementer→reviewer'
```

Example — fan-out to multiple language coders:
```bash
foxctl flow build_fan_out --name multi-lang \
  --source analyst \
  --sinks 'go-coder,python-coder,rust-coder'
```

### Pipe Protocol

Structured agent-to-agent data flow via room messages:

- **`foxctl_pipe_emit`** — send typed data to a pipe
- **`foxctl_pipe_receive`** — receive data from a pipe

Pipes use room messages with subject `pipe:<pipe_id>` for delivery.

## Background Curator

The curator worker runs inside the foxctl daemon with two concurrent loops:

| Loop | Interval | Purpose |
|------|----------|---------|
| **Active** | 5 minutes | Light checks — lifecycle transitions, stale detection, quick sweeps |
| **Dream** | 24 hours | Deep analysis — dedup, overlap detection, utility scoring, consolidation |

The curator is fully deterministic (no LLM calls). It applies age, confidence, count, and utility heuristics. A future extension will add LLM-assisted review for borderline cases in dream mode.

Configuration via `~/.foxctl/config.yaml`:
```yaml
curator:
  active_enabled: true
  dream_enabled: true
  active_interval: 5m
  dream_interval: 24h
  stale_after_days: 30
  archive_after_days: 90
  dry_run: false
```

## Skills Pack

Four teaching skills help agents learn coordination patterns:

| Skill | Location | Teaches |
|-------|----------|---------|
| Agent Coordination | `configs/skills-pack/foxctl-agent-coordination/` | Room tasks, agent discovery, context broadcasting |
| Flow Orchestration | `configs/skills-pack/foxctl-flow-orchestration/` | DAG creation, pipeline/fan-out patterns, node types |
| Context Curator | `configs/skills-pack/foxctl-context-curator/` | Running curator reports, interpreting proposals |
| Hermes Integration | `configs/skills-pack/foxctl-hermes-integration/` | Using the foxctl plugin from Hermes |

## Quick Start

### 1. Start the daemon

```bash
foxctl web serve --port 8091
```

### 2. Start the room loop (herdr relay)

```bash
foxctl room loop alpha --workspace /path/to/project --backend herdr &
```

### 3. Configure Hermes

```yaml
# ~/.hermes/config.yaml
foxctl:
  url: http://localhost:8091
  workspace: /path/to/project
  room: alpha
  actor: actor:hermes:local
  vault_path: /path/to/obsidian/vault
```

### 4. Configure Pi

```bash
pi --foxctl-url http://localhost:8091 \
   --foxctl-workspace /path/to/project \
   --foxctl-context \
   --foxctl-epic $EPIC_ID \
   --foxctl-room alpha
```

### 5. Coordinate

Agents can now:
- Share knowledge via ContextWiki (`foxctl_context_observe`, `foxctl_context_capture`)
- Coordinate tasks via rooms (`foxctl_room_task_add`, `foxctl_room_task_claim`)
- Message directly via mailbox (`foxctl_mailbox_send`, `foxctl_mailbox_inbox`)
- Build workflows via flows (`foxctl_flow_create`, `foxctl_flow_start`)
- Get notified on write conflicts automatically (file guard hook)

## Key Files

| Component | Path | Lines |
|-----------|------|-------|
| Hermes client | `integrations/hermes/client.py` | ~1,450 |
| Hermes tools | `integrations/hermes/tools.py` | ~2,200 |
| Hermes config | `integrations/hermes/config.py` | ~100 |
| Hermes plugin manifest | `integrations/hermes/plugin.yaml` | ~30 |
| Pi extension | `integrations/pi/foxctl.ts` | ~1,800 |
| Curator worker | `internal/runtime/curator/worker.go` | ~430 |
| File guard hook | `skills/hooks_file_guard/main.go` | ~280 |
| Flow engine | `internal/runtime/flow/engine.go` | ~750 |
| ContextWiki handlers | `internal/interfaces/web/api/contextwiki_handlers.go` | ~950 |
| Mailbox API | `internal/interfaces/web/api/mailbox.go` | ~300 |
| Room agile API | `internal/interfaces/web/api/room_agile.go` | ~600 |

## Merge History

| MR | Title | Key additions |
|----|-------|---------------|
| !33 | Pi room-agile + herdr bridge | Pi extension, room-agile lifecycle, herdr mux relay |
| !35 | Hermes plugin + deep intelligence | 42 tools, repoindex, code search, embedding pipeline |
| !37 | Vault/knowledge plane | Obsidian vault bridge, ContextWiki, 7 vault tools |
| !38 | Multi-agent context plane | Curator, mailbox, file guard notifications, flow orchestration, pipe protocol, 29 new tools |
