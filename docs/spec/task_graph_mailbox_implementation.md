# Task Graph + Mailbox/Blackboard Implementation Spec

**Status:** Draft\
**Last Updated:** 2024-11-27\
**Depends On:** `core_profile_v1.md`, `task_graph_insights.md`,
`mailbox_blackboard.md`, `bd_mail_agent_interface.md`

---

## Overview

This document is the implementation blueprint for integrating task graph
analysis and a local mailbox/blackboard system into `agentctl`. It consolidates
the designs from `task_graph_insights.md`, `mailbox_blackboard.md`, and
`bd_mail_agent_interface.md` into an actionable, phased plan with concrete file
paths, interfaces, schemas, and test requirements.

### Goals

1. **Task Graph Insights** – Expose PageRank, critical-path, in/out-degree,
   topological order, and cycle detection over workspace tasks via
   `todo/manage.graph_insights`.
2. **Mailbox/Blackboard** – Provide per-workspace actor-style messaging with
   priorities, streams, ack semantics, and advisory file reservations.
3. **Hook Integration** – Wire mailbox state into Claude hooks via `mail_router`
   (context injection) and `file_guard` (reservation enforcement).
4. **Overseer/Viewer** – Combine graph + mailbox stats for "what next?"
   recommendations.

### Non-Goals

- Federated/remote mailbox (local SQLite only).
- Hard-blocking file locks (advisory only).
- Breaking changes to existing `todo/manage` operations.

---

## Phase 1: Task Graph Analyzer

### 1.1 New Package: `internal/analysis/tasksgraph`

**Location:** `internal/analysis/tasksgraph/`

**Files:**

- `graph.go` – Core analyzer implementation.
- `graph_test.go` – Unit tests.

#### Types

```go
package tasksgraph

import (
    "time"
    "github.com/jkatigb/agentctl/internal/storage/tasks"
)

// NodeMetrics holds computed metrics for a single task node.
type NodeMetrics struct {
    TaskID            string  `json:"task_id"`
    PageRank          float64 `json:"pagerank"`
    CriticalPathScore int     `json:"critical_path_score"`
    InDegree          int     `json:"in_degree"`
    OutDegree         int     `json:"out_degree"`
}

// Insights is the output of graph analysis for a workspace.
type Insights struct {
    WorkspaceID      string        `json:"workspace_id"`
    GeneratedAt      time.Time     `json:"generated_at"`
    Nodes            []NodeMetrics `json:"nodes"`
    TopologicalOrder []string      `json:"topological_order"` // empty if cycles
    Cycles           [][]string    `json:"cycles"`            // detected cycles
}

// Analyzer computes graph metrics from a task set.
type Analyzer interface {
    Analyze(tasks []tasks.Task, workspaceID string) (Insights, error)
}

// NewAnalyzer returns the default gonum-backed analyzer.
func NewAnalyzer() Analyzer
```

#### Graph Construction

- **Nodes:** One node per `Task.ID`.
- **Edges:** Directed edge `u → v` if `v ∈ u.DependsOn` (i.e., `u` depends on
  `v`, so `v` must complete first).
- **Filter:** By default, include all tasks. Optionally filter by
  `Status != "completed"` via options.

#### Metrics Computation

| Metric                  | Algorithm                                                  | Notes                                 |
| ----------------------- | ---------------------------------------------------------- | ------------------------------------- |
| **PageRank**            | `gonum/graph/network.PageRank`                             | Default damping 0.85, 100 iterations. |
| **In-Degree**           | Count incoming edges                                       | How many tasks depend on this one.    |
| **Out-Degree**          | Count outgoing edges                                       | How many tasks this one depends on.   |
| **Critical-Path Score** | Longest path from node to any sink (node with in-degree 0) | BFS/DFS from each node; memoize.      |
| **Topological Order**   | Kahn's algorithm or `gonum/graph/topo.Sort`                | Return empty if cycles exist.         |
| **Cycles**              | `gonum/graph/topo.DirectedCyclesIn` or Tarjan SCC          | Return list of cycles.                |

