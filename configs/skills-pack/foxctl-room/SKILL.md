---
name: foxctl-room
description: "Run durable multi-agent rooms with transport-first delivery, participant state, room tasks, and optional tmux or zellij viewers."
---

## What I do

- Keep a shared room timeline durable in `foxctl`, not trapped in pane scrollback.
- Deliver room messages through foxctl-owned participant transport first.
- Treat `tmux` and `zellij` as optional viewers and manual consoles, not the canonical room transport.
- Track room-scoped tasks and broadcast completion/status changes to participants.

## When to use me

- More than two agents need the same shared conversation.
- You want a central chat room with durable delivery independent of whether a mux viewer is attached.
- You want shared todos visible to everyone in the room.
- You want task completion to be announced automatically.

## Mental model

- `foxctl` is the headless room/runtime kernel.
- CLI, web/API, `gui-agent`, mux panes, and chat adapters are clients or presentation layers on top of that kernel.
- `foxctl room` is the source of truth.
- room-scoped clients should use `GET /api/rooms/{room-id}/events?workspace_id=...` instead of subscribing to the global `/api/events` feed and filtering `room.message` client-side.
- room member payloads expose `delivery_binding` as the authoritative routing/binding record; older top-level fields like `backend`, `session`, `pane_id`, `transport_endpoint`, and `transport_kind` are compatibility mirrors while clients migrate.
- `room send` writes durable chat messages and triggers participant transport first; mux viewer delivery is secondary.
- **`room send` routing:** prefer **`--to <participant-id>`** for anything meant for **one** agent so relay and inbox routing stay unambiguous; omit `--to` only when you **intentionally** broadcast to everyone else in the room.
- **`room send` identity:** prefer **`--sender <your-participant-id>`** whenever the shell is not clearly bound to a mux pane (running outside tmux/zellij, scripts, or MCP); when you are inside the correct pane, sender can be omitted because foxctl infers it.
- `room send --to <participant>` writes a direct room request instead of a broadcast.
- the `gui-agent` `Term` surface is a room control plane, not a browser transport:
  - PTY preview is read-only presentation
  - direct/broadcast sends still go through room transport
  - reminder creation/cancel is a durable room action, not a browser timer
- `room ack` marks a specific room message as acknowledged.
- `room resolve` lets the coordinator clear stale room messages once they have been handled out-of-band, and it resolves reminder chains by the original request id.
- reply-required satisfaction is chain-aware:
  a later message only satisfies an earlier request when it is from the intended recipient and belongs to the same room message chain.
- `room inbox` shows actionable direct requests and pending ack/reply work for one participant.
- `room status` shows the coordinator-facing room pulse: participants, task counts, stale work, and compact actionable backlog summaries.
- `room status` is the coordinator-facing room/task pulse; `room pulse` is a separate room-wide epic mission-control surface.
- `room status --only blocked,stale,reply` narrows the action summary to the coordinator lane you care about right now.
- `room status --verbose` includes richer top-entry detail for debugging without making the default coordinator view noisy.
- `room status` and the room-loop API expose persisted `last_delivery_trace` data:
  use that trace to inspect the chosen binding, transport, fallback attempt, outcome, and cursor movement for the most recent delivery decision.
- `room coordinator set` transfers coordinator ownership to another room participant.
- `room send --to @coordinator` resolves to the current coordinator without hard-coding an actor id.
- actor naming should be explicit and distinctive:
  avoid generic ids like `coordinator`, `reviewer`, or `codex-coordinator` when multiple rooms or agent runtimes may coexist.
  actor ids should include the feature, branch, room, or workstream they belong to, for example `feat-room-loop-codex`, `room-ci-reviewer-a`, or `docs-runtime-owner`.
- `room relay` mirrors room messages into terminal panes when you want a viewer layer, but room transport no longer depends on viewer attachment.
- `room task assign`, `room task claim`, `room task complete`, and other task transitions persist first, then use the same transport-first relay path as `room loop`.
- task/control transitions are intentionally strict:
  assigning already-claimed work, claiming canceled work, reassigning unowned work, completing non-`in_progress` work, or unblocking non-`blocked` work should be treated as invalid state-machine transitions.
