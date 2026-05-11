# LadybugDB Phase 3 Query Proofs

Status: passed on 2026-05-11

This note records the CLI-only query proof for
[docs/plans/features/ladybugdb-graph-projection-spike.md](../plans/features/ladybugdb-graph-projection-spike.md).

Phase 3 stayed outside foxctl runtime code. It used a rebuilt Ladybug
projection, read-only `lbug` invocations, and three checked-in proof scripts:

```text
scripts/ladybug_trace_path.sh
scripts/ladybug_smart_context.sh
scripts/ladybug_blast_radius.sh
```

## Environment

- Machine: Darwin arm64
- CLI: `Lbug 0.16.1`
- `lbug` binary used for proof:
  `/private/tmp/ladybug-phase0/bin/lbug`
- Disposable export directory:
  `/private/tmp/ladybug-phase3/export`
- Disposable DB path:
  `/private/tmp/ladybug-phase3/proof.lbug`
- Isolated Ladybug home:
  `/private/tmp/ladybug-phase3/home`

## Rebuild Command

```bash
scripts/ladybug_rebuild_projection.sh \
  --lbug-bin /private/tmp/ladybug-phase0/bin/lbug \
  --export-dir /private/tmp/ladybug-phase3/export \
  --db-path /private/tmp/ladybug-phase3/proof.lbug \
  --home /private/tmp/ladybug-phase3/home
```

Relevant output:

```text
[
{"result":"7 tuples have been copied to the GraphNode table."}
]
[
{"result":"5 tuples have been copied to the Contains table."}
]
[
{"result":"1 tuples have been copied to the Calls table."}
]
[
{"result":"1 tuples have been copied to the DescribedBy table."}
]
[
{"result":"1 tuples have been copied to the CoChangesWith table."}
]
[
{"node_count":7}
]
```

The proof fixture includes one repo, one package, two files, two symbols, one
concept, and representative `CONTAINS`, `CALLS`, `DESCRIBED_BY`, and
`CO_CHANGES_WITH` rows.

## trace_path

Command:

```bash
scripts/ladybug_trace_path.sh \
  --lbug-bin /private/tmp/ladybug-phase0/bin/lbug \
  --db-path /private/tmp/ladybug-phase3/proof.lbug \
  --home /private/tmp/ladybug-phase3/home \
  --src-id repo-fixture::sym:go:example:A \
  --dst-id repo-fixture::sym:go:example:B \
  --max-depth 3
```

Output:

```json
[
{"path_len":1,"node_ids":["repo-fixture::sym:go:example:A","repo-fixture::sym:go:example:B"]}
]
```

Fallback mapping:

- Current repoindex `code/dag_grep` can produce an explanation subgraph around
  a query.
- Arbitrary ID-to-ID path explanation is not the current primary repoindex
  shape.
- This proof is materially clearer for direct bounded path explanation because
  the path query is one graph expression and returns the exact hop list.

## smart_context

Command:

```bash
scripts/ladybug_smart_context.sh \
  --lbug-bin /private/tmp/ladybug-phase0/bin/lbug \
  --db-path /private/tmp/ladybug-phase3/proof.lbug \
  --home /private/tmp/ladybug-phase3/home \
  --node-id repo-fixture::sym:go:example:A
```

Output:

```json
[
{"section":"self","node_id":"repo-fixture::sym:go:example:A","kind":"symbol","pkg":"go:example","file":"internal/a.go","name":"A","signature":"func A()"}
]
[
{"section":"contains_in","edge_type":"CONTAINS","src_id":"repo-fixture::file:go:example:internal/a.go","dst_id":"repo-fixture::sym:go:example:A","source":"repoindex","meta_json":null}
]
[
]
[
{"section":"callees","edge_type":"CALLS","src_id":"repo-fixture::sym:go:example:A","dst_id":"repo-fixture::sym:go:example:B","source":"repoindex","meta_json":"{\"kind\":\"call\"}"}
]
[
]
[
{"section":"docs_concepts","edge_type":"DESCRIBED_BY","src_id":"repo-fixture::sym:go:example:A","dst_id":"repo-fixture::kw:alpha","source":"repoindex","meta_json":null}
]
[
]
```

Fallback mapping:

- Current contextplane behavior still owns ranking, summarization, semantic
  hints, memory, and priors.
- Ladybug is useful here as a local neighborhood fact collector, not as a
  replacement for contextplane policy.
- The proof is simpler for grouped raw graph facts because the script expresses
  each edge family directly and leaves ranking outside Ladybug.

## blast_radius

Command:

```bash
scripts/ladybug_blast_radius.sh \
  --lbug-bin /private/tmp/ladybug-phase0/bin/lbug \
  --db-path /private/tmp/ladybug-phase3/proof.lbug \
  --home /private/tmp/ladybug-phase3/home \
  --node-id repo-fixture::file:go:example:internal/a.go \
  --max-depth 3
```

Output:

```json
[
{"section":"forward_reason","path_len":2,"node_ids":["repo-fixture::file:go:example:internal/a.go","repo-fixture::sym:go:example:A","repo-fixture::kw:alpha"]},
{"section":"","path_len":2,"node_ids":["repo-fixture::file:go:example:internal/a.go","repo-fixture::sym:go:example:A","repo-fixture::sym:go:example:B"]},
{"section":"","path_len":2,"node_ids":["repo-fixture::file:go:example:internal/a.go","repo-fixture::file:go:example:internal/b.go","repo-fixture::sym:go:example:B"]},
{"section":"","path_len":1,"node_ids":["repo-fixture::file:go:example:internal/a.go","repo-fixture::file:go:example:internal/b.go"]},
{"section":"","path_len":1,"node_ids":["repo-fixture::file:go:example:internal/a.go","repo-fixture::sym:go:example:A"]}
]
[
]
[
{"section":"co_change","edge_type":"CO_CHANGES_WITH","src_id":"repo-fixture::file:go:example:internal/a.go","dst_id":"repo-fixture::file:go:example:internal/b.go","source":"repoindex","meta_json":"{\"count\":2,\"source\":\"git\"}"}
]
```

Fallback mapping:

- Existing repoindex reverse expansion and contextplane impact logic can answer
  parts of this with Go-side traversal and ranking.
- Ladybug makes bounded raw graph reasons easy to express, especially when
  structural, semantic, and empirical edges are all projected.
- The proof is promising but less polished than `trace_path` and
  `smart_context`: the variable-length path result emitted an empty repeated
  literal `section` value after the first row, so consumers should rely on
  stable path fields and explicit follow-up edge-type queries.

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
  -run 'TestLadybug(QueryProofScripts|RebuildScriptRoundTrip)'
```

Both commands passed on 2026-05-11.

## Phase 3 Decision

Phase 3 passed as a CLI-only proof:

- `trace_path` is clearly better than app-level traversal for direct bounded
  ID-to-ID path explanation.
- `smart_context` is simpler for grouped raw graph facts, as long as ranking
  and summarization remain in foxctl code.
- `blast_radius` is promising for raw graph reasons but needs real-repo
  measurement and output shaping before it should drive user-facing behavior.

At least two query paths look meaningfully better for proof purposes, but this
does not justify runtime integration yet. Phase 4 should decide whether to keep
only export/proof artifacts or run a separate real-repo measurement slice before
adding any foxctl command or graph runtime interface.
