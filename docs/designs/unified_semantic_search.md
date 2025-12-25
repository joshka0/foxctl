# Unified Semantic Search Design

> Improving swe_grep with cross-domain semantic search across code symbols,
> sessions, and memories.

## Goals

1. **Accurate semantic search** - Find relevant code without digging through
   everything
2. **Context surfacing** - Surface related sessions/memories when searching
   ("you solved this before")
3. **Automatic embedding updates** - Keep embeddings fresh as code changes
4. **Single search API** - Unified interface for all search domains

## Current Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      swe_grep (current)                         │
│                   code/swe_grep skill                           │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                    ┌─────────────────┐
                    │  Code Symbols   │
                    │  (BM25 search)  │
                    └─────────────────┘
```

**Gaps:**

- Symbol embeddings exist in schema but not generated (queue disabled by
  default)
- Sessions have embeddings but swe_grep doesn't query them
- Memories can have embeddings but rarely populated
- No cross-domain search or context surfacing

## Proposed Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Unified Search API                                │
│                         code/semantic_search                                │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
        ┌──────────────┬──────────────┼──────────────┬──────────────┐
        ▼              ▼              ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│Code Symbols │ │  Sessions   │ │  Memories   │ │   Tasks     │ │ File Chunks │
│(named_memory│ │(sessions.db)│ │(named_memory│ │ (tasks.db)  │ │(named_memory│
│             │ │             │ │             │ │             │ │             │
│BM25 + Vector│ │Vector search│ │Vector search│ │Vector search│ │Vector search│
└─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘ └─────────────┘
        │              │              │              │              │
        └──────────────┴──────────────┼──────────────┴──────────────┘
                                      ▼
                           ┌──────────────────┐
                           │  Ranked Results  │
                           │  with Context    │
                           │  Hints           │
                           └──────────────────┘
```

## Implementation Phases

### Phase 1: Enable Embedding Pipeline

**Effort:** Low (1-2 days) **Impact:** High - enables all downstream features

> Use the existing embedding queue/pipeline shared with semantic file index +
> symbol index (post-review and live-index hooks). Do **not** introduce a new
> queue; the embedding worker consumes that shared queue.

#### 1.1 Enable embedding queue by default

```bash
# .claude/hooks/live-index.sh
# Change from:
embed_queue="${AGENTCTL_EMBED_QUEUE:-0}"
# To:
embed_queue="${AGENTCTL_EMBED_QUEUE:-1}"
```

#### 1.2 Add background embedding worker

Create `skills/embedding_worker/main.go`:

```go
type Input struct {
    BatchSize   int `json:"batch_size,omitempty"`   // Default: 10
    MaxDuration int `json:"max_duration,omitempty"` // Seconds, default: 300
}

type Output struct {
    Processed int    `json:"processed"`
    Remaining int    `json:"remaining"`
    Errors    int    `json:"errors"`
    Status    string `json:"status"` // "completed", "timeout", "error"
}
```

Features:

- Process queued symbols in batches
- Exponential backoff on API errors
- Timeout to prevent runaway execution
- Can be run as background job

#### 1.3 Hook integration

Add to `.claude/settings.json`:

```json
{
  "hooks": {
    "Stop": [
      {
        "type": "command",
        "command": ".claude/hooks/embedding-flush.sh"
      }
    ]
  }
}
```

### Phase 2: Unified Search Skill

**Effort:** Medium (3-4 days) **Impact:** High - main user-facing feature

#### 2.1 Create unified search skill

Create `skills/code_semantic_search/`:

```go
// main.go
type Input struct {
    Query         string   `json:"query"`
    Scope         []string `json:"scope,omitempty"`         // ["symbols", "sessions", "memories", "tasks"]
    Workspace     string   `json:"workspace,omitempty"`
    Limit         int      `json:"limit,omitempty"`         // Default: 20
    MinSimilarity float64  `json:"min_similarity,omitempty"` // Default: 0.3
    IncludeContext bool    `json:"include_context,omitempty"` // Include session/task hints
    Summarize     bool     `json:"summarize,omitempty"`     // Send candidates to LLM for synthesis
    SummarizeModel string  `json:"summarize_model,omitempty"` // Override default model
}

type Output struct {
    Query         string           `json:"query"`
    Results       []SearchResult   `json:"results"`
    ContextHints  []ContextHint    `json:"context_hints,omitempty"`
    SearchStats   SearchStats      `json:"stats"`
    Summary       *SynthesisSummary `json:"summary,omitempty"` // Present when summarize=true
}

// SynthesisSummary contains the LLM-generated synthesis of search results.
type SynthesisSummary struct {
    Answer      string   `json:"answer"`       // Direct answer to the query
    KeyInsights []string `json:"key_insights"` // Important findings from results
    Gotchas     []string `json:"gotchas"`      // Warnings or caveats
    NextSteps   []string `json:"next_steps"`   // Suggested follow-up actions
    Model       string   `json:"model"`        // Model used for synthesis
    TokensUsed  int      `json:"tokens_used"`  // Approximate tokens consumed
}

type SearchResult struct {
    Source     string  `json:"source"`     // "symbol", "session", "memory"
    ID         string  `json:"id"`
    Name       string  `json:"name"`
    Path       string  `json:"path,omitempty"`
    Snippet    string  `json:"snippet,omitempty"`
    Summary    string  `json:"summary,omitempty"`
    Similarity float64 `json:"similarity"`
    Rank       int     `json:"rank"`
}

type ContextHint struct {
    Type      string   `json:"type"`      // "past_solution", "gotcha", "decision"
    SessionID string   `json:"session_id"`
    Summary   string   `json:"summary"`
    Items     []string `json:"items,omitempty"` // Gotchas, decisions, etc.
}
```

#### 2.2 Search algorithm

```go
func (s *Searcher) Search(ctx context.Context, input Input) (*Output, error) {
    // 1. Generate query embedding
    queryVec, err := s.embedProvider.Embed(ctx, input.Query)

    // 2. Parallel search across enabled scopes
    var wg sync.WaitGroup
    results := make(chan []SearchResult, 3)

    if contains(input.Scope, "symbols") {
        wg.Add(1)
        go func() {
            defer wg.Done()
            r := s.searchSymbols(ctx, input.Query, queryVec, input.Limit)
            results <- r
        }()
    }

    if contains(input.Scope, "sessions") {
        wg.Add(1)
        go func() {
            defer wg.Done()
            r := s.searchSessions(ctx, queryVec, input.Limit)
            results <- r
        }()
    }

    if contains(input.Scope, "memories") {
        wg.Add(1)
        go func() {
            defer wg.Done()
            r := s.searchMemories(ctx, queryVec, input.Limit)
            results <- r
        }()
    }

    if contains(input.Scope, "tasks") {
        wg.Add(1)
        go func() {
            defer wg.Done()
            r := s.searchTasks(ctx, queryVec, input.Workspace, input.Limit)
            results <- r
        }()
    }

    // 3. Collect and rank using reciprocal rank fusion
    allResults := collectResults(results)
    ranked := reciprocalRankFusion(allResults)

    // 4. Extract context hints from session matches
    hints := extractContextHints(ranked)

    output := &Output{
        Query:        input.Query,
        Results:      ranked[:min(input.Limit, len(ranked))],
        ContextHints: hints,
    }

    // 5. Optional: Synthesize results with LLM
    if input.Summarize {
        summary, err := s.synthesizeResults(ctx, input.Query, output.Results, input.SummarizeModel)
        if err == nil {
            output.Summary = summary
        }
    }

    return output
}
```

#### 2.3 Result synthesis with LLM

When `summarize=true`, send the top results to an LLM for synthesis:

```go
// synthesizeResults sends search results to an LLM for intelligent synthesis.
// Uses OpenRouter by default (devstral free tier), falls back to other providers.
func (s *Searcher) synthesizeResults(ctx context.Context, query string, results []SearchResult, model string) (*SynthesisSummary, error) {
    // Build prompt with search results
    prompt := buildSynthesisPrompt(query, results)

    // Get LLM provider (same priority as session_summarize)
    // Priority: OpenRouter → Groq → Cerebras
    provider := s.getLLMProvider(model)
    if provider == nil {
        return nil, fmt.Errorf("no LLM provider available for synthesis")
    }

    // Call LLM with structured output
    resp, err := provider.Complete(ctx, prompt, SynthesisSchema)
    if err != nil {
        return nil, fmt.Errorf("synthesis failed: %w", err)
    }

    return parseSynthesisResponse(resp, provider.Name)
}

func buildSynthesisPrompt(query string, results []SearchResult) string {
    var sb strings.Builder
    sb.WriteString("You are a code analysis assistant. Synthesize these search results to answer the user's question.\n\n")
    sb.WriteString("## User Question\n")
    sb.WriteString(query)
    sb.WriteString("\n\n## Search Results\n\n")

    for i, r := range results {
        fmt.Fprintf(&sb, "### Result %d: %s (%s)\n", i+1, r.Name, r.Source)
        if r.Path != "" {
            fmt.Fprintf(&sb, "**Path:** %s\n", r.Path)
        }
        if r.Snippet != "" {
            fmt.Fprintf(&sb, "```\n%s\n```\n", r.Snippet)
        }
        if r.Summary != "" {
            fmt.Fprintf(&sb, "%s\n", r.Summary)
        }
        sb.WriteString("\n")
    }

    sb.WriteString("## Instructions\n")
    sb.WriteString("Provide a structured response with:\n")
    sb.WriteString("- answer: Direct answer to the question\n")
    sb.WriteString("- key_insights: Important findings (2-5 items)\n")
    sb.WriteString("- gotchas: Warnings or caveats to be aware of\n")
    sb.WriteString("- next_steps: Suggested follow-up actions\n")

    return sb.String()
}
```

