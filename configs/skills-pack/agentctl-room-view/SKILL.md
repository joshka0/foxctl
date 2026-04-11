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
- `agentctl room restore` when the real goal is to revive an existing room participant in one step
- `agentctl mux list` for viewer metadata
- `agentctl mux read` for PTY inspection
- `agentctl mux observe` when a live pane exchange should become durable ACA evidence
- `agentctl mux send` for a manual terminal poke outside the normal room transport path
- `agentctl room restore` should be preferred over manual `mux create` + `room rebind` when you are reviving an existing participant runtime

Launch-mode rule:

- `agentctl mux create --agent droid --mode auto` enables the Droid-specific startup profile used by the transport-first wrapper.
- That profile waits for the Droid UI to reach its `Auto (Off)` state and then sends `Ctrl+L` three times to clear/advance the runtime into its intended high-autonomy startup path.
- `--mode interactive` does **not** use that startup profile.
- So if someone launches Droid with `--mode interactive`, they have explicitly bypassed the Droid auto-start behavior and may still see approval-gated runtime behavior.
- Treat that as a launch/runtime choice, not a room-delivery failure.

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
