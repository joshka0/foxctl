Got it—you want an explicit “lookup table” so when you point at a task in the
plan you immediately know which codemap(s) to consult.

Below I:

1. Define short IDs for each codemap.
2. Map **impl plan tasks** (especially Phases 0–3 + phase‑level for 4–9) to
   those IDs.

You can copy/paste this into a separate doc if you want to keep it nearby.

---

## 1) Codemap Index (IDs → Names)

- **CM1** – _agentctl Skill System: Build → Discovery → Execution → Runtime_
- **CM2** – _agentctl Skill System: Discovery, Validation, and Execution_
- **CM3** – _agentctl Envelope Protocol & CLI Pipeline_
- **CM4** – _agentctl Skill System: Installation, Execution, Jobs, CAS, and
  Plugin Architecture_
- **CM5** – _CAS Storage & Integrity Verification in agentctl_
- **CM6** – _Dspy-Go Agent Runtime & Tools Integration in agentctl_
- **CM7** – _agentctl Job System: Types, Storage, and WFQ Scheduler_
- **CM8** – _agentctl Job System: Submission, SQLite Storage, State Transitions,
  and WFQ Scheduler_
- **CM9** – _Agentctl Overseer & Agent Hierarchy: Spawn Protocol, Depth Limits,
  and Session Management_
- **CM10** – _agentctl Knowledge System & Factory Droids_
- **CM11** – _agentctl Execution Runners: WASI & Exec with Network/FS Policy
  Enforcement_
- **CM12** – _agentctl CAS: Put, Get, Integrity, Deduplication & Integration_
- **CM13** – _agentctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow_
- **CM14** – _agentctl Test Infrastructure: Test-Watch, Feedback Hooks, and CI
  Targets_
- **CM15** – _OpenAPI Skill & Plugin Protocol: Request Flow and Plugin
  Integration_
- **CM16** – _agentctl Planning LLM Stack: Auto, Providers, and Integration
  Tests_
- **CM17** – _Test Infrastructure & CI Pipeline: Test Watcher, Makefile Targets,
  and Coverage Thresholds_

You can refer to these as “CM3” etc. in comments, issues, or docs.

---

## 2) Impl Plan Tasks → Codemaps

### Phase 0 – Pre-flight & Guardrails

- **Task:** Verify current state of: Jobs + CAS integration\
  **Codemaps:** CM1, CM2, CM4, CM7, CM8, CM5, CM12, CM13

- **Task:** Verify current state of: `task_guard`, `file_guard`,
  `test_watch_feedback` hooks\
  **Codemaps:** CM1, CM2, CM4, CM11, CM14

- **Task:** Verify current state of: Named memory + optional vector support\
  **Codemaps:** CM10, CM13

- **Task:** Add/confirm CI checks for lint, vet, `go test ./...`, `-race`\
  **Codemaps:** CM14, CM17

- **Task:** Enforce “no new CGO deps” on new code\
  **Codemaps:** CM13, CM17

- **Task:** Document in `README` / `ARCHITECTURE` where post-review and indexing
  fit\
  **Codemaps:** CM3, CM13, CM9, CM10

---

### Phase 1 – Review Gate v1 (Kernel-Owned Dirtying)

- **Task:** Extend `todo/manage` operations (`review_request`, `complete`,
  `review_status`)\
  **Codemaps:** CM3, CM7, CM8, CM13

- **Task:** Implement review artifact storage + CAS logs\
  **Codemaps:** CM5, CM12, CM13

- **Task:** Implement `hooks/task_guard` dirtying behavior\
  **Codemaps:** CM1, CM2, CM4, CM7, CM8, CM11, CM14