**LLM Provider Priority:**

| Provider | Model | Cost | Context | Notes |
|----------|-------|------|---------|-------|
| OpenRouter | mistralai/devstral-2505:free | Free | 32k | Default, fast, good at code |
| OpenRouter | meta-llama/llama-3.3-8b-instruct:free | Free | 8k | Fallback |
| Groq | llama-3.3-70b-versatile | Free tier | 32k | Fast inference |
| Cerebras | llama-3.3-70b | Free tier | 8k | Very fast |

**Environment variables:**
- `OPENROUTER_API_KEY` - Enables OpenRouter providers
- `OPENROUTER_MODELS` - Comma-separated model list (optional override)
- `GROQ_API_KEY` - Enables Groq provider
- `CEREBRAS_API_KEY` - Enables Cerebras provider

#### 2.4 Reciprocal rank fusion

Combine results from different sources:

```go
func reciprocalRankFusion(sources [][]SearchResult) []SearchResult {
    const k = 60 // RRF constant
    // Per-source weights: code symbols highest, then tasks/sessions, then memories
    weights := map[string]float64{
        "symbol":  1.0,
        "task":    0.95, // Tasks are high-value for understanding work context
        "session": 0.9,
        "memory":  0.7,
    }
    scores := make(map[string]float64)
    items := make(map[string]SearchResult)

    for _, source := range sources {
        for rank, result := range source {
            key := result.Source + ":" + result.ID
            w := weights[result.Source]
            if w == 0 {
                w = 1.0
            }
            scores[key] += w * (1.0 / float64(k+rank+1))
            if _, ok := items[key]; !ok {
                items[key] = result
            }
        }
    }

    // Sort by fused score
    var results []SearchResult
    for key, result := range items {
        result.Similarity = scores[key]
        results = append(results, result)
    }
    sort.Slice(results, func(i, j int) bool {
        return results[i].Similarity > results[j].Similarity
    })

    return results
}
```

### Phase 3: swe_grep Integration

**Effort:** Medium (2-3 days) **Impact:** Medium - enhances existing workflow

#### 3.1 Add context to swe_grep output

Modify `skills/code_swe_grep/main.go`:

```go
type Output struct {
    // Existing fields
    Question   string      `json:"question"`
    Candidates []Candidate `json:"candidates"`
    Sources    string      `json:"sources"`
    Total      int         `json:"total"`

    // NEW: Context from related sessions
    RelatedSessions []SessionContext `json:"related_sessions,omitempty"`
    RelatedMemories []MemoryContext  `json:"related_memories,omitempty"`
}

type SessionContext struct {
    SessionID string   `json:"session_id"`
    Summary   string   `json:"summary"`
    Gotchas   []string `json:"gotchas,omitempty"`
    Decisions []string `json:"decisions,omitempty"`
    KeyFiles  []string `json:"key_files,omitempty"`
}
```

#### 3.2 Integration flow

```go
func main() {
    // ... existing candidate generation ...

    // NEW: If we have embeddings, search for related sessions
    if geminiKey != "" && len(input.Question) > 0 {
        queryVec, err := generateEmbedding(ctx, geminiKey, input.Question)
        if err == nil {
            sessions := searchRelatedSessions(ctx, queryVec, 3)
            output.RelatedSessions = sessions
        }
    }

    // Output includes context hints
}
```

### Phase 4: Task Embeddings

**Effort:** Low (1-2 days) **Impact:** Medium - enables task-aware search

Tasks (todos) contain valuable context about work history, gotchas learned, and
decisions made. Embedding tasks enables semantic queries like:
- "Find tasks related to authentication"
- "What gotchas did we learn about rate limiting?"
- "Show completed tasks similar to this problem"

#### 4.1 Task embedding content

Combine task fields for rich semantic representation:

```go
// taskEmbeddingContent generates the text to embed for a task.
func taskEmbeddingContent(task *tasks.Task) string {
    var parts []string
    parts = append(parts, task.Title)
    if task.Description != "" {
        parts = append(parts, task.Description)
    }
    if task.Notes != "" {
        parts = append(parts, "Implementation notes: "+task.Notes)
    }
    if task.Gotchas != "" {
        parts = append(parts, "Gotchas learned: "+task.Gotchas)
    }
    return strings.Join(parts, "\n\n")
}
```

#### 4.2 Storage in named_memory

Store task embeddings alongside other named memory entries:

```go
// Store task embedding
name := fmt.Sprintf("task://%s", task.ID)
entry := memory.Entry{
    Name:      name,
    Type:      "task_embedding",
    Workspace: task.WorkspaceID,
    Summary:   task.Title,
    Embedding: embedding,
}
store.SaveWithEmbedding(ctx, entry, embedding, embeddingModel)
```

#### 4.3 Task embedding skill

Create `skills/embedding_tasks/main.go`:

```go
type Input struct {
    Scope     string `json:"scope"`     // "all", "pending", "completed", "workspace"
    Name      string `json:"name"`      // workspace_id or task_id for single refresh
    BatchSize int    `json:"batch_size"` // Default: 10
}

type Output struct {
    Processed int    `json:"processed"`
    Skipped   int    `json:"skipped"`   // Already embedded
    Errors    int    `json:"errors"`
    Status    string `json:"status"`
}
```

#### 4.4 Hook integration for task updates

Add to `.claude/hooks/task-embed.sh`:

```bash
#!/bin/bash
# Triggered on task completion to embed gotchas/notes
AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
input=$(cat)

operation=$(echo "$input" | jq -r '.tool_input.operation // empty')
task_id=$(echo "$input" | jq -r '.tool_input.complete.id // .tool_input.update.id // empty')

if [[ "$operation" == "complete" || "$operation" == "update" ]] && [[ -n "$task_id" ]]; then
    "$AGENTCTL_BIN" run embedding/tasks --input "{\"scope\": \"task\", \"name\": \"$task_id\"}" &
fi

echo '{"decision": "approve"}'
```

### Phase 5: Automatic Embedding Updates

**Effort:** Low (1 day) **Impact:** Medium - keeps embeddings fresh

#### 5.1 Existing hooks (already working)

| Hook                | Trigger     | Action                  |
| ------------------- | ----------- | ----------------------- |
| `live-index.sh`     | File edit   | Queue symbol embeddings |
| `session-save.sh`   | Pre-compact | Save session state      |
| `session-summarize` | Session end | Generate session embed  |

#### 5.2 New hook: memory embedding refresh

Create `.claude/hooks/memory-embed.sh`:

```bash
#!/bin/bash
# Triggered when memories are updated
# Refreshes embeddings for modified memories

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
input=$(cat)

# Extract memory operation from hook input
operation=$(echo "$input" | jq -r '.tool_input.operation // empty')

if [[ "$operation" == "set" || "$operation" == "append" ]]; then
    name=$(echo "$input" | jq -r '.tool_input.name // empty')
    if [[ -n "$name" ]]; then
        "$AGENTCTL_BIN" run embedding/refresh --input "{\"scope\": \"memory\", \"name\": \"$name\"}" &
    fi
fi

echo '{"decision": "approve"}'
```

### Phase 6: Native Vector/Turso Integration

**Effort:** Medium (2-3 days) **Impact:** High - enables production-grade vector
search at scale

Native vector support via Turso/libsql provides:

- **F32_BLOB columns** for efficient vector storage
- **vector_top_k** for indexed ANN search
- **vector_distance_cos/l2** for similarity calculation
- **Embedded replicas** for local caching with sync

#### 6.1 Storage driver (already implemented)

The `internal/storage/dbdriver/turso.go` provides:

```go
type TursoConfig struct {
    URL                string
    AuthToken          string
    DatabaseName       string
    EnableVectorSearch bool
    VectorDimensions   int
}

// Opens with embedded replica for local caching
db, err := openTurso(ctx, cfg, migrationFunc)
```

Build tags: `//go:build cgo && !race && !vector`

#### 6.2 Embedding metadata tracking

Track provider/model/dimensions per workspace to detect dimension mismatches:

```sql
CREATE TABLE IF NOT EXISTS embedding_metadata (
    workspace TEXT PRIMARY KEY,
    provider TEXT NOT NULL,    -- e.g., "gemini"
    model TEXT NOT NULL,       -- e.g., "gemini-embedding-001"
    dimensions INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
```

Methods on `memory.Store`:

```go
// Validate dimensions match existing embeddings for workspace
func (s *Store) ValidateEmbeddingDimensions(ctx, workspace, dims) error

// Get/set embedding metadata
func (s *Store) GetEmbeddingMetadata(ctx, workspace) (*EmbeddingMetadata, error)
func (s *Store) SetEmbeddingMetadata(ctx, meta EmbeddingMetadata) error
```

#### 6.3 VectorHelper for SQL expressions

The `VectorHelper` generates SQL expressions for vector operations:

```go
vh, _ := NewVectorHelper(db)

// Generate expressions
vh.VectorExpression(vec)           // vector('[0.1,0.2,...]')
vh.CosineSimilarity("col", query)  // vector_distance_cos(col, '[...]')
vh.EuclideanDistance("col", query) // vector_distance_l2(col, '[...]')
vh.VectorTopK("idx", query, 10)    // vector_top_k('idx', '[...]', 10)
vh.ExtractVector("col")            // vector_extract(col)
```

#### 6.4 Platform config integration

Database settings in `~/.agentctl/config.yaml`:

```yaml
database:
  driver: turso # "sqlite" (default), "libsql", or "turso"
  turso:
    url: libsql://your-db.turso.io # or TURSO_DATABASE_URL env
    auth_token: "" # or TURSO_AUTH_TOKEN env
  vector:
    enabled: true
    dimensions: 3072 # matches embedding.dimensions by default
```

Environment variable overrides:

- `TURSO_DATABASE_URL` - Turso database URL
- `TURSO_AUTH_TOKEN` - Turso authentication token
- Auto-detects driver from URL when set

#### 6.5 Search fallback behavior

| Driver | Vector Support | Fallback |
| ------ | -------------- | -------- |
| SQLite | No | BM25 only via FTS5 |
| libsql | Optional (local) | BM25 if vectors disabled |
| Turso | Full (cloud) | Hybrid BM25 + vector search |

The `SearchableStore` automatically selects the best search strategy:

```go
func (s *SearchableStore) HybridSearch(ctx, query, opts) ([]Result, error) {
    if s.vectorEnabled {
        // Native vector_top_k + BM25 fusion
        return s.hybridVectorBM25(ctx, query, opts)
    }
    // Fallback: FTS5 BM25 only
    return s.bm25Only(ctx, query, opts)
}
```

#### 6.6 Tests (CGO-tagged)

Tests require Turso credentials and vector-enabled group:

```bash
# Run tests with Turso credentials
TURSO_DATABASE_URL=libsql://... \
TURSO_AUTH_TOKEN=... \
TURSO_VECTOR_ENABLED=1 \
CGO_ENABLED=1 go test -v -tags 'cgo' ./internal/storage/dbdriver/...
```

Tests gracefully skip when credentials are not set.

## Configuration

### Environment variables

| Variable                   | Default | Description                    |
| -------------------------- | ------- | ------------------------------ |
| `AGENTCTL_EMBED_QUEUE`     | `1`     | Enable embedding queue         |
| `AGENTCTL_SEMANTIC_SEARCH` | `1`     | Enable unified semantic search |
| `AGENTCTL_CONTEXT_HINTS`   | `1`     | Include session context hints  |
| `GEMINI_API_KEY`           | -       | Required for embeddings        |
| `TURSO_DATABASE_URL`       | -       | Turso database URL (overrides config) |
| `TURSO_AUTH_TOKEN`         | -       | Turso authentication token     |

### Config file additions

```yaml
# ~/.agentctl/config.yaml
embedding:
  enabled: true
  provider: gemini # default; can be overridden via EMBEDDING_PROVIDER
  model: gemini-embedding-001 # EMBEDDING_MODEL overrides
  dimensions: 3072 # informational; ensure matches provider/model
  queue:
    enabled: true
    batch_size: 10
    max_retries: 3
  auto_refresh:
    on_edit: true
    on_task_complete: true
    on_session_end: true
    on_memory_update: true

search:
  default_scope:
    - symbols
    - sessions
    - memories
    - tasks
  min_similarity: 0.3
  include_context: true
  max_context_hints: 3
```

#### Embedding provider options

| Model / Provider                | Cost      | Dimensions | Notes                        |
| ------------------------------- | --------- | ---------- | ---------------------------- |
| text-embedding-3-small (OpenAI) | \$0.02/1M | 1536       | Good baseline                |
| text-embedding-3-large (OpenAI) | \$0.13/1M | 3072       | Higher quality               |
| voyage-3-large (Voyage)         | \$0.06/1M | 1024       | Strong general semantic      |
| voyage-code-3 (Voyage)          | \$0.06/1M | 1024       | Best for code                |
| nomic-embed-text (local)        | Free      | 768        | Local/offline, privacy-first |

