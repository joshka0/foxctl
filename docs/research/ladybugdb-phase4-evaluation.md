# LadybugDB Phase 4 Evaluation

Status: evaluated on 2026-05-11

This note closes the CLI-only spike described in
[docs/plans/features/ladybugdb-graph-projection-spike.md](../plans/features/ladybugdb-graph-projection-spike.md).

Phase 4 does not add runtime integration. It decides whether the Phase 0-3
evidence justifies a foxctl command, graph runtime interface, daemon, Go
binding, or cgo path.

## Evidence Reviewed

- Phase 0 CLI gate:
  [LadybugDB Phase 0 CLI Gate](ladybugdb-phase0-cli-gate.md)
- Phase 1 export projection:
  [LadybugDB Repoindex Export Projection Phase 1](../goals/ladybugdb-repoindex-export-projection_goal.md)
- Phase 2 import/rebuild proof:
  [LadybugDB Phase 2 Import Rebuild Proof](ladybugdb-phase2-import-rebuild.md)
- Phase 3 query proofs:
  [LadybugDB Phase 3 Query Proofs](ladybugdb-phase3-query-proofs.md)

## Evaluation

| Area | Result | Notes |
| --- | --- | --- |
| Code simplicity | Good for proof scope | The implementation stays in `repoindex` plus standalone scripts. There is no graph engine abstraction, command surface, daemon, or binding. |
| Query expressiveness | Strong for `trace_path`, useful for `smart_context`, promising for `blast_radius` | Bounded path and grouped neighborhood queries are easier to express in Cypher than with app-level traversal. |
| Speed on this repo | Not proven | The proof used synthetic export fixtures and gated CLI tests. There is no committed user-facing export command or real-repo timing harness yet. |
| Projection rebuild cost | Acceptable for proof scope | Phase 2 rebuilt a disposable strict schema from CSV files and reopened read-only. Larger repo cost remains unmeasured. |
| Debuggability and Explorer usefulness | Good | CSV files, schema, and JSON-mode query output are inspectable without touching canonical storage. |
| CLI/import friction | Manageable | `LADYBUG_HOME` isolation and CSV import order are explicit. Unknown edge CSVs fail before import. |
| Concurrency sharp edges | Contained | The model is rebuild-then-query; writer exits before read-only scripts run. No pool, daemon, or mixed writer/reader design was added. |
| Testability | Good | Default tests are Ladybug-free. Real `lbug` execution is gated behind `FOXCTL_LADYBUG_TEST=1`. |
| Default build cleanliness | Preserved | No Go dependency, cgo requirement, build tag, `liblbug`, or `system_ladybug` path was introduced. |

## Query Adoption Gate

The plan said to adopt only if at least two proof queries are meaningfully
better.

- `trace_path`: passes the value test. Direct bounded ID-to-ID path explanation
  is clearer through Ladybug.
- `smart_context`: passes for raw graph facts. It groups local structural and
  semantic facts cleanly while leaving ranking and summarization in foxctl.
- `blast_radius`: not yet adoption-ready. It is promising, but the proof is
  synthetic and one variable-length path result showed an output-shaping caveat
  where repeated literal `section` values were empty after the first row.

The proof clears the "worth preserving" bar, but it does not clear the
"integrate into runtime" bar because real-repo speed, output quality, and
operator ergonomics are not measured.

## Decision

Keep:

- the repoindex export snapshot and deterministic Ladybug projection artifact
- the explicit export writer
- the CLI rebuild script
- the three proof query scripts
- the skipped-by-default gated Ladybug tests
- the research notes and plan links

Do not add now:

- foxctl command or skill
- `GraphQueryEngine`, `GraphProjection`, or other runtime abstraction
- daemon/server wrapper
- Ladybug Go binding
- cgo, `liblbug`, `system_ladybug`, or default build dependency
- vector or FTS proof
- contextplane or memory-derived projection rows

## Next Slice If Resumed

The next safe slice is a real-repo measurement goal, not runtime integration:

1. Add or run an internal-only export harness that writes the current repoindex
   snapshot to a disposable projection directory.
2. Rebuild a Ladybug DB from that real export.
3. Run the three proof scripts against real file/symbol IDs.
4. Capture rebuild time, query time, row counts, output readability, and
   fallback behavior.
5. Only then decide whether an experimental user-facing command is justified.

## Final Recommendation

Stop before runtime integration.

Ladybug is useful enough to keep as a disposable graph lens and proof artifact.
It is not yet proven enough to become a foxctl runtime dependency or user-facing
query path.
