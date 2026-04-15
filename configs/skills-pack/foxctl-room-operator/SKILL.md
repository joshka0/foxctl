---
name: foxctl-room-operator
description: "Operate inside an existing foxctl room: orient from room status/inbox, claim and update room tasks correctly, escalate to @coordinator, and close work with durable completion or review notes."
---

# foxctl-room-operator

Use this skill when you are already working inside an existing `foxctl room` and need to behave correctly as a participant, reviewer, or coordinator.

This skill is about **room operating protocol**, not room creation.

If the room is explicitly running the agile epic/milestone/story workflow, also use `foxctl-room-agile`.

## Use me when

- a room already exists and you need to pick up or continue work
- you were assigned a room task
- you need to reply to a direct room request
- you are acting as coordinator or reviewer in a live room
- you need to know whether to `send`, `ack`, `resolve`, `claim`, `touch`, `block`, or `complete`
- you need to answer or verify a room interview question
- you need to manage a bounded scheduled follow-up for a participant
- you need to work inside an epic/milestone/story structure instead of only tasks and chat

## Core rules

- `foxctl` is the headless room/runtime kernel.
- CLI, web/API, `gui-agent`, mux panes, and chat adapters are clients or presentation layers over that kernel.
- The room timeline is canonical. Pane scrollback is not.
- Participant transport and mux presentation are different things. A pane label or mux session is not proof of live delivery.
- Room-scoped clients should prefer `GET /api/rooms/{room-id}/events?workspace_id=...` over the global `/api/events` feed for room timeline updates.
- Room member `delivery_binding` is the canonical transport/routing record. The mirrored top-level member transport fields are compatibility-only and should not be treated as new source-of-truth state.
- Reply-required work is chain-aware:
  a later message only satisfies an earlier request when it is from the intended recipient and belongs to the same room message chain.
- Room status and loop surfaces expose a durable `last_delivery_trace`:
  use that trace to debug the last chosen binding, transport, fallback attempt, and outcome instead of guessing from pane behavior.
- use explicit, distinctive participant ids.
- avoid generic actor ids like `coordinator`, `reviewer`, or `codex-coordinator` when multiple rooms or agent runtimes may coexist.
- actor ids should include feature, branch, room, or workstream context, for example `feat-room-loop-codex`, `room-ci-reviewer-a`, or `runtime-owner-a`.
- Room context and room membership are different things. `FOXCTL_ROOM_ID`, a tmux/zellij session name, or a startup prompt only mean the pane knows about the room. They do **not** prove the participant is joined and routable.
- Room durability and live execution are different things. A room can continue to hold epic/task/history state even when there are no panes or live participant runtimes.
- When invoking **`room send`**, **prefer explicit `--to <participant>`** for anything meant for one person (so relay + inbox stay aligned), and **`--sender <you>`** whenever your pane context might not identify you (scripts, MCP, or outside tmux/zellij). Omit `--to` only for deliberate **broadcasts** to the rest of the room.
- Start with `room status`, then `room inbox --actor <you>`.
- Direct obligations beat casual browsing of the timeline.
- If a task is assigned to you, `claim` it before doing real work.
- When creating a new room task, treat lane selection as explicit contract:
  - plain `room task add` defaults to the newest open epic's quiet `Chores` milestone when one exists
  - use `room task add --milestone-id <milestone-id>` for milestone-scoped execution work
  - do not use title keywords to decide where a task belongs
- If work takes time, `touch` the task instead of posting vague status chat.
- If blocked, `room task block --reason ...` with a concrete reason.
- When done, `room task complete --notes ...` so the outcome is durable.
- Use `room send --to @coordinator` for escalation or routing decisions.
- Do not work a task that another participant already owns unless the coordinator reassigns or reclaims it.
- If the room is using the interview protocol, use `room interview next --actor <you>` before browsing the full transcript.
- If the room is using the agile layer, orient from `room epic show`, `room milestone show`, or `room story show` instead of reconstructing the tranche from raw chat.
- Read the participant state shown by `room status` as four separate dimensions: membership, transport availability, runtime availability, and presentation attachment.

## Room entry flow

Run this first:

```bash
foxctl room status <room-id>
foxctl room inbox <room-id> --actor <you>
foxctl room task list <room-id>
foxctl room interview next <room-id> --actor <you>
```

If the room is using the agile layer, orient here too:

```bash
foxctl room epic show <room-id>
foxctl room milestone show <room-id>
foxctl room story show <room-id>
```

Practical rule:

- if an epic is still in `discovery` or `intake_in_progress`, do not start milestone work yet
- answer epic intake questions first
- milestones open only after the coordinator finalizes the epic brief
- after finalization, prefer `foxctl room epic shape <room-id> <epic-id>` so milestone proposals become durable room objects before execution starts
- when the coordinator opens a milestone from one of those proposals, prefer `foxctl room milestone start <room-id> <epic-id> --proposal <proposal-id>` over retyping the proposal by hand

If you are in an existing `zellij` pane and the environment is missing
`FOXCTL_ROOM_ID` / `FOXCTL_PARTICIPANT`, you are not room-bound yet even if
the pane lives in the same zellij session as other room participants. Bind the
current pane explicitly before assuming relay or coordinator messages will land:

```bash
foxctl room join <room-id> --current --role <room-role>
```

If the participant already exists in the room but moved to a different
`tmux`/`zellij` pane, update the transport binding instead of re-joining:

```bash
foxctl room rebind <room-id> <actor-id> --backend tmux --session <session> --pane-id <pane>
```

Practical rule:

- `tmux` or `zellij` session membership is not the same thing as room membership
- if `room status` does not show your participant, you are **not joined yet** even if the pane has room env vars or a room startup note
- each pane that should receive room traffic must either join explicitly or auto-register transport when launched by `foxctl`
- do not assume a session-wide broadcast will reach unmanaged panes
- task assign/claim/complete use the same transport-first relay path as `room relay`; pane delivery is a viewer effect, not the room’s source of truth
- if no agents are meant to execute right now, it is valid to leave the room in durable-room-only mode and avoid rebuilding panes until they are actually needed
- operator surfaces must converge on the same shared runtime truth; if CLI, API, and GUI disagree, treat that as a room-runtime bug, not normal behavior

Then decide:

- direct ask waiting on you: answer or acknowledge it first
- interview item waiting on you: answer or verify it first
- assigned task waiting on you: claim it
- no direct ask, no assigned task: ask the coordinator or pick unclaimed work only if the room policy allows it

## Correct action by situation

### I need to answer a direct request

Use:

```bash
foxctl room send <room-id> --to <sender> --sender <you> "Your reply"
```

If the request only needs confirmation:

```bash
foxctl room ack <room-id> <message-id>
```

### I am starting assigned work

Use:

```bash
foxctl room task claim <room-id> --id <task-id>
```

### I need to create new work

Use plain task creation for quiet chores:

```bash
foxctl room task add <room-id> --title "Follow up on reviewer debt"
```

Use explicit milestone linkage for active delivery work:

```bash
foxctl room task add <room-id> \
  --title "Implement current slice" \
  --milestone-id <milestone-id>
```

If you do not know the correct milestone, inspect `room milestone show <room-id>` first.

### I am still working and want to keep heartbeat current

Use:

```bash
foxctl room task touch <room-id> --id <task-id>
```

### I am blocked

Use:

```bash
foxctl room task block <room-id> --id <task-id> --reason "waiting on <specific thing>" --sender <you>
foxctl room send <room-id> --to @coordinator --sender <you> --reply-expected "Blocked on <specific thing>"
```

### I finished the task

Use:

```bash
foxctl room task complete <room-id> --id <task-id> --notes "<completion note>"
```

### I need a coordinator decision

Use:

```bash
foxctl room send <room-id> "Need coordinator input on <issue>" --to @coordinator --reply-expected
```

Prefer `--sender <you>` whenever you are not obviously in the correct room-bound pane.

### I need a durable scheduled follow-up

Use:

```bash
foxctl room remind add <room-id> <participant> "Check MR !26 and report status" \
  --every 15m \
  --max-iterations 3 \
  --reply-expected
```

This writes the original direct request now, then lets the room loop send bounded reminder follow-ups until the recipient replies or the retry budget is exhausted.

