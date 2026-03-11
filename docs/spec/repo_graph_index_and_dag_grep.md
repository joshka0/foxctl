# Repo Graph Index and DAG Grep Spec

This document is the canonical behavior spec for the repo graph index
(`repoindex`) and the `code/dag_grep` explanation query built on top of it.

It complements, but does not replace:

- [docs/general/repoindex.md](../general/repoindex.md) for user-facing commands
  and operational usage.
- [docs/general/search.md](../general/search.md) for the broader retrieval and
  indexing surface area.

This spec is normative for:

- The repo graph data model.
- Current edge semantics and confidence bands.
- `DAGGrep` request defaults and result semantics.

This spec is descriptive of the current implementation. If code changes the
behavior described here, this document must be updated in the same change.

---

## 1. Goals and Non-Goals

### 1.1 Goals

- Define the canonical graph model used by `agentctl index repo ...`.
- Define the explanation-query contract used by `code/dag_grep`.
- Make structural edges, doc edges, and weights explicit.
- Clarify which behavior is exact and which behavior is heuristic.

### 1.2 Non-Goals

- Defining a generic Cypher- or SQL-level query language for agents.
- Replacing Protocol v1 envelopes.
- Guaranteeing a complete whole-program call graph for every language.
- Making `dag_grep` a shortest-path or all-path graph algorithm.

---

## 2. Repo Graph Model

### 2.1 Storage Scope

The repo graph index is a local, per-workspace graph store backed by SQLite.
Each workspace gets its own database under the repoindex storage directory.

The graph is repository-scoped:

- Every node ID is namespaced by `repo_key`.
- Every edge row is also scoped by `repo_key`.
- Cross-repo edges are not stored in a single shared graph.

### 2.2 Node Kinds

The graph currently supports four node kinds:

- `package`
- `file`
- `symbol`
- `concept`

Definitions:

- `package`: language/package/module grouping unit.
- `file`: a source file in the workspace.
- `symbol`: a language symbol extracted from a file.
- `concept`: synthetic node used for repo rollups and doc-derived concepts.

### 2.3 Node Fields

Every node has:

- `id`: repo-scoped stable identifier.
- `kind`: one of the node kinds above.
- `updated_at`: UTC timestamp of index materialization.

Nodes may also include:

- `pkg`: package identifier for package/file/symbol nodes.
- `file`: relative file path for file/symbol nodes.
- `name`: display name.
- `signature`: symbol signature when available.
- `span_start`, `span_end`: 1-based line range when known.
- `exported`: whether the symbol is exported/public by language rules.
- `doc`: parsed documentation body for symbols.
- `summary`: optional file/symbol/package/repo summary.
- `meta`: JSON metadata blob.
- `hash`: file content hash or symbol body hash.

### 2.4 Node ID Rules

Node IDs are repo-namespaced and MUST follow the pattern:

`<repo_key>::<raw_id>`

Raw ID families are:

- `pkg:<pkg-id>`
- `file:<pkg-id>:<relative-path>`
- `sym:<pkg-id>:<symbol-id>`
- `repo:<repo-name>` for the synthetic repo rollup concept node
- concept-prefixed IDs such as `kw:<term>`, `field:<field>`, `res:<resource>`,
  `event:<event>`

Implications:

- Node identity is stable within a repo key, not globally across repos.
- Symbol identity is only as stable as the underlying extractor key.
- File and symbol IDs are path-sensitive.

### 2.5 Edge Types

Current structural edge types:

- `CONTAINS`
- `IMPORTS`
- `REFERS_TO`
- `CALLS`
- `IMPLEMENTS`
- `EMBEDS`
- `TESTS`

Current doc/comment-derived edge types:

- `HAS_KEYWORD`
- `HAS_OUTPUT_FIELD`
- `TOUCHES_RESOURCE`
- `EMITS_EVENT`
- `DOC_RELATED`
- `DOC_FLOW`

`IMPLEMENTS`, `EMBEDS`, and `TESTS` are part of the canonical edge vocabulary
even if current language coverage is limited.

### 2.6 Edge Semantics and Weights

Weights encode confidence/strength, not cost.
Higher weight means stronger traversal preference.

Current weight bands:

- `1.0`: exact structural edges and synthetic anchor edges.
- `0.9`: heuristic TypeScript `CALLS`.
- `0.85`: heuristic Elixir `REFERS_TO`.
- `0.75`: doc-to-symbol edges (`DOC_RELATED`, `DOC_FLOW`).
- `0.7`: TypeScript `IMPORTS`.
- `0.6`: concept edges from parsed doc index blocks.

Interpretation:

- `1.0` edges are treated as hard graph facts from the current indexer.
- `<1.0` edges are heuristics or soft/documentation-derived relationships.

### 2.7 Build-Time Materialization

