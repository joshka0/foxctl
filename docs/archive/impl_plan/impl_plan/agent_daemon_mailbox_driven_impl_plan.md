### Mailbox-driven Agent Daemon – Implementation Plan

This plan implements an **always-on, mailbox-driven, fully agentic** Agent
Profile runtime for `agentctl`.

It is written as an implementation blueprint (deliverables + file touchpoints +
acceptance criteria) and is intended to be tracked via the project TODO list IDs
(`T1.*`, `T2.*`, `T3.*`, `D1.*`, etc.).

---

## 0. Scope & non-goals (contract guardrails)

### 0.1 In scope

- Replace stubbed `dspy-go` agent tools with store-backed implementations:
  - `mail.*` tools: coordination inbox + reservations via **BoardStore**.
  - `todo.*` tools: tasks persistence + graph insights via **tasks.Store**.
  - `bb.*` tools: coordination topic bus via **blackboard.Store**.
- Implement an **agent daemon** that:
  - Runs continuously.
  - Polls **mailbox.Store** for `agent.ask` / `agent.cmd` / `agent.event`
    messages.
  - Executes DSPy turns + tool calls.
  - Responds with `agent.reply` and acks mailbox messages.
  - Maintains agent state + heartbeats.
  - Is correct under **at-least-once** delivery (TTL + dedupe).
- Add CLI UX for:
  - Running the daemon (`agent run`).
  - Sending asks/commands (`agent ask`, `agent cmd`).
  - Waiting for replies (`agent ask --wait`).
- Add deterministic tests (no API keys) and update docs/specs to match
  implemented behavior.

### 0.2 Non-goals (explicitly deferred)

- Cluster/distributed orchestration (multi-host).
- Full WFQ scheduler + quotas enforcement.
- Full overseer supervision strategy (out-of-process supervisor, restart
  policies, PID tracking) beyond an MVP.
- Changing Core Profile v1 envelope shape or `meta.*` behavior.

### 0.3 Invariants / "MUST NOT break"

- **Envelope contract is sacred** (Core Profile v1): do not change envelope
  shape.
- **CAS and large output policy**: large outputs use `data.summary` +
  `data.artifact` as per Core rules.
- **Security**:
  - No secrets in logs or envelopes.
  - Validate untrusted input at boundaries (mailbox payloads, CLI args, tool
    inputs).
- **Correctness under at-least-once delivery**:
  - Consumers MUST tolerate duplicate mailbox messages.
  - TTL MUST be enforced by daemon dispatch.

---

## 0.4 Key type/interface reference (quick lookup)

### BoardStore interface (`internal/storage/blackboard/board_store.go`)

```go
type BoardStore interface {
    Close() error
    // Message operations
    SendMessage(ctx context.Context, msg *agent.BoardMessage) error
    Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error)
    MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
    AckMessages(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
    CountMessagesByTask(ctx context.Context, workspaceID, taskID string) (admin, overseer, total int, err error)
    // Reservation operations
    Reserve(ctx context.Context, res *agent.FileReservation) error
    CheckConflicts(ctx context.Context, workspaceID string, paths []string, holder string, mode agent.ReservationMode) ([]agent.ReservationConflict, error)
    Release(ctx context.Context, workspaceID, actorID string, paths []string) (int, error)
    ReleaseByID(ctx context.Context, reservationIDs []string) (int, error)
    ListReservations(ctx context.Context, workspaceID string) ([]agent.FileReservation, error)
}
```

### tasks.Store interface (`internal/storage/tasks/store.go`)

```go
type Store interface {
    Close() error
    Add(ctx context.Context, t Task) (Task, error)
    Update(ctx context.Context, t Task) (Task, error)
    Get(ctx context.Context, id string) (Task, error)
    ListByWorkspace(ctx context.Context, workspaceID string) ([]Task, error)
    GetActive(ctx context.Context, workspaceID string) (Task, bool, error)
    SetActive(ctx context.Context, workspaceID, taskID string) (Task, error)
    ClearActive(ctx context.Context, workspaceID string) error
    EnsureActive(ctx context.Context, workspaceID, defaultTitle, scopePath string) (Task, bool, error)
    DirtyIfReviewed(ctx context.Context, taskID string) (Task, bool, error)
}
```

### blackboard.Store interface (`internal/storage/blackboard/store.go`)

```go
type Store interface {
    Close() error
    Post(ctx context.Context, record agent.BlackboardRecord) error
    Get(ctx context.Context, id string) (agent.BlackboardRecord, error)
    Search(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error)
    Claim(ctx context.Context, id, agentID string, leaseDuration time.Duration) (agent.BlackboardRecord, error)
    Release(ctx context.Context, id string) error
    Delete(ctx context.Context, id string) error
    ListByTopic(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error)
    Watch(ctx context.Context, ns, topic string, fromTS int64) (<-chan agent.BlackboardRecord, <-chan error)
}
```

### mailbox.Store interface (`internal/storage/mailbox/store.go`)

```go
type Store interface {
    Close() error
    Send(ctx context.Context, msg agent.Message) error
    Poll(ctx context.Context, agentNS string, timeout time.Duration, maxMessages int) ([]agent.Message, error)
    Ack(ctx context.Context, messageID string) error
    Nack(ctx context.Context, messageID string, visibilityTimeout time.Duration) error
    List(ctx context.Context, agentNS string, limit int) ([]agent.Message, error)
    Delete(ctx context.Context, messageID string) error
}
```

### Key domain types

```go
// agent.BoardMessage (internal/domain/agent/board_message.go)
type BoardMessage struct {
    ID, WorkspaceID, TaskID, Stream, Sender, Recipient string
    Kind        BoardMessageKind   // instruction|info|alert|review_request
    Priority    int                // 1 (highest) .. 5 (lowest)
    AckRequired bool
    Status      BoardMessageStatus // unread|read|acked
    Subject, Body string
    CreatedAt   time.Time
}

// agent.FileReservation (internal/domain/agent/board_message.go)
type FileReservation struct {
    ID, WorkspaceID, TaskID, Path, Holder string
    Mode      ReservationMode // exclusive|shared
    Reason    string
    ExpiresAt time.Time
    CreatedAt time.Time
}

// agent.Message (internal/domain/agent/mailbox.go) — for daemon dispatch
type Message struct {
    ID, FromNS, ToNS string
    Type      MessageType       // agent.ask|agent.reply|agent.cmd|agent.event
    TTLMS     int64
    Headers   map[string]string // includes "correlation" for ask↔reply
    Payload   json.RawMessage   // envelope JSON
    VisibleAt int64
    Attempt   int
    Timestamp int64
}

// tasks.Task (internal/storage/tasks/store.go)
type Task struct {
    ID, WorkspaceID, Title, Description, ScopePath, ParentID string
    Children, DependsOn []string
    Status      string // pending|in_progress|ready_for_review|completed|blocked|canceled
    CreatedAt   time.Time
    CompletedAt *time.Time
    Notes, Gotchas string
    LastReviewStatus, LastReviewID string
    LastReviewAt *time.Time
}

// tasksgraph.Insights (internal/intelligence/analysis/tasksgraph/graph.go)
type Insights struct {
    WorkspaceID      string
    GeneratedAt      time.Time
    Nodes            []NodeMetrics
    TopologicalOrder []string
    Cycles           [][]string
}
```

---

## 1. Canonical docs (context links)

### 1.1 Source-of-truth specs

- `docs/agent_profile.md`
- `docs/spec/mailbox_blackboard.md`
- `docs/spec/task_graph_mailbox_implementation.md`

### 1.2 Core runtime contracts

- `docs/spec/core_profile_v1.md`
- `docs/spec/protocol_v1.md`
- `docs/spec/dspy_trajectory_capture.md`

---

## 2. Current code touchpoints (for implementers)

### 2.1 Tool layer (DSPy agent tools)

- `internal/agent/tools/tools.go` (registry + telemetry wrapper)
- `internal/agent/tools/mail_tools.go` (**currently stubbed**)
- `internal/agent/tools/todo_tools.go` (**currently stubbed**)
- `internal/agent/tools/code_tools.go` (`code.search`, `code.swe_grep`,
  `code.symbol_search`)
- `internal/agent/tools/edit_tools.go`, `fs_tools.go`, `test_tools.go` (already
  real)

### 2.2 Persistence (already implemented)

- Mailbox (ask/reply/cmd/event queue):
  - `internal/storage/mailbox/store.go`
- Agent records:
  - `internal/storage/agents/*`
- Tasks:
  - `internal/storage/tasks/store.go`
- Blackboard (topic bus):
  - `internal/storage/blackboard/store.go`
- BoardStore (inbox-style messages + file reservations):
  - `internal/storage/blackboard/board_store.go`

### 2.3 Runtime / orchestration

- DSPy session runtime (one-shot today):
  - `internal/agent/runtime/runtime.go`
- Spawn/overseer support:
  - `internal/agent/runtime/overseer.go`

### 2.4 CLI

- Native agent lifecycle commands:
  - `cmd/agentctl/cmd/agent.go`
