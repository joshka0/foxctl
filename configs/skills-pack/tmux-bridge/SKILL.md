---
name: tmux-bridge
description: Cross-pane communication for Codex, Claude, and other terminal agents in tmux. Use `agentctl tmux prepare/read/observe` for structured access and `./scripts/tmux-bridge` for direct pane messaging.
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
- send a direct message into another pane
- promote a bridge exchange into ACA as an observation

## Default Flow

Prepare a neutral multi-agent session:

```bash
agentctl tmux prepare --session agentctl-collab --panes 3 --attach
```

If you do not pass `--agent`, this creates panes labeled `agent-a`, `agent-b`, `agent-c`.
If you pass `--agent claude`, `--agent codex`, or `--agent gemini`, the default labels become `claude-a`, `codex-a`, or `gemini-a`.
Use `--label-prefix` only when you want to override that default.

Launch a specific agent CLI in every pane:

```bash
agentctl tmux prepare --session codex-collab \
  --panes 3 \
  --agent codex \
  --agent-arg=--model \
  --agent-arg=gpt-5 \
  --agent-arg=--full-auto \
  --attach

agentctl tmux prepare --session claude-collab \
  --panes 3 \
  --agent claude \
  --agent-arg=--model \
  --agent-arg=sonnet \
  --agent-arg=--permission-mode \
  --agent-arg=default \
  --attach

agentctl tmux prepare --session gemini-collab \
  --panes 3 \
  --agent gemini \
  --agent-arg=--model \
  --agent-arg=gemini-2.5-pro \
  --agent-arg=--approval-mode \
  --agent-arg=auto_edit \
  --attach
```

`--agent-arg` is repeatable and preserves order, so it works with each CLI’s own flags instead of forcing one shared option schema.

Resume an existing Codex or Claude session in tmux:

```bash
agentctl tmux prepare --session codex-resume \
  --panes 1 \
  --agent codex \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach

agentctl tmux prepare --session claude-resume \
  --panes 1 \
  --agent claude \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach
```

`--agent-session-id` currently supports `codex` and `claude` only, and requires `--panes 1` so one resumed session is not duplicated across multiple panes.

## Structured Access

Prefer `agentctl` when you want machine-friendly envelopes:

```bash
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux observe agent-b --lines 80
agentctl tmux doctor
```

Use `agentctl tmux observe` when a tmux exchange should become a durable ACA observation.

## Direct Messaging

Use the bundled script for pane-to-pane interaction:

```bash
./scripts/tmux-bridge read agent-b 40
./scripts/tmux-bridge send agent-b "Please review internal/storage/mailbox/store.go for lease races."
```

`send` is the default for agent-to-agent messaging:

- it enforces read-before-send
- it prepends the stable `[tmux-bridge from=...]` header
- it presses Enter for you

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
