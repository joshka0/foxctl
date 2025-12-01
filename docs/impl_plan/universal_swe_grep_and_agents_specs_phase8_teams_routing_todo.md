# Phase 8 – Teams & Routing Todo Spec

This spec breaks down Phase 8 of `universal_swe_grep_and_agents` into concrete
steps focused on **teams, routing, and team-aware agents/skills**.

- Earlier phases establish tasks, overseer, dspy-go agents, and retrieval.
- Phase 8 adds a concrete `teams` store and optional skills so overseer, agents,
  and UIs can route work via `team:<slug>` and reason at team level.

> **Cross-refs**
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 8)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 8)
> - Specs:
>   - `docs/spec/dspy_go_agents.md` (§4.3 Teams and Assignments).
>   - `docs/spec/skills_spec/README.md` (§6 Teams & Routing future skills).
>   - `docs/spec/core_profile_v1.md`.
> - Codemaps for this phase (from codemap index):
>   - CM6 – Dspy-Go Agent Runtime & Tools Integration in agentctl.
>   - CM9 – Agentctl Overseer & Agent Hierarchy.
>   - CM10 – Knowledge System & Factory Droids.
>   - CM13 – Core Profile v1: End-to-End Envelope, Jobs & CAS Flow.

---

## A. Data Model & Storage

Goal: define a **stable, minimal data model** for teams and membership that
supports routing and dashboards, consistent with `dspy_go_agents.md` §4.3.

### A1. `teams` and `team_members` tables

- [ ] Implement SQLite tables for teams and memberships per `dspy_go_agents.md`:
  - `teams` (conceptual fields):
    - `team_id` (PK, string) – e.g. `team:backend`, unique per workspace.
    - `workspace_id` (string).
    - `name` (string).
    - `description` (string, optional).
    - `primary_epics` (optional, e.g. JSON array or separate join table).
    - `tags` (optional, e.g. JSON array or separate join table).
  - `team_members` (conceptual fields):
    - `team_id` (FK → teams).
    - `actor_id` (string) – `actor:agent:dspy:<slug>` or `actor:human:<id>`.
    - `role` (string) – `coder`, `reviewer`, `planner`, etc.
    - Optional metadata: skills, capacity, tags.
- [ ] Decide representation for multi-valued fields (`primary_epics`, `tags`):
  - Prefer normalized tables where possible; otherwise JSON with clear
    conventions.

### A2. Go types and access layer

- [ ] Add Go types mirroring the tables (e.g. `Team`, `TeamMember`) in a
  dedicated internal package (e.g. `internal/storage/teams`):
  - CRUD methods: `Create/UpsertTeam`, `GetTeam`, `ListTeams`,
    `AddMember`, `RemoveMember`, `ListMembers`.
  - Idempotent upserts for teams and members (no duplicate members per
    `(team_id, actor_id)`).
- [ ] Ensure the access layer:
  - Accepts `workspace_id` explicitly for all queries.
  - Plays nicely with existing SQLite helpers and migrations (CM13).

### A3. Agent config linkage

- [ ] Integrate `TeamID` in `AgentConfig` (already present) with the new teams
  store:
  - Decide how agents learn their team(s): config, skills, or overseer
    assignments.
  - Document invariants: e.g., `TeamID` MUST reference an existing team when set.

---

## B. Skills & APIs (`teams/manage.*`)

Goal: optionally ship **teams management skills** that wrap the teams store with
Protocol v1 envelopes, per `skills_spec/README.md` §6.

### B1. Skill manifest and contracts

- [ ] Optionally implement `teams/manage.*` skills as exec or WASI skills:
  - `teams/manage.list` – list teams for a workspace.
  - `teams/manage.describe` – show details and members for a team.
  - `teams/manage.upsert` – create or update a team.
  - `teams/manage.add_member` / `teams/manage.remove_member` – manage membership.
- [ ] For each skill, define input/output contracts consistent with
  `dspy_go_agents.md` §4.3 and `core_profile_v1`:
  - Inputs include `workspace_id`, `team_id`, and any necessary fields.
  - Outputs include basic team + member data; large results MAY use CAS
    artifacts (list responses), but v1 can likely remain inline.

### B2. CLI and tool integration

- [ ] Decide whether to expose teams management via:
  - CLI commands (`agentctl teams ...`) that call the skills.
  - dspy-go tools wrapping these skills for agents/admins.
- [ ] Ensure any new tools obey existing patterns:
  - Tools only; no direct SQL exposed to LLMs.
  - All I/O via Protocol v1 envelopes.

---

## C. Routing, Overseer, and Viewer Integration

Goal: make it possible to **route work to teams** and have the overseer/agents
understand team ownership, without changing the core wire contracts.

### C1. Mailbox routing to `team:<slug>`

- [ ] Implement or refine mailbox support for `team:<slug>` recipients:
  - When a message is addressed to `team:<slug>`, fan out to current members or
    surface in a shared team inbox, per `dspy_go_agents.md` §4.3.
  - Ensure routing is backed by the teams store, not config-only.
- [ ] Update or add integration tests (per Phase 8 testing plan):
  - `team:<slug>` recipients correctly resolve to memberships.
  - Viewer-level queries can list mail/messages per team.

### C2. Overseer planning and assignments

- [ ] Integrate teams into overseer planning where helpful (CM9):
  - Allow epics/tasks to be assigned to `team_id` rather than just actors.
  - Keep this as metadata; actual spawn/assignment behavior can be simple in v1.
- [ ] Document how Planner agents should use team information (per
  `dspy_go_agents.md` §4.3.2):
  - Group tasks per team.
  - Prefer routing review/help requests to `team:<slug>` instead of individuals.

### C3. Viewer & knowledge integration

- [ ] Ensure team-level data is consumable by future Viewer UIs:
  - Team boards (epics/tasks, active agents, unread messages).
  - Simple queries over `teams` / `team_members` with workspace filters.
- [ ] Optionally seed or update knowledge entries (CM10) that describe team
  concepts and recommended usage patterns for humans.

---

## D. Tests, Golden Fixtures, and CI

Goal: validate **team storage, skills (if implemented), and routing** via unit,
integration, and possibly golden tests.

### D1. Unit tests (storage)

- [ ] Add unit tests over the teams storage layer:
  - CRUD over `teams` and `team_members`.
  - Idempotent upserts and membership updates.
  - Workspace scoping and basic constraints (no duplicate members).

### D2. Skill tests (`teams/manage.*`)

- [ ] If `teams/manage.*` skills are implemented, add skill-level tests per test
  plan:
  - `teams/manage.list`, `.describe`, `.upsert`, `.add_member`, `.remove_member`.
  - Ensure skills respect workspace boundaries and emit valid envelopes.

### D3. Integration tests (routing & viewer-level queries)

- [ ] Add integration tests that:
  - Create teams and members in the store.
  - Send/route messages to `team:<slug>`.
  - Query from a viewer perspective to join tasks/agents with teams.
- [ ] Ensure these tests tie back to overseer + runtime where necessary (CM6,
  CM9), without expanding scope beyond basic routing.

### D4. CI & test infra

- [ ] Wire new tests into existing CI targets (CM14):
  - Ensure they run in standard `go test ./...` and/or `make` targets.
  - Keep fixtures small and deterministic.

---

## Open Questions / To Discuss

- How much of the teams functionality should Phase 8 implement vs defer (e.g.,
  storage only vs full `teams/manage.*` skills + CLI/tools)?
- What is the minimal, stable set of team fields needed for v1, given future
  Viewer and routing plans?
- Should agents be able to mutate team membership directly (via tools), or
  should that remain an admin-only path in v1?
