---
name: tmux-bridge
description: Cross-pane communication for Codex, Claude, and other terminal agents. Use `agentctl tmux create/read/send/observe` for tmux, `agentctl room` for durable room chat, and the plugin-backed zellij room relay for session-aware fanout.
metadata:
  openclaw:
    emoji: "🌉"
    os:
      - darwin
      - linux
    requires:
      bins:
        - tmux
        - agentctl
---

# tmux-bridge

Use this skill when multiple terminal agents are running in tmux and need to:

- open or switch into a prepared collaboration session
- inspect another pane with structured `agentctl` output
- send a direct message into another pane without depending on a repo-local shell script
- promote a bridge exchange into ACA as an observation
- share a durable room timeline or room-scoped tasks across panes

## Default Flow

Prepare a neutral multi-agent session:

```bash
agentctl tmux create --session agentctl-collab --panes 3 --attach
```

If you do not pass `--agent`, this creates panes labeled `agent-a`, `agent-b`, `agent-c`.
If you pass `--agent claude`, `--agent codex`, `--agent gemini`, `--agent agent`, or `--agent droid`, the default labels become `claude-a`, `codex-a`, `gemini-a`, `agent-a`, or `droid-a`.
Use `--label-prefix` only when you want to override that default.

Launch a specific agent CLI in every pane:

```bash
agentctl tmux create --session codex-collab \
  --panes 3 \
  --agent codex \
  --agent-arg=--model \
  --agent-arg=gpt-5 \
  --agent-arg=--full-auto \
  --attach

agentctl tmux create --session claude-collab \
  --panes 3 \
  --agent claude \
  --agent-arg=--model \
  --agent-arg=sonnet \
  --agent-arg=--permission-mode \
  --agent-arg=default \
  --attach

agentctl tmux create --session gemini-collab \
  --panes 3 \
  --agent gemini \
  --agent-arg=--model \
  --agent-arg=gemini-2.5-pro \
  --agent-arg=--approval-mode \
  --agent-arg=auto_edit \
  --attach

agentctl tmux create --session cursor-collab \
  --panes 3 \
  --agent agent \
  --agent-arg=--model \
  --agent-arg=claude-sonnet-4 \
  --attach

agentctl tmux create --session droid-collab \
  --panes 3 \
  --agent droid \
  --attach
```

For known autonomous mappings, use `--mode auto` instead of hand-writing the
provider flag:

```bash
agentctl tmux create --session codex-auto --panes 2 --agent codex --mode auto --attach
agentctl tmux create --session claude-auto --panes 2 --agent claude --mode auto --attach
agentctl tmux create --session gemini-auto --panes 2 --agent gemini --mode auto --attach
agentctl tmux create --session cursor-auto --panes 2 --agent agent --mode auto --attach
```

This changes autonomy/approval flags only. It does not inject `--model`; the
provider CLI keeps its current configured default model unless you explicitly
override it with repeated `--agent-arg` values.

`--agent-arg` is repeatable and preserves order, so it works with each CLI’s own flags instead of forcing one shared option schema.
For Cursor CLI (`agent`) and Droid (`droid`), you can also switch models from inside the session using their own slash commands such as `/model`.

Resume an existing Codex or Claude session in tmux:

```bash
agentctl tmux create --session codex-resume \
  --panes 1 \
  --agent codex \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach

agentctl tmux create --session claude-resume \
  --panes 1 \
  --agent claude \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach
```

`--agent-session-id` currently supports `codex` and `claude` only, and requires `--panes 1` so one resumed session is not duplicated across multiple panes.

For child agents, launch panes with parent metadata instead of adding them to a
shared room directly:

```bash
agentctl tmux create --session codex-tree \
  --panes 1 \
  --agent codex \
  --mode auto \
  --label-prefix child \
  --parent-participant codex-a \
  --parent-agent-id agent:parent-1
```

That exports stable mux identity into the pane environment:

- `AGENTCTL_PARTICIPANT_ID`
- `AGENTCTL_PARENT_PARTICIPANT_ID`
- `AGENTCTL_PARENT_AGENT_ID`
- `AGENTCTL_MUX_BACKEND`
- `AGENTCTL_MUX_SESSION`
- `AGENTCTL_MUX_PANE_ID`

If `--room-access direct` is used, the pane also gets `AGENTCTL_ROOM_ID`.

## Structured Access

Prefer `agentctl` when you want machine-friendly envelopes:

```bash
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux send agent-b "Please review internal/storage/mailbox/store.go for lease races."
agentctl tmux observe agent-b --lines 80
agentctl tmux doctor
```

When sending from outside tmux, specify your sender pane label:

```bash
agentctl tmux send agent-b "Please review internal/storage/mailbox/store.go for lease races." --sender agent-a
```

Use `agentctl tmux observe` when a tmux exchange should become a durable ACA observation.

When a child pane needs to talk upward, prefer the private parent path:

```bash
agentctl tmux send-parent "Need a decision on the retry backoff helper."
```

That uses `AGENTCTL_PARENT_PARTICIPANT_ID` instead of guessing who the parent is.

## Durable Rooms

Use rooms when the shared state should outlive pane scrollback:

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review mailbox retries."
agentctl room task add alpha --title "Refactor mailbox retries"
agentctl room subscribe alpha --follow
```

Relay room messages back into panes with:

```bash
agentctl room relay alpha --backend tmux
agentctl room relay alpha --backend zellij --session alpha-room
agentctl room loop alpha --backend tmux
```

`room loop` extends relay with a central task/status loop, so room-associated
task completion gets broadcast to the whole room.

## Direct Messaging

The bundled script is optional for lower-level pane control:

```bash
./scripts/tmux-bridge read agent-b 40
./scripts/tmux-bridge type agent-b "Please review internal/storage/mailbox/store.go for lease races."
./scripts/tmux-bridge keys agent-b Enter
```

`agentctl tmux send` is the default for agent-to-agent messaging:

- it resolves the sender pane from the current tmux pane or `--sender`
- it prepends the stable `[tmux-bridge from=...]` header
- it presses Enter for you

`agentctl room ...` now follows the same identity rule: derive the current pane
participant first, then fall back to canonical ids like `tmux:<session>:%7` or
`zellij:<session>:terminal_3` when no human-friendly pane name is present.

Room policy is intentionally asymmetric:

- top-level panes can join rooms
- child panes should stay parent-private by default
- parents decide what gets promoted into the room

Use `type` plus `keys` only when you intentionally need manual control, such as interacting with a non-agent prompt.

## Read Guard

The bridge enforces read-before-act:

1. `read` marks a target as safe to interact with
2. `send`, `type`, and `keys` fail if you did not read first
3. each successful action clears the read mark

That means the safe manual cycle is:

```bash
./scripts/tmux-bridge read agent-b 20
./scripts/tmux-bridge type agent-b "y"
./scripts/tmux-bridge read agent-b 20
./scripts/tmux-bridge keys agent-b Enter
```

## Do Not Poll Agent Panes

When you send to another agent pane, do not poll that pane for the reply.
The intended pattern is that the other agent sends a bridge message back into your pane.

Read the target pane again only when:

- you need to verify manually typed text before pressing Enter
- the target is a non-agent pane
- you are explicitly inspecting its state

## Reply Convention

Bridge `send` already frames the message for you.
If you are replying manually with `type`, keep the stable header format:

```text
[tmux-bridge from=agent-b pane=%4 reply_to=agent-b] I reviewed the mailbox code; the lease path looks safe.
```

## ACA Fit

tmux is the live coordination plane.
ACA is the durable continuity plane.

Promote only derived facts:

```bash
agentctl tmux observe agent-b --lines 80
agentctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

Do not treat raw pane scrollback as canonical history.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
