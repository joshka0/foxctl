---
name: agentctl-room
description: "Durable multi-agent room coordination with shared chat, direct requests, actionable inboxes, plugin-backed zellij/tmux relay, room tasks, and a central room loop that broadcasts task transitions."
---

## What I do

- Keep a shared room timeline durable in `agentctl`, not trapped in pane scrollback.
- Relay room messages into `tmux` or `zellij` panes.
- Track room-scoped tasks and broadcast completion/status changes to participants.

## When to use me

- More than two agents need the same shared conversation.
- You want a central chat room with live terminal fanout.
- You want shared todos visible to everyone in the room.
- You want task completion to be announced automatically.

## Mental model

- `agentctl room` is the source of truth.
- `room send` writes durable chat messages.
- `room send --to <participant>` writes a direct room request instead of a broadcast.
- `room ack` marks a specific room message as acknowledged.
- `room resolve` lets the coordinator clear stale room messages once they have been handled out-of-band, and it resolves reminder chains by the original request id.
- `room inbox` shows actionable direct requests and pending ack/reply work for one participant.
- `room status` shows the coordinator-facing room pulse: participants, task counts, stale work, and compact actionable backlog summaries.
- `room status --only blocked,stale,reply` narrows the action summary to the coordinator lane you care about right now.
- `room status --verbose` includes richer top-entry detail for debugging without making the default coordinator view noisy.
- `room coordinator set` transfers coordinator ownership to another room participant.
- `room send --to @coordinator` resolves to the current coordinator without hard-coding an actor id.
- `room relay` mirrors room messages into terminal panes.
- `mux submit` is the convenience submit gesture when a live pane already has drafted text waiting in its composer.
- `room interview` runs a durable round-robin clarification loop inside the room.
- `room epic`, `room milestone`, `room story`, and `room log` give the room an agile-shaped long-running delivery structure.
- `room epic resume` and `room epic next` give that agile layer a resumable continuity surface.
- when that agile layer is the main workflow, explicitly use `agentctl-room-agile` instead of relying on this broader room skill alone.
- `room remind` schedules bounded durable follow-ups for direct requests.
- `room task` links shared tasks to the room.
- `room loop` runs the central coordination loop:
  - relay new messages
  - watch room tasks
  - broadcast task status changes back into the room
  - nudge stale direct requests and stale claimed tasks with reminder pulses
  - nudge the coordinator when unresolved work still needs oversight

Do not rely on scrollback as canonical history. The room log is canonical.

## Operating contract

When you are working inside an active room, follow this behavior by default:

- Read `room status` first to understand the current coordinator, pending work, and stale lanes.
- Read `room inbox --actor <you>` before starting new work; direct requests and ack/reply obligations take priority over browsing the whole timeline.
- If a task is assigned to you, `claim` it before starting real work.
- If a task is in progress for a while, `touch` it instead of posting vague “still working” chat updates.
- If you are blocked, use `room task block` with a concrete reason instead of only chatting about the problem.
- When work is done, use `room task complete --notes ...` so the outcome is durable and visible in the room.
- Use `room send --to @coordinator` when you need escalation, reassignment, or a decision from the coordinator.
- Do not duplicate work on a task that is already claimed unless the coordinator explicitly reassigns or reclaims it.
- Treat the room timeline as the durable audit trail; use direct room messages for requests, not ad hoc pane-only chat.

Role expectations:

- `coordinator`
  - keeps assignments, replies, and stale work moving
  - uses `room status`, `room resolve`, `room coordinator set`, and coordinator-only task actions
  - is the final authority on task routing and review closure
- `reviewer`
  - posts findings first, then approval or block verdict
  - uses room tasks and direct requests instead of passive observation
- general participant
  - claims assigned work explicitly
  - keeps task heartbeat current
  - escalates blockers through the room instead of assuming others noticed

Live-pane convenience:

- if a room relay or direct live nudge leaves text sitting in an agent composer, use `agentctl mux submit ...` instead of retyping or sending ad hoc key presses
- tmux supports targeted submit by pane label or pane id
- zellij submit currently applies to the focused pane in the named session

Interview protocol:

- use `agentctl room interview start` when a spec or plan needs meaning checks before implementation
- use `agentctl room interview ask` for one directed question at a time
- use `agentctl room interview answer` only from the intended respondent or coordinator
- use `agentctl room interview verify` from the verifier or coordinator to record `accept`, `clarify`, or `reject`
- use `agentctl room interview next --actor <you>` to fetch the next concrete interview obligation instead of rereading the whole room
- use `agentctl room status --only interview` when the coordinator wants just the unresolved interview lane

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

