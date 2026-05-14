---
title: Repoindex and PageIndex
description: How PageIndex's table-of-contents retrieval idea maps to repoindex, DAG grep, and semantic comments.
---

Repoindex is inspired by PageIndex's central retrieval idea: before asking an
LLM to read raw content, give it a compact, navigable structure that helps it
decide where to look.

PageIndex applies that idea to long documents. It builds a hierarchy similar to
a table of contents, then uses tree search over section summaries and node IDs
to retrieve precise document evidence. Repoindex applies the same instinct to a
software workspace, but the structure is a code graph rather than only a
document tree.

## Similarities

| PageIndex pattern | Repoindex equivalent |
|---|---|
| A table-of-contents tree gives the model a readable map before content is opened. | Packages, files, symbols, concepts, summaries, and semantic tree output give agents a readable map before snippets are opened. |
| Retrieval narrows top-down from broad sections to targeted nodes. | Agents search, expand, and open graph nodes instead of dumping the whole repo into context. |
| Nodes carry short summaries and references back to raw content. | Repoindex nodes carry names, paths, signatures, docs, summaries, and source locations. |
| The index is derived from the source and can be rebuilt. | Repoindex is derived from the workspace and stored locally under foxctl storage. |
| The retrieval path should be inspectable. | DAG grep renders a bounded explanation subgraph with visible seeds, nodes, and edges. |

The shared principle is not "use a tree for everything." It is "make the
retrieval structure legible enough that an agent can plan its next read."

## Important differences

PageIndex is document-first. Its source objects are pages, sections, headings,
tables, and page ranges. Repoindex is code-first. Its source objects are
packages, files, symbols, concepts, and typed relationships.

PageIndex emphasizes vectorless reasoning over a document tree. Repoindex is a
deterministic local graph with SQLite FTS/BM25, typed edges, optional summaries,
semantic anchors, semantic stores, and DAG rendering. LLMs can use repoindex,
but repoindex itself should remain useful without a live model.

PageIndex's core traversal is mostly hierarchical. Repoindex starts with a
hierarchy but becomes a graph:

- `CONTAINS` captures package, file, symbol, and concept nesting.
- `IMPORTS` captures dependency direction.
- `CALLS` and `REFERS_TO` capture code relationships.
- Semantic-anchor and `Index:` edges add curated retrieval hints.
- Co-change and other empirical edges can add observed repo behavior when
  explicitly enabled.

## Semantic commenting

Semantic commenting is how source code teaches repoindex about relationships
that syntax alone cannot express cleanly.

Use the two lanes separately:

| Lane | Purpose | Think of it as |
|---|---|---|
| `Index:` blocks | Broad discoverability and soft graph edges | "Searchers will ask for this; connect the nearby terms, flows, and related symbols." |
| `[[...]]` semantic anchors | Typed, evidence-only claims | "This owner proves or implements a stable invariant, risk, protocol, decision, domain, doc link, or test contract." |

Do not add comments for volume. Add them near the strongest owner when they
make future retrieval more truthful:

```go
// Index:
//   Purpose: Builds semantic anchor graph edges for indexed source comments.
//   Keywords: semantic anchors, repoindex, evidence edges
//   Related: applySemanticAnchorEdges, NewSemanticAnchorEdge
//
// [[protocol:semantic-anchor-indexing]]
// [[doc:docs/general/repoindex-pageindex.md#How To Think About Semantic Commenting]]
func applySemanticAnchorEdges(...) { ... }
```

`Index:` blocks help repoindex find and connect code. Semantic anchors help
agents explain why a node matters. Neither lane is an instruction or policy
authority by itself. Agents should still open source files, docs, and tests
before treating a retrieved relationship as evidence.

Good semantic-comment targets include:

- protocol and envelope boundaries
- lifecycle components with `Run(ctx)` ownership
- storage transitions and indexing builders
- safety gates and authorization checks
- concurrency guards and bounded queues
- tests that prove a durable behavior

Poor targets include obvious wrappers, leaf helpers, temporary TODOs, generated
code, repeated broad themes, or claims that are not enforced by the nearby code.

## Design rules

1. Preserve the smart-table-of-contents experience. `code/semantic_search` tree
   mode and repoindex summaries should make a repo easy to scan before opening
   code.
2. Keep the graph factual and rebuildable. The source repo remains canonical.
   Indexes are derived artifacts.
3. Prefer explicit typed edges over hidden prompt behavior. If a relationship
   matters, model it as an edge or anchor.
4. Keep summaries short and evidence-bound. Summaries are navigation labels, not
   substitute source.
5. Use `search -> expand -> open` as the normal flow. Search finds likely nodes,
   expand builds local context, and open retrieves precise source evidence.
6. Do not collapse code into plain chunks. Repoindex earns its keep by
   preserving names, owners, relationships, and source locations.

## Sources

- [PageIndex README](https://github.com/VectifyAI/PageIndex/blob/main/README.md)
- [PageIndex framework article](https://pageindex.ai/blog/pageindex-intro)
- [PageIndex file system article](https://pageindex.ai/blog/pageindex-filesystem)
- [Repoindex model](/retrieval/repoindex-model/)
- [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/)
