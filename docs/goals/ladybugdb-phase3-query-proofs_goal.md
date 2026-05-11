# Goal: LadybugDB Phase 3 Query Proof Scripts

## Goal

Implement Phase 3 of
`docs/plans/features/ladybugdb-graph-projection-spike.md`: CLI/script proofs
for `trace_path`, `smart_context`, and `blast_radius` against a rebuilt
Ladybug projection.

This goal does not add a foxctl command, skill, daemon, runtime interface,
general graph abstraction, Go Ladybug binding, or cgo build path. It only
creates proof scripts and gated tests that decide whether Ladybug traversal is
meaningfully better before Phase 4 evaluation.

## Context

- Phase 0 proof: `docs/research/ladybugdb-phase0-cli-gate.md`
- Phase 2 proof: `docs/research/ladybugdb-phase2-import-rebuild.md`
- Plan: `docs/plans/features/ladybugdb-graph-projection-spike.md`
- Rebuild script: `scripts/ladybug_rebuild_projection.sh`
- Export implementation:
  - `internal/intelligence/indexing/repoindex/ladybug_export.go`
  - `internal/intelligence/indexing/repoindex/ladybug_export_test.go`

## Constraints

- Keep Phase 3 CLI-only and proof-script-only.
- Do not add Go dependencies, cgo, `liblbug`, `system_ladybug`, or Ladybug Go
  bindings.
- Do not add a foxctl user-facing command, skill, daemon, or runtime query
  interface.
- Do not rename or change canonical repoindex nodes/edges.
- Do not require Ladybug for default tests; actual `lbug` execution must stay
  behind `FOXCTL_LADYBUG_TEST=1`.
- Keep query depth bounded and inputs explicit.
- Output must be JSON mode output from `lbug`, suitable for transcript and
  golden comparison.

## Milestones

### Milestone 1: Proof Scripts

Done when:

- `trace_path` proof script resolves two projected IDs through bounded shortest
  path traversal and returns path length plus node IDs.
- `smart_context` proof script returns definition/local neighborhood facts
  grouped by explicit edge type and source plane.
- `blast_radius` proof script returns bounded raw graph reasons for candidates
  affected by a changed node.
- Scripts accept explicit `LBUG_BIN`, `DB_PATH`, `LADYBUG_HOME`, and IDs through
  flags or env.
- Scripts reject unsafe or missing required inputs before invoking `lbug`.

### Milestone 2: Gated Query Proof Test

Done when:

- A skipped-by-default test runs only with `FOXCTL_LADYBUG_TEST=1`.
- The test rebuilds a disposable projection from a synthetic Phase 1 export,
  runs all three proof scripts, and verifies stable JSON output fragments.
- Default package tests remain Ladybug-free.

### Milestone 3: Phase 3 Proof Note

Done when:

- A research note records:
  - exact `lbug` version
  - rebuild command
  - script commands and outputs for all three query proofs
  - fallback notes mapping each proof back to existing repoindex/contextplane
    behavior
  - whether at least two query paths look meaningfully better
- The spike plan links to the Phase 3 proof note.

### Milestone 4: Verification

Done when:

- `gofmt -w <touched-go-files>` passes for touched Go files.
- `go test -count=1 ./internal/intelligence/indexing/repoindex` passes without
  requiring Ladybug.
- If `lbug` is available, the gated Ladybug query proof test passes with
  `FOXCTL_LADYBUG_TEST=1`.
- `make check-doc-links` passes.
- `git diff --check` passes.

## Done Criteria

- All three planned query proofs have scripts and gated verification.
- Outputs are JSON-mode and suitable for golden/transcript comparison.
- Phase 4 has enough evidence to decide whether to adopt, stop, or keep only
  export artifacts.
- No runtime integration, command surface, Go binding, cgo requirement, or
  canonical repoindex schema change is introduced.

## Stop Conditions

- Stop before adding a general `GraphQueryEngine` or runtime interface.
- Stop before adding a foxctl command or skill.
- Stop before adding a dependency, cgo requirement, or Ladybug Go binding.
- Stop if proof scripts need unbounded graph traversal.
- Stop after 3 failed attempts at the same verification failure and summarize
  the blocker with exact command output.