- when a tmux pane is created with `agentctl mux create --agent ... --room-id <room-id>` and direct room access, agentctl injects a lightweight startup prompt into that pane
- the prompt tells the agent to read `agentctl-tmux` and `agentctl-room`, then start with `room status`, `room inbox`, and `room task list` for the attached room

## Default room workflow

Use this sequence unless the room already has a more specific protocol:

```bash
# 1. orient
agentctl room status <room-id>
agentctl room inbox <room-id> --actor <you>

# 2. take work
agentctl room task claim <room-id> --id <task-id>

# 3. keep heartbeat current during longer work
agentctl room task touch <room-id> --id <task-id>

# 4. escalate or ask for a decision
agentctl room send <room-id> "Need coordinator input on <issue>" --to @coordinator --reply-expected

# 5. close with durable notes
agentctl room task complete <room-id> --id <task-id> --notes "..."
```

When the room is in a meaning-check or spec-clarification phase, use this interview loop instead:

```bash
agentctl room interview start <room-id> <topic> \
  --spec "<summary>" \
  --submitter <submitter> \
  --questioner <questioner> \
  --respondent <respondent> \
  --verifier <verifier>

agentctl room interview ask <room-id> <session-id> "Question text" --sender <questioner>
agentctl room interview next <room-id> --actor <respondent>
agentctl room interview answer <room-id> <question-id> "Answer text" --sender <respondent>
agentctl room interview next <room-id> --actor <verifier>
agentctl room interview verify <room-id> <answer-id> accept "Matches the intended meaning" --sender <verifier>
agentctl room status <room-id> --only interview
```

When the room is running a longer agile tranche, use this structure:

```bash
agentctl room epic start <room-id> "Delivery ledger and agile room model" \
  --goal "Give the room a durable epic/milestone/story hierarchy" \
  --owner human-a \
  --scope room \
  --scope gui-agent

agentctl room epic ask <room-id> <epic-id> "What must be true before milestones can open?" --kind success --to human-a
agentctl room epic ask <room-id> <epic-id> "What constraints must the first tranche respect?" --kind constraint --to human-a
agentctl room epic answer <room-id> <question-id> "The epic needs a clarified brief and no open intake questions." --sender human-a
agentctl room epic finalize <room-id> <epic-id> "Clarified brief: ship the room agile layer first, then surface it in the GUI."
agentctl room epic shape <room-id> <epic-id>

agentctl room epic show <room-id> <epic-id>
agentctl room milestone start <room-id> <epic-id> --proposal <proposal-id>

agentctl room milestone criteria <room-id> <milestone-id> "Epic and milestone hierarchy is visible via show commands"
agentctl room story add <room-id> <milestone-id> "Implement CLI flow" "Add epic, milestone, story, and log commands." --owner gemini-a
agentctl room milestone review <room-id> <milestone-id> pass "Foundation slice met the milestone criteria"
agentctl room log append <room-id> <epic-id> "Foundation landed" \
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
agentctl room remind add <room-id> <participant> "Check MR !26 and report status" \
  --every 15m \
  --max-iterations 3 \
  --reply-expected

agentctl room remind list <room-id>
agentctl room remind cancel <room-id> <reminder-id>
```

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
verified: go test -tags=libsqlite3 ./internal/web/api ./cmd/agentctl/cmd -run '...'; go build -tags=libsqlite3 ./cmd/agentctl
remaining: gui-agent auth still uses local dev identity in local mode
```

Example review note:

```text
result: approved
findings: 0 blocking, 1 non-blocking follow-up
scope: /loop API, coordinator gating, reminder floor behavior
```

Default room policy:

- the participant who creates the room becomes `coordinator` when agentctl can derive the current pane identity
- top-level agents may join the room
- child panes stay parent-private by default
- parents forward child summaries or task results into the room when appropriate
- the coordinator is responsible for keeping assignments, replies, and stale work on track

## Quick start

```bash
agentctl room create alpha --title "Alpha Room"
agentctl room join alpha agent-a --role lead
agentctl room join alpha agent-b --role reviewer
agentctl room send alpha "Review the retry path in client.ts"
agentctl room send alpha "Claude, please review the retry path." --to claude-a --reply-expected
agentctl room send alpha "Coordinator, please reassign the blocked task." --to @coordinator
agentctl room ack alpha <message-id>
agentctl room resolve alpha <message-id> --mode acked
agentctl room resolve alpha --all --only ack
agentctl room coordinator set alpha gemini-a
agentctl room inbox alpha --actor claude-a
agentctl room status alpha
agentctl room subscribe alpha --follow
```

## Shared task flow

```bash
agentctl room task add alpha \
  --title "Refactor retry path" \
  --description "Flatten duplicate recovery branches"