Configure via `embedding.provider` / `embedding.model`; dimensions must match
the provider.

Environment overrides:

- `EMBEDDING_PROVIDER` / `EMBEDDING_MODEL` override config values
- `GEMINI_API_KEY` (or provider-specific key) required for vector search;
  otherwise BM25-only mode

## Contracts & Integrations

### Data sources (reuse existing schemas)

- **Code symbols (named memory)**: `symbols` records with stable IDs
  `workspace_id:path:symbol_span`, `kind`, `body_digest`, `content_hash`,
  `tags`. Backed by `code_symbol_index` outputs; no duplicate embedding
  queues—consume the same queue used by post-review and live-index hooks.
- **Semantic file chunks**: `file_embedding` / `file_embedding_chunk`
  named-memory entries (from `semantic_file_index`), keyed by
  `workspace_id:path:chunking_config_hash:chunk_index` with stored embeddings
  and chunk text refs.
- **Sessions (SQLite `sessions.db`)**: Embedded summaries from the progressive
  memory pipeline (`session/embed`), embedding text built from
  summary/decisions/gotchas/tags/key_files; `id` is the session UUID.
- **Memories (named memory)**: `memory` type with `name`, `tags`, `embedding`,
  `last_updated`. Memory refresh hook triggers `embedding/refresh` rather than a
  bespoke queue.
- **Tasks (tasks.db)**: Todo items with `id`, `title`, `description`, `notes`,
  `gotchas`, `status`. Embeddings stored in `named_memory` as type
  `task_embedding` with name `task://<task_id>`. Embedding content combines
  title + description + notes + gotchas for rich semantic search. Enables
  queries like "find tasks related to authentication" or "what gotchas did we
  learn about rate limiting?"

### Envelope, errors, and CAS

- Emit canonical envelopes: `version:1`, `status:"ok"|"error"`, `meta.ts`
  (RFC3339 UTC), `meta.correlation_id` propagated when available. For
  `status:"error"`, set actionable `error.code` (e.g., `E_INPUT`,
  `E_EMBED_PROVIDER`, `E_SOURCE_EMPTY`) and `error.message`; `data.hint` for
  remediation.
- Large outputs: inline results up to the global inline limit; beyond that, set
  `data.summary` (≤2 KiB) and `data.artifact` digest, with `meta.cas_digest`
  matching the artifact.
- Path safety: all file access goes through `PathValidator` anchored to the
  workspace; reject traversal early with `EPOLICY`.
- Cancellation/timeouts: honor `ctx` deadlines; cap per-source concurrency to
  avoid unbounded goroutines.

#### Error codes (non-exhaustive)

| Code               | When it fires                                    | Remediation hint                          |
| ------------------ | ------------------------------------------------ | ----------------------------------------- |
| `E_INPUT`          | Missing query/scope, invalid workspace, bad JSON | Fix input; check required fields          |
| `E_EMBED_PROVIDER` | Embedding provider error/rate limit              | Retry with backoff; verify API key/model  |
| `E_SOURCE_EMPTY`   | All sources disabled/empty for the request       | Enable sources or run backfill for embeds |
| `E_POLICY`         | PathValidator rejection                          | Query within allowed workspace roots      |
| `E_RUNTIME`        | Unexpected internal error                        | Retry; check logs; file an issue          |

### Ranking, fusion, and deduplication

- Use shared `internal/retrieval` generators for symbols/semantic/ripgrep
  candidates; `code/semantic_search` layers additional session/memory searches
  on top.
- Stable IDs per source:
  - `symbol:<workspace>:<path>#<span>`
  - `file_chunk:<workspace>:<path>#<chunk_index>`
  - `session:<session_id>`
  - `memory:<name>`
  - `task:<task_id>`
- Deduplicate by ID before reciprocal rank fusion; if two sources reference the
  same file path, prefer the higher-scoring source and record `source_details`
  for traceability.
- When a source is empty, still return deterministic ordering from remaining
  sources; BM25-only mode is allowed and documented.

### Cold-start and fallback matrix

