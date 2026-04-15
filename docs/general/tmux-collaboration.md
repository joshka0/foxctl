# Tmux Collaboration

Status: current room + tmux + zellij slice

This document defines the first tmux collaboration surface for `foxctl`:

- tmux is the live coordination plane
- `foxctl tmux` is the native create/read/send surface
- `foxctl room` is the durable room timeline, task, and relay surface
- `tmux-bridge` is an optional low-level helper
- ACA and the Obsidian vault remain the durable continuity layers

The point is to let multiple Codex or Claude panes interact while the TUI stays open, without turning tmux scrollback into canonical state.

Rooms are now the canonical shared chat substrate. tmux relay is the live
fanout layer on top of room messages rather than the source of truth.

Room traffic now supports both:

- broadcast messages to the whole room
- direct requests to one participant with optional `ack` and `reply` markers

The current hierarchy rule is:

- top-level panes may join rooms directly
- child panes stay parent-private by default
- child panes message their parent, not the room
- parents decide what to summarize or promote back into the room

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

`foxctl` exposes native tmux setup and messaging:

```bash
foxctl tmux create --session foxctl-collab --panes 3 --attach
foxctl tmux list
foxctl tmux read agent-b --lines 80
foxctl tmux send agent-b "Review internal/storage/mailbox/store.go for lease races." --sender agent-a
foxctl tmux observe agent-b --lines 80
foxctl tmux doctor
```

`create` creates or extends a detached tmux session, tiles it, and returns an attach command plus native send examples. `prepare` remains as a compatibility alias.

For zellij-backed agent panes, `list` can inspect persisted agent-owned
bindings instead of scraping the live session layout:

```bash
foxctl tmux list --backend zellij --session foxctl-zellij-smoke
```

This zellij mode is intentionally narrower than tmux:

- it lists `foxctl`-owned panes from durable `terminal_binding` metadata
- it does not attempt to enumerate arbitrary non-agent panes in the session
- it is the preferred observability path for spawned parent/child agent panes

Without `--agent`, panes default to labels like `agent-a`, `agent-b`, `agent-c`.
With `--agent claude`, `--agent codex`, `--agent gemini`, `--agent agent`, or `--agent droid`, the default labels become `claude-a`, `codex-a`, `gemini-a`, `agent-a`, or `droid-a`.

With `--attach`, `foxctl` jumps directly into the prepared session. Outside tmux it uses `attach-session`; inside tmux it uses `switch-client`.

To launch a specific agent CLI in each pane, use `--agent` plus repeated `--agent-arg` flags:

```bash
foxctl tmux create --session claude-collab --panes 3 \
  --agent claude \
  --agent-arg=--model \
  --agent-arg=sonnet \
  --agent-arg=--permission-mode \
  --agent-arg=default \
  --attach

foxctl tmux create --session cursor-collab --panes 3 \
  --agent agent \
  --agent-arg=--model \
  --agent-arg=claude-sonnet-4 \
  --attach

foxctl tmux create --session droid-collab --panes 3 \
  --agent droid \
  --attach
```

By default, `foxctl` does not inject `--model`. The provider CLI keeps its
current configured default model unless you explicitly add `--agent-arg=--model`
and a value.

For provider-specific autonomous launches, prefer `--mode auto` over manually
remembering the raw permission flags:

```bash
foxctl tmux create --session codex-auto --panes 2 --agent codex --mode auto --attach
foxctl tmux create --session claude-auto --panes 2 --agent claude --mode auto --attach
foxctl tmux create --session gemini-auto --panes 2 --agent gemini --mode auto --attach
foxctl tmux create --session cursor-auto --panes 2 --agent agent --mode auto --attach
```

Current `--mode auto` mappings:

- `codex` -> `--full-auto`
- `claude` -> `--dangerously-skip-permissions`
- `gemini` -> `--yolo`
- `agent` *(Cursor CLI)* -> `--yolo`

Agents without a known safe mapping still require explicit `--agent-arg` values.

For Cursor CLI (`agent`) and Droid (`droid`), model selection can happen inside the interactive session with their native slash commands such as `/model`.

This keeps `foxctl` neutral while still letting each CLI use its own option surface.

To resume an existing Codex or Claude session in tmux:

```bash
foxctl tmux create --session codex-resume \
  --panes 1 \
  --agent codex \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach

foxctl tmux create --session claude-resume \
  --panes 1 \
  --agent claude \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach
```

`--agent-session-id` currently supports `codex` and `claude` only, and requires `--panes 1`.

### Child panes and parent-private routing

`foxctl tmux create` can now stamp child-pane metadata into the launched pane
environment:

```bash
foxctl tmux create --session codex-tree \
  --panes 1 \
  --agent codex \
  --mode auto \
  --label-prefix child \
  --parent-participant codex-a \
  --parent-agent-id agent:parent-1
```

Relevant flags:

- `--parent-participant` marks the launched pane as a child of a parent participant
- `--parent-agent-id` exports the durable parent agent id
- `--room-id` exports a room id when room access is direct
- `--room-access default|direct|none` controls whether the pane sees room metadata

Default policy:

- top-level panes: `room-access=direct`
- child panes with `--parent-participant`: `room-access=none`

That means child panes get stable mux identity without automatically joining the
room.

Exported environment includes:

- `FOXCTL_PARTICIPANT_ID`
- `FOXCTL_PARENT_PARTICIPANT_ID`
- `FOXCTL_PARENT_AGENT_ID`
- `FOXCTL_MUX_BACKEND`
- `FOXCTL_MUX_SESSION`
- `FOXCTL_MUX_PANE_ID`
- `FOXCTL_ROOM_ID` when room access is direct

The other commands return structured envelopes with pane metadata and bounded scrollback captures.

### Native Sends

Prefer the native CLI for pane-to-pane interaction:

```bash
foxctl tmux create --session foxctl-collab --panes 3 --agent codex --attach
foxctl tmux send agent-b "Review internal/storage/mailbox/store.go for lease races."
```

When invoking `send` from outside tmux, provide the sender pane label explicitly:

```bash
foxctl tmux create --session praze-collab --panes 2 --label-prefix praze
foxctl tmux send praze-b "Please review this path." --sender praze-a
```

This means external callers such as Praze can create their own tmux session and send as one of their labeled panes without depending on a repo-local script.

The bundled `tmux-bridge` helper is still available for lower-level workflows such as `type`, `keys`, or read-before-send guardrails, but it is no longer required for the core create/send path.

Child panes can send directly to their configured parent without guessing a
sender or target:

```bash
foxctl tmux send-parent "Blocked on the mailbox retry path."
```

`send-parent` resolves the target from `FOXCTL_PARENT_PARTICIPANT_ID`.

### Agent spawn into a tmux pane

`foxctl agent spawn` can now allocate a dedicated tmux pane, bind a durable
canonical participant id from the real pane id, and repurpose that pane into an
`foxctl agent watch` stream after the spawn succeeds.

Example:

```bash
foxctl agent spawn \
  --role researcher \
  --prompt "Inspect mailbox ack behavior" \
  --mux-backend tmux \
  --mux-session collab-runtime \
  --spawn-in-pane
```

This currently supports `tmux` only. The pane is allocated first, the binding is
persisted as typed `terminal_binding` metadata on the spawned agent, and the
pane is then respawned into:

```bash
foxctl agent watch <agent-id>
```

For child agents, combine it with parent-private metadata:

```bash
foxctl agent spawn \
  --role coder \
  --prompt "Extract retry helper" \
  --mux-backend tmux \
  --mux-session collab-runtime \
  --parent-participant codex-a \
  --parent-agent-id agent:parent-1 \
  --spawn-in-pane
```

That keeps the child parent-private by default while still giving it a dedicated
tmux presence and durable terminal binding metadata.

Zellij now supports the same `agent spawn --spawn-in-pane` shape, using a named
pane created with `zellij run --name ...`:

```bash
foxctl agent spawn \
  --role reviewer \
  --prompt "Review mailbox ack behavior" \
  --mux-backend zellij \
  --mux-session collab-runtime \
  --spawn-in-pane
```

Important nuance:

- tmux uses a real pane id and can derive a canonical `tmux:<session>:%pane`
  participant
- zellij currently uses a generated pane title as the durable participant id for
  spawned agents, because the CLI exposes reliable named-pane creation but not
  the same exact respawn-by-pane-id flow

For those spawned zellij panes, the recommended inspection path is:

```bash
foxctl tmux list --backend zellij --session collab-runtime
```

## Room Surface

Rooms are durable coordination timelines backed by the blackboard store. They
are independent of tmux and can be used from any terminal, including zellij.

When `foxctl room create` can derive the current tmux/zellij participant, it
automatically adds that creator as a room member with role `coordinator`
without overwriting any explicit role you already supplied for the same actor.

