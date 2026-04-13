# Phase 1: Retrieval/Query Embedding Quality

> 1 PR improving query embedding handling for better retrieval.

## Overview

This phase focuses on the retrieval side of embeddings:
- Use query-optimized embeddings when available (immediate quality win)

This PR can ship immediately with measurable improvement to search quality.

---

## PR 1.1: Use Query-Optimized Embeddings in Retrieval

### Summary

Update the semantic search retrieval path to use `EmbedQuery` when the provider supports the `QueryEmbeddingProvider` interface. This leverages Voyage's query-optimized input type for better retrieval quality.

### Background

Voyage AI (and some other providers) support different embedding modes:
- `document` - For content being indexed (optimized for being found)
- `query` - For search queries (optimized for finding)

Currently, `internal/intelligence/retrieval/semantic_search.go` always uses `Embed()`:

```go
// Current implementation (line 29)
vec, err := g.embedProvider.Embed(ctx, question)
```

The `VoyageProvider` already implements `EmbedQuery`:

```go
// internal/intelligence/indexing/semantic/provider_voyage.go (line 411)
func (p *VoyageProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    embeddings, err := p.doEmbedRequest(ctx, []string{query}, "query")
    // ...
}
```

We need to wire these together.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/intelligence/retrieval/semantic_search.go` | Modify | Use EmbedQuery when available |
| `internal/intelligence/retrieval/semantic_search_test.go` | Create | Unit tests for query embedding selection |
| `internal/intelligence/retrieval/generator.go` | Modify (minor) | Ensure embed provider is accessible |

### Implementation Details

#### `internal/intelligence/retrieval/semantic_search.go`

```go
package retrieval

import (
    "context"
    "path/filepath"
    "strings"

    "github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
    "github.com/jkatigb/agentctl/internal/platform/config"
    "github.com/jkatigb/agentctl/internal/storage/dbdriver"
    "github.com/jkatigb/agentctl/internal/storage/memory"
)

// searchSemanticIndex searches the semantic index using vector similarity.
// Falls back to BM25 if vector search is not available.
//
// Index:
//   Purpose: Find code using vector similarity search
//   Related: Generator, EmbeddingProvider, QueryEmbeddingProvider
//   Keywords: semantic search, vector search, hybrid search
func (g *Generator) searchSemanticIndex(ctx context.Context, workspaceID, question string, limit int) ([]Candidate, error) {
    if g.embedProvider == nil {
        g.logger.Debug().Msg("no embedding provider, skipping semantic search")
        return []Candidate{}, nil
    }

    // Check if we have a SearchableStore for advanced search
    if g.searchableStore == nil {
        g.logger.Debug().Msg("no searchable store, trying BM25 fallback")
        return g.semanticBM25Fallback(ctx, workspaceID, question, limit)
    }

    searchable := g.searchableStore

    // Get embedding for the question - use query-optimized if available
    vec, err := g.embedForQuery(ctx, question)
    if err != nil {
        g.logger.Warn().Err(err).Msg("embedding failed, falling back to BM25")
        return g.semanticBM25Fallback(ctx, workspaceID, question, limit)
    }

    // Convert to dbdriver.Vector
    queryVec := dbdriver.Vector(vec)

    // Try hybrid search (BM25 + Vector)
    results, err := searchable.SearchHybrid(ctx, question, queryVec, workspaceID, limit*2)
    if err != nil {
        g.logger.Debug().Err(err).Msg("hybrid search failed, trying BM25")
        // Fall back to BM25 only
        results, err = searchable.SearchBM25(ctx, question, workspaceID, limit*2)
        if err != nil {
            return nil, err
        }
    }

    // Convert to candidates
    return g.memoryResultsToCandidates(results, limit)
}

