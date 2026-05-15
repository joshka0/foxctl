# Pi Room-Agile Epic Integration

| Field | Value |
|-------|-------|
| Status | Draft |
| Owner | foxctl |
| Scope | `integrations/pi`, room-agile epic system, Pi extension/TUI surfaces |
| Related | `configs/skills-pack/foxctl-room-agile/SKILL.md`, `integrations/pi/foxctl.ts`, `integrations/pi/README.md` |

## Goal

Make Pi a first-class room-agile participant for foxctl epics. A Pi session
should be able to discover the active epic, see the current milestone/story
frontier, update story state, attach validation, and receive the right context
before each agent turn without reconstructing state from room chat.

## Pi Capabilities to Use

Pi extensions are TypeScript modules loaded from `.pi/extensions` or
`~/.pi/agent/extensions`. They can:

- register custom tools with `pi.registerTool()`
- register slash commands with `pi.registerCommand()`
- inject hidden context during `before_agent_start`
- intercept or transform user input
- persist extension state with `pi.appendEntry()`
- render TUI status, widgets, dialogs, and custom components through `ctx.ui`

Those capabilities map well to foxctl room-agile:

- tools for machine-callable epic/milestone/story actions
- commands for human TUI workflow shortcuts
- hidden context injection for active epic state
- widgets/status for current milestone and next story
- persistent Pi-side selection for active room/epic/milestone/story

## Existing foxctl Surfaces

The current Pi bridge at `integrations/pi/foxctl.ts` already exposes broad
foxctl daemon functionality:

- room membership, inbox, status, tasks, messages, loop status
- task create/action/ack/resolve
- context overview and control proposals
- repoindex, semantic search, memory, session recall, refactor scout
- optional `before_agent_start` foxctl context injection

The missing piece is first-class room-agile command coverage.

foxctl already has the complete room-agile command model in the CLI and MCP
tool surface:

- `epic_start`, `epic_show`, `epic_resume`, `epic_health`, `epic_next`,
  `epic_checkpoint`, `epic_grade`, `epic_shape`
- `milestone_start`, `milestone_contract`, `milestone_criteria`,
  `milestone_review`, `milestone_summary`, `milestone_show`
- `story_propose`, `story_accept`, `story_add`, `story_state`,
  `story_validate`, `story_show`
- `log_append`, `log_show`, `retro_add`, `retro_show`
- `contextwiki_promote`, `workpack_show`, `workpack_sync`

## Critical Design Choice

Do not make shell execution the primary Pi integration path.

Shelling out from the extension would be quick, but it would create a second
transport path with different errors, process lifetime, and configuration. The
deep integration should use one canonical backend surface that Pi, MCP, GUI, and
future integrations can share.

Preferred path:

1. Add a daemon HTTP endpoint for room-agile actions that mirrors the existing
   MCP `room_agile` action schema.
2. Use that endpoint from `integrations/pi/foxctl.ts`.
3. Keep the existing MCP implementation as a sibling adapter over the same
   action contract.

Acceptable temporary path:

1. Add a generic Pi `foxctl_room_agile` tool that calls a daemon endpoint when
   available.
2. If the daemon endpoint is unavailable, fail with a clear message and show the
   equivalent CLI command. Do not silently shell out.

## Proposed Contract

Add a daemon endpoint:

```http
POST /api/rooms/{room_id}/agile
```

Request:

```json
{
  "workspace": ".",
  "sender": "actor:pi:local",
  "actor": "actor:pi:local",
  "action": "epic_next",
  "epic_id": "msg-...",
  "limit": 100
}
```

Response:

```json
{
  "action": "epic_next",
  "room_id": "alpha",
  "workspace": "/abs/workspace",
  "result": {
    "version": 1,
    "status": "ok",
    "command": "foxctl.room.epic.next",
    "data": {}
  }
}
```

Rules:

- The HTTP endpoint must preserve the CLI envelope in `result`.
- The request schema should match the MCP `room_agile` action keys where
  possible.
- The endpoint should reject unsupported actions explicitly.
- Mutating actions must still use the existing coordinator/role checks.
- Read actions should be safe for Pi participants.

## Pi Extension Additions

### Flags

Add:

- `--foxctl-epic`: active epic id for context and commands
- `--foxctl-milestone`: active milestone id
- `--foxctl-story`: active story id
- `--foxctl-epic-context`: inject epic resume/health/next before agent turns
  when `--foxctl-context` is enabled

### Tools

Add one generic tool first:

- `foxctl_room_agile`

Parameters should mirror the backend action schema:

- `action`
- `room_id`
- `workspace`
- `sender`
- `actor`
- `epic_id`
- `milestone_id`
- `story_id`
- `proposal_id`
- `story_proposal_id`
- `title`
- `goal`
- `notes`
- `verdict`
- `validator_type`
- `validation_status`
- action-specific arrays such as `scope`, `success`, `required_lane`,
  `optional_lane`, `completed`, `next`, `related_story_ids`

