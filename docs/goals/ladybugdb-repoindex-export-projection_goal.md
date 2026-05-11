# Goal: LadybugDB Repoindex Export Projection Phase 1

## Goal

Implement Phase 1 of `docs/plans/features/ladybugdb-graph-projection-spike.md`:
a dependency-free repoindex export projection that emits deterministic typed CSV
files and metadata from canonical repoindex data.

This goal does not implement Ladybug import, schema, query, CLI subprocess, Go
binding, or runtime integration. It only implements the repoindex export
projection contract that can later feed a Ladybug integration after the `lbug`
CLI gate passes.

## Context

- Plan: `docs/plans/features/ladybugdb-graph-projection-spike.md`
- Project language: `CONTEXT.md`
- Topology rule: `docs/architecture/package-topology.md`
- Primary implementation area:
  - `internal/intelligence/indexing/repoindex/types.go`
  - `internal/intelligence/indexing/repoindex/store.go`
  - new repoindex-local exporter files in `internal/intelligence/indexing/repoindex`
- Existing repoindex behavior to preserve:
  - repoindex owns canonical node and edge types
  - query expansion is capped and must not be used as a full-export source
  - no new canonical repo node kind is needed for Phase 1
- Current plan decisions:
  - Phase 1 is a **repoindex export projection**, not a Ladybug integration.
  - Use a **repoindex export snapshot** for full, ordered export reads.
  - Use typed CSV files, omitting empty node-kind and edge-type files.
  - `Repo` is an export-only row.
  - `repo_root_identity = repo_key`.
  - Do not include absolute source store or workspace paths in deterministic
    projection artifacts.
  - Canonicalize JSON fields; invalid repoindex metadata fails the export.
  - Inject export time; exporter core must not call the system clock.
  - Build an in-memory artifact before filesystem writing.
  - Writer uses replace-all semantics for only the target export directory.
  - Co-change edges are passively exported only if already present.
  - No Ladybug schema/import/query placeholders until Phase 0 passes.

## Constraints

- Keep the implementation small and local to the repoindex package unless a
  concrete test proves a narrower helper belongs elsewhere.
- Do not create a new top-level `internal/*` package and do not use
  `internal/v2`.
- Do not add dependencies.
- Do not modify `go.mod` or introduce cgo, Ladybug, `lbug`, `liblbug`,
  `system_ladybug`, or sqlite build-tag requirements.
- Do not add a foxctl CLI command or skill in this goal. If future user-facing
  work is needed, it must preserve foxctl envelope stdout discipline.
- Do not add placeholder Ladybug schema, import, rebuild, or query files.
- Do not add `NodeRepo` or otherwise change canonical repoindex node kinds for
  this goal.
- Do not infer or build co-change edges in the exporter; export only stored
  edges.
- Do not read the system clock in export-building core logic.
- Do not include absolute local store paths or workspace paths in exported
  `meta.json` or `nodes_graph.csv`.
- Preserve public behavior of repoindex search, expand, DAG grep, build, and
  existing tests.
- Preserve AGENTS.md invariants: canonical envelope fields, WASI
  `network:"none"` for skills, no keyword heuristics for routing/classification,
  and docs link hygiene for markdown changes.
- Work with existing dirty/untracked files; do not revert user changes.
- Stop after 3 failed attempts at the same verification failure and summarize
  the blocker with the exact command and output.

## Milestones

### Milestone 1: Export Snapshot Contract

Done when:

- A read-only repoindex export snapshot type exists in the repoindex package.
- Snapshot data includes repo key, `IndexMeta`, file states, all package/file/
  symbol/concept nodes, and all stored edges needed for export.
- Full edge export is read through an uncapped ordered store method or snapshot
  method, not by repeatedly calling capped query expansion APIs.
- Snapshot ordering is deterministic.
- Tests prove full stored edges can be listed without per-node query caps.

### Milestone 2: In-Memory Export Artifact

Done when:

- Export building accepts a snapshot and injected export time.
- Export building returns an in-memory artifact representing `meta.json` and
  CSV files.
- `nodes_graph.csv` uses one strict graph-node shape:
  `id,kind,repo_key,repo_root_identity,pkg,file,name,signature,span_start,span_end,exported,doc,summary,hash,updated_at,meta_json`.
- Relationship CSVs use:
  `src_id,dst_id,weight,source,meta_json`.
- Empty edge-type files are omitted.
- `meta_json` is canonical compact JSON when non-empty.
- Invalid JSON metadata fails export with a clear error.
- `repo_root_identity` is exactly the repo key.
- Export-only repo-to-package containment, if present, is marked with
  `source=export_projection`; canonical repoindex edges use `source=repoindex`.

### Milestone 3: Filesystem Writer

Done when:

- A writer writes an export artifact to an explicit caller-supplied directory.
- The writer uses replace-all semantics for only the target export directory.
- Stale CSV files from a previous export are removed.
- Writer behavior is deterministic across repeated writes.
- The default output layout helper, if added, computes
  `.foxctl/projections/ladybug/<repo-key>/export/` without writing there by
  default.

### Milestone 4: Behavior-Focused Tests

Done when:

- Tests use a synthetic export snapshot, not a temporary parsed repository.
- Tests cover:
  - exact artifact grouping by graph nodes and edge type
  - exact CSV headers and rows
  - omission of empty files
  - canonical JSON output from intentionally unordered input metadata
  - invalid JSON failure
  - fixed injected export time
  - `repo_root_identity = repo_key`
  - absence of absolute source store/workspace paths from exported files
  - passive export of a stored `CO_CHANGES_WITH` edge
  - writer-boundary golden output from actual files
  - replace-all removal of a pre-existing stale CSV
- Tests assert behavior and artifact contracts, not private helper call shapes.
- Golden fixtures stay small enough that failures are readable.

### Milestone 5: Review and Verification

Done when:

- Go files are formatted.
- Markdown docs still pass link checking.
- The implementation is reviewed for sprawl:
  - no speculative graph engine abstraction
  - no command/skill surface
  - no new dependencies
  - no broad package movement
  - no Ladybug placeholder artifacts
- Final self-review lists changed files, protected behaviors, verification
  results, residual risks, and confidence score.

## Verification

Run focused verification after each milestone:

```bash
gofmt -w <touched-go-files>
go test -count=1 ./internal/intelligence/indexing/repoindex
```

Run the final gate before marking the goal complete:

```bash
go test -count=1 ./internal/intelligence/indexing/repoindex
make check-doc-links
git diff --check
```

If implementation touches command, skill, or broader query surfaces despite the
constraints, stop and ask for direction before continuing.

## Done Criteria

- Phase 1 exporter behavior from the plan is implemented in the repoindex
  package.
- The export artifact is deterministic and portable.
- The writer emits actual files with replace-all semantics.
- Tests protect the meaningful export contract and do not chase coverage for
  private helpers.
- No Ladybug integration artifacts, CLI/skill surfaces, dependencies, cgo, or
  canonical repoindex node-kind changes are introduced.
- Focused repoindex tests, docs link check, and diff whitespace check pass, or
  any unrelated pre-existing failure is recorded with exact command output.

## Stop Conditions

- Stop before adding a dependency, cgo requirement, or Ladybug Go binding.
- Stop before creating Ladybug schema/import/query scripts.
- Stop before adding a user-facing CLI command or skill.
- Stop before adding a new top-level internal package.
- Stop before changing canonical repoindex node kinds or schema unless the plan
  is explicitly revised.
- Stop after 3 failed attempts at the same failing check.
- Stop if verification cannot run and report the exact command and reason.
