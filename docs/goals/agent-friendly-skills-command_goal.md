# Goal: agent-friendly skills command readiness

## Goal
Finish the AI-agent-friendly `foxctl skills` command feature on branch
`feat/agent-friendly-skills-help` in the isolated worktree
`/home/dev/repos/foxctl-agent-skills-help`, leaving it ready for review and
merge without touching the original checkout at `/home/dev/repos/foxctl`.

The intended behavior is:

- Root `foxctl --help` shows a prominent `Start here (for AI agents):` block
  immediately after root usage syntax.
- `foxctl skills` and `foxctl skills list` list installed skills.
- `foxctl skills --compact` and `foxctl skills list --compact` provide
  context-window-friendly skill summaries.
- `foxctl skills search "<task>"` returns ranked skill matches with concrete
  next-step commands.
- `foxctl skills get <name>` emits concise, copy-pasteable guide text for an
  installed skill.
- `foxctl skills get foxctl` emits a mandatory built-in core onboarding guide.
- `foxctl skills path [name]` prints the absolute skills root or resolved
  skill directory path for filesystem-aware agents.
- `foxctl skills doctor [name] --strict` acts as a drift guardrail between
  `skill.yaml` feature contracts and `SKILL.md` agent-facing guides.
- CI runs the strict doctor gate for changed skills only; the global doctor
  remains advisory while legacy skill guides are cleaned up.

## Context
- Worktree: `/home/dev/repos/foxctl-agent-skills-help`.
- Branch: `feat/agent-friendly-skills-help`.
- Original checkout to leave untouched: `/home/dev/repos/foxctl`.
- Current touched files:
  - `README.md`
  - `.gitlab-ci.yml`
  - `Makefile`
  - `cmd/foxctl/cmd/root.go`
  - `cmd/foxctl/cmd/completion_test.go`
  - `cmd/foxctl/cmd/context_records.go`
  - `cmd/foxctl/cmd/curator_test.go`
  - `cmd/foxctl/cmd/errors_test.go`
  - `cmd/foxctl/cmd/fs_test.go`
  - `cmd/foxctl/cmd/memory_helpers.go`
  - `cmd/foxctl/cmd/memory_test.go`
  - `cmd/foxctl/cmd/obs_test.go`
  - `cmd/foxctl/cmd/openapi_test.go`
  - `cmd/foxctl/cmd/searchindex.go`
  - `cmd/foxctl/cmd/searchindex_test.go`
  - `cmd/foxctl/cmd/skills.go`
  - `cmd/foxctl/cmd/skills_doctor.go`
  - `cmd/foxctl/cmd/skills_list.go`
  - `cmd/foxctl/cmd/skills_search.go`
  - `cmd/foxctl/cmd/skills_commands_test.go`
  - `cmd/foxctl/cmd/root_help_test.go`
  - `cmd/foxctl/cmd/skills_get.go`
  - `cmd/foxctl/cmd/skills_get_path_test.go`
  - `cmd/foxctl/cmd/skills_path.go`
  - `docs/general/skills.md`
  - `docs/goals/agent-friendly-skills-command_goal.md`
  - `internal/platform/workspace/workspace.go`
  - `internal/platform/workspace/workspace_test.go`
  - `internal/runtime/hooks/analysisflow/analysisflow.go`
  - `internal/runtime/hooks/analysisflow/analysisflow_test.go`
  - `internal/runtime/runservice/remember.go`
- Current focused verification passes:
  ```sh
  env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./cmd/foxctl/cmd -run 'Test(RootHelp|Skills(Default|CommandDefaults|List|Get|Path|Describe|Help))'
  ```
- Current staged strict-gate verification passes:
  ```sh
  make skills-doctor-changed BASE_REF=HEAD HEAD_REF=HEAD
  ```
- Current advisory global doctor target exits successfully while reporting
  legacy guide drift:
  ```sh
  make skills-doctor
  ```
- Current smoke commands pass through `go run`:
  ```sh
  env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go run ./cmd/foxctl --help
  env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go run ./cmd/foxctl skills get foxctl
  ```
- Current broad command-package verification passes after fixing explicit
  workspace handling for command flags, hook analysis, searchindex stats, and
  `run --remember` memory writes.

## Constraints
- Do not edit `/home/dev/repos/foxctl` or any branch other than the isolated
  worktree branch.
- Do not revert unrelated user or agent work.
- Do not change protocol envelope shape, `meta.*` semantics, or existing
  `skills list` JSON behavior.
- Do not add new dependencies.
- Do not introduce keyword-heuristic routing or classification behavior.
- Keep the feature in the existing Cobra command style.
- Keep outputs concise and useful for AI agents, with copy-pasteable commands.
- Preserve stdout/stderr expectations: guide/list/path command outputs only on
  stdout; errors on stderr through existing command conventions.
- If exact visual fidelity is required, compare against `image.png` before
  changing the help template further.
- Stop after three repeated failed attempts at the same verification failure
  and summarize the blocker.

## Milestones

### 1. Finalize Feature Semantics
- Review the root help template against the requested `image.png` structure.
- Confirm root-only help injection does not affect subcommand help output.
- Confirm `foxctl skills` remains equivalent to `foxctl skills list`.
- Confirm compact skill listing emits concise text and preserves JSON default.
- Confirm `skills search` ranks matches and emits next-step commands.
- Confirm `skills get` behavior for:
  - Built-in `foxctl` guide.
  - Installed skill with `README.md`.
  - Installed skill with `SKILL.md`.
  - Installed skill with manifest-only fallback.
  - Missing skill error.
