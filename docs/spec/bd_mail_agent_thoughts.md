You already have the right primitives in agentctl:

- **Tasks:** `internal/storage/tasks` + `todo/manage` + `hooks/task_guard`
  (`task_id`, `workspace_id`, `scope_path`, `DependsOn`).
- **Hooks:** `hooks/task_guard` and `hooks/knowledge_router` with a clean
  `hook.Input`/`Output` contract.
- **Memory spec:** [task_hooks_memory.md](./task_hooks_memory.md) describes
  task-aware memory entries and a future `hooks/context_inject`.
- **Mail/blackboard:** same spec has a “Mailbox (Inter-Agent Messaging)”
  section.

Beads + beads_viewer + mcp_agent_mail essentially give you:

- A **graph model over tasks** + centrality metrics and execution plans (`bd` +
  `bv`).
- A **mail + coordination fabric** with Git-backed artifacts and MCP tools
  (`mcp_agent_mail`).
- A **lightweight adapter** that makes mail “advisory, non-blocking, and
  optional” (`beads/lib/beads_mail_adapter.py`).

You can lift the ideas almost directly into agentctl.

---

## 1. Graph / PageRank layer for `task_id` + memory

### 1.1 Graph model for tasks

You already store:

- Nodes:
  [tasks.Task](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:41:0-55:1)
  (`id`, `workspace_id`, `status`, `DependsOn`, `ParentID`, `Children`)
- Implicit edges: `DependsOn` (and conceptually parent/child)

From beads + bv graph:

- They treat issues as **nodes** and `blocks` dependencies as **directed
  edges**.
- bv’s
  [Analyzer](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/beads_viewer/pkg/analysis/graph.go:30:0-35:1)
  builds a directed graph and computes:

  - `PageRank`, `Betweenness`, `Eigenvector`
  - `InDegree` / `OutDegree`
  - `CriticalPathScore` via topo sort + “height”
  - Cycles + density + topological order

For agentctl, I’d:

- Define an internal package, e.g. `internal/analysis/tasksgraph`:
  - Load all tasks for a `workspace_id`.
  - Build a directed graph:
    - Node = `task_id`.
    - Edge `u -> v` if task `u` depends on `v` (exactly like bv).
    - Optionally distinguish dependency kinds later (blocks, related,
      parent-child) if you adopt beads-style types.
  - Compute a subset of metrics using a **pure Go** graph lib (gonum/graph or a
    lightweight in-house version):
    - `pagerank[task_id]`
    - `in_degree`, `out_degree`
    - `critical_path_score[task_id]` (impact depth / “how many things ultimately
      depend on this?”)
    - `topological_order` (execution order candidate)
    - cycles, if any (should be rare; treat as structural warnings).

This becomes your **“beads brain”** for tasks.

### 1.2 Expose graph insights via `todo/manage` + CLI

- Add a new `operation`, e.g. `graph_insights` (name to bikeshed), that:
  - Inputs: `workspace_id`
  - Outputs JSON mirroring a trimmed-down
    [GraphStats](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/beads_viewer/pkg/analysis/graph.go:15:0-27:1):

    ```jsonc
    {
      "nodes": [
        {
          "task_id": "...",
          "pagerank": 0.0123,
          "critical_path_score": 5.0,
          "in_degree": 3,
          "out_degree": 1,
          "status": "pending"
        }
      ],
      "topological_order": ["t1", "t2", ...]
    }
    ```

- CLI sugar: `agentctl todo list --sort pagerank` or `--sort impact` using the
  graph layer under the hood.

This directly answers “PageRank-style algorithm over task_ids”.

### 1.3 Connecting memory entries to the task graph

