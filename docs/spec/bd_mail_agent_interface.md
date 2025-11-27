I’ll move into implementation design: outline Go packages and interfaces for the
task graph analyzer, mailbox storage/APIs, and hook/overseer integration, and
set up concrete implementation-phase TODOs.

I’ll sketch an implementation plan: packages and interfaces for (1) the task
graph analyzer and `todo/manage.graph_insights`, (2) mailbox storage + skills,
and (3) mail-aware hooks. I want to align this with the existing `todo/manage`
and task storage layout, so I’ll quickly scan for the current todo skill
implementation to match patterns before proposing concrete Go package shapes.

Good call. Let’s move into concrete implementation design and keep it
incremental.

I’ll break this into three implementation tracks that line up with the specs and
current code:

- **(1) Task graph analyzer + `todo/manage.graph_insights`**
- **(2) Mailbox storage + `mailbox/manage` skill**
- **(3) Hooks: `mail_router` + `file_guard`**

---

## 1. Task graph analyzer + `todo/manage.graph_insights`

### 1.1 New package: `internal/analysis/tasksgraph`

**Package shape:**

- `internal/analysis/tasksgraph/graph.go`:

  ```go
  package tasksgraph

  import "github.com/jkatigb/agentctl/internal/storage/tasks"

  type NodeMetrics struct {
      TaskID             string
      PageRank           float64
      CriticalPathScore  float64
      InDegree           int
      OutDegree          int
  }

  type Insights struct {
      WorkspaceID      string
      GeneratedAt      time.Time
      Nodes            []NodeMetrics
      TopologicalOrder []string
      Cycles           [][]string
  }

  type Analyzer interface {
      Analyze(tasks []tasks.Task, workspaceID string) (Insights, error)
  }

  func NewAnalyzer() Analyzer { /* gonum-backed impl */ }
  ```

- Implementation uses **gonum/graph/simple** internally:
  - Build `simple.DirectedGraph`.
  - Add node per task.
  - Add edges `u -> v` for `DependsOn`.
  - Run:
    - PageRank.
    - In/Out-degree by inspecting edges.
    - Critical-path via longest-path-on-DAG (or DAGified approximation; cycles
      flagged separately).
    - Topological sort if DAG.
    - Cycle detection via DFS or existing gonum helpers.

**Tests:**

- `graph_test.go`:
  - Build small synthetic task sets:
    - Simple chain A→B→C.
    - Fork/join shapes.
    - A cycle A→B→A.
  - Assert:
    - Topo order for DAGs.
    - Cycles detected.
    - Reasonable rank / critical-path ordering (just relative, not exact
      numbers).

### 1.2 Integrate into `skills/todo` (`todo/manage`)

You already have `skills/todo/main.go`. We extend:

- **Input struct**:

  ```go
  type input struct {
      Operation    string           `json:"operation"`
      WorkspaceID  string           `json:"workspace_id"`
      // existing fields…
      // no extra sub-struct needed for v1
  }
  ```

- **New operation switch case** in `run`:

  ```go
  case "graph_insights":
      allTasks, err := store.ListByWorkspace(ctx, workspaceID)
      if err != nil { return err }

      analyzer := tasksgraph.NewAnalyzer()
      insights, err := analyzer.Analyze(allTasks, workspaceID)
      if err != nil { return err }

      data = map[string]any{
          "workspace_id": insights.WorkspaceID,
          "generated_at": insights.GeneratedAt.UTC().Format(time.RFC3339Nano),
          "nodes":        insightsToJSON(insights),
          "topological_order": insights.TopologicalOrder,
          "cycles":            insights.Cycles,
      }
  ```

- **Helper** `insightsToJSON` converts `[]NodeMetrics` into the JSON shape from
  the spec.

- **Update `skill.yaml`**:

  - Extend `operation` description to include `graph_insights`.
  - Add return fields `nodes`, `topological_order`, `cycles` as non-breaking
    additions.

### 1.3 CLI: `agentctl todo insights`

In `cmd/agentctl/cmd/todo.go`:

- Add a new subcommand:

  ```go
  func newTodoInsightsCommand() *cobra.Command {
      var workspaceID string
      cmd := &cobra.Command{
          Use:   "insights",
          Short: "Show graph-based insights for tasks in a workspace",
          RunE: func(cmd *cobra.Command, _ []string) error {
              payload := map[string]any{
                  "operation": "graph_insights",
              }
              if workspaceID != "" {
                  payload["workspace_id"] = workspaceID
              }
              return runTodoSkill(cmd, payload)
          },
      }
      cmd.Flags().StringVar(&workspaceID, "workspace", "", "Workspace ID (default: cwd)")
      return cmd
  }
  ```

- Wire into `newTodoCommand()`:

  ```go
  cmd.AddCommand(
      newTodoAddCommand(),
      newTodoCompleteCommand(),
      newTodoListCommand(),
      newTodoActiveCommand(),
      newTodoInsightsCommand(),
  )
  ```

**Tests:**

- Extend `skills/todo/main_test.go`:
  - After creating a small dependency graph, call `operation="graph_insights"`.
  - Decode `nodes`, check:
    - Non-empty.
    - Node IDs match tasks.
    - Critical-path scores monotone along a simple chain.

---

## 2. Mailbox storage + `mailbox/manage` skill

### 2.1 New storage package: `internal/storage/mailbox`

**Types (from spec):**

