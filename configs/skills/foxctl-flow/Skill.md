---
name: foxctl Flow Engine
description: Envelope-contract flow graphs with agent nodes, transforms, push output, and daemon integration. Use for orchestrating multi-step agent pipelines, automated research workflows, and structured output processing.
---

# foxctl Flow Engine

Envelope-contract flow graphs: DAGs where each node consumes and produces JSON envelopes. Nodes are connected by edges with optional transforms and conditions. The engine orchestrates execution, tracks state, and streams logs.

## Architecture

```
Source Node → Edge (transform) → Target Node → Edge → ...
    ↓                              ↓
  OutputBus ──────────→ Edge Evaluators (subscribe + fan-out)
```

- **Nodes**: Agents (droid exec), transforms (file_write, jq, passthrough), skills
- **Edges**: Connect nodes with optional transforms and trigger conditions
- **OutputBus**: Pub/sub for node outputs; edge evaluators subscribe and fan out
- **Daemon**: Per-workspace engine cache; foxprox-backed agent spawner for droid exec

## Quick Start

```bash
# Create a flow
foxctl flow create --name "research-pipeline" --workspace .

# Add an agent node (push mode)
foxctl flow flow add-node <flow-id> --workspace . \
  --label "researcher" --kind agent \
  --config '{
    "role": "researcher",
    "prompt": "Research the topic from upstream data.",
    "output_mode": "push"
  }'

# Add a file_write transform node
foxctl flow add-node <flow-id> --workspace . \
  --label "writer" --kind transform \
  --config '{
    "transform": "file_write",
    "path": "docs/output/report.md",
    "format": "markdown"
  }'

# Connect them
foxctl flow add-edge <flow-id> --workspace . \
  --from researcher --to writer

# Start the flow (routes through daemon)
foxctl flow start research-pipeline --workspace .

# Check status
foxctl flow status research-pipeline --workspace .

# Stream logs
foxctl flow logs <run-id> --workspace . --follow

# Push output from an external agent
foxctl flow output <run-id> --node <node-id> \
  --data '{"summary": "...", "findings": [...]}' \
  --workspace .
```

## Node Types

| Kind | Description | Config Fields |
|------|-------------|---------------|
| `agent` | CLI agent (droid) via foxprox PTY | `role`, `prompt`, `output_mode`, `cli_cmd` |
| `transform` | Data transform (file_write, jq, etc.) | `transform`, `path`, `format`, or `config` |
| `skill` | foxctl skill execution | `skill`, `input` |

### Agent Node

Agent nodes launch CLI agents (droid) through foxprox PTY sessions. The daemon uses `droid exec --skip-permissions-unsafe` for push mode agents.

**Output modes:**

| Mode | How output is captured |
|------|----------------------|
| `push` | Agent calls `foxctl flow output` to push structured JSON back. Recommended. |
| `session_summary` | Engine polls agent status until completion. No structured output. |
| `ask` | Engine sends follow-up message and waits for reply. |

**Push mode flow:**
1. Engine spawns droid via foxprox with prompt containing run_id and node_id
2. Agent executes autonomously (no permission dialogs)
3. Agent calls `foxctl flow output <run-id> --node <node-id> --data '<json>'`
4. Engine receives output via OutputBus, triggers downstream edge evaluators

**Config example (push mode with file_write):**
```json
{
  "role": "researcher",
  "prompt": "Analyze the codebase and report findings as structured JSON.",
  "output_mode": "push"
}
```

### Transform Node

Transforms process upstream data and produce output. The `transform` field names the transform kind.

**Available transforms:**

| Transform | Description | Config |
|-----------|-------------|--------|
| `file_write` | Write data to a file | `path`, `format` (raw/json/markdown) |
| `passthrough` | Pass input through unchanged | (none) |
| `jq_filter` | JQ query on JSON data | `filter` |
| `regex_extract` | Extract patterns from text | `pattern`, `group` |
| `template` | Go template rendering | `template` |
| `split_lines` | Split text into lines | (none) |
| `map_fields` | Rename/map object fields | `mapping` |

**file_write example:**
```json
{
  "transform": "file_write",
  "path": "docs/report/{{.NodeID}}.md",
  "format": "markdown"
}
```

Path supports Go template syntax: `{{.NodeID}}`, `{{.FlowID}}`, and envelope data fields.

## Flow Lifecycle

```
draft → running → stopped
              ↘ paused → running (resume)
```

### Commands