#### Test Cases (`graph_test.go`)

1. **Empty graph** – 0 tasks → empty nodes, empty topo order, no cycles.
2. **Linear chain** – A → B → C → D.
   - Topo order: `[D, C, B, A]` (or reverse, depending on edge direction).
   - No cycles.
   - CriticalPathScore(A) = 3, CriticalPathScore(D) = 0.
3. **Fork-join** – A → {B, C} → D.
   - D has highest in-degree (2).
   - Topo order exists.
4. **Cycle** – A → B → C → A.
   - Topo order empty.
   - Cycles contains `[A, B, C]`.
5. **Disconnected subgraphs** – Two independent chains.
   - Both included in metrics.

---

### 1.2 Extend `todo/manage` Skill

**Location:** `skills/todo/main.go`

#### Input Schema Addition

```go
type input struct {
    Operation       string           `json:"operation"`
    WorkspaceID     string           `json:"workspace_id"`
    Add             *addRequest      `json:"add"`
    Complete        *completeRequest `json:"complete"`
    SetActive       *setActiveReq    `json:"set_active"`
    EnsureActive    *ensureActiveReq `json:"ensure_active"`
    GraphInsights   *graphInsightsReq `json:"graph_insights"` // NEW
}

type graphInsightsReq struct {
    IncludeCompleted bool `json:"include_completed"` // default false
    Limit            int  `json:"limit"`             // max nodes to return, 0 = all
}
```

#### New Operation: `graph_insights`

```go
case "graph_insights":
    allTasks, err := store.ListByWorkspace(ctx, workspaceID)
    if err != nil {
        return err
    }
    // Filter completed if requested
    if in.GraphInsights == nil || !in.GraphInsights.IncludeCompleted {
        allTasks = filterPending(allTasks)
    }
    insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
    if err != nil {
        return err
    }
    // Apply limit
    if in.GraphInsights != nil && in.GraphInsights.Limit > 0 && len(insights.Nodes) > in.GraphInsights.Limit {
        // Sort by CriticalPathScore desc, then PageRank desc
        sort.Slice(insights.Nodes, func(i, j int) bool {
            if insights.Nodes[i].CriticalPathScore != insights.Nodes[j].CriticalPathScore {
                return insights.Nodes[i].CriticalPathScore > insights.Nodes[j].CriticalPathScore
            }
            return insights.Nodes[i].PageRank > insights.Nodes[j].PageRank
        })
        insights.Nodes = insights.Nodes[:in.GraphInsights.Limit]
    }
    data = map[string]any{
        "insights": insights,
        "summary":  fmt.Sprintf("analyzed %d tasks, %d cycles", len(allTasks), len(insights.Cycles)),
    }
```

#### Output Envelope

```json
{
	"version": 1,
	"status": "ok",
	"command": "todo/manage",
	"data": {
		"insights": {
			"workspace_id": "/path/to/workspace",
			"generated_at": "2024-11-27T12:00:00Z",
			"nodes": [
				{
					"task_id": "01JDXYZ...",
					"pagerank": 0.32,
					"critical_path_score": 4,
					"in_degree": 2,
					"out_degree": 1
				}
			],
			"topological_order": ["01JD...", "01JE...", "01JF..."],
			"cycles": []
		},
		"summary": "analyzed 12 tasks, 0 cycles"
	},
	"meta": {
		"source": "run",
		"runner": "exec"
	}
}
```

---

### 1.3 CLI: `agentctl todo insights`

**Location:** `cmd/agentctl/cmd/todo.go`

```go
func newTodoInsightsCommand() *cobra.Command {
    var workspaceID string
    var includeCompleted bool
    var limit int

    cmd := &cobra.Command{
        Use:   "insights",
        Short: "Show task graph insights (PageRank, critical path, cycles)",
        RunE: func(cmd *cobra.Command, _ []string) error {
            payload := map[string]any{
                "operation": "graph_insights",
                "graph_insights": map[string]any{
                    "include_completed": includeCompleted,
                    "limit":             limit,
                },
            }
            if workspaceID != "" {
                payload["workspace_id"] = workspaceID
            }
            return runTodoSkill(cmd, payload)
        },
    }

    cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID")
    cmd.Flags().BoolVar(&includeCompleted, "include-completed", false, "Include completed tasks")
    cmd.Flags().IntVar(&limit, "limit", 0, "Max nodes to return (0 = all)")
    return cmd
}
```

