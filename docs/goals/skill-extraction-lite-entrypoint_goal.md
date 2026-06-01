# Goal: skill extraction lite entrypoint readiness

## Goal
Complete the current foxctl skill-extraction preparation work for the `feature/skill-pack-lite-entrypoint` branch by converting the remaining low-risk, store-less extraction candidates to `internal/adapters/skillslib/skillmain/lite`, documenting the skills that still need separate helper or architecture work, and leaving the merge request ready for CI review.

This goal is for the current extraction-prep MR. It must not move skills into a new repository, publish a public SDK module, or perform broad architecture rewrites unless the user explicitly starts a new phase for that work.

## Context
- Branch: `feature/skill-pack-lite-entrypoint`.
- Existing lite entrypoint package: `internal/adapters/skillslib/skillmain/lite`.
- Current converted pilots:
  - `skills/json_transform`
  - `skills/presence_parse`
  - `skills/cloud_localstack_blueprint`
  - `skills/providers`
  - `skills/presence_orchestrate`
  - `skills/skill_inspect`
  - `skills/build_godot`
  - `skills/build_unity`
  - `skills/quality_gate`
  - `skills/setup_install`
  - `skills/html_edit`
  - `skills/jira_board`
  - `skills/jira_issue`
  - `skills/heartwood_action`
  - `skills/heartwood_state`
  - `skills/unity_input`
  - `skills/unity_packages`
  - `skills/unity_scenes`
  - `skills/x402_payment`
  - `skills/ardoq_resource`
  - `skills/ci_checks`
  - `skills/setup_foxctl_mode`
  - `skills/code_llm_search`
  - `skills/lsp_gopls`
  - `skills/lsp_pylsp`
  - `skills/lsp_tsserver`
  - `skills/git_worktree`
  - `skills/mcp_bridge`
  - `skills/mcp_install`
- Current docs:
  - `docs/architecture/skill-pack-split-analysis.md`
  - `docs/architecture/skill-pack-split-analysis-pi.md`
- Dependency audit entrypoint:
  - `make skills-dependency-audit`
  - `scripts/skill-dependency-audit.sh`
  - `scripts/skill_dependency_audit/`
- Current likely simple candidates include store-less skills that only need envelope/config/path validation and `lite.Emit`.
- Skills using `skillout.PersistBuffer`, `EmitWithCAS`, `PreviewAndPersist*`, full store providers, runtime observability, or intelligence packages are not simple conversion candidates unless a narrow helper phase is approved.
- Pi and Hermes should do most implementation slices through `herdr` panes; the integrator owns scope, review, commits, pushes, and CI/MR follow-up.

## Current Classification Notes
- `skills/skill_inspect`, `skills/build_godot`, `skills/build_unity`, `skills/quality_gate`, `skills/setup_install`, `skills/html_edit`, `skills/jira_board`, `skills/jira_issue`, `skills/heartwood_action`, `skills/heartwood_state`, `skills/unity_input`, `skills/unity_packages`, `skills/unity_scenes`, `skills/x402_payment`, `skills/ardoq_resource`, `skills/ci_checks`, `skills/setup_foxctl_mode`, `skills/code_llm_search`, `skills/lsp_gopls`, `skills/lsp_pylsp`, `skills/lsp_tsserver`, `skills/git_worktree`, `skills/mcp_bridge`, and `skills/mcp_install`: converted in this goal; dependency checks are quiet.
- `skills/agent_handbook`: `helper-needed` / defer. The skill itself is simple, but it imports `internal/runtime/agentpolicy`; converting it would violate the lite package boundary until agent policy types move to a non-runtime package or a narrow stable type surface is introduced.
- `skills/fs_apply_edit`: `helper-needed` / defer. Its shared edit helper takes full `skillmain.RunContext` and supports CAS backup hints, so it needs a deliberate helper split before lite conversion.
- `skills/presence_character`: `helper-needed` / defer. It reads full config storage settings, which are intentionally absent from `LiteConfig`.
- `skills/repo_index_build` and `skills/repo_index_enrich_summaries`: `keep-core` / defer. They are store-less wrappers, but their protocol decoding currently pulls storage/intelligence packages transitively, so the dependency audit is not quiet.
- CAS-heavy and persistence-heavy skills remain out of scope for this MR unless a separate helper phase is approved. Examples include skills using `skillout.PersistBuffer`, `EmitWithCAS`, `PreviewAndPersist*`, `rc.CAS`, or `rc.MaxPreview`.

