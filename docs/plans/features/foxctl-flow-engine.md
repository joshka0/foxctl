# foxctl flow -- Terminal Flow Engine

**Status**: Draft
**Last reviewed**: 2026-05-02
**Scope**: CLI-driven flow engine where nodes are foxprox PTY sessions and edges are typed pipes that route output from one session to input of another with optional transforms.

## Problem

foxprox already gives us PTY sessions with intent-based I/O, rooms with fan-out message delivery, readiness detection, input lease arbitration, vtscreen state extraction, and delivery policies. What's missing is a way to wire sessions into directed graphs -- "n8n for terminals" where each node is a live PTY process and edges route data between them.

This enables patterns like:

- Agent orchestration chains (diff -> summarize -> review -> fix)
- Parallel fan-out + fan-in (one source, N reviewers, merge)
- Watch-transform loops (file watcher -> filter -> agent -> action)
- Any CLI tool becomes a flow node without an API wrapper

## Design Principles

1. **foxprox stays simple** -- the flow engine is a new orchestration layer *on top of* foxprox, not a modification to the broker core
2. **CLI-first, visuals later** -- Phase 1-3 are CLI-driven; the visual canvas (web-based) comes in Phase 5
3. **Session = node, room = edge substrate** -- flow nodes are foxprox sessions; flow edges use foxprox rooms and readiness for coordination
4. **Transform at the edge** -- data transformation happens in the flow engine between source output and target input, not inside foxprox
5. **No new external deps** -- uses only foxprox client, vtscreen, SQLite (modernc), and ULID (all already in tree)

## Package Placement

Per `docs/architecture/package-topology.md`:

| Package | Purpose | Family |
|---|---|---|
| `internal/runtime/flow/` | Flow engine, evaluator, transforms | runtime (peer to `runtime/terminal/`) |
| `internal/storage/flow/` | SQLite persistence for flow definitions + state | storage |
| `cmd/foxctl/cmd/flow.go` | CLI commands | cmd |

Not in `internal/v2` (not agent-runtime replacement) or `internal/intelligence` (not retrieval).

## Data Model

```go
// Flow is a named directed graph of PTY sessions.
type Flow struct {
    ID        string    // ULID
    Name      string
    Workspace string
    State     FlowState // draft | running | paused | stopped | errored
    CreatedAt time.Time
    UpdatedAt time.Time
}

type FlowState string

const (
    FlowDraft   FlowState = "draft"
    FlowRunning FlowState = "running"
    FlowPaused  FlowState = "paused"
    FlowStopped FlowState = "stopped"
    FlowErrored FlowState = "errored"
)

// FlowNode is a foxprox PTY session within a flow.
type FlowNode struct {
    ID         string   // ULID
    FlowID     string
    SessionID  string   // foxprox session ID (empty until flow starts)
    Kind       NodeKind // source | transform | sink | agent
    Label      string   // display name
    Cmd        []string // command to spawn
    Cwd        string
    Adapter    string
    SubmitKey  string
    Env        []string
    Rows       uint16
    Cols       uint16
}

type NodeKind string

const (
    NodeSource    NodeKind = "source"    // produces output (watcher, git diff, etc.)
    NodeTransform NodeKind = "transform" // receives input, produces transformed output
    NodeSink      NodeKind = "sink"      // receives input, no output expected
    NodeAgent     NodeKind = "agent"     // AI agent (claude, codex, etc.) with readiness detection
)

// FlowEdge is a typed pipe between two nodes.
type FlowEdge struct {
    ID              string        // ULID
    FlowID          string
    FromNodeID      string        // source node
    ToNodeID        string        // target node
    Transform       TransformKind // passthrough | regex_extract | template | jq
    TransformConfig string        // JSON config for the transform
    Trigger         TriggerKind   // output_idle | screen_match | exit | manual
    TriggerConfig   string        // JSON config (idle_ms, pattern, etc.)
    Policy          string        // foxprox delivery policy: immediate | queue | safe_prompt_only | interrupt
    Condition       string        // optional: "exit_code == 0", "output_len > 0"
}

type TransformKind string

const (
    TransformPassthrough TransformKind = "passthrough"
    TransformRegex       TransformKind = "regex_extract"
    TransformTemplate    TransformKind = "template"
    TransformJQ          TransformKind = "jq_filter"
    TransformSplitLines  TransformKind = "split_lines"
)

type TriggerKind string

const (
    TriggerOutputIdle  TriggerKind = "output_idle"
    TriggerScreenMatch TriggerKind = "screen_match"
    TriggerExit        TriggerKind = "exit"
    TriggerManual      TriggerKind = "manual"
)

// FlowRun tracks a single execution of a flow.
type FlowRun struct {
    ID          string
    FlowID      string
    State       RunState // running | completed | failed
    StartedAt   time.Time
    CompletedAt time.Time
    Error       string
}

type RunState string

const (
    RunRunning   RunState = "running"
    RunCompleted RunState = "completed"
    RunFailed    RunState = "failed"
)
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     foxctl CLI                               │
│  foxctl flow create | add-node | add-edge | start | stop    │
└──────────────────────────┬──────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────┐
│              internal/runtime/flow                           │
│                                                             │
│  Engine ──────────────────────────────────────────────      │
│    - creates foxprox sessions for each node                │
│    - starts evaluator goroutines for each edge             │
│    - manages flow lifecycle (start/stop/pause)             │
│                                                             │
│  Evaluator (goroutine per edge) ────────────────────       │
│    1. watch source session readiness                       │
│    2. extract output from vtscreen                         │
│    3. apply transform function                             │
│    4. deliver to target session via terminal.submit        │
│                                                             │
│  Transform Registry ───────────────────────────────        │
│    - passthrough, regex_extract, template, jq_filter       │
│                                                             │
│  Extractor ───────────────────────────────────────         │
│    - reads vtscreen snapshot from foxprox session          │
│    - extracts clean text output                            │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP/JSON over Unix socket
                           ▼
┌─────────────────────────────────────────────────────────────┐
│                   foxprox broker                             │
│  sessions │ rooms │ router │ leases │ vtscreen │ storage    │
└─────────────────────────────────────────────────────────────┘
```