Wire into `newTodoCommand()`:

```go
cmd.AddCommand(
    newTodoAddCommand(),
    newTodoCompleteCommand(),
    newTodoListCommand(),
    newTodoActiveCommand(),
    newTodoInsightsCommand(), // NEW
)
```

---

## Phase 2: Mailbox/Blackboard Storage + Skill

### 2.1 Domain Types

**Location:** `internal/domain/mailbox/types.go` (new)

```go
package mailbox

import "time"

// Message represents a mailbox message between actors.
type Message struct {
    ID          string    `json:"id"`
    WorkspaceID string    `json:"workspace_id"`
    TaskID      string    `json:"task_id,omitempty"`      // optional task association
    Stream      string    `json:"stream"`                  // e.g., "general", "review", "urgent"
    Sender      string    `json:"sender"`                  // actor ID (e.g., "admin", "agent-1")
    Recipient   string    `json:"recipient"`               // actor ID or "*" for broadcast
    Kind        string    `json:"kind"`                    // directive, info, question, status
    Priority    int       `json:"priority"`                // 1 = highest, 5 = lowest
    AckRequired bool      `json:"ack_required"`
    Status      string    `json:"status"`                  // unread, read, acked
    Subject     string    `json:"subject"`
    Body        string    `json:"body"`
    CreatedAt   time.Time `json:"created_at"`
}

// Reservation represents an advisory file lock.
type Reservation struct {
    ID          string    `json:"id"`
    WorkspaceID string    `json:"workspace_id"`
    Path        string    `json:"path"`                    // relative to workspace root
    Holder      string    `json:"holder"`                  // actor ID
    Mode        string    `json:"mode"`                    // "exclusive" or "shared"
    ExpiresAt   time.Time `json:"expires_at"`
    CreatedAt   time.Time `json:"created_at"`
}

// InboxFilter specifies inbox query parameters.
type InboxFilter struct {
    TaskID     string    `json:"task_id,omitempty"`
    Stream     string    `json:"stream,omitempty"`
    OnlyUnread bool      `json:"only_unread,omitempty"`
    Since      time.Time `json:"since,omitempty"`
    Limit      int       `json:"limit,omitempty"`
}

// Message statuses.
const (
    StatusUnread = "unread"
    StatusRead   = "read"
    StatusAcked  = "acked"
)

// Reservation modes.
const (
    ModeExclusive = "exclusive"
    ModeShared    = "shared"
)

// Message kinds.
const (
    KindDirective = "directive"
    KindInfo      = "info"
    KindQuestion  = "question"
    KindStatus    = "status"
)

// Priority levels (1 = highest).
const (
    PriorityUrgent = 1
    PriorityHigh   = 2
    PriorityNormal = 3
    PriorityLow    = 4
    PriorityBatch  = 5
)
```

---

### 2.2 Storage Interface

**Location:** `internal/storage/mailbox/blackboard.go` (new file, separate from
existing `store.go`)

```go
package mailbox

import (
    "context"
    "time"

    "github.com/jkatigb/agentctl/internal/domain/mailbox"
)

// BlackboardStore handles messages and file reservations.
type BlackboardStore interface {
    Close() error

    // Messages
    AddMessage(ctx context.Context, m mailbox.Message) (mailbox.Message, error)
    ListInbox(ctx context.Context, workspaceID, actorID string, f mailbox.InboxFilter) ([]mailbox.Message, error)
    MarkRead(ctx context.Context, workspaceID string, ids []string) error
    AckMessages(ctx context.Context, workspaceID, actorID string, ids []string) error
    DeleteMessages(ctx context.Context, workspaceID string, ids []string) error

    // Reservations
    ReservePaths(ctx context.Context, workspaceID, actorID string, paths []string, mode string, ttl time.Duration) (granted []mailbox.Reservation, conflicts []mailbox.Reservation, err error)
    ListReservations(ctx context.Context, workspaceID string, paths []string) ([]mailbox.Reservation, error)
    ReleaseReservations(ctx context.Context, workspaceID, actorID string, ids []string) error
    ReleaseByPaths(ctx context.Context, workspaceID, actorID string, paths []string) error
    CleanExpired(ctx context.Context, workspaceID string) (int, error)
}
```