Repo graph build is full-replacement, not incremental in-place graph mutation:

1. Read language inputs for enabled languages.
2. Materialize nodes and edges in memory.
3. Resolve pending name-based heuristic edges.
4. Add package rollups, repo rollup, and comment/doc edges.
5. Atomically replace the stored graph.
6. Upsert symbol locator entries.
7. Persist index metadata.

This means the canonical graph for a workspace is the most recent successful
build result, not an append-only history.

### 2.8 Rollups and Synthetic Nodes

The builder may create synthetic nodes and edges beyond direct source entities:

- Package rollup summaries on `package` nodes.
- A repo rollup `concept` node connected to local packages by `CONTAINS`.
- Concept nodes from parsed doc index metadata.

These synthetic nodes are first-class graph nodes and may appear in
`dag_grep` output.

### 2.9 Locator Table

In addition to graph nodes/edges, repoindex persists a symbol locator table.

The locator table is not part of the traversed graph itself. Its role is to:

- Map stable symbol keys to current file/package/span metadata.
- Support symbol relocation and lookup outside pure graph traversal.

---

## 3. Current Language Coverage

### 3.1 Go

Go currently provides:

- `package`, `file`, and `symbol` nodes.
- Exact `IMPORTS` edges from package imports.
- Exact `CONTAINS` edges for package->file and file->symbol.
- Exact `CALLS` edges from typed AST call resolution when the target symbol is
  present in the graph.
- Exact `REFERS_TO` edges from typed identifier/selector resolution when the
  target symbol is present in the graph.

### 3.2 TypeScript

TypeScript currently provides:

- `package`, `file`, and `symbol` nodes.
- `CONTAINS` edges for package->file and file->symbol.
- Heuristic `CALLS` edges resolved by symbol-name matching after extraction.
- `IMPORTS` edges resolved from import strings through the TS package resolver.

TypeScript call edges are not type-checked and MUST be treated as heuristic.

### 3.3 Elixir

Elixir currently provides:

- `package`, `file`, and `symbol` nodes.
- `CONTAINS` edges for package->file and file->symbol.
- Heuristic `REFERS_TO` edges derived from extracted references/calls and
  resolved by name matching.

Elixir package identity is currently directory-based.

### 3.4 Documentation-Derived Edges

For symbol nodes with parsed index metadata in `meta`, the builder may add:

- Concept edges to keyword, resource, event, and output-field concept nodes.
- `DOC_RELATED` edges to referenced symbols.
- `DOC_FLOW` edges to referenced symbols.

Doc-derived symbol targets are only created when name resolution is unique.
Ambiguous references are skipped.

---

## 4. Invariants

The current implementation relies on these invariants:

- `nodes.id` is unique within a repo graph database.
- `edges` are unique on `(src, dst, type, repo_key)`.
- `src` and `dst` must reference valid node IDs in the same repo namespace.
- Every stored edge is directed.
- `dag_grep` may render a layered DAG view, but the underlying graph is not
  required to be acyclic.

The graph is therefore a directed property graph, not a strict DAG.

---

## 5. Search Surface

### 5.1 Text Search

Repoindex search uses SQLite FTS over node text fields.

Indexed search fields are:

- `name`
- `signature`
- `doc`
- `summary`

Search is lexical and scored via BM25.

Search fallback behavior is:

1. raw trimmed query
2. quoted query fallback
3. OR-joined multi-word fallback

This fallback sequence is part of the current query contract because it affects
seed selection for `dag_grep`.

### 5.2 Search Result Semantics

`SearchScored` returns BM25 scores where lower is better.
`DAGGrep` normalizes these scores into a descending relevance score with:

`normalized = 1 / (1 + bm25)`

This normalized score is the seed score used to prioritize traversal.

---

## 6. DAG Grep Contract

### 6.1 Purpose

`DAGGrep` is an explanation-subgraph query over repoindex.

It is intended to answer questions such as:

- "what calls or uses X?"
- "show me the nearby graph around Y"
- "what code and doc relationships surround this topic?"

It is not a generic graph query engine and does not attempt exhaustive graph
enumeration.

### 6.2 Request Fields

`DAGGrepRequest` supports:

- `query`: required text query.
- `mode`: optional query mode hint.
- `k`: number of top seeds to keep.
- `node_kinds`: optional node-kind filter applied to search results.
- `edge_types`: explicit edge types to traverse.
- `direction`: `out` or `in`.
- `depth`: traversal depth limit.
- `budget`: maximum distinct nodes to include.
- `per_node_cap`: maximum fetched edges per expanded node.
- `include_anchors`: whether to add package/file anchor context.

### 6.3 Defaults

If omitted, current defaults are:

- `mode = "hybrid"`
- `k = 10`
- `direction = "out"`
- `depth = 2`
- `budget = 80`
- `per_node_cap = 20`
- `include_anchors = true`