| Embeddings available                                          | Behavior                                                                                                                                      |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| None                                                          | BM25 symbols only; return `source="symbol"` results with deterministic order; include `data.hint` suggesting embedding backfill.              |
| Symbols only                                                  | RRF over symbols; sessions/memories/tasks omitted with `stats.sources_missing=["sessions","memories","tasks"]`.                               |
| Sessions only                                                 | Session results plus BM25 symbols; semantic chunks skipped.                                                                                   |
| Symbols + sessions                                            | Full fusion across symbols + sessions; memories/tasks optional.                                                                               |
| Symbols + tasks                                               | Full fusion; task gotchas/notes surface as context hints.                                                                                     |
| Full (symbols + semantic chunks + sessions + memories + tasks)| Full fusion; context hints derived from session and task matches; cap context hints to `max_context_hints`.                                   |

### Telemetry and limits

- Record per-source latency, candidate counts, and cache hits; expose in
  `SearchStats`.
- Defaults: `limit=20`, `min_similarity=0.3`, `max_context_hints=3`, per-source
  timeouts (e.g., 300ms for symbol/semantic stores, 500ms for
  sessions/memories), and rate-limit/backoff for embedding providers aligned
  with embedding worker settings.

## API Examples

### Unified semantic search

```bash
agentctl run code/semantic_search --input '{
  "query": "error handling in HTTP requests",
  "scope": ["symbols", "sessions"],
  "limit": 10,
  "include_context": true
}'
```

### Semantic search with LLM synthesis

```bash
agentctl run code/semantic_search --input '{
  "query": "How do we handle rate limiting in API calls?",
  "scope": ["symbols", "sessions", "tasks"],
  "limit": 10,
  "summarize": true
}'
```

Output with synthesis:

```json
{
  "query": "How do we handle rate limiting in API calls?",
  "results": [...],
  "summary": {
    "answer": "Rate limiting is implemented using a sliding window algorithm in the VoyageProvider and GeminiProvider. Both providers track request timestamps and wait when the limit is exceeded.",
    "key_insights": [
      "VoyageProvider uses 3 RPM limit with 62-second window",
      "GeminiProvider uses 5 RPM for gemini-embedding-001, 15 RPM for text-embedding-004",
      "Both support RateLimitWait option to block vs error on limit"
    ],
    "gotchas": [
      "Gemini free tier has strict 429 enforcement - implement proper backoff",
      "Batch API calls count as single request for rate limiting"
    ],
    "next_steps": [
      "Check provider_gemini.go:170 for waitForRateLimit implementation",
      "Consider upgrading API tier if 5 RPM is insufficient"
    ],
    "model": "openrouter:mistralai/devstral-2505:free",
    "tokens_used": 1847
  }
}
```

Output:

```json
{
  "query": "error handling in HTTP requests",
  "results": [
    {
      "source": "symbol",
      "id": "internal/http/client.go:handleError",
      "name": "handleError",
      "path": "internal/http/client.go",
      "similarity": 0.89,
      "snippet": "func handleError(resp *http.Response) error {"
    },
    {
      "source": "session",
      "id": "session-abc123",
      "name": "HTTP error handling refactor",
      "summary": "Refactored HTTP client error handling...",
      "similarity": 0.82
    }
  ],
  "context_hints": [
    {
      "type": "past_solution",
      "session_id": "session-abc123",
      "summary": "Previously solved similar issue",
      "items": [
        "Gotcha: Don't forget to close response body on error"
      ]
    }
  ]
}
```

### Task semantic search

```bash
agentctl run code/semantic_search --input '{
  "query": "rate limiting gotchas",
  "scope": ["tasks"],
  "limit": 5
}'
```

Output:

```json
{
  "query": "rate limiting gotchas",
  "results": [
    {
      "source": "task",
      "id": "01KD2S51X9XWNA65N46Q9M64N1",
      "name": "Implement API rate limiting",
      "summary": "Added token bucket rate limiter to API client",
      "similarity": 0.87,
      "snippet": "Gotchas learned: Use token bucket, not fixed window. Remember to handle 429 status separately from other errors."
    }
  ],
  "context_hints": [
    {
      "type": "gotcha",
      "task_id": "01KD2S51X9XWNA65N46Q9M64N1",
      "summary": "Rate limiting implementation lessons",
      "items": [
        "Use token bucket, not fixed window",
        "Handle 429 status separately"
      ]
    }
  ]
}
```

### Enhanced swe_grep

```bash
agentctl run code/swe_grep --input '{
  "question": "How do we handle rate limiting?",
  "path": "internal/"
}'
```

Output includes:

```json
{
  "question": "How do we handle rate limiting?",
  "candidates": [...],
  "related_sessions": [
    {
      "session_id": "session-xyz789",
      "summary": "Implemented rate limiting for API client",
      "gotchas": [
        "Use token bucket, not fixed window",
        "Remember to handle 429 status separately"
      ]
    }
  ]
}
```

## Migration Path