- `room send` inside tmux/zellij may still use a sender-pane submit convenience when sender identity is inferred from the current pane; when you pass an explicit `--sender`, foxctl skips that local mux-submit hook by default.
- `room interview` runs a durable round-robin clarification loop inside the room.
- `room epic`, `room milestone`, `room story`, and `room log` give the room an agile-shaped long-running delivery structure.
- `room epic resume` and `room epic next` give that agile layer a resumable continuity surface.
- when that agile layer is the main workflow, explicitly use `foxctl-room-agile` instead of relying on this broader room skill alone.
- `room remind` schedules bounded durable follow-ups for direct requests.
- `room remind` is first-class in the API / GUI too:
  - use the same request body as the original request being followed up on
  - choose the reminder recipient explicitly
  - treat the reminder as loop-owned follow-up, not a second ad hoc chat thread
- recurring reminder schedules are not cleared by acknowledging one emitted reminder instance:
  the schedule continues until the root request is satisfied, `max_iterations` is reached, or linked work is completed.
- reminders may optionally link to `task_id`, `story_id`, or `milestone_id` so loop-owned follow-ups can auto-stop when that tracked work is finished.
- `room task` links shared tasks to the room.
- `room task add` now uses an explicit lane contract:
  - when you omit `--milestone-id`, new room tasks attach to the newest open epic's default quiet `Chores` milestone when one exists
  - when you need milestone-scoped work instead of quiet chores, pass `--milestone-id <milestone-id>` explicitly
  - do not infer task lane from title keywords or free-form text
- `room loop` runs the central coordination loop:
  - trigger participant delivery for new messages
  - watch room tasks
  - broadcast task status changes back into the room
  - nudge stale direct requests and stale claimed tasks with reminder pulses
  - nudge the coordinator when unresolved work still needs oversight

Do not rely on scrollback as canonical history. The room log is canonical.

Architecture rule:

- runtime behavior belongs in shared room services
- the CLI is a client, not the canonical implementation
- mux attachment is presentation, not room truth
- `bash tests/regression/run.sh` is the canonical regression entrypoint for the hardened room-runtime invariants.

Membership rule:

- room-aware startup context is not enough by itself
- a pane is only a room participant if `room status` shows it
- if a pane has room env vars or a startup note but is missing from `room status`, explicitly `room join` it or repair the binding with `room rebind`
- if an existing participant runtime needs to be launched or resumed after a restart, prefer `foxctl room restore` over manually chaining `mux create` plus `room rebind`

Execution-mode rule:

- A room can exist with no panes and no live participant runtimes.
- Epic, milestone, story, task, reminder, and timeline state remain durable even when no viewer is attached.
- Bring up panes only when you want live terminal-backed runtimes or human PTY inspection.
- Think in this order:
  - room state
  - participant membership
  - participant transport/runtime
  - viewer attachment
- Do not collapse those layers into one idea like "the pane is the room."

## Operating contract

When you are working inside an active room, follow this behavior by default:

- For **`room send`**, **default to explicit addressing**: pass **`--to <participant>`** for directed work (peer, coordinator, assignee) and **`--sender <you>`** when identity would otherwise be ambiguous. Only omit `--to` when a **room-wide** broadcast is what you intend (everyone else gets live relay + inbox visibility).
- Read `room status` first to understand the current coordinator, pending work, and stale lanes.
- Read `room inbox --actor <you>` before starting new work; direct requests and ack/reply obligations take priority over browsing the whole timeline.
- If a task is assigned to you, `claim` it before starting real work.
- If a task is in progress for a while, `touch` it instead of posting vague “still working” chat updates.
- If you are blocked, use `room task block` with a concrete reason instead of only chatting about the problem.
- When work is done, use `room task complete --notes ...` so the outcome is durable and visible in the room.
- Use `room send --to @coordinator` when you need escalation, reassignment, or a decision from the coordinator.
- Do not duplicate work on a task that is already claimed unless the coordinator explicitly reassigns or reclaims it.
- If a task action is rejected, do not brute-force it with a different command:
  pick the explicit lifecycle action that matches the current state (`reassign`, `reclaim`, `unblock`, or `abandon`).
- Treat the room timeline as the durable audit trail; use direct room messages for requests, not ad hoc pane-only chat.
- Treat participant transport state as distinct from mux viewer state. A named pane or session does not prove a live participant runtime.

Role expectations:

- `coordinator`
  - keeps assignments, replies, and stale work moving
  - uses `room status`, `room resolve`, `room coordinator set`, and coordinator-only task actions
  - is the final authority on task routing and review closure
  - owns high-privilege room mutations such as room patching, full member replacement, and role-changing member binding updates
- `reviewer`
  - posts findings first, then approval or block verdict
  - uses room tasks and direct requests instead of passive observation
- general participant
  - claims assigned work explicitly
  - keeps task heartbeat current
  - escalates blockers through the room instead of assuming others noticed

Live-pane behavior:

- Prefer **`room send`**, **`room task`**, and **`room relay` / `room loop`**: they deliver through participant transport first and choose the provider-appropriate submit behavior for the target.
- wrapped panes export canonical participant identity into the child process, so provider replies should use the participant id (for example `codex-a`) instead of a raw mux pane id.
- Do **not** treat a hidden `mux submit` command as part of the normal workflow; it exists only as a legacy escape hatch.
- If you are using tmux/zellij only as a viewer, the room still works. Use `room relay` / `room loop` when you want live pane visibility layered on top of the durable room.

Two valid operating modes:

- `durable-room only`
  - no live participant runtime required
  - no pane required
  - use when humans are coordinating, planning, or reassigning work and no agent needs to actively execute
- `live-agent mode`
  - live participant runtime required
  - pane is optional presentation, but often the practical host for Claude/Codex/Droid terminals
  - use when agents need to actively receive room messages and work tasks
  - prefer `foxctl room restore <room-id> <actor-id> --agent <provider> [--agent-session-id <provider-session>]` when reviving an existing participant
- `room loop` is not required just to preserve the room; it is required when you need reminders, stale-work nudges, coordinator pulses, or automatic rebroadcast behavior
- persisted loop state now includes reminder/pulse operational memory:
  restart should resume counters, suppression windows, and coordinator pulse state rather than behaving like a fresh loop.

Interview protocol:

- use `foxctl room interview start` when a spec or plan needs meaning checks before implementation
- use `foxctl room interview ask` for one directed question at a time
- use `foxctl room interview answer` only from the intended respondent or coordinator
- use `foxctl room interview verify` from the verifier or coordinator to record `accept`, `clarify`, or `reject`
- use `foxctl room interview next --actor <you>` to fetch the next concrete interview obligation instead of rereading the whole room
- use `foxctl room status --only interview` when the coordinator wants just the unresolved interview lane

MCP exposure:

- the MCP facade exposes this as `room_interview`
- actions: `start`, `ask`, `answer`, `verify`, `next`, `show`
- prefer `room_interview.next` or `room status --only interview` when you need the next actionable clarification item rather than raw transcript history
- the MCP facade also exposes `room_remind`
- actions: `add`, `list`, `cancel`
- use it for scheduled check-ins like “check MR !26 in 15 minutes and reply with status”
- the MCP facade also exposes `room_agile`
- actions: `epic_start`, `epic_ask`, `epic_answer`, `epic_finalize`, `epic_shape`, `epic_show`, `milestone_start`, `milestone_criteria`, `milestone_review`, `milestone_show`, `story_add`, `story_show`, `log_append`, `log_show`
- actions: `epic_start`, `epic_ask`, `epic_answer`, `epic_finalize`, `epic_shape`, `epic_show`, `epic_resume`, `epic_next`, `milestone_start`, `milestone_criteria`, `milestone_review`, `milestone_show`, `story_add`, `story_show`, `log_append`, `log_show`
- use it when the room needs a durable epic/milestone/story structure instead of only free-form chat and tasks

Startup injection:

- when a tmux pane is created with `foxctl mux create --agent ... --room-id <room-id>` and direct room access, foxctl injects a lightweight startup prompt into that pane
- those panes are wrapped by `foxctl pane serve`, auto-register participant transport, and expose viewer metadata separately from room membership
- the prompt tells the agent to read `foxctl-room` and `foxctl-room-agent`, then start with `room status`, `room inbox`, and `room task list` for the attached room
- if that startup check still shows only the coordinator or omits the expected participant, stop and fix membership first with `room join`, `room restore`, or `room rebind` before assuming relay will work

Source-panel agent creation:

