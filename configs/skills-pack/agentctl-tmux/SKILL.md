---
name: agentctl-tmux
description: "Live terminal collaboration for multiple Codex/Claude panes: tmux native messaging, durable room timelines, plugin-backed zellij relay, and room-scoped task broadcasts."
---

# Tmux Collaboration

Use this skill when multiple AI agents are open in tmux or zellij and need to:

- inspect other panes without leaving the TUI open
- send direct pane-to-pane messages
- keep a shared durable room log
- track shared room tasks and broadcast completion updates
- keep tmux as the live coordination plane and ACA as the durable continuity plane

## Mental Model

- `tmux` is the live coordination plane
- `agentctl tmux` is the native create/read/send surface
- `agentctl room` is the durable shared room surface
- `room relay` and `room loop` fan room events back into terminal panes
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
agentctl tmux list --backend zellij --session alpha-room
agentctl tmux read agent-b --lines 80
agentctl tmux doctor
```

Send a message natively:

```bash
agentctl tmux send agent-b "Review internal/actor/supervisor.go for mailbox ack risks."
```

For autonomous panes, prefer `--mode auto`:

```bash
agentctl tmux create --session codex-auto --panes 2 --agent codex --mode auto --attach
agentctl tmux create --session claude-auto --panes 2 --agent claude --mode auto --attach
agentctl tmux create --session gemini-auto --panes 2 --agent gemini --mode auto --attach
agentctl tmux create --session cursor-auto --panes 2 --agent agent --mode auto --attach
```

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
agentctl tmux create --session collab \
  --panes 1 \
  --agent codex \
  --mode auto \
  --label-prefix child \
  --parent-participant codex-a \
  --parent-agent-id agent:parent-1

agentctl tmux send-parent "Blocked on the mailbox retry path."
```

When `--parent-participant` is set, `agentctl` defaults `--room-access` to
`none`. That means child panes can talk to their parent, but they do not join
the room unless you explicitly promote them with `--room-access direct`.

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

Useful hierarchy flags on `tmux create`:

- `--parent-participant` marks the launched panes as children of a parent participant
- `--parent-agent-id` exports the durable parent agent id into the pane env
- `--room-id` exports a room id when room access is direct
- `--room-access default|direct|none` controls whether the pane should see room metadata

Child panes can send a private message to the configured parent with:

```bash
agentctl tmux send-parent "Need clarification on the retry helper extraction."
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
agentctl tmux list --backend zellij --session <session-name>
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

Use `agentctl tmux send` for standard agent-to-agent messages. Use `type` and `keys` only when you intentionally need lower-level control.

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

Room commands derive the sender from the current tmux pane label when possible,
with canonical fallbacks like `tmux:<session>:%7` or
`zellij:<session>:terminal_3`. Use `--sender` only when overriding or when
running outside a mux session.

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
agentctl tmux observe agent-b --lines 80
agentctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

If a tmux exchange produced durable repo knowledge, capture it through the existing ACA and Obsidian flow after review.

Reference doc: [`docs/general/tmux-collaboration.md`](../../../docs/general/tmux-collaboration.md)