Then add focused aliases only where they improve model behavior:

- `foxctl_epic_status`: `epic_resume` + `epic_health` + `epic_next`
- `foxctl_story_start`: `story_state in_progress`
- `foxctl_story_review`: `story_state in_review`
- `foxctl_story_validate`: `story_validate`

### Commands

Add human-facing commands:

- `/epic` - show active epic status/resume/next
- `/epic-next` - show next actions for active actor
- `/epic-health` - show health warnings
- `/milestones` - list milestones for active room/epic
- `/stories` - list stories for active room/milestone
- `/story-start <story-id>` - mark story in progress
- `/story-review <story-id>` - mark story in review
- `/story-validate <story-id>` - guided validation prompt
- `/workpack` - show work-pack mirror status

Commands should use `ctx.ui.notify()` for the first slice. A richer custom TUI
widget can come after the data contract is stable.

### Context Injection

Extend the existing `before_agent_start` hook:

- when `--foxctl-context` and `--foxctl-epic-context` are true
- if `--foxctl-room` and `--foxctl-epic` are set
- inject:
  - `epic_resume`
  - `epic_health`
  - `epic_next`
  - current milestone/story summaries when set

This hidden context should be concise. It should not paste full work-pack files
unless explicitly requested.

### TUI Status

Extend `session_start` UI status:

- current room
- active epic short id/title/status
- current milestone/story if set

Later, add a small widget above or below the editor:

```text
Epic: Pi integration     Status: finalized     Health: needs_attention
Milestone: Backend API   Next: validate story pi-agile-2
```

## Milestones

### M1: Backend Action Endpoint

Deliver:

- daemon HTTP endpoint for room-agile action dispatch
- shared request type that stays close to MCP `room_agile`
- tests for read actions:
  - `epic_show`
  - `epic_resume`
  - `epic_health`
  - `epic_next`
  - `milestone_show`
  - `story_show`
- tests for mutating actions:
  - `story_state`
  - `story_validate`

Acceptance:

- HTTP endpoint returns the same envelope shape as CLI/MCP-backed room-agile.
- unsupported actions fail with a typed error.
- coordinator restrictions still apply to scope-changing actions.

### M2: Pi Generic Tool and Commands

Deliver:

- `foxctl_room_agile` tool in `integrations/pi/foxctl.ts`
- `/epic`, `/epic-next`, `/epic-health`, `/milestones`, `/stories`
- README updates for flags and commands

Acceptance:

- Pi can show active epic state without shelling out.
- Pi can list milestone/story state from the configured room.
- Tool output includes structured `details` for model use and readable text for
  humans.

### M3: Active Epic Context

Deliver:

- `--foxctl-epic`, `--foxctl-milestone`, `--foxctl-story`,
  `--foxctl-epic-context`
- hidden context injection for resume/health/next
- concise status rendering in Pi footer

Acceptance:

- starting a Pi turn with active epic context gives the model the next work item
  and current health warnings.
- context injection remains bounded and does not flood the prompt.

### M4: Story Lifecycle from Pi

Deliver:

- focused tools/commands for story state and validation
- guided validation command using Pi UI prompts
- optional work-pack sync/show command

Acceptance:

- Pi can move a story through `in_progress`, `in_review`, and validated states.
- validation records include command/artifact fields when supplied.
- work-pack mirror updates after state/validation mutations.

### M5: Rich TUI Panel

Deliver:

- custom TUI component for epic/milestone/story status
- interactive selection for active epic/milestone/story
- keyboard shortcuts for next/story validation flow

Acceptance:

- human can select and inspect active epic state inside Pi without typing ids.
- model still receives structured tools; the TUI is convenience, not the source
  of truth.

## Risks

- Duplicating CLI/MCP logic in HTTP handlers would drift. The endpoint should
  share the action contract rather than reimplement semantics.
- A generic action tool may be too broad for the LLM. Start generic, then add
  focused aliases for high-frequency actions.
- Context injection can become noisy. Keep `epic_resume`, `epic_health`, and
  `epic_next` summarized and bounded.
- Pi-side active ids can go stale. Commands should fail clearly and recommend
  `/epic` or `/milestones` refresh.

## First Implementation Slice

Implement M1 and M2 only:

1. Backend: `POST /api/rooms/{room_id}/agile`.
2. Pi: `foxctl_room_agile` plus read-only commands `/epic`, `/epic-next`,
   `/epic-health`, `/milestones`, and `/stories`.
3. Docs: update `integrations/pi/README.md` with the new endpoint dependency,
   flags, tool, and commands.

Defer mutating story lifecycle shortcuts until the read path is stable in Pi.