---

### 2.3 SQLite Schema

**Location:** `internal/storage/mailbox/blackboard.go` (in `migrate` func)

```sql
-- Messages table
CREATE TABLE IF NOT EXISTS mail_messages (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    task_id      TEXT,
    stream       TEXT NOT NULL DEFAULT 'general',
    sender       TEXT NOT NULL,
    recipient    TEXT NOT NULL,
    kind         TEXT NOT NULL DEFAULT 'info',
    priority     INTEGER NOT NULL DEFAULT 3,
    ack_required INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'unread',
    subject      TEXT NOT NULL,
    body         TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mail_inbox 
    ON mail_messages (workspace_id, recipient, status, priority, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_mail_task 
    ON mail_messages (workspace_id, task_id);

-- Reservations table
CREATE TABLE IF NOT EXISTS file_reservations (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    path         TEXT NOT NULL,
    holder       TEXT NOT NULL,
    mode         TEXT NOT NULL DEFAULT 'exclusive',
    expires_at   TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reservations_active 
    ON file_reservations (workspace_id, path, expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_reservations_path_holder 
    ON file_reservations (workspace_id, path, holder);
```

---

### 2.4 Reservation Logic

```go
func (s *sqlBlackboardStore) ReservePaths(
    ctx context.Context,
    workspaceID, actorID string,
    paths []string,
    mode string,
    ttl time.Duration,
) (granted []mailbox.Reservation, conflicts []mailbox.Reservation, err error) {
    now := time.Now().UTC()
    expiresAt := now.Add(ttl)

    for _, path := range paths {
        // Check for existing non-expired reservations
        existing, err := s.getActiveReservations(ctx, workspaceID, path, now)
        if err != nil {
            return nil, nil, err
        }

        hasConflict := false
        for _, r := range existing {
            if r.Holder == actorID {
                // Same holder - extend/update
                continue
            }
            if mode == mailbox.ModeExclusive || r.Mode == mailbox.ModeExclusive {
                // Conflict: exclusive vs any, or any vs exclusive
                conflicts = append(conflicts, r)
                hasConflict = true
            }
            // shared vs shared is OK
        }

        if !hasConflict {
            // Grant reservation
            res := mailbox.Reservation{
                ID:          ulid.Make().String(),
                WorkspaceID: workspaceID,
                Path:        path,
                Holder:      actorID,
                Mode:        mode,
                ExpiresAt:   expiresAt,
                CreatedAt:   now,
            }
            if err := s.insertReservation(ctx, res); err != nil {
                return nil, nil, err
            }
            granted = append(granted, res)
        }
    }

    return granted, conflicts, nil
}
```

---

### 2.5 New Skill: `skills/mailbox`

**Location:** `skills/mailbox/`

**Files:**

- `main.go` – Skill implementation.
- `main_test.go` – Tests.
- `skill.yaml` – Manifest.

#### Input Schema

