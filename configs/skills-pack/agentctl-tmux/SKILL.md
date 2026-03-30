---
name: agentctl-tmux
description: "Live tmux collaboration for multiple Codex/Claude panes: inspect panes with agentctl, send bounded pane-to-pane messages, and promote durable facts into ACA."
---

# Tmux Collaboration

Use this skill when multiple AI agents are open in one tmux session and need to:

- inspect other panes without leaving the TUI open
- send direct pane-to-pane messages
- keep tmux as the live coordination plane and ACA as the durable continuity plane

## Mental Model

- `tmux` is the live coordination plane
- `agentctl tmux` is the structured read surface
- `tmux-bridge` is the write/send helper
- ACA and the vault keep only promoted observations, tensions, and handoffs

Do not treat tmux scrollback as canonical history. It is pane state, not durable conversation state.

## Quick Start

Label each pane once:

```bash
agentctl tmux prepare --session agentctl-collab --panes 3 --attach
```

Inspect panes with structured output:

```bash
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux doctor
```

Send a message after reading the target pane:

```bash
./scripts/tmux-bridge read agent-b 40
./scripts/tmux-bridge send agent-b "Review internal/actor/supervisor.go for mailbox ack risks."
```

The bridge enforces read-before-send per sender pane, not just per target pane.

## Commands

### Structured read surface

```bash
agentctl tmux prepare --session agentctl-collab --panes 3 --pane-command codex
agentctl tmux list
agentctl tmux read <target> --lines 50
agentctl tmux observe <target> --lines 80
agentctl tmux doctor
```

Use `agentctl tmux ...` when you want machine-friendly envelopes and pane metadata.

### Interactive bridge

```bash
./scripts/tmux-bridge list
./scripts/tmux-bridge read <target> [lines]
./scripts/tmux-bridge send <target> <text>
./scripts/tmux-bridge type <target> <text>
./scripts/tmux-bridge keys <target> Enter
./scripts/tmux-bridge name <target> <label>
./scripts/tmux-bridge resolve <label>
./scripts/tmux-bridge doctor
./scripts/tmux-bridge id
```

Use `send` for normal agent-to-agent messages. Use `type` and `keys` only when you intentionally need lower-level control.

## Message Format

`send` prepends a stable header before pressing Enter:

```text
[tmux-bridge from=agent-a pane=%3 reply_to=agent-a] review mailbox retry logic
```

That keeps replies human-readable and parseable without depending on fragile prose.

## ACA Promotion

Promote only derived facts, not raw pane dumps. Good examples:

```bash
agentctl tmux observe agent-b --lines 80
agentctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

If a tmux exchange produced durable repo knowledge, capture it through the existing ACA and Obsidian flow after review.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