- Mailbox store CLI:
  - `cmd/agentctl/cmd/mailbox.go`
- Blackboard store CLI:
  - `cmd/agentctl/cmd/bb.go`
- Todo skill CLI wrappers:
  - `cmd/agentctl/cmd/todo.go`

### 2.5 Skills (reference semantics)

These are used as a behavioral reference, but the agent tool layer should prefer
**direct store calls** for determinism.

- `skills/mailbox/main.go` (`mailbox/manage` semantics; BoardStore)
- `skills/todo/main.go` (`todo/manage` semantics; tasks store + tasksgraph)
- Hooks:
  - `skills/hooks_mail_router/main.go`
  - `skills/hooks_file_guard/main.go`

---

## 3. MVP architecture (what calls what)

### 3.1 Three distinct coordination surfaces (do not conflate)

- **Mailbox store** (`internal/storage/mailbox`)
  - Purpose: agent-to-agent “RPC queue” for `agent.ask` / `agent.reply` /
    `agent.cmd` / `agent.event`.
  - Used by: **daemon dispatch** and CLI `agent ask --wait`.

- **BoardStore** (`internal/storage/blackboard/board_store`)
  - Purpose: inbox-like admin/human/overseer messages + file reservations.
  - Used by: `mail.*` agent tools.

- **Blackboard store** (`internal/storage/blackboard/store`)
  - Purpose: topic bus with TTL + leases (queue/work-claim patterns).
  - Used by: `bb.*` agent tools.

### 3.2 Dataflow (MVP)

1. Human/admin sends `agent.ask` → stored in mailbox store.
2. Daemon polls mailbox store → runs a DSPy turn.
3. DSPy turn invokes tools:
   - reads code/files/tests (`fs.*`, `code.*`, `tests.*`, `edit.*`)
   - coordinates/reserves via `mail.*` (BoardStore)
   - posts/claims work via `bb.*` (blackboard topic bus)
   - persists plan progress via `todo.*` (tasks store)
4. Daemon sends `agent.reply` → stored in mailbox store.
5. CLI `agent ask --wait` polls for correlated reply → prints envelope.

---

## 4. Milestones and detailed work breakdown

Each section below includes:

- **Deliverables** (what code/doc/test changes exist at the end)
- **Touchpoints** (specific files/packages)
- **Acceptance criteria** (observable behavior)

### Milestone A — Replace stubbed agent tools (unblocks "fully agentic")

#### A1) `mail.*` tools via BoardStore

The `mail.*` tools use **BoardStore**
(`internal/storage/blackboard/board_store.go`) for workspace-scoped coordination
messages and file reservations. This is **distinct** from the daemon's mailbox
store (which handles `agent.ask/reply`).

##### T1.0 — Implement `mail.send`

- **Current state**: Stub in `mail_tools.go:124-181` returns placeholder data.
- **Target behavior**: Persist a real `agent.BoardMessage` via BoardStore.

**Implementation steps**:

1. Add a `boardStore blackboard.BoardStore` field to `tools.Registry` (or open
   it on-demand using `r.config.WorkspaceID` as root).
2. In `mailSend()`, construct an `agent.BoardMessage`:
   ```go
   msg := &agent.BoardMessage{
       WorkspaceID: r.config.WorkspaceID,
       TaskID:      r.config.TaskID,      // optional context
       Stream:      agent.DefaultStream,  // "coordination"
       Sender:      r.config.ActorID,     // e.g. "actor:agent:coder-1"
       Recipient:   recipient,            // from args
       Kind:        agent.BoardMessageKind(kind), // instruction|info|alert|review_request
       Priority:    priority,             // 1-5, default 3
       AckRequired: ackRequired,
       Subject:     subject,
       Body:        body,
   }
   ```
3. Call `boardStore.SendMessage(ctx, msg)`.
4. Return `{"message_id": msg.ID, "sent_to": recipient, "success": true}`.

**Team fan-out (optional)**:

- If `recipient` starts with `"team:"`, look up team members via
  `internal/storage/teams` and call `SendMessage` for each member.

- **Acceptance criteria**:
  - `boardStore.Inbox(ctx, filter)` returns the sent message for the recipient.

##### T1.1 — Implement `mail.inbox`

- **Current state**: Stub in `mail_tools.go:183-224` returns placeholder array.
- **Target behavior**: Query real messages via `BoardStore.Inbox`.

**Implementation steps**:

1. Build an `agent.InboxFilter`:
   ```go
   filter := agent.InboxFilter{
       WorkspaceID: r.config.WorkspaceID,
       ActorID:     r.config.ActorID,
       TaskID:      taskID,       // optional from args
       Stream:      stream,       // optional from args
       OnlyUnread:  unreadOnly,   // default true
       Limit:       limit,        // default 20
   }
   ```
2. Call `messages, err := boardStore.Inbox(ctx, filter)`.
3. Optionally mark messages as read:
   ```go
   if markRead && len(messages) > 0 {
       ids := make([]string, len(messages))
       for i, m := range messages { ids[i] = m.ID }
       boardStore.MarkRead(ctx, r.config.WorkspaceID, r.config.ActorID, ids)
   }
   ```
4. Return `{"messages": messages, "count": len(messages)}`.

- **Acceptance criteria**:
  - `only_unread=true` excludes read/acked messages.
  - After retrieval with `mark_read=true`, messages transition to `read` status.

##### T1.2 — Implement `mail.ack`

- **Current state**: Stub in `mail_tools.go:226-251` returns placeholder.
- **Target behavior**: Transition message status to `acked`.

**Implementation steps**:

1. Extract `message_id` (or `message_ids[]`) from args.
2. Call:
   ```go
   count, err := boardStore.AckMessages(ctx, r.config.WorkspaceID, r.config.ActorID, []string{messageID})
   ```
3. Return `{"message_id": messageID, "acknowledged": true, "count": count}`.

- **Acceptance criteria**:
  - Acked messages have `status = "acked"` and no longer appear in unread inbox.

##### T1.3 — Add `mail.reserve`

- **Current state**: Tool does not exist.
- **Target behavior**: Create file reservations via `BoardStore.Reserve`.

**Implementation steps**:

1. Register new tool `mail.reserve` with schema:
   ```json
   {
     "paths": ["string array", "required"],
     "mode": "exclusive|shared (default exclusive)",
     "ttl_seconds": "int (default 600 = 10 min)",
     "reason": "string (optional, defaults to active task title)"
   }
   ```
2. For each path, construct an `agent.FileReservation`:
   ```go
   res := &agent.FileReservation{
       WorkspaceID: r.config.WorkspaceID,
       TaskID:      r.config.TaskID,
       Path:        path,
       Holder:      r.config.ActorID,
       Mode:        agent.ReservationMode(mode),
       Reason:      reason,
       ExpiresAt:   time.Now().Add(time.Duration(ttlSeconds) * time.Second),
   }
   ```
3. Check for conflicts first:
   ```go
   conflicts, _ := boardStore.CheckConflicts(ctx, workspaceID, paths, holder, mode)
   if len(conflicts) > 0 {
       return errorResult("reservation conflict: ...")
   }
   ```
4. Call `boardStore.Reserve(ctx, res)` for each path.
5. Return `{"reserved": paths, "expires_at": res.ExpiresAt, "success": true}`.

- **Acceptance criteria**:
  - `hooks/file_guard` (or direct `CheckConflicts`) detects the reservation.
  - Exclusive reservations block other exclusive requests for the same path.

##### T1.4 — Add `mail.release`

- **Current state**: Tool does not exist.
- **Target behavior**: Release file reservations.

**Implementation steps**:

1. Register new tool `mail.release` with schema:
   ```json
   {
     "paths": ["string array (release by path)"],
     "reservation_ids": ["string array (release by ID)"]
   }
   ```
2. If `paths` provided:
   ```go
   count, err := boardStore.Release(ctx, workspaceID, r.config.ActorID, paths)
   ```
3. If `reservation_ids` provided:
   ```go
   count, err := boardStore.ReleaseByID(ctx, reservationIDs)
   ```
4. Return `{"released_count": count, "success": true}`.

- **Acceptance criteria**:
  - After release, `CheckConflicts` no longer reports conflicts for those paths.

##### T1.5 — Add unit tests for `mail.*`

- **File**: `internal/agent/tools/mail_tools_test.go`
- **Setup**: Use `t.TempDir()` as storage root, open BoardStore.
- **Test cases**:
  - `TestMailSend_PersistsMessage`: send → inbox returns it.
  - `TestMailInbox_FiltersByUnread`: send 2, mark 1 read, inbox unread
    returns 1.
  - `TestMailAck_TransitionsStatus`: ack → status is "acked".
  - `TestMailReserve_CreatesReservation`: reserve → ListReservations returns it.
  - `TestMailReserve_DetectsConflict`: reserve exclusive → second reserve fails.
  - `TestMailRelease_RemovesReservation`: reserve → release → no conflicts.

---

#### A2) `todo.*` tools via tasks store

The `todo.*` tools use **tasks.Store** (`internal/storage/tasks/store.go`) for
task persistence and **tasksgraph.Analyzer** for graph insights.

