# Spec refinements based on external review

Date: 2025-11-29

This changelog captures small but important refinements to several specs based
on external review feedback.

## Updated specs

- **`docs/spec/semantic_file_index.md`**
  - Clarified that chunk boundaries **do not depend on file contents** and are a
    pure function of `(path, chunk_bytes, chunk_overlap_bytes, cfg hash)`.
  - Made post-review indexing explicitly **canonical**, while allowing
    best-effort refreshes on git commits and other events as non-normative
    heuristics.

- **`docs/spec/review_gate.md`**
  - Tightened the **dirtying semantics**:
    - `hooks/task_guard` MUST auto-demote tasks from `ready_for_review` or
      `completed` back to `in_progress` when new writes occur under their scope.
    - Any prior `ok` review is marked conceptually **stale**; a new review is
      required before completion again.
  - Explicitly allowed workspaces to treat **max file/function length and
    complexity/duplication thresholds** as part of the minimal static review
    pipeline.

- **`docs/spec/code_symbol_index_and_swe_grep.md`**
  - Clarified the relationship between **canonical post-review updates** and
    **heuristic triggers** (e.g. on git commits) for the symbol index.
  - Added a section on **live SWE Grep vs index staleness**, stating that:
    - The symbol/semantic indexes represent the last accepted snapshot.
    - `code/snippet_extract` always reads **live workspace files**.
    - When there is a discrepancy, reasoning should favor SWE Grep snippets and
      diffs for the current task over stale index metadata.

- **`docs/spec/dspy_go_agents.md`**
  - Specified that `edit.apply_patch` uses the **JSON structured diff format
    emitted by the `code_diff` skill**, not raw unified diffs.
  - Clarified that team data SHOULD live in dedicated SQLite tables (`teams`,
    `team_members`), with config used primarily for seeding.

- **`docs/spec/skills_spec/README.md`**
  - Added a **Teams & Routing** section sketching future `teams/manage.*` skills
    (`list`, `describe`, `upsert`, `add_member`, `remove_member`) and tied them
    to the teams data model in `dspy_go_agents.md`.

- **`docs/spec/task_graph_insights.md`**
  - Defined SCC-based handling for **cycles**:
    - Analyzer MUST compute strongly connected components and conceptually treat
      each SCC as a super-node for metrics that assume a DAG (e.g. critical
      path).
  - Clarified that **critical_path_score** is computed on the SCC-DAG, and that
    nodes within the same SCC may share the same base score.
  - Allowed `topological_order` to be expressed over SCCs when cycles exist, or
    omitted/marked incomplete.

These changes keep the original architecture intact but reduce ambiguity for
implementers and make kernel-owned behavior (dirtying, indexing triggers, patch
format, and cycle handling) more explicit.