```go
type input struct {
    Operation   string       `json:"operation"`
    WorkspaceID string       `json:"workspace_id"`
    Send        *sendReq     `json:"send,omitempty"`
    Inbox       *inboxReq    `json:"inbox,omitempty"`
    Ack         *ackReq      `json:"ack,omitempty"`
    Reserve     *reserveReq  `json:"reserve,omitempty"`
    Release     *releaseReq  `json:"release,omitempty"`
}

type sendReq struct {
    TaskID      string `json:"task_id,omitempty"`
    Stream      string `json:"stream"`
    Recipient   string `json:"recipient"`
    Kind        string `json:"kind"`
    Priority    int    `json:"priority"`
    AckRequired bool   `json:"ack_required"`
    Subject     string `json:"subject"`
    Body        string `json:"body"`
}

type inboxReq struct {
    TaskID     string `json:"task_id,omitempty"`
    Stream     string `json:"stream,omitempty"`
    OnlyUnread bool   `json:"only_unread,omitempty"`
    Limit      int    `json:"limit,omitempty"`
}

type ackReq struct {
    IDs []string `json:"ids"`
}

type reserveReq struct {
    Paths []string `json:"paths"`
    Mode  string   `json:"mode"`  // exclusive, shared
    TTLMS int      `json:"ttl_ms"` // milliseconds
}

type releaseReq struct {
    IDs   []string `json:"ids,omitempty"`
    Paths []string `json:"paths,omitempty"`
}
```

#### Operations

| Operation | Description                                                                     |
| --------- | ------------------------------------------------------------------------------- |
| `send`    | Create a new message. Sender inferred from `AGENTCTL_AGENT_NAME` env or config. |
| `inbox`   | List messages for the current actor.                                            |
| `ack`     | Acknowledge messages by ID.                                                     |
| `reserve` | Request advisory locks on file paths.                                           |
| `release` | Release reservations by ID or path.                                             |

#### skill.yaml

```yaml
name: mailbox/manage
version: "1.0.0"
description: Inter-agent mailbox and file reservation management
runner: exec
network: none

operations:
    - name: send
      description: Send a message to another actor
    - name: inbox
      description: List inbox messages
    - name: ack
      description: Acknowledge messages
    - name: reserve
      description: Reserve file paths (advisory lock)
    - name: release
      description: Release file reservations
```

---

## Phase 3: Hook Skills

### 3.1 `hooks/mail_router`

**Location:** `skills/hooks_mail_router/`

**Purpose:** Inject relevant mailbox messages into Claude context on
`PreToolUse` and `UserPromptSubmit` events.

#### Behavior

1. **Input:** `hook.Input` with `event`, `tool_name`, `input_preview`,
   `session_id`.
2. **Resolve context:**
   - `workspace_id` from `AGENTCTL_WORKSPACE` or config.
   - `actor_id` from `AGENTCTL_AGENT_NAME` or default `"claude"`.
   - `task_id` from active task (call tasks store or use cached).
3. **Fetch inbox:**
   - `mailbox/manage.inbox` with `only_unread=true`, `limit=10`.
   - Optionally filter by `task_id`.
4. **Rank messages:**
   - Primary: `priority` (1 highest).
   - Secondary: sender role weight (`admin` = 10, `overseer` = 8, `agent` = 5).
   - Tertiary: `ack_required` = +3, recency.
5. **Build context:**
   ```
   ## Mailbox (3 unread)

   - **[URGENT] admin**: Review security implications before proceeding (ack required)
   - **[HIGH] overseer**: Task #01JD... is now unblocked
   - **[NORMAL] agent-2**: Completed dependency analysis
   ```
6. **Output:** `hook.Output` with `decision: "none"`, `context` string, `meta`
   with message IDs.

#### Output Schema

```go
type hookOutput struct {
    Decision string            `json:"decision"` // "none", "block", "modify"
    Context  string            `json:"context"`  // injected text
    Meta     map[string]any    `json:"meta"`     // message_ids, counts, etc.
}
```

---

### 3.2 `hooks/file_guard`

**Location:** `skills/hooks_file_guard/`

**Purpose:** Check file reservations before write operations.

#### Behavior

1. **Input:** `hook.Input` with `event: "PreToolUse"`, `tool_name`,
   `input_preview`.
2. **Extract paths:** Parse `input_preview` for file paths from
   Edit/Write/MultiEdit tools.
3. **Check reservations:**
   - Call `mailbox/manage.reserve` with `mode=exclusive`, `ttl=5m`.
   - If `conflicts` non-empty → reservation held by another actor.
