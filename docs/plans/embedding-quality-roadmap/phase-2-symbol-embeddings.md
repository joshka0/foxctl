# Phase 2: Doc-enriched Symbol Embeddings

> **Goal:** Improve symbol embedding quality by incorporating documentation context (package docs, function comments, type descriptions) into the embedded text, while maintaining backward compatibility and incremental re-embedding on doc changes.

---

## Overview

Currently, symbol embeddings use raw code snippets or function bodies. This phase enriches the embedded text with structured documentation to improve semantic search quality. Changes are gated behind feature flags to allow gradual rollout.

**Total PRs:** 3

---

## Symbol Embedding Text Contract (v1)

Symbol embeddings are generated from (in order):

1. **Identity**: kind, name, file path, package, signature
2. **Doc**: GoDoc + Index block (normalized)
3. **Relationships** (optional, capped, sorted+deduped): calls/implements/etc.
4. **Code excerpt** (optional, capped)

Caps (v1):
- Max total chars: 12000
- Max code excerpt chars: 8000

Doc-only edits should trigger a re-embed; whitespace-only changes should not. `content_digest` is computed from structured components (see design-notes).

---

## PR 2.1: Wire Doc-enriched Text into Symbol Embedding Enqueue (Flagged)

### Summary

Modify the symbol embedding enqueue path to use `embeddingtext.BuildSymbolEmbeddingText()` to combine code with documentation context. Gate behind `EMBED_SYMBOL_TEXT_MODE` environment variable to allow A/B comparison and safe rollout.

### Files Touched

| File | Changes |
|------|---------|
| `internal/indexing/symbol/enqueue.go` | Add `embeddingtext.BuildSymbolEmbeddingText()` call, conditional on flag |
| `internal/indexing/symbol/enqueue_test.go` | Add tests for new text building logic |
| `internal/indexing/embeddingtext/symbol_text.go` | Add `BuildSymbolEmbeddingText()` implementation |
| `internal/indexing/embeddingtext/symbol_text_test.go` | Add unit tests for text builder |
| `internal/platform/config/config.go` | Add `EmbedSymbolTextMode` field |

### Implementation Details

#### 1. Text Builder Function

```go
// internal/indexing/embeddingtext/symbol_text.go

// BuildSymbolEmbeddingText constructs embedding-optimized text for a symbol.
// Doc-enriched mode uses NormalizeDoc and caps code/relationship sections.
func BuildSymbolEmbeddingText(info SymbolInfo, opts SymbolTextOptions) string {
    // 1) Identity: kind, name, file, package, signature
    // 2) Doc: GoDoc + Index block (normalized)
    // 3) Relationships: optional, capped (sorted + deduped)
    // 4) Code excerpt: optional, capped
}
```

#### 1b. Content Digest Helper

```go
// internal/indexing/embeddingtext/digest.go

type SymbolDigestInput struct {
    Doc         string
    IndexBlock  string
    BodyDigest  string
    SigHash     string
    Calls       []string
    Model       string
    FileSummary string
}

// BuildSymbolContentDigest computes the content_digest from structured components.
func BuildSymbolContentDigest(input SymbolDigestInput) string {
    // v1 contract (doc/body/sig/relationships + model)
}
```

#### 2. Enqueue Integration

```go
// internal/indexing/symbol/enqueue.go

func (e *Enqueuer) EnqueueSymbol(ctx context.Context, sym Symbol) error {
    var content string
    
    switch e.cfg.EmbedSymbolTextMode {
    case "doc_enriched":
        content = embeddingtext.BuildSymbolEmbeddingText(sym.Info, embeddingtext.DefaultSymbolTextOptionsDocEnriched())
    default: // "raw"
        content = sym.Body
    }

    // content_digest is computed from structured components (doc/body/sig/relationships + model)
    digest := embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
        Doc:         sym.Info.Doc,
        IndexBlock:  sym.Info.IndexBlock,
        BodyDigest:  sym.BodyDigest,
        SigHash:     sym.SigHash,
        Calls:       sym.Info.Calls,
        Model:       e.model,
        FileSummary: sym.FileSummary,
    })
    
    // Existing enqueue logic with content and digest
    return e.queue.Enqueue(ctx, EmbedJob{
        SymbolID:      sym.ID,
        Content:       content,
        ContentDigest: digest,
    })
}
```

#### 3. Config Addition

```go
// internal/platform/config/config.go

type EmbeddingConfig struct {
    VoyageAPIKey       string `env:"VOYAGE_API_KEY"`
    SymbolTextMode     string `env:"EMBED_SYMBOL_TEXT_MODE" default:"raw"` // "raw" | "doc_enriched"
}
```

#### 4. Storage + Retrieval Wiring

