---
title: Agent lifecycle
description: Spawn, inspect, ask, watch, and resume foxctl agents.
---

Status: Current with mixed legacy/v2 runtime details linked.

The agent daemon persists agents, mailbox messages, and conversation state
across engine runs. Production docs should always name which dispatcher/runtime
path a command exercises.

## Spawn

```bash
foxctl agent spawn --role researcher \
  --prompt "Research the hook system architecture" \
  --exec-mode proactive \
  --max-auto-turns 3 \
  --max-iterations 20
```

Classic run path:

```bash
foxctl agent run <agent-id>
```

## Inspect and watch

```bash
foxctl agent info <agent-id>
```

```bash
foxctl agent watch <agent-id>
```

## Ask

```bash
foxctl agent ask <agent-id> --question "What did you find?" --wait
```

## Resume

```bash
foxctl agent resume <session-id> --prompt "Continue from the prior summary"
```

## Runtime notes

- Reactive agents wait for mailbox messages.
- Autonomous agents run initial work and bounded continuation turns.
- Proactive agents run autonomous work and keep polling.
- Tick mode is used for explicit scheduler-style progression.
- `agent run` still exercises the classic daemon path.
- `agent ask` uses the v2 ask service with mailbox or Jido dispatcher paths.
- Jido-backed ask/dispatch findings are not evidence about classic `agent run`
  unless that dispatcher path was used.

## Canonical sources

- [docs/general/agent-daemon.md](https://github.com/joshka0/foxctl/blob/main/docs/general/agent-daemon.md)
- [docs/general/runtime-orchestration.md](https://github.com/joshka0/foxctl/blob/main/docs/general/runtime-orchestration.md)
- [docs/spec/v1/agent_profile_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/agent_profile_v1.md)
