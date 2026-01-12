# Search & Embeddings

agentctl provides semantic search across code symbols, sessions, memories, and codemaps.

---

## Quick Start

```bash
# Semantic search (uses skills)
agentctl run code/semantic_search --input '{"query": "authentication flow", "limit": 10}'

# With scope filter
agentctl run code/semantic_search --input '{"query": "auth", "scopes": ["symbols", "memories"]}'
```

---

## Search Modes

| Mode | Type | Best For |
|------|------|----------|
| **Vector** | Semantic/meaning | Conceptual queries, "how does X work" |
| **BM25** | Lexical/keyword | Exact terms, technical names |
| **Hybrid** | Combined | General purpose (default) |

---

## Embedding Models

All embeddings use **1024 dimensions** (Voyage AI).

| Scope | Model | Cost/1M | Use |
|-------|-------|---------|-----|
| `symbols` | `voyage-code-3` | $0.18 | Code search |
| `memories` | `voyage-3.5` | $0.06 | Gotchas, notes |
| `tasks` | `voyage-3.5` | $0.06 | Task descriptions |
| `sessions` | `voyage-3.5` | $0.06 | Session context |
| `codemaps` | `voyage-3.5` | $0.06 | Code relationship maps |

**Why two models?** `voyage-code-3` is 13.8% better than OpenAI on code retrieval but costs 3x more. We use it only for code symbols.

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `VOYAGE_API_KEY` | - | **Required** for embeddings |
| `AGENTCTL_VECTOR_DIMS` | `1024` | Vector dimensions |
| `AGENTCTL_SEMANTIC_RERANK` | `0` | Enable Voyage rerank-2.5 |
| `AGENTCTL_EMBEDDING_RATE_LIMIT` | `3` | RPM limit (0=unlimited) |

### Optional Overrides

```bash
# Override models
export EMBEDDING_MODEL_CODE=voyage-code-3
export EMBEDDING_MODEL_TEXT=voyage-3.5

# Use Gemini instead (3072 dims - incompatible with Voyage)
export GEMINI_API_KEY=your-key
export EMBEDDING_MODEL=gemini-embedding-001
```

---

## Search Scopes

| Scope | Content | Storage |
|-------|---------|---------|
| `symbols` | Code functions, types, methods | `memory.db` |
| `memories` | Gotchas, decisions, patterns | `memory.db` |
| `sessions` | Session summaries | `sessions.db` |
| `codemaps` | Generated code traces | `memory.db` |
| `tasks` | Task descriptions | `tasks.db` |

---

## Hybrid Search Tuning

The `alpha` parameter balances BM25 vs vector search:

```
alpha = 0.7  → 70% BM25, 30% vector (favor exact matches)
alpha = 0.5  → 50/50 balanced (default)
alpha = 0.3  → 30% BM25, 70% vector (favor concepts)
```

---

## Troubleshooting

### Dimension Mismatch
```
Error: dimension mismatch: query 3072, stored 1024
```
Query and stored embeddings use different providers. Re-index with consistent provider.

### Zero Results
Embedding model changed since indexing. Verify `ScopeModelRecommendation()` matches storage.

### Rate Limiting (429)
Set `AGENTCTL_EMBEDDING_RATE_LIMIT=0` for paid tier or increase value.

---

## Building with Vector Support

Vector search requires CGO:

```bash
# With vector support
make build-cgo

# Without (default)
make build
```

See [gotchas.md](gotchas.md#cgo-build) for CGO build details.

---

## Cost Estimate

~$0.30/month for typical usage (3M tokens across all scopes).

---

## Related

- [Skills](skills.md) - Search skill usage
- [Gotchas](gotchas.md) - CGO build requirements
- [Storage](storage.md) - Where embeddings are stored