```bash
foxctl room create alpha --title "Agent Alpha"
foxctl room join alpha agent-a --role lead
foxctl room join alpha agent-b --role reviewer
foxctl room send alpha "Review the retry branch in client.ts"
foxctl room send alpha "Claude, please review the retry branch." --to claude-a --reply-expected
foxctl room send alpha "Gemini, confirm receipt." --to gemini-a --ack-required
foxctl room ack alpha <message-id>
foxctl room inbox alpha --actor claude-a
foxctl room status alpha
foxctl room task add alpha --title "Refactor retry path"
foxctl room task claim alpha --id <task-id>
foxctl room task touch alpha --id <task-id>
foxctl room task block alpha --id <task-id> --reason "waiting on benchmark data"
foxctl room task unblock alpha --id <task-id>
foxctl room task complete alpha --id <task-id> --notes "Retry ladder flattened"
foxctl room show alpha
foxctl room subscribe alpha --follow
foxctl room relay alpha --backend tmux
foxctl room loop alpha --backend zellij --session alpha-room
```

The intended model is:

- `room send` writes the canonical message into the durable room log
- `room send --to <participant>` writes a direct room message and only relays it to the targeted participant
- `room send --to @coordinator` resolves to the current room coordinator
- `--reply-expected` is for direct requests only; broadcasts stay FYI by default
- `--ack-required` marks a message as requiring an explicit acknowledgment
- `room ack` marks one or more room messages as `acked` in the durable log
- `room resolve` lets the coordinator clear stale handled reminders from the room surface and resolves the whole related message chain by original request id, not just one later recipient message
- `room remind add` creates a durable reminder schedule owned by the room loop, not a one-off chat resend
- room patch and full member replacement are coordinator-only control-surface mutations
- member transport or binding updates are self-service only for the target participant unless the caller has coordinator access
- role-changing binding updates are coordinator-only even when the caller is updating their own binding
- `room coordinator set` transfers coordinator ownership to another room participant
- `room inbox` shows the actionable queue for one participant instead of the full room archive
- `room status` shows the coordinator-facing pulse for participants, task state, stale owned work, and compact actionable backlog summaries
- `room status --only blocked,stale,reply` narrows the summary to the exact coordination lane you want to inspect
- `room status --verbose` keeps the compact default but adds richer actionable entry detail when you need to debug the underlying room traffic
- `room status` and loop/status surfaces also expose `last_delivery_trace` from the persisted room-loop row
- use `last_delivery_trace` as the canonical explanation for the latest delivery decision:
  chosen binding, chosen transport, fallback attempt, outcome, and cursor movement belong there, not in pane guesswork
- `room subscribe` reads or tails the room log in any terminal
- `room relay --backend tmux` fans new room messages into tmux member panes by
  matching room member ids to tmux pane labels

This keeps room history durable while still allowing live terminal delivery.

The coordinator owns room flow by default:

- participants can nudge the coordinator directly with `--to @coordinator`
- the room loop can remind the coordinator when unresolved work still needs oversight
- coordinator handoff is explicit via `room coordinator set`

### Room Tasks

Rooms can now carry shared task state on top of the existing task store:

```bash
foxctl room task add alpha \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery blocks in client.ts"

foxctl room task list alpha

foxctl room task assign alpha --id <task-id> --to gemini-a --notes "Take first pass"
foxctl room task claim alpha --id <task-id>
foxctl room task touch alpha --id <task-id>
foxctl room task block alpha --id <task-id> --reason "waiting on benchmark data"
foxctl room task unblock alpha --id <task-id>

foxctl room task complete alpha \
  --id <task-id> \
  --notes "Retry helper extracted"
```

`room task add`, `claim`, `touch`, `block`, `unblock`, `abandon`, and `complete` all
write durable room messages with a `task_id`, so every participant sees the
task lifecycle in the same shared room timeline.

The intended lifecycle is strict rather than permissive:

- `pending -> in_progress -> blocked -> completed`
- `assign` records intended ownership and sends a direct request, but the assignee still claims the task explicitly
- assigned tasks are claimable only by the assignee until they are reassigned, reclaimed, or abandoned
- `assign` is for untouched or reclaimed work; it should not be used to overwrite already-claimed work
- `reassign` is for retargeting work that already has an assignee or owner
- `reclaim` returns work to the unowned pool and clears assignment/ownership so it can be assigned or claimed again cleanly
- `abandon` returns a task to `pending`
- `complete` requires the current participant to claim the task first and only succeeds from a real `in_progress` owner state
- `touch` refreshes the task heartbeat without changing state
- `block` / `unblock` keep ownership while making the stall visible
- `block` is only valid from `in_progress`, and `unblock` is only valid from `blocked`
- rejected task actions should be treated as state-machine protection, not as a prompt to mutate fields by hand

### Room Loop

`room relay` is a pure message fanout loop.