If no edge types are provided by the caller, tool-layer defaults currently map
to the structural edge set.

### 6.4 Mode Semantics

Current `mode` behavior is advisory, not fully implemented:

- `DAGGrep` records the requested mode.
- `ModeUsed` is currently `fts`.
- Non-FTS modes emit a warning that semantic/hybrid modes currently fall back
  to FTS behavior.

So "hybrid" currently means "FTS seed selection plus weighted graph expansion,"
not lexical+vector retrieval.

### 6.5 Seed Selection

Seed selection proceeds as follows:

1. Run scored FTS search with limit `k * 3`.
2. Apply optional node-kind filtering.
3. Normalize BM25 scores.
4. Keep the first `k` filtered results.
5. Sort seeds by descending normalized score.

If zero seeds remain after filtering, the result is empty and traversal does
not run.

### 6.6 Expansion Algorithm

Traversal is a weighted best-first expansion, not plain BFS.

Algorithm:

1. Initialize a max-heap frontier with seed nodes keyed by normalized score.
2. Pop the highest-scoring frontier item.
3. If `depth >= req.depth`, do not expand that node further.
4. Fetch matching outgoing or incoming edges for that node.
5. Add unseen edges to the graph result.
6. For each unseen neighbor, compute next score:

`next_score = current_score * edge_weight * 0.85^(next_depth)`

7. Push the neighbor into the frontier.
8. Stop when the frontier is exhausted or the node budget is reached.

Consequences:

- Higher-weight edges are preferred.
- Closer neighbors are preferred.
- Strong seeds dominate weak seeds.
- The output is a relevance-biased neighborhood, not an exhaustive closure.

### 6.7 Direction Semantics

`direction = "out"` means:

- expansion fetches outgoing edges from the current node
- DAG layering treats `src -> dst` as forward

`direction = "in"` means:

- expansion fetches incoming edges to the current node
- DAG layering inverts edge orientation for traversal/layering purposes

The stored edge direction in the graph is unchanged.

### 6.8 Anchor Semantics

When `include_anchors = true`, `DAGGrep` may add contextual anchor nodes:

- For a `symbol` node, add its containing `file` node and a synthetic
  `file CONTAINS symbol` edge if missing.
- For a `file` node, add its containing `package` node and a synthetic
  `package CONTAINS file` edge if missing.

Anchor insertion is a presentation/context aid.
It does not change seed selection or traversal ordering.

### 6.9 DAG View Semantics

After expansion, `DAGGrep` computes a layered view:

- Seed nodes are layer `0`.
- Reachable neighbors are assigned the first discovered layer distance.
- Edges whose oriented source layer is lower than destination layer are placed
  in `dag.edges`.
- All other edges are placed in `dag.back_edges`.

Important:

- `dag.edges` is the forward explanation view.
- `dag.back_edges` are not necessarily errors; they capture cross-links,
  same-layer links, reverse links under the chosen direction, and other
  non-layer-advancing edges.

### 6.10 Result Contract

`DAGGrepResult` includes:

- `query`
- `mode`
- `mode_used`
- `seeds`
- `graph.nodes`
- `graph.edges`
- `dag.layers`
- `dag.edges`
- `dag.back_edges`
- `stats.seed_count`
- `stats.node_count`
- `stats.edge_count`
- `warnings`

The full graph result is sorted deterministically before emission:

- nodes by `id`
- edges by `src`, then `dst`, then `type`

This deterministic ordering is part of the current golden-test-friendly
contract.

---

## 7. Error and Empty-Result Semantics

- Empty `query` returns an empty `DAGGrepResult`.
- Missing store/query engine returns `ErrNotFound`.
- Invalid direction is rejected at the skill layer.
- Unknown node kinds or edge-set labels are rejected at the skill layer.
- No matching seeds returns an empty graph and an empty layer map.

---

## 8. Practical Interpretation Rules

Consumers SHOULD interpret output as follows:

- Prefer `CALLS` over `REFERS_TO` when reasoning about executable flow.
- Treat TypeScript and Elixir non-`1.0` edges as heuristic evidence.
- Treat doc-derived edges as contextual hints, not proof of runtime coupling.
- Use anchors to explain placement in the package/file hierarchy, not as proof
  of semantic relevance.
- Use `dag.back_edges` as relationship spillover, not necessarily graph noise.

---

## 9. Out of Scope for the Current Spec

The following are intentionally not yet specified as stable contracts:

- Vector-backed seed selection for repoindex.
- A formal graph query language.
- Incremental package-level graph updates.
- Path ranking beyond the current weighted frontier heuristic.
- Benchmark corpus and scoring harness for graph-query quality.

Those should be specified separately once they are introduced as durable
behavior.
