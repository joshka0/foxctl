---
name: agentctl-room
description: "Durable multi-agent room coordination with shared chat, direct requests, actionable inboxes, plugin-backed zellij/tmux relay, room tasks, and a central room loop that broadcasts task transitions."
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
- `room send --to <participant>` writes a direct room request instead of a broadcast.
- `room ack` marks a specific room message as acknowledged.
- `room inbox` shows actionable direct requests and pending ack/reply work for one participant.
- `room status` shows the coordinator-facing room pulse: participants, task counts, stale work, and backlog.
- `room relay` mirrors room messages into terminal panes.
- `room task` links shared tasks to the room.
- `room loop` runs the central coordination loop:
  - relay new messages
  - watch room tasks
  - broadcast task status changes back into the room
  - nudge stale direct requests and stale claimed tasks with reminder pulses

Do not rely on scrollback as canonical history. The room log is canonical.

Default room policy:

- the participant who creates the room becomes `coordinator` when agentctl can derive the current pane identity
- top-level agents may join the room
- child panes stay parent-private by default
- parents forward child summaries or task results into the room when appropriate

## Quick start

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review the retry path in client.ts"
agentctl room send alpha "Claude, please review the retry path." --to claude-a --reply-expected
agentctl room ack alpha <message-id>
agentctl room inbox alpha --actor claude-a
agentctl room status alpha
agentctl room subscribe alpha --follow
```

## Shared task flow

```bash
agentctl room task add alpha \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery branches"

agentctl room task list alpha

agentctl room task assign alpha --id <task-id> --to gemini-a --notes "Take first pass"
agentctl room task claim alpha --id <task-id>
agentctl room task touch alpha --id <task-id>
agentctl room task block alpha --id <task-id> --reason "waiting on benchmark data"
agentctl room task unblock alpha --id <task-id>

agentctl room task complete alpha \
  --id <task-id> \
  --notes "Retry helper extracted"
```

This writes task lifecycle events back into the room timeline so everyone sees them.

Task lifecycle is intentionally lightweight:

- `pending -> in_progress -> blocked -> completed`
- `assign` records intended ownership and sends a direct task request, but the assignee still claims the task explicitly
- assigned tasks are claimable only by the assignee until they are reassigned, reclaimed, or abandoned
- `abandon` returns a task to `pending`
- `complete` requires the current participant to claim the task first
- `touch` refreshes the owner heartbeat without changing task state
- `block` / `unblock` preserve ownership while making the stall visible to everyone

Use `room task claim` before doing real work. That is the guardrail that keeps multiple top-level agents from stepping on the same task.

## Live relay

### tmux

```bash
agentctl room relay alpha --backend tmux
agentctl room loop alpha --backend tmux --pulse 30s --reply-stale 2m --task-stale 5m
```

### zellij

```bash
agentctl room relay alpha --backend zellij --session alpha-room
agentctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a local plugin and matches room member ids to zellij pane titles or canonical pane ids.

## Conventions

- Use stable actor ids like `agent-a`, `agent-b`, `reviewer`, `planner` when you want human-friendly names.
- `room send` and `room task` derive the sender from the current tmux/zellij pane when possible.
- Broadcast room messages should not expect a response.
- Use `--to <participant>` plus `--reply-expected` for direct asks.
- Use `--to <participant>` plus `--ack-required` when you only need confirmation.
- Use `room inbox` as the actionable queue for each participant; it is not a full archive.
- `room join <room-id> --current` registers the current pane without hand-writing the id.
- In `tmux`, room member ids can be pane labels or canonical ids like `tmux:<session>:%7`.
- In `zellij`, room member ids can be pane titles or canonical ids like `zellij:<session>:terminal_3`.
- The sender should also be a room member if you want them excluded from fanout.
- Child panes launched with `agentctl tmux create --parent-participant ...` should usually use `agentctl tmux send-parent ...` instead of joining the room directly.

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
agentctl room task add review --title "Audit retry ladder"

# chat in the room
agentctl room send review "Please check the 401 fallback branch."
```

## Limits / caveats

- The room log is durable, but relay delivery is best-effort.
- `room relay` only mirrors messages; it does not infer task changes.
- `room loop` is the higher-level coordinator when you want automatic task broadcasts and reminder pulses.
- The zellij path requires the room relay plugin to be available and permission-granted.

## Related

- `configs/skills-pack/agentctl-orchestrate/SKILL.md`
- `configs/skills-pack/agentctl-tmux/SKILL.md`
- `docs/general/tmux-collaboration.md`