##### T2.0 — Implement `todo.query`

- **Current state**: Stub in `todo_tools.go:130-163` returns placeholder.
- **Target behavior**: Query real tasks from sqlite.

**Implementation steps**:

1. Add a `tasksStore tasks.Store` field to `tools.Registry` (or open on-demand).
2. In `todoQuery()`:
   ```go
   allTasks, err := tasksStore.ListByWorkspace(ctx, r.config.WorkspaceID)
   activeTask, hasActive, _ := tasksStore.GetActive(ctx, r.config.WorkspaceID)
   ```
3. Apply filters from args:
   - `status`: filter by `t.Status == status` (or "all" for no filter)
   - `parent_id`: filter by `t.ParentID == parentID`
   - `limit`: truncate result
4. Return:
   ```go
   {"tasks": filteredTasks, "active_task": activeTask, "count": len(filteredTasks)}
   ```

- **Acceptance criteria**:
  - Result matches `agentctl todo list` output for the same workspace.

##### T2.1 — Implement `todo.add`

- **Current state**: Stub in `todo_tools.go:192-219` returns placeholder.
- **Target behavior**: Create a real task via `tasks.Store.Add`.

**Implementation steps**:

1. Construct a `tasks.Task`:
   ```go
   newTask := tasks.Task{
       WorkspaceID: r.config.WorkspaceID,
       Title:       title,       // required
       Description: description, // optional
       ScopePath:   scopePath,   // optional
       ParentID:    parentID,    // optional
       DependsOn:   dependsOn,   // optional []string
       Status:      tasks.StatusPending,
   }
   ```
2. Call `created, err := tasksStore.Add(ctx, newTask)`.
3. If `parentID` is set, update the parent's `Children` array:
   ```go
   parent, _ := tasksStore.Get(ctx, parentID)
   parent.Children = append(parent.Children, created.ID)
   tasksStore.Update(ctx, parent)
   ```
4. Return `{"task": created, "success": true}`.

- **Acceptance criteria**:
  - Task appears in subsequent `todo.query` and `agentctl todo list`.

##### T2.2 — Implement `todo.complete`

- **Current state**: Stub in `todo_tools.go:221-243` returns placeholder.
- **Target behavior**: Update task status to `completed`.

**Implementation steps**:

1. Fetch the task: `task, err := tasksStore.Get(ctx, taskID)`.
2. Update fields:
   ```go
   now := time.Now().UTC()
   task.Status = tasks.StatusCompleted
   task.CompletedAt = &now
   if summary != "" {
       task.Notes = task.Notes + "\n\nCompletion summary: " + summary
   }
   ```
3. Call `updated, err := tasksStore.Update(ctx, task)`.
4. Return `{"task": updated, "success": true}`.

- **Idempotency**: If already completed, return success without error.

- **Acceptance criteria**:
  - Task status is `completed` and `completed_at` is set.

##### T2.3 — Implement `todo.graph_insights`

- **Current state**: Stub in `todo_tools.go:165-190` returns placeholder.
- **Target behavior**: Compute real graph metrics via `tasksgraph.Analyzer`.

**Implementation steps**:

1. Fetch all tasks:
   ```go
   allTasks, _ := tasksStore.ListByWorkspace(ctx, r.config.WorkspaceID)
   ```
2. Create analyzer and compute:
   ```go
   analyzer := tasksgraph.NewAnalyzer()
   insights, err := analyzer.Analyze(allTasks, r.config.WorkspaceID)
   ```
3. Return the `Insights` struct (or a subset based on `insight_type` arg):
   ```go
   {
     "workspace_id": insights.WorkspaceID,
     "generated_at": insights.GeneratedAt,
     "nodes": insights.Nodes,           // []NodeMetrics with PageRank, CriticalPathScore, etc.
     "topological_order": insights.TopologicalOrder,
     "cycles": insights.Cycles,
   }
   ```

- **Acceptance criteria**:
  - Output is deterministic for a fixed task graph.
  - Matches `agentctl todo insights` output.

##### T2.4 — Implement `todo.set_active`

- **Current state**: Tool not registered.
- **Target behavior**: Set the active task for the workspace.

**Implementation steps**:

1. Register tool with schema: `{"task_id": "string, required"}`.
2. Call `task, err := tasksStore.SetActive(ctx, r.config.WorkspaceID, taskID)`.
3. Return `{"active_task": task, "success": true}`.

##### T2.5 — Implement `todo.ensure_active`

- **Current state**: Tool not registered.
- **Target behavior**: Get or create an active task.

**Implementation steps**:

1. Register tool with schema:
   ```json
   {
     "default_title": "string (used if creating)",
     "scope_path": "string (optional)"
   }
   ```
2. Call:
   ```go
   task, created, err := tasksStore.EnsureActive(ctx, workspaceID, defaultTitle, scopePath)
   ```
3. Return `{"active_task": task, "created": created, "success": true}`.

##### T2.6 — Add unit tests for `todo.*`

- **File**: `internal/agent/tools/todo_tools_test.go`
- **Setup**: Use `t.TempDir()` as storage root, open tasks.Store.
- **Test cases**:
  - `TestTodoAdd_CreatesTask`: add → query returns it.
  - `TestTodoComplete_UpdatesStatus`: add → complete → status is "completed".
  - `TestTodoQuery_FiltersStatus`: add 2 with different status → filter works.
  - `TestTodoSetActive_PersistsSelection`: set_active → get_active returns it.
  - `TestTodoEnsureActive_CreatesIfMissing`: ensure_active when none → creates.
  - `TestTodoGraphInsights_ComputesMetrics`: add tasks with deps → insights has
    nodes.

---

#### A3) `bb.*` tools via blackboard topic bus

The `bb.*` tools use **blackboard.Store**
(`internal/storage/blackboard/store.go`) for topic-based coordination with TTL
and lease-based claiming.

##### T3.0 — Add `bb.post`

- **Target behavior**: Post a record to a topic.

**Implementation steps**:

1. Create new file `internal/agent/tools/bb_tools.go`.
2. Register tool with schema:
   ```json
   {
     "topic": "string, required",
     "payload": "object, required",
     "ttl_seconds": "int (default 3600 = 1 hour)",
     "cas_ref": "string (optional, for large payloads)"
   }
   ```
3. Construct record:
   ```go
   record := agent.BlackboardRecord{
       ID:     ulid.Make().String(),
       NS:     r.config.WorkspaceID,  // namespace = workspace
       Topic:  topic,
       TS:     time.Now().Unix(),
       TTLSec: ttlSeconds,
       Payload: payloadJSON,
       CASRef: casRef,
   }
   ```
4. Call `bbStore.Post(ctx, record)`.
5. Return `{"record_id": record.ID, "topic": topic, "success": true}`.

##### T3.1 — Add `bb.search`

- **Target behavior**: Search records by topic.

**Implementation steps**:

1. Register tool with schema:
   ```json
   {
     "topic": "string, required",
     "limit": "int (default 20)",
     "unleased_only": "bool (default false)"
   }
   ```
2. Call `records, _ := bbStore.Search(ctx, ns, topic, limit)`.
3. If `unleased_only`, filter out records where `record.Lease != nil`.
4. Return `{"records": records, "count": len(records)}`.

##### T3.2 — Add `bb.claim`

- **Target behavior**: Claim a record with a lease.

**Implementation steps**:

1. Register tool with schema:
   ```json
   {
     "record_id": "string, required",
     "lease_seconds": "int (default 300 = 5 min)"
   }
   ```
2. Call:
   ```go
   record, err := bbStore.Claim(ctx, recordID, r.config.ActorID, time.Duration(leaseSeconds)*time.Second)
   ```
3. Handle `ErrAlreadyLeased` → return error result with conflict info.
4. Return
   `{"record": record, "leased_until": record.Lease.Until, "success": true}`.

- **Idempotency**: If already leased by same agent, extend lease (or return
  success).

##### T3.3 — Add `bb.release`

- **Target behavior**: Release a claimed record.

**Implementation steps**:

1. Register tool with schema: `{"record_id": "string, required"}`.
2. Call `bbStore.Release(ctx, recordID)`.
3. Return `{"record_id": recordID, "released": true}`.

##### T3.4 — Add `bb.list`

- **Target behavior**: List records by topic (alias for search with defaults).

**Implementation steps**:

1. Register tool with schema: `{"topic": "string, required", "limit": "int"}`.
2. Call `bbStore.ListByTopic(ctx, ns, topic, limit)`.
3. Return `{"records": records, "count": len(records)}`.

##### T3.5 — Add `bb.watch` (MVP: polling)

- **Target behavior**: Watch for new records on a topic.

**Implementation steps (MVP)**:

1. Register tool with schema:
   ```json
   {
     "topic": "string, required",
     "since_ts": "int64 (unix timestamp, default 0)",
     "timeout_seconds": "int (default 30)"
   }
   ```
2. Use `bbStore.Watch(ctx, ns, topic, sinceTS)` which returns channels.
3. Collect records until timeout or context cancellation.
4. Return `{"records": collected, "last_ts": lastTS}`.