4. **Decision:**
   - **Strict mode** (`AGENTCTL_FILE_GUARD_MODE=strict`):
     - `decision: "block"`, `reason: "Path X is reserved by agent-2 until ..."`
   - **Advisory mode** (default):
     - `decision: "none"`,
       `context: "⚠️ Warning: Path X is reserved by agent-2"`
     - Optionally send mailbox message to holder notifying of attempted access.
5. **On success:** Grant reservation for the requesting actor.

#### Path Extraction

```go
func extractWritePaths(toolName string, inputPreview json.RawMessage) ([]string, error) {
    switch toolName {
    case "Edit", "edit":
        var e struct{ FilePath string `json:"file_path"` }
        json.Unmarshal(inputPreview, &e)
        return []string{e.FilePath}, nil
    case "Write", "write_to_file":
        var w struct{ TargetFile string `json:"TargetFile"` }
        json.Unmarshal(inputPreview, &w)
        return []string{w.TargetFile}, nil
    case "MultiEdit", "multi_edit":
        var m struct{ FilePath string `json:"file_path"` }
        json.Unmarshal(inputPreview, &m)
        return []string{m.FilePath}, nil
    default:
        return nil, nil // Not a write tool
    }
}
```

---

### 3.3 Hook Chain Order

Update `.claude/settings.local.json` or hook registration:

```json
{
	"hooks": {
		"PreToolUse": [
			"hooks_task_guard",
			"hooks_file_guard",
			"hooks_mail_router",
			"hooks_knowledge_router",
			"hooks_test_feedback"
		],
		"UserPromptSubmit": [
			"hooks_mail_router"
		]
	}
}
```

---

## Phase 4: Overseer & Viewer

### 4.1 Task Scoring Formula

Combine graph metrics and mailbox state:

```
task_score = 
    α * normalized(critical_path_score) +
    β * normalized(pagerank) +
    γ * unread_admin_count * 10 +
    δ * unread_overseer_count * 5 +
    ε * recency_factor
```

Where:

- `α = 0.3`, `β = 0.2`, `γ = 0.25`, `δ = 0.15`, `ε = 0.1`
- `recency_factor = 1 / (1 + days_since_last_update)`

### 4.2 New Operation: `mailbox/manage.stats`

```go
type statsReq struct {
    GroupBy string `json:"group_by"` // "task", "sender", "stream"
}

// Output
type statsOutput struct {
    TotalMessages   int                       `json:"total_messages"`
    UnreadCount     int                       `json:"unread_count"`
    ByTask          map[string]messageStats   `json:"by_task,omitempty"`
    BySender        map[string]messageStats   `json:"by_sender,omitempty"`
    ByStream        map[string]messageStats   `json:"by_stream,omitempty"`
    ActiveReservations int                    `json:"active_reservations"`
}

type messageStats struct {
    Total    int `json:"total"`
    Unread   int `json:"unread"`
    Urgent   int `json:"urgent"`   // priority 1-2
    AckPending int `json:"ack_pending"`
}
```

### 4.3 CLI: `agentctl todo recommend`

```go
func newTodoRecommendCommand() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "recommend",
        Short: "Recommend next tasks based on graph + mailbox state",
        RunE: func(cmd *cobra.Command, _ []string) error {
            // 1. Get graph insights
            // 2. Get mailbox stats
            // 3. Compute scores
            // 4. Print ranked list
        },
    }
    return cmd
}
```

Output format:

```
# Task Recommendations

| Rank | Task ID     | Title                  | Score | Critical Path | Unread |
|------|-------------|------------------------|-------|---------------|--------|
| 1    | 01JDXYZ...  | Implement auth flow    | 0.87  | 5             | 2 (1🔴)|
| 2    | 01JDABC...  | Fix database migration | 0.72  | 3             | 1      |
| 3    | 01JDDEF...  | Update documentation   | 0.45  | 1             | 0      |
```

---

## Phase 5: Memory Integration

### 5.1 Memory-Graph Scoring