- when an agent is spawned with `--room-id <room-id>` and `--spawn-in-pane`, foxctl creates a dedicated mux pane in a room-scoped session (e.g., `room-<room-id>` for tmux) and runs the agent live in that pane
- the spawned agent is automatically joined as a room member and registers transport metadata so room delivery does not rely on that pane remaining attached
- agents in source panes run `foxctl agent run <agent-id>` instead of just `foxctl agent watch`, so users can watch agents work together in a shared tmux session
- `room task assign --provision-pane` auto-creates a mux pane for the assignee when they don't already have one; use `--pane-agent codex` or `--pane-agent claude` to choose the agent CLI
- the room loop and room relay deliver messages to source panes the same way as any other transport-registered room participant

## Default room workflow

Use this sequence unless the room already has a more specific protocol:

```bash
# 1. orient
foxctl room status <room-id>
foxctl room inbox <room-id> --actor <you>

# 2. take work
foxctl room task claim <room-id> --id <task-id> --sender <you>

# 3. keep heartbeat current during longer work
foxctl room task touch <room-id> --id <task-id> --sender <you>

# 4. escalate or ask for a decision (directed: prefer --to and --sender)
foxctl room send <room-id> --to @coordinator --sender <you> --reply-expected "Need coordinator input on <issue>"

# 5. close with durable notes
foxctl room task complete <room-id> --id <task-id> --notes "..." --sender <you>
```

Task creation rule:

- low-urgency chores and follow-up debt should usually use plain `room task add` and rely on the default quiet chores lane
- milestone execution work should use `room task add --milestone-id <milestone-id>` so the task is explicitly attached to the current delivery lane
- if you are not sure which milestone owns the work, check `room milestone show` first instead of guessing

When the room is in a meaning-check or spec-clarification phase, use this interview loop instead:

```bash
foxctl room interview start <room-id> <topic> \
  --spec "<summary>" \
  --submitter <submitter> \
  --questioner <questioner> \
  --respondent <respondent> \
  --verifier <verifier>

foxctl room interview ask <room-id> <session-id> "Question text" --sender <questioner>
foxctl room interview next <room-id> --actor <respondent>
foxctl room interview answer <room-id> <question-id> "Answer text" --sender <respondent>
foxctl room interview next <room-id> --actor <verifier>
foxctl room interview verify <room-id> <answer-id> accept "Matches the intended meaning" --sender <verifier>
foxctl room status <room-id> --only interview
```

When the room is running a longer agile tranche, use this structure:

```bash
foxctl room epic start <room-id> "Delivery ledger and agile room model" \
  --goal "Give the room a durable epic/milestone/story hierarchy" \
  --owner human-a \
  --scope room \
  --scope gui-agent

foxctl room epic ask <room-id> <epic-id> "What must be true before milestones can open?" --kind success --to human-a
foxctl room epic ask <room-id> <epic-id> "What constraints must the first tranche respect?" --kind constraint --to human-a
foxctl room epic answer <room-id> <question-id> "The epic needs a clarified brief and no open intake questions." --sender human-a
foxctl room epic finalize <room-id> <epic-id> "Clarified brief: ship the room agile layer first, then surface it in the GUI."
foxctl room epic shape <room-id> <epic-id>

foxctl room epic show <room-id> <epic-id>
foxctl room milestone start <room-id> <epic-id> --proposal <proposal-id>

foxctl room milestone criteria <room-id> <milestone-id> "Epic and milestone hierarchy is visible via show commands"
foxctl room story add <room-id> <milestone-id> "Implement CLI flow" "Add epic, milestone, story, and log commands." --owner gemini-a
foxctl room milestone review <room-id> <milestone-id> pass "Foundation slice met the milestone criteria"
foxctl room log append <room-id> <epic-id> "Foundation landed" \
  --completed "CLI hierarchy shipped" \
  --in-flight "GUI surfacing" \
  --next "Wire status and planning views"
```

Agile room model:

- `epic` is the long-running objective for the room
- `epic` starts in discovery, not execution
- use typed epic intake questions: `product`, `technical`, `constraint`, `success`
- use `epic ask` / `epic answer` / `epic finalize` to clarify the brief before opening milestones
- use `epic shape` after finalization to write durable milestone proposals back into the room
- `milestone` is the current bounded tranche under that epic
- `story` is a concrete work item under a milestone
- `acceptance criteria` are attached to the milestone, not buried in chat
- `delivery log` is the durable session-to-session progress journal