## Constraints
- Preserve observable behavior and envelope shape for converted skills.
- Prefer small, disjoint agent slices with explicit file ownership.
- Pi and Hermes must not run git commands.
- Do not revert user or agent work unless explicitly requested.
- Do not add new dependencies without explicit approval.
- Do not introduce broad compatibility layers or duplicate SDK abstractions.
- Do not migrate CAS-heavy skills by weakening large-output behavior.
- Avoid brittle test theatre. Add or keep behavior-focused tests through the public `run`/envelope boundary when useful.
- Keep the full `skillmain` package for skills that legitimately need stores, runtime, CAS persistence, observability, or intelligence integration.
- Stop after three repeated failed attempts at the same verification failure and summarize the blocker.

## Milestones

### 1. Finish Current In-Flight Slices
- Let Pi complete `skills/agent_handbook` conversion if it is already in progress.
- Let Hermes complete `skills/skill_inspect` conversion if it is already in progress.
- Review and trim any new tests so they protect behavior rather than implementation details.
- Run focused tests and forbidden dependency checks before committing.

### 2. Audit Remaining Candidates
- Re-run a candidate scan for skills still using full `skillmain`.
- Classify each remaining candidate as:
  - `convert-now`: store-less and simple `lite.Emit` path.
  - `helper-needed`: blocked by CAS/persistence helper or narrow shared SDK gap.
  - `keep-core`: depends on runtime, storage, intelligence, observability, or core foxctl harness behavior.
  - `defer`: extractable later but not needed for this MR.
- Update docs or MR notes with the classification and next TODOs.

### 3. Convert Only Low-Risk Candidates
- Assign Pi and Hermes disjoint `convert-now` slices.
- Convert imports and run signatures from full `skillmain`/`skillout` to `skillmain/lite`.
- Update tests to use `lite.BuildRunContext` when they exercise `run`.
- Avoid converting any skill that uses `skillout.PersistBuffer`, `EmitWithCAS`, `PreviewAndPersist*`, full stores, runtime, intelligence, or context packages unless the helper work is explicitly approved.

### 4. Verification And Commit Hygiene
- After each slice, run focused tests for touched packages.
- Run the forbidden dependency check for touched packages:
  ```sh
  go list -deps ./skills/<name> | rg '^github.com/joshka0/foxctl/internal/(storage|runtime|intelligence|context)' || true
  ```
- Before commit, run:
  ```sh
  go test ./internal/adapters/skillslib/skillmain/lite ./skills/<touched>...
  make static-analysis
  ```
- Commit logical batches with focused messages.
- Push `feature/skill-pack-lite-entrypoint` after each stable commit.

### 5. MR Readiness
- Confirm the working tree is clean.
- Summarize:
  - Converted skills.
  - Remaining `helper-needed`, `keep-core`, and `defer` lists.
  - Verification commands and results.
  - Any CI failures and the smallest next fix.

## Verification
- Focused package tests pass for all touched skills.
- `go test ./internal/adapters/skillslib/skillmain/lite` passes.
- Forbidden dependency checks are quiet for every converted skill.
- `make static-analysis` passes.
- No generated binaries or build artifacts are staged.
- Branch is pushed to GitLab.
- Final self-review lists residual risks and whether any extraction work remains outside this MR.

## Done When
- All current `convert-now` candidates selected for this MR are converted or explicitly deferred with a reason.
- Documentation/MR notes identify remaining extraction work.
- CI-relevant local checks pass.
- The branch is clean and pushed.

## Stop Conditions
- Stop before external repo creation or public SDK module extraction.
- Stop before adding CAS persistence to `skillmain/lite`; capture it as a separate helper-needed phase instead.
- Stop before changing public skill input/output contracts.
- Stop if an agent edits outside assigned files.
- Stop if the same test or static-analysis failure repeats three times without a new hypothesis.
