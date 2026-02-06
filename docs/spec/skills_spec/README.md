# Skills Spec Index (Draft)

## 1. Purpose

This directory collects **skill- and job-level contracts** that are implied by
other specs (review gate, semantic index, symbol index + SWE Grep, trajectories,
agent runtime). The goal is to maintain a single, implementation-oriented view
of:

- Which skills/operations must exist.
- How they are grouped by domain.
- Which spec sections define their behavior.

This file is an index and planning aid. The **normative behavior and data
shapes** always live in the linked specs.

Related specs:

- `../review_gate.md`
- `../semantic_file_index.md`
- `../code_symbol_index_and_swe_grep.md`
- `../archive/specs/dspy_go_agents.md` (legacy runtime reference)
- `../archive/specs/dspy_trajectory_capture.md` (legacy trajectory format)

---

## 2. Task & Review Skills

These are primarily operations on the existing `todo/manage` and
`mailbox/manage` skills.

### 2.1 `todo/manage` Operations

- **`todo/manage.review_request`**
  - **Purpose:** Initiate the review pipeline for a task.
  - **Spec:** `review_gate.md` §292–317.
  - **Notes:**
    - Validates task state (`in_progress` / `ready_for_review`).
    - Schedules review jobs and returns `review_id` or `job_id`.

- **`todo/manage.complete` (revised semantics)**
  - **Purpose:** Mark a task as completed, respecting the review gate.
  - **Spec:** `review_gate.md` §318–341.
  - **Notes:**
    - When the gate is enabled, SHOULD require a recent passing review.
    - May implicitly trigger `review_request`.

- **`todo/manage.review_status` (optional)**
  - **Purpose:** Query last review outcome for a task.
  - **Spec:** `review_gate.md` §342–356.

### 2.2 Mailbox-Based Review Flow

- **`mailbox/manage.send`**
  - **Purpose:** Send `review_request` / `review_result` messages.
  - **Spec:** `review_gate.md` §421–446, `mailbox_blackboard.md`.

- **`mailbox/manage.inbox` / `mailbox/manage.ack`**
  - **Purpose:** Allow reviewers and overseer agents to process review-related
    mail.
  - **Spec:** `archive/specs/dspy_go_agents.md` §5.5 (legacy runtime), `mailbox_blackboard.md`.

These skills already exist conceptually; this spec simply surfaces them in one
place for planning.

---

## 3. Semantic File Index & Symbol Index Jobs

The following are **internal jobs/operations** that may or may not be exposed as
first-class skills, but should have clear contracts.

### 3.1 Semantic File Index Jobs

- **`semantic_index.init_files`**
  - **Purpose:** Initial embedding for one or more files.
  - **Spec:** `semantic_file_index.md` §6.1–6.3.

- **`semantic_index.update_files`**
  - **Purpose:** Re-embed existing entries (post-review, manual refresh).
  - **Spec:** `semantic_file_index.md` §4.2, §6.1–6.3.

These jobs:

- Take
  `(workspace_id, files[{path, digest, change_kind, ...}], reason, task_id?, review_id?)`.
- Read content from CAS or workspace under `PathValidator`.
- Write `named_memory` rows and optional vector rows.

### 3.2 Code Symbol Index Jobs

Defined in `code_symbol_index_and_swe_grep.md`.

- **`code_symbol_index.update_files`** (conceptual)
  - **Purpose:** Maintain per-symbol embeddings and call graph for touched
    files.
  - **Inputs:**
    - `workspace_id`.
    - `files[{path, digest, change_kind, ...}]` from a review or commit.
    - `reason` (e.g. `"post_review"`, `"manual"`).
    - Optional `task_id`, `review_id`.
  - **Behavior:**
    - Parse files with Tree-sitter.
    - For each symbol, update `symbols` and `calls` per the symbol index spec.
    - Update `file_meta` hashes.

This job is typically invoked by a **post-review handler** in response to an
accepted review (see `review_gate.md` and `semantic_file_index.md` §8.2–8.3).

---

## 4. SWE Grep & Retrieval Skills

These are new skills implied by the symbol index and agent runtime specs.