- Confirm `skills path` behavior for:
  - No argument.
  - Canonical skill names such as `text/grep`.
  - Normalized names such as `text_grep`.
  - Missing skill error.
- Confirm `skills doctor` behavior for:
  - Missing guide files.
  - Required parameters absent from guides.
  - `--strict` non-zero exit with a report envelope.
- Confirm `make skills-doctor-changed BASE_REF=<base> HEAD_REF=HEAD` gates only
  directly changed skill directories and `make skills-doctor` remains advisory
  for the global report.

### 2. Tighten Tests
- Remove redundant or overlapping tests if they make the suite harder to read.
- Keep behavior-focused coverage for root help ordering, default and compact
  skills list, search next steps, `skills get`, JSON guide output, `skills
  path`, and `skills doctor`.
- Add a missing-skill test only if the current error behavior is not already
  covered elsewhere.
- Run focused tests after every test edit.

### 3. Real Binary And Real Skill Smoke
- Build the local binary:
  ```sh
  make build
  ```
- Smoke the built binary:
  ```sh
  ./bin/foxctl --help
  ./bin/foxctl skills
  ./bin/foxctl skills --compact
  ./bin/foxctl skills search "code search" --compact
  ./bin/foxctl skills get foxctl
  ./bin/foxctl skills path
  ./bin/foxctl skills doctor text/grep
  ```
- Smoke at least one real installed skill when available:
  ```sh
  ./bin/foxctl skills get <real-skill-name>
  ./bin/foxctl skills path <real-skill-name>
  ```

### 4. Broad Verification
- Run final formatting and diff checks:
  ```sh
  /usr/local/go/bin/gofmt -w cmd/foxctl/cmd/root.go cmd/foxctl/cmd/root_help_test.go cmd/foxctl/cmd/skills.go cmd/foxctl/cmd/skills_list.go cmd/foxctl/cmd/skills_search.go cmd/foxctl/cmd/skills_doctor.go cmd/foxctl/cmd/skills_get.go cmd/foxctl/cmd/skills_path.go cmd/foxctl/cmd/skills_commands_test.go cmd/foxctl/cmd/skills_get_path_test.go
  git diff --check
  ```
- Verify the staged strict gate:
  ```sh
  make skills-doctor-changed BASE_REF=HEAD HEAD_REF=HEAD
  ```
- Run focused command tests:
  ```sh
  env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./cmd/foxctl/cmd -run 'Test(RootHelp|Skills(Default|CommandDefaults|List|Get|Path|Describe|Help))'
  ```
- Run the broad command package test:
  ```sh
  env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./cmd/foxctl/cmd
  ```
- Broad verification should pass; if it regresses, first inspect explicit
  workspace ID handling before expanding scope.
- Because this goal file changes docs, run:
  ```sh
  make check-doc-links
  ```

### 5. Review And Commit Readiness
- Confirm untracked feature files are intentionally included.
- Review the final diff for accidental generated files, build artifacts, or
  unrelated edits.
- Prepare a concise commit message, for example:
  ```text
  Add agent-friendly skills help commands
  ```
- Do not push or open a PR unless explicitly instructed.

## Verification
- Root help includes `Start here (for AI agents):` immediately after usage.
- Focused tests for root help and skills commands pass.
- Compact list/search and doctor drift guardrail tests pass.
- Changed-skill strict doctor target passes for an empty impact range.
- `make build` passes.
- Built binary smoke commands pass.
- `git diff --check` passes.
- `make check-doc-links` passes after this docs change.
- Broad `./cmd/foxctl/cmd` package test passes.
- Final self-review lists:
  - Changed files.
  - Verification commands and results.
  - Any unrelated failing tests.
  - Residual risks.

## Done When
- The isolated worktree contains a review-ready implementation of the root help
  injection, compact/search discovery, `skills get/path`, and doctor guardrail
  commands.
- Tests and smoke checks establish the requested behavior.
- Deferred items are documented instead of silently ignored.
- The original checkout remains untouched.
- The branch is ready for the user to review, commit, or hand to another agent.

## Deferred Tasks
- Generated CLI reference docs: no maintained generated-docs workflow found;
  update the hand-written docs instead.
- `skills get` now prefers `SKILL.md` over `README.md`.
- Decide whether `skills path` should support `--format json`; current
  requirement only needs plain absolute paths.
- Decide whether `skills doctor` should become a CI target after existing
  installed skill guides have been brought up to strict compliance. Current CI
  hard-fails only changed skills and leaves the global report advisory.
- Missing-skill UX now includes a short next-step hint.
- Shell completion coverage now verifies `skills get/path` are emitted.
- Known broad command-package failures are fixed in this worktree.

## Stop Conditions
- Stop before changing unrelated hook, memory, envelope, storage, or skill
  runtime behavior.
- Stop before adding dependencies or changing public command contracts beyond
  the requested `skills get/path` additions.
- Stop before editing the original checkout.
- Stop if verification failures require a broad architecture change; summarize
  the blocker and smallest next step.
