# LadybugDB Phase 2 Import Rebuild Proof

Status: passed on 2026-05-11

This note records the CLI-only import/rebuild proof for
[docs/plans/features/ladybugdb-graph-projection-spike.md](../plans/features/ladybugdb-graph-projection-spike.md).

Phase 2 stayed outside foxctl runtime code. It used the Phase 1 export shape
(`nodes_graph.csv` plus typed `edges_*.csv` files), the standalone `lbug` CLI,
and the checked-in rebuild script:

```text
scripts/ladybug_rebuild_projection.sh
```

## Environment

- Machine: Darwin arm64
- CLI: `Lbug 0.16.1`
- `lbug` binary used for proof:
  `/private/tmp/ladybug-phase0/bin/lbug`
- Disposable export directory:
  `/private/tmp/ladybug-phase0/fixtures`
- Disposable DB path:
  `/private/tmp/ladybug-phase2/proof.lbug`
- Isolated Ladybug home:
  `/private/tmp/ladybug-phase2/home`

## Rebuild Command

```bash
scripts/ladybug_rebuild_projection.sh \
  --lbug-bin /private/tmp/ladybug-phase0/bin/lbug \
  --export-dir /private/tmp/ladybug-phase0/fixtures \
  --db-path /private/tmp/ladybug-phase2/proof.lbug \
  --home /private/tmp/ladybug-phase2/home
```

Relevant output:

```text
ladybug_rebuild_projection: skip missing edges_imports.csv
ladybug_rebuild_projection: skip missing edges_uses_symbol.csv
ladybug_rebuild_projection: skip missing edges_refers_to.csv
ladybug_rebuild_projection: skip missing edges_implements.csv
...
mode set as json
[
{"result":"Table GraphNode has been created."}
]
[
{"result":"Table Contains has been created."}
]
[
{"result":"Table Calls has been created."}
]
[
{"result":"6 tuples have been copied to the GraphNode table."}
]
[
{"result":"5 tuples have been copied to the Contains table."}
]
[
{"result":"1 tuples have been copied to the Calls table."}
]
[
{"node_count":6}
]
```

The script creates all known relationship tables up front and copies only
relationship CSVs present in the export directory. Missing optional edge files
are logged and skipped. Unknown `edges_*.csv` files fail before `lbug` runs.

## Read-Only Verification

Command:

```bash
HOME=/private/tmp/ladybug-phase2/home \
  /private/tmp/ladybug-phase0/bin/lbug \
  /private/tmp/ladybug-phase2/proof.lbug -r -b -s \
  < /private/tmp/ladybug-phase0/query_shortest_call.cypher
```

Output:

```text
mode set as json
[
{"path_len":1,"node_ids":["repo-fixture::sym:go:example:A","repo-fixture::sym:go:example:B"]}
]
```

## Automated Verification

Default package tests stay Ladybug-free:

```bash
go test -count=1 ./internal/intelligence/indexing/repoindex
```

Gated CLI integration test:

```bash
FOXCTL_LADYBUG_TEST=1 \
LBUG_BIN=/private/tmp/ladybug-phase0/bin/lbug \
go test -count=1 ./internal/intelligence/indexing/repoindex \
  -run TestLadybugRebuildScriptRoundTrip
```

Both commands passed on 2026-05-11.

## Failure Modes And Guardrails

- The rebuild script requires `meta.json` and `nodes_graph.csv`.
- The rebuild script removes only the explicit `DB_PATH`.
- The rebuild script uses a hardcoded relationship CSV allowlist. Unknown
  `edges_*.csv` files fail instead of being silently ignored.
- Default `go test` skips actual `lbug` execution unless
  `FOXCTL_LADYBUG_TEST=1` is set.
- `LADYBUG_HOME` or `--home` should point at a temp directory in sandboxed
  contexts because `lbug` creates `~/.lbdb`.

## Phase 2 Decision

Phase 2 passed:

- a disposable Ladybug database can be rebuilt from export files alone
- node import runs before relationship import
- typed relationship imports work for present edge CSVs
- read-only query mode works after the rebuild process exits
- no Go dependency, cgo setup, Go binding, foxctl command, skill, daemon, or
  runtime interface was added

Phase 3 can now focus on query-proof scripts for `trace_path`,
`smart_context`, and `blast_radius`.
