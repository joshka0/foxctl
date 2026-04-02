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
- `agentctl tmux` is the native create/read/send surface
- `tmux-bridge` is an optional low-level helper
- ACA and the vault keep only promoted observations, tensions, and handoffs

Do not treat tmux scrollback as canonical history. It is pane state, not durable conversation state.

## Quick Start

Label each pane once:

```bash
agentctl tmux create --session agentctl-collab --panes 3 --attach
```

Inspect panes with structured output:

```bash
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux doctor
```

Send a message natively:

```bash
agentctl tmux send agent-b "Review internal/actor/supervisor.go for mailbox ack risks."
```

If you are invoking from outside tmux, pass your sender pane label explicitly:

```bash
agentctl tmux send agent-b "Review internal/actor/supervisor.go for mailbox ack risks." --sender agent-a
```

## Commands

### Structured read surface

```bash
agentctl tmux create --session agentctl-collab --panes 3 --pane-command codex
agentctl tmux list
agentctl tmux read <target> --lines 50
agentctl tmux send <target> "review this pane" [--sender <pane-label>]
agentctl tmux observe <target> --lines 80
agentctl tmux doctor
```

Use `agentctl tmux ...` when you want machine-friendly envelopes, pane metadata, and a native send path that does not depend on repo-local scripts.

Common agent launches include `--agent codex`, `--agent claude`, `--agent gemini`, `--agent agent` for Cursor CLI, and `--agent droid` for Factory Droid.

### Interactive bridge

`tmux-bridge` remains available for lower-level control:

```bash
./scripts/tmux-bridge list
./scripts/tmux-bridge read <target> [lines]
./scripts/tmux-bridge type <target> <text>
./scripts/tmux-bridge keys <target> Enter
./scripts/tmux-bridge name <target> <label>
./scripts/tmux-bridge resolve <label>
./scripts/tmux-bridge doctor
./scripts/tmux-bridge id
```

Use `agentctl tmux send` for standard agent-to-agent messages. Use `type` and `keys` only when you intentionally need lower-level control.

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