- Persist embeddings in `symbol_embeddings` as the durable store.
- On embedding completion, also update the symbol's `named_memory` entry embedding
  (e.g., `symbol://<workspace>/<file>:<name>`) so existing retrieval paths
  (`SearchSimilar`, vector indexes) immediately benefit.
- If retrieval should use `symbol_embeddings` directly instead, add an explicit
  search path and keep both stores in sync.

### Testing Strategy

1. **Unit tests for `BuildSymbolEmbeddingText()`**
   - Empty metadata produces minimal valid output
   - Full metadata produces expected format
   - Unicode handling in docs/code
   - Very long docs are truncated appropriately

2. **Digest stability tests**
   - Whitespace-only changes to doc don't change docDigest
   - Doc edits change docDigest and content_digest
   - Code edits change bodyDigest and content_digest
   - Relationship order changes do not change content_digest (sorted + deduped)

3. **Integration tests**
   - Enqueue with `raw` mode uses old path
   - Enqueue with `doc_enriched` mode uses new path
   - Both produce valid embed jobs

### Acceptance Criteria

- [ ] `BuildSymbolEmbeddingText()` implemented with normalization and caps
- [ ] `EMBED_SYMBOL_TEXT_MODE=doc_enriched` enables new path
- [ ] Default behavior (`raw`) unchanged
- [ ] Doc-only edits trigger re-embed; whitespace-only edits do not
- [ ] Embedding inputs are secret-scanned (redact/block policy)
- [ ] All existing symbol embedding tests pass
- [ ] New unit tests for text builder achieve >90% coverage

---

## PR 2.2: Incremental Correctness - Doc Changes Trigger Re-embed

### Summary

Use the embedding store as the source of truth for content digests. Before enqueueing a symbol embed job, check the existing embedding record's `content_digest` for the workspace+symbol, and only skip when the stored model also matches. This keeps doc-enriched embeddings current without re-embedding unchanged symbols.

### Files Touched

| File | Changes |
|------|---------|
| `internal/indexing/embedding/store.go` | Add `GetContentDigest()` (workspaceID, symbolID) helper |
| `internal/indexing/symbol/enqueue.go` | Check embed-store digest before enqueuing |
| `internal/indexing/embedding/store_test.go` | Tests for digest lookup |
| `internal/indexing/symbol/enqueue_test.go` | Tests for enqueue skip behavior |

### Implementation Details

#### 1. Query embedding store

```go
// internal/indexing/embedding/store.go

// GetContentDigest returns the last stored content digest and model for a symbol.
func (s *Store) GetContentDigest(ctx context.Context, workspaceID, symbolID string) (digest string, model string, ok bool, err error) {
    // SELECT content_digest, model FROM symbol_embeddings WHERE workspace_id=? AND symbol_id=? LIMIT 1
}
```

#### 2. Skip Logic in Enqueue

```go
// internal/indexing/symbol/enqueue.go

func (e *Enqueuer) EnqueueSymbol(ctx context.Context, sym Symbol) error {
    content := e.buildContent(sym)
    currentDigest := embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
        Doc:         sym.Info.Doc,
        IndexBlock:  sym.Info.IndexBlock,
        BodyDigest:  sym.BodyDigest,
        SigHash:     sym.SigHash,
        Calls:       sym.Info.Calls,
        Model:       e.model,
        FileSummary: sym.FileSummary,
    })

    priorDigest, priorModel, ok, err := e.embedStore.GetContentDigest(ctx, e.workspaceID, sym.ID)
    if err != nil {
        return err
    }
    if ok && priorDigest == currentDigest && priorModel == e.model {
        e.logSkip(sym.ID, "unchanged embed digest")
        return nil
    }

    // Proceed with enqueue
    return e.queue.Enqueue(ctx, EmbedJob{
        SymbolID:      sym.ID,
        Content:       content,
        ContentDigest: currentDigest,
    })
}

func (e *Enqueuer) logSkip(symbolID, reason string) {
    if e.cfg.Debug {
        log.Printf("[embed-skip] symbol=%s reason=%s", symbolID, reason)
    }
    // Also emit observability event
    event := observability.NewEvent("symbol.embed.skipped").
        WithData("symbol_id", symbolID).
        WithData("reason", reason)
    observability.Emit(context.Background(), event)
}
```

#### 3. Persist After Embedding

Embedding store writes `content_digest` alongside the embedding record. Ensure this field is populated for all new jobs so the lookup remains authoritative.

### Testing Strategy

1. **Unit tests for `GetContentDigest()`**
   - No record returns ok=false
   - Record returned for workspace+symbol
   - Model returned alongside digest

2. **Integration tests for skip logic**
   - First enqueue: symbol enqueued, digest stored
   - Second enqueue (unchanged): symbol skipped
   - Third enqueue (doc changed): symbol re-enqueued

3. **Debug logging verification**
   - Skip events logged when `Debug=true`
   - Observability events emitted for monitoring

