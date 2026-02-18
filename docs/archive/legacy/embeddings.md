# Embedding Strategy in agentctl

## Overview

agentctl uses embedding models to power semantic search across different content types. This document explains our embedding strategy, model selection, and how to configure the system.

## Scope-Based Model Selection

Different content types benefit from different embedding models. We use **scope-specific models** optimized for each content type:

| Scope | Content Type | Model | Price/1M Tokens | Rationale |
|-------|--------------|-------|-----------------|-----------|
| `symbols` | Code symbols | `voyage-code-3` | $0.18 | 13.8% better than OpenAI on code retrieval |
| `memories` | Gotchas, notes | `voyage-3.5` | $0.06 | Best price/performance for text |
| `tasks` | Task descriptions | `voyage-3.5` | $0.06 | Good quality at 1/3 cost |
| `sessions` | Session context | `voyage-3.5` | $0.06 | Good quality at 1/3 cost |
| `codemaps` | Code relationship maps | `voyage-3.5` | $0.06 | Semantic text, matches memory |

All models output 1024-dimensional vectors, ensuring storage compatibility.

## Why Two Models?

### voyage-code-3 (Code)

- Optimized specifically for code retrieval benchmarks
- 13.8% better than OpenAI ada-002 on code search tasks
- Higher cost ($0.18/1M) justified by code-specific optimization
- Used only for `symbols` scope

### voyage-3.5 (Text)

- Best price/performance ratio for general text
- 3x cheaper than voyage-3-large ($0.06 vs $0.18/1M)
- Sufficient quality for memories, tasks, sessions, codemaps
- Lower cost allows more aggressive embedding usage

## Architecture

### Query Embedding Generation

When a search query is executed, embeddings are generated in parallel:

```
Query: "how does authentication work"
           │
           ├─► voyage-code-3 embedding (for symbols)
           │
           └─► voyage-3.5 embedding (for text scopes)
```

This ensures each scope is searched with an embedding from the same model space as its stored embeddings.

### Storage Embedding Generation

When content is indexed or created:

- **Code symbols**: Embedded with `voyage-code-3` during indexing
- **Memories**: Embedded with `voyage-3.5` when saved
- **Tasks**: Embedded with `voyage-3.5` when created
- **Sessions**: Embedded with `voyage-3.5` when summarized
- **Codemaps**: Embedded with `voyage-3.5` after generation

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `VOYAGE_API_KEY` | - | Required for Voyage embeddings |
| `GEMINI_API_KEY` | - | Alternative: Gemini embeddings |
| `EMBEDDING_MODEL_CODE` | `voyage-code-3` | Override code embedding model |
| `EMBEDDING_MODEL_TEXT` | `voyage-3.5` | Override text embedding model |
| `EMBEDDING_MODEL` | - | Fallback for both if specific not set |
| `AGENTCTL_VECTOR_DIMS` | `1024` | Global vector dimensions |
| `AGENTCTL_EMBEDDING_RATE_LIMIT` | `3` | Requests per minute (0=unlimited) |

### Switching to Gemini

To use Gemini instead of Voyage:

```bash
export GEMINI_API_KEY=your-key
export EMBEDDING_MODEL=gemini-embedding-001
```

Note: Gemini uses 3072 dimensions. All stored embeddings must use the same provider.

## Provider Selection Logic

The system selects a provider based on available API keys:

```
1. Check if model override is specified (EMBEDDING_MODEL_CODE/TEXT)
2. Check if fallback model is specified (EMBEDDING_MODEL)
3. Auto-detect from API keys:
   - VOYAGE_API_KEY → Voyage provider
   - GEMINI_API_KEY → Gemini provider
4. Return no-op provider if no keys available
```

## Cost Estimation

Monthly cost estimates (assuming 1M tokens/month per scope):

| Scope | Tokens/Month | Model | Cost |
|-------|--------------|-------|------|
| symbols | 1M | voyage-code-3 | $0.18 |
| memories | 1M | voyage-3.5 | $0.06 |
| tasks | 500K | voyage-3.5 | $0.03 |
| sessions | 500K | voyage-3.5 | $0.03 |
| codemaps | 200K | voyage-3.5 | $0.012 |
| **Total** | ~3.2M | - | **~$0.30** |

Cost-effective strategy: Using voyage-3.5 for text scopes saves ~$0.36/month vs using voyage-3-large for everything.

## Implementation Details

### ScopeModelRecommendation()

```go
// internal/indexing/semantic/provider.go
func ScopeModelRecommendation(scope EmbeddingScope) (model string, isCodeModel bool) {
    switch scope {
    case ScopeSymbols:
        return "voyage-code-3", true
    default:
        return "voyage-3.5", false
    }
}
```

### Semantic Search Embedding Flow

```go
// skills/code_semantic_search/main.go
func generateScopedEmbeddings(ctx context.Context, query string, scopeSet map[string]bool, codeModel, textModel string) (scopedEmbeddings, error) {
    // Generate code embedding if symbols scope is requested
    if scopeSet["symbols"] {
        codeProvider := createProviderWithModel(codeModel)
        codeEmb = codeProvider.Embed(ctx, query)
    }

    // Generate text embedding if any text scope is requested
    if scopeSet["memories"] || scopeSet["tasks"] || scopeSet["sessions"] || scopeSet["codemaps"] {
        textProvider := createProviderWithModel(textModel)
        textEmb = textProvider.Embed(ctx, query)
    }

    return scopedEmbeddings{code: codeEmb, text: textEmb}, nil
}
```

## Troubleshooting

### Dimension Mismatch

**Error**: `dimension mismatch: query 3072, stored 1024`

**Cause**: Query embedding was generated with Gemini (3072 dims) but stored embeddings use Voyage (1024 dims).

**Fix**: Use the same provider for queries and storage. Re-index with the desired provider.

### Zero Search Results

**Cause**: Query embedding generated with different model than stored embeddings.

**Fix**: Ensure `ScopeModelRecommendation()` matches the model used during indexing.

### Rate Limiting

**Error**: `429 Too Many Requests`

**Fix**: Set `AGENTCTL_EMBEDDING_RATE_LIMIT` to control RPM:
- `0`: No rate limiting (for paid tier with higher limits)
- `3`: Default free tier limit
- `300`: Paid tier with higher limits

## Reranking (Optional)

Enable reranking for improved precision:

```bash
export AGENTCTL_SEMANTIC_RERANK=1
```

Uses Voyage `rerank-2.5` to reorder results by relevance after initial vector search. Adds latency but improves result quality.

## Related Documentation

- [Vector Search Implementation](./vector-search.md) - Low-level vector storage details
- [Vector Search Notes](./vector-search-implementation-notes.md) - SQLite vector evaluation
- [Semantic Search Design](../../designs/unified_semantic_search.md) - Architecture design
