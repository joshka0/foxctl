---
name: agentctl-room-view
description: "Presentation-layer guidance for room-attached panes: tmux/zellij viewer setup, PTY inspection, manual pane pokes, and viewer-vs-transport debugging."
---

# agentctl-room-view

Use this skill when you need the presentation layer for a room participant:

- create or inspect tmux/zellij panes
- read PTY output
- place or relabel viewer panes
- send a one-off manual poke into a pane
- debug viewer state separately from room transport

This skill is not the room source of truth.

## Mental model

- `agentctl room` is canonical coordination and delivery.
- participant transport is canonical execution.
- `tmux`, `zellij`, GUI PTY previews, and future xterm viewers are presentation attachments.
- `agentctl mux` is for viewer setup, pane reads, and manual live interaction.

## Default flow

When a room already exists:

```bash
agentctl room status <room-id>
agentctl mux list
agentctl mux read <target> --lines 80
```

Use `room status` to understand health. Use `mux list/read` to understand viewer placement and PTY state.

## What mux is for

- `agentctl mux create` for pane/session creation
- `agentctl mux list` for viewer metadata
- `agentctl mux read` for PTY inspection
- `agentctl mux observe` when a live pane exchange should become durable ACA evidence
- `agentctl mux send` for a manual terminal poke outside the normal room transport path

## What mux is not for

- canonical room history
- task state
- reminder execution
- proof of participant membership
- proof of participant transport health

## Viewer debugging rules

- If pane output looks wrong but `room status` shows healthy transport/runtime, debug the viewer layer.
- If the pane looks fine but room delivery fails, debug participant transport and membership before touching tmux/zellij.
- A missing PTY is a presentation issue until room transport also fails.

## Compatibility

Older docs may still refer to:

- `tmux-bridge`
- `agentctl-tmux`

Treat those as compatibility names for this presentation-layer role. The architecture now belongs to `agentctl-room` + `agentctl-room-agent` + `agentctl-room-operator`.
