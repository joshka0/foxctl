---
title: Multi-agent coordination
description: How foxctl agents coordinate work through intelligence, memory, rooms, direct mailbox, write conflict detection, and flow orchestration.
---

Status: Current for Hermes plugin, Pi extension, room tasks, mailbox, and file guard.

Foxctl provides a complete multi-agent coordination stack. Agents like [Pi](https://github.com/badlogic/pi-mono) and [Hermes](https://github.com/joshka/hermes-agent) collaborate through shared intelligence, memory, and real-time coordination primitives — without requiring any centralized orchestrator.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    foxctl daemon (:8091)                  │
│                                                          │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌─────────┐  │
│  │ REST API │  │ Room     │  │ Actor    │  │ Curator │  │
│  │          │  │ Protocol │  │ Mailbox  │  │ 5m/24h  │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────────┘  │
│       │              │              │                     │
│  ┌────┴──────────────┴──────────────┴────┐               │
│  │         Storage (Turso + SQLite)      │               │
│  │  Vector DB │ Blackboard │ Mailbox     │               │
│  └───────────────────────────────────────┘               │
│                                                          │
│  ┌────────────┐  ┌────────────┐  ┌───────────────────┐   │
│  │ Repo Index │  │ ContextWiki│  │ Flow Engine (DAG) │   │
│  └────────────┘  └────────────┘  └───────────────────┘   │
└──────────────────────────────────────────────────────────┘
        ▲                ▲                   ▲
        │ HTTP           │ herdr room        │ HTTP
   ┌────┴─────┐    ┌─────┴─────┐      ┌─────┴─────┐
   │    Pi    │    │   herdr   │      │  Hermes   │
   │ (TS ext) │    │   (mux)   │      │ (Py plug) │
   └──────────┘    └───────────┘      └───────────┘
```

## Intelligence layer

### Repo index

The repo index is a semantic code graph — symbols, files, packages, and concepts linked by structural edges (calls, implements, imports, contains).

- **`repoindex_search`** — find symbols by natural language query
- **`repoindex_dag`** — build an explanation graph from seed nodes
- **`repoindex_expand`** — traverse edges from specific nodes to understand dependencies

### Code search

Multi-lane search combining symbol lookup, ripgrep, and vector similarity:

1. **Symbol search** — repo index symbol lookup
2. **ripgrep** — fast literal/regex pattern matching
3. **Semantic search** — vector similarity over code embeddings (4096d Qwen3-Embedding-8B)

### Embedding pipeline

- Local GPU embedding server (Qwen3-Embedding-8B, 4096d)
- Incremental indexing with SHA-256 unchanged-file skip
- Circuit breaker on embedding worker failures
- 12,924 items indexed

## Memory layer

### Memory store

Named memory records with lifecycle states (active → stale → archived):

- **`memory_put`** — store learnings, decisions, gotchas, preferences, patterns
- **`memory_search`** — BM25 + vector hybrid search
- **Confidence scoring** — records carry confidence levels for retrieval ranking

### ContextWiki

A shared context plane that agents read and write to share knowledge that isn't visible from code alone:

- **Observations** — factual, confidence-scored, accumulate over time
- **Tensions** — contradictions, performance issues, complexity markers
- **Handoffs** — structured session handoffs (phase, outcome, next actions)
- **Top-of-mind bundle** — current workspace context (objective, constraints, active tasks)

### Obsidian vault / knowledge plane

Bridge between operational knowledge and long-form documentation:

- **`vault_promote`** — draft evergreen notes from ContextWiki observations
- **`vault_graph`** — generate structured notes from the repo index graph
- **`vault_bridge`** — reconcile repo docs with vault notes
- **`vault_search`** — full-text + semantic search over vault index

## Coordination layer

### Direct mailbox

Agents send direct messages without going through rooms:

```bash
# Send a direct message to another agent
curl -X POST http://localhost:8091/api/mailbox \
  -H "Content-Type: application/json" \
  -d '{
    "workspace_id": "/home/dev/repos/foxctl",
    "sender": "actor:hermes:local",
    "recipient": "actor:pi:local",
    "subject": "Cross-cutting concern",
    "body": "The auth middleware is also called from cron/sync.go",
    "kind": "alert",
    "priority": 2
  }'
