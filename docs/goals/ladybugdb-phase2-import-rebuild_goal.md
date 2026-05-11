# Goal: LadybugDB Phase 2 Import Rebuild Proof

## Goal

Implement Phase 2 of
`docs/plans/features/ladybugdb-graph-projection-spike.md`: a CLI-only
LadybugDB import/rebuild proof that can rebuild a disposable Ladybug projection
from the Phase 1 export artifact.

This goal does not add a foxctl command, skill, daemon, runtime query interface,
Go Ladybug binding, cgo build path, or default test dependency on Ladybug. It
only makes the import/rebuild proof durable enough to support Phase 3 query
proof scripts.

## Context

- Phase 0 proof: `docs/research/ladybugdb-phase0-cli-gate.md`
- Phase 1 goal: `docs/goals/ladybugdb-repoindex-export-projection_goal.md`
- Plan: `docs/plans/features/ladybugdb-graph-projection-spike.md`
- Export implementation:
  - `internal/intelligence/indexing/repoindex/ladybug_export.go`
  - `internal/intelligence/indexing/repoindex/ladybug_export_writer.go`
  - `internal/intelligence/indexing/repoindex/ladybug_export_test.go`

Current contract:

- `nodes_graph.csv` is the strict node import file.
- Relationship CSVs are typed by edge kind and use:
  `src_id,dst_id,weight,source,meta_json`.
- Import order is nodes first, then relationships.
- The projection is disposable and rebuildable from canonical export files.
- Phase 3 query scripts must not start until this rebuild path works.

## Constraints

- Keep Phase 2 CLI-only.
- Do not add a Go dependency, cgo requirement, `liblbug`, `system_ladybug`, or
  Ladybug Go binding.
- Do not add a foxctl CLI command, skill, daemon, or runtime interface.
- Do not change canonical repoindex node kinds or edge kinds.
- Do not add `NodeRepo`; `repo` remains an export-only `nodes_graph.csv` row.
- Do not require Ladybug for default `go test`; any Ladybug execution must be
  gated behind `FOXCTL_LADYBUG_TEST=1`.
- Do not delete or rewrite outside the explicit target Ladybug database path.
- Keep scripts deterministic and explicit about `LBUG_BIN`, `EXPORT_DIR`,
  `DB_PATH`, and `HOME`.
- Preserve default build cleanliness: no cgo, no native library, no global
  install requirement.

## Milestones

### Milestone 1: Rebuild Script

Done when:

- A small script exists that accepts or reads:
  - `LBUG_BIN`
  - `EXPORT_DIR`
  - `DB_PATH`
  - optional `LADYBUG_HOME`
- The script removes only the explicit target database path before rebuilding.
- The script creates the strict `GraphNode` schema matching `nodes_graph.csv`.
- The script creates typed relationship tables for supported exported edge CSVs.
- The script copies `nodes_graph.csv` before relationship CSVs.
- Missing relationship CSVs are skipped explicitly.
- The script can run non-interactively through the `lbug` CLI.

### Milestone 2: Gated Integration Test

Done when:

- A skipped-by-default test runs only when `FOXCTL_LADYBUG_TEST=1`.
- The test locates `lbug` through `LBUG_BIN` or `PATH`.
- The test builds a synthetic Phase 1 export artifact through repoindex export
  code, writes it to a temp export directory, runs the rebuild script, reopens
  the database read-only, and verifies:
  - node count
  - representative typed relationship count
  - bounded traversal or shortest direct call traversal
- The test does not require Ladybug in default package tests.

### Milestone 3: Phase 2 Proof Note

Done when:

- A research note records:
  - exact `lbug` version
  - rebuild command
  - schema/import behavior
  - read-only verification query output
  - any failure modes
- The spike plan links to the Phase 2 proof note.

### Milestone 4: Review And Verification

Done when:

- `gofmt -w <touched-go-files>` passes for touched Go files.
- `go test -count=1 ./internal/intelligence/indexing/repoindex` passes without
  requiring Ladybug.
- If `lbug` is available, the gated Ladybug test passes with
  `FOXCTL_LADYBUG_TEST=1`.
- `make check-doc-links` passes.
- `git diff --check` passes.

## Done Criteria

- A disposable Ladybug database can be deleted and rebuilt from a Phase 1 export
  directory alone.
- Node import and typed relationship import are proven through `lbug`.
- Read-only query after rebuild is proven.
- Default tests remain Ladybug-free.
- No runtime integration, command surface, Go binding, cgo requirement, or
  canonical repoindex schema change is introduced.

## Stop Conditions

- Stop before adding a dependency, cgo requirement, or Ladybug Go binding.
- Stop before adding a foxctl user-facing command or runtime query interface.
- Stop if the rebuild script needs global filesystem writes or global install
  assumptions.
- Stop after 3 failed attempts at the same verification failure and summarize
  the blocker with exact command output.
