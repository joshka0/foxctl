# Mailbox / Blackboard Spec (Draft)

## Overview

Local, SQLite-backed actor-style mailbox / blackboard for agents and humans,
including admin / overseer roles and file-guard semantics. This is **not**
email; it is an in-repo coordination layer that:

- Provides per-actor inboxes and streams of messages.
- Allows an admin (human) and overseer (system) to direct and prioritize work.
- Exposes advisory file reservations to reduce edit conflicts.
- Integrates with hooks so new messages and reservations show up in context.

## Goals

- Give every actor (agent or human) a simple, queryable mailbox within a
  workspace.
- Let an admin send high-priority instructions to specific actors or broadcast
  to all.
- Let an overseer agent coordinate work based on task graph insights and mailbox
  state.
- Provide advisory file reservations that can be enforced via `task_guard` /
  `file_guard`-style hooks.
- Keep storage and APIs local to `agentctl` (SQLite + JSON envelopes), with
  optional bridges to external systems later.

## Non-Goals

- Implement SMTP, IMAP, or any external email protocol.
- Require Git, MCP servers, or remote services for core functionality.
- Replace Beads or mcp_agent_mail; this spec borrows their concepts but keeps
  `agentctl` storage independent.
- Define final CLI UX in detail; this spec focuses on the underlying primitives
  and hook behaviors.

## Roles & Actors

Mailbox identities are opaque strings but should be stable and
human-inspectable. Recommended conventions (non-normative):

- **Normal actor**
  - Example IDs: `actor:agent:<slug>`, `actor:human:<name>`.
  - Reads and writes to its own inbox.
  - Typically associated with one or more tasks in a workspace.

- **Admin** (privileged human, e.g. you)
  - Example ID: `actor:admin:<name>` or simply `admin`.
  - Can send messages to any specific actor or broadcast to all.
  - Can set high priority and `ack_required` flags on messages.
  - Semantically, admin messages are instructions or decisions that other actors
    should strongly respect.

- **Overseer** (coordinator agent)
  - Example ID: `actor:system:overseer`.
  - System-level actor that reads task graph insights + mailbox stats and sends
    advisory messages to other actors.
  - May be implemented as a dedicated agent that periodically runs skills like
    `todo/manage.graph_insights` and `mailbox/stats`.

- **Agent Viewer / UI**
  - Not a mailbox actor itself; uses APIs to **read** graph and mailbox state.
  - Provides dashboards (per-actor inboxes, task boards) and lets the admin or
    overseer compose messages.
  - Should expose a "robot protocol" JSON mode so agents could also consume the
    same views if needed.

## Data Model

Logical data shapes, not final SQL schemas. All records are scoped by
`workspace_id`.

### Messages

Each mailbox message represents one logical communication between actors.

- `id` (string) – Unique message identifier (e.g. ULID).
- `workspace_id` (string) – Owning workspace.
- `task_id` (string, optional) – Task this message is primarily about.
- `stream` (string) – Logical channel, e.g. `coordination`, `alerts`, `system`,
  `review`.
- `sender` (string) – Actor ID that sent the message (`admin`,
  `actor:agent:<slug>`, `actor:system:overseer`, etc.).
- `recipient` (string) – Target actor ID, or a broadcast token such as `*`.
- `kind` (string) – High-level type: `instruction`, `info`, `alert`,
  `review_request`, etc.
- `priority` (int) – 1 (highest) .. 5 (lowest).
- `ack_required` (bool) – Whether recipient must explicitly acknowledge.
- `status` (string) – `unread`, `read`, `acked` (per recipient semantics).
- `subject` (string) – Short human-readable summary.
- `body` (string) – Freeform content (Markdown or JSON-encoded details).
- `created_at` (timestamp).

### File Reservations

File reservations are advisory locks over paths within a workspace.

- `id` (string) – Unique reservation ID.
- `workspace_id` (string).
- `path` (string) – File or directory path relative to workspace root.
- `holder` (string) – Actor ID that currently holds the reservation.
- `mode` (string) – `exclusive` or `shared`.
- `expires_at` (timestamp) – When reservation should be considered invalid.
- `created_at` (timestamp).

Reservations are advisory only. Hooks decide how strictly to enforce them when
tools attempt writes.

## Operations / APIs

Skill-like operations for interacting with the mailbox and reservations. All I/O
uses the standard JSON envelope contract.

### `mailbox/send`

Create a new message.

- **Inputs** (data fields):
  - `workspace_id` (required).
  - `sender` (required; must be a valid actor ID).
  - `recipient` (required; actor ID or broadcast token).
  - `subject`, `body` (required).
  - `task_id` (optional).
  - `stream`, `kind` (optional, default `coordination` / `info`).
  - `priority` (optional, default 3).
  - `ack_required` (optional, default false).