- **Note**: Full streaming (NDJSON) is deferred; MVP returns batch after
  timeout.

##### T3.6 — Add unit tests for `bb.*`

- **File**: `internal/agent/tools/bb_tools_test.go`
- **Test cases**:
  - `TestBBPost_CreatesRecord`: post → search returns it.
  - `TestBBClaim_AcquiresLease`: post → claim → record has lease.
  - `TestBBClaim_FailsIfAlreadyLeased`: claim by A → claim by B fails.
  - `TestBBRelease_ClearsLease`: claim → release → claim by B succeeds.
  - `TestBBSearch_FiltersUnleased`: post 2, claim 1 → unleased_only returns 1.

---

### Milestone B — Agent daemon MVP (mailbox-driven loop)

The daemon uses **mailbox.Store** (`internal/storage/mailbox/store.go`) to poll
for `agent.ask` / `agent.cmd` / `agent.event` messages, runs DSPy turns, and
sends `agent.reply` responses.

#### D1.0 — Create daemon package

- **Deliverables**
  - New package: `internal/agent/daemon/`
  - Files: `daemon.go`, `config.go`, `handlers.go`, `dedupe.go`
  - Entrypoint: `func Run(ctx context.Context, opts Options) error`

**Recommended structure**:

```go
// internal/agent/daemon/config.go
type Options struct {
    AgentID           string
    StorageRoot       string        // ~/.agentctl
    PollInterval      time.Duration // default 500ms
    HeartbeatInterval time.Duration // default 10s
    MaxPollMessages   int           // default 10
}

// internal/agent/daemon/daemon.go
func Run(ctx context.Context, opts Options) error {
    // 1. Open stores (agents, mailbox, tasks, boardStore, blackboard)
    // 2. Load agent record, validate state
    // 3. Transition state to "running"
    // 4. Initialize tool registry + DSPy runtime
    // 5. Start heartbeat ticker
    // 6. Enter poll loop
    // 7. On exit: transition state to "stopped" or "error"
}
```

- **Acceptance criteria**:
  - `agentctl agent run <id>` starts the daemon and blocks until stopped.

#### D1.1 — Daemon startup

**Implementation steps**:

1. Open stores:
   ```go
   agentStore, _ := agents.Open(ctx, opts.StorageRoot)
   mailboxStore, _ := mailbox.Open(ctx, opts.StorageRoot)
   tasksStore, _ := tasks.Open(ctx, opts.StorageRoot)
   boardStore, _ := blackboard.OpenBoardStore(ctx, opts.StorageRoot)
   bbStore, _ := blackboard.Open(ctx, opts.StorageRoot)
   ```

2. Load and validate agent:
   ```go
   agentRecord, err := agentStore.Get(ctx, opts.AgentID)
   if agentRecord.State == agent.StateStopped {
       return errors.New("agent is stopped")
   }
   ```

3. Transition to running:
   ```go
   agentStore.UpdateState(ctx, opts.AgentID, agent.StateRunning)
   ```

4. Build tool registry with allowlist filtering:
   ```go
   toolsCfg := tools.Config{
       WorkspaceRoot: agentRecord.Prompt, // or derive from config
       WorkspaceID:   agentRecord.Namespace,
       ActorID:       "actor:agent:" + opts.AgentID,
       // ... other fields
   }
   registry, _ := tools.NewRegistry(toolsCfg, telemetryRecorder)
   // TODO: filter tools by agentRecord.SkillsAllow
   ```

5. Initialize DSPy runtime (reuse patterns from `internal/agent/runtime`):
   ```go
   llm := initLLM(ctx, llmConfig)  // from runtime.go patterns
   agent := agents.NewReActAgent(llm, registry.Tools(), agentRecord.Prompt)
   ```

- **Acceptance criteria**:
  - Agent state is `running` after startup.
  - Heartbeat updates begin immediately.

#### D1.2 — Mailbox poll loop

**Implementation steps**:

```go
// internal/agent/daemon/daemon.go (inside Run)
pollTicker := time.NewTicker(opts.PollInterval)
defer pollTicker.Stop()

for {
    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-pollTicker.C:
        messages, err := mailboxStore.Poll(ctx, agentRecord.Namespace, 0, opts.MaxPollMessages)
        if err != nil {
            log.Error().Err(err).Msg("poll failed")
            continue
        }
        for _, msg := range messages {
            if err := handleMessage(ctx, msg, ...); err != nil {
                // nack with backoff
                mailboxStore.Nack(ctx, msg.ID, backoffDuration(msg.Attempt))
            } else {
                mailboxStore.Ack(ctx, msg.ID)
            }
        }
    }
}
```

- **Key behaviors**:
  - `Poll` returns messages where `visible_at <= now` and updates visibility.
  - Each message gets a 30-second lease (from `Poll` implementation).
  - Failed messages are nacked with exponential backoff.

#### D1.3 — TTL expiry

**Implementation steps**:

Before processing each message, check TTL:

```go
func isExpired(msg agent.Message) bool {
    if msg.TTLMS <= 0 {
        return false // no TTL = never expires
    }
    expiresAt := msg.Timestamp + msg.TTLMS
    return time.Now().UnixMilli() > expiresAt
}

// In handleMessage:
if isExpired(msg) {
    log.Warn().Str("msg_id", msg.ID).Msg("message expired, acking without processing")
    mailboxStore.Ack(ctx, msg.ID)
    // optionally emit agent.event kind=expired
    return nil
}
```

- **Acceptance criteria**:
  - Expired messages are acked immediately without triggering DSPy.

#### D1.4 — Dedupe (at-least-once safety)

**Implementation steps**:

1. Create a dedupe store (sqlite table or in-memory with persistence):
   ```go
   // internal/agent/daemon/dedupe.go
   type DedupeStore interface {
       IsProcessed(ctx context.Context, agentID, messageID string) (bool, error)
       MarkProcessed(ctx context.Context, agentID, messageID string) error
   }
   ```

2. Before processing:
   ```go
   if processed, _ := dedupeStore.IsProcessed(ctx, opts.AgentID, msg.ID); processed {
       log.Debug().Str("msg_id", msg.ID).Msg("duplicate message, acking")
       mailboxStore.Ack(ctx, msg.ID)
       return nil
   }
   ```

3. After successful processing:
   ```go
   dedupeStore.MarkProcessed(ctx, opts.AgentID, msg.ID)
   mailboxStore.Ack(ctx, msg.ID)
   ```

- **Acceptance criteria**:
  - Crash + restart + redelivery does not cause duplicate work.

#### D1.5 — `agent.ask` handler

**Implementation steps**:

```go
// internal/agent/daemon/handlers.go
func handleAsk(ctx context.Context, msg agent.Message, dspyAgent *agents.ReActAgent, mailboxStore mailbox.Store) error {
    // 1. Parse payload envelope
    var envelope struct {
        Data agent.AskData `json:"data"`
    }
    json.Unmarshal(msg.Payload, &envelope)
    askData := envelope.Data

    // 2. Build prompt from ask
    prompt := fmt.Sprintf("Question: %s\nContext: %v", askData.Question, askData.Context)

    // 3. Execute DSPy turn
    result, err := dspyAgent.Execute(ctx, prompt)
    if err != nil {
        return fmt.Errorf("dspy execution failed: %w", err)
    }

    // 4. Build reply payload
    replyData := agent.ReplyData{
        AskID:  askData.AskID,
        Answer: map[string]interface{}{"response": result.Output},
    }
    replyPayload, _ := json.Marshal(envelope.Envelope{
        Version: 1,
        Status:  "ok",
        Command: "agent.reply",
        Data:    replyData,
    })

    // 5. Send reply with correlation
    replyMsg := agent.Message{
        ID:        ulid.Make().String(),
        FromNS:    msg.ToNS,   // we are the sender now
        ToNS:      msg.FromNS, // reply to asker
        Type:      agent.MessageTypeReply,
        TTLMS:     300000, // 5 min
        Headers:   map[string]string{"correlation": askData.AskID},
        Payload:   replyPayload,
        VisibleAt: time.Now().Unix(),
        Timestamp: time.Now().Unix(),
    }
    return mailboxStore.Send(ctx, replyMsg)
}
```

- **Acceptance criteria**:
  - `agent ask --wait` receives the correlated reply.

#### D1.6 — `agent.cmd` handler (MVP)

**Implementation steps**:

```go
func handleCmd(ctx context.Context, msg agent.Message, ...) error {
    var envelope struct {
        Data agent.CmdData `json:"data"`
    }
    json.Unmarshal(msg.Payload, &envelope)
    cmdData := envelope.Data

    switch cmdData.Action {
    case "run_skill":
        // Invoke skill via runner (future)
        return errors.New("run_skill not yet implemented")
    case "run_turn", "do_work":
        // Execute DSPy turn with cmdData.Args as context
        prompt := fmt.Sprintf("Command: %s\nArgs: %v", cmdData.Action, cmdData.Args)
        _, err := dspyAgent.Execute(ctx, prompt)
        return err
    default:
        return fmt.Errorf("unknown action: %s", cmdData.Action)
    }
}
```

