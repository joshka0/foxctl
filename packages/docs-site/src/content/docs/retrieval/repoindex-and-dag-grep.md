---
title: Repoindex and DAG grep
description: Build the repo graph, search it, expand neighborhoods, and render explanation subgraphs.
---

Status: Current, with semantic-anchor and co-change edges gated by current repoindex behavior.

Repoindex is foxctl's per-workspace code graph. It stores packages, files,
symbols, concepts, and typed edges so agents can navigate beyond text search.

For the data model, see [repoindex model](/retrieval/repoindex-model/).

## Build

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

For repos without Go:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir
```

For infrastructure and scripts:

```bash
foxctl index repo build --workspace . --terraform --kubernetes --shell
```

## Enrich summaries

```bash
foxctl index file-summaries --workspace .
```

```bash
foxctl index symbol-summaries --workspace .
```

```bash
foxctl index repo enrich summaries --workspace .
```

## Search and expand

```bash
foxctl index repo search --workspace . --query "Supervisor" --limit 10
```

```bash
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
```

## DAG grep

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

DAG grep starts from query seeds, expands a bounded graph neighborhood, and
renders a compact explanation subgraph.

## Current boundaries

- The repoindex store remains the canonical local graph index.
- Repoindex is derived from source and can be rebuilt. Source files, docs, and
  stores remain canonical.
- Semantic anchor edges are opt-in and should not be assumed for every graph.
- Graph results explain relationships; ranking and query planning still live in
  app code.
- TypeScript `CALLS` edges and Elixir `REFERS_TO` edges are heuristic, not
  complete language-server-quality call graphs.
- DAG grep is not vector search. It seeds from repoindex search and renders a
  bounded explanation subgraph.
- Generated repoindex database files are local artifacts and should not be
  committed.

## Canonical sources

- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
- [docs/spec/repo_graph_index_and_dag_grep.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/repo_graph_index_and_dag_grep.md)
- [docs/plans/features/semantic-code-anchors.md](https://github.com/joshka0/foxctl/blob/main/docs/plans/features/semantic-code-anchors.md)
