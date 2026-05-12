---
title: Repoindex and DAG grep
description: Build the repo graph, search it, expand neighborhoods, and render explanation subgraphs with DAG grep.
---

Repoindex is foxctl's per-workspace code graph. It stores packages, files,
symbols, concepts, and typed edges so agents can navigate code by relationships
— calls, references, imports — beyond text search.

For the data model details (node kinds, edge kinds, IDs, weights), see
[repoindex model](/retrieval/repoindex-model/).

## Build the index

The repo graph is derived from source code and must be built before it can be
queried. Builds are incremental by default — only changed files are re-indexed.

### Standard build

For a Go + TypeScript + Elixir project:

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

For repos without Go source, disable Go indexing:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir
```

### Infrastructure and scripts

Include Terraform, Kubernetes manifests, and shell scripts when those files
matter for navigation:

```bash
foxctl index repo build --workspace . --terraform --kubernetes --shell
```

### Build flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--go` | true | Index Go source files |
| `--typescript` | false | Index TypeScript `.ts`/`.tsx` files |
| `--elixir` | false | Index Elixir `.ex`/`.exs` files |
| `--terraform` | false | Index Terraform `.tf` files |
| `--kubernetes` | false | Index Kubernetes YAML manifests |
| `--shell` | false | Index shell scripts |
| `--incremental` | true | Skip rebuild when stored file state is current |
| `--include-tests` | false | Index test files |
| `--go-pattern` | `./...` | Scope Go packages |
| `--include-semantic-anchors` | false | Include semantic anchor concept nodes and edges |
| `--include-cochange` | false | Include empirical git co-change file edges |
| `--dry-run` | false | Build without writing to the index |

Force a full rebuild:

```bash
foxctl index repo build --workspace . --go --typescript --incremental=false
```

### Check status

View the active database path and build metadata:

```bash
foxctl index repo status --workspace .
```

Generated repoindex databases live under:

```
~/.foxctl/storage/repoindex/<repo>-repoindex-<hash>.db
```

These are local artifacts and should not be committed to version control.

## Enrich summaries

Repoindex build does not attach summaries by default. After building the graph,
generate and attach stored file and symbol summaries to graph nodes:

```bash
# Generate summaries
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .

# Attach summaries to graph nodes
foxctl index repo enrich summaries --workspace .
```

Summaries come from the same file summary store that the semantic tree uses.
After enrichment, graph search and `open` output include summary text alongside
node metadata.

Equivalent skill wrappers:

```bash
foxctl run repo/index_build --input '{"workspace": ".", "include_go": true, "include_typescript": true}'
foxctl run repo/index_enrich_summaries --input '{"workspace": "."}'
```

After a repoindex schema change, rebuild any affected skill artifacts:

```bash
make skill SKILL=repo_index_search
```

## Search the graph

Full-text search over node names, signatures, docs, and summaries using SQLite
FTS with BM25 scoring:

```bash
foxctl index repo search --workspace . --query "Supervisor" --limit 10
```

Search fallback behavior:
1. Raw trimmed query
2. Quoted query fallback
3. OR-joined multi-word fallback

This fallback sequence affects seed selection for DAG grep.

## Open a node

Retrieve a single node by its ID:

```bash
foxctl index repo open --workspace . --id "<node-id>"
```

Copy node IDs from `search`, `expand`, or `dag_grep` output. Do not hand-write
IDs unless a test owns the fixture.

## Ask a question

Run an LLM tool loop over repoindex to answer natural-language questions:

```bash
foxctl index repo ask --workspace . --question "Where is repoindex built?"
```

## Expand relationships

Starting from a seed node, traverse typed edges to discover connected code:

```bash
foxctl index repo expand --workspace . \
  --seed "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:Builder.addGoReferenceEdges" \
  --edge CALLS --edge REFERS_TO \
  --direction out --depth 2 --budget 50
```

### Expand parameters

