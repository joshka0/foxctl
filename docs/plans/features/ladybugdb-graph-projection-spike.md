# LadybugDB Graph Projection Spike Plan

Status: proposed spike
Owner: foxctl
Last updated: 2026-05-11

## Goal

Evaluate LadybugDB as a disposable graph projection layer for repoindex,
semantic backlinks, and code-memory traversal.

The desired posture is:

```text
repoindex/Turso-family canonical stores = source of truth
LadybugDB = disposable graph lens for traversal proofs
```

This spike does not try to replace repoindex, memory storage, vector search,
or FTS. It asks a narrower question:

> Do `trace_path`, `smart_context`, or `blast_radius` become meaningfully
> simpler, clearer, faster, or easier to explain when run against a LadybugDB
> projection built from canonical foxctl data?

## Decision

Proceed with Phase 0 and Phase 1 only.

Do not add LadybugDB as a Go dependency. Do not create a runtime
`GraphQueryEngine` interface yet. Do not use Ladybug vector or FTS features in
the spike. Do not dual-write.

Use the Ladybug CLI and import path first:

```text
Phase 0/1:
  use Ladybug CLI only

Later:
  maybe call Ladybug via CLI subprocess

Much later:
  consider Go binding only behind explicit build tags
```

The hard gate is:

```text
Proceed only if Ladybug CLI/import works without any Go cgo dependency.
```

## Non-Goals

- No Ladybug dependency in default `go.mod`.
- No cgo, `CGO_LDFLAGS`, `liblbug`, or `system_ladybug` requirement in default
  builds.
- No Ladybug-backed default repoindex path.
- No Ladybug vector index proof.
- No Ladybug FTS proof.
- No daemon/server wrapper until the CLI proof is valuable.
- No broad repoindex rewrite.
- No generic graph abstraction before the proof queries pass.

## Existing System Fit

The repo already has a graph-shaped canonical substrate:

- repoindex node and edge types live in
  [internal/intelligence/indexing/repoindex/types.go](../../../internal/intelligence/indexing/repoindex/types.go).
- repoindex storage persists `nodes`, `edges`, `node_fts`, `file_state`,
  `pkg_state`, and `symbol_locator` in
  [internal/intelligence/indexing/repoindex/store.go](../../../internal/intelligence/indexing/repoindex/store.go).
- repoindex build materializes a full graph before replacing store contents in
  [internal/intelligence/indexing/repoindex/builder.go](../../../internal/intelligence/indexing/repoindex/builder.go).
- repoindex query and expansion live in
  [internal/intelligence/indexing/repoindex/query.go](../../../internal/intelligence/indexing/repoindex/query.go).
- DAG grep is the current explanation-subgraph query in
  [internal/intelligence/indexing/repoindex/dag_grep.go](../../../internal/intelligence/indexing/repoindex/dag_grep.go).
- retrieval v2 consumes repoindex through narrow search/DAG query shapes in
  [internal/intelligence/retrieval/v2/types.go](../../../internal/intelligence/retrieval/v2/types.go).
- contextplane already combines repoindex, embeddings, memory, co-change, and
  semantic hints in
  [internal/context/contextplane/retrieval.go](../../../internal/context/contextplane/retrieval.go).
- Obsidian graph tooling already materializes graph drafts from repoindex in
  [internal/tooling/tools/obsidian/graph.go](../../../internal/tooling/tools/obsidian/graph.go).

Ladybug should sit beside this stack as a rebuildable projection, not beneath
it as a storage replacement.

## Ladybug Constraints That Shape the Spike

Source notes:

- Ladybug is embedded and schema-first by default. Its docs say schema is
  required before inserts, while newer DDL docs also support open type graphs
  via `CREATE GRAPH mygraph ANY`. For foxctl, use strict schema because the repo
  graph shape should be explicit and inspectable.
- Bulk import is via `COPY FROM`; relationship imports require endpoint nodes
  to already exist.
- The Go binding exists, but its documented install path includes generated
  native libraries, cgo flags, and a `system_ladybug` build tag. That stays out
  of the spike.
