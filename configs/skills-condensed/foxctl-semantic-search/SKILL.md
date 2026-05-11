---
name: foxctl Semantic Search
description: Semantic code search using embeddings and vector similarity. Search across symbols, memories, tasks, sessions, and codemaps.
---

# Semantic Search

Vector-based search using local OpenAI-compatible embeddings by default.

```bash
foxctl run code/semantic_search --input '{"query": "auth middleware", "scope": ["symbols", "memories"]}'
foxctl run code/semantic_search --input '{"query": "auth middleware", "format": "tree"}'
```

## Scopes

| Scope | Default Model | Content |
|-------|---------------|---------|
| `symbols` | `text-embedding-qwen3-embedding-8b` | Code symbols |
| `memories` | `text-embedding-qwen3-embedding-8b` | Gotchas, notes |
| `tasks` | `text-embedding-qwen3-embedding-8b` | Task descriptions |
| `sessions` | `text-embedding-qwen3-embedding-8b` | Session context |
| `codemaps` | `text-embedding-qwen3-embedding-8b` | Code relationship maps |

## Parameters

| Param | Description |
|-------|-------------|
| `query` | Natural language query |
| `scope` | Content types to search |
| `limit` | Max results (default: 10) |
| `format` | Output: `json` (default) or `tree` |
| `repo_index_mode` | Repoindex mode for symbol/code search |
| `memory_decay_enabled` | Local memory recency/access rerank |
| `rerank_enabled` | Local Qwen/OpenAI-compatible model rerank |

## Environment

| Variable | Description |
|----------|-------------|
| `FOXCTL_EMBEDDING_BASE_URL` | OpenAI-compatible embedding endpoint |
| `FOXCTL_EMBEDDING_MODEL` | Default embedding model |
| `FOXCTL_RERANK_BASE_URL` | OpenAI-compatible rerank endpoint |
| `FOXCTL_RERANK_MODEL` | Rerank model |

Full docs: `~/.foxctl/share/configs/skills/foxctl-semantic-search/Skill.md`
