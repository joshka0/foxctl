---
title: Search and index
description: Choose semantic search, smart search, context grep, or repo graph search.
---

Status: Current.

foxctl exposes several retrieval paths. Choose by the shape of the question,
not by implementation detail.

## Which search to use

| Question shape | Start with |
|---|---|
| "Where is this concept implemented?" | `code/semantic_search` |
| "Find snippets that answer this" | `code/smart_search` |
| "Find exact text or function shapes" | `code/context_grep` |
| "Show graph neighbors and paths" | `repoindex` or `code/dag_grep` |
| "Recall named project memory" | `memory/query` or session recall |

## Semantic search

```bash
foxctl run code/semantic_search --input '{"query":"auth middleware","format":"tree","limit":25}'
```

Use tree mode for orientation before editing unfamiliar code.

## Smart search

```bash
foxctl run code/smart_search --input '{"query":"how does session restore work"}'
```

Smart search combines candidate generation with snippet extraction.

The newer task-oriented input shape is:

```bash
foxctl run code/smart_search --input '{"question":"How does session restore work?","limits":{"max_snippets":20}}'
```

## Context grep

```bash
foxctl run code/context_grep --input '{"pattern":"Run\\(ctx","path":"internal"}'
```

Use this when exact text, function signatures, or structured grep are more
reliable than semantic ranking.

## Production guidance

- Use semantic search to orient.
- Re-open the canonical source file before editing.
- Use repoindex or DAG grep when relationships matter.
- Use `snippet_extract` when you already have candidates; use `smart_search`
  when you need candidate generation and extraction together.
- Treat generated codemaps as supporting artifacts, not canonical behavior.

## Snippet extraction

```bash
foxctl run code/snippet_extract --input '{"question":"explain this path","candidates":[{"path":"internal/foo.go"}]}'
```

## Canonical sources

- [docs/general/search.md](https://github.com/joshka0/foxctl/blob/main/docs/general/search.md)
- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
- [docs/general/code-search-evals.md](https://github.com/joshka0/foxctl/blob/main/docs/general/code-search-evals.md)
- [docs/general/retrieval-evals.md](https://github.com/joshka0/foxctl/blob/main/docs/general/retrieval-evals.md)
