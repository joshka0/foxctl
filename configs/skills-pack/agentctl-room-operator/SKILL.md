---
name: agentctl-room-operator
description: "Operate inside an existing agentctl room: orient from room status/inbox, claim and update room tasks correctly, escalate to @coordinator, and close work with durable completion or review notes."
---

# agentctl-room-operator

Use this skill when you are already working inside an existing `agentctl room` and need to behave correctly as a participant, reviewer, or coordinator.

This skill is about **room operating protocol**, not room creation.

## Use me when

- a room already exists and you need to pick up or continue work
- you were assigned a room task
- you need to reply to a direct room request
- you are acting as coordinator or reviewer in a live room
- you need to know whether to `send`, `ack`, `resolve`, `claim`, `touch`, `block`, or `complete`

## Core rules

- The room timeline is canonical. Pane scrollback is not.
- Start with `room status`, then `room inbox --actor <you>`.
- Direct obligations beat casual browsing of the timeline.
- If a task is assigned to you, `claim` it before doing real work.
- If work takes time, `touch` the task instead of posting vague status chat.
- If blocked, `room task block --reason ...` with a concrete reason.
- When done, `room task complete --notes ...` so the outcome is durable.
- Use `room send --to @coordinator` for escalation or routing decisions.
- Do not work a task that another participant already owns unless the coordinator reassigns or reclaims it.

## Room entry flow

Run this first:

```bash
agentctl room status <room-id>
agentctl room inbox <room-id> --actor <you>
agentctl room task list <room-id>
```

If you are in an existing `zellij` pane and the environment is missing
`AGENTCTL_ROOM_ID` / `AGENTCTL_PARTICIPANT`, you are not room-bound yet even if
the pane lives in the same zellij session as other room participants. Bind the
current pane explicitly before assuming relay or coordinator messages will land:

```bash
agentctl room join <room-id> --current --role <room-role>
```

Practical rule:

- `tmux` or `zellij` session membership is not the same thing as room membership
- each pane that should receive room traffic must be joined explicitly unless it
  was launched by `agentctl` with room metadata already exported
- do not assume a session-wide broadcast will reach unmanaged zellij panes

Then decide:

- direct ask waiting on you: answer or acknowledge it first
- assigned task waiting on you: claim it
- no direct ask, no assigned task: ask the coordinator or pick unclaimed work only if the room policy allows it

## Correct action by situation

### I need to answer a direct request

Use:

```bash
agentctl room send <room-id> "Your reply" --to <sender>
```

If the request only needs confirmation:

```bash
agentctl room ack <room-id> <message-id>
```

### I am starting assigned work

Use:

```bash
agentctl room task claim <room-id> --id <task-id>
```

### I am still working and want to keep heartbeat current

Use:

```bash
agentctl room task touch <room-id> --id <task-id>
```

### I am blocked

Use:

```bash
agentctl room task block <room-id> --id <task-id> --reason "waiting on <specific thing>"
agentctl room send <room-id> "Blocked on <specific thing>" --to @coordinator --reply-expected
```

### I finished the task

Use:

```bash
agentctl room task complete <room-id> --id <task-id> --notes "<completion note>"
```

### I need a coordinator decision

Use:

```bash
agentctl room send <room-id> "Need coordinator input on <issue>" --to @coordinator --reply-expected
```

### I am the coordinator

Use these as your primary controls:

```bash
agentctl room status <room-id>
agentctl room resolve <room-id> <message-id> --mode read
agentctl room task assign <room-id> --id <task-id> --to <participant>
agentctl room task reassign <room-id> --id <task-id> --to <participant> --reason "<reason>"
agentctl room task reclaim <room-id> --id <task-id> --reason "<reason>"
agentctl room coordinator set <room-id> <participant>
```

Coordinator responsibility:

- keep assignments explicit
- keep stale work moving
- close reminder noise when it has already been handled
- make the final call on routing and review closure

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
verified: go test -tags=libsqlite3 ./internal/web/api ./cmd/agentctl/cmd -run '...'; npm --prefix packages/gui-agent run build
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

## Related

- `configs/skills-pack/agentctl-room/SKILL.md`
- `configs/skills-pack/agentctl-tmux/SKILL.md`
- `configs/skills-pack/tmux-bridge/SKILL.md`
- `docs/general/tmux-collaboration.md`