## Key Interfaces

```go
// FoxproxClient is the subset of foxprox operations the flow engine needs.
// Implemented by wrapping foxproxbridge.HTTPClient.
type FoxproxClient interface {
    CreateSession(ctx context.Context, cmd []string, cwd string, env []string, rows, cols uint16, adapter, submitKey string) (Session, error)
    DeleteSession(ctx context.Context, sessionID string) error
    SessionReadiness(ctx context.Context, sessionID string) (Readiness, error)
    SessionScreen(ctx context.Context, sessionID string) (ScreenSnapshot, error)
    Submit(ctx context.Context, sessionID, text string) error
    WaitForReadiness(ctx context.Context, sessionID string, opts ReadinessOpts) (<-chan Readiness, error)
}

// TransformFunc transforms source output before delivery.
type TransformFunc func(input string, config string) (string, error)
```

## Execution Model

When `foxctl flow start <flow-id>` runs:

1. **Engine** creates a foxprox room for the flow
2. **For each node**: creates a foxprox session (PTY) with the node's cmd/cwd/adapter config, waits for readiness
3. **For each edge**: starts an evaluator goroutine that loops:
   - Watch source session readiness (poll or stream via foxprox client)
   - When trigger fires AND condition passes:
     - Extract output from source session's vtscreen
     - Apply the edge's transform function
     - Deliver result to target session via `terminal.submit` with the edge's delivery policy
   - Record delivery in FlowRun state
4. **Engine** tracks overall flow state; marks `completed` when all source nodes finish, or `failed` on error

Edge evaluators are independent goroutines. For fan-out (one source, N targets), each edge runs its own evaluator. For fan-in, see Phase 4.

## Phases

### Phase 1: Flow CRUD + Persistence

Define and store flows, nodes, and edges in SQLite. No execution.

**New files**:

| File | ~Lines | Purpose |
|---|---|---|
| `internal/runtime/flow/model.go` | 150 | Flow, FlowNode, FlowEdge, FlowRun types + enums |
| `internal/runtime/flow/store.go` | 80 | Store interface |
| `internal/storage/flow/sqlite.go` | 250 | SQLite implementation |
| `internal/storage/flow/migrations.go` | 50 | Schema DDL |
| `cmd/foxctl/cmd/flow.go` | 400 | CLI commands |

**CLI surface**:

```bash
foxctl flow create --name "review-pipeline" --workspace .
foxctl flow list [--workspace .]
foxctl flow show <flow-id>
foxctl flow delete <flow-id>

foxctl flow add-node <flow-id> --label "watcher" --cmd "bash" --kind source
foxctl flow add-node <flow-id> --label "agent" --cmd "claude" --kind agent --submit-key Enter
foxctl flow remove-node <flow-id> <node-id>

foxctl flow add-edge <flow-id> --from <node-id> --to <node-id> \
  --trigger output_idle --transform passthrough
foxctl flow remove-edge <flow-id> <edge-id>
```

**Tests**: Unit tests on model, store interface, SQLite implementation with temp DBs.

**Exit criteria**: Can create/list/show/delete flows with nodes and edges. Data persists in SQLite. No execution.

### Phase 2: Flow Execution Engine

Start/stop flows. The engine creates foxprox sessions for each node and runs edge evaluation goroutines.

**New files**:

| File | ~Lines | Purpose |
|---|---|---|
| `internal/runtime/flow/engine.go` | 200 | Engine: Start/Stop/Pause, session creation, evaluator lifecycle |
| `internal/runtime/flow/evaluator.go` | 150 | Per-edge goroutine: watch -> extract -> transform -> deliver |
| `internal/runtime/flow/transform.go` | 120 | Transform registry + passthrough, regex_extract, template |
| `internal/runtime/flow/extractor.go` | 60 | vtscreen output extraction |
| `internal/runtime/flow/engine_test.go` | 200 | Integration tests with in-memory foxprox |
| `internal/runtime/flow/transform_test.go` | 100 | Table-driven transform tests |

**CLI additions**:

```bash
foxctl flow start <flow-id>     # starts all sessions + edge evaluators
foxctl flow stop <flow-id>      # graceful stop
foxctl flow pause <flow-id>     # pause edge evaluation, keep sessions alive
foxctl flow status <flow-id>    # show per-node and per-edge state
```

**Tests**: Integration tests using in-memory foxprox broker. Table-driven tests for transforms.

**Exit criteria**: Can start a flow with 2+ nodes and a passthrough edge. Source output arrives at target session when source goes idle.

### Phase 3: Trigger + Transform Expansion

Add more trigger types, transform kinds, and conditional edges.

**New triggers**:
- `screen_match` -- fires when vtscreen matches a regex
- `exit` -- fires when session exits (exit code available)
- `manual` -- only fires on explicit `foxctl flow trigger <edge-id>`

**New transforms**:
- `regex_extract` -- `{"pattern": "...", "group": 1}`
- `template` -- `{"template": "Review this:\n{{.output}}"}`
- `jq_filter` -- `{"filter": ".[].file"}`
- `split_lines` -- `{"delimiter": "\n"}` (each line fans out separately)

**Conditional edges**: New `Condition` field on FlowEdge. Edge only fires if condition evaluates to true. Examples: `exit_code == 0`, `output_contains:error`, `output_len > 0`.

**Exit criteria**: Can build a 3-node pipeline with template transform and output_idle trigger that passes data through.

### Phase 4: Fan-in / Join Primitive

Enable diamond-shaped flows where multiple upstream edges must complete before delivery.

**New data model**:

```go
type FlowJoin struct {
    ID              string
    FlowID          string
    ToNodeID        string   // target
    FromEdges       []string // edge IDs that must all complete
    Transform       TransformKind
    TransformConfig string
    MergeMode       string   // concat | array | template
}
```

**Example flow**:

```
       +-> [agent A] -+
[src] -+              +-> [merge] -> [report]
       +-> [agent B] -+
```

**Exit criteria**: Can build diamond-shaped flows where a join node waits for all upstream edges.

### Phase 5: Web API + Visual Canvas Preparation

Expose flow CRUD and status via the foxctl web server for future visual canvas consumption.

**New files**:

| File | Purpose |
|---|---|
| `internal/interfaces/web/api/flow.go` | HTTP handlers for flow API |

**API surface**:

```
GET    /api/flows                     -- list flows
POST   /api/flows                     -- create flow
GET    /api/flows/:id                 -- show flow (nodes, edges, state)
DELETE /api/flows/:id                 -- delete flow
POST   /api/flows/:id/nodes           -- add node
DELETE /api/flows/:id/nodes/:nid      -- remove node
POST   /api/flows/:id/edges           -- add edge
DELETE /api/flows/:id/edges/:eid      -- remove edge
POST   /api/flows/:id/start           -- start flow
POST   /api/flows/:id/stop            -- stop flow
GET    /api/flows/:id/status          -- runtime status (node states, edge activity)
```

