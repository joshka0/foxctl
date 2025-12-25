# Semantic Search Phase 3: Retrieval Integration Plan

> Refactor `code/semantic_search` to use `internal/retrieval` infrastructure instead of ad-hoc search.

## Current State (Phase 2)

The skill implements custom symbol/session search with:
- Manual embedding generation (Gemini-only)
- Custom cosine similarity computation
- Ad-hoc RRF fusion
- No BM25 fallback (despite skill.yaml claim)

## Target State (Phase 3)

Leverage existing `internal/retrieval` infrastructure:
- `Generator.Generate()` for symbols + semantic + ripgrep
- `SearchableStore.SearchHybrid/SearchBM25` for FTS5 + vector
- Built-in RRF fusion and fallbacks
- Session search as additional source

---

## Architecture

```
code/semantic_search skill
│
├── Input validation + PathValidator
│
├── Symbol/Semantic search ──→ retrieval.Generator.Generate()
│   ├── searchSymbolIndex() ──→ symbol candidates
│   ├── searchSemanticIndex()
│   │   ├── SearchHybrid() (BM25 + vector + RRF)
│   │   └── BM25 fallback when no embeddings
│   └── searchRipgrep() ──→ ripgrep fallback
│
├── Session search ──→ sessions.Store.SearchSimilar()
│   └── (keep separate - different data model)
│
├── Memories search ──→ memory.SearchableStore.SearchHybrid()
│   └── (future: when memory embeddings exist)
│
├── ID normalization + dedup
│   ├── symbol:<workspace>:<path>#<span>
│   ├── session:<id>
│   └── memory:<name>
│
└── RRF fusion across sources → Output
```

---

## Implementation Tasks

### 1. Refactor to use retrieval.Generator

**File:** `skills/code_semantic_search/main.go`

```go
// Create generator with embed provider
gen := retrieval.NewGenerator(
    memStore,           // memory.Store for symbol index
    searchableStore,    // memory.SearchableStore for hybrid search
    embedProvider,      // semantic.EmbeddingProvider (or nil for BM25-only)
    workspacePath,
    logger,
)

// Generate candidates (symbols + semantic + ripgrep)
opts := retrieval.Options{
    EnableSymbols:        scopeSet[ScopeSymbols],
    EnableSemantic:       scopeSet[ScopeSymbols], // uses same index
    EnableRipgrep:        true,                   // fallback
    MaxTotalCandidates:   in.Limit * 3,
    MaxSymbolCandidates:  in.Limit * 2,
    MaxSemanticCandidates: in.Limit * 2,
}

result, err := gen.Generate(ctx, workspaceID, in.Query, opts)
```

### 2. Keep session search separate

Sessions have different data model (summaries, gotchas) - keep existing `searchSessions()` but add timeout:

```go
if scopeSet[ScopeSessions] {
    sourceCtx, cancel := context.WithTimeout(ctx, DefaultSourceTimeout)
    defer cancel()
    sessionResults, err := searchSessions(sourceCtx, storageRoot, queryEmbedding, in.Limit*2)
    // ... handle results
}
```

### 3. Add memories scope (future)

When memory embeddings exist:

```go
if scopeSet[ScopeMemories] && searchableStore != nil {
    sourceCtx, cancel := context.WithTimeout(ctx, DefaultSourceTimeout)
    defer cancel()
    memResults, err := searchableStore.SearchHybrid(sourceCtx, in.Query, queryVec, workspaceID, in.Limit*2)
    // ... convert to Result
}
```

### 4. ID normalization

Apply canonical IDs when converting candidates to results:

```go
func normalizeID(source, workspaceID string, candidate retrieval.Candidate) string {
    switch source {
    case "symbol":
        // symbol:<workspace>:<path>#<span>
        span := ""
        if candidate.StartLine > 0 {
            span = fmt.Sprintf("#L%d", candidate.StartLine)
        }
        return fmt.Sprintf("symbol:%s:%s%s", workspaceID, candidate.Path, span)
    case "session":
        return fmt.Sprintf("session:%s", candidate.ID)
    case "memory":
        return fmt.Sprintf("memory:%s", candidate.Name)
    }
    return candidate.ID
}
```

### 5. Provider-agnostic embedding

Read from config, default to Gemini:

```go
func createEmbedProvider(cfg config.Config) (semantic.EmbeddingProvider, error) {
    // Check config for provider preference
    providerName := cfg.Get("embedding.provider", "gemini")

    switch providerName {
    case "gemini":
        apiKey := os.Getenv("GEMINI_API_KEY")
        if apiKey == "" {
            return nil, nil // BM25-only mode
        }
        return semantic.NewGeminiProvider(semantic.GeminiConfig{
            APIKey: apiKey,
            Model:  cfg.Get("embedding.model", "gemini-embedding-001"),
        })
    // Future: openai, voyage, local
    default:
        return nil, fmt.Errorf("unknown embedding provider: %s", providerName)
    }
}
```

### 6. Timeouts per source

```go
const (
    DefaultSourceTimeout = 500 * time.Millisecond
    DefaultTotalTimeout  = 2 * time.Second
)

// Wrap entire search with total timeout
searchCtx, cancel := context.WithTimeout(ctx, DefaultTotalTimeout)
defer cancel()

// Generator.Generate already handles internal timeouts
// Session search gets its own timeout (see task 2)
```

---

## File Changes

| File | Change |
|------|--------|
| `skills/code_semantic_search/main.go` | Refactor to use `retrieval.Generator` |
| `skills/code_semantic_search/main.go` | Add ID normalization |
| `skills/code_semantic_search/main.go` | Provider-agnostic embedding creation |
| `skills/code_semantic_search/skill.yaml` | Update help text for BM25 fallback |
| `internal/retrieval/candidates.go` | (optional) Add session source |

---

## Testing

1. **No API key** → BM25-only results for symbols
2. **With API key** → Hybrid (BM25 + vector) results
3. **Sessions scope** → Session results with context hints
4. **Mixed scopes** → RRF fusion across sources
5. **Timeout handling** → Graceful degradation on slow sources

---

## Migration Notes

- Workspace ID derivation already fixed (SHA256 hash)
- Existing embeddings may not match new workspace ID
- Re-indexing may be needed after migration

---

## Dependencies

```
internal/retrieval
├── Generator
├── Candidate
├── GenerateResult
└── Options

internal/storage/memory
├── Store
├── SearchableStore
└── SearchResult

internal/indexing/semantic
└── EmbeddingProvider (interface)
```

---

## Future Enhancements

1. **Turso/libSQL native vectors** - Replace current vector storage with `F32_BLOB` columns
2. **Multi-provider support** - OpenAI, Voyage, local embeddings
3. **Weighted BM25** - Column weighting via `bm25(table, w1, w2...)`
4. **Memory embeddings** - Enable memories scope with vector search