When ranking memory entries for context injection, incorporate task graph:

```
memory_score =
    w1 * semantic_similarity(query, embedding) +
    w2 * task_graph_weight(memory.task_id) +
    w3 * recency(memory.created_at) +
    w4 * kind_weight(memory.kind)
```

Where `task_graph_weight` = normalized PageRank or critical-path score of the
associated task.

### 5.2 `hooks/context_inject` Enhancement

Update to pull task graph context:

1. Identify active task and its neighborhood (deps + dependents).
2. Fetch memories associated with neighborhood tasks.
3. Boost memories from high-impact tasks.
4. Include task graph summary in context when cycles detected.

---

## Phase 6: Optional Bridges (Future)

### 6.1 Beads Bridge

Config:

```yaml
todo:
    backend: beads # or "local" (default)
    beads_path: ~/.beads
```

Shim layer maps `bd issue` operations to `todo/manage` interface.

### 6.2 MCP Agent Mail Bridge

For multi-repo scenarios:

- Call `mcp_agent_mail` tools via `http/openapi` skill.
- Sync local mailbox ↔ remote.
- Use remote reservations for cross-workspace coordination.

---

## Testing Matrix

| Component                      | Test Type   | Coverage                                   |
| ------------------------------ | ----------- | ------------------------------------------ |
| `tasksgraph.Analyzer`          | Unit        | Empty, chain, fork-join, cycle graphs      |
| `todo/manage.graph_insights`   | Integration | Golden envelope outputs                    |
| `BlackboardStore.AddMessage`   | Unit        | Insert, duplicate handling                 |
| `BlackboardStore.ReservePaths` | Unit        | Grant, conflict, expiry                    |
| `mailbox/manage` skill         | Integration | Send/inbox roundtrip, ack, reserve/release |
| `hooks_mail_router`            | Golden      | Fixture inputs → expected context          |
| `hooks_file_guard`             | Unit        | With/without conflicts, strict/advisory    |

---

## File Inventory

### New Files

```
internal/
├── analysis/
│   └── tasksgraph/
│       ├── graph.go
│       └── graph_test.go
├── domain/
│   └── mailbox/
│       └── types.go
└── storage/
    └── mailbox/
        ├── blackboard.go      # new store implementation
        └── blackboard_test.go

skills/
├── mailbox/
│   ├── main.go
│   ├── main_test.go
│   └── skill.yaml
├── hooks_mail_router/
│   ├── main.go
│   ├── main_test.go
│   └── skill.yaml
└── hooks_file_guard/
    ├── main.go
    ├── main_test.go
    └── skill.yaml

cmd/agentctl/cmd/
└── todo.go                    # extend with insights, recommend
```

### Modified Files

```
skills/todo/main.go            # add graph_insights operation
go.mod                         # add gonum/graph dependency
```

---

## Implementation Order

1. **Phase 1.1** – `internal/analysis/tasksgraph` package with tests.
2. **Phase 1.2** – `todo/manage.graph_insights` operation.
3. **Phase 1.3** – `agentctl todo insights` CLI command.
4. **Phase 2.1-2.3** – Domain types + storage interface + schema.
5. **Phase 2.4** – Reservation logic.
6. **Phase 2.5** – `mailbox/manage` skill.
7. **Phase 3.1** – `hooks_mail_router` skill.
8. **Phase 3.2** – `hooks_file_guard` skill.
9. **Phase 4** – Overseer scoring + `todo recommend` CLI.
10. **Phase 5** – Memory integration (follow-up).

---

## Success Criteria

- [ ] `agentctl todo insights` returns valid graph metrics for workspaces with
      tasks.
- [ ] `mailbox/manage send` + `inbox` roundtrip works.
- [ ] `reserve` correctly detects conflicts.
- [ ] `hooks_mail_router` injects context on PreToolUse.
- [ ] `hooks_file_guard` blocks/warns on reserved paths.
- [ ] All tests pass with `-race`.
- [ ] No CGO dependencies.
- [ ] JSON envelope contract unchanged.
