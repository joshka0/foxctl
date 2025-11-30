# 2025-11-30 – Phase 0 docs pointer and hooks/file_guard tests

## Changes

- Added an explicit pointer in `README.md` under the Skill Execution Flow
  section to `docs/spec/review_semantic_trajectory_specs.md`, describing how the
  review gate, post-review handler, semantic/symbol index, SWE Grep, and
  trajectory capture fit into the overall execution pipeline.
- Added a related documentation entry in `ARCHITECTURE.md` that links to
  `docs/spec/review_semantic_trajectory_specs.md` as the end-to-end spec for
  review → post-review indexing → trajectories.
- Implemented `skills/hooks_file_guard/main_test.go` with tests that:
  - Verify `hooks/file_guard` reserves a file path for the active task and
    records a corresponding file reservation in the blackboard store.
  - Verify strict mode blocks when a conflicting exclusive reservation already
    exists for the same file, surfacing conflict details in the hook output.
- Updated `docs/impl_plan/universal_swe_grep_and_agents.md` Phase 0 to mark the
  documentation pointer task as completed.
- Updated `docs/impl_plan/universal_swe_grep_and_agents_testing.md` Phase 0 to
  mark the Hook sanity checklist as completed now that both `task_guard` and
  `file_guard` behaviors are covered by tests.
