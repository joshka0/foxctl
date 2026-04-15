---
name: foxctl-tmux
description: "Compatibility alias for the room presentation layer. Prefer `foxctl-room-view` for current tmux/zellij/gui viewer guidance."
---

# Mux Collaboration

`foxctl-tmux` is now a compatibility name.

Use [`foxctl-room-view`](../foxctl-room-view/SKILL.md) for current viewer and
presentation-layer guidance.

Use [`foxctl-room-agent`](../foxctl-room-agent/SKILL.md) for participant
agent behavior inside a room.

Keep using the existing `foxctl mux ...` commands and bundled
`./scripts/tmux-bridge` helper where needed. The change is conceptual: rooms are
canonical, mux is presentation.

Important runtime note:

- for Droid, `foxctl mux create --agent droid --mode auto` is not equivalent to `--mode interactive`
- `--mode auto` enables the Droid startup profile in the pane wrapper
- `--mode interactive` bypasses that profile and can leave Droid in approval-gated behavior
`zellij:<session>:terminal_3`. Use `--sender` only when overriding or when
running outside a mux session.

### Restarting an existing zellij session

If you reopened or manually created a zellij session, do not assume the panes
are already room-bound just because they share a session name.

Check first:

```bash
printf 'ROOM=%s ACTOR=%s ROLE=%s SESSION=%s PANE=%s\n' \
  "$FOXCTL_ROOM_ID" \
  "$FOXCTL_PARTICIPANT" \
  "$FOXCTL_ROOM_ROLE" \
  "$ZELLIJ_SESSION_NAME" \
  "$ZELLIJ_PANE_ID"
```

If `ROOM` / `ACTOR` are empty, bind the pane explicitly:

```bash
foxctl room join <room-id> --current --role <room-role>
```

Use that in each zellij pane that should receive room traffic. For zellij,
room membership is pane-bound, not merely session-bound.

If an existing participant moves to a different pane, repair the stored mux
binding instead of pretending it is a new member:

```bash
foxctl room rebind <room-id> <actor-id> --backend <tmux|zellij> --session <session> --pane-id <pane>
```

The intended room policy is:

- top-level panes may join rooms directly
- child panes stay parent-private by default
- parents summarize child work back into the room when needed

## Message Format

`send` prepends a stable header before pressing Enter:

```text
[tmux-bridge from=agent-a pane=%3 reply_to=agent-a] review mailbox retry logic
```

That keeps replies human-readable and parseable without depending on fragile prose.

## ACA Promotion

Promote only derived facts, not raw pane dumps. Good examples:

```bash
foxctl mux observe agent-b --lines 80
foxctl mux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/runtime/actor/supervisor.go"
```

If a tmux exchange produced durable repo knowledge, capture it through the existing ACA and Obsidian flow after review.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