// embedForQuery generates an embedding for a search query.
// Uses EmbedQuery if the provider supports it (e.g., Voyage with input_type=query),
// otherwise falls back to standard Embed.
//
// The query mode can be controlled via:
//   - EMBED_QUERY_MODE env var (auto|embed|embed_query)
//   - config.Embedding.Flags.QueryMode
//
// Index:
//   Purpose: Generate query-optimized embeddings for search
//   Related: QueryEmbeddingProvider, EmbeddingProvider, searchSemanticIndex
//   Keywords: query embedding, EmbedQuery, retrieval
func (g *Generator) embedForQuery(ctx context.Context, question string) ([]float32, error) {
    // Resolve the query mode from config/env
    queryMode := config.ResolveEmbedQueryMode(g.embedQueryMode)

    switch queryMode {
    case config.EmbedQueryModeEmbed:
        // Force standard Embed
        g.logger.Debug().Msg("using Embed (forced by query_mode=embed)")
        return g.embedProvider.Embed(ctx, question)

    case config.EmbedQueryModeEmbedQuery:
        // Force EmbedQuery - error if not supported
        if qp, ok := g.embedProvider.(semantic.QueryEmbeddingProvider); ok {
            g.logger.Debug().Msg("using EmbedQuery (forced by query_mode=embed_query)")
            return qp.EmbedQuery(ctx, question)
        }
        g.logger.Warn().Msg("EmbedQuery forced but provider doesn't support it, falling back to Embed")
        return g.embedProvider.Embed(ctx, question)

    case config.EmbedQueryModeAuto:
        fallthrough
    default:
        // Auto: use EmbedQuery if available
        if qp, ok := g.embedProvider.(semantic.QueryEmbeddingProvider); ok {
            g.logger.Debug().
                Str("provider", g.embedProvider.Model()).
                Msg("using EmbedQuery (auto-detected)")
            return qp.EmbedQuery(ctx, question)
        }
        g.logger.Debug().
            Str("provider", g.embedProvider.Model()).
            Msg("using Embed (provider doesn't support EmbedQuery)")
        return g.embedProvider.Embed(ctx, question)
    }
}

// Rest of file unchanged...
```

#### Generator struct update (`internal/intelligence/retrieval/generator.go`)

Add the query mode field to the Generator:

```go
type Generator struct {
    // ... existing fields ...

    // embedQueryMode controls query embedding behavior.
    // Resolved from config.Embedding.Flags.QueryMode.
    embedQueryMode config.EmbedQueryMode
}

// GeneratorOption for setting query mode
func WithEmbedQueryMode(mode config.EmbedQueryMode) GeneratorOption {
    return func(g *Generator) {
        g.embedQueryMode = mode
    }
}
```

#### `internal/intelligence/retrieval/semantic_search_test.go`

```go
package retrieval