For bounded scheduled follow-ups, use:

```bash
foxctl room remind add <room-id> <participant> "Check MR !26 and report status" \
  --every 15m \
  --max-iterations 3 \
  --reply-expected

foxctl room remind list <room-id>
foxctl room remind cancel <room-id> <reminder-id>
```

For room-wide manual submit / interrupt across room members, use:

```bash
foxctl mux submit-all --room <room-id> --workspace .
foxctl mux interrupt-all --room <room-id> --workspace .
```

These are viewer/control conveniences layered on top of canonical room participants. They should target canonical room participants, not raw mux ids.

## Task note format

Use consistent completion and review notes so other agents do not need to reread the whole room.

Implementation/completion notes should include:

- `changed`: what was implemented
- `verified`: what commands/tests/manual checks were run
- `remaining`: any known gaps or follow-up items

Review notes should include:

- `result`: `approved` or `blocked`
- `findings`: count and severity summary
- `scope`: files/components/behavior reviewed

Example completion note:

```text
changed: wired durable loop GET/PATCH and runtime persistence
verified: make test-short; make build
remaining: gui-agent auth still uses local dev identity in local mode
```

Example review note:

```text
result: approved
findings: 0 blocking, 1 non-blocking follow-up
scope: /loop API, coordinator gating, reminder floor behavior
```

Default room policy:

- the participant who creates the room becomes `coordinator` when foxctl can derive the current pane identity
- top-level agents may join the room
- child panes stay parent-private by default
- parents forward child summaries or task results into the room when appropriate
- the coordinator is responsible for keeping assignments, replies, and stale work on track

## Quick start

```bash
foxctl room create alpha --title "Alpha Room"
foxctl room join alpha agent-a --role lead
foxctl room join alpha agent-b --role reviewer
foxctl room send alpha "Review the retry path in client.ts"
foxctl room send alpha "Claude, please review the retry path." --to claude-a --reply-expected
foxctl room send alpha "Coordinator, please reassign the blocked task." --to @coordinator
foxctl room ack alpha <message-id>
foxctl room resolve alpha <message-id> --mode acked
foxctl room resolve alpha --all --only ack
foxctl room coordinator set alpha gemini-a
foxctl room inbox alpha --actor claude-a
foxctl room status alpha
foxctl room subscribe alpha --follow
```

### Source-pane agent spawning

Spawn agents directly into mux panes so they are visible alongside other room participants:

```bash
# Spawn an agent into a room-scoped tmux session pane
foxctl agent spawn --role researcher \
  --prompt "Review the retry path" \
  --room-id alpha \
  --spawn-in-pane \
  --participant-id reviewer-a \
  --exec-mode autonomous \
  --max-auto-turns 3

# Assign a task and auto-provision a pane for the assignee
foxctl room task assign alpha --id <task-id> --to coder-a --provision-pane --pane-agent codex
```

## Shared task flow

```bash
foxctl room task add alpha \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery branches"

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

This writes task lifecycle events back into the room timeline so everyone sees them.

Task lifecycle is intentionally lightweight:

- `pending -> in_progress -> blocked -> completed`
- `assign` records intended ownership and sends a direct task request, but the assignee still claims the task explicitly
- assigned tasks are claimable only by the assignee until they are reassigned, reclaimed, or abandoned
- `abandon` returns a task to `pending`
- `complete` requires the current participant to claim the task first
- `touch` refreshes the owner heartbeat without changing task state
- `block` / `unblock` preserve ownership while making the stall visible to everyone
- `complete` and `block` are only valid from a real `in_progress` owner state.
- `reassign` is for retargeting already-owned or explicitly assigned work; `assign` is for untouched or reclaimed work.
- `reclaim` and `abandon` return work to a clean pending state so the next claim starts from explicit ownership.

Use `room task claim` before doing real work. That is the guardrail that keeps multiple top-level agents from stepping on the same task.

## Live relay

### tmux

```bash
foxctl room relay alpha --backend tmux
foxctl room loop alpha --backend tmux
```

### zellij

```bash
foxctl room relay alpha --backend zellij --session alpha-room
foxctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a local plugin and matches room member ids to zellij pane titles or canonical pane ids.

Important restart rule for zellij:

- an existing zellij pane is not room-bound just because it lives in session
  `alpha-room`
