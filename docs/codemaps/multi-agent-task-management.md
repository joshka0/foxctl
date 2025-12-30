# Multi-Agent Task Management & Coordination System

**ID:** `Multi-Agent_Task_Management___Coordination_System_20251217_134642`

## Description

This map covers the multi-agent workflow system with SQLite-backed task storage,
dependency graph analysis, mailbox coordination, and overseer scoring. Key entry
points include task creation, graph analysis, message coordination, file
conflict detection, and overseer recommendations.

## Traces

### 1. Task Creation with Dependency Validation

Task storage layer - shows how tasks are created, validated against
dependencies, and linked to parent tasks.

```text
Task Creation with Dependency Validation
├── todo/manage skill operation dispatch
│   └── handleAdd() invocation <-- 1a
│       ├── validateDependencies() check <-- 1b
│       │   └── checks all deps exist in index <-- main.go:946
│       ├── store.Add() call <-- 1c
│       │   ├── generate ULID if needed <-- store.go:163
│       │   ├── marshal children & depends_on JSON <-- store.go:178
│       │   └── SQL INSERT execution <-- 1d
│       │       └── persists to tasks table
│       └── parent task update (if has parent) <-- 1e
│           ├── append child ID to parent <-- 1f
│           └── store.Update() call <-- 1g
│               └── persists updated parent
└── return task + all tasks list <-- main.go:495
```

- **[1a] Task add operation dispatch**: Entry point for adding a new task via
  the `todo/manage` skill.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:221`
- **[1b] Dependency validation**: Validates that all dependency task IDs exist
  before creating the task.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:461`
- **[1c] Task store Add method**: Core storage layer that persists tasks to
  SQLite.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:161`
- **[1d] SQL task insertion**: Executes the INSERT statement with all task
  fields including dependencies.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:199`
- **[1e] Parent-child linking**: Updates parent task's children array to
  maintain bidirectional relationship.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:483`
- **[1f] Parent task update**: Persists the updated parent task with new child
  reference.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:484`

### 2. Task Graph Analysis with PageRank & Critical Path

Graph analysis layer - traces how task dependencies are analyzed using gonum to
compute PageRank, critical paths, and detect cycles.

```text
Task Graph Analysis Flow
├── todo/manage skill operation handler
│   └── graph_insights case <-- 2a
│       └── tasksgraph.NewAnalyzer().Analyze()
│
└── tasksgraph.Analyzer
    └── Analyze() entry point <-- 2b
        ├── Build directed graph from tasks
        │   ├── Map task IDs to node IDs <-- graph.go:66
        │   ├── Add nodes to gonum graph <-- graph.go:76
        │   └── Create dependency edges <-- 2c
        │
        ├── Compute graph metrics
        │   ├── network.PageRank() <-- 2d
        │   ├── In/out degree calculation <-- graph.go:104
        │   ├── detectCycles() via Tarjan SCC <-- 2e
        │   ├── computeCriticalPaths() <-- 2f
        │   └── topo.Sort() for ordering <-- graph.go:118
        │
        └── Build & return Insights
            └── NodeMetrics with all scores <-- graph.go:132
```

- **[2a] Graph analysis invocation**: Skill layer calls the graph analyzer with
  workspace tasks.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:320`
- **[2b] Graph analysis entry point**: Main analyzer that builds directed graph
  and computes all metrics.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/tasksgraph/graph.go:49`
- **[2c] Dependency edge creation**: Builds directed edges where task A → B
  means A depends on B.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/tasksgraph/graph.go:89`
- **[2d] PageRank computation**: Uses gonum to compute PageRank scores for task
  importance.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/tasksgraph/graph.go:95`
- **[2e] Cycle detection**: Identifies circular dependencies using Tarjan's SCC
  algorithm.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/tasksgraph/graph.go:109`
- **[2f] Critical path calculation**: Computes longest path from each node to
  any sink for priority scoring.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/tasksgraph/graph.go:114`

### 3. Mailbox Message Coordination Flow

Mailbox/blackboard layer - demonstrates inter-agent messaging with
priority-based inbox retrieval and broadcast support.

```text
Mailbox Message Coordination Flow
├── E2E Test Layer
│   └── Admin sends message <-- 3a
│       └── boardStore.SendMessage() <-- multiagent_workflow_test.go:138
│           └── BoardStore Implementation
│               ├── SendMessage() entry <-- 3b
│               │   ├── Populate msg defaults <-- board_store.go:61
│               │   └── SQL INSERT execution <-- 3c
│               │       └── board_messages table
│               └── Agent checks inbox <-- 3d
│                   └── Inbox() method <-- board_store.go:91
│                       ├── Build SQL query <-- board_store.go:98
│                       ├── Priority ordering <-- 3e
│                       └── Broadcast filter <-- 3f
│                           └── WHERE (recipient = ? 
│                               OR recipient = '*')
```