- `Message` and `Reservation` matching
  [mailbox_blackboard.md](cci:7://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/docs/spec/mailbox_blackboard.md:0:0-0:0).

- [Store](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:18:0-38:1)
  interface:

  ```go
  type Store interface {
      Close() error

      AddMessage(ctx context.Context, m Message) (Message, error)
      ListInbox(ctx context.Context, workspaceID, actorID string, f InboxFilter) ([]Message, error)
      AckMessages(ctx context.Context, workspaceID, actorID string, ids []string) error

      ReservePaths(ctx context.Context, workspaceID, actorID string, paths []string, mode string, ttl time.Duration) (granted []Reservation, conflicts []Reservation, err error)
      ReleaseReservations(ctx context.Context, workspaceID string, ids []string, actorID string, paths []string) error
  }

  type InboxFilter struct {
      TaskID     string
      Stream     string
      OnlyUnread bool
      Since      time.Time
  }
  ```

- [sqlStore](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:63:0-65:1)
  implementation:
  - New DB `mailbox.db` under `cfg.Storage.Root`.
  - Schema as in spec:
    - `mail_messages` table.
    - `file_reservations` table.
  - Migration helper patterned after `internal/storage/tasks`.

### 2.2 New skill: `skills/mailbox`

Single skill `mailbox/manage` with `operation` field, mirroring todo/manage.

- **Input struct**:

  ```go
  type input struct {
      Operation   string          `json:"operation"`
      WorkspaceID string          `json:"workspace_id"`
      Send        *sendReq        `json:"send"`
      Inbox       *inboxReq       `json:"inbox"`
      Ack         *ackReq         `json:"ack"`
      Reserve     *reserveReq     `json:"reserve"`
      Release     *releaseReq     `json:"release"`
  }
  ```

- **Operations**:

  - `"send"` → `mailbox/send` semantics.
  - `"inbox"` → `mailbox/inbox`.
  - `"ack"` → `mailbox/ack`.
  - `"reserve"` → `mailbox/reserve`.
  - `"release"` → `mailbox/release`.

- **Skill wiring:**

  - Use `runner.NewRunnerContext` + `config.Load` identical to todo skill.
  - Open
    [mailbox.Store](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:18:0-38:1)
    via
    [internal/storage/mailbox.Open](cci:1://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:67:0-75:1).

- **`skill.yaml`**:

  - Name: `mailbox/manage`.
  - IO: JSON, `network: "none"`, `filesystem: workdir`.
  - Signature:
    - `operation` param.
    - `workspace_id`.
    - Optional operation-specific params described in comments / additional
      fields.

**Tests:**

- `skills/mailbox/main_test.go`:
  - Send + inbox round-trip.
  - Ack transitions `status`.
  - Reserve with/without conflicts.
  - Release reservations.

---

## 3. Hooks: `mail_router` + `file_guard`

This is mostly about **skills that follow the hook.Input/Output contract**
described in
[task_hooks_memory.md](cci:7://file:///Users/jkatigbak/repos/personal/agentctl/docs/spec/task_hooks_memory.md:0:0-0:0) +
your new specs.

### 3.1 `hooks/mail_router` skill

**Location:**

- `skills/hooks/mail_router/`.

**Behavior:**

- Input: `hook.Input` JSON (per existing hook spec).
  - Read `workspace_id`, `actor_id`, `task_id`, event type (`PreToolUse` /
    `UserPromptSubmit`).
- Logic:
  1. Open config + runner context.
  2. Call `mailbox/manage` with operation `"inbox"`:
     - `actor_id`, `workspace_id`, `task_id`, `only_unread=true`.
  3. Rank messages in-process:
     - Priority, sender weight (admin/overseer), recency.
  4. Construct `hook.Output`:
     - `Decision: "none"`.
     - `Context`: small, human-readable bullets of top N messages.
     - `Meta`: full list of message IDs + senders + priorities.
  5. Optionally call `"ack"` (configurable) for messages that were surfaced.

**Tests:**

- Golden tests for small hook.Input → hook.Output mappings (fixtures in
  `testdata/`).

### 3.2 `hooks/file_guard` skill

**Location:**

- `skills/hooks/file_guard/`.

**Behavior:**

- Input: `hook.Input` containing:
  - Proposed filesystem changes or target paths (per hook spec).
  - `workspace_id`, `actor_id`.
- Logic:
  1. Determine list of paths that will be written.
  2. Call `mailbox/manage` with `"reserve"`:
     - `paths`, `mode`, `ttl`.
  3. If conflicts:
     - In strict mode:
       - Return `Decision: "block"` + message naming holder and paths.
     - In advisory:
       - Return `Decision: "none"` but add warnings to `Context`, and optionally
         send a mailbox message to conflicting actor.

**Tests:**

- Fake `mailbox` backend or temporary DB.
- Input with/without pre-existing reservations.

---

## 4. How this ties to overseer/admin/agent_viewer

With the above in place:

- Overseer agent can:
  - Call `todo/manage.graph_insights` + `mailbox/manage.inbox/stats`.
  - Compute scores and send admin/overseer messages via `mailbox/manage.send`.

- Agent viewer can:
  - Call `todo/manage.graph_insights` and `mailbox/manage.inbox` (and maybe a
    future `mailbox/stats` op).
  - Render the UIs described in your specs.

---

## 5. Next step

My suggestion for actual coding order:

1. **Task graph analyzer + `graph_insights` op** (smallest surface area, nicely
   self-contained).
2. **Mailbox storage + skill** (local only, no hook coupling yet).
3. **Hooks (`mail_router`, `file_guard`)** wired into the existing hook spec.

If you’d like, I can start with step 1 and draft the
`internal/analysis/tasksgraph` package plus the new `graph_insights` operation
in `skills/todo/main.go` (no wiring into CLI yet), or would you rather start on
the mailbox side first?