- for reliable delivery, each pane that should participate must run:

```bash
foxctl room join alpha --current --role <room-role>
```

- this captures the current zellij pane binding so direct and broadcast room
  relay can target that pane correctly
- if `FOXCTL_ROOM_ID` is missing in the pane environment, the pane was not
  launched with room metadata and must be joined explicitly
- session-only assumptions are unsafe for multi-pane zellij rooms; pane binding
  is the correct unit of room membership
- if an existing participant moves to a new pane and the runtime is already live, prefer `foxctl room rebind`
  to update only the stored transport binding
- if the runtime itself needs to be launched or resumed, prefer `foxctl room restore`

## Conventions

- Use stable but specific actor ids when you want human-friendly names.
- Do not use generic labels that can collide across rooms or sessions.
- Prefer names that include feature, branch, room, or workstream context, for example `feat-room-loop-agent-a`, `room-ci-reviewer-a`, or `docs-planner-a`.
- `room send` and `room task` derive the sender from the current tmux/zellij pane when possible.
- Broadcast room messages should not expect a response.
- Use `--to <participant>` plus `--reply-expected` for direct asks.
- Use `--to <participant>` plus `--ack-required` when you only need confirmation.
- Use `room inbox` as the actionable queue for each participant; it is not a full archive.
- Use `room relay` when you only need room-message fanout into panes/viewers.
- Use `room loop` when you need reminders, stale-reply nudges, coordinator pulses, or task-state broadcasts in addition to message fanout.
- If a room is using `room remind` or coordinator stale detection, `room loop` must be running; `room relay` alone is not enough.
- `room loop` reads persisted loop policy; change timing through `room loop patch` or the API, not runtime flags.
- `task_followup_interval` is independent from `pulse_interval`.
- `task_followup_interval=0` disables task follow-up check-ins while leaving reminder pulses active.
- `room join <room-id> --current` registers the current pane without hand-writing the id.
- `room rebind <room-id> <actor-id> --backend <tmux|zellij> --session <session> --pane-id <pane>`
  repairs a moved pane binding for an existing participant.
- `room restore <room-id> <actor-id> --agent <provider> --backend <tmux|zellij> [--agent-session-id <provider-session>]`
  launches or resumes the participant runtime and then rebinds the room member to that exact transport.
- In `tmux`, room member ids can be pane labels or canonical ids like `tmux:<session>:%7`.
- In `zellij`, room member ids can be pane titles or canonical ids like `zellij:<session>:terminal_3`.
- The sender should also be a room member if you want them excluded from fanout.
- Child panes launched with `foxctl tmux create --parent-participant ...` should usually use `foxctl tmux send-parent ...` instead of joining the room directly.
- Coordinator-only actions include `room resolve`, `room coordinator set`, `room task assign`, `room task reassign`, and `room task reclaim`.
- high-privilege API/control-surface mutations are coordinator-only too:
  room patch, full member replacement, and role-changing binding updates should not be treated as self-service operations.
- `room send --to @coordinator` is preferred over hard-coding the coordinator actor id.
- Direct room requests should usually carry either `--ack-required` or `--reply-expected`; broadcasts usually should not.

## Typical pattern

```bash
# create durable room
foxctl room create review --title "Review Room"

# register participants
foxctl room join review codex-a --role lead
foxctl room join review claude-b --role reviewer
foxctl room join review gemini-c --role observer

# start the central loop
foxctl room loop review --backend tmux

# add shared work
foxctl room task add review --title "Audit retry ladder"

# chat in the room
foxctl room send review "Please check the 401 fallback branch."
```

## Limits / caveats

- The room log is durable, but relay delivery is best-effort.
- `room relay` only mirrors messages; it does not infer task changes.
- `room loop` is the higher-level coordinator when you want automatic task broadcasts and reminder pulses.
- The zellij path requires the room relay plugin to be available and permission-granted.

## Related

- `configs/skills-pack/foxctl-room-operator/SKILL.md`
- `configs/skills-pack/foxctl-room-agent/SKILL.md`
- `configs/skills-pack/foxctl-room-agile/SKILL.md`
- `configs/skills-pack/foxctl-orchestrate/SKILL.md`
- `configs/skills-pack/foxctl-room-view/SKILL.md`
- `docs/general/tmux-collaboration.md`
