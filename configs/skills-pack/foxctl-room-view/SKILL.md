---
name: foxctl-room-view
description: "Viewer-layer guidance for room panes: tmux or zellij setup, PTY inspection, manual pane pokes, and transport-vs-viewer debugging."
---

# foxctl-room-view

Use this skill when you need the presentation layer for a room participant:

- create or inspect tmux/zellij panes
- read PTY output
- place or relabel viewer panes
- send a one-off manual poke into a pane
- debug viewer state separately from room transport

This skill is not the room source of truth.

## Mental model

- `foxctl` is the headless room/runtime kernel.
- `foxctl room` is canonical coordination and delivery.
- participant transport is canonical execution.
- room-scoped clients should consume `GET /api/rooms/{room-id}/events?workspace_id=...` for room timeline updates; the global `/api/events` feed is broader transport, not the room-specific contract.
- `delivery_binding` is the canonical room member routing record; mux metadata is a viewer/presentation layer and older top-level member transport fields are only mirrors.
- `last_delivery_trace` is the canonical explanation surface for the last delivery decision:
  when debugging “why did this message route here,” prefer room status / loop status over PTY guesswork.
- `tmux`, `zellij`, GUI PTY previews, and future xterm viewers are presentation attachments only.
- `foxctl mux` is for viewer setup, pane reads, and manual live interaction.

Hard boundary:

- mux is not the room core
- mux is not the delivery authority
- mux is not the source of room truth

## Default flow

When a room already exists:

```bash
foxctl room status <room-id>
foxctl mux list
foxctl mux read <target> --lines 80
```

Use `room status` to understand health. Use `mux list/read` to understand viewer placement and PTY state.

## What mux is for

- `foxctl mux create` for pane/session creation
- `foxctl room restore` when the real goal is to revive an existing room participant in one step
- `foxctl mux list` for viewer metadata
- `foxctl mux read` for PTY inspection
- `foxctl mux observe` when a live pane exchange should become durable ACA evidence
- `foxctl mux send` for a manual terminal poke outside the normal room transport path
- `foxctl room restore` should be preferred over manual `mux create` + `room rebind` when you are reviving an existing participant runtime

Launch-mode rule:

- `foxctl mux create --agent droid --mode auto` enables the Droid-specific startup profile used by the transport-first wrapper.
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
- If reminder behavior is surprising, remember that reminder-instance `ack` does not stop the schedule by itself; check loop state, linked work, and chain satisfaction before blaming the viewer layer.

## Regression Check

- When you need to confirm the hardened room-runtime invariants end to end, use `bash tests/regression/run.sh`.

## Compatibility

Older docs may still refer to:

- `tmux-bridge`
- `foxctl-tmux`

Treat those as compatibility names for this presentation-layer role. The architecture now belongs to `foxctl-room` + `foxctl-room-agent` + `foxctl-room-operator`.