### 4.1 `code/snippet_extract` (exec skill)

- **Purpose:** Given a natural-language question and candidate files/symbols,
  extract high-signal code snippets via live reads and a small LM.
- **Spec:** `code_symbol_index_and_swe_grep.md` §5.
- **Inputs (conceptual):**
  - `workspace_id`.
  - `question`.
  - `candidates[]` with `path`, optional `symbol_id`, `priority`.
  - Optional `limits`.
- **Outputs (conceptual):**
  - `summary` (files considered/relevant, snippets emitted).
  - `snippets_inline[]` (small previews).
  - Optional CAS `artifact` (NDJSON snippets) (with optional `meta.cas_digest`
    matching `data.artifact`).

### 4.2 Agent Tools (Runtime Layer)

While not skills themselves, the following tools wrap skills or internal helpers
and should be implemented alongside `code/snippet_extract`:

- **`code.symbol_search`**
  - **Backend:** Go helper over the symbol index.
  - **Spec:** `code_symbol_index_and_swe_grep.md` §6.1, `archive/specs/dspy_go_agents.md` (legacy runtime)
    §5.1.

- **`code.swe_grep`** (tool)
  - **Backend:** `code/snippet_extract` skill.
  - **Spec:** `code_symbol_index_and_swe_grep.md` §6.2, `archive/specs/dspy_go_agents.md` (legacy runtime)
    §5.1.

These tools form the recommended **funnel** for Coding/Review agents:

1. Use `code.symbol_search` and/or semantic file index search to propose
   candidate files/symbols.
2. Call `code.swe_grep` to obtain focused snippets.
3. Use snippets with tasks, reviews, and docs to plan edits or answer queries.

---

## 5. Trajectory & Export Jobs

### 5.1 `trajectory.export` (job/skill)

- **Purpose:** Build training-ready episodes from stored trajectories for export.
- **Spec:** `archive/specs/dspy_trajectory_capture.md` §7 (legacy).
- **Inputs (conceptual):**
  - `workspace_id`.
  - Filters: `task_id?`, `epic_id?`, time range, `agent_role?`, `status?`.
  - `format` (e.g. `"ndjson"`).
  - `include_raw_traces` (bool).
- **Outputs:**
  - Either streamed envelopes with `data.episode`, or a CAS artifact digest for
    an NDJSON file of episodes.

This is a natural candidate for an internal job first, and an optional skill
wrapper later.

---

## 6. Teams & Routing (Future Skills)

Teams are defined conceptually in `archive/specs/dspy_go_agents.md` §4.3 (legacy runtime). To support routing,
dashboards, and overseer behavior, the following skills are natural follow-ons:

- **`teams/manage.list`**
  - **Purpose:** List teams in a workspace.
  - **Spec:** `archive/specs/dspy_go_agents.md` §4.3 (legacy runtime).

- **`teams/manage.describe`**
  - **Purpose:** Show details (members, tags, `primary_epics`) for a team.

- **`teams/manage.upsert`**
  - **Purpose:** Create or update a team definition.

- **`teams/manage.add_member` / `teams/manage.remove_member`**
  - **Purpose:** Manage team membership for agents and humans.

Storage expectation:

- Backed by SQLite tables `teams` and `team_members` (see `archive/specs/dspy_go_agents.md` legacy section)
  >4.3.1).
- Config files MAY seed initial data, but runtime changes SHOULD go through
  these operations once implemented.

---

## 7. Implementation Notes

- **Kernel-owned:** All embedding/indexing and SWE Grep behavior is implemented
  in Go and/or tightly-scoped exec skills. Agents access these via tools, not by
  constructing low-level job inputs.
- **Post-review as canonical trigger:** Both semantic file index and symbol
  index jobs SHOULD treat post-review events as their normative refresh point,
  with optional heuristic triggers (e.g. on commit) layered on top.
- **CAS and Protocol v1:** All large outputs (e.g. SWE Grep results, trajectory
  exports) must respect Protocol v1 CAS rules (`meta.cas_digest` is optional; if
  set it MUST match `data.artifact`).

This index should be updated as new specs introduce additional skills or job
entry points that we want to implement in parallel.