- **Task:** Wire minimal static pipeline (lint, fast tests, CI-style checks;
  plug into
  [tests.run](cci:1://file:///Users/jkatigbak/repos/personal/agentctl/skills/hooks_task_guard/main.go:54:0-155:1))\
  **Codemaps:** CM14, CM17, CM3

---

### Phase 2 – Post-Review Pipeline Harness

- **Task:** Implement overseer post-review handler
  ([PostReviewHandler](cci:2://file:///Users/jkatigbak/repos/personal/agentctl/internal/indexing/handler.go:14:0-20:1),
  events, indexer fanout)\
  **Codemaps:** CM9, CM7, CM8, CM13

- **Task:** Implement configuration for `indexing.post_review.indexers`\
  **Codemaps:** CM9, CM10, CM13

- **Task:** Emit a single internal event/command consumed by semantic, symbol,
  future indexers\
  **Codemaps:** CM9, CM10, CM13

---

### Phase 3 – Semantic File Index v1

- **Task:** Implement named-memory schema + helper lib for `file_embedding` +
  chunks\
  **Codemaps:** CM10, CM13

- **Task:** Implement embedding jobs (`semantic_index.init_files` /
  `update_files`)\
  **Codemaps:** CM7, CM8, CM10, CM13

- **Task:** Wire to post-review handler (subscribe to Phase 2 output, reindex
  touched files)\
  **Codemaps:** CM9, CM10, CM7, CM8

- **Task:** Implement optional CLI (`agentctl semantic-index ...`)\
  **Codemaps:** CM3, CM10

- **Task:** Add tests + golden outputs (no chunking, chunking, config changes,
  error codes)\
  **Codemaps:** CM14, CM17, CM10, CM13

---

### Phase 4 – Code Symbol Index v1 (Go-first)

Treat all symbol-index tasks in this phase as sharing the same codemap set:

- **Phase 4 (all tasks)**\
  **Codemaps:** CM1, CM2, CM4, CM7, CM8, CM10, CM11, CM13

---

### Phase 5 – SWE Grep

Remember: `code/context_ripgrep` is the basis for `code/snippet_extract`.

- **Phase 5 (all tasks: live workspace grep, symbol/semantic awareness, error
  codes)**\
  **Codemaps:** CM1, CM2, CM4, CM11, CM5, CM12, CM13, CM10

---

### Phase 6 – dspy-go Tools & Agents Wiring

- **Phase 6 (all tasks: exposing retrieval tools to agents, tool wiring)**\
  **Codemaps:** CM1, CM2, CM4, CM6, CM9, CM10, CM11, CM13, CM15, CM16

---

### Phase 7 – Trajectory Capture & Export

- **Phase 7 (all tasks: record tool usage, CAS artifacts, export jobs)**\
  **Codemaps:** CM3, CM5, CM12, CM13, CM7, CM8, CM10, CM14

---

### Phase 8 – Teams & Routing

- **Phase 8 (all tasks: teams, routing, team-aware agents/skills)**\
  **Codemaps:** CM6, CM9, CM10, CM13

---

### Phase 9 – End-to-End Flows & Polish

- **Phase 9 (all tasks: UX polish, full workflows, docs)**\
  **Codemaps:** CM3, CM13, CM14, CM17, plus whatever specific subsystems you
  touch (e.g., CM10 for knowledge-heavy flows, CM6/CM9 for agent-heavy flows).

---

## 3) How to Use This

- When you reference a task like **“Phase 2 – Implement overseer post-review
  handler”**, you can append something like:\
  `#refs: CM9 (overseer), CM7/CM8 (jobs), CM13 (core profile)`
- When you talk about **“Phase 5 – SWE Grep v1”**, tie it to:\
  `#refs: CM1/CM2/CM4/CM11 (skill + runner), CM10 (index-backed grep), CM5/CM12/CM13 (CAS+envelopes)`

---

## 4) Codemaps → Phases (Impl + Testing)

This section is the **authoritative crosswalk** from each codemap (CM1–CM17) to
the phases in:

- **Impl plan:** `docs/impl_plan/universal_swe_grep_and_agents.md`
- **Test plan:** `docs/impl_plan/universal_swe_grep_and_agents_testing.md`

It stays at **phase / theme level**, not every micro-task, so it remains
readable.

### CM1 – agentctl Skill System: Build → Discovery → Execution → Runtime

- **Impl plan phases**
  - **Phase 0:** Pre-flight checks around hooks/skills readiness (hooks are just
    skills built and run through this system).
  - **Phase 1:** `hooks/task_guard` implementation and usage.
  - **Phase 5:** SWE Grep skill (`code/snippet_extract` / `code/context_ripgrep`) as a
    first-class skill.
  - **Phase 6:** dspy-go tools exposed as skills (`todo/manage`, SWE Grep,
    http/openapi, etc.).
- **Test plan phases**
  - **Phase 0:** Basic smoke tests that skills can be built and invoked via
    `agentctl run`.
  - **Phases 5–6:** Skill-level integration / golden tests for SWE Grep and
    agent-facing tools.

### CM2 – agentctl Skill System: Discovery, Validation, and Execution

- **Impl plan phases**
  - **Phase 0:** Verifying that existing hooks/skills are discoverable under the
    configured search paths.
  - **Phase 1:** `hooks/task_guard` manifest and execution behavior.
  - **Phase 5:** Skill naming, manifests, and execution for `code/snippet_extract`.
  - **Phase 6:** Discovery + validation for dspy-go tools and other agent-facing
    skills.
- **Test plan phases**
  - **Phase 0:** Tests that invalid manifests / wrong distro types fail with
    actionable errors.
  - **Phases 5–6:** Tests that skills used by SWE Grep and agents can be
    resolved and executed correctly.

### CM3 – agentctl Envelope Protocol & CLI Pipeline

- **Impl plan phases**
  - **Phase 0:** Pre-flight verification that `agentctl` CLI and envelope I/O
    are behaving correctly.
  - **Phase 2–3–5:** Any tasks that mention **golden envelopes** or CAS-wrapped
    outputs for post-review indexing and SWE Grep.
  - **Phase 7:** Trajectory capture/export uses envelopes as the core wire
    contract.
  - **Phase 9:** End-to-end flows and UX polish across the CLI pipeline.
- **Test plan phases**
  - Cross-cutting **golden envelope** tests for jobs, skills, indexing, and SWE
    Grep outputs.

### CM4 – agentctl Skill System: Installation, Execution, Jobs, CAS, and Plugin Architecture

- **Impl plan phases**
  - **Phase 0:** Verifying skill install footprint and that installed skills
    execute correctly with Jobs + CAS.
  - **Phase 5:** Distribution/installation story for `code/snippet_extract` as a skill.
  - **Phase 6:** Any plugin-based skills used by agents (e.g., skills that rely
    on auth/pagination plugins).
- **Test plan phases**
  - **Phase 0:** Installation + execution tests for core skills.
  - **Phases 5–6:** Tests that installed skills participate correctly in jobs
    and CAS workflows.

### CM5 – CAS Storage & Integrity Verification in agentctl

- **Impl plan phases**
  - **Phase 0:** Part of "Jobs + CAS integration" verification.
  - **Phase 1:** "Review artifact storage + CAS logs" task.
  - **Phase 7:** CAS-backed storage for captured trajectories and exports.
- **Test plan phases**
  - **Phase 0:** CAS unit/integration tests (put/get, digest mismatch,
    deduplication).
  - **Phase 1:** Tests that review artifacts are stored in CAS and validated.
  - **Phase 7:** Tests that trajectory data is CAS-wrapped when large.

### CM6 – Dspy-Go Agent Runtime & Tools Integration in agentctl

- **Impl plan phases**
  - **Phase 6:** dspy-go Tools & Agents (tool wiring, orchestration,
    tool-calling behavior).
  - **Phase 8:** Teams & routing where agents are grouped and coordinated.
- **Test plan phases**
  - **Phase 6:** Agent-level integration tests invoking tools and honoring
    job/memory semantics.
  - **Phase 8:** Team-aware agent tests (routing, ownership, coordination).

### CM7 – agentctl Job System: Types, Storage, and WFQ Scheduler

- **Impl plan phases**
  - **Phase 0:** "Verify current state of Jobs" task.
  - **Phase 2:** Post-review pipeline, if or when indexers are executed via
    jobs.
  - **Phase 3:** Embedding "jobs" in the semantic index (conceptually job-like
    work, even if wired directly).
  - **Phase 7:** Trajectory capture/export jobs.
- **Test plan phases**
  - **Phase 0:** Job store + scheduler tests (submission, WFQ, state
    transitions).
  - **Phase 7:** Tests that long-running exports go through the job system.

### CM8 – agentctl Job System: Submission, SQLite Storage, State Transitions, and WFQ Scheduler

- **Impl plan phases**
  - Same as **CM7**, with emphasis on the concrete SQLite-backed state
    transitions and submission paths for jobs across phases 0, 2, 3, and 7.
- **Test plan phases**
  - Same as **CM7**, focusing on submission APIs and state transitions.

### CM9 – Agentctl Overseer & Agent Hierarchy: Spawn Protocol, Depth Limits, and Session Management

- **Impl plan phases**
  - **Phase 2:** Overseer-style coordination in the post-review handler (the
    harness that fans out to indexers).
  - **Phase 6:** Agent hierarchy (child agents, depth limits) as part of dspy-go
    tools.
  - **Phase 8:** Teams & routing where higher-level controllers manage
    specialist agents.
- **Test plan phases**
  - **Phase 2:** Tests of post-review coordination (single event, multiple
    indexers).
  - **Phases 6–8:** Agent hierarchy and session management tests.

### CM10 – agentctl Knowledge System & Factory Droids

- **Impl plan phases**
  - **Phase 3:** Semantic File Index v1 – knowledge over file embeddings.
  - **Phase 4:** Code Symbol Index v1 – knowledge over code symbols.
  - **Phase 5:** SWE Grep that draws on semantic/symbol indices (index-backed
    grep).
  - **Phase 6:** Agents using knowledge-backed retrieval.
  - **Phase 7:** Knowledge-aware trajectory capture.
- **Test plan phases**
  - **Phases 3–4–5:** Tests that semantic/symbol indices are queryable and
    stable.
  - **Phase 6:** Agent tools using these indices for retrieval.

### CM11 – agentctl Execution Runners: WASI & Exec with Network/FS Policy Enforcement

- **Impl plan phases**
  - **Phase 0:** Verifying exec/WASI runners and path/network policies.
  - **Phase 1:** `hooks/task_guard` (exec) and its path policy behavior.
  - **Phase 5:** Runner behavior for `code/snippet_extract` (likely exec), including
    workspace path handling.
  - **Phase 6:** Runners for agent-facing skills (exec or WASI) with correct
    policies.
- **Test plan phases**
  - Cross-cutting tests around:
    - Path validation & workspace anchoring.
    - `network="none"` enforcement for WASI.
    - Environment injection (`AGENTCTL_WORKSPACE`).

### CM12 – agentctl CAS: Put, Get, Integrity, Deduplication & Integration

- **Impl plan phases**
  - Overlaps with **CM5**, focusing on how CAS is integrated into workflows in
    phases 0, 1, and 7.
- **Test plan phases**
  - Same CAS tests as **CM5**, plus any CAS integration tests for skills and
    indexing.

### CM13 – agentctl Core Profile v1: End-to-End Envelope, Jobs & CAS Flow

- **Impl plan phases**
  - **Phase 0:** Canonical reference for overall pre-flight correctness.
  - **Phases 1–3–4–5:** Ensuring new behaviors (dirtying, indexing, SWE Grep)
    respect the core profile contract.
  - **Phase 7:** Job+CAS+envelope semantics for trajectory capture/export.
  - **Phase 9:** Final polish and conformance to the core profile.
- **Test plan phases**
  - Cross-cutting conformance/golden tests for envelopes, jobs, and CAS across
    all phases.

### CM14 – agentctl Test Infrastructure: Test-Watch, Feedback Hooks, and CI Targets

- **Impl plan phases**
  - **Phase 0:** CI checks and test feedback integration.
  - **Phase 1:** `hooks/test_feedback` and test watcher ergonomics.
  - **Phase 9:** End-to-end DX polish involving watch + feedback.
- **Test plan phases**
  - Front-matter principles (deterministic tests, fixtures, CI) and any sections
    referencing:
    - `agentctl watch tests`.
    - `hooks/test_feedback`.

### CM15 – OpenAPI Skill & Plugin Protocol: Request Flow and Plugin Integration

- **Impl plan phases**
  - **Phase 6:** dspy-go Tools & Agents, where agents call `http/openapi`:
    request building, auth, pagination, and plugins.
- **Test plan phases**
  - **Phase 6:** Agent + `http/openapi` integration tests, including dry-run
    request plans, auth failures, and pagination.

### CM16 – agentctl Planning LLM Stack: Auto, Providers, and Integration Tests

- **Impl plan phases**
  - **Phase 6:** Any work where agents or `todo/manage` delegate planning to an
    external LLM (OpenRouter/Groq/OpenAI).
- **Test plan phases**
  - **Phase 6:** LLM planning integration tests, gated by environment flags and
    CI configuration.

### CM17 – Test Infrastructure & CI Pipeline: Test Watcher, Makefile Targets, and Coverage Thresholds

- **Impl plan phases**
  - **Phase 0:** "Add/confirm CI checks" and local coverage thresholds.
  - **Phase 9:** Polishing end-to-end flows including CI coverage guarantees.
- **Test plan phases**
  - Cross-cutting references to CI jobs, coverage thresholds, and `make` targets
    used by tests across all phases.
