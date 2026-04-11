---
name: agentctl-tmux
description: "Live terminal collaboration for multiple Codex/Claude panes: tmux native messaging, durable room timelines, plugin-backed zellij relay, and room-scoped task broadcasts."
---

# Mux Collaboration

Use this skill when multiple AI agents are open in tmux or zellij and need to:

- inspect other panes without leaving the TUI open
- send direct pane-to-pane messages
- keep a shared durable room log
- track shared room tasks and broadcast completion updates
- keep tmux as the live coordination plane and ACA as the durable continuity plane

## Mental Model

- the live mux pane layer can be `tmux` or `zellij`
- `agentctl mux` is the backend-neutral create/read/send surface
- `agentctl room` is the durable shared room surface
- `room relay` and `room loop` fan room events back into terminal panes (delivery ends with a newline/submit so the agent UI accepts the text)
- `room send`: prefer **`--to <participant>`** and **`--sender <you>`** when addressing or when identity is ambiguous; from inside tmux/zellij the sender often infers from the pane. After storing, agentctl live-relays to targets and mux-submits the **current** pane (`--no-mux-submit` / `--no-live-relay` to skip either)
- `tmux-bridge` is an optional low-level helper
- ACA and the vault keep only promoted observations, tensions, and handoffs

Do not treat tmux scrollback as canonical history. It is pane state, not durable conversation state.

When a room is active, the default rule is:

- use `agentctl room` for durable coordination
- use `agentctl mux send` for one-off live pane nudges
- do not treat pane reads as the source of truth for task state or coordinator decisions

If you are an agent entering an existing collaboration session, start with:

```bash
agentctl room status <room-id>
agentctl room inbox <room-id> --actor <you>
agentctl mux list
```

That gives you the durable room state first, then the live pane state.

Important:

- `mux create --room-id ...` gives the pane room context, not a guarantee of room membership
- always verify membership with `agentctl room status <room-id>`
- if your participant is missing from `room status`, explicitly `room join` or `room rebind` before assuming room traffic will reach you

## Quick Start

Label each pane once:

```bash
agentctl mux create --session agentctl-collab --panes 3 --attach
```

Inspect panes with structured output:

```bash
agentctl mux list
agentctl mux list --backend zellij --session alpha-room
agentctl mux read agent-b --lines 80
agentctl mux doctor
```

Send a message natively:

```bash
agentctl mux send agent-b "Review internal/actor/supervisor.go for mailbox ack risks."
```

For autonomous panes, prefer `--mode auto`:

```bash
agentctl mux create --session codex-auto --panes 2 --agent codex --mode auto --attach
agentctl mux create --session claude-auto --panes 2 --agent claude --mode auto --attach
agentctl mux create --session gemini-auto --panes 2 --agent gemini --mode auto --attach
agentctl mux create --session cursor-auto --panes 2 --agent agent --mode auto --attach
```

Default participant naming is scoped when possible:

- room-bound panes default to `<room-id>-<agent>-<suffix>`
- named non-default sessions default to `<session>-<agent>-<suffix>`
- only plain ad hoc sessions fall back to generic labels like `claude-a`

This keeps participant ids obviously tied to their feature or room so other agents do not accidentally target the wrong runtime.

Current auto mappings:

- `codex` -> `--full-auto`
- `claude` -> `--dangerously-skip-permissions`
- `gemini` -> `--yolo`
- `agent` -> `--yolo`

`agentctl` does not force `--model` by default. The provider CLI keeps its own
current default model unless you explicitly pass repeated `--agent-arg`
overrides.

Use a durable shared room when more than two agents need the same timeline:

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review the retry path in client.ts"
agentctl room task add alpha --title "Refactor retry path"
agentctl room loop alpha --backend tmux
```

For child panes, keep room access private by default and give them an explicit
parent:

```bash
agentctl mux create --session collab \
  --panes 1 \
  --agent codex \
  --mode auto \
  --label-prefix child \
  --parent-participant codex-a \
  --parent-agent-id agent:parent-1