```

Hermes tools:

- **`foxctl_mailbox_send`** — send a direct message to another agent
- **`foxctl_mailbox_inbox`** — read your inbox (filter by unread)
- **`foxctl_mailbox_ack`** — acknowledge messages

### Write conflict detection

The `hooks_file_guard` skill prevents conflicting edits with just-in-time coordination:

1. Agent A writes to `shared/utils.go` → file guard **auto-reserves** the file
2. Agent B tries to write the same file → file guard detects conflict
3. **Both agents get mailbox alerts**:
   - Agent B: "File reserved by Agent A. They are working on: [reason]. Send them a message to coordinate."
   - Agent A: "Agent B tried to write to your file. Coordinate if needed."

No upfront planning required — coordination happens when conflicts arise.

### Flow engine (DAG workflows)

Structured multi-agent workflows:

- **7 node kinds**: Agent, Skill, PTY, HTTP, Transform, Condition, Timer
- **7 edge transforms**: map input/output between nodes
- **4 triggers**: manual, cron, webhook, event

```bash
# Linear pipeline
foxctl flow build_pipeline --name code-review \
  --stages 'analyzer→implementer→reviewer'

# Fan-out to multiple language coders
foxctl flow build_fan_out --name multi-lang \
  --source analyst \
  --sinks 'go-coder,python-coder,rust-coder'
```

### Pipe protocol

Structured agent-to-agent data flow via room messages with subject `pipe:<pipe_id>`:

- **`foxctl_pipe_emit`** — send typed data to a named pipe
- **`foxctl_pipe_receive`** — receive data from a pipe

## Background curator

The curator worker runs inside the foxctl daemon with two concurrent loops:

| Loop | Interval | Purpose |
|------|----------|---------|
| **Active** | 5 minutes | Light checks — lifecycle transitions, stale detection, quick sweeps |
| **Dream** | 24 hours | Deep analysis — dedup, overlap detection, utility scoring, consolidation |

The curator is deterministic (no LLM calls). Configuration via `~/.foxctl/config.yaml`:

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

## Agent integrations

### Pi extension

A TypeScript pi extension (`integrations/pi/foxctl.ts`) exposing 100+ tools. Pi gets context injected before each turn when `--foxctl-context` is enabled, including memory matches, session recall, and active epic state.

```bash
pi --foxctl-url http://localhost:8091 \
   --foxctl-workspace /path/to/project \
   --foxctl-context \
   --foxctl-epic $EPIC_ID \
   --foxctl-room alpha
```

### Hermes plugin

A zero-dependency Python plugin (`integrations/hermes/`) providing 71 tools across 11 categories. Uses only Python stdlib (`urllib`, `json`, `os`, `subprocess`) for maximum portability.

```yaml
# ~/.hermes/config.yaml
foxctl:
  url: http://localhost:8091
  workspace: /path/to/project
  room: alpha
  actor: actor:hermes:local
```

## Quick start

```bash
# 1. Start the daemon
foxctl web serve --port 8091

# 2. Start the room loop (herdr relay)
foxctl room loop alpha --workspace /path/to/project --backend herdr &

# 3. Agents can now:
#    - Share knowledge via ContextWiki
#    - Coordinate tasks via rooms
#    - Message directly via mailbox
#    - Build workflows via flows
#    - Get notified on write conflicts (automatic)
```

## Canonical sources

- [docs/general/multi-agent-coordination.md](https://github.com/joshka0/foxctl/blob/main/docs/general/multi-agent-coordination.md)
- [integrations/hermes/README.md](https://github.com/joshka0/foxctl/blob/main/integrations/hermes/README.md)
- [integrations/pi/README.md](https://github.com/joshka0/foxctl/blob/main/integrations/pi/README.md)
