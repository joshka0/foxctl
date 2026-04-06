## Phase 8 – Teams & Routing (Summary)

Phase 8 adds a **workspace-scoped teams store** and the minimum routing hooks
needed for overseer/agents/tools to address messages to `team:<slug>`
recipients. This enables team-aware coordination (help requests, review routing,
admin broadcasts) and unlocks future Viewer views (team boards, team inboxes).

Status in this repo:

- Teams store implemented in `internal/storage/teams`.
- Mailbox `team:<slug>` fanout implemented in `skills/mailbox`.
- Unit/integration tests added for both.

This phase should **not change Core Profile v1 envelope shape** or `meta.*`
contracts. New behavior is implemented via internal storage (SQLite) and
optional new skills that follow existing Protocol v1 envelope patterns.

---

## A/B/C/D Todo Structure – Sanity Check vs Phase 8 Spec

- **Section A – Data model & storage**
  - Implement a dedicated `teams` store (SQLite) and Go access layer.
  - Keep fields minimal but stable, aligned with `docs/spec/dspy_go_agents.md`
    §4.3 (team identity, members, optional tags/primary_epics).
  - **Codemaps most relevant**:
    - **CM13** – Core Profile v1: End-to-End Envelope, Jobs & CAS Flow (storage
      patterns + migrations).
    - **CM9** – Agentctl Overseer & Agent Hierarchy (team-aware
      routing/assignments).

- **Section B – Skills & APIs (`teams/manage.*`)**
  - Optionally expose a management surface to list/describe/upsert teams and
    manage membership.
  - Ensure all I/O is via Protocol v1 envelopes; no direct SQL exposure.
  - **Codemaps**:
    - **CM13** – envelope validation and skill execution patterns.

- **Section C – Routing, overseer, and viewer integration**
  - Add routing semantics for `team:<slug>` recipients and integrate with the
    existing mailbox/blackboard store.
  - Keep overseer integration minimal in v1: team-aware addressing and basic
    “who is in this team?” queries.
  - **Codemaps**:
    - **CM6** – dspy-go Agent Runtime & Tools Integration (agent config has
      `TeamID`).
    - **CM9** – Overseer planning and hierarchy.

- **Section D – Tests, golden fixtures, CI**
  - Unit tests for the teams store.
  - Integration tests for routing (send to team → members’ inbox).
  - Skill-level tests if `teams/manage.*` is implemented.

---

## Proposed PRs for Phase 8 – Teams & Routing

### PR 1 – Teams Store v1 (A1/A2)

Status: complete.

- **Scope**
  - Add a new storage package (recommended): `internal/storage/teams` with its
    own SQLite DB file under `cfg.Storage.Root` (e.g. `teams.db`).
  - Implement migrations and a Go access layer:
    - Types: `Team`, `TeamMember`.
    - CRUD helpers: `UpsertTeam`, `GetTeam`, `ListTeams`.
    - Membership helpers: `AddMember`, `RemoveMember`, `ListMembers`.
  - Data model (v1 recommendation):
    - `teams` table keyed by `(workspace_id, team_id)`.
    - `team_members` table keyed by `(workspace_id, team_id, actor_id)`.
    - Optional multi-valued fields (`primary_epics`, `tags`) stored as JSON with
      explicit conventions, or normalized tables if needed.

- **Constraints**
  - Workspace scoping is mandatory for all queries.
  - Idempotence:
    - Team upserts must be idempotent by `(workspace_id, team_id)`.
    - Member insert must be idempotent by `(workspace_id, team_id, actor_id)`.

- **Validation**
  - Unit tests for:
    - Migration idempotence.
    - CRUD and membership uniqueness.
    - Workspace boundary enforcement.

---

### PR 2 – Optional `teams/manage.*` Skills + CLI Wiring (B1/B2)

- **Scope**
  - Optionally add exec skills to manage teams via envelopes:
    - `teams/manage.list` – list teams.
    - `teams/manage.describe` – details + members.
    - `teams/manage.upsert` – create/update team fields.
    - `teams/manage.add_member` / `teams/manage.remove_member` – membership.
  - Optionally add `agentctl teams ...` CLI commands that call these skills.

- **Constraints**
  - All outputs are Protocol v1 envelopes.
  - Results should likely remain inline for v1; allow CAS only if lists can grow
    beyond inline limits.

- **Validation**
  - Skill tests that validate:
    - Envelope shape (`protocol.Validate`).
    - Workspace scoping.
    - Idempotent upserts.

---

### PR 3 – Mailbox Routing to `team:<slug>` (C1)

Status: complete.

- **Scope**
  - Implement routing for `team:<slug>` recipients using the new teams store.
  - Recommended v1 routing model: **fanout at send time**.
    - Rationale: current board/mailbox message tables store a single global
      `status` per message row; a shared team message would cause one member’s
      reads/acks to affect all members.
    - Fanout creates one message per member so read/ack state remains per-actor
      without schema changes.
  - Integrate into the existing coordination surface:
    - `skills/mailbox` uses `internal/storage/blackboard` (board messages).
    - On `mailbox/manage.send`, if `recipient` is `team:<slug>`:
      - Resolve members via `teams` store.
      - Insert one message per member with `recipient = <actor_id>`.

- **Output**
  - For team fanout sends, `mailbox/manage` returns:
    - `message_id` (first message id, for compatibility).
    - `message_ids` (all created message ids).
    - `delivered_count`.

- **Constraints**
  - Team membership is resolved at send time; membership changes do not
    retroactively change delivery.
  - No new envelope fields required.

- **Validation**
  - Integration tests:
    - Create team + members.
    - Send a message to `team:<slug>`.
    - Confirm each member’s inbox receives one message.
    - Confirm a non-member does not receive the message.

---

### PR 4 – Overseer / Agent Config Linkage (C2/C3) (Optional)

- **Scope**
  - Define how `AgentConfig.TeamID` should be validated/used:
    - When set, it should reference an existing team in the store.
  - Keep v1 overseer integration minimal:
    - Prefer team recipients for mailbox communications (help/review requests).
    - Defer task/epic assignment schema changes unless required.

- **Constraints**
  - Adding `team_id` fields to tasks/epics would require a storage migration and
    should be treated as a separate, explicitly reviewed change.

- **Validation**
  - Unit tests for team lookup/validation.

---

### PR 5 – Tests, Goldens, and CI Integration (D1–D4)

Status: partially complete.

- **Scope**
  - **Storage unit tests** for teams.
  - **Routing integration tests** for `team:<slug>` fanout.
  - **Skill tests** if `teams/manage.*` is implemented.
  - Optional golden fixtures if team management envelopes are expected to be
    stable across refactors.

- **Constraints**
  - Deterministic fixtures; no reliance on wall clock ordering.

---

## Validation Overview – How We Know Phase 8 Is "Done"

- **Teams store correctness**
  - Teams and members can be created, queried, and updated with strict workspace
    scoping and idempotence.

- **Routing correctness**
  - Sending to `team:<slug>` results in delivery to all current members.
  - Non-members do not receive team-addressed mail.

- **Tests & CI**
  - Unit + integration tests cover storage and routing, and pass under
    `make test`.

---

## Open Questions / To Discuss

- Should Phase 8 ship **storage-only** first, or include `teams/manage.*` skills
  and CLI in the same batch?
- Do we want a future **shared team inbox** (single message visible to all
  members) and if so, what schema changes are acceptable to support per-actor
  read/ack state?
- Should agents be allowed to mutate team membership via tools, or should that
  remain admin-only in v1?
