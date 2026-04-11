---
name: agentctl-room-agent
description: "Participant-agent protocol for transport-first rooms: startup, membership checks, inbox/task handling, durable replies, reminders, and escalation."
---

# agentctl-room-agent

Use this skill when you are a participant agent working inside an existing
`agentctl room`.

This skill is the participant-side companion to:

- `agentctl-room` for the canonical room model
- `agentctl-room-operator` for coordinator/reviewer operating protocol

## Core rules

- `agentctl room` is the source of truth.
- Participant transport is canonical delivery.
- `tmux`, `zellij`, GUI PTY previews, and xterm/webterm are presentation only.
- Room context is not room membership. A startup note, env var, or pane label does not prove you are joined.
- Pane health is not delivery health. Check `room status` before blaming the viewer layer.

## Startup flow

Start here:

```bash
agentctl room status <room-id>
agentctl room inbox <room-id> --actor <you>
agentctl room task list <room-id>
```

Acceptance checks:

- your participant id is visible in `room status`
- transport is available if you are expected to receive live delivery
- any direct asks in your inbox are handled before new exploratory work

If the room does not show your participant, fix membership first:

```bash
agentctl room join <room-id> --current --role participant
```

If you moved to a new pane/session and already exist as a member, repair the binding:

```bash
agentctl room rebind <room-id> <actor-id> --backend <tmux|zellij> --session <session> --pane-id <pane>
```

## Default behavior

- Reply through the room, not pane-local chat.
- Prefer direct messages for one recipient:

```bash
agentctl room send <room-id> --to <participant> --sender <you> "Reply text"
```

- Use `room ack` when confirmation is enough.
- Claim assigned work before starting:

```bash
agentctl room task claim <room-id> --id <task-id>
```

- Use `room task touch` during longer work instead of vague status chatter.
- Use `room task block` with a concrete reason when blocked.
- Close work durably with `room task complete --notes ...`.

## Reminders and loops

- `room relay` is viewer fanout only.
- `room loop` is required for reminder follow-ups, stale-reply nudges, and coordinator pulses.
- If you receive or set reminders, confirm the room loop is running.

## Escalation

Escalate through the room, not by hoping someone notices a pane:

```bash
agentctl room send <room-id> --to @coordinator --sender <you> --reply-expected "Need a decision on <issue>"
```

## When to use the view skill

Only reach for `agentctl-room-view` when you explicitly need:

- pane inspection
- viewer setup or placement
- a manual poke into a live terminal UI

Do not use viewer success as proof that room transport is healthy.