### Acceptance Criteria

- [ ] Embed store exposes `GetContentDigest()` for workspace+symbol (returns model)
- [ ] Skip logic implemented in enqueue path
- [ ] Debug logging shows skip reasons
- [ ] Observability events emitted for skipped symbols
- [ ] `content_digest` persisted with embeddings
- [ ] Re-embed triggered when doc content changes
- [ ] No re-embed for formatting-only changes (due to normalization in PR 2.1)
- [ ] All existing tests pass

---

## PR 2.3: Backfill Command

### Summary

Add a CLI command to backfill doc-enriched embeddings for symbols that were embedded with the old `raw` mode. Supports bounded execution, dry-run, and progress reporting.

### Files Touched

| File | Changes |
|------|---------|
| `cmd/agentctl/cmd/embedding_backfill.go` | **New file** - CLI command implementation |
| `cmd/agentctl/cmd/embedding_backfill_test.go` | **New file** - command tests |
| `cmd/agentctl/cmd/root.go` | Register backfill command |
| `internal/indexing/symbol/backfill.go` | **New file** - backfill business logic |
| `internal/indexing/symbol/backfill_test.go` | **New file** - backfill logic tests |

### Implementation Details

#### 1. CLI Command

```go
// cmd/agentctl/cmd/embedding_backfill.go

var embeddingBackfillCmd = &cobra.Command{
    Use:   "embedding-backfill",
    Short: "Backfill doc-enriched embeddings for symbols",
    Long: `Rebuild embeddings for symbols that were embedded before
doc-enriched mode was enabled. Useful for upgrading existing indexes.

Examples:
  # Dry run - see what would be backfilled
  agentctl embedding-backfill --dry-run

  # Backfill up to 1000 symbols
  agentctl embedding-backfill --limit 1000

  # Backfill specific workspace
  agentctl embedding-backfill --workspace /path/to/repo

  # Force re-embed all (ignore existing digest)
  agentctl embedding-backfill --force`,
    RunE: runEmbeddingBackfill,
}

func init() {
    embeddingBackfillCmd.Flags().Bool("dry-run", false, "Show what would be backfilled without doing it")
    embeddingBackfillCmd.Flags().Int("limit", 0, "Maximum symbols to backfill (0 = unlimited)")
    embeddingBackfillCmd.Flags().Int("batch-size", 100, "Symbols per batch")
    embeddingBackfillCmd.Flags().String("workspace", "", "Workspace path (default: current directory)")
    embeddingBackfillCmd.Flags().Bool("force", false, "Re-embed even if digest matches")
    embeddingBackfillCmd.Flags().Bool("verbose", false, "Show per-symbol progress")
    rootCmd.AddCommand(embeddingBackfillCmd)
}

func runEmbeddingBackfill(cmd *cobra.Command, args []string) error {
    ctx := cmd.Context()
    
    dryRun, _ := cmd.Flags().GetBool("dry-run")
    limit, _ := cmd.Flags().GetInt("limit")
    batchSize, _ := cmd.Flags().GetInt("batch-size")
    workspace, _ := cmd.Flags().GetString("workspace")
    force, _ := cmd.Flags().GetBool("force")
    verbose, _ := cmd.Flags().GetBool("verbose")
    
    if workspace == "" {
        workspace = workspace.Detect("")
    }
    
    cfg, err := config.Load(ctx)
    if err != nil {
        return err
    }
    
    backfiller, err := symbol.NewBackfiller(cfg, symbol.BackfillOptions{
        DryRun:    dryRun,
        Limit:     limit,
        BatchSize: batchSize,
        Workspace: workspace,
        Force:     force,
        Verbose:   verbose,
        Writer:    cmd.OutOrStdout(),
    })
    if err != nil {
        return err
    }
    
    result, err := backfiller.Run(ctx)
    if err != nil {
        return err
    }
    
    // Print summary
    fmt.Fprintf(cmd.OutOrStdout(), "\nBackfill complete:\n")
    fmt.Fprintf(cmd.OutOrStdout(), "  Scanned:   %d symbols\n", result.Scanned)
    fmt.Fprintf(cmd.OutOrStdout(), "  Skipped:   %d (already doc-enriched)\n", result.Skipped)
    fmt.Fprintf(cmd.OutOrStdout(), "  Backfilled: %d\n", result.Backfilled)
    fmt.Fprintf(cmd.OutOrStdout(), "  Errors:    %d\n", result.Errors)
    
    return nil
}
```

#### 2. Backfill Logic