import (
    "context"
    "testing"

    "github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
    "github.com/jkatigb/agentctl/internal/platform/config"
    "github.com/rs/zerolog"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// mockEmbeddingProvider implements EmbeddingProvider only.
type mockEmbeddingProvider struct {
    embedCalls      int
    embedQueryCalls int
    model           string
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
    m.embedCalls++
    return make([]float32, 1024), nil
}

func (m *mockEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
    result := make([][]float32, len(texts))
    for i := range result {
        m.embedCalls++
        result[i] = make([]float32, 1024)
    }
    return result, nil
}

func (m *mockEmbeddingProvider) Model() string {
    return m.model
}

func (m *mockEmbeddingProvider) Dimensions() int {
    return 1024
}

// mockQueryEmbeddingProvider implements both EmbeddingProvider and QueryEmbeddingProvider.
type mockQueryEmbeddingProvider struct {
    mockEmbeddingProvider
}

func (m *mockQueryEmbeddingProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    m.embedQueryCalls++
    return make([]float32, 1024), nil
}

func TestEmbedForQuery_AutoMode_UsesEmbedQuery(t *testing.T) {
    // Given: a provider that supports QueryEmbeddingProvider
    mock := &mockQueryEmbeddingProvider{
        mockEmbeddingProvider: mockEmbeddingProvider{model: "voyage-code-3"},
    }
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeAuto,
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: EmbedQuery should be used
    assert.Equal(t, 1, mock.embedQueryCalls, "EmbedQuery should be called")
    assert.Equal(t, 0, mock.embedCalls, "Embed should not be called")
}

func TestEmbedForQuery_AutoMode_FallsBackToEmbed(t *testing.T) {
    // Given: a provider that does NOT support QueryEmbeddingProvider
    mock := &mockEmbeddingProvider{model: "gemini-embedding-001"}
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeAuto,
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: Embed should be used as fallback
    assert.Equal(t, 1, mock.embedCalls, "Embed should be called")
}

func TestEmbedForQuery_EmbedMode_ForcesEmbed(t *testing.T) {
    // Given: a provider that supports QueryEmbeddingProvider
    mock := &mockQueryEmbeddingProvider{
        mockEmbeddingProvider: mockEmbeddingProvider{model: "voyage-code-3"},
    }
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeEmbed, // Force standard Embed
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: Embed should be used despite provider supporting EmbedQuery
    assert.Equal(t, 1, mock.embedCalls, "Embed should be called")
    assert.Equal(t, 0, mock.embedQueryCalls, "EmbedQuery should not be called")
}

func TestEmbedForQuery_EmbedQueryMode_ForcesEmbedQuery(t *testing.T) {
    // Given: a provider that supports QueryEmbeddingProvider
    mock := &mockQueryEmbeddingProvider{
        mockEmbeddingProvider: mockEmbeddingProvider{model: "voyage-code-3"},
    }
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeEmbedQuery, // Force EmbedQuery
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: EmbedQuery should be used
    assert.Equal(t, 1, mock.embedQueryCalls, "EmbedQuery should be called")
    assert.Equal(t, 0, mock.embedCalls, "Embed should not be called")
}

func TestEmbedForQuery_EmbedQueryMode_FallsBackWhenUnsupported(t *testing.T) {
    // Given: a provider that does NOT support QueryEmbeddingProvider
    mock := &mockEmbeddingProvider{model: "gemini-embedding-001"}
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeEmbedQuery, // Force EmbedQuery
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: Embed should be used as fallback (with warning logged)
    assert.Equal(t, 1, mock.embedCalls, "Embed should be called as fallback")
}

func TestEmbedForQuery_EnvOverridesConfig(t *testing.T) {
    // Given: config says "embed" but env says "auto"
    t.Setenv("EMBED_QUERY_MODE", "auto")

    mock := &mockQueryEmbeddingProvider{
        mockEmbeddingProvider: mockEmbeddingProvider{model: "voyage-code-3"},
    }
    g := &Generator{
        embedProvider:  mock,
        embedQueryMode: config.EmbedQueryModeEmbed, // Config says embed
        logger:         zerolog.Nop(),
    }

    // When: embedding a query
    _, err := g.embedForQuery(context.Background(), "find auth handler")
    require.NoError(t, err)

    // Then: EmbedQuery should be used (env overrides config)
    assert.Equal(t, 1, mock.embedQueryCalls, "EmbedQuery should be called")
    assert.Equal(t, 0, mock.embedCalls, "Embed should not be called")
}
```

### Testing Strategy

1. **Unit tests with mock providers** (as shown above):
   - Auto mode uses EmbedQuery when available
   - Auto mode falls back to Embed when not available
   - Embed mode forces Embed
   - EmbedQuery mode forces EmbedQuery (with fallback warning)
   - Env var overrides config

2. **Integration test** (optional):
   ```go
   func TestSemanticSearch_UsesQueryEmbedding_Integration(t *testing.T) {
       if os.Getenv("VOYAGE_API_KEY") == "" {
           t.Skip("VOYAGE_API_KEY not set")
       }
       // Create real VoyageProvider
       // Run searchSemanticIndex
       // Verify results are returned (quality is subjective)
   }
   ```

3. **Manual A/B testing**:
   - Run same queries with `EMBED_QUERY_MODE=embed` vs `EMBED_QUERY_MODE=auto`
   - Compare result quality (subjective but informative)

### Acceptance Criteria

- [ ] `embedForQuery` method added to Generator
- [ ] Query mode resolution respects env > config > default
- [ ] Auto mode uses EmbedQuery when provider supports it
- [ ] Logs indicate which mode was used (debug level)
- [ ] All unit tests pass
- [ ] No breaking changes to existing behavior (default is auto)
