---
title: Repo navigation
description: Semantic search, repoindex, smart search, and DAG grep workflows.
---

Status: Current shell, detailed behavior remains canonical in repo docs.

Use repo navigation when an agent needs to find code, explain relationships, or
prepare a compact evidence bundle before editing.

## Semantic search

```bash
foxctl run code/semantic_search --input '{"query":"auth middleware","format":"tree","limit":25}'
```

Use this first when the topic is fuzzy or you need a tree-shaped orientation.

## Smart search

```bash
foxctl run code/smart_search --input '{"query":"how does session restore work"}'
```

Use this when you need snippets and candidate expansion in one step.

## Repoindex

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

```bash
foxctl index repo search --workspace . --query "Supervisor" --limit 10
```

```bash
foxctl index repo expand --workspace . --seed "<node-id>" --edge CALLS --edge REFERS_TO --depth 2
```

## DAG grep

```bash
foxctl run code/dag_grep --input '{"query":"buildEvidencePack","workspace":".","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'
```

DAG grep is the compact explanation-subgraph path. It is the better default
when the question is about graph relationships rather than text snippets.

## Canonical sources

- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
- [docs/spec/repo_graph_index_and_dag_grep.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/repo_graph_index_and_dag_grep.md)
- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