Per
[task_hooks_memory.md](cci:7://file:///Users/jkatigbak/repos/personal/agentctl/docs/spec/task_hooks_memory.md:0:0-0:0),
memory entries should carry:

- `workspace_id`
- `task_id`
- `kind` (diff, test_output, note, mail, etc.)
- `summary`, optional embedding

To **“better leverage task_ids/memories”**:

- Treat **tasks** as the primary graph nodes.
- Treat **memory entries** as attributes on nodes:

  - When you write a memory entry for `(workspace_id, task_id)`, you’re
    attaching metadata to a node.
  - When retrieving context, you:
    1. Identify the **active task** (via
       [EnsureActive](cci:1://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:36:1-37:99)
       or
       [GetActive](cci:1://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:30:1-31:71)).
    2. Pull the **subgraph**: active task + its ancestors/descendants on the
       critical path.
    3. Rank associated memory entries by:

       ```text
       score(memory) =
           w1 * semantic_similarity(query, embedding)
         + w2 * normalized(task.pagerank or critical_path_score)
         + w3 * recency(memory)
         + w4 * kind-weight (e.g. test failures > casual notes)
       ```

- Implement this in a future `hooks/context_inject` skill:
  - Input: `hook.Input` (UserPromptSubmit or PreToolUse).
  - It:
    - Resolves `workspace_id`, `task_id`.
    - Queries memory store for entries tied to `(workspace_id, task_id)` and
      maybe its immediate dependencies.
    - Uses the graph stats to bias towards **high-impact tasks + near
      neighbors**.
    - Injects a short **Context** string and/or CAS-backed summary.

This is conceptually very close to `bv --robot-insights` + `--robot-plan`, but
**fully local** and keyed by your existing `task_id`s.

### 1.4 Optional: direct reuse of `bd` / `bv`

If you want to piggyback on existing tooling instead of re-implementing metrics:

- For repos that _also_ use Beads:

  - Add an opt-in config: `agentctl.todo.backend: beads`.
  - Implement a shim that shells out to `bd --json` or `bv --robot-insights` and
    maps:

    - Beads `issue.id` → agentctl `task_id` or a parallel “external_task_id”.
    - Graph metrics → the same internal
      [GraphStats](cci:2://file:///Users/jkatigbak/repos/personal/claude-harness/beads_viewer/pkg/analysis/graph.go:15:0-27:1)
      structure.

- For pure-agentctl repos, you keep the internal `tasksgraph` path.

My bias: **copy the ideas**, not the storage, for agentctl’s core; then
optionally offer a “bd-bridge” for folks already on Beads.

---

## 2. Making mail first-class in hooks (mcp_agent_mail concepts)

### 2.1 What mcp_agent_mail gives you

Key pieces from `mcp_agent_mail`:

- **Git-backed message archive** (`messages/YYYY/MM/...`, thread digests).
- **MCP tools** like `send_message`, `fetch_inbox`, `acknowledge_message`, plus
  agent/project registries.
- **Advisory file reservations** (`FileReservation`) to avoid conflicts.
- **Guards**: Python chain-runners for Git hooks (`guard.py`) that compose guard
  scripts.

The `beads/lib/beads_mail_adapter.py` then wraps this with:

- `reserve_issue`, `release_issue`, `notify`, `check_inbox`, `get_reservations`
- **Graceful degradation**: no-ops when server is down.

### 2.2 Agentctl “mail-aware” hook

Instead of trying to “push” into Claude (which you don’t control), you can
**pull mail at the next hook firing**:

- Implement `hooks/mail_router` skill:

  - Input: `hook.Input` (likely on `UserPromptSubmit` and/or `PreToolUse`).
  - It determines:

    - `project_key` = `workspace_root` (or configurable mapping).
    - `agent_name` from env/config (e.g. `AGENTCTL_AGENT_NAME`).

  - It calls `mcp_agent_mail` via MCP:

    - Either through the generic `http/openapi` skill (HTTP to the MCP HTTP
      transport), or via a thin exec skill that talks JSON-RPC to
      `mcp_agent_mail`.

  - It fetches:

    - Unread or `ack_required` messages for `(project_key, agent_name)`.
    - Optionally only new since last `acknowledged_ts`.

  - For each new message:

    - Create a **memory entry** scoped to `(workspace_id, task_id)` if the mail
      carries `task_id` headers (as recommended in §8.1 of your spec).
    - Or, if no explicit `task_id`, bind to the active task found via
      [EnsureActive](cci:1://file:///Users/jkatigbak/repos/personal/claude-harness/agentctl/internal/storage/tasks/store.go:36:1-37:99).
    - Summarize into a few bullets suitable for `hook.Output.Context`.

  - Emit `hook.Output` with:

    - `Decision: "none"` (advisory).
    - `Context`: e.g. “New mail threads: [T-123] Review auth refactor; [T-456]
      Database migration warnings”.
    - `Meta`: structured list of thread IDs, message IDs, flags (`ack_required`,
      urgency).

  - Optionally call `acknowledge_message` in mcp_agent_mail once a message has
    been surfaced, so we don’t re-surface it unless there’s follow-up.

Wire it like:

- `.claude/hooks/mail-router.sh` wrapper similar to `task_guard`.
- In settings, run **before** `knowledge_router` and **after** `task_guard`:

  - `PreToolUse`:
    1. `task_guard` → ensure `task_id`.
    2. `mail_router` → pull in any relevant new mail.
    3. `knowledge_router` → suggest knowledge packs.
  - `UserPromptSubmit` (future): `mail_router` can inject “You have unread
    instructions for this task” at the top of the context.

This gives you the behavior you described:

> “upon receiving any new mail it will immediately show up in the hook”

…in the sense of: **on the next hook event**, new mail is pulled, written into
memory, and surfaced via `hook_output.Context`.

### 2.3 File reservations + task_guard

mcp_agent_mail’s `FileReservation` model is a direct upgrade to
`hooks/task_guard`:

- Extend `task_guard` (or a sibling `hooks/file_guard`) to:

  - Before approving a write, call mcp_agent_mail’s `file_reservation_paths` for
    `(project_key, agent_name, [file_path], exclusive=true, ttl=...)`.
  - If reservation fails due to existing exclusive holder, either:

    - Block in strict mode (`DecisionBlock` with reason citing the other agent).
    - Warn in advisory mode and add context but still approve.

- Record the reservation metadata in your **task** or **memory** entries (e.g.,
  a note “You currently hold an exclusive lease on `src/api/...` via Agent
  Mail.”).

For strict file reservation semantics, any `file_guard` block that prevents a
write SHOULD be surfaced to tools/agents as `E_GUARD_VIOLATION` (see
`dspy_go_agents.md` §11.3), indicating a guard-level conflict rather than an OS
or transport failure.

This aligns your **task graph** and **mail / lease graph**: tasks that touch the
same files can be connected via shared reservations.

---

## 3. Putting it together: a concrete integration plan

### Phase 1 – Graph metrics for tasks

- [ ] Spec: `docs/spec/task_graph_insights.md` describing:
  - Graph model (nodes, edges from `DependsOn`, how we treat parent-child).
  - Metrics (pagerank, impact depth, degrees, cycles).
  - New `todo/manage` op and JSON schema.
- [ ] Impl: `internal/analysis/tasksgraph` + `todo/manage` extension.
- [ ] CLI: `agentctl todo list --sort impact` and/or `agentctl todo insights`.

### Phase 2 – Memory ranking & context injection

- [ ] Extend memory spec to include a `graph_rank_weight` concept.
- [ ] Implement `hooks/context_inject`:
  - Pulls active task, graph stats, and memory entries.
  - Scores memories using similarity + task rank + recency.
  - Injects compact context.

### Phase 3 – Mail-aware hooks + reservations

- [ ] Design `hooks/mail_router` skill:
  - Tools it calls on mcp_agent_mail (fetch_inbox, acknowledge, maybe thread
    digests).
  - Mapping from messages → `(workspace_id, task_id)` memory entries.
- [ ] Implement wrapper script & settings wiring.
- [ ] Optionally extend `task_guard` or a new `file_guard` to use **file
      reservations**.

---
