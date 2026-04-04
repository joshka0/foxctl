# Tmux Collaboration

Status: current room + tmux + zellij slice

This document defines the first tmux collaboration surface for `agentctl`:

- tmux is the live coordination plane
- `agentctl tmux` is the native create/read/send surface
- `agentctl room` is the durable room timeline, task, and relay surface
- `tmux-bridge` is an optional low-level helper
- ACA and the Obsidian vault remain the durable continuity layers

The point is to let multiple Codex or Claude panes interact while the TUI stays open, without turning tmux scrollback into canonical state.

Rooms are now the canonical shared chat substrate. tmux relay is the live
fanout layer on top of room messages rather than the source of truth.

## Why This Exists

Opening several AI agents in one tmux session is useful because:

- the operators can see the agents talk in real time
- agents can inspect nearby work without leaving their own pane
- cross-pane communication can happen without standing up a separate message bus

But tmux is still a terminal substrate, not a durable protocol. That means:

- use tmux for live coordination
- use mailbox, ACA, sessions, and vault notes for durable state

## Command Surface

### Structured Reads

`agentctl` exposes native tmux setup and messaging:

```bash
agentctl tmux create --session agentctl-collab --panes 3 --attach
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux send agent-b "Review internal/storage/mailbox/store.go for lease races." --sender agent-a
agentctl tmux observe agent-b --lines 80
agentctl tmux doctor
```

`create` creates or extends a detached tmux session, tiles it, and returns an attach command plus native send examples. `prepare` remains as a compatibility alias.

Without `--agent`, panes default to labels like `agent-a`, `agent-b`, `agent-c`.
With `--agent claude`, `--agent codex`, `--agent gemini`, `--agent agent`, or `--agent droid`, the default labels become `claude-a`, `codex-a`, `gemini-a`, `agent-a`, or `droid-a`.

With `--attach`, `agentctl` jumps directly into the prepared session. Outside tmux it uses `attach-session`; inside tmux it uses `switch-client`.

To launch a specific agent CLI in each pane, use `--agent` plus repeated `--agent-arg` flags:

```bash
agentctl tmux create --session claude-collab --panes 3 \
  --agent claude \
  --agent-arg=--model \
  --agent-arg=sonnet \
  --agent-arg=--permission-mode \
  --agent-arg=default \
  --attach

agentctl tmux create --session cursor-collab --panes 3 \
  --agent agent \
  --agent-arg=--model \
  --agent-arg=claude-sonnet-4 \
  --attach

agentctl tmux create --session droid-collab --panes 3 \
  --agent droid \
  --attach
```

By default, `agentctl` does not inject `--model`. The provider CLI keeps its
current configured default model unless you explicitly add `--agent-arg=--model`
and a value.

For provider-specific autonomous launches, prefer `--mode auto` over manually
remembering the raw permission flags:

```bash
agentctl tmux create --session codex-auto --panes 2 --agent codex --mode auto --attach
agentctl tmux create --session claude-auto --panes 2 --agent claude --mode auto --attach
agentctl tmux create --session gemini-auto --panes 2 --agent gemini --mode auto --attach
agentctl tmux create --session cursor-auto --panes 2 --agent agent --mode auto --attach
```

Current `--mode auto` mappings:

- `codex` -> `--full-auto`
- `claude` -> `--dangerously-skip-permissions`
- `gemini` -> `--yolo`
- `agent` *(Cursor CLI)* -> `--yolo`

Agents without a known safe mapping still require explicit `--agent-arg` values.

For Cursor CLI (`agent`) and Droid (`droid`), model selection can happen inside the interactive session with their native slash commands such as `/model`.

This keeps `agentctl` neutral while still letting each CLI use its own option surface.

To resume an existing Codex or Claude session in tmux:

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

`--agent-session-id` currently supports `codex` and `claude` only, and requires `--panes 1`.

The other commands return structured envelopes with pane metadata and bounded scrollback captures.

### Native Sends

Prefer the native CLI for pane-to-pane interaction:

```bash
agentctl tmux create --session agentctl-collab --panes 3 --agent codex --attach
agentctl tmux send agent-b "Review internal/storage/mailbox/store.go for lease races."
```

When invoking `send` from outside tmux, provide the sender pane label explicitly:

```bash
agentctl tmux create --session praze-collab --panes 2 --label-prefix praze
agentctl tmux send praze-b "Please review this path." --sender praze-a
```

This means external callers such as Praze can create their own tmux session and send as one of their labeled panes without depending on a repo-local script.

The bundled `tmux-bridge` helper is still available for lower-level workflows such as `type`, `keys`, or read-before-send guardrails, but it is no longer required for the core create/send path.

## Room Surface

