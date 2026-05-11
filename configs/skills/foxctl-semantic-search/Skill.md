---
name: foxctl Semantic Search
description: Semantic code search using embeddings and vector similarity. Search across symbols, memories, tasks, sessions, and codemaps.
---

# Semantic Search with foxctl

Vector-based semantic search across multiple content types using local
OpenAI-compatible embeddings by default.

## Quick Usage

```bash
foxctl run code/semantic_search --input '{
  "query": "authentication middleware",
  "scope": ["symbols", "memories", "codemaps"],
  "limit": 10
}'

foxctl run code/semantic_search --input '{
  "query": "authentication middleware",
  "format": "tree"
}'
```

## Scopes

| Scope | Content Type | Default Model | Description |
|-------|--------------|---------------|-------------|
| `symbols` | Code | `text-embedding-qwen3-embedding-8b` | Functions, classes, variables from codebase |
| `memories` | Text | `text-embedding-qwen3-embedding-8b` | Gotchas, learnings, notes |
| `tasks` | Text | `text-embedding-qwen3-embedding-8b` | Task descriptions and notes |
| `sessions` | Text | `text-embedding-qwen3-embedding-8b` | Session summaries and context |
| `codemaps` | Text | `text-embedding-qwen3-embedding-8b` | Semantic code relationship maps |

## Parameters

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `query` | string | required | Natural language search query |
| `scope` | string[] | all scopes | Content types to search |
| `limit` | int | 10 | Max results per scope |
| `format` | string | json | Output: `json` or `tree` |
| `repo_index_mode` | string | auto | Repoindex contribution mode for symbol/code scope |
| `memory_decay_enabled` | bool | false | Apply memory recency/access rerank to memory-scope candidates |
| `rerank_enabled` | bool | false | Apply local Qwen/OpenAI-compatible model rerank after fusion |
| `rerank_top_k` | int | 50 | Candidate count passed to the reranker |
| `rerank_model` | string | `Qwen/Qwen3-Reranker-0.6B` | Reranker model override |

## Embedding Strategy

The default provider is `openai_compat`, using
`text-embedding-qwen3-embedding-8b` for code and text scopes. Per-scope
overrides are still supported when a smaller or specialized model is needed.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `FOXCTL_EMBEDDING_PROVIDER` | `openai_compat` | Embedding provider |
| `FOXCTL_EMBEDDING_MODEL` | `text-embedding-qwen3-embedding-8b` | Default embedding model |
| `FOXCTL_EMBEDDING_BASE_URL` | `http://127.0.0.1:1234/v1` | OpenAI-compatible embedding endpoint |
| `FOXCTL_EMBEDDING_API_KEY` | - | Optional bearer token for the embedding endpoint |
| `FOXCTL_EMBEDDING_MODEL_<SCOPE>` | - | Per-scope embedding model override |
| `FOXCTL_RERANK_ENABLED` | false | Enable model reranking |
| `FOXCTL_RERANK_BASE_URL` | inherits embedding base URL | OpenAI-compatible rerank endpoint |
| `FOXCTL_RERANK_MODEL` | `Qwen/Qwen3-Reranker-0.6B` | Rerank model |

## Examples

### Search Code Symbols

```bash
foxctl run code/semantic_search --input '{
  "query": "rate limiting",
  "scope": ["symbols"],
  "repo_index_mode": "search"
}'
```

### Search Memories and Gotchas

```bash
foxctl run code/semantic_search --input '{
  "query": "authentication gotchas",
  "scope": ["memories"],
  "memory_decay_enabled": true
}'
```

### Cross-Scope Search

```bash
foxctl run code/semantic_search --input '{
  "query": "database connection pooling",
  "scope": ["symbols", "memories", "sessions"]
}'
```

### Tree View of Related Files

```bash
foxctl run code/semantic_search --input '{
  "query": "embedding vector search",
  "scope": ["symbols"],
  "format": "tree"
}'
```

## Output Format

```json
{
  "results": [
    {
      "source": "symbols",
      "id": "symbol:/workspace/internal/file.go#L42",
      "name": "FunctionName",
      "snippet": "func FunctionName(...) { ... }",
      "similarity": 0.85,
      "rank": 1
    }
  ],
  "stats": {
    "total_results": 5,
    "source_counts": {"symbols": 3, "memories": 2},
    "embedding_dimensions": 4096
  }
}
```

## Reranking

Enable reranking for improved precision:

```bash
export FOXCTL_RERANK_ENABLED=true
export FOXCTL_RERANK_BASE_URL=http://127.0.0.1:8000/v1
foxctl run code/semantic_search --input '{"query": "...", "rerank_enabled": true}'
```

The reranker posts fused candidates to a local OpenAI-compatible `/rerank`
endpoint and uses the returned relevance scores to reorder the top candidates.