- **Backoff on failure**:
  ```go
  func backoffDuration(attempt int) time.Duration {
      base := 5 * time.Second
      return base * time.Duration(1<<min(attempt, 5)) // max 160s
  }
  ```

#### D1.7 — `agent.event` handler (MVP)

**Implementation steps**:

```go
func handleEvent(ctx context.Context, msg agent.Message, ...) error {
    var envelope struct {
        Data agent.EventData `json:"data"`
    }
    json.Unmarshal(msg.Payload, &envelope)
    eventData := envelope.Data

    // MVP: just log it
    log.Info().
        Str("event_id", eventData.EventID).
        Str("kind", eventData.Kind).
        Int("job_count", eventData.JobCount).
        Msg("received agent event")

    // Future: propagate to overseer, update metrics, etc.
    return nil
}
```

#### D1.8 — Heartbeats

**Implementation steps**:

```go
// In Run(), start a separate goroutine:
heartbeatTicker := time.NewTicker(opts.HeartbeatInterval)
defer heartbeatTicker.Stop()

go func() {
    for {
        select {
        case <-ctx.Done():
            return
        case <-heartbeatTicker.C:
            if err := agentStore.UpdateHeartbeat(ctx, opts.AgentID); err != nil {
                log.Error().Err(err).Msg("heartbeat update failed")
            }
        }
    }
}()
```

- **Acceptance criteria**:
  - `agentctl agent info <id>` shows recent `heartbeat_at`.

#### D1.9 — Stop/kill semantics

**Implementation steps**:

1. Periodically check agent state in the poll loop:
   ```go
   // Every N iterations or on a separate ticker:
   currentAgent, _ := agentStore.Get(ctx, opts.AgentID)
   if currentAgent.State == agent.StateStopped {
       log.Info().Msg("agent state is stopped, exiting daemon")
       return nil
   }
   ```

2. On daemon exit (deferred):
   ```go
   defer func() {
       finalState := agent.StateStopped
       if r := recover(); r != nil {
           finalState = agent.StateError
       }
       agentStore.UpdateState(context.Background(), opts.AgentID, finalState)
   }()
   ```

- **Acceptance criteria**:
  - `agentctl agent kill <id>` causes daemon to exit within one poll interval.

#### D1.10 — Observability

**Implementation steps**:

1. Use zerolog for structured logging to stderr:
   ```go
   log := zerolog.New(os.Stderr).With().
       Str("agent_id", opts.AgentID).
       Str("trace_id", traceID).
       Logger()
   ```

2. Integrate with trajectory capture (from `tools.go` patterns):
   ```go
   toolsCfg := tools.Config{
       TraceID:               traceID,
       TrajectoryStorageRoot: opts.StorageRoot,
       // ...
   }
   ```

3. Per-message logging:
   ```go
   log.Info().
       Str("msg_id", msg.ID).
       Str("type", string(msg.Type)).
       Int("attempt", msg.Attempt).
       Msg("processing message")
   ```

- **Acceptance criteria**:
  - Logs are structured JSON on stderr.
  - Trajectory events include `trace_id` and `actor_id`.

---

### Milestone B2 — SQLite Dedupe Persistence

The in-memory `MemoryDedupeStore` loses state on daemon restart, which can cause
duplicate message processing after crash recovery. This milestone adds SQLite
persistence for the dedupe store.

#### B2.0 — Create SQLite dedupe store

- **Deliverables**
  - New file: `internal/agent/daemon/dedupe_sqlite.go`
  - New file: `internal/agent/daemon/dedupe_sqlite_test.go`
  - Schema migration in dedupe store initialization

**Schema**:

```sql
CREATE TABLE IF NOT EXISTS daemon_dedupe (
    agent_id    TEXT NOT NULL,
    message_id  TEXT NOT NULL,
    processed_at INTEGER NOT NULL,  -- unix timestamp
    PRIMARY KEY (agent_id, message_id)
);

CREATE INDEX IF NOT EXISTS idx_dedupe_agent ON daemon_dedupe(agent_id);
CREATE INDEX IF NOT EXISTS idx_dedupe_processed_at ON daemon_dedupe(processed_at);
```

#### B2.1 — Implement `SQLiteDedupeStore`

**Implementation steps**:

1. Define the struct:
   ```go
   // internal/agent/daemon/dedupe_sqlite.go
   package daemon

   import (
       "context"
       "database/sql"
       "fmt"
       "path/filepath"
       "time"

       _ "modernc.org/sqlite"
   )

   type SQLiteDedupeStore struct {
       db *sql.DB
   }
   ```

2. Implement `Open` function:
   ```go
   func OpenSQLiteDedupeStore(ctx context.Context, storageRoot string) (*SQLiteDedupeStore, error) {
       dbPath := filepath.Join(storageRoot, "daemon_dedupe.db")
       db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
       if err != nil {
           return nil, fmt.Errorf("open dedupe db: %w", err)
       }

       // Run migrations
       if err := migrateDedupe(ctx, db); err != nil {
           db.Close()
           return nil, fmt.Errorf("migrate dedupe db: %w", err)
       }

       return &SQLiteDedupeStore{db: db}, nil
   }

   func migrateDedupe(ctx context.Context, db *sql.DB) error {
       schema := `
       CREATE TABLE IF NOT EXISTS daemon_dedupe (
           agent_id     TEXT NOT NULL,
           message_id   TEXT NOT NULL,
           processed_at INTEGER NOT NULL,
           PRIMARY KEY (agent_id, message_id)
       );
       CREATE INDEX IF NOT EXISTS idx_dedupe_agent ON daemon_dedupe(agent_id);
       CREATE INDEX IF NOT EXISTS idx_dedupe_processed_at ON daemon_dedupe(processed_at);
       `
       _, err := db.ExecContext(ctx, schema)
       return err
   }
   ```

3. Implement `IsProcessed`:
   ```go
   func (s *SQLiteDedupeStore) IsProcessed(ctx context.Context, agentID, messageID string) (bool, error) {
       var count int
       err := s.db.QueryRowContext(ctx,
           `SELECT COUNT(*) FROM daemon_dedupe WHERE agent_id = ? AND message_id = ?`,
           agentID, messageID,
       ).Scan(&count)
       if err != nil {
           return false, fmt.Errorf("check processed: %w", err)
       }
       return count > 0, nil
   }
   ```

4. Implement `MarkProcessed`:
   ```go
   func (s *SQLiteDedupeStore) MarkProcessed(ctx context.Context, agentID, messageID string) error {
       _, err := s.db.ExecContext(ctx,
           `INSERT OR IGNORE INTO daemon_dedupe (agent_id, message_id, processed_at) VALUES (?, ?, ?)`,
           agentID, messageID, time.Now().Unix(),
       )
       if err != nil {
           return fmt.Errorf("mark processed: %w", err)
       }
       return nil
   }
   ```

5. Implement `Close`:
   ```go
   func (s *SQLiteDedupeStore) Close() error {
       return s.db.Close()
   }
   ```

- **Acceptance criteria**:
  - `SQLiteDedupeStore` implements `DedupeStore` interface.
  - Database file created at `~/.agentctl/daemon_dedupe.db`.

#### B2.2 — Add TTL-based cleanup

Old dedupe records should be pruned to prevent unbounded growth.

**Implementation steps**:

1. Add `Cleanup` method:
   ```go
   // Cleanup removes dedupe records older than the given duration.
   func (s *SQLiteDedupeStore) Cleanup(ctx context.Context, olderThan time.Duration) (int64, error) {
       cutoff := time.Now().Add(-olderThan).Unix()
       result, err := s.db.ExecContext(ctx,
           `DELETE FROM daemon_dedupe WHERE processed_at < ?`,
           cutoff,
       )
       if err != nil {
           return 0, fmt.Errorf("cleanup dedupe: %w", err)
       }
       return result.RowsAffected()
   }
   ```

2. Add cleanup to daemon startup or periodic ticker:
   ```go
   // In daemon.Run(), after opening dedupe store:
   dedupeStore, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
   if err != nil {
       return fmt.Errorf("open dedupe store: %w", err)
   }
   defer dedupeStore.Close()

   // Cleanup records older than 7 days on startup
   if cleaned, err := dedupeStore.Cleanup(ctx, 7*24*time.Hour); err != nil {
       log.Warn().Err(err).Msg("dedupe cleanup failed")
   } else if cleaned > 0 {
       log.Info().Int64("cleaned", cleaned).Msg("dedupe cleanup completed")
   }
   ```

- **Acceptance criteria**:
  - Records older than 7 days are removed on daemon startup.
  - Database size remains bounded over time.

#### B2.3 — Update daemon to use SQLite dedupe

**Implementation steps**:

1. Update `daemon.go` to use `SQLiteDedupeStore`:
   ```go
   // Replace:
   dedupeStore := NewMemoryDedupeStore()

   // With:
   dedupeStore, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
   if err != nil {
       return fmt.Errorf("open dedupe store: %w", err)
   }
   defer func() { _ = dedupeStore.Close() }() //nolint:errcheck
   ```

