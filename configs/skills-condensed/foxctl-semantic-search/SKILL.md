---
name: foxctl Semantic Search
description: Semantic code search using embeddings and vector similarity. Search across symbols, memories, tasks, sessions, and codemaps.
---

# Semantic Search

Vector-based search using Voyage AI embeddings.

```bash
# JSON output (default)
foxctl run code/semantic_search --input '{"query": "auth middleware", "scope": ["symbols", "memories"]}'

# Tree view - shows related files grouped by directory
foxctl run code/semantic_search --input '{"query": "auth middleware", "format": "tree"}'
```

## Scopes

| Scope | Model | Content |
|-------|-------|---------|
| `symbols` | voyage-code-3 | Code symbols |
| `memories` | voyage-3.5 | Gotchas, notes |
| `tasks` | voyage-3.5 | Task descriptions |
| `sessions` | voyage-3.5 | Session context |
| `codemaps` | voyage-3.5 | Code relationship maps |

## Parameters

| Param | Description |
|-------|-------------|
| `query` | Natural language query |
| `scope` | Content types to search |
| `limit` | Max results (default: 10) |
| `format` | Output: `json` (default) or `tree` |
| `path` | Directory for symbol search |

## Environment

| Variable | Description |
|----------|-------------|
| `VOYAGE_API_KEY` | Required for Voyage |
| `EMBEDDING_MODEL_CODE` | Override: voyage-code-3 |
| `EMBEDDING_MODEL_TEXT` | Override: voyage-3.5 |

Full docs: `~/.foxctl/share/configs/skills/foxctl-semantic-search/Skill.md`