- **[3a] Admin message sending**: Admin sends high-priority instruction to
  specific agent.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:138`
- **[3b] Message send implementation**: BoardStore persists message with
  priority and recipient info.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:60`
- **[3c] Message SQL insertion**: Stores message in board_messages table with
  all metadata.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:77`
- **[3d] Agent inbox retrieval**: Agent queries their inbox with filtering
  options.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:169`
- **[3e] Priority-based ordering**: Messages sorted by priority (1 highest) then
  recency.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:118`
- **[3f] Broadcast message filtering**: SQL query matches both direct messages
  and broadcasts (*).
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:101`

### 4. File Reservation Conflict Detection

File guard hook system - shows how file reservations prevent concurrent edits
through conflict detection and advisory locking.

```text
File Reservation Conflict Detection Flow
├── Hook System Entry
│   └── run() hook intercepts write op <-- 4a
│       ├── extractFilePath() from tool input <-- 4b
│       └── CheckConflicts() invocation <-- 4c
│           └── BoardStore.CheckConflicts() <-- 4d
│               ├── Clean expired reservations <-- board_store.go:233
│               ├── Build SQL query with paths <-- board_store.go:236
│               ├── Exclusive mode check <-- 4e
│               │   ├── Conflicts with ANY holder <-- board_store.go:254
│               │   └── Query: holder != current <-- board_store.go:256
│               └── Shared mode check <-- board_store.go:257
│                   └── Conflicts only with exclusive <-- board_store.go:261
├── Conflict Decision Branch
│   ├── If conflicts found <-- main.go:133
│   │   ├── Strict mode: block operation <-- main.go:137
│   │   └── Advisory mode: warn but allow <-- main.go:152
│   └── If no conflicts
│       └── Grant reservation <-- 4f
│           └── INSERT into file_reservations <-- board_store.go:215
└── Result: Decision + Context returned <-- main.go:193
```

- **[4a] File guard hook entry**: Hook intercepts write operations before tool
  execution.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_file_guard/main.go:64`
- **[4b] File path extraction**: Parses tool input JSON to get target file path.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_file_guard/main.go:93`
- **[4c] Conflict check invocation**: Queries board store for existing
  reservations on the file.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_file_guard/main.go:125`
- **[4d] Conflict detection logic**: Core reservation conflict detection
  implementation.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:225`
- **[4e] Exclusive mode check**: Exclusive reservations conflict with any
  existing reservation.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/blackboard/board_store.go:252`
- **[4f] Reservation grant**: If no conflicts, creates new reservation for the
  requesting actor.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_file_guard/main.go:180`

### 5. Overseer Task Recommendation Scoring

Overseer scoring system - combines graph metrics (PageRank, critical path) with
mailbox state to prioritize tasks.

```text
Overseer Task Recommendation Flow
├── todo/manage skill layer
│   └── scorer.Recommend() invocation <-- 5a
│       └── passes workspace & limit
├── Overseer Scorer (core algorithm) <-- 5b
│   ├── Filter pending tasks <-- scorer.go:74
│   ├── Get graph insights <-- 5c
│   │   └── analyzer.Analyze() <-- scorer.go:91
│   │       ├── PageRank computation <-- graph.go:95
│   │       └── Critical path scores <-- graph.go:114
│   ├── Collect mailbox stats <-- 5d
│   │   └── getTaskMailCounts() <-- scorer.go:221
│   │       └── CountMessagesByTask() <-- 5e
│   │           ├── Query unread messages <-- board_store.go:437
│   │           └── Group by sender type <-- board_store.go:454
│   ├── Normalize all metrics (0-1) <-- scorer.go:139
│   ├── Apply weighted formula <-- 5f
│   │   ├── 30% critical path <-- scorer.go:19
│   │   ├── 20% PageRank <-- scorer.go:20
│   │   ├── 25% admin messages <-- scorer.go:21
│   │   ├── 15% overseer messages <-- scorer.go:22
│   │   └── 10% recency factor <-- scorer.go:23
│   └── Sort by score descending <-- 5g
└── Return Recommendation <-- scorer.go:211
    ├── TopRecommended task <-- scorer.go:208
    └── Scored task list <-- scorer.go:204
```

- **[5a] Recommendation request**: Skill invokes overseer scorer for task
  recommendations.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/todo/main.go:355`
- **[5b] Scorer recommendation entry**: Main scoring algorithm that combines
  multiple signals.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:65`
- **[5c] Graph insights retrieval**: Gets PageRank and critical path scores from
  graph analyzer.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:91`
- **[5d] Mailbox stats collection**: Retrieves unread message counts per task
  from admin/overseer.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:103`