agentctl room task list alpha

agentctl room task assign alpha --id <task-id> --to gemini-a --notes "Take first pass"
agentctl room task claim alpha --id <task-id>
agentctl room task touch alpha --id <task-id>
agentctl room task block alpha --id <task-id> --reason "waiting on benchmark data"
agentctl room task unblock alpha --id <task-id>

agentctl room task complete alpha \
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

Use `room task claim` before doing real work. That is the guardrail that keeps multiple top-level agents from stepping on the same task.

## Live relay

### tmux

```bash
agentctl room relay alpha --backend tmux
agentctl room loop alpha --backend tmux --pulse 30s --reply-stale 2m --task-stale 5m
```

### zellij

```bash
agentctl room relay alpha --backend zellij --session alpha-room
agentctl room loop alpha --backend zellij --session alpha-room
```

The zellij backend uses a local plugin and matches room member ids to zellij pane titles or canonical pane ids.

Important restart rule for zellij:

- an existing zellij pane is not room-bound just because it lives in session
  `alpha-room`
- for reliable delivery, each pane that should participate must run:

```bash
agentctl room join alpha --current --role <room-role>
```

- this captures the current zellij pane binding so direct and broadcast room
  relay can target that pane correctly
- if `AGENTCTL_ROOM_ID` is missing in the pane environment, the pane was not
  launched with room metadata and must be joined explicitly
- session-only assumptions are unsafe for multi-pane zellij rooms; pane binding
  is the correct unit of room membership

## Conventions

- Use stable actor ids like `agent-a`, `agent-b`, `reviewer`, `planner` when you want human-friendly names.
- `room send` and `room task` derive the sender from the current tmux/zellij pane when possible.
- Broadcast room messages should not expect a response.
- Use `--to <participant>` plus `--reply-expected` for direct asks.
- Use `--to <participant>` plus `--ack-required` when you only need confirmation.
- Use `room inbox` as the actionable queue for each participant; it is not a full archive.
- `room join <room-id> --current` registers the current pane without hand-writing the id.
- In `tmux`, room member ids can be pane labels or canonical ids like `tmux:<session>:%7`.
- In `zellij`, room member ids can be pane titles or canonical ids like `zellij:<session>:terminal_3`.
- The sender should also be a room member if you want them excluded from fanout.
- Child panes launched with `agentctl tmux create --parent-participant ...` should usually use `agentctl tmux send-parent ...` instead of joining the room directly.
- Coordinator-only actions include `room resolve`, `room coordinator set`, `room task assign`, `room task reassign`, and `room task reclaim`.
- `room send --to @coordinator` is preferred over hard-coding the coordinator actor id.
- Direct room requests should usually carry either `--ack-required` or `--reply-expected`; broadcasts usually should not.

## Typical pattern

```bash
# create durable room
agentctl room create review --title "Review Room"

# register participants
agentctl room join review codex-a --role lead
agentctl room join review claude-b --role reviewer
agentctl room join review gemini-c --role observer

# start the central loop
agentctl room loop review --backend tmux

# add shared work
agentctl room task add review --title "Audit retry ladder"

# chat in the room
agentctl room send review "Please check the 401 fallback branch."
```

## Limits / caveats

- The room log is durable, but relay delivery is best-effort.
- `room relay` only mirrors messages; it does not infer task changes.
- `room loop` is the higher-level coordinator when you want automatic task broadcasts and reminder pulses.
- The zellij path requires the room relay plugin to be available and permission-granted.

## Related

- `configs/skills-pack/agentctl-room-operator/SKILL.md`
- `configs/skills-pack/agentctl-room-agile/SKILL.md`
- `configs/skills-pack/agentctl-orchestrate/SKILL.md`
- `configs/skills-pack/agentctl-tmux/SKILL.md`
- `docs/general/tmux-collaboration.md`
