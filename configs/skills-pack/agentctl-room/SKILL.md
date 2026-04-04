---
name: agentctl-room
description: "Durable multi-agent room coordination with shared chat, plugin-backed zellij/tmux relay, room tasks, and a central room loop that broadcasts task completion."
---

## What I do

- Keep a shared room timeline durable in `agentctl`, not trapped in pane scrollback.
- Relay room messages into `tmux` or `zellij` panes.
- Track room-scoped tasks and broadcast completion/status changes to participants.

## When to use me

- More than two agents need the same shared conversation.
- You want a central chat room with live terminal fanout.
- You want shared todos visible to everyone in the room.
- You want task completion to be announced automatically.

## Mental model

- `agentctl room` is the source of truth.
- `room send` writes durable chat messages.
- `room relay` mirrors room messages into terminal panes.
- `room task` links shared tasks to the room.
- `room loop` runs the central coordination loop:
  - relay new messages
  - watch room tasks
  - broadcast task status changes back into the room

Do not rely on scrollback as canonical history. The room log is canonical.

## Quick start

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review the retry path in client.ts" --sender agent-a
agentctl room subscribe alpha --follow
```

## Shared task flow

```bash
agentctl room task add alpha \
  --sender agent-a \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery branches"

agentctl room task list alpha

agentctl room task complete alpha \
  --sender agent-b \
  --id <task-id> \
  --notes "Retry helper extracted"
```

This writes task lifecycle events back into the room timeline so everyone sees them.

## Live relay

### tmux

```bash
agentctl room relay alpha --backend tmux
agentctl room loop alpha --backend tmux
```

### zellij

```bash
agentctl room relay alpha --backend zellij --session alpha-room
agentctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a local plugin and matches room member ids to zellij pane titles.

## Conventions

- Use stable actor ids like `agent-a`, `agent-b`, `reviewer`, `planner`.
- In `tmux`, room member ids should match pane labels.
- In `zellij`, room member ids should match pane titles.
- The sender should also be a room member if you want them excluded from fanout.

## Typical pattern

```bash
# create durable room
agentctl room create review --title "Review Room"

# register participants
agentctl room join review codex-a --role lead
agentctl room join review claude-b --role reviewer
agentctl room join review gemini-c --role observer

# start the central loop
agentctl room loop review --backend tmux

# add shared work
agentctl room task add review --sender codex-a --title "Audit retry ladder"

# chat in the room
agentctl room send review "Please check the 401 fallback branch." --sender codex-a
```

## Limits / caveats

- The room log is durable, but relay delivery is best-effort.
- `room relay` only mirrors messages; it does not infer task changes.
- `room loop` is the higher-level coordinator when you want automatic task broadcasts.
- The zellij path requires the room relay plugin to be available and permission-granted.

## Related

- `configs/skills-pack/agentctl-orchestrate/SKILL.md`
- `configs/skills-pack/agentctl-tmux/SKILL.md`
- `docs/general/tmux-collaboration.md`