Rooms are durable coordination timelines backed by the blackboard store. They
are independent of tmux and can be used from any terminal, including zellij.

```bash
agentctl room create alpha --title "Agent Alpha"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review the retry branch in client.ts"
agentctl room task add alpha --title "Refactor retry path"
agentctl room task complete alpha --id <task-id> --notes "Retry ladder flattened"
agentctl room show alpha
agentctl room subscribe alpha --follow
agentctl room relay alpha --backend tmux
agentctl room loop alpha --backend zellij --session alpha-room
```

The intended model is:

- `room send` writes the canonical message into the durable room log
- `room subscribe` reads or tails the room log in any terminal
- `room relay --backend tmux` fans new room messages into tmux member panes by
  matching room member ids to tmux pane labels

This keeps room history durable while still allowing live terminal delivery.

### Room Tasks

Rooms can now carry shared task state on top of the existing task store:

```bash
agentctl room task add alpha \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery blocks in client.ts"

agentctl room task list alpha

agentctl room task complete alpha \
  --id <task-id> \
  --notes "Retry helper extracted"
```

`room task add` and `room task complete` both write a durable room message with a
`task_id`, so every participant sees the task lifecycle in the same shared room
timeline.

### Room Loop

`room relay` is a pure message fanout loop.

`room loop` is the higher-level room coordinator:

- relays new room messages into member panes
- watches room-associated tasks for status transitions
- broadcasts task completion or status updates back into the room

That gives the room a central coordination loop without making terminal
scrollback the source of truth.

### Join vs Subscribe

- `room join` adds a participant id to the room membership set
- `room subscribe` reads the room, optionally in follow mode

Use `join` when a pane or agent should be a room recipient for relay. Use
`subscribe` when a human or agent just wants to watch the room timeline.

Inside tmux, room commands derive the sender from the current pane label. If the
pane has no label, `agentctl` falls back to a canonical id like
`tmux:<session>:%7`.

Inside zellij, room commands derive the sender from
`AGENTCTL_ZELLIJ_PARTICIPANT` when present, otherwise they fall back to a
canonical id like `zellij:<session>:terminal_3` using
`ZELLIJ_SESSION_NAME` and `ZELLIJ_PANE_ID`.

So:

- `--sender` is the override path, not the default path
- `room join <room-id> --current` registers the current pane as a room member
- relay accepts both human-friendly pane names and canonical pane ids

### Zellij

`room subscribe` works in zellij because it only tails the durable room log.

Live relay injection for zellij is now plugin-backed:

```bash
agentctl room relay alpha --backend zellij --session alpha-room
agentctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a session-local plugin that maps room member ids to pane
titles or canonical pane ids and writes messages into those panes. The first run needs a zellij
permission grant for:

- `ReadApplicationState`
- `WriteToStdin`
- `ReadCliPipes`

By default the Go relay looks for the plugin in:

- `--plugin-path`
- `AGENTCTL_ZELLIJ_ROOM_PLUGIN`
- `~/.agentctl/plugins/zellij_room_relay.wasm`
- a repo-local build at `plugins/zellij-room-relay/...`

When run from the `agentctl` repo, it can auto-build the plugin source if the
wasm artifact is missing.

## Message Shape

Bridge messages use a stable ASCII header:

```text
[tmux-bridge from=agent-a pane=%3 reply_to=agent-a] review mailbox retry logic
```

That keeps the exchange human-readable while still giving other agents a parseable cue.

## ACA Fit

Promote only derived facts into ACA. Good tmux-derived records are:

- observations
- tensions
- handoffs

Examples:

```bash
agentctl tmux observe agent-b --lines 80
agentctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

`agentctl tmux observe` reads the latest bridge message in the target pane, converts it into an ACA observation, and stores pane/session bridge refs as evidence.

Do not promote raw pane dumps directly into the vault. If a tmux exchange produces durable repo knowledge, summarize it first, then use the normal ACA and Obsidian promotion path.

## Scope Boundary

What tmux should do:

- expose live pane layout and scrollback
- support direct agent-to-agent nudges
- give operators a visible coordination layer

What tmux should not replace:

- mailbox request/reply semantics
- session continuity
- ACA top-of-mind, observations, tensions, or handoffs
- reviewed Obsidian notes

## Related

- [docs/architecture/context-architecture.md](../architecture/context-architecture.md)
- [docs/general/runtime-orchestration.md](runtime-orchestration.md)
- [configs/skills-pack/agentctl-room/SKILL.md](../../configs/skills-pack/agentctl-room/SKILL.md)
- [configs/skills-pack/agentctl-tmux/SKILL.md](../../configs/skills-pack/agentctl-tmux/SKILL.md)
