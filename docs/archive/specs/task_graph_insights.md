# Task Graph Insights Spec (Draft)

## Overview

Gonum-style directed task graph and metrics (PageRank-like importance, impact,
critical path) for `agentctl` tasks. The graph is per-workspace and is used to:

- Prioritize which tasks agents and humans should focus on.
- Power the overseer/coordinator and admin views.
- Bias memory retrieval and context injection toward high-impact work.

This spec is storage- and implementation-agnostic, but assumes a graph library
with standard centrality and topological analysis functions.

## Goals

- Define a consistent graph model over tasks in a workspace.
- Provide a concrete set of node- and graph-level metrics (centrality, degrees,
  critical path, cycles).
- Expose these insights via `todo/manage`-style operations for use by agents,
  hooks, and UIs.
- Enable integration with the mailbox / overseer system for prioritization and
  coordination.
- Keep computation **local** and independent of Beads, with optional bridging
  paths later.

## Non-Goals

- Replace the existing tasks storage schema.
- Implement a full scheduler or automatic task assignment; this spec is about
  **insights**, not mandatory orchestration.
- Define cross-workspace graphs; all graphs are scoped to a single
  `workspace_id`.
- Reuse Beads storage or `beads_viewer` code directly; we copy concepts and
  metrics, not implementations.

## Data Model

This section describes the **logical** structures used in APIs and internal
reasoning.

### Tasks

Analysis operates over the existing task records for a given workspace. Each
task **conceptually** provides at least:

- `id` (string) – Task identifier.
- `workspace_id` (string).
- `title` (string).
- `status` (string) – e.g. `pending`, `in_progress`, `done`, `blocked`.
- `depends_on` (list of string task IDs) – Zero or more upstream tasks.
- `parent_id` (string, optional) – For hierarchical grouping/epics.

Future extensions may introduce explicit dependency types (blocks vs related vs
parent-child). This spec assumes a single blocking-like dependency type for
graph purposes.

### Graph

For each `workspace_id`, analysis builds a directed graph:

- **Nodes**: tasks in the workspace.
- **Edges**: a directed edge `u -> v` if task `u` depends on task `v`.

Semantics:

- Edge direction matches execution order: `u -> v` means "u cannot be completed
  until v is satisfied".
- All entries in `depends_on` are treated as **blocking** for purposes of
  centrality and critical path.
- `parent_id` may be used for grouping and display, but does not necessarily
  introduce an edge unless explicitly configured in future revisions.

Cycles are allowed in storage but are considered **structural issues** by the
analyzer and must be surfaced as warnings.

## Graph Construction

Graph construction steps (conceptually):

1. **Load tasks**
   - Fetch all tasks for a given `workspace_id`.
   - Filter out tasks that are hard-deleted; closed/completed tasks may
     optionally remain in the graph for historical context, but implementers
     SHOULD provide a way to focus on active tasks only.

2. **Create nodes**
   - For each task, create a graph node keyed by `task_id`.

3. **Create edges from dependencies**
   - For each task `t` and each `d` in `t.depends_on`, add edge `t -> d` if `d`
     exists in the workspace.
   - Missing dependency targets SHOULD be logged and ignored, not fatal.

4. **Handle parent/child relationships**
   - For now, `parent_id` is treated as **metadata only** for analysis.
   - Future revisions MAY introduce an option to treat parent-child
     relationships as blocking edges or a separate view of the graph.

5. **Detect cycles**
   - Run a cycle detection algorithm over the graph.
   - Cycles MUST be surfaced in the output (list of task-id sequences).
   - The analyzer MUST also compute strongly connected components (SCCs) and
     conceptually treat each SCC as a single super-node for metrics that assume
     a DAG (e.g. critical path).

## Metrics

Metrics are computed over the constructed graph. Implementations MAY use any
graph library, but SHOULD adhere to the semantics below.

### Node-level metrics

All node-level metrics are keyed by `task_id`.

- **PageRank** (or similar) – `pagerank[task_id]`
  - Stationary distribution centrality computed over the directed graph.
  - Intuition: tasks that many important tasks depend on will have higher
    scores.

- **In-Degree** – `in_degree[task_id]`
  - Count of incoming edges (number of tasks that depend on this task).
  - Intuition: high in-degree → high impact / many dependents.

- **Out-Degree** – `out_degree[task_id]`
  - Count of outgoing edges (number of tasks this task depends on).
  - Intuition: high out-degree → more fragile / heavily blocked.

- **Critical Path Score** – `critical_path_score[task_id]`
  - Heuristic reflecting how deep a task sits on dependency chains.
  - Computed on the DAG of SCC super-nodes from cycle detection; a common
    formulation is the longest distance (in edges) from the task's SCC to any
    sink SCC reachable following dependencies.
  - Higher scores indicate tasks that gate long chains of work; tasks within the
    same SCC MAY share the same base score.

