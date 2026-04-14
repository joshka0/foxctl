---
name: foxctl-room-agent
description: "Participant-agent protocol for transport-first rooms: startup, membership checks, inbox/task handling, durable replies, reminders, and escalation."
---

# foxctl-room-agent

Use this skill when you are a participant agent working inside an existing
`foxctl room`.

This skill is the participant-side companion to:

- `foxctl-room` for the canonical room model
- `foxctl-room-operator` for coordinator/reviewer operating protocol

## Core rules

- `foxctl` is the headless room/runtime kernel.
- CLI, web/API, `gui-agent`, and mux surfaces are clients or presentation layers over that kernel.
- `foxctl room` is the source of truth.
- Participant transport is canonical delivery.
- `tmux`, `zellij`, GUI PTY previews, and xterm/webterm are presentation only.
- room-aware clients should consume `GET /api/rooms/{room-id}/events?workspace_id=...` for room timeline updates rather than watching the global `/api/events` feed.
- when you inspect room member records, treat `delivery_binding` as canonical and treat the older top-level transport fields as compatibility mirrors.
- when you create room tasks, do not guess the lane from prose:
  - plain `room task add` defaults to the newest open epic's quiet `Chores` milestone when one exists
  - use `room task add --milestone-id <milestone-id>` when the work belongs to a specific active milestone
- reply-required requests are chain-aware:
  your later message only satisfies an earlier request when it replies within the same room message chain.
- `room status` and loop/status APIs can expose `last_delivery_trace` for the most recent delivery decision; use that trace when delivery behavior needs explanation.
- use explicit, distinctive participant ids.
- avoid generic actor ids like `coordinator`, `reviewer`, or `codex-coordinator` when more than one room or agent runtime may exist.
- actor ids should include feature, branch, room, or workstream context, for example `feat-room-loop-codex`, `room-ci-reviewer-a`, or `docs-operator-a`.
- Room context is not room membership. A startup note, env var, or pane label does not prove you are joined.
- Pane health is not delivery health. Check `room status` before blaming the viewer layer.
- A room can remain fully valid with no panes and no live participant runtime. In that state, the room is still usable for durable planning and task state, but you should not expect an agent runtime to consume direct work until transport/runtime is live again.

## Startup flow

Start here:

```bash
foxctl room status <room-id>
foxctl room inbox <room-id> --actor <you>
foxctl room task list <room-id>
```

Acceptance checks:

- your participant id is visible in `room status`
- transport is available if you are expected to receive live delivery
- any direct asks in your inbox are handled before new exploratory work

If you are not expected to execute right now, durable-room-only mode is valid:

- the room still exists
- task / epic / story state still exists
- you do not need to rebuild a pane just to preserve room state

If you are expected to execute right now, you need more than room membership:

- membership visible in `room status`
- live transport/runtime for delivery
- pane/viewer only if humans need presentation or PTY inspection
- if your runtime needs to be brought back after a restart, prefer `foxctl room restore <room-id> <actor-id> --agent <provider> [--agent-session-id <provider-session>]`

If the room does not show your participant, fix membership first:

```bash
foxctl room join <room-id> --current --role participant
```

If you moved to a new pane/session and already exist as a member, repair the binding:

```bash
foxctl room rebind <room-id> <actor-id> --backend <tmux|zellij> --session <session> --pane-id <pane>
```

Use `room rebind` only when the runtime is already alive and only the stored binding is wrong. Use `room restore` when the runtime itself needs to be launched or resumed.

## Default behavior

- Reply through the room, not pane-local chat.
- Prefer direct messages for one recipient:

```bash
foxctl room send <room-id> --to <participant> --sender <you> "Reply text"
```

- Use `room ack` when confirmation is enough.
- Claim assigned work before starting:

```bash
foxctl room task claim <room-id> --id <task-id>
```

- Use `room task touch` during longer work instead of vague status chatter.
- Use `room task block` with a concrete reason when blocked.
- Close work durably with `room task complete --notes ...`.
- If a task transition is rejected, assume the strict lifecycle guardrails are in effect and use the correct action for the current state (`reassign`, `reclaim`, `unblock`, or `abandon`).

## Reminders and loops

- `room relay` is viewer fanout only.
- `room loop` is required for reminder follow-ups, stale-reply nudges, and coordinator pulses.
- If you receive or set reminders, confirm the room loop is running.
- `room loop` is not the thing that makes the room exist; it is the thing that drives room automation.
- acknowledging one reminder follow-up does not cancel the recurring schedule by itself.
- reminders may stop automatically when linked `task_id`, `story_id`, or `milestone_id` work is completed.
- loop restart should preserve reminder and pulse memory instead of resetting the scheduler state.

Architecture rule:

- do not treat CLI behavior or pane behavior as the canonical implementation
- durable room state and shared room services are the source of truth
- use `bash tests/regression/run.sh` when you need the canonical regression bundle for room-runtime hardening.

## Escalation

Escalate through the room, not by hoping someone notices a pane:

```bash
foxctl room send <room-id> --to @coordinator --sender <you> --reply-expected "Need a decision on <issue>"
```

## When to use the view skill

Only reach for `foxctl-room-view` when you explicitly need:

- pane inspection
- viewer setup or placement
- a manual poke into a live terminal UI

Do not use viewer success as proof that room transport is healthy.