agentctl mux send-parent "Blocked on the mailbox retry path."
```

When `--parent-participant` is set, `agentctl` defaults `--room-access` to
`none`. That means child panes can talk to their parent, but they do not join
the room unless you explicitly promote them with `--room-access direct`.

If you are invoking from outside tmux, pass your sender pane label explicitly:

```bash
agentctl mux send agent-b "Review internal/actor/supervisor.go for mailbox ack risks." --sender agent-a
```

## Commands

### Structured read surface

```bash
agentctl mux create --session agentctl-collab --panes 3 --pane-command codex
agentctl mux list
agentctl mux read <target> --lines 50
agentctl mux send <target> "review this pane" [--sender <pane-label>]
agentctl mux observe <target> --lines 80
agentctl mux doctor
```

Use `agentctl mux ...` when you want machine-friendly envelopes, pane metadata, and a native send path that does not depend on repo-local scripts. `agentctl tmux ...` remains as a compatibility alias.

Do **not** document `mux submit` as a primary workflow: room relay/loop and `room send` already cover submit behavior for coordination.

When `agentctl mux create` launches a tmux agent pane with direct room access, agentctl also injects a lightweight startup note into that pane telling the agent to read `agentctl-tmux` and `agentctl-room`, along with the initial `room status` / `room inbox` / `room task list` commands for the attached room.

That startup note is only orientation. The acceptance check is still: does `room status` show your participant?

Common agent launches include `--agent codex`, `--agent claude`, `--agent gemini`, `--agent agent` for Cursor CLI, and `--agent droid` for Factory Droid.

Useful hierarchy flags on `mux create`:

- `--parent-participant` marks the launched panes as children of a parent participant
- `--parent-agent-id` exports the durable parent agent id into the pane env
- `--room-id` exports a room id when room access is direct
- `--room-access default|direct|none` controls whether the pane should see room metadata

Child panes can send a private message to the configured parent with:

```bash
agentctl mux send-parent "Need clarification on the retry helper extraction."
```

For internal `agentctl` agents, you can also allocate a dedicated tmux pane at
spawn time and have it switch into `agentctl agent watch` after the spawn:

```bash
agentctl agent spawn \
  --role researcher \
  --prompt "Inspect mailbox ack behavior" \
  --mux-backend tmux \
  --mux-session collab-runtime \
  --spawn-in-pane
```

For zellij, the same command shape works with `--mux-backend zellij` and an
explicit `--mux-session`. The current zellij path uses a named pane as the
durable participant identity for the spawned agent.

For spawned zellij panes, inspect the current session through persisted
`terminal_binding` metadata rather than raw layout scraping:

```bash
agentctl mux list --backend zellij --session <session-name>
```

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

Use `agentctl mux send` for standard agent-to-agent messages. Use `type` and `keys` only when you intentionally need lower-level control.

### Durable room surface

```bash
agentctl room create <room-id> --title "..."
agentctl room join <room-id> --current --role lead
agentctl room subscribe <room-id> --follow
agentctl room relay <room-id> --backend tmux
agentctl room relay <room-id> --backend zellij --session <session-name>
agentctl room loop <room-id> --backend tmux
agentctl room task add <room-id> --title "..."
agentctl room task list <room-id>
agentctl room task complete <room-id> --id <task-id>
```

Use `room relay` for pure room-message fanout.
Use `room loop` when you also want room-associated task status transitions to be
broadcast back into the room and then relayed to participants.
If the room is using reminders, coordinator stale detection, or task heartbeat nudges, prefer `room loop`; `room relay` by itself is not enough.

Operational rule:

- top-level agents coordinate through the room
- child panes coordinate upward through `send-parent`
- reviewers should post findings in the room, not only in pane-local chat
- coordinators should prefer `room status` / `room resolve` / `room task ...` over manual pane polling

Room commands derive the sender from the current tmux pane label when possible,
with canonical fallbacks like `tmux:<session>:%7` or
`zellij:<session>:terminal_3`. Use `--sender` only when overriding or when
running outside a mux session.

### Restarting an existing zellij session

If you reopened or manually created a zellij session, do not assume the panes
are already room-bound just because they share a session name.

Check first:

```bash
printf 'ROOM=%s ACTOR=%s ROLE=%s SESSION=%s PANE=%s\n' \
  "$AGENTCTL_ROOM_ID" \
  "$AGENTCTL_PARTICIPANT" \
  "$AGENTCTL_ROOM_ROLE" \
  "$ZELLIJ_SESSION_NAME" \
  "$ZELLIJ_PANE_ID"
```

If `ROOM` / `ACTOR` are empty, bind the pane explicitly:

```bash
agentctl room join <room-id> --current --role <room-role>
```

Use that in each zellij pane that should receive room traffic. For zellij,
room membership is pane-bound, not merely session-bound.

If an existing participant moves to a different pane, repair the stored mux
binding instead of pretending it is a new member:

```bash
agentctl room rebind <room-id> <actor-id> --backend <tmux|zellij> --session <session> --pane-id <pane>
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
agentctl mux observe agent-b --lines 80
agentctl mux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

If a tmux exchange produced durable repo knowledge, capture it through the existing ACA and Obsidian flow after review.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
