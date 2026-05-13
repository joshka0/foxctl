# Goal: Refactor Scout Deepening and Small-Code Cleanup

## Goal

Deepen the refactor scout modules through a small, behavior-preserving cleanup
sequence that improves locality, leverage, and testability without changing the
public scout contract.

The work should turn the current scout findings into concrete refactors:

1. Move the new skill-target routing lens out of
   `skills/code_refactor_scout/main.go` into a focused module.
2. Split dead-code scout workflow phases in
   `skills/code_refactor_scout/deadcode.go`.
3. Extract the repeated Elixir DB/slop scan workflow in
   `skills/code_refactor_scout/db_slop_elixir_cgo.go`.
4. Make snapshot diffing in
   `internal/intelligence/refactor/changes/changes.go` easier to test as pure
   file and symbol diff modules.
5. Decompose co-change parsing and scoring in
   `internal/intelligence/refactor/hot/cochange.go`.

Do not broaden into RLM helper-factory refactoring during this goal. The active
RLM hardening branch may already have unrelated changes; preserve them and leave
`internal/rlm/runtime/helper_factory_tools.go` and
`internal/rlm/runtime/helperpipeline/` alone unless the user explicitly expands
the goal.

## Context

- Current branch context: `feat/rlm-helper-pipeline-hardening`.
- The worktree may already contain hardening changes and generated validation
  artifacts. Do not revert unrelated edits.
- Recent deterministic scout runs flagged these follow-up targets:
  - `skills/code_refactor_scout/main.go`: large module; fresh target-lens
    helpers should become their own small module.
  - `skills/code_refactor_scout/deadcode.go`: dead-code workflow mixes store
    access, structural roots, symbol classification, file/package summaries, and
    suppression.
  - `skills/code_refactor_scout/db_slop_elixir_cgo.go`: several functions
    repeat the same symbol body extraction, tree parse, candidate collection,
    tree close, and finding emission shape.
  - `internal/intelligence/refactor/changes/changes.go`: `diffSnapshots` mixes
    file map creation, path union, file changes, symbol map creation, symbol
    changes, and summary aggregation.
  - `internal/intelligence/refactor/hot/cochange.go`: `BuildCochangeIndex`
    mixes git invocation, log parsing, scope filtering, co-change graph
    construction, weighting, and neighbor sorting.
- Relevant docs:
  - `docs/general/refactor-scout.md`
  - `docs/plans/features/refactor-intelligence-substrate.md`
  - `docs/plans/features/refactor-deterministic-detection-backlog.md`
  - `docs/architecture/package-topology.md`
- Relevant skills and standards:
  - `small-composable-code`: prefer narrow, behavior-preserving refactors; test
    through behavior; add files only when they reduce total complexity.
  - `improve-codebase-architecture`: use module, interface, implementation,
    depth, seam, adapter, leverage, and locality vocabulary; apply the deletion
    test before adding a module or seam.

## Milestones

### Milestone 0: Protect the Branch and Baseline Behavior

Done when:

- `git status --short --branch` has been inspected.
- Unrelated dirty files are identified and left untouched.
- The current target-lens behavior is understood from
  `skills/code_refactor_scout/main.go`,
  `skills/code_refactor_scout/main_test.go`,
  `skills/code_refactor_scout/skill.yaml`,
  `cmd/foxctl/cmd/refactor.go`, and `docs/general/refactor-scout.md`.
- Focused baseline tests pass or any pre-existing failure is recorded:

```bash
GOWORK=off go test -count=1 ./skills/code_refactor_scout
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestRefactor'
```

### Milestone 1: Extract the Target-Lens Module

Done when:

- Target routing constants, target assignment, target filtering, and skill target
  lane construction are moved from `main.go` into a focused file such as
  `skills/code_refactor_scout/targets.go`.
- Target tests are moved or added in a focused file such as
  `skills/code_refactor_scout/targets_test.go`.
- The public JSON contract remains unchanged:
  - `target`
  - `signals.targeted_review`
  - `finding.targets`
  - `finding.target_reasons`
  - `finding.evidence.review_targets`
  - `finding.evidence.review_target_reasons`
  - `data.presentation.lanes.skill_targets`
- Existing semantic anchors and `Index:` metadata stay adjacent to the owning
  target-routing module.
- No new abstraction is introduced beyond the extracted module. The module earns
  its depth by hiding the target mapping and presentation lane implementation
  behind existing scout call sites.

### Milestone 2: Split Dead-Code Scout Phases

Done when:

- `buildDeadCodeFindings` remains the orchestration entrypoint but delegates to
  smaller behavior-focused functions.
- The implementation has distinct phases with clear inputs and outputs, such as:
  - collect file nodes and structural roots
  - collect symbol info and inbound summaries
  - classify symbol reachability
  - build symbol findings
  - build file findings
  - build package findings
  - suppress family findings
- Existing rule IDs, scores, evidence fields, sorting, and focus behavior are
  preserved.
- Tests still cover dead-code focus filtering, file/package suppression, inbound
  summaries, structural root behavior, and language-specific dead-code cases.
- Do not change repoindex schema or dead-code reachability semantics in this
  milestone.

### Milestone 3: Extract the Elixir Slop Scan Workflow

Done when:

- Repeated Elixir symbol-scan boilerplate is concentrated in a small helper that
  owns body extraction, tree parsing, tree closing, and candidate traversal.
