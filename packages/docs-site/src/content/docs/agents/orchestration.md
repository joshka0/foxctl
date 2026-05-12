---
title: Overseer and orchestration
description: Coordinate subagents through explicit hierarchy, spawn, and result aggregation rules.
---

Status: Current spec shell, active room-agile plans remain planned.

foxctl's multi-agent model is overseer-first. The overseer owns plan changes
and cross-agent coordination; child agents request work through documented
spawn and mail flows.

## Hierarchy rules

| Concept | Production meaning |
|---|---|
| Overseer | Root coordinator for plan and spawn decisions |
| Child agent | Scoped worker or researcher with bounded authority |
| Depth limits | Guardrails that prevent unmanaged spawn trees |
| Mailbox | Durable coordination channel |
| Result aggregation | Parent-owned synthesis of child outputs |

## Common checks

```bash
foxctl agent hierarchy <session-id>
```

```bash
foxctl agent list
```

```bash
foxctl agent info <agent-id>
```

## Documentation stance

- Current hierarchy and overseer behavior belongs in current docs.
- Room-agile milestones, workpacks, and evidence lanes should remain planned
  until the corresponding implementation docs are promoted.
- Subagent prompts should state scope, non-goals, verification, and stop
  conditions.

## Canonical sources

- [docs/spec/agent_hierarchy.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/agent_hierarchy.md)
- [docs/spec/overseer_profile.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/overseer_profile.md)
- [docs/spec/mailbox_blackboard.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/mailbox_blackboard.md)
- [docs/general/runtime-orchestration.md](https://github.com/joshka0/foxctl/blob/main/docs/general/runtime-orchestration.md)