2. Add fallback option for testing:
   ```go
   // In Options struct:
   type Options struct {
       // ... existing fields ...
       UseMemoryDedupe bool // For testing; defaults to false (use SQLite)
   }

   // In Run():
   var dedupeStore DedupeStore
   if opts.UseMemoryDedupe {
       dedupeStore = NewMemoryDedupeStore()
   } else {
       sqliteStore, err := OpenSQLiteDedupeStore(ctx, opts.StorageRoot)
       if err != nil {
           return fmt.Errorf("open dedupe store: %w", err)
       }
       defer func() { _ = sqliteStore.Close() }() //nolint:errcheck
       dedupeStore = sqliteStore
   }
   ```

- **Acceptance criteria**:
  - Daemon uses SQLite dedupe by default.
  - Tests can opt into in-memory dedupe for speed.

#### B2.4 — Add unit tests for SQLite dedupe

- **File**: `internal/agent/daemon/dedupe_sqlite_test.go`
- **Test cases**:
  - `TestSQLiteDedupeStore_Basic`: mark → isProcessed returns true.
  - `TestSQLiteDedupeStore_Persistence`: close → reopen → isProcessed still
    true.
  - `TestSQLiteDedupeStore_Cleanup`: insert old records → cleanup → records
    removed.
  - `TestSQLiteDedupeStore_AgentIsolation`: agent A marks → agent B isProcessed
    returns false.

**Example test**:

```go
func TestSQLiteDedupeStore_Persistence(t *testing.T) {
    ctx := context.Background()
    tmpDir := t.TempDir()

    // First session: mark a message
    store1, err := OpenSQLiteDedupeStore(ctx, tmpDir)
    require.NoError(t, err)
    err = store1.MarkProcessed(ctx, "agent-1", "msg-1")
    require.NoError(t, err)
    require.NoError(t, store1.Close())

    // Second session: verify persistence
    store2, err := OpenSQLiteDedupeStore(ctx, tmpDir)
    require.NoError(t, err)
    defer store2.Close()

    processed, err := store2.IsProcessed(ctx, "agent-1", "msg-1")
    require.NoError(t, err)
    assert.True(t, processed, "message should be marked as processed after restart")
}
```

- **Acceptance criteria**:
  - All tests pass.
  - Persistence survives daemon restart.

---

### Milestone C — CLI UX (run + ask + wait)

#### C1.0 — `agentctl agent run <agent-id>`

- **Touchpoints**
  - `cmd/agentctl/cmd/agent.go` (new subcommand)
  - `internal/agent/daemon/*`

**Implementation steps**:

1. Add Cobra command:
   ```go
   var agentRunCmd = &cobra.Command{
       Use:   "run <agent-id>",
       Short: "Run an agent daemon in the foreground",
       Args:  cobra.ExactArgs(1),
       RunE:  runAgentRun,
   }
   ```

2. Implement `runAgentRun`:
   ```go
   func runAgentRun(cmd *cobra.Command, args []string) error {
       ctx := cmd.Context()
       cfg := config.MustFromContext(ctx)
       agentID := args[0]

       opts := daemon.Options{
           AgentID:           agentID,
           StorageRoot:       cfg.Storage.Root,
           PollInterval:      500 * time.Millisecond,
           HeartbeatInterval: 10 * time.Second,
           MaxPollMessages:   10,
       }

       return daemon.Run(ctx, opts)
   }
   ```

- **Acceptance criteria**:
  - `agentctl agent run <id>` blocks until SIGINT/SIGTERM or agent state becomes
    `stopped`.

#### C1.1 — `agentctl agent ask <agent-id> --question ... [--wait]`

- **Touchpoints**
  - `cmd/agentctl/cmd/agent.go`
  - `internal/storage/mailbox/store.go`
  - `internal/domain/agent/mailbox.go`

**Implementation steps**:

1. Add Cobra command with flags:
   ```go
   var agentAskCmd = &cobra.Command{
       Use:   "ask <agent-id>",
       Short: "Send an ask message to an agent",
       Args:  cobra.ExactArgs(1),
       RunE:  runAgentAsk,
   }

   func init() {
       agentAskCmd.Flags().String("question", "", "The question to ask (required)")
       agentAskCmd.Flags().String("kind", "context", "Ask kind: context|secret|approval|toolhint|other")
       agentAskCmd.Flags().Bool("wait", false, "Wait for reply before returning")
       agentAskCmd.Flags().Duration("timeout", 5*time.Minute, "Timeout for --wait")
       agentAskCmd.MarkFlagRequired("question")
   }
   ```

2. Implement `runAgentAsk`:
   ```go
   func runAgentAsk(cmd *cobra.Command, args []string) error {
       ctx := cmd.Context()
       cfg := config.MustFromContext(ctx)
       agentID := args[0]
       question, _ := cmd.Flags().GetString("question")
       kind, _ := cmd.Flags().GetString("kind")
       wait, _ := cmd.Flags().GetBool("wait")
       timeout, _ := cmd.Flags().GetDuration("timeout")

       // Open mailbox store
       mailboxStore, _ := mailbox.Open(ctx, cfg.Storage.Root)
       defer mailboxStore.Close()

       // Get agent to find its namespace
       agentStore, _ := agents.Open(ctx, cfg.Storage.Root)
       defer agentStore.Close()
       agentRecord, _ := agentStore.Get(ctx, agentID)

       // Build ask message
       askID := ulid.Make().String()
       askData := agent.AskData{
           AskID:    askID,
           Kind:     kind,
           Question: question,
       }
       payload, _ := json.Marshal(envelope.Envelope{
           Version: 1,
           Status:  "ok",
           Command: "agent.ask",
           Data:    askData,
       })

       msg := agent.Message{
           ID:        ulid.Make().String(),
           FromNS:    "cli:" + ulid.Make().String(), // unique caller namespace
           ToNS:      agentRecord.Namespace,
           Type:      agent.MessageTypeAsk,
           TTLMS:     int64(timeout.Milliseconds()),
           Headers:   map[string]string{"correlation": askID},
           Payload:   payload,
           VisibleAt: time.Now().Unix(),
           Timestamp: time.Now().Unix(),
       }

       if err := mailboxStore.Send(ctx, msg); err != nil {
           return writeErrorEnvelope(cmd, "agent/ask", "ESEND", err.Error())
       }

       // Output ask confirmation
       env := envelope.OK("agent/ask", map[string]any{
           "ask_id":     askID,
           "message_id": msg.ID,
           "sent_to":    agentRecord.Namespace,
       })
       envelope.Write(os.Stdout, env)

       if wait {
           return waitForReply(ctx, mailboxStore, msg.FromNS, askID, timeout)
       }
       return nil
   }
   ```

- **Acceptance criteria**:
  - Ask message appears in agent's mailbox.
  - Output envelope contains `ask_id` for correlation.

#### C1.2 — `agentctl agent cmd <agent-id> --action ...`

**Implementation steps**:

1. Add Cobra command:
   ```go
   var agentCmdCmd = &cobra.Command{
       Use:   "cmd <agent-id>",
       Short: "Send a command to an agent",
       Args:  cobra.ExactArgs(1),
       RunE:  runAgentCmd,
   }

   func init() {
       agentCmdCmd.Flags().String("action", "", "Command action: run_skill|run_turn|do_work")
       agentCmdCmd.Flags().String("skill", "", "Skill to run (for run_skill action)")
       agentCmdCmd.Flags().String("args", "{}", "JSON args for the command")
       agentCmdCmd.MarkFlagRequired("action")
   }
   ```

2. Build `agent.cmd` message similar to ask, using `agent.CmdData`.

- **Acceptance criteria**:
  - Cmd message appears in agent's mailbox with correct action/args.

#### C1.3 — Reply waiting (`--wait`)

**Implementation steps**:

```go
func waitForReply(ctx context.Context, store mailbox.Store, callerNS, askID string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    pollInterval := 500 * time.Millisecond

    for time.Now().Before(deadline) {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(pollInterval):
            // Poll for replies to our namespace
            messages, _ := store.List(ctx, callerNS, 50)
            for _, msg := range messages {
                if msg.Type != agent.MessageTypeReply {
                    continue
                }
                if msg.Headers["correlation"] == askID {
                    // Found our reply!
                    var replyEnv envelope.Envelope
                    json.Unmarshal(msg.Payload, &replyEnv)

                    // Ack the reply
                    store.Ack(ctx, msg.ID)

                    // Output the reply envelope
                    return envelope.Write(os.Stdout, replyEnv)
                }
            }
        }
    }
    return fmt.Errorf("timeout waiting for reply to ask_id=%s", askID)
}
```

- **Acceptance criteria**:
  - `agent ask --wait` blocks until reply arrives.
  - Reply envelope is printed to stdout.
  - Reply message is acked.

#### C1.4 — Improve `agentctl agent watch`

- **Current state**: `runAgentWatch` in `agent.go:321-501` emits message counts
  only.
