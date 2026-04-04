# Agent Mux + Room Hierarchy

Status: partial implementation; launcher identity + parent-private defaults landed

This design defines how terminal panes *(in practical terms: tmux panes or zellij panes)*,
agent hierarchy, room membership, and sender identity should work together.

The goal is to make multi-agent collaboration visible and durable without letting
low-level worker chatter flood shared rooms.

## Core policy

The policy is:

- top-level agents may join shared rooms
- subagents do not join rooms by default
- subagents talk only to their parent
- parents decide what to forward into the room
- sender identity is derived from the mux binding, not guessed by the agent

In short:

- room = principal coordination plane
- parent/child channel = private execution plane
- tmux/zellij pane = live terminal tenancy for one agent

## Why this is the right boundary

If subagents speak directly into the room:

- the room fills with low-level execution noise
- lateral coordination becomes ambiguous
- sender identity becomes easier to spoof or hallucinate
- it becomes hard to see which messages are authoritative

If only parent agents can publish to the room:

- room traffic stays high-signal
- child execution remains private and local
- the parent becomes the summarizer/filter
- task broadcasts remain understandable

This matches the overseer model already used elsewhere in `agentctl`: authority
and coordination should stay explicit.

## Mental model

There are three distinct layers:

### 1. Mux layer

This is the live terminal substrate:

- `tmux`
- `zellij`

Responsibilities:

- allocate panes
- label/rename panes
- inject text into panes
- capture pane content if needed

### 2. Agent hierarchy layer

This is the runtime ownership model:

- root agent
- child agent
- descendant agent

Responsibilities:

- parent/child ownership
- spawn policy
- private parent-child communication
- pane ownership per agent

### 3. Room layer

This is the durable shared coordination surface:

- room log
- room tasks
- relay / loop

Responsibilities:

- high-signal cross-agent communication
- shared task announcements
- operator-visible durable history

## Topology

The default topology should be:

- one mux session per workstream
- one pane per agent
- one root room per workstream
- one top-level participant id per top-level agent
- subagent panes nested under the same session/tab tree

Recommended rendering:

### tmux

- one session per workstream
- one window per top-level agent group or workstream phase
- panes for agents within that window

### zellij

- one session per workstream
- one tab per top-level agent or phase
- panes/stacks for child agents inside the tab

This avoids “every agent gets its own tmux instance”, which creates transport
fragmentation and weakens identity/routing.

## New runtime concepts

### `MuxBackend`

Abstract terminal multiplexer operations behind one interface:

- `PrepareSession`
- `EnsureContainer`
- `CreatePane`
- `RenamePane`
- `ResolvePane`
- `Send`
- `Read`
- `ClosePane`

Backends:

- `tmux`
- `zellij`

### `AgentTerminalBinding`

Persist a binding per agent:

- `agent_id`
- `parent_agent_id`
- `backend`
- `session`
- `window_or_tab`
- `pane_id`
- `participant_id`
- `room_id`
- `visibility`

Where:

- `participant_id` is the durable sender id
- `visibility` is `room` or `parent_private`

### `RoomMembershipPolicy`

Policies:

- `top_level_only` *(default)*
- `inherited`
- `none`
- `direct`

For this design:

- root agents: `direct`
- subagents: `none` by default

## Sender identity

Sender identity should not come from agent prose or hand-entered flags unless
explicitly overridden.

Resolution order should be:

1. explicit CLI flag
2. explicit environment variable
3. current mux binding
4. stored workspace participant identity
5. error

### New env vars

- `AGENTCTL_AGENT_ID`
- `AGENTCTL_PARENT_AGENT_ID`
- `AGENTCTL_PARTICIPANT_ID`
- `AGENTCTL_PARENT_PARTICIPANT_ID`
- `AGENTCTL_ROOM_ID`
- `AGENTCTL_MUX_BACKEND`
- `AGENTCTL_MUX_SESSION`
- `AGENTCTL_MUX_PANE_ID`

### Mux-derived defaults

For live pane usage:

- in `tmux`, default sender resolution should use pane label first
- in `zellij`, default sender resolution should use pane title first

That means the agent does not need to remember “I am `codex-a`”.
The runtime already knows.

## Parent-child private channel

Subagents need a private communication path that does not go through the room.

This should be modeled as a parent-scoped channel:

- `parent_agent_id`
- `child_agent_id`
- durable event stream or mailbox topic
- optional live pane relay between parent and child

Suggested shape:

- durable channel name: `agent-child:<parent_agent_id>`
- optional live fanout into the parent pane only

Allowed traffic:

- child -> parent progress
- child -> parent blocker/question
- parent -> child delegation/update/stop

Disallowed by default:

- child -> room
- child -> sibling

## Room publication policy

Only top-level agents publish directly to the room by default.

Subagent results reach the room only when the parent chooses to promote them.

Examples:

### allowed

- parent publishes a summary of child findings
- parent marks a room task complete after child finishes
- parent asks the room for help based on child blocker

### not allowed by default

- child posts raw execution logs into room
- child asks sibling agents directly through room
- child joins room automatically just because it exists

## Spawn behavior

### Top-level agent spawn

Top-level agents may:

- allocate their own pane
- join a room
- get a durable `participant_id`

### Subagent spawn

Subagents should:

- allocate a pane in the same mux session
- inherit `backend` and `session`
- get a child-local `participant_id`
- not join the room by default
- attach to their parent-private channel

## Proposed CLI flags

### root / explicit spawn

- `--mux tmux|zellij`
- `--mux-session <name>`
- `--room <room-id>`
- `--participant-id <id>`

### child spawn

- `--parent-agent-id <id>`
- `--parent-participant <id>`
- `--mux-parent-pane <id>`
- `--room-access none|direct|inherited`
- `--spawn-in-pane`

Recommended default:

- root spawn: `room-access=direct`
- child spawn: `room-access=none`

## Room loop behavior

`room loop` should remain room-scoped, not child-scoped.

It should:

- relay room messages to room participants
- broadcast room task changes
- ignore child-private traffic

This preserves the separation:

- room loop for shared coordination
- parent-child channel for execution chatter

## Suggested storage additions

### New table or store: `agent_terminal_bindings`

Fields:

- `agent_id`
- `parent_agent_id`
- `workspace_id`
- `backend`
- `session`
- `window_or_tab`
- `pane_id`
- `participant_id`
- `room_id`
- `room_access`
- `created_at`
- `updated_at`

### Optional new stream family

- `room:<room_id>`
- `agent-child:<parent_agent_id>`

This keeps shared coordination and private execution separate at the storage
layer, not just at the UX layer.

## Acceptance criteria

### Identity

- room commands can infer sender from mux binding
- top-level agents do not need to invent sender ids
- sender identity is stable across turns in the same pane

### Hierarchy

- subagents spawn into panes under the same mux session
- subagents can message parent privately
- subagents do not appear in room membership unless explicitly allowed

### Room hygiene

- rooms show top-level collaboration only
- room task broadcasts stay visible and useful
- child execution chatter does not flood the room

### Backend parity

- `tmux` path supports full hierarchy + room flow
- `zellij` path supports the same policy with plugin-backed delivery

## Current state

Implemented in the launcher and room/tmux surfaces:

1. room sender identity resolves from tmux/zellij pane bindings and canonical pane ids
2. `agentctl tmux create` can inject child-pane hierarchy env into launched panes
3. child-pane default policy is parent-private when `--parent-participant` is set
4. `agentctl tmux send-parent` gives child panes a direct parent path without joining rooms
5. `agentctl agent spawn` / daemon spawn / persisted agent metadata now carry a typed `terminal_binding` object
6. overseer child sessions inherit backend/session/parent-private defaults from the parent session binding
7. `agentctl agent spawn --spawn-in-pane --mux-backend tmux` can allocate an exact tmux pane, derive a canonical participant id from that pane, and repurpose it into `agentctl agent watch <agent-id>`

Still pending:

1. spawn-time pane allocation for real runtime subagent spawn flows
2. a durable parent-private child channel beyond the pane-message helper
3. explicit enforcement of top-level-only room membership in runtime spawn policy
4. zellij parity checks for child-pane tenancy

## Related

- [tmux-collaboration.md](../../general/tmux-collaboration.md)
- [agent_hierarchy.md](../../spec/agent_hierarchy.md)
- [overseer_profile.md](../../spec/overseer_profile.md)
- [agentctl-room](../../../configs/skills-pack/agentctl-room/SKILL.md)