- **[5e] Message counting by task**: Counts unread messages grouped by sender
  type (admin/overseer).
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:230`
- **[5f] Weighted scoring formula**: Combines normalized metrics: 30% critical
  path, 20% PageRank, 25% admin mail, 15% overseer mail, 10% recency.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:166`
- **[5g] Score sorting**: Sorts tasks by computed score descending for
  recommendations.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/analysis/overseer/scorer.go:190`

### 6. Task Guard Hook with Status Demotion

Task guard hook system - ensures active task exists and demotes reviewed tasks
back to in_progress on new writes.

```text
Task Guard Hook Flow (Write Operation Interception)
├── Hook System Entry
│   └── run() hook entrypoint <-- 6a
│       ├── Check if write operation <-- main.go:57
│       └── Get workspace & actor context <-- main.go:68
├── Active Task Management
│   ├── EnsureActive() call <-- 6b
│   │   └── EnsureActive() implementation <-- 6c
│   │       ├── Check for existing active task <-- store.go:335
│   │       └── Create new task if none exists <-- store.go:350
│   └── Task retrieved or created
├── Review Status Handling
│   ├── DirtyIfReviewed() call <-- 6d
│   │   └── DirtyIfReviewed() logic <-- 6e
│   │       ├── Check task status <-- store.go:372
│   │       ├── Status demotion to in_progress <-- 6f
│   │       └── Mark review as stale <-- 6g
│   └── Task status updated if needed
└── Hook Output
    └── Emit approval with task metadata <-- main.go:119
```

- **[6a] Task guard hook entry**: Hook runs before write operations to enforce
  task-centric workflow.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_task_guard/main.go:55`
- **[6b] Active task enforcement**: Auto-creates or retrieves active task for
  the workspace.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_task_guard/main.go:98`
- **[6c] EnsureActive implementation**: Returns existing active task or creates
  new one if none exists.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:333`
- **[6d] Review status check**: Checks if task needs demotion from reviewed
  status.
  - `@/Users/jkatigbak/repos/personal/agentctl/skills/hooks_task_guard/main.go:106`
- **[6e] DirtyIfReviewed logic**: Demotes ready_for_review/completed tasks back
  to in_progress.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:365`
- **[6f] Status demotion**: Changes task status to in_progress when new writes
  occur.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:377`
- **[6g] Review invalidation**: Marks previous passing review as stale due to
  new changes.
  - `@/Users/jkatigbak/repos/personal/agentctl/internal/storage/tasks/store.go:381`

### 7. End-to-End Multi-Agent Workflow Test

Integration test layer - demonstrates complete workflow from task creation
through graph analysis, messaging, reservations, to recommendations.

```text
E2E Multi-Agent Workflow Test <-- multiagent_workflow_test.go:27
├── Phase 1: Task Creation <-- multiagent_workflow_test.go:32
│   └── taskStore.Add() creates dependency chain <-- 7a
├── Phase 2: Graph Analysis <-- multiagent_workflow_test.go:97
│   └── analyzer.Analyze() computes metrics <-- 7b
├── Phase 3: Mailbox Messaging <-- multiagent_workflow_test.go:126
│   ├── boardStore.SendMessage() admin msg <-- 7c
│   └── boardStore.Inbox() retrieves messages <-- multiagent_workflow_test.go:169
├── Phase 4: File Reservations <-- multiagent_workflow_test.go:196
│   ├── boardStore.Reserve() by coder1 <-- 7d
│   └── boardStore.CheckConflicts() by coder2 <-- 7e
└── Phase 5: Overseer Recommendations <-- multiagent_workflow_test.go:249
    ├── scorer.Recommend() combines signals <-- 7f
    └── Verify admin message boost score <-- 7g
```

- **[7a] Task graph creation**: Creates dependency chain: A → B → C (A depends
  on B, B depends on C).
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:44`
- **[7b] Graph analysis phase**: Analyzes task graph for PageRank and critical
  path metrics.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:100`
- **[7c] Admin coordination message**: Admin sends priority instruction about
  Task C to coder agent.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:138`
- **[7d] File reservation phase**: Coder1 reserves file for exclusive editing
  during Task C work.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:199`
- **[7e] Conflict detection test**: Coder2 attempts to reserve same file,
  detects conflict.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:214`
- **[7f] Overseer recommendation**: Overseer scores all tasks combining graph
  metrics and mailbox state.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:252`
- **[7g] Verification of admin boost**: Confirms Task C has high score due to
  admin message priority.
  - `@/Users/jkatigbak/repos/personal/agentctl/test/e2e/multiagent_workflow_test.go:283`