- **Target behavior**: Include message IDs, types, and correlation samples.

**Implementation steps**:

Modify the mailbox messages section in `runAgentWatch`:

```go
// Instead of just:
// "message_count": len(messages),

// Include sample details:
messageSamples := make([]map[string]any, 0, min(len(messages), 3))
for i, msg := range messages {
    if i >= 3 {
        break
    }
    messageSamples = append(messageSamples, map[string]any{
        "id":          msg.ID,
        "type":        msg.Type,
        "from":        msg.FromNS,
        "correlation": msg.Headers["correlation"],
    })
}

data := map[string]interface{}{
    "event":           "mailbox_messages",
    "agent_id":        agentID,
    "message_count":   len(messages),
    "message_samples": messageSamples,
}
```

- **Acceptance criteria**:
  - Watch output includes message type and correlation for debugging.

---

### Milestone D — Overseer (multi-agent) (MVP)

The overseer manages agent lifecycle (spawn, kill, supervision) and processes
spawn requests from parent agents via the mailbox.

#### O1.0 — Define spawn request message format

**Message type**: `agent.cmd` with `action: "spawn"`

```go
// Spawn request payload (sent by parent agent)
type SpawnRequest struct {
    CmdID       string            `json:"cmd_id"`
    Action      string            `json:"action"` // "spawn"
    ChildConfig ChildAgentConfig  `json:"child_config"`
}

type ChildAgentConfig struct {
    Role        string   `json:"role"`        // e.g., "coder", "reviewer"
    Prompt      string   `json:"prompt"`      // system prompt
    SkillsAllow []string `json:"skills_allow"` // tool allowlist
    ParentNS    string   `json:"parent_ns"`   // for reply routing
}

// Spawn response (sent by overseer to parent)
type SpawnResponse struct {
    ChildID    string `json:"child_id"`
    ChildNS    string `json:"child_ns"`
    Success    bool   `json:"success"`
    Error      string `json:"error,omitempty"`
}
```

#### O1.1 — Implement overseer daemon loop

- **Touchpoints**
  - `internal/agent/runtime/overseer.go`
  - `internal/execution/agentmanager/manager.go`

**Implementation steps**:

```go
// internal/agent/runtime/overseer.go
func RunOverseer(ctx context.Context, opts OverseerOptions) error {
    mailboxStore, _ := mailbox.Open(ctx, opts.StorageRoot)
    agentManager := agentmanager.New(opts.StorageRoot)

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(opts.PollInterval):
            // Poll for spawn requests to overseer namespace
            messages, _ := mailboxStore.Poll(ctx, "actor:system:overseer", 0, 10)
            for _, msg := range messages {
                if msg.Type == agent.MessageTypeCmd {
                    handleOverseerCmd(ctx, msg, agentManager, mailboxStore)
                }
                mailboxStore.Ack(ctx, msg.ID)
            }
        }
    }
}

func handleOverseerCmd(ctx context.Context, msg agent.Message, mgr *agentmanager.Manager, store mailbox.Store) {
    var cmd struct {
        Data SpawnRequest `json:"data"`
    }
    json.Unmarshal(msg.Payload, &cmd)

    if cmd.Data.Action != "spawn" {
        return // ignore unknown actions
    }

    // Spawn child agent
    childID, err := mgr.Spawn(ctx, agentmanager.SpawnRequest{
        Role:        cmd.Data.ChildConfig.Role,
        Prompt:      cmd.Data.ChildConfig.Prompt,
        SkillsAllow: cmd.Data.ChildConfig.SkillsAllow,
        ParentID:    msg.FromNS,
    })

    // Send response to parent
    response := SpawnResponse{
        ChildID: childID,
        ChildNS: "actor:agent:" + childID,
        Success: err == nil,
    }
    if err != nil {
        response.Error = err.Error()
    }

    replyPayload, _ := json.Marshal(envelope.Envelope{
        Version: 1,
        Status:  "ok",
        Command: "agent.reply",
        Data:    response,
    })

    store.Send(ctx, agent.Message{
        ID:        ulid.Make().String(),
        FromNS:    "actor:system:overseer",
        ToNS:      msg.FromNS,
        Type:      agent.MessageTypeReply,
        Headers:   map[string]string{"correlation": cmd.Data.CmdID},
        Payload:   replyPayload,
        VisibleAt: time.Now().Unix(),
        Timestamp: time.Now().Unix(),
    })
}
```

#### O1.2 — Add `agent.spawn` tool for agents

- **Touchpoints**: `internal/agent/tools/agent_tools.go` (new file)

**Implementation steps**:

```go
// Register tool: agent.spawn
// Schema: {"role": "string", "prompt": "string", "skills_allow": ["string"]}
func (r *Registry) agentSpawn(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
    role := args["role"].(string)
    prompt := args["prompt"].(string)
    skillsAllow, _ := args["skills_allow"].([]string)

    cmdID := ulid.Make().String()
    spawnReq := SpawnRequest{
        CmdID:  cmdID,
        Action: "spawn",
        ChildConfig: ChildAgentConfig{
            Role:        role,
            Prompt:      prompt,
            SkillsAllow: skillsAllow,
            ParentNS:    r.config.ActorID,
        },
    }

    // Send to overseer
    payload, _ := json.Marshal(envelope.Envelope{
        Version: 1, Status: "ok", Command: "agent.cmd", Data: spawnReq,
    })
    msg := agent.Message{
        ID:        ulid.Make().String(),
        FromNS:    r.config.ActorID,
        ToNS:      "actor:system:overseer",
        Type:      agent.MessageTypeCmd,
        Headers:   map[string]string{"correlation": cmdID},
        Payload:   payload,
        VisibleAt: time.Now().Unix(),
        Timestamp: time.Now().Unix(),
    }
    r.mailboxStore.Send(ctx, msg)

    return successResult(map[string]any{
        "spawn_request_id": cmdID,
        "sent_to":          "overseer",
        "note":             "Spawn request sent. Reply will arrive in mailbox.",
    }), nil
}
```

#### O1.3 — CLI: `agentctl overseer run`

**Implementation steps**:

```go
var overseerRunCmd = &cobra.Command{
    Use:   "run",
    Short: "Run the overseer daemon",
    RunE: func(cmd *cobra.Command, args []string) error {
        ctx := cmd.Context()
        cfg := config.MustFromContext(ctx)
        return overseer.RunOverseer(ctx, overseer.Options{
            StorageRoot:  cfg.Storage.Root,
            PollInterval: 500 * time.Millisecond,
        })
    },
}
```

- **Acceptance criteria**:
  - Parent agent can call `agent.spawn` → overseer creates child → parent
    receives child ID.

---

### Milestone E — Security & policy

#### S1.0 — Enforce allowlist in daemon/tool registry

- **Key decision**: `Agent.SkillsAllow` is a **tool allowlist** using tool names
  (e.g., `fs.read_file`, `edit.apply_patch`, `mail.send`).

**Implementation steps**:

1. Modify `tools.NewRegistry` to accept an allowlist:
   ```go
   func NewRegistry(cfg Config, recorder TelemetryRecorder, allowlist []string) (*Registry, error) {
       r := &Registry{...}
       r.registerAllTools() // register all tools first

       if len(allowlist) > 0 {
           r.filterByAllowlist(allowlist)
       }
       return r, nil
   }

   func (r *Registry) filterByAllowlist(allowlist []string) {
       allowSet := make(map[string]bool)
       for _, name := range allowlist {
           allowSet[name] = true
       }

       // Get all registered tools and remove those not in allowlist
       allTools := r.tools.List()
       for _, tool := range allTools {
           if !allowSet[tool.Name()] {
               r.tools.Unregister(tool.Name())
           }
       }
   }
   ```

2. In daemon startup, pass agent's allowlist:
   ```go
   registry, _ := tools.NewRegistry(toolsCfg, recorder, agentRecord.SkillsAllow)
   ```

- **Acceptance criteria**:
  - Agent with `skills_allow: ["fs.read_file", "code.search"]` cannot invoke
    `edit.apply_patch`.

#### S1.1 — Enforce policy per mailbox turn

**Implementation steps**:

1. Wrap DSPy execution with timeout:
   ```go
   turnCtx, cancel := context.WithTimeout(ctx, agentRecord.Policy.TurnTimeout)
   defer cancel()

   result, err := dspyAgent.Execute(turnCtx, prompt)
   if errors.Is(err, context.DeadlineExceeded) {
       // Log timeout, nack message with backoff
       return fmt.Errorf("turn timeout exceeded")
   }
   ```

2. Enforce max output size in tool results:
   ```go
   // In tool wrapper (tools.go wrapWithTelemetry)
   if len(result.Content) > policy.MaxOutputBytes {
       // Store in CAS, return summary + artifact reference
       digest := cas.Put(ctx, result.Content)
       result.Content = fmt.Sprintf("Output too large (%d bytes). Stored as artifact: %s", len(result.Content), digest)
   }
   ```

- **Acceptance criteria**:
  - Turns exceeding timeout are aborted and nacked.
  - Large outputs are automatically artifactized.