| Parameter | Default | Purpose |
|-----------|---------|---------|
| `--seed` | required | Starting node ID |
| `--edge` | none | Edge types to traverse (repeatable) |
| `--direction` | `out` | `out` (forward) or `in` (reverse) |
| `--depth` | `2` | Maximum traversal depth |
| `--budget` | `50` | Maximum distinct nodes to return |

### Example output

```json
{
  "data": {
    "result": {
      "nodes": [
        {
          "id": "sym:go:...:Builder.addGoReferenceEdges",
          "kind": "symbol",
          "file": "internal/intelligence/indexing/repoindex/builder.go",
          "name": "Builder.addGoReferenceEdges"
        },
        {
          "id": "sym:go:...:goCallTargetNodeID",
          "kind": "symbol",
          "file": "internal/intelligence/indexing/repoindex/builder.go",
          "name": "goCallTargetNodeID"
        }
      ],
      "edges": [
        {
          "src": "sym:go:...:Builder.addGoReferenceEdges",
          "dst": "sym:go:...:goCallTargetNodeID",
          "type": "CALLS",
          "weight": 1
        }
      ]
    }
  }
}
```

## DAG grep

DAG grep is an explanation-subgraph query over repoindex. It starts from
query seeds, expands a bounded graph neighborhood, and renders a compact
layered explanation subgraph.

Use it to answer questions like:
- "What calls or uses X?"
- "Show me the nearby graph around Y"
- "What code and doc relationships surround this topic?"

### Basic usage

```bash
foxctl run code/dag_grep --input '{
  "query": "buildEvidencePack",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80,
  "k": 5
}'
```

### Parameters

| Field | Default | Purpose |
|-------|---------|---------|
| `query` | required | Text query for seed selection |
| `mode` | `hybrid` | Query mode hint: `fts`, `semantic`, `hybrid` |
| `k` | `10` | Number of top seeds to keep |
| `node_kinds` | none | Filter seeds by node kind |
| `edge_types` | structural set | Explicit edge types to traverse |
| `direction` | `out` | `out` or `in` |
| `depth` | `2` | Traversal depth limit |
| `budget` | `80` | Maximum distinct nodes in result |
| `per_node_cap` | `20` | Maximum edges fetched per expanded node |
| `include_anchors` | `true` | Add package/file anchor context |
| `render` | none | `tree` or `mermaid` output format |

### Edge sets

| Edge set | Includes |
|----------|----------|
| `structural` | `CALLS`, `REFERS_TO`, `IMPORTS`, `CONTAINS`, `IMPLEMENTS`, `EMBEDS`, `TESTS` |
| Custom | Pass specific `edge_types` for targeted traversal |

### How it works

1. **Seed selection** — Runs FTS search with limit `k * 3`, normalizes BM25
   scores (`1 / (1 + bm25)`), and keeps the top `k` results.
2. **Weighted expansion** — Traverses edges using a max-heap frontier keyed by
   relevance score. Score decays with depth: `next_score = current_score *
   edge_weight * 0.85^(next_depth)`.
3. **Anchoring** — When `include_anchors` is true, adds containing file and
   package nodes for symbol results.
4. **Layering** — Computes a DAG view with forward edges and back-edges.
   Seeds are layer 0; reachable neighbors get increasing layer numbers.
5. **Output** — Returns nodes, edges, DAG layers, forward edges, back-edges,
   and statistics.

### Direction semantics

- `direction: "out"` — expansion fetches outgoing edges from each node; forward
  DAG layering treats `src -> dst` as forward.
- `direction: "in"` — expansion fetches incoming edges to each node; DAG
  layering inverts edge orientation.

The stored edge direction in the graph is unchanged regardless of direction
setting.

### Render formats

- `tree` — ASCII tree rendering of the explanation subgraph
- `mermaid` — Mermaid diagram syntax

### Result contract

A DAG grep result includes:

| Field | Contents |
|-------|----------|
| `seeds` | Starting nodes with normalized scores |
| `graph.nodes` | All discovered nodes |
| `graph.edges` | All discovered edges |
| `dag.layers` | Layer assignments (seeds = layer 0) |
| `dag.edges` | Forward edges (source layer < destination layer) |
| `dag.back_edges` | Cross-links, reverse links, same-layer links |
| `stats` | Seed count, node count, edge count |
| `warnings` | Any notices about mode fallback or limits |

Back-edges are not errors — they capture cross-links, reverse links, and other
non-layer-advancing edges.

## Edge weight semantics

Weights encode confidence and traversal preference, not cost. Higher weight
means stronger preference:

| Weight | Edge types | Interpretation |
|--------|-----------|----------------|
| `1.0` | Go `CALLS`, `REFERS_TO`, `IMPORTS`, `CONTAINS`; synthetic anchors | Exact structural facts from the indexer |
| `0.9` | TypeScript heuristic `CALLS` | Strong heuristic, not type-checked |
| `0.85` | Elixir heuristic `REFERS_TO` | Strong heuristic, name-matched |
| `0.75` | Doc-to-symbol edges (`DOC_RELATED`, `DOC_FLOW`) | Contextual hints, not runtime coupling |
| `0.7` | TypeScript `IMPORTS` | Moderate confidence |
| `0.6` | Concept edges from doc index blocks | Soft discoverability hints |

**Practical rules:**
- Prefer `CALLS` over `REFERS_TO` when reasoning about executable flow.
- Treat TypeScript and Elixir non-`1.0` edges as heuristic evidence.
- Treat doc-derived edges as contextual hints, not proof of runtime coupling.

## Language coverage

| Language | Nodes | Edges | Notes |
|----------|-------|-------|-------|
| Go | packages, files, symbols | `CONTAINS`, `IMPORTS`, `CALLS`, `REFERS_TO` | Exact typed AST resolution; complete when target symbol is in the graph |
| TypeScript | packages, files, symbols | `CONTAINS`, `IMPORTS`, heuristic `CALLS` | Call edges are not type-checked; treat as heuristic |
| Elixir | packages, files, symbols | `CONTAINS`, heuristic `REFERS_TO` | Name-matched references; package identity is directory-based |
| Terraform | packages, files, concepts | Resource, module, provider, variable, output concepts | |
| Kubernetes | packages, files, concepts | Resource concepts from `apiVersion` + `kind` manifests | |
| Shell | packages, files, concepts | Command and environment variable concepts | |

## Skill wrappers

For convenience, repoindex operations are also available as skill invocations:

```bash
# Build via skill
foxctl run repo/index_build --input '{"workspace": ".", "include_go": true, "include_typescript": true}'

# DAG grep via skill
foxctl run code/dag_grep --input '{
  "query": "RetrieveMixed",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80
}'
```

## Current boundaries

- Repoindex is derived from source and can be rebuilt at any time. Source
  files, docs, and stores remain canonical.
- TypeScript `CALLS` edges and Elixir `REFERS_TO` edges are heuristic, not
  complete language-server-quality call graphs.
- DAG grep seeds from FTS search, not vector search. The "hybrid" mode
  currently falls back to FTS behavior.
- The graph is a directed property graph, not a strict DAG — cycles can exist.
- Generated repoindex database files are local artifacts and should not be
  committed to version control.
- Semantic anchor edges are opt-in (`--include-semantic-anchors`) and should not
  be assumed for every graph.

## Storage

Repoindex databases live under local foxctl storage:

```
~/.foxctl/storage/repoindex/<repo>-repoindex-<hash>.db
```

Each workspace gets its own database scoped by repo key. Cross-repo edges are
not stored in a single shared graph.

## Observability

Repoindex queries emit `repo_index` events into the observability stream. See
the storage documentation for the default observability directory.

## Cross-references

- [Repoindex model](/retrieval/repoindex-model/) — node kinds, edge kinds, IDs, anchors
- [Search and index](/retrieval/search-and-index/) — text and semantic search surfaces
- [Skills runtime](/skills/runtime-and-install/) — running skills
- [Repo navigation workflow](/workflows/repo-navigation/) — end-to-end navigation guide
