---
name: agentctl Multi-Agent
description: Spawn and coordinate subagents with depth limits, roles, and hierarchy tracking. Manage agent sessions.
---

# Multi-Agent Coordination

Hierarchical agent spawning with depth limits and role-based coordination.

## Agent Hierarchy

```
Depth 0: Overseer (actor:system:overseer)
  ├── Depth 1: Planner Agent
  │    └── Depth 2: Coder Subagent
  └── Depth 1: Coder Agent
```

## Spawn Request

```json
{
  "tool": "agent.spawn",
  "args": {
    "epic_id": "epic-123",
    "requested_subagents": [
      {"role": "coder", "reason": "Implement auth module", "local_max_depth": 2}
    ]
  }
}
```

## Roles

`planner`, `coder`, `reviewer`, `researcher`

## Depth Controls

- **Depth**: Current level (0 = root)
- **MaxDepth**: Global cap (default: 3)
- **LocalMaxDepth**: Subtree cap

## CLI

```bash
agentctl agent list
agentctl agent hierarchy --session <session-id>
```

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-agents/Skill.md`
