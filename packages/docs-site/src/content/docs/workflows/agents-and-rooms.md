---
title: Agents and rooms
description: Agent daemon, overseer hierarchy, ask/reply flow, and room workflows.
---

Status: Current shell with mixed current and planned room-agile details.

foxctl supports local and durable agent coordination. Production docs must be
precise about which runtime path a command exercises.

## Agent daemon

Common commands:

```bash
foxctl agent spawn --role researcher --prompt "Research the hook system architecture" --exec-mode autonomous --max-auto-turns 3
```

```bash
foxctl agent ask <agent-id> --question "What did you find?" --wait
```

```bash
foxctl agent info <agent-id>
```

## Overseer model

The overseer owns cross-agent coordination and spawn control. Non-overseer
agents should request subagents through the documented spawn workflow instead of
creating unmanaged sessions directly.

## Rooms

Room workflows should distinguish:

- transport state from viewer state
- durable tasks from chat messages
- current room behavior from active room-agile plans
- evidence and milestone policy from implementation proposals

## Canonical sources

- [docs/general/agent-daemon.md](https://github.com/joshka0/foxctl/blob/main/docs/general/agent-daemon.md)
- [docs/spec/agent_hierarchy.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/agent_hierarchy.md)
- [docs/spec/overseer_profile.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/overseer_profile.md)
- [docs/spec/v1/agent_profile_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/agent_profile_v1.md)