- **Outputs**:
  - Created message metadata (id, timestamps).

### `mailbox/inbox`

Read messages for an actor.

- **Inputs**:
  - `workspace_id`.
  - `actor_id` (recipient).
  - Optional filters: `task_id`, `stream`, `only_unread`, `since`.
- **Outputs**:
  - List of messages sorted primarily by `priority` (1 first) then `created_at`
    (newest first).

### `mailbox/ack`

Mark messages as acknowledged.

- **Inputs**:
  - `workspace_id`.
  - `actor_id`.
  - `message_ids` (list).
- **Outputs**:
  - Count of messages updated and their final statuses.

### `mailbox/reserve`

Acquire one or more file reservations.

- **Inputs**:
  - `workspace_id`.
  - `actor_id` (holder).
  - `paths` (list of relative paths).
  - `mode` (`exclusive` or `shared`).
  - `ttl_seconds` (optional; default reasonable value).
- **Outputs**:
  - Reservations granted (ids, paths, mode, expires_at).
  - Conflicts (paths and existing holders) if any.

### `mailbox/release`

Release reservations.

- **Inputs**:
  - `workspace_id`.
  - Either `reservation_ids` or `paths` + `actor_id`.
- **Outputs**:
  - Count of reservations released.

## Hook Integration

Hooks consume mailbox and reservation state to influence context and, in strict
modes, decisions.

### Mail-aware hook (`hooks/mail_router`)

Intended to run after `task_guard` (so `task_id` is known) and before
`knowledge_router`.

- On `PreToolUse` and/or `UserPromptSubmit`:
  - Resolve `workspace_id`, `actor_id`, and active `task_id`.
  - Call `mailbox/inbox` with filters such as:
    - `actor_id = current actor`.
    - `task_id = active task` (plus possibly unscoped messages).
    - `only_unread = true`.
  - Rank messages by:
    - `priority`.
    - Sender weight (`admin` / `overseer` > others).
    - Recency.
  - Attach a compact summary of the top N messages to `hook.Output.Context`,
    clearly labelling admin and overseer messages.
  - Optionally call `mailbox/ack` for messages once surfaced, depending on
    configuration.

By default this hook is **advisory**: it does not block tool use, but brings
relevant mail into the model’s context.

### File guard hook (`hooks/file_guard` or extended `task_guard`)

Before tools that write to the filesystem run, a guard hook can:

- Determine the set of paths that will be written.
- Call `mailbox/reserve` for those paths on behalf of the current actor.
- If conflicts are reported:
  - **Strict mode**: emit `Decision:block` with a reason indicating the
    conflicting holder and path.
  - **Advisory mode**: allow the write but add strong warnings into
    `hook.Output.Context` and possibly send a notification message to the
    conflicting actor.

## Prioritization & Overseer Strategy

The overseer and admin rely on a combination of task graph metrics and mailbox
state to prioritize work.

### Scoring for tasks

Each task can be given an overall score such as:

```text
task_score =
    α * normalized(critical_path_score or pagerank)
  + β * unread_admin_weight
  + γ * unread_overseer_weight
  + δ * recency_of_last_update
```

Where the unread weights derive from counts of admin/overseer messages attached
to that task.

### Scoring for messages

Within an inbox, messages can be ranked by:

- `priority` (1..5, mapped to a numeric weight).
- Sender (`admin` / `overseer` boosted).
- Recency.
- Whether `ack_required` is true.

Admin and overseer tools may surface or auto-generate messages for tasks whose
`task_score` crosses certain thresholds (e.g. high impact but stale, high impact
with no owner, etc.).

## Agent Viewer / UI

The agent viewer is any TUI or web UI that consumes the mailbox and task graph
APIs to provide a human- and agent-friendly control panel.

Expected capabilities:

- **Per-actor inboxes**
  - For each actor, show unread / total messages, highlighting admin and
    overseer messages and outstanding `ack_required` items.

- **Task-centric view**
  - For each high-impact task (based on task graph insights), show related
    messages, owners (if tracked), and reservations.

- **Admin / overseer console**
  - Compose and send messages as `admin` or `overseer` to specific actors or
    broadcast.
  - View reservation conflicts and nudge actors via messages.

- **Robot protocol**
  - Expose a JSON summary endpoint (or envelope-based skill) that returns the
    same aggregated data used by the UI so that agents can programmatically
    query "what should I work on next?" or "do I have unread admin instructions
    for this task?".

Implementation details of the viewer (CLI vs TUI vs web) are out of scope for
this spec; only the expectations of the underlying APIs and behaviors are
normative.