#### S1.2 — Implement filesystem policy `home` + `tmp`

- **Touchpoints**: `internal/domain/policy/policy.go`,
  `internal/agent/tools/tools.go`

**Implementation steps**:

1. Extend `PathValidator` to support policy-based roots:
   ```go
   // In tools.Config
   type Config struct {
       // ...
       FilesystemPolicy string // "workspace" | "home" | "tmp" | "all"
   }

   // In NewRegistry
   var allowedRoots []string
   switch cfg.FilesystemPolicy {
   case "workspace":
       allowedRoots = []string{cfg.WorkspaceRoot}
   case "home":
       allowedRoots = []string{cfg.WorkspaceRoot, os.Getenv("HOME")}
   case "tmp":
       allowedRoots = []string{cfg.WorkspaceRoot, os.TempDir()}
   case "all":
       allowedRoots = []string{cfg.WorkspaceRoot, os.Getenv("HOME"), os.TempDir()}
   }
   pathValidator, _ := policy.NewPathValidator(cfg.WorkspaceRoot, allowedRoots)
   ```

- **Acceptance criteria**:
  - Agent with `filesystem_policy: "workspace"` cannot read `~/.ssh/id_rsa`.

---

### Milestone F — Tests (deterministic)

#### TST1.0 — Fake/mock LLM for DSPy

- **Touchpoints**: `internal/agent/runtime/testutil/fake_llm.go` (new)

**Implementation steps**:

```go
// internal/agent/runtime/testutil/fake_llm.go
package testutil

import (
    "context"
)

// FakeLLM returns scripted responses for deterministic testing.
type FakeLLM struct {
    Responses []string
    CallIndex int
}

func (f *FakeLLM) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
    if f.CallIndex >= len(f.Responses) {
        return &core.LLMResponse{Content: "No more scripted responses"}, nil
    }
    response := f.Responses[f.CallIndex]
    f.CallIndex++
    return &core.LLMResponse{Content: response}, nil
}

// NewFakeLLM creates a fake LLM with scripted responses.
func NewFakeLLM(responses ...string) *FakeLLM {
    return &FakeLLM{Responses: responses}
}
```

- **Usage in tests**:
  ```go
  func TestDaemonAskHandler(t *testing.T) {
      fakeLLM := testutil.NewFakeLLM(
          "I will use fs.read_file to check the code.",
          "The file contains a bug on line 42.",
      )
      agent := agents.NewReActAgent(fakeLLM, registry.Tools(), "You are a coder.")
      // ... test daemon with this agent
  }
  ```

- **Acceptance criteria**:
  - Daemon tests run without API keys.
  - Tests are deterministic and reproducible.

#### TST1.1 — Daemon unit tests

- **File**: `internal/agent/daemon/daemon_test.go`

**Test cases**:

```go
func TestDaemon_TTLExpiry(t *testing.T) {
    // Setup: send message with TTLMS = 1 (already expired)
    // Assert: message is acked without DSPy execution
}

func TestDaemon_Dedupe(t *testing.T) {
    // Setup: send same message ID twice
    // Assert: second delivery is skipped
}

func TestDaemon_AskReplyCorrelation(t *testing.T) {
    // Setup: send agent.ask with ask_id
    // Assert: agent.reply has headers.correlation = ask_id
}

func TestDaemon_NackBackoff(t *testing.T) {
    // Setup: configure fake LLM to return error
    // Assert: message is nacked with increasing visibility timeout
}

func TestDaemon_Heartbeat(t *testing.T) {
    // Setup: run daemon for 2 heartbeat intervals
    // Assert: agent record heartbeat_at is updated
}

func TestDaemon_StopOnKill(t *testing.T) {
    // Setup: run daemon, then set agent state to "stopped"
    // Assert: daemon exits cleanly
}
```

#### TST1.2 — Integration tests

- **File**: `test/integration/daemon_test.go`

**Test cases**:

```go
func TestDaemon_EndToEnd(t *testing.T) {
    // 1. Spawn agent via agentmanager
    // 2. Start daemon in goroutine
    // 3. Send agent.ask via mailbox
    // 4. Wait for agent.reply
    // 5. Verify reply content and correlation
    // 6. Kill agent, verify daemon exits
}

func TestDaemon_ToolExecution(t *testing.T) {
    // 1. Configure fake LLM to request fs.read_file
    // 2. Send ask
    // 3. Verify tool was called (via telemetry recorder)
    // 4. Verify reply contains tool output
}
```

- **Acceptance criteria**:
  - All tests pass with `CGO_ENABLED=0 go test ./...`.
  - No flaky tests (deterministic fake LLM).

---

### Milestone G — Docs updates

#### DOC1.0 — Update `docs/agent_profile.md`

**Sections to add/update**:

1. **Daemon mode** (new section):
   ````markdown
   ## Running an Agent Daemon

   An agent can run as a long-lived daemon that polls for mailbox messages:

   ```bash
   agentctl agent run <agent-id>
   ```
   ````

   The daemon:
   - Polls `mailbox.Store` for `agent.ask`, `agent.cmd`, `agent.event` messages
   - Executes DSPy turns with the configured tool registry
   - Sends `agent.reply` with correlation headers
   - Maintains heartbeats and state transitions
   ```
   ```

2. **Ask/reply semantics** (new section):
   ````markdown
   ## Sending Asks and Waiting for Replies

   ```bash
   # Fire-and-forget ask
   agentctl agent ask <agent-id> --question "What files need refactoring?"

   # Wait for reply (blocks until response or timeout)
   agentctl agent ask <agent-id> --question "..." --wait --timeout 5m
   ```
   ````

   The `--wait` flag polls for a correlated `agent.reply` message.
   ```
   ```

3. **TTL and dedupe** (new section):
   ```markdown
   ## Message Delivery Semantics

   - **TTL**: Messages with `ttl_ms` are expired if `timestamp + ttl_ms < now`.
     Expired messages are acked without processing.
   - **At-least-once**: The daemon may receive duplicate messages after crashes. A
     dedupe store tracks processed message IDs to prevent duplicate work.
   - **Visibility leases**: Polled messages get a 30-second visibility timeout.
     Failed processing triggers a nack with exponential backoff.
   ```

#### DOC1.1 — Update `docs/spec/mailbox_blackboard.md`

**Sections to update**:

1. Add daemon poll semantics to the mailbox section.
2. Document the `headers.correlation` convention for ask/reply.
3. Add examples of `agent.ask`, `agent.reply`, `agent.cmd`, `agent.event`
   payloads.

#### DOC1.2 — Add `docs/spec/daemon_protocol.md` (new)

**Contents**:

- Daemon lifecycle states: `pending` → `running` → `stopped` / `error`
- Message handling flow diagram
- TTL expiry algorithm
- Dedupe store schema
- Heartbeat interval and liveness detection

- **Acceptance criteria**:
  - New users can understand daemon mode from docs alone.
  - Specs match implemented behavior (no drift).

---

### Milestone H — Verification

#### VER1.0 — Run CI-equivalent checks

**Commands to run**:

```bash
# Format check
make fmt
git diff --exit-code  # ensure no changes

# Lint
make lint

# Unit tests (no CGO)
CGO_ENABLED=0 go test ./...

# Race detector (for daemon concurrency)
go test -race ./internal/agent/daemon/...
go test -race ./internal/storage/mailbox/...

# Integration tests
go test -v ./test/integration/...
```

#### VER1.1 — Manual smoke test

**Steps**:

1. Build: `go build -o bin/agentctl ./cmd/agentctl`
2. Spawn agent:
   `./agentctl agent spawn --role coder --prompt "You are a coder"`
3. Run daemon in background: `./agentctl agent run <agent-id> &`
4. Send ask:
   `./agentctl agent ask <agent-id> --question "Hello" --wait --timeout 30s`
5. Verify reply envelope is printed.
6. Kill agent: `./agentctl agent kill <agent-id>`
7. Verify daemon process exits.

- **Acceptance criteria**:
  - All CI checks pass.
  - Smoke test completes successfully.

---

## 5. Recommended execution order (dependency-aware)

1. **Milestone A** (replace stubbed tools) – unlocks real agent behavior.
2. **Milestone B** (daemon MVP) – unlocks always-on ask/reply.
3. **Milestone C** (CLI UX) – unlocks interactive loop.
4. **Milestone E + F** (policy + tests) – makes it safe and regression-proof.
5. **Milestone D + G** (overseer + docs) – expand multi-agent and document
   contracts.

---

## 6. Definition of Done (project-level)

This phase is “done” when:

- A spawned agent can be run as a daemon and can answer mailbox `agent.ask`
  messages.
- The agent can:
  - manage tasks via `todo.*` backed by sqlite,
  - coordinate via `mail.*` backed by BoardStore,
  - coordinate/queue work via `bb.*` backed by blackboard topic store,
  - safely edit and test code via existing `fs.*`, `edit.*`, `tests.*` tools.
- The mailbox loop is correct under retries (TTL + dedupe).
- Tests are deterministic and docs/specs match behavior.