```go
// internal/indexing/symbol/backfill.go

type Backfiller struct {
    store   *memory.Store
    enqueue *Enqueuer
    opts    BackfillOptions
}

type BackfillOptions struct {
    DryRun    bool
    Limit     int
    BatchSize int
    Workspace string
    Force     bool
    Verbose   bool
    Writer    io.Writer
}

type BackfillResult struct {
    Scanned    int
    Skipped    int
    Backfilled int
    Errors     int
}

func (b *Backfiller) Run(ctx context.Context) (*BackfillResult, error) {
    result := &BackfillResult{}
    
    // Query symbols that need backfill
    iter, err := b.store.IterateSymbols(ctx, b.opts.Workspace)
    if err != nil {
        return nil, err
    }
    defer iter.Close()
    
    batch := make([]Symbol, 0, b.opts.BatchSize)
    
    for iter.Next() {
        if b.opts.Limit > 0 && result.Scanned >= b.opts.Limit {
            break
        }
        
        sym := iter.Symbol()
        result.Scanned++
        
        // Check if needs backfill
        if !b.needsBackfill(sym) {
            result.Skipped++
            continue
        }
        
        batch = append(batch, sym)
        
        // Process batch
        if len(batch) >= b.opts.BatchSize {
            backfilled, errors := b.processBatch(ctx, batch)
            result.Backfilled += backfilled
            result.Errors += errors
            batch = batch[:0]
        }
    }
    
    // Process remaining
    if len(batch) > 0 {
        backfilled, errors := b.processBatch(ctx, batch)
        result.Backfilled += backfilled
        result.Errors += errors
    }
    
    return result, nil
}

func (b *Backfiller) needsBackfill(sym Symbol) bool {
    if b.opts.Force {
        return true
    }
    
    // Build doc-enriched content and check digest
    content := BuildSymbolEmbeddingText(sym.Info, embeddingtext.DefaultSymbolTextOptionsDocEnriched())
    currentDigest := embeddingtext.BuildSymbolContentDigest(embeddingtext.SymbolDigestInput{
        Doc:         sym.Info.Doc,
        IndexBlock:  sym.Info.IndexBlock,
        BodyDigest:  sym.BodyDigest,
        SigHash:     sym.SigHash,
        Calls:       sym.Info.Calls,
        Model:       b.model,
        FileSummary: sym.FileSummary,
    })
    
    return sym.Meta.NeedsReembed(currentDigest)
}

func (b *Backfiller) processBatch(ctx context.Context, batch []Symbol) (backfilled, errors int) {
    for _, sym := range batch {
        if b.opts.Verbose {
            fmt.Fprintf(b.opts.Writer, "  Backfilling: %s (%s)\n", sym.Meta.Name, sym.Meta.FilePath)
        }
        
        if b.opts.DryRun {
            backfilled++
            continue
        }
        
        if err := b.enqueue.EnqueueSymbol(ctx, sym); err != nil {
            if b.opts.Verbose {
                fmt.Fprintf(b.opts.Writer, "    Error: %v\n", err)
            }
            errors++
            continue
        }
        
        backfilled++
    }
    
    return backfilled, errors
}
```

### Testing Strategy

1. **Unit tests for backfill logic**
   - Empty store returns zero results
   - Symbols without prior `content_digest` are backfilled
   - Symbols with current digest are skipped
   - `--force` flag overrides skip logic
   - `--limit` caps processed symbols

2. **Integration tests for CLI**
   - `--dry-run` doesn't enqueue jobs
   - Workspace detection works
   - Output format is correct
   - Errors don't abort entire run

3. **E2E test with real store**
   - Create test symbols with old-style embedding
   - Run backfill
   - Verify new digests stored
   - Verify embedding content is doc-enriched

### Acceptance Criteria

- [ ] `agentctl embedding-backfill` command implemented
- [ ] `--dry-run` shows what would be backfilled
- [ ] `--limit` bounds execution
- [ ] `--batch-size` controls memory usage
- [ ] `--workspace` scopes to specific repo
- [ ] `--force` re-embeds everything
- [ ] `--verbose` shows per-symbol progress
- [ ] Summary output shows scanned/skipped/backfilled/errors
- [ ] Reuses existing enqueue path (consistent with PR 2.1)
- [ ] All tests pass

---

## Dependencies

```
PR 2.1 (text builder) ─┬─► PR 2.2 (digest tracking)
                       │
                       └─► PR 2.3 (backfill) ◄─── PR 2.2
```

- PR 2.2 depends on PR 2.1 for `BuildSymbolEmbeddingText()`
- PR 2.3 depends on both PR 2.1 (text builder) and PR 2.2 (digest tracking)

---

## Rollout Plan

1. **Merge PR 2.1** - New code path exists but not active
2. **Internal testing** - Enable `EMBED_SYMBOL_TEXT_MODE=doc_enriched` on test repos
3. **Merge PR 2.2** - Digest tracking prevents redundant work
4. **Merge PR 2.3** - Backfill command available
5. **Production rollout** - Enable flag, run backfill on active workspaces
6. **Make default** - Change default from `raw` to `doc_enriched` in future release