`room loop` is the higher-level room coordinator:

- relays new room messages into member panes
- watches room-associated tasks for status transitions
- broadcasts task completion or status updates back into the room
- emits direct reminder pulses for stale `ack_required` / `reply_expected` requests
- emits direct reminder pulses for claimed or blocked tasks whose heartbeat has gone stale

That gives the room a central coordination loop without making terminal
scrollback the source of truth.

Loop policy is persisted room state, not a `room loop` runtime flag set:

```bash
foxctl room loop alpha --backend tmux
```

Important policy rule:

- `room loop` executes the stored policy; it does not own timing defaults at runtime
- use `room loop patch` or the API loop patch surface to change policy
- `task_followup_interval` is a separate persisted field from `pulse_interval`
- `task_followup_interval=0` means task follow-up check-ins are disabled even when reminder pulses are enabled
- this is the intended default after the hard cut, so upgraded rooms must set `task_followup_interval` explicitly if they still want automatic task follow-up messages
- acknowledging one emitted reminder follow-up does not stop the recurring schedule by itself
- recurring schedules stop when the root request is satisfied, when linked `task_id`, `story_id`, or `milestone_id` work is completed, when the reminder is cancelled, or when `max_iterations` is exhausted

`room inbox` is the per-participant actionable view that pairs with this pulse
behavior:

- already-acked messages are hidden
- direct `reply_expected` requests disappear only after the intended recipient replies within the same message chain
- `room resolve` follows that same chain model, so resolving a root request clears its related reminder / follow-up chain together
- the result is a queue of unresolved asks rather than a full transcript

### Canonical Client Surfaces

Room client adoption now follows two stable contracts:

- `GET /api/rooms/{room-id}/events?workspace_id=...` is the room-scoped SSE stream for room timeline updates
- room member payloads expose `delivery_binding` as the authoritative transport/routing record

Operationally:

- use the room-scoped SSE stream for room views instead of subscribing to the global `/api/events` feed and filtering `room.message` client-side
- treat `delivery_binding` as canonical when resolving mux backend, session, pane id, transport endpoint, submit mode, health, or fallback policy
- treat `last_delivery_trace` as the first place to inspect when someone asks “why did this message route here” or “did fallback happen”
- the older top-level member fields (`backend`, `session`, `pane_id`, `transport_endpoint`, `transport_kind`) are compatibility mirrors and should not be the source of truth for new client work
- use `bash tests/regression/run.sh` as the default room-runtime verification bundle before reaching for ad hoc command mixes
- if the symptom is "the message is visible in the pane but looks unsent," run `FOXCTL_INTEGRATION_TMUX=1 go test -tags='integration libsqlite3' ./cmd/foxctl/cmd -run 'TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux' -count=1 -v` to verify the relay path is consumed by the target terminal process rather than left as drafted pane input

### Join vs Subscribe

- `room join` adds a participant id to the room membership set
- `room subscribe` reads the room, optionally in follow mode

Use `join` when a pane or agent should be a room recipient for relay. Use
`subscribe` when a human or agent just wants to watch the room timeline.

In the intended hierarchy:

- top-level panes use `room join`
- child panes usually do not join the room
- child progress goes to the parent first
- parent summaries and task updates go to the room

Inside tmux, room commands derive the sender from the current pane label. If the
pane has no label, `foxctl` falls back to a canonical id like
`tmux:<session>:%7`.

Inside zellij, room commands derive the sender from
`FOXCTL_ZELLIJ_PARTICIPANT` when present, otherwise they fall back to a
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
foxctl room relay alpha --backend zellij --session alpha-room
foxctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a session-local plugin that maps room member ids to pane
titles or canonical pane ids and writes messages into those panes. The first run needs a zellij
permission grant for:

- `ReadApplicationState`
- `WriteToStdin`
- `ReadCliPipes`

By default the Go relay looks for the plugin in:

- `--plugin-path`
- `FOXCTL_ZELLIJ_ROOM_PLUGIN`
- `~/.foxctl/plugins/zellij_room_relay.wasm`
- a repo-local build at `plugins/zellij-room-relay/...`

When run from the `foxctl` repo, it can auto-build the plugin source if the
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
foxctl tmux observe agent-b --lines 80
foxctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/runtime/actor/supervisor.go"
```

`foxctl tmux observe` reads the latest bridge message in the target pane, converts it into an ACA observation, and stores pane/session bridge refs as evidence.

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
- [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md)
- [configs/skills-pack/foxctl-tmux/SKILL.md](../../configs/skills-pack/foxctl-tmux/SKILL.md)