1. **Enable embedding queue** - Flip default, existing code works
2. **Populate embeddings** - Run one-time batch job on existing symbols
3. **Add unified search** - New skill, no breaking changes
4. **Add task embeddings** - Create embedding/tasks skill, embed existing todos
5. **Enhance swe_grep** - Add optional context fields
6. **Update hooks** - Add memory and task embedding refresh hooks

## Success Metrics

1. **Embedding coverage** - % of symbols with embeddings
2. **Task embedding coverage** - % of completed tasks with embeddings
3. **Search relevance** - Top-5 accuracy for known queries
4. **Context hit rate** - % of searches with useful session/task hints
5. **Gotcha surfacing** - % of gotchas discovered via semantic search
6. **Latency** - P95 search time < 500ms

## Risks and Mitigations

| Risk                       | Mitigation                            |
| -------------------------- | ------------------------------------- |
| API rate limiting          | Exponential backoff, local queue      |
| Stale embeddings           | TTL-based refresh, edit triggers      |
| Large embedding storage    | Compression, selective embedding      |
| Cold start (no embeddings) | Graceful fallback to BM25-only search |

## Config-Driven Dimension Alignment

All embedding producers and consumers are aligned around config-driven dimensions
to prevent dimension mismatches and corrupted vector searches.

### Default Configuration

```yaml
# ~/.agentctl/config.yaml
embedding:
  provider: gemini
  model: gemini-embedding-001  # 3072-dimensional model
  dimensions: 3072             # Must match model output

database:
  vector:
    enabled: true
    dimensions: 3072  # Must match embedding.dimensions
```

### Dimension Enforcement

| Component | Validation Point | Behavior on Mismatch |
|-----------|-----------------|---------------------|
| session_summarize | After embedding generation | Fail with actionable error |
| embedding_worker | After API response | Mark job failed, log hint |
| code_semantic_search | Before vector search | Skip vector sources, set `stats.hint` |
| Sessions Turso store | On Open() | Return error with reindex guidance |
| Memory Turso store | On SaveWithEmbedding() | Reject entry, return dimension error |

### Error Messages and Remediation

When dimension mismatch is detected:

```
dimension mismatch: got 768, expected 3072; update embedding.model or
embedding.dimensions in config.yaml
```

**Remediation options:**
1. Update `embedding.dimensions` to match actual model output
2. Change `embedding.model` to one producing expected dimensions
3. Reindex sessions/memories after config change

### Memory Vector Search (Turso-Only)

Memory vector search requires Turso with CGO:

| Driver | Vector Support | Search Method |
|--------|---------------|---------------|
| SQLite | No | BM25 text search only |
| Turso (CGO) | Yes | Native `vector_distance_cos` + BM25 hybrid |
| Non-CGO build | No | Falls back to BM25 with hint |

#### Memory Turso Store API

```go
// Open with config-driven dimensions
store, err := memory.OpenTurso(ctx, dbdriver.TursoConfig{
    URL:              url,
    AuthToken:        token,
    VectorDimensions: cfg.Embedding.Dimensions, // 3072
})

// Save with embedding (validates dimensions)
entry, err := store.SaveWithEmbedding(ctx, entry, embedding, "gemini-embedding-001")

// Search similar memories
results, err := store.SearchSimilar(ctx, workspace, queryEmbedding, limit)
```

#### Fallback Behavior

When vector search is unavailable:

1. **No Turso configured**: Uses SQLite BM25 search
2. **CGO disabled**: Returns hint, uses BM25 fallback
3. **Dimension mismatch**: Skips vector sources, logs hint
4. **Empty embeddings**: Returns BM25 results only

### Reindexing After Model Change

When switching embedding models:

```bash
# 1. Update config
# ~/.agentctl/config.yaml
embedding:
  model: new-model-name
  dimensions: 768  # New model dimensions

# 2. Clear existing embeddings
rm ~/.agentctl/sessions.db  # Or truncate sessions table

# 3. Regenerate embeddings
agentctl run session/summarize --input '{"reindex": true}'

# 4. For memory vectors
agentctl run embedding/worker --input '{"batch_size": 100}'
```

### Testing Dimension Enforcement

Tests verify dimension validation at each layer:

```bash
# Unit tests (no CGO required)
CGO_ENABLED=0 go test ./skills/code_semantic_search/...
CGO_ENABLED=0 go test ./internal/storage/memory/...

# Integration tests (CGO + Turso credentials)
TURSO_DATABASE_URL=libsql://... \
TURSO_AUTH_TOKEN=... \
CGO_ENABLED=1 go test -v ./internal/storage/memory/... -run Turso
```
