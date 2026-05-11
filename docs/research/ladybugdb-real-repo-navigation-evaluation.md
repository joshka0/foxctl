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
{"path_len":1,"node_ids":["...:TestWriteLadybugExportReplaceAll","...:WriteLadybugExport"]}
]
```

Wall time was about 0.109s.

Verdict: strong for exact ID-to-ID navigation. This is the clearest win:
shortest-path traversal is natural in Ladybug and immediately answers "does
this test touch this implementation?"

### Smart Context

Query:

```bash
scripts/ladybug_smart_context.sh \
  --db-path /private/tmp/ladybug-real-repo/projection.lbug \
  --node-id '...:BuildLadybugExport'
```

Result shape:

- self node with package, file, name, and signature
- containing file
- callees: `buildLadybugMeta`, `groupLadybugEdges`,
  `buildLadybugEdgeCSV`, `buildLadybugGraphNodeCSV`
- callers: export tests, rebuild-script tests, and query-proof tests

Wall time was about 0.177s.

Verdict: good as a graph neighborhood view for an already-resolved symbol. It
is more compact than raw repoindex expansion for callers/callees, but current
repoindex CLI output still carries better line hints and docs.

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
- no ranking, no grouping by changed surface, and no line hints

Wall time was about 0.130s.

Verdict: too noisy in its current script form. It proves traversal works, but it
is not yet a good navigation product. It needs query shaping, direction
controls, ranking outside Ladybug, and tighter result limits.

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
3. render compact rows with names, files, line hints, edge type, and reason
4. keep ranking and summarization in foxctl
5. fall back to repoindex expansion when the projection is absent or stale

Do not promote `blast_radius` until it has ranking and tighter output shaping.
