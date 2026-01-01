---
name: agentctl Orchestration
description: Multi-agent coordination with agentctl - spawn agents, send messages, prioritize tasks, lock files. Use when running tasks in parallel, coordinating agents, or preventing edit conflicts.
---

# Agent Orchestration

Use this skill for multi-agent workflows, messaging, task prioritization, and file locking.

**Trigger phrases**: "run in parallel", "spawn an agent", "coordinate agents", "send message to", "prioritize tasks", "prevent conflicts", "lock this file", "check for conflicts"

## Spawn Agents

```bash
# Spawn subagent with role
agentctl run agent/spawn --input '{
  "epic_id": "epic-123",
  "requested_subagents": [
    {"role": "coder", "reason": "Implement auth", "local_max_depth": 2}
  ]
}'

# List agents
agentctl agent list

# View hierarchy
agentctl agent hierarchy --session <session-id>
```

Roles: `planner`, `coder`, `reviewer`, `researcher`

## Mailbox Messaging

```bash
# Send message
agentctl run mailbox --input '{
  "operation": "send",
  "send": {
    "sender": "actor:coder:agent1",
    "recipient": "actor:coder:agent2",
    "subject": "Review needed",
    "body": "Please review auth.go",
    "priority": 1
  }
}'

# Check inbox
agentctl run mailbox --input '{"operation": "inbox", "inbox": {"actor_id": "actor:coder:agent1"}}'

# Broadcast to all
agentctl run mailbox --input '{"send": {"recipient": "*", "subject": "Update", "body": "..."}}'
```

Priority: 1=urgent, 2=high, 3=normal

## Task Prioritization

```bash
# Get prioritized recommendations
agentctl run todo/manage --input '{"operation": "recommend", "limit": 5}'

# Graph analysis (PageRank, critical path)
agentctl run todo/manage --input '{"operation": "graph_insights"}'
```

Scoring: 30% critical path, 20% PageRank, 25% admin messages, 15% overseer messages, 10% recency

## File Reservations

```bash
# Reserve files (prevent conflicts)
agentctl run mailbox --input '{
  "operation": "reserve",
  "reserve": {"actor_id": "agent1", "paths": ["auth.go"], "mode": "exclusive"}
}'

# Check conflicts before editing
agentctl run mailbox --input '{
  "operation": "check_conflicts",
  "check_conflicts": {"actor_id": "agent1", "paths": ["auth.go"]}
}'

# Release when done
agentctl run mailbox --input '{"operation": "release", "release": {"paths": ["auth.go"]}}'
```

Modes: `exclusive` (only holder edits), `shared` (multiple readers)

Full docs: See individual skill docs in `configs/skills/`
