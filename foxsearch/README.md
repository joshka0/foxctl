# foxsearch

Semantic-search building blocks extracted from foxctl: embedding providers,
approximate/exact vector search, and reciprocal-rank fusion. Zero dependency
on foxctl internals — usable standalone.

## Packages

| Package      | Purpose                                                        |
| ------------ | -------------------------------------------------------------- |
| `embed`      | Embedding provider clients (OpenAI-compatible, Gemini, Mistral, Codestral), the `Embedder` facade (provider selection, rate-limit wait, guard hooks), scope-based model recommendations, and dimension resolution. |
| `vector`     | Exact brute-force cosine search, turbovec-backed ANN search with exact rerank, float32 serialization helpers. |
| `turbovec`   | Client for the turbovec sidecar (ANN index daemon) and the per-workspace `VectorIndex`. |
| `searchrank` | Reciprocal-rank fusion (RRF) over multiple scored candidate lists. |

## Host integration hooks

foxsearch stays free of host-specific concerns through three injection points:

- `embed.SetObserver(embed.Observer)` — receive an `embed.Event` after every
  provider API call (duration, tokens, cost, status). Hosts map this to their
  own telemetry (foxctl maps it to foxcular observability events).
- `embed.SetDefaultDimensionsFunc(func() int)` / `embed.SetDefaultDimensions(n)` —
  resolve the fallback vector dimension (foxctl wires `FOXCTL_VECTOR_DIMS`).
- `turbovec.NewVectorIndex(workspaceID, ...)` takes the workspace ID verbatim;
  hosts canonicalize path-like IDs before calling (foxctl uses
  `workspace.CanonicalID`).

## Environment variables

- `FOXCTL_EMBEDDING_MODEL_<SCOPE>`, `FOXCTL_EMBEDDING_MODEL_CODE`,
  `FOXCTL_EMBEDDING_MODEL_TEXT` — per-scope model overrides in
  `embed.ScopeModelRecommendation` (retained names for foxctl compatibility;
  will be renamed with a `FOXSEARCH_` prefix in a future major version).

## Error types

`embed.EmbeddingServiceError` classifies provider failures
(`connection_refused`, `timeout`, `http_<status>`) with actionable hints.