Operator rule:

- if you set or expect reminders, confirm `room loop` is running for that room
- `room relay` alone is only pane fanout; it will not drive reminder follow-ups or stale-reply nudges
- `room loop` reads persisted policy; do not expect runtime flags to change reminder or stale-work behavior
- `task_followup_interval=0` disables automatic task follow-up check-ins while leaving reminder follow-ups active
- acknowledging one emitted reminder instance does not stop the recurring schedule
- recurring reminders can also stop when linked `task_id`, `story_id`, or `milestone_id` work is completed

### I need to understand the current milestone

Use:

```bash
foxctl room milestone show <room-id>
foxctl room story show <room-id>
```

Read the milestone acceptance criteria before starting a story. If the criteria are unclear, use the room interview or planning protocol instead of guessing.

### I am the interview respondent

Use:

```bash
foxctl room interview next <room-id> --actor <you>
foxctl room interview answer <room-id> <question-id> "<answer>"
```

Do not answer by free-form room chat if the room is already using a durable interview session.

### I am the interview verifier

Use:

```bash
foxctl room interview next <room-id> --actor <you>
foxctl room interview verify <room-id> <answer-id> accept "<why it matches>"
foxctl room interview verify <room-id> <answer-id> clarify "<what is still ambiguous>"
foxctl room interview verify <room-id> <answer-id> reject "<why it diverged>"
```

The verifier is usually the original submitter or the coordinator.

### I am the coordinator

Use these as your primary controls:

```bash
foxctl room status <room-id>
foxctl room resolve <room-id> <message-id> --mode read
foxctl room task assign <room-id> --id <task-id> --to <participant>
foxctl room task reassign <room-id> --id <task-id> --to <participant> --reason "<reason>"
foxctl room task reclaim <room-id> --id <task-id> --reason "<reason>"
foxctl room coordinator set <room-id> <participant>
```

Coordinator responsibility:

- keep assignments explicit
- keep stale work moving
- close reminder noise when it has already been handled
- make the final call on routing and review closure
- distinguish transport failures from viewer-only problems before escalating relay bugs
- own coordinator-only mutation surfaces:
  full room patching, full member replacement, and role-changing member binding updates should not be delegated as self-service room actions

### I am the reviewer

Default reviewer behavior:

- findings first
- then verdict: `approved` or `blocked`
- then scope and any non-blocking follow-ups

Do not leave review conclusions only in pane chat. Write them into the room or task notes.

## Completion and review note templates

Implementation/completion notes should include:

- `changed`: what changed
- `verified`: tests/build/manual checks run
- `remaining`: known gaps or follow-ups

Example:

```text
changed: wired loop PATCH and persisted runtime state
verified: go test -tags=libsqlite3 ./internal/interfaces/web/api ./cmd/foxctl/cmd -run '...'; npm --prefix packages/gui-agent run build
remaining: local GUI auth still uses dev-local-user fallback
```

Review notes should include:

- `result`: `approved` or `blocked`
- `findings`: count and severity
- `scope`: files/components/behavior reviewed

Example:

```text
result: approved
findings: 0 blocking, 1 non-blocking follow-up
scope: /loop API, coordinator gating, reminder floor behavior
```

## Anti-patterns

Do not:

- treat pane scrollback as the room source of truth
- post “working on it” repeatedly instead of `task touch`
- silently start assigned work without `claim`
- resolve coordinator-only items if you are not acting with coordinator authority
- close a review gate without an explicit `approved` or `blocked` outcome
- treat a rejected task transition as a bug before you check the task lifecycle:
  `assign`, `reassign`, `claim`, `block`, `unblock`, `complete`, `reclaim`, and `abandon` now enforce a stricter state machine

## Verification

- Prefer `bash tests/regression/run.sh` as the canonical regression entrypoint when you need to re-check the hardened room-runtime invariants end to end.

## Related

- `configs/skills-pack/foxctl-room/SKILL.md`
- `configs/skills-pack/foxctl-room-agent/SKILL.md`
- `configs/skills-pack/foxctl-room-view/SKILL.md`
- `docs/general/tmux-collaboration.md`