Implementations MAY compute additional metrics (e.g. betweenness, eigenvector,
HITS) but they are not required for v1 and SHOULD be surfaced as optional fields
if present.

### Graph-level metrics

Graph-level outputs include:

- **Topological Order** – `topological_order`
  - An ordering of task IDs consistent with dependencies if the graph is a DAG.
  - If cycles exist, this field MAY instead reflect a topological ordering of
    SCC super-nodes, or be omitted/marked incomplete.

- **Cycles** – `cycles`
  - List of cycles, each represented as an ordered list of `task_id`s.
  - Intended for diagnostics and surfaced prominently to humans.

Additional aggregate metrics (e.g. density, number of connected components) MAY
be included but are non-normative.

## APIs / Skills

The primary consumer-facing entrypoint is a `todo/manage` operation that returns
graph insights for a workspace.

### `todo/manage.graph_insights`

- **Command**: `todo/manage` with `operation = "graph_insights"`.
- **Inputs** (data fields):
  - `workspace_id` (required).
  - Optional filters (non-normative in v1):
    - `include_completed` (bool, default false).
    - `limit` (int) – limit number of nodes in the response, e.g. top-N by
      critical path score.

- **Outputs** (`data` payload, conceptual schema):

  ```jsonc
  {
    "workspace_id": "...",
    "generated_at": "2025-11-27T12:34:56Z",
    "nodes": [
      {
        "task_id": "T-123",
        "title": "...",
        "status": "pending",
        "pagerank": 0.0123,
        "critical_path_score": 5.0,
        "in_degree": 3,
        "out_degree": 1
      }
      // ...
    ],
    "topological_order": ["T-1", "T-2", "T-3"],
    "cycles": [
      ["T-10", "T-20", "T-10"]
    ]
  }
  ```

Implementations MAY add additional per-node or graph-level fields as long as
they do not break existing consumers.

## CLI UX

CLI commands are thin wrappers over the `todo/manage` operation.

Non-normative examples:

- `agentctl todo insights` – Show top-K tasks by impact.
  - Internally calls `todo/manage.graph_insights`.
  - Displays a table with columns like `task_id`, `title`, `status`,
    `critical_path_score`, `in_degree`, `out_degree` and a short human-readable
    explanation.

- `agentctl todo list --sort impact` – Sorts the existing task list by
  `critical_path_score` (and/or `pagerank`).
  - May reuse cached graph metrics if available.

- `agentctl todo graph --json` – Emit raw graph insights JSON (for debugging or
  piping into other tools).

Exact flag names and subcommand shapes are left to implementation, as long as
they ultimately rely on the common `graph_insights` operation.

## Integration with Memory & Hooks

Task graph insights are intended to inform **what** context is surfaced to
agents and humans, and **in what order**.

### Memory ranking and context injection

The future `hooks/context_inject` skill can combine graph metrics with semantic
similarity and recency when choosing which memory entries to surface.

Given a `(workspace_id, active_task_id)` and a user query:

1. Call `todo/manage.graph_insights` for the workspace (or use cached results).
2. Identify a local **neighborhood** of tasks: the active task, its
   dependencies, and its dependents (and optionally nearby nodes in topological
   order).
3. Fetch memory entries associated with these task IDs from the memory store.
4. Score each memory entry using a combination such as:

   ```text
   memory_score =
       w1 * semantic_similarity(query, memory.embedding)
     + w2 * normalized(critical_path_score or pagerank of the owning task)
     + w3 * recency(memory.timestamp)
     + w4 * kind_weight(memory.kind)
   ```

5. Inject the highest-scoring entries into `hook.Output.Context` (and/or CAS
   artifacts) with concise summaries.

This ties long-horizon planning (critical-path tasks) to what the model sees in
each interaction.

### Overseer, mailbox, and viewer

The overseer and admin tools defined in the `mailbox_blackboard.md` spec rely on
this graph:

- The overseer uses `graph_insights` to identify high-impact or neglected tasks,
  then sends messages via the mailbox (e.g. "Focus on T-123 next").
- The agent viewer combines `graph_insights` with mailbox stats to present
  dashboards of tasks sorted by impact and annotated with unread admin /
  overseer messages.

The scoring formulas in `mailbox_blackboard.md` SHOULD treat
`critical_path_score` (and optionally `pagerank`) as primary indicators of a
task's structural importance.

### Hooks beyond context_inject

Other hooks may consume graph insights, for example:

- **Knowledge router** – Prefer knowledge packs associated with high-impact
  tasks and their dependencies.
- **Task guard / creation helpers** – Warn if new tasks are added that create
  suspicious cycles or extremely deep dependency chains.

These integrations are optional in v1 but SHOULD consider the same graph model
and metrics defined above.
