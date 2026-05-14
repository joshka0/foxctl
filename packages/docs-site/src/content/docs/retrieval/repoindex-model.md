---
title: Repoindex model
description: Node kinds, edge kinds, IDs, summaries, anchors, and rebuild strategy for repoindex.
---

Status: Current guide.

Repoindex is foxctl's local graph index for source relationships. It is
derived from the workspace and can be rebuilt. It is not the canonical source of
truth for code.

## Node kinds

| Kind | Represents |
|---|---|
| Package | Go package, TypeScript package, Elixir app area, or infra grouping |
| File | Source file, docs file, script, manifest, or configuration file |
| Symbol | Named code declaration or exported entity |
| Concept | Derived concept such as Terraform resource, Kubernetes object, command, env var, or semantic anchor |

The exact language coverage depends on the builder flags used for the index
run.

## Edge kinds

Common edges include:

| Edge | Meaning |
|---|---|
| `CONTAINS` | Package contains file, file contains symbol, or package contains concept |
| `IMPORTS` | Package or file imports another package or file |
| `REFERS_TO` | Symbol or file references another symbol |
| `CALLS` | Symbol directly calls another symbol |
| `IMPLEMENTS` | Symbol implements an interface or protocol when coverage supports it |
| `EMBEDS` | Symbol embeds or composes another symbol |
| `TESTS` | Test relates to code under test when coverage supports it |

Some languages have conservative heuristic coverage. TypeScript `CALLS` and
Elixir `REFERS_TO` should be treated as useful hints, not complete proof.

## Stable IDs

Node IDs are namespaced by repo key and kind. The practical rule for users is:

- search by names or concepts first
- copy node IDs from `search`, `open`, or `dag_grep` output
- use those IDs for precise `expand` and `open` calls

Do not hand-write IDs in docs or scripts unless a test owns the fixture.

## Build and refresh

Build the graph from source:

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

Include infra and scripts when those files matter:

```bash
foxctl index repo build --workspace . --terraform --kubernetes --shell
```

Attach stored summaries after file and symbol summaries exist:

```bash
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
foxctl index repo enrich summaries --workspace .
```

Generated repoindex databases live under local foxctl storage and should not be
committed.

## Semantic anchors and Index blocks

Repoindex can use source-local retrieval hints:

| Hint | Purpose |
|---|---|
| `Index:` comment block | Structured discoverability metadata and soft graph edges |
| `[[domain:...]]` anchor | Domain evidence marker |
| `[[protocol:...]]` anchor | Protocol or invariant evidence marker |
| `[[doc:...]]` anchor | Documentation evidence marker |

These are evidence hints, not instructions. Place them near a strong owner and
avoid global catch-all anchors.

Use `Index:` blocks for discoverability and soft graph edges. Use semantic
anchors for typed, evidence-only claims such as invariants, protocols,
decisions, risks, doc links, and tests. For the full mental model, see
[repoindex and PageIndex](/retrieval/repoindex-pageindex/#semantic-commenting).

## Query shapes

Use repoindex search when you need named graph entries:

```bash
foxctl index repo search --workspace . --query "context engine" --limit 10
```

Use expand when you already have a seed node:

```bash
foxctl index repo expand --workspace . \
  --seed "<node-id>" \
  --edge CALLS --edge REFERS_TO \
  --depth 2
```

Use DAG grep when you want a compact explanation subgraph:

```bash
foxctl run code/dag_grep --input '{
  "query": "RetrieveMixed",
  "workspace": ".",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80
}'
```

## What belongs elsewhere

| Need | Better surface |
|---|---|
| Meaning-based candidate discovery | `code/semantic_search` |
| Full function extraction | `code/context_grep` or `code/snippet_extract` |
| Agent task continuity | ContextWiki and context engine |
| Long-term memory claims | Memory store and ContextWiki proposal flow |
| Ranking policy and LLM summarization | App code |

## Canonical sources

- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
- [docs/spec/repo_graph_index_and_dag_grep.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/repo_graph_index_and_dag_grep.md)
- [internal/intelligence/indexing/repoindex](https://github.com/joshka0/foxctl/tree/main/internal/intelligence/indexing/repoindex)