| Command | Description |
|---------|-------------|
| `foxctl flow create --name <n>` | Create a new flow |
| `foxctl flow list` | List all flows |
| `foxctl flow show <id>` | Show flow detail (nodes, edges) |
| `foxctl flow add-node <id> --label <l> --kind <k> --config '<json>'` | Add a node |
| `foxctl flow add-edge <id> --from <label> --to <label>` | Add an edge |
| `foxctl flow remove-node <id> --node <node-id>` | Remove a node |
| `foxctl flow remove-edge <id> --edge <edge-id>` | Remove an edge |
| `foxctl flow start <id>` | Start execution (via daemon) |
| `foxctl flow stop <id>` | Stop a running flow |
| `foxctl flow pause <id>` | Pause a running flow |
| `foxctl flow status <id>` | Show runtime state per node |
| `foxctl flow logs <run-id>` | Show run logs |
| `foxctl flow logs <run-id> --follow` | Stream logs (NDJSON) |
| `foxctl flow output <run-id> --node <id> --data '<json>'` | Push output |
| `foxctl flow delete <id>` | Delete a flow |

## Daemon Integration

Flows route through the foxctl daemon for long-lived execution. The daemon:

1. **Auto-start**: `foxctl flow start` starts the daemon if not running
2. **Per-workspace engines**: Each workspace gets its own flow engine + SQLite store
3. **Agent spawner**: Uses foxprox for PTY management when foxprox socket is available
4. **Fallback**: Falls back to in-process execution if daemon is unavailable

### Per-Workspace Storage

```
<workspace>/.foxctl/flow.db    ← flow definitions + run logs
```

Each workspace's flows are isolated. The `--workspace` flag is required for all operations.

## Logs & Streaming

```bash
# Historical logs (JSON envelope)
foxctl flow logs <run-id> --workspace .

# Stream live logs (NDJSON, one envelope per line)
foxctl flow logs <run-id> --workspace . --follow

# Filter by node
foxctl flow logs <run-id> --workspace . --node <node-id>

# Filter by node label
foxctl flow logs <run-id> --workspace . --node researcher
```

Log entries are JSON envelopes with `meta.seq` (monotonic), `status:"progress"` for intermediate, and `meta.final:true` for terminal.

## Push Output API

External agents push structured output back into the flow engine:

```bash
foxctl flow output <run-id> \
  --node <node-id-or-label> \
  --data '{"key": "value"}' \
  --workspace .
```

This publishes to the run's OutputBus. Edge evaluators subscribed to that node automatically pick up the output and trigger downstream nodes.

**End-to-end flow:**
```
1. foxctl flow start → daemon creates foxprox session (droid exec)
2. droid receives prompt with run_id + node_id
3. droid does work autonomously
4. droid calls: foxctl flow output <run-id> --node <node-id> --data '<json>'
5. Engine receives output via OutputBus
6. Edge evaluator triggers writer (file_write)
7. file_write writes markdown to disk
```

## Error Handling

Flow errors use foxctl envelope error codes:

| Code | Meaning |
|------|---------|
| `ENOTFOUND` | Flow/run/node not found |
| `EARG` | Missing or invalid parameter |
| `EALREADY` | Flow already running |
| `ESTATE` | Invalid state transition (e.g., stop a stopped flow) |
| `ECYCLE` | Flow graph contains a cycle |
| `EPARSE` | Transform or config parse error |
| `ETIMEOUT` | Agent execution timeout |
| `ECANCELED` | Flow cancelled or context done |

## Code Locations

| Component | Path |
|-----------|------|
| Flow model | `internal/runtime/flow/model.go` |
| Engine (Start/Stop/Pause/Status) | `internal/runtime/flow/engine.go` |
| Node executors | `internal/runtime/flow/executors.go` |
| Edge evaluator | `internal/runtime/flow/evaluator.go` |
| Transform registry | `internal/runtime/flow/transform.go` |
| OutputBus | `internal/runtime/flow/bus.go` |
| Foxprox agent spawner | `internal/runtime/flow/foxprox_spawner.go` |
| Daemon RPC handlers | `internal/runtime/daemon/service.go` |
| Daemon client | `internal/runtime/daemon/client.go` |
| CLI commands | `cmd/foxctl/cmd/flow.go` |
| CLI output command | `cmd/foxctl/cmd/flow_output.go` |
| CLI logs command | `cmd/foxctl/cmd/flow_logs.go` |
| CLI daemon routing | `cmd/foxctl/cmd/flow_daemon.go` |
| SQLite store | `internal/storage/flow/sqlite.go` |

## Related Skills

- `foxctl-run` — Execute individual foxctl skills
- `foxctl-agents` — Multi-agent coordination and spawning
- `foxctl-daemon` — Agent daemon architecture and engine selection
