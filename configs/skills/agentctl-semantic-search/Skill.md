---
name: agentctl Semantic Search
description: Semantic code search using embeddings and vector similarity. Search across symbols, memories, tasks, sessions, and codemaps.
---

# Semantic Search with agentctl

Vector-based semantic search across multiple content types using Voyage AI embeddings.

## Quick Usage

```bash
agentctl run code/semantic_search --input '{
  "query": "authentication middleware",
  "scope": ["symbols", "memories", "codemaps"],
  "limit": 10
}'
```

## Scopes

| Scope | Content Type | Model | Description |
|-------|--------------|-------|-------------|
| `symbols` | Code | voyage-code-3 | Functions, classes, variables from codebase |
| `memories` | Text | voyage-3.5 | Gotchas, learnings, notes |
| `tasks` | Text | voyage-3.5 | Task descriptions and notes |
| `sessions` | Text | voyage-3.5 | Session summaries and context |
| `codemaps` | Text | voyage-3.5 | Semantic code relationship maps |

## Parameters

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `query` | string | required | Natural language search query |
| `scope` | string[] | all scopes | Content types to search |
| `limit` | int | 10 | Max results per scope |
| `path` | string | workspace | Directory to scope symbol search |
| `threshold` | float | 0.0 | Minimum similarity score (0-1) |

## Embedding Strategy

Two embedding models are used for optimal retrieval:

- **voyage-code-3** ($0.18/1M tokens): Optimized for code, 13.8% better than OpenAI
- **voyage-3.5** ($0.06/1M tokens): Best price/performance for text

The skill automatically generates separate embeddings for code (symbols) and text (everything else) scopes.

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `VOYAGE_API_KEY` | - | Required for Voyage embeddings |
| `GEMINI_API_KEY` | - | Alternative: Gemini embeddings |
| `EMBEDDING_MODEL_CODE` | voyage-code-3 | Override code embedding model |
| `EMBEDDING_MODEL_TEXT` | voyage-3.5 | Override text embedding model |
| `EMBEDDING_MODEL` | - | Fallback for both |

## Examples

### Search Code Symbols

```bash
agentctl run code/semantic_search --input '{
  "query": "rate limiting",
  "scope": ["symbols"],
  "path": "internal/api/"
}'
```

### Search Memories and Gotchas

```bash
agentctl run code/semantic_search --input '{
  "query": "authentication gotchas",
  "scope": ["memories"]
}'
```

### Search Codemaps

```bash
agentctl run code/semantic_search --input '{
  "query": "how does auth work",
  "scope": ["codemaps"]
}'
```

### Cross-Scope Search

```bash
agentctl run code/semantic_search --input '{
  "query": "database connection pooling",
  "scope": ["symbols", "memories", "sessions"]
}'
```

## Output Format

```json
{
  "results": [
    {
      "source": "symbols",
      "id": "symbol:///path/file.go:FunctionName",
      "name": "FunctionName",
      "snippet": "func FunctionName(...) { ... }",
      "similarity": 0.85,
      "rank": 1
    }
  ],
  "stats": {
    "total_results": 5,
    "source_counts": {"symbols": 3, "memories": 2},
    "embedding_dimensions": 1024
  }
}
```

## Hooks Integration

The `semantic-search` hook automatically runs on Grep/Glob operations to surface relevant context:

```yaml
# .claude/settings.json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Grep|Glob",
        "command": "agentctl run code/semantic_search ..."
      }
    ]
  }
}
```

## Reranking

Enable reranking for improved precision:

```bash
export AGENTCTL_SEMANTIC_RERANK=1
agentctl run code/semantic_search --input '{"query": "...", "rerank": true}'
```

Uses Voyage rerank-2.5 to reorder results by relevance.
