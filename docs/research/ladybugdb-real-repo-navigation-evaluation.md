# LadybugDB Real Repo Navigation Evaluation

Status: evaluated on 2026-05-11

This note evaluates the Ladybug projection as a navigation surface against the
real foxctl checkout, not the tiny Phase 3 fixture.

## Setup

Repoindex was rebuilt for this worktree with Go, TypeScript, tests, semantic
anchors, Terraform, Kubernetes, and shell sources:

```bash
go run ./cmd/foxctl index repo build \
  --workspace . \
  --go \
  --typescript \
  --include-tests \
  --semantic-anchors \
  --terraform \
  --kubernetes \
  --shell
```

Result:

```text
duration_ms=80624
packages=1473
files=4415
symbols=67522
nodes=46721
edges=229242
languages=go,typescript,terraform,kubernetes,shell
```

The full rebuild cost dominated the workflow. After repoindex was fresh, the
Ladybug export path was small:

```text
snapshot_nodes=46721
snapshot_edges=229242
artifact_files=19
export_size=88M
snapshot_ms=1584
build_export_ms=211
write_ms=51
```

## Import Result

The first real import exposed a real-data bug: `lbug COPY` auto-detection failed
once quoted free-text fields appeared deep in `nodes_graph.csv`, and the CLI
still exited successfully after printing import errors. The rebuild script now
uses explicit CSV options documented by
[Ladybug CSV import](https://docs.ladybugdb.com/import/csv/) and checks the
imported node count against `nodes_graph.csv`.

Successful rebuild:

```text
46722 GraphNode rows
43716 Contains rows
9797 Imports rows
973 UsesSymbol rows
108807 RefersTo rows
65036 Calls rows
81 HasKeyword rows
203 TouchesResource rows
107 Enforces rows
36 ProtectsAgainst rows
24 VerifiedBy rows
12 DescribedBy rows
3 DecidedBy rows
187 ImplementsProtocol rows
227 ParticipatesIn rows
rebuild wall time: 1.819s
```

The extra graph node is the export-only repo row.

## Navigation Probes

### Find A File's Symbols

A direct Ladybug query over `GraphNode.file` returned the symbol set for
`internal/intelligence/indexing/repoindex/ladybug_export.go`, including
`BuildLadybugExport`, `buildLadybugGraphNodeCSV`, `buildLadybugEdgeCSV`,
`canonicalLadybugJSON`, and the exported artifact types.

Verdict: useful once the user or agent already has a file path. It is not a
replacement for fuzzy search or semantic search.

### Trace Path

Query:

```bash
scripts/ladybug_trace_path.sh \
  --db-path /private/tmp/ladybug-real-repo/projection.lbug \
  --src-id '...:TestWriteLadybugExportReplaceAll' \
  --dst-id '...:WriteLadybugExport' \
  --max-depth 4
```

Result:

```json
[
{"path_len":1,"node_ids":["...:TestWriteLadybugExportReplaceAll","...:WriteLadybugExport"],"node_names":["TestWriteLadybugExportReplaceAll","WriteLadybugExport"],"node_files":["internal/intelligence/indexing/repoindex/ladybug_export_test.go","internal/intelligence/indexing/repoindex/ladybug_export_writer.go"],"node_span_starts":["131","13"],"node_span_ends":["160","45"]}
]
```

Wall time was about 0.13-0.14s in repeated samples.

Verdict: strong for exact ID-to-ID navigation. This is the clearest win:
shortest-path traversal is natural in Ladybug and immediately answers "does
this test touch this implementation?"

### Smart Context

Query:

```bash
scripts/ladybug_smart_context.sh \
  --db-path /private/tmp/ladybug-real-repo/projection.lbug \
  --node-id '...:BuildLadybugExport' \
  --limit 50
```

Result shape:

- self node with package, file, name, signature, spans, doc, and summary
- containing file
- callees with source and destination names, files, and spans:
  `buildLadybugMeta`, `groupLadybugEdges`, `buildLadybugEdgeCSV`,
  `buildLadybugGraphNodeCSV`
- callers: export tests, rebuild-script tests, and query-proof tests

Wall time was about 0.177s.

Verdict: good as a graph neighborhood view for an already-resolved symbol. It
is more compact than raw repoindex expansion for callers/callees, but current
repoindex CLI output still carries better line-opening affordances. The script
now caps each relationship section with `--limit` so high-degree helpers do not
produce large unbounded outputs.

### Blast Radius

Query:

```bash
scripts/ladybug_blast_radius.sh \
  --db-path /private/tmp/ladybug-real-repo/projection.lbug \
  --node-id '...:internal/intelligence/indexing/repoindex/ladybug_export.go' \
  --max-depth 2
```

Result shape:

- all symbols directly contained by the file
- second-hop symbols reached through calls from contained symbols
- node names, files, spans, and path lengths
- no relationship labels inside recursive paths
- no ranking or grouping by changed surface

Wall time was about 0.19-0.22s in repeated bounded samples.

Verdict: useful as a bounded graph expansion proof. It is still not yet a good
navigation product. It needs query shaping, direction controls, ranking outside
Ladybug, and edge-label reconstruction for recursive paths.

## Detailed Script Drilldown

Follow-up samples on the same real-repo projection exercised the scripts on
small, doc-linked, file-level, and high-degree nodes.

`smart_context` on `BuildLadybugExport` returned one self row, one containing
file row, four callees, and seven callers. That is a useful neighborhood view:
it shows the implementation helpers and the tests that exercise the export
builder.

`smart_context` on `NormalizeDAGGrepRequest` returned a doc concept edge to
`docs/plans/features/semantic-code-anchors.md#Remaining Work`, plus the direct
callee `NormalizeExpandOptions` and two callers. This confirms doc anchor edges
survive the projection and remain queryable from implementation symbols.

The high-degree stress case was `internal/platform/errors.Ignore`, which has
279 incoming call edges. Before capping, `smart_context` returned 152KB. With
the default per-section limit of 50, it returned 28KB. With `--limit 10`, it
returned 6.8KB. The limit is necessary if this script ever becomes an agent
tool surface.

Trace probes were readable:

- symbol to symbol:
  `TestWriteLadybugExportReplaceAll -> WriteLadybugExport` at path length 1
- file to symbol:
  `ladybug_export_test.go -> TestWriteLadybugExportReplaceAll -> WriteLadybugExport`
  at path length 2
- implementation to helper:
  `BuildLadybugExport -> buildLadybugGraphNodeCSV` at path length 1

Blast probes were also bounded and readable:

- `ladybug_export.go`, depth 2, limit 12: six one-hop rows and six two-hop rows
- `BuildLadybugExport`, depth 3, limit 12: twelve forward rows and seven
  incoming-call rows

Repeated local timing samples against the 35MB projection were:

```text
smart_context BuildLadybugExport: 0.15s, 0.16s after warmup
blast_radius BuildLadybugExport depth 3 limit 12: 0.20s-0.22s
blast_radius ladybug_export.go depth 2 limit 12: 0.19s
trace_path test to writer: 0.13s-0.14s
```

Path detail limitation: Ladybug returned `label(r)` for a concrete relationship
match, but `label(e)` failed on a recursive path relationship variable. The
current trace and blast scripts therefore show path nodes and spans, but not
the relationship labels between adjacent path nodes. A future user-facing
navigator should reconstruct edge labels after path discovery, or emit
fixed-depth queries that can keep each relationship variable concrete.

## Baseline Comparison

`foxctl index repo search --query BuildLadybugExport` found the target symbol
with file, line hint, signature, docs, and stable ID. `foxctl index repo expand`
returned a typed one-hop graph with line hints and edge types.

Repoindex remains better for:

- first-hop discovery from text
- line-oriented navigation
- command envelopes
- bounded, user-facing output
- agent-friendly source anchors

Ladybug is better for:

- exact shortest-path traversal
- fast ad hoc graph joins after IDs are known
- exploring multiple edge families in one graph query
- disposable local experiments without changing canonical storage

## Verdict

As a way to navigate through the repo, Ladybug is a good graph microscope, not a
complete navigation interface.

It works well when an agent already has stable repoindex node IDs or file paths
and wants to answer graph questions such as:

- "How are these two symbols connected?"
- "What calls this function and what does it call?"
- "Which symbol, file, concept, or doc nodes sit within two graph hops?"

It works poorly as the first tool in the loop because it has no fuzzy resolver,
no ranking, no summarization, no line-opening affordance, and no envelope-native
foxctl command surface.

The right next product shape is not a general graph runtime. It is a narrow
experimental navigator:

1. resolve query text or file path through repoindex search
2. run one Ladybug graph query against the resolved IDs
3. render compact rows with names, files, line hints, direct edge types, and
   path reasons
4. keep ranking and summarization in foxctl
5. fall back to repoindex expansion when the projection is absent or stale

Do not promote `blast_radius` until it has ranking and recursive-path edge
labels.
