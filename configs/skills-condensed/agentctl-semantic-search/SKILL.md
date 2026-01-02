---
name: agentctl Semantic Search
description: Semantic code search using embeddings and vector similarity. Search across symbols, memories, tasks, sessions, and codemaps.
---

# Semantic Search

Vector-based search using Voyage AI embeddings.

```bash
agentctl run code/semantic_search --input '{"query": "auth middleware", "scope": ["symbols", "memories"]}'
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
| `path` | Directory for symbol search |

## Environment

| Variable | Description |
|----------|-------------|
| `VOYAGE_API_KEY` | Required for Voyage |
| `EMBEDDING_MODEL_CODE` | Override: voyage-code-3 |
| `EMBEDDING_MODEL_TEXT` | Override: voyage-3.5 |

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-semantic-search/Skill.md`
