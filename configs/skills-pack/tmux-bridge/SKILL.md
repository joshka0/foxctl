---
name: tmux-bridge
description: "Compatibility alias for the room viewer layer. Prefer `foxctl-room-view` for current tmux, zellij, and GUI guidance."
metadata:
  openclaw:
    emoji: "🌉"
    os:
      - darwin
      - linux
    requires:
      bins:
        - tmux
        - foxctl
---

# tmux-bridge

`tmux-bridge` is now a compatibility name.

Use [`foxctl-room-view`](../foxctl-room-view/SKILL.md) for current guidance.

The architecture split is:

- `foxctl-room`: canonical room model and delivery
- `foxctl-room-agent`: participant-agent protocol
- `foxctl-room-operator`: coordinator/reviewer operating protocol
- `foxctl-room-view`: tmux/zellij/gui presentation layer

Keep using the existing `foxctl mux ...` commands and the `./scripts/tmux-bridge`
helper when you need them. The rename is about responsibility, not command
removal.

`foxctl mux send` is the default for agent-to-agent messaging:

- it resolves the sender pane from the current tmux pane or `--sender`
- it prepends the stable `[tmux-bridge from=...]` header
- it presses Enter for you

`foxctl room ...` now follows the same identity rule: derive the current pane
participant first, then fall back to canonical ids like `tmux:<session>:%7` or
`zellij:<session>:terminal_3` when no human-friendly pane name is present.

Viewer metadata:

- wrapped panes expose participant/provider/room metadata in `mux list`
- treat that metadata as operator-facing only; it does not replace room membership or participant transport state
- when a room exists, prefer `room status` for health and `mux list` for viewer placement

For spawned zellij panes, prefer `foxctl mux list --backend zellij --session <session-name>`.
That view is driven by persisted `terminal_binding` metadata and is more
reliable than trying to infer pane names from non-interactive layout dumps.

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
foxctl mux observe agent-b --lines 80
foxctl mux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/runtime/actor/supervisor.go"
```

Do not treat raw pane scrollback as canonical history.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
