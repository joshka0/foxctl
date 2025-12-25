---
name: agentctl Overseer
description: Multi-agent orchestration with task scoring, graph analysis, and recommendations. Prioritize tasks using PageRank and critical path.
---

# Overseer Coordination with agentctl

Intelligent task prioritization combining graph metrics with coordination
signals.

## Get Task Recommendations

```bash
agentctl run todo/manage --input '{
  "operation": "recommend",
  "workspace_id": "/path/to/workspace",
  "limit": 5
}'
```

Returns scored tasks with:

- `score` - Combined priority score (0-1)
- `critical_path` - Position on longest dependency chain
- `pagerank` - Task importance from graph centrality
- `unread_admin` - Admin message count (high weight)
- `unread_overseer` - Overseer message count

## Scoring Formula

Tasks are scored using weighted combination:

- **30%** Critical path position
- **20%** PageRank centrality
- **25%** Admin message count
- **15%** Overseer message count
- **10%** Recency factor

## Graph Analysis

Analyze task dependency graph:

```bash
agentctl run todo/manage --input '{
  "operation": "graph_insights",
  "workspace_id": "/path/to/workspace"
}'
```

Returns:

- `pagerank` - Per-task importance scores
- `critical_path` - Longest path scores
- `cycles` - Detected circular dependencies
- `topological_order` - Valid execution order

## Cycle Detection

Uses Tarjan's strongly connected components algorithm to detect:

- Multi-task cycles (A→B→C→A)
- Self-loops (A→A)

Cycles block task completion ordering.

## Agent Hierarchy

The overseer manages agent spawning with depth limits:

```
Depth 0: Overseer (planner role)
  └── Depth 1: Agent (coder role)
       └── Depth 2: Subagent (coder role)
```

Default max depth: 3 levels

## Integration with Mailbox

Admin messages to specific tasks boost their priority score. Use mailbox to:

1. Send priority directives
2. Query task-specific messages
3. Coordinate between agents

## Example Workflow

```bash
# 1. Create tasks with dependencies
agentctl todo add --title "Task A" --depends-on "task-b-id"

# 2. Send admin priority message
agentctl run mailbox --input '{
  "operation": "send",
  "send": {
    "sender": "actor:system:admin",
    "recipient": "actor:coder:agent1",
    "task_id": "task-c-id",
    "subject": "Focus here",
    "priority": 1
  }
}'

# 3. Get recommendations (Task C should score highest)
agentctl run todo/manage --input '{"operation": "recommend"}'
```