- Ladybug allows either one read-write database object or multiple read-only
  database objects for the same DB. Mixing a writer with another reader/writer
  database object is unsafe.
- Algorithm projected graphs are connection-bound. They live until dropped or
  until the connection closes, so async/pool usage can fail if a later query
  lands on a different connection.
- Ladybug FTS currently indexes node-table string properties. Ladybug vector
  indexes currently apply to vectors stored as node table properties. Both are
  distractions for this spike.

Primary external references:

- [Ladybug import docs](https://docs.ladybugdb.com/import/)
- [Ladybug CSV import docs](https://docs.ladybugdb.com/import/csv/)
- [Ladybug DDL docs](https://docs.ladybugdb.com/cypher/data-definition/)
- [Ladybug Neo4j differences](https://docs.ladybugdb.com/cypher/difference/)
- [Ladybug concurrency docs](https://docs.ladybugdb.com/concurrency/)
- [Ladybug algorithm extension docs](https://docs.ladybugdb.com/extensions/algo/)
- [Ladybug Go binding](https://github.com/LadybugDB/go-ladybug)
- [Ladybug FTS extension](https://docs.ladybugdb.com/extensions/full-text-search/)
- [Ladybug vector extension](https://docs.ladybugdb.com/extensions/vector/)

## Phase 0 Gates

Phase 0 is a gate, not a coding phase. Stop if any hard gate fails.

Ladybug CLI must be able to:

- create a local DB from the command line
- create a strict schema
- `COPY` node CSVs
- `COPY` relationship CSVs
- run a bounded path query
- run a shortest path query
- emit query output in a script-friendly form
- exit cleanly with no resident process

Additional gates:

- no Go module dependency is required
- no cgo build tags are required
- projection files can be deleted and rebuilt from scratch
- Explorer/CLI does not need concurrent write access during query proof
- a writer can build the projection, close cleanly, and later query/explorer
  processes can open read-only

The spike proceeds only if all of these are true.

## Projection Shape

Prefer strict, typed tables rather than one generic `nodes.csv` and
`edges.csv`. Ladybug relationship tables are typed, so per-label/per-edge CSVs
should map better to schema and query readability.

Phase 1 uses this typed CSV shape from the start. It should emit only files for
node kinds and edge types that have source rows.

The exporter should read from a dedicated repoindex export snapshot rather than
from query expansion APIs. Expansion is budgeted and per-node capped for
retrieval use; full export needs an ordered, uncapped read of the canonical
repo graph.

Initial export directory:

```text
.foxctl/projections/ladybug/<repo-key>/export/
  meta.json
  nodes_repo.csv
  nodes_package.csv
  nodes_file.csv
  nodes_symbol.csv
  nodes_concept.csv
  edges_contains.csv
  edges_imports.csv
  edges_calls.csv
  edges_refers_to.csv
  edges_uses_symbol.csv
  edges_implements.csv
  edges_tests.csv
  edges_described_by.csv
  edges_verified_by.csv
  edges_enforces.csv
  edges_co_changes_with.csv
```

Only include a CSV when source data exists. Keep output deterministic:

- stable column order
- stable sort order
- stable IDs copied from repoindex
- JSON metadata serialized deterministically
- no absolute paths unless already part of canonical metadata

Phase 1 relationship CSVs use a fixed minimal column shape:

```text
src_id,dst_id,weight,source,meta_json
```

`source` should distinguish canonical repoindex rows from export-only rows such
as optional repo-to-package containment. `meta_json` preserves edge-specific
details as compact deterministic JSON. Do not explode metadata into typed
relationship columns in Phase 1; mark relationship properties as the first
schema expansion point if proof queries need stronger filtering or explanation
fields.

Phase 1 node CSVs should be kind-specific files with mostly shared columns:

```text
id,pkg,file,name,signature,span_start,span_end,exported,doc,summary,hash,updated_at,meta_json
```

`nodes_repo.csv` is the exception because `Repo` is export-only:

```text
id,repo_key,repo_root_identity,head_sha,worktree_dirty,dirty_status_hash,default_ref,default_ref_sha,merge_base_sha,commits_ahead,commits_behind,schema_version,indexed_at,languages_json
```

Do not add custom typed columns for every node family in Phase 1. Preserve
kind-specific details in `meta_json` and treat promoted node properties as a
later schema expansion point if proof queries require them.

JSON export fields must be canonicalized. Empty metadata stays empty. Non-empty
`meta_json` and `languages_json` values should be decoded and re-encoded as
compact JSON so golden files are byte-stable. Invalid repoindex metadata should
fail the export rather than being passed through silently.

## Source Mapping

| Projection | Source | Spike? | Notes |
| --- | --- | --- | --- |
| `Repo` | repoindex `IndexMeta` | yes | export-only row with repo key, root, git snapshot, schema version |
| `Package` | repoindex package nodes | yes | use existing package IDs |
| `File` | repoindex file nodes + `file_state` | yes | path, hash, language/meta, summary |
| `Symbol` | repoindex symbol nodes + `symbol_locator` if needed | yes | name, signature, file, span, exported, doc |
| `Concept` | repoindex concept nodes | yes | semantic anchors and comment-derived concepts |
| `AgentNote` | memorycore/contextplane | later | context enrichment projection only after graph proof is valuable |
| `MemoryClaim` | contextengine/memorycore | later | future context enrichment projection; avoid first-spike scope creep |
| Structural edges | repoindex `edges` | yes | `CONTAINS`, `IMPORTS`, `CALLS`, `REFERS_TO`, etc. |
| Semantic edges | semantic anchor/comment edges | yes | preserve edge plane/type metadata |
| Empirical edges | co-change edges | optional | passively export if already present; do not build or infer them in the exporter |

Contextplane and memory-derived rows are explicitly not part of the Phase 1
base export. If added later, they should be exported as a separate context
enrichment projection that references stable repoindex node IDs rather than
creating a second code identity graph.

The `Repo` row is export-only in Phase 1. Do not add a new canonical repoindex
`NodeKind` just to satisfy the Ladybug projection. If the export needs
repo-to-package containment rows for schema convenience, mark them as
export-projection edges rather than canonical repoindex edges.

Co-change and other empirical edges are passive in Phase 1. The exporter should
write them if they already exist in the canonical repoindex store, but it must
not trigger git-history analysis or infer new empirical edges.

## Operational Model

Use a rebuild-then-query snapshot:

```text
repoindex canonical store
  -> deterministic CSV export
  -> Ladybug CLI creates strict schema
  -> Ladybug CLI imports nodes, then relationships
  -> writer exits and closes DB
  -> query proof opens read-only or uses a single controlled CLI process
```

Avoid this:

```text
import process holds DB open read-write
Explorer opens same DB
query process opens same DB concurrently
```

For graph algorithms, use one process and one connection. Projected graphs are
connection-bound, so do not design the spike around connection pools, async
workers, or daemon fanout.

## Proof Queries

Continue past Phase 1 only if:

```text
trace_path is clearly better than current DAG grep
AND either smart_context or blast_radius is clearly simpler or more explainable
```

### 1. trace_path

Purpose: explain how two files, symbols, or concepts are connected.

Current baseline:

- `code/dag_grep` gives a query-centered explanation subgraph.
- Arbitrary ref-to-ref path explanation is not the primary repoindex query
  shape.

Ladybug proof:

- resolve both refs to projected node IDs
- run bounded shortest or weighted path query
- render each hop with node label, repoindex ID, edge type, and edge metadata

Success criteria:

- query is materially clearer than app-level traversal
- output explains why each hop exists
- bounded query avoids walk explosions
- no Ladybug feature outside graph traversal is needed

### 2. smart_context

Purpose: given a file/symbol/chunk, return local graph neighborhood context.

Current baseline:

- repoindex expansion plus contextplane orchestration merges structure,
  semantic hints, docs, memory, and priors.

Ladybug proof:

- from one node, retrieve definition, containing file/package, callers,
  callees, tests, docs/concepts, and selected neighboring symbols
- leave ranking and summarization in app code

Success criteria:

- one or a few graph queries replace scattered edge lookups
- output groups facts by edge type and source plane
- fallback to repoindex expansion remains straightforward

### 3. blast_radius

Purpose: given a changed file/symbol, find affected code, tests, and docs.

Current baseline:

- reverse edge expansion and contextplane impact logic can already answer parts
  of this, with ranking kept in Go.

Ladybug proof:

- traverse reverse calls/imports/refs, `TESTS`, doc/semantic, and optional
  co-change edges within bounded depth
- return candidates with raw graph reasons, not final ranking policy

Success criteria:

- graph reasons are clearer than the current app-level traversal
- likely tests/docs are visible without custom per-edge plumbing
- ranking can remain outside Ladybug

## Phase Plan

### Phase 0: CLI Capability Gate

Deliverables:

- exact Ladybug version used
- local install command used
- minimal strict schema script
- minimal node and relationship CSV fixtures
- import command transcript
- bounded path query proof
- shortest path query proof
- documented failure modes

Exit:

- proceed only if all Phase 0 gates pass without Go dependency or cgo setup

Result:

- Passed on 2026-05-11 with `Lbug 0.16.1`; see
  [LadybugDB Phase 0 CLI Gate](../../research/ladybugdb-phase0-cli-gate.md).

### Phase 1: Repoindex Export Files

Deliverables:

- deterministic export files under ignored projection output
- per-label node CSVs, omitting empty node-kind files
- per-edge relationship CSVs, omitting empty edge-type files
- `meta.json` with repo key, repoindex schema version, git snapshot, and export
  time
- golden test over a tiny repoindex fixture

Implementation boundary:

- files and scripts only
- no Ladybug Go dependency
- no Ladybug schema, import, rebuild, or query files until the Phase 0 CLI gate
  passes
- no runtime interface
- implement the Phase 1 exporter in the existing `repoindex` package; do not
  create a new top-level internal package or use `internal/v2`
- no user-facing CLI or skill in the first implementation slice
- any later user-facing command must preserve the foxctl envelope contract
  (`stdout` envelopes only, logs elsewhere)
- add a read-only repoindex export snapshot method if needed for deterministic
  uncapped node/edge export
- keep the `Repo` CSV row export-only; do not add a repo node kind to canonical
  repoindex in this slice
- no changes to canonical repoindex schema unless a missing stable export field
  is proven unavoidable

The export builder should not read the system clock. Accept an injected export
time from the caller so `meta.json` is deterministic in golden tests; production
callers can supply the current UTC time at the shell boundary.

Keep deterministic export metadata portable. Do not include absolute local
storage paths such as the repoindex DB path or absolute workspace root in
`meta.json` or `nodes_repo.csv`. Use `repo_root_identity` for a portable root
identity in the projection artifact. In Phase 1, `repo_root_identity` should be
the `repo_key` because that already namespaces every repoindex ID and stays
stable across checkout path changes. If a later user-facing command needs local
debug detail or a friendlier workspace name, include it in the command envelope
metadata instead of the projection artifact.

Split export building from filesystem writing:

- build an in-memory deterministic export artifact from the repoindex export
  snapshot
- write that artifact to an explicit caller-supplied output directory
- provide a helper for the default layout
  `.foxctl/projections/ladybug/<repo-key>/export/`

This keeps path policy and filesystem effects at the edge while preserving the
planned projection layout.

Writing an export should use replace-all semantics for the target export
directory. Because empty node-kind and edge-type files are omitted, selective
overwrite can leave stale CSVs behind. The writer may clear or swap only the
specific projection export directory; it must not delete broader `.foxctl`
state.

The Phase 1 golden fixture should be a synthetic repoindex export snapshot, not
a temporary repository build. Include one export-only repo row, one package, two
files, two symbols, one concept, and representative `CONTAINS`, `IMPORTS`,
`CALLS`, `DESCRIBED_BY`, and optional `CO_CHANGES_WITH` edges. Include
intentionally unordered JSON metadata in the input to prove canonicalization,
use a fixed export time, and assert exact CSV/`meta.json` bytes plus omission of
empty files.

Test at both layers: unit-test the in-memory export artifact for grouping and
canonicalization, then golden-test the filesystem writer by writing to a temp
directory and comparing actual files. Seed a stale CSV in the target directory
before writing so the golden test proves replace-all semantics remove stale
files.

Do not add placeholder Ladybug schema, import, rebuild, or query scripts in
Phase 1. Those are Ladybug integration artifacts and should wait until the
Phase 0 `lbug` CLI gate passes with real syntax and transcripts.

### Phase 2: Ladybug Import Script

Deliverables:

- strict Ladybug schema for first-pass node and relationship tables
- import script that copies nodes before relationships
- rebuild script that deletes the prior projection and recreates it
- read-only query mode after writer closes

Exit:

- projection can be deleted and rebuilt from canonical export alone

### Phase 3: Query Proof Scripts

Deliverables:

- CLI/script proof for `trace_path`
- CLI/script proof for `smart_context`
- CLI/script proof for `blast_radius`
- JSON outputs suitable for golden comparison
- fallback notes mapping each proof back to existing repoindex/contextplane
  behavior

Still do not add a Go runtime interface in this phase unless the proof queries
already pass and the next decision explicitly asks for integration.

### Phase 4: Evaluation and Decision

Evaluate:

- code simplicity
- query expressiveness
- speed on this repo
- projection rebuild cost
- debuggability and Explorer usefulness
- CLI/import friction
- concurrency/connection sharp edges
- testability
- default build cleanliness

Decision:

- adopt only if at least two proof queries are meaningfully better
- otherwise keep the export artifacts if useful and stop before runtime
  integration

## Later Integration Options

Do these only after the CLI proof is accepted.

1. CLI subprocess adapter behind an experimental command.
2. Query-only `GraphProjection` or `GraphQueryEngine` interface.
3. Optional daemon/server wrapper if single-process CLI use is too slow.
4. Go binding behind explicit build tags, only if subprocess cost is proven
   unacceptable.

The first acceptable integration should preserve default behavior:

```text
default foxctl build/test path: no Ladybug
experimental path: opt-in projection query
failure path: repoindex/contextplane fallback
```

## Test Plan

Unit tests:

- exporter emits deterministic rows
- repoindex IDs are preserved
- relationship CSVs sort deterministically
- metadata JSON is stable
- unsupported or missing edge types are skipped explicitly

Golden tests:

- tiny repoindex fixture export
- query-proof JSON output for all three proof queries

Integration tests:

- gated behind an environment variable such as `FOXCTL_LADYBUG_TEST=1`
- skipped by default when Ladybug CLI is unavailable
- rebuild projection from scratch
- import nodes before relationships
- run the three proof queries

Regression tests:

- existing repoindex, DAG grep, smart search, and contextplane tests must pass
  without Ladybug installed
- default `go test ./...` must not require cgo or Ladybug libraries

## Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| cgo/native library friction | CLI-only first; no Go dependency |
| schema churn | strict generated schema and rebuild-only projection |
| concurrent DB access footguns | writer closes before read-only query/explorer |
| projected graph connection lifetime | one process, one connection for algorithm proofs |
| graph query walk explosion | bounded depth, allowed edge sets, explicit limits |
| vector/FTS distraction | exclude from proof scope |
| premature abstraction | no `GraphQueryEngine` until after CLI proofs pass |
| projection drift | include repoindex schema/git metadata; rebuild from canonical store |

## Final Recommendation

Run Phase 0 and Phase 1.

Stop unless the Ladybug CLI can create a strict schema, import typed CSV node and
relationship tables, and run the three graph traversal proofs without adding any
Go dependency or cgo build requirement.

The best outcome is not a new canonical database. The best outcome is:

```text
foxctl repoindex remains canonical
Ladybug becomes a disposable graph lens
agents get trace_path, smart_context, and blast_radius
default builds stay clean
```