This is the API surface a future web canvas (React Flow, xyflow, etc.) would consume.

**Exit criteria**: All flow operations accessible via REST API. Response shapes are documented and stable enough for a frontend to consume.

## Future: Visual Canvas

The visual canvas is explicitly out of scope for Phase 1-4. When it's time:

1. The web API from Phase 5 is the backend contract
2. Canvas options:
   - **Web-based**: React + React Flow / xyflow library, terminals via xterm.js connected to foxprox sessions (fits the existing `foxctl web serve --foxprox` pattern)
   - **Native**: Fork or contribute to Horizon, replace its PTY layer with foxprox session rendering, add edge SVG rendering
   - **Tauri**: Similar to Terminal-64, embed xterm.js in a native shell with foxprox as the PTY backend
3. Edge rendering: SVG lines/curves between terminal panels with animated dashes for data flow
4. Node status badges: running/waiting/error/idle
5. The canvas reads flow definitions via the API and renders them spatially

## Example Usage (Phase 2+)

```bash
# Create a code review pipeline
foxctl flow create --name "review"
foxctl flow add-node review --label "diff" --cmd "git diff main" --kind source
foxctl flow add-node review --label "reviewer" --cmd "claude" --kind agent --submit-key Enter
foxctl flow add-node review --label "tests" --cmd "bash" --kind sink

foxctl flow add-edge review --from diff --to reviewer \
  --trigger output_idle \
  --transform template \
  --transform-config '{"template":"Review these changes for bugs:\n{{.output}}"}'

foxctl flow add-edge review --from reviewer --to tests \
  --trigger output_idle \
  --transform regex_extract \
  --transform-config '{"pattern":"suggested fix:\\s*(.+)","group":1}'

foxctl flow start review
```

```bash
# Parallel security + perf review
foxctl flow create --name "dual-review"
foxctl flow add-node dual-review --label "diff" --cmd "git diff main" --kind source
foxctl flow add-node dual-review --label "security" --cmd "claude" --kind agent --submit-key Enter
foxctl flow add-node dual-review --label "perf" --cmd "claude" --kind agent --submit-key Enter

foxctl flow add-edge dual-review --from diff --to security \
  --trigger output_idle --transform template \
  --transform-config '{"template":"Security review:\n{{.output}}"}'

foxctl flow add-edge dual-review --from diff --to perf \
  --trigger output_idle --transform template \
  --transform-config '{"template":"Performance review:\n{{.output}}"}'

foxctl flow start dual-review
```

## File Inventory (Phase 1 + 2)

```
internal/runtime/flow/
  model.go            ~150 lines   Flow, FlowNode, FlowEdge, FlowRun types + enums
  store.go            ~80 lines    Store interface
  engine.go           ~200 lines   Start/Stop/Pause, session creation, evaluator lifecycle
  evaluator.go        ~150 lines   Per-edge goroutine: watch -> extract -> transform -> deliver
  transform.go        ~120 lines   Transform registry + passthrough, regex_extract, template
  extractor.go        ~60 lines    vtscreen output extraction helper
  engine_test.go      ~200 lines   Integration tests with in-memory foxprox
  transform_test.go   ~100 lines   Table-driven transform tests

internal/storage/flow/
  sqlite.go           ~250 lines   SQLite Store implementation
  migrations.go       ~50 lines    Schema DDL

cmd/foxctl/cmd/
  flow.go             ~400 lines   CLI commands
```

Total estimate: ~1600 lines for a working flow engine with passthrough + template transforms and output_idle triggers.

## Dependencies

| Dependency | Already in repo? | Purpose |
|---|---|---|
| `github.com/joshka/foxprox/foxprox/client` | Yes (via foxproxbridge) | Session, room, readiness ops |
| `github.com/joshka/foxprox/foxprox/broker/vtscreen` | Yes | Output extraction |
| `modernc.org/sqlite` | Yes | Flow persistence |
| `github.com/oklog/ulid/v2` | Yes | Flow/node/edge IDs |

No new external dependencies.