- The helper's interface is smaller than the repeated implementation it hides.
- Existing Elixir findings keep the same rule IDs, titles, score formulas,
  evidence fields, line calculations, and confidence values.
- The helper is private to `skills/code_refactor_scout`; do not introduce a
  generic scanner unless at least two real adapters need the seam.
- Existing cgo and stub build behavior remains unchanged.

### Milestone 4: Pure Snapshot Diff Modules

Done when:

- `diffSnapshots` in `internal/intelligence/refactor/changes/changes.go`
  delegates file diffing and symbol diffing to pure helpers.
- Helpers accept snapshot values and limits directly and do not read git, storage
  or environment state.
- Summary aggregation remains deterministic and sorted.
- Existing public types and command outputs stay compatible.
- Tests cover added, deleted, modified, unchanged, limit truncation, and stable
  ordering for both file and symbol changes.

### Milestone 5: Co-Change Parsing and Scoring Locality

Done when:

- `BuildCochangeIndex` remains the public entrypoint and owns git execution.
- Git log parsing, scope filtering, co-change graph construction, and neighbor
  sorting are moved into pure or narrowly side-effecting helpers.
- Time and recency behavior remains deterministic through the existing `now`
  input.
- Existing caps and test-file filtering behavior remain unchanged.
- Tests cover parsing commits, skipping out-of-scope paths, excluding tests when
  requested, max files per commit, max neighbors, and stable neighbor ordering.

### Milestone 6: Final Review and Optional Documentation Update

Done when:

- Any changed docs pass link checks.
- The final self-review identifies:
  - which modules became deeper
  - which interfaces became smaller or stayed intentionally unchanged
  - where leverage and locality improved
  - any shallow modules left for later
  - residual risks and confidence score
- If public behavior did not change, do not add broad release notes.

## Constraints

- Preserve public behavior and JSON output contracts unless this goal explicitly
  lists the field change.
- Do not add dependencies.
- Do not introduce new public packages, exported interfaces, or generic
  abstractions unless the deletion test shows the module earns its depth.
- Do not rename rule IDs, evidence field names, CLI flags, command names, or
  skill manifest names.
- Do not change repoindex schema, storage layout, protocol envelopes, or
  `meta.*` fields.
- Preserve WASI `capabilities.network: "none"` for skills.
- Do not use keyword heuristics for routing, classification, promotion, or
  suppression behavior. Use explicit rule IDs, typed fields, scored features,
  and tests.
- Before adding or moving anything under `internal/*`, read
  `docs/architecture/package-topology.md` and keep work inside the existing
  `internal/intelligence/refactor` family.
- Treat side effects as edge behavior:
  - storage and repoindex reads stay at orchestration edges
  - git execution stays at the `BuildCochangeIndex` edge
  - pure diff, parsing, scoring, and classification helpers should be testable
    without filesystem, git, or database setup
- Keep semantic comments bounded:
  - add `Index:` blocks or `[[...]]` anchors only near durable owner modules
  - do not scatter anchors across leaf helpers
  - validate semantic-anchor parser tests if anchors change
- Preserve user and prior-agent work. Never revert unrelated dirty files.
- Stop after three failed attempts at the same verification failure and
  summarize the blocker with exact command output.

## Verification

Run focused checks after each milestone:

```bash
gofmt -w <touched-go-files>
GOWORK=off go test -count=1 ./skills/code_refactor_scout
```

Run refactor CLI checks when CLI, manifest, or docs contract changes:

```bash
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run 'TestRefactor'
```

Run semantic-anchor checks when `Index:` blocks or `[[...]]` anchors change:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/indexing/semanticanchors ./internal/intelligence/indexing/repoindex
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist
```

Run refactor substrate checks after internal refactor changes:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/refactor/changes ./internal/intelligence/refactor/hot ./internal/intelligence/refactor/scope ./internal/intelligence/refactor/status ./internal/intelligence/refactor/evidence
```

Run docs and diff hygiene before completion:

```bash
make check-doc-links
git diff --check
```

Optional smoke after the refactor scout package passes:

```bash
GOWORK=off go build -o /tmp/code_refactor_scout ./skills/code_refactor_scout
printf '{"path":"./skills/code_refactor_scout","language":"go","target":"improve-codebase-architecture","min_score":70,"max_results":8}' | FOXCTL_STORAGE_ROOT=/tmp/foxctl-refactor-scout-storage /tmp/code_refactor_scout
```

## Done Criteria

- The target-lens module is extracted and behavior-compatible.
- Dead-code, Elixir slop, snapshot diff, and co-change code have smaller
  behavior-focused modules with clearer interfaces.
- Callers exercise the same public seams as before.
- Tests prove behavior through public or package-level interfaces, not fragile
  implementation details.
- No new dependencies or broad architecture changes were introduced.
- Focused tests, docs checks, and diff hygiene pass, or unrelated pre-existing
  failures are documented with exact commands.
- Final response includes changed files, verification results, residual risks,
  and confidence score.

## Stop Conditions

- Stop before changing public JSON fields, CLI flags, skill names, rule IDs,
  evidence field names, repoindex schema, or storage layout.
- Stop before adding dependencies.
- Stop before refactoring `internal/rlm/runtime/helper_factory_tools.go` or
  `internal/rlm/runtime/helperpipeline/`.
- Stop if a proposed module is shallow by the deletion test: if deleting it
  would make complexity vanish instead of concentrating behavior, do not add it.
- Stop if verification cannot run and report the exact command and blocker.
- Stop after three failed attempts at the same failing test or check.
