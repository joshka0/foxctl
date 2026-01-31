# Phase 0: Standards + Scaffolding

> 3 PRs establishing documentation standards, embedding text utilities, and feature flags.

## Overview

This phase creates the foundation for all embedding quality improvements:
1. Standardized comment formats for consistent embedding input
2. Utility package for building and normalizing embedding text
3. Feature flags to control rollout and A/B testing

---

## PR 0.1: Comment Style + Prompt Pack

### Summary

Establish a documentation standard for GoDoc comments that includes an `Index:` block with structured metadata. This enables doc-enriched embeddings to consistently surface relevant symbols during search.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `docs/dev/doc-comments.md` | Create | GoDoc + Index format specification |

### Implementation Details

#### `docs/dev/doc-comments.md`

```markdown
# Doc Comment Standards for Embedding Quality

## Purpose

Well-structured doc comments serve two audiences:
1. **Developers** - IDE tooltips, godoc pages
2. **Embeddings** - Semantic search, code navigation

## Format

### GoDoc Header

Every exported symbol should have a doc comment starting with the symbol name:

```go
// FunctionName does X by Y, returning Z.
// 
// It handles edge cases A, B, C.
func FunctionName(...) ...
```

### Index Block (for embedding enrichment)

Add an `Index:` block at the end of the doc comment for symbols that need enhanced discoverability:

```go
// ProcessTask executes a task in the job queue.
// It validates the task, acquires a lock, and runs the handler.
//
// Index:
//   Purpose: Execute queued tasks with validation and locking
//   Related: TaskQueue, TaskHandler, AcquireLock
//   Keywords: job processing, worker, async execution
func ProcessTask(ctx context.Context, task *Task) error
```

### Index Block Fields

| Field | Required | Description |
|-------|----------|-------------|
| `Purpose` | Yes | 1-sentence description of what the symbol accomplishes (not how) |
| `Related` | Yes | Comma-separated list of related symbols (functions, types, constants) |
| `Keywords` | Optional | Search terms that aren't in the symbol name or signature |

### Index Syntax Contract

- Structured `Index:` blocks are canonical and produce edges.
- Single-line `Index: term1, term2` is keywords-only (no Related/Flow edges).
- Keep the Index block as the last part of the doc comment.
- Field lines may optionally be prefixed with `-` or `*`.

### Embedding Dimension Policy

- All repo-retrieval embeddings within a workspace **must use a single dimension**.
- If dimensions differ (e.g., 1024 vs 3072), store them in separate workspaces/stores.
- Validate dimensions at write time and fail fast with a clear error.

### Guidelines

1. **Purpose should describe intent, not implementation**
   - Good: "Authenticate users via OAuth2 flow"
   - Bad: "Calls the OAuth2 library to get a token"

2. **Related should include:**
   - Symbols this function calls or is called by
   - Types this function produces or consumes
   - Interface implementations or implementors
   - Avoid guessing: only list symbols that exist in the same package/file unless explicitly provided

3. **Keywords should fill gaps:**
   - Domain terms (e.g., "RBAC" for role-based access)
   - Alternative names users might search for
   - Error types this function can produce

### Examples

#### Function with Index Block

```go
// SearchHybrid performs a combined BM25 + vector similarity search.
// Results are merged using Reciprocal Rank Fusion (RRF) scoring.
//
// The bm25Weight parameter controls the balance between lexical and
// semantic matching. Values closer to 1.0 favor BM25; closer to 0.0
// favor vector similarity.
//
// Index:
//   Purpose: Find memories using both keyword and semantic search
//   Related: SearchBM25, SearchVector, RRFMerge, memory.SearchResult
//   Keywords: hybrid search, RRF, reciprocal rank fusion, retrieval
func (s *Store) SearchHybrid(ctx context.Context, query string, vec Vector, workspace string, limit int) ([]SearchResult, error)
```

#### Interface with Index Block

```go
// EmbeddingProvider generates embeddings for text content.
// Implementations may call external APIs, use WASI skills, or local models.
//
// Index:
//   Purpose: Abstract interface for generating text embeddings
//   Related: VoyageProvider, GeminiProvider, QueryEmbeddingProvider, Embedder
//   Keywords: vector, embedding, voyage, gemini, semantic search
type EmbeddingProvider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    // ...
}
```

#### Struct with Index Block

```go
// Candidate represents a file or symbol discovered during code search.
// Candidates are scored and ranked before extraction.
//
// Index:
//   Purpose: Search result item with relevance scoring
//   Related: Generator, Source, MergeCandidates, searchSemanticIndex
//   Keywords: search result, relevance, ranking, retrieval candidate
type Candidate struct {
    Path     string  `json:"path"`
    Score    float64 `json:"score"`
    RawScore float64 `json:"raw_score,omitempty"`
    Source   Source  `json:"source"`
}
```

## Review Checklist

When reviewing PRs that add or modify exported symbols:

- [ ] Doc comment starts with the symbol name
- [ ] Index block has Purpose and Related fields (for non-trivial symbols)
- [ ] Purpose describes intent, not implementation
- [ ] Related lists 2-5 actually-related symbols
- [ ] Keywords add discoverability (not redundant with name/signature)

## Enforcement

The `agentctl index symbol-summaries` command will extract Index blocks and include them in embedding text. Symbols without Index blocks will use only their GoDoc header.

Future: A linter could warn on exported symbols missing Index blocks.
```

### LLM Prompt Template

Include a prompt template for LLM-assisted Index block generation:

```markdown
## Prompt for Generating Index Blocks

When asked to add Index blocks to Go code, use this template:

---

You are adding `Index:` blocks to Go doc comments. For each exported symbol:

1. **Purpose**: Write a single sentence describing what this symbol accomplishes (the "what" and "why", not "how"). Start with a verb.

2. **Related**: List 2-5 symbols that are closely related:
   - Functions this calls or is called by
   - Types this produces or consumes  
   - Interfaces this implements or is implemented by

3. **Keywords**: Add 0-3 terms that users might search for but aren't in the name/signature:
   - Domain-specific terms
   - Alternative names
   - Error types produced

4. **Do not invent Related symbols**: only list actual identifiers found in the same package/file or explicitly provided in the task.

Example input:
```go
// ParseConfig reads configuration from a YAML file.
func ParseConfig(path string) (*Config, error)
```

Example output:
```go
// ParseConfig reads configuration from a YAML file.
//
// Index:
//   Purpose: Load application configuration from disk
//   Related: Config, ValidateConfig, WriteConfig
//   Keywords: yaml, settings, load config
func ParseConfig(path string) (*Config, error)
```

---
```

### Testing Strategy

1. **Manual review**: Have 2-3 team members review the doc-comments.md for clarity
2. **Example validation**: Verify all examples compile and make semantic sense
3. **Tool compatibility**: Ensure godoc correctly ignores Index blocks (they're just comments)

### Acceptance Criteria

- [ ] `docs/dev/doc-comments.md` exists with complete specification
- [ ] Includes at least 3 concrete examples (function, interface, struct)
- [ ] Includes review checklist
- [ ] Includes LLM prompt template for assisted generation
- [ ] All code examples are valid Go syntax

---

## PR 0.2: Embedding Text Utility Package

### Summary

Create a new package `internal/indexing/embeddingtext` with utilities for building and normalizing text that will be embedded. This centralizes the logic for converting symbols, docs, and code into high-quality embedding input.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/indexing/embeddingtext/normalize.go` | Create | Text normalization functions |
| `internal/indexing/embeddingtext/digest.go` | Create | SHA256 digest for change detection |
| `internal/indexing/embeddingtext/symbol_text.go` | Create | Build symbol embedding text |
| `internal/indexing/embeddingtext/doc.go` | Create | Package documentation |

### Implementation Details

#### `internal/indexing/embeddingtext/doc.go`

```go
// Package embeddingtext provides utilities for building and normalizing
// text content for vector embeddings.
//
// The package supports two modes of operation:
//   - Raw mode: Embed the original content as-is
//   - Doc-enriched mode: Combine doc comments, signatures, and relationships
//
// Index:
//   Purpose: Build high-quality text for embedding generation
//   Related: semantic.EmbeddingProvider, symbol.Symbol, indexing.embeddingtext
//   Keywords: embedding text, normalize, digest, symbol text
package embeddingtext
```

#### `internal/indexing/embeddingtext/normalize.go`

```go
package embeddingtext

import (
    "regexp"
    "strings"
    "unicode"
)

var reMultiBlank = regexp.MustCompile(`\n{3,}`)

// NormalizeDoc cleans a documentation string for embedding.
// It strips comment markers per line, normalizes line endings,
// preserves paragraph boundaries, and preserves fenced code blocks.
// Outside fences, it unwraps line-wrapped paragraphs by joining
// consecutive non-empty lines with spaces.
//
// Index:
//   Purpose: Clean documentation text for consistent embedding
//   Related: NormalizeForDigest, BuildSymbolEmbeddingText
//   Keywords: whitespace, doc comment, cleanup
func NormalizeDoc(doc string) string {
    if doc == "" {
        return ""
    }

    // Normalize line endings
    doc = strings.ReplaceAll(doc, "\r\n", "\n")
    doc = strings.ReplaceAll(doc, "\r", "\n")

    // Strip block comment wrapper if present
    doc = strings.TrimPrefix(doc, "/*")
    doc = strings.TrimSuffix(doc, "*/")

    lines := strings.Split(doc, "\n")
    for i, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "//") {
            line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
        }
        if strings.HasPrefix(line, "*") {
            line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
        }
        lines[i] = collapseSpaces(line)
    }
    doc = strings.Join(lines, "\n")

    // Collapse multiple newlines to a single blank line
    doc = reMultiBlank.ReplaceAllString(doc, "\n\n")

    return strings.TrimSpace(doc)
}

// collapseSpaces reduces multiple consecutive spaces to a single space.
func collapseSpaces(s string) string {
    var result strings.Builder
    prevSpace := false
    for _, r := range s {
        if unicode.IsSpace(r) && r != '\n' {
            if !prevSpace {
                result.WriteRune(' ')
                prevSpace = true
            }
        } else {
            result.WriteRune(r)
            prevSpace = false
        }
    }
    return result.String()
}

// NormalizeForDigest prepares text for digest computation.
// Use this on doc/comment components, not on full embedding text.
// This produces a canonical form that ignores insignificant whitespace
// changes but detects meaningful content changes.
//
// Index:
//   Purpose: Canonicalize text for content-based change detection
//   Related: DigestSHA256, NormalizeDoc
//   Keywords: canonical, digest input, change detection
func NormalizeForDigest(text string) string {
    if text == "" {
        return ""
    }

    // Normalize all whitespace to single spaces
    var result strings.Builder
    prevSpace := false
    for _, r := range text {
        if unicode.IsSpace(r) {
            if !prevSpace {
                result.WriteRune(' ')
                prevSpace = true
            }
        } else {
            result.WriteRune(r)
            prevSpace = false
        }
    }

    return strings.TrimSpace(result.String())
}

// TruncateForEmbedding truncates text to fit within embedding model limits.
// Most models accept 8192 tokens; we target ~6000 words to stay safe.
//
// Index:
//   Purpose: Ensure text fits within embedding model token limits
//   Related: BuildSymbolEmbeddingText
//   Keywords: token limit, truncation, embedding size
func TruncateForEmbedding(text string, maxWords int) string {
    if maxWords <= 0 {
        maxWords = 6000 // Safe default for 8k token models
    }

    words := strings.Fields(text)
    if len(words) <= maxWords {
        return text
    }

    // Truncate and add indicator
    return strings.Join(words[:maxWords], " ") + " [truncated]"
}
```

Implementation notes:
- Compile regexes at package scope (avoid per-call MustCompile).
- Add NormalizeFirstComment in embeddingtext for file headers.

#### `internal/indexing/embeddingtext/digest.go`

```go
package embeddingtext

import (
    "crypto/sha256"
    "encoding/hex"
)

// DigestSHA256 computes a SHA256 digest of the normalized text.
// The input is normalized before hashing to ensure stable digests
// across insignificant formatting changes.
//
// Index:
//   Purpose: Generate stable content hash for change detection
//   Related: NormalizeForDigest, embedding.Job
//   Keywords: sha256, hash, content digest, change detection
func DigestSHA256(text string) string {
    normalized := NormalizeForDigest(text)
    hash := sha256.Sum256([]byte(normalized))
    return "sha256:" + hex.EncodeToString(hash[:])
}

// DigestSHA256Prefix returns the first n characters of the digest.
// Useful for shorter identifiers when full digest isn't needed.
//
// Index:
//   Purpose: Generate short content identifier
//   Related: DigestSHA256
//   Keywords: short hash, prefix
func DigestSHA256Prefix(text string, n int) string {
    digest := DigestSHA256(text)
    if n > len(digest) {
        return digest
    }
    return digest[:n]
}
```

#### `internal/indexing/embeddingtext/symbol_text.go`

```go
package embeddingtext

import (
    "fmt"
    "sort"
    "strings"
)

// SymbolTextOptions controls how symbol embedding text is built.
//
// Index:
//   Purpose: Configuration for symbol embedding text generation
//   Related: BuildSymbolEmbeddingText, SymbolInfo
type SymbolTextOptions struct {
    // IncludeCode includes the full source code (if available).
    // When false, only doc + signature + hints are included.
    IncludeCode bool

    // IncludeRelationships adds "Calls:" and "CalledBy:" hints.
    IncludeRelationships bool

    // MaxCodeLines limits code inclusion to prevent oversized text.
    MaxCodeLines int

    // MaxRelationships limits the number of related symbols shown.
    MaxRelationships int
}

// DefaultSymbolTextOptions returns sensible defaults for symbol embedding.
//
// Index:
//   Purpose: Provide default configuration for symbol embedding
//   Related: SymbolTextOptions, BuildSymbolEmbeddingText
func DefaultSymbolTextOptions() SymbolTextOptions {
    return DefaultSymbolTextOptionsDocEnriched()
}

// DefaultSymbolTextOptionsDocEnriched includes doc + signature + capped code.
func DefaultSymbolTextOptionsDocEnriched() SymbolTextOptions {
    return SymbolTextOptions{
        IncludeCode:          true,  // Include a capped excerpt for semantic grounding
        IncludeRelationships: true,
        MaxCodeLines:         50,
        MaxRelationships:     10,
    }
}

// DefaultSymbolTextOptionsSummaryOnly uses doc + signature only.
func DefaultSymbolTextOptionsSummaryOnly() SymbolTextOptions {
    return SymbolTextOptions{
        IncludeCode:          false,
        IncludeRelationships: false,
        MaxCodeLines:         0,
        MaxRelationships:     0,
    }
}

// SymbolInfo contains the information needed to build embedding text.
//
// Index:
//   Purpose: Input data for symbol embedding text generation
//   Related: BuildSymbolEmbeddingText, symbol.Symbol
type SymbolInfo struct {
    // Name is the symbol name (e.g., "SearchHybrid")
    Name string

    // Kind is the symbol kind (e.g., "function", "type", "interface")
    Kind string

    // Package is the package path (e.g., "internal/storage/memory")
    Package string

    // FilePath is the file where the symbol is defined
    FilePath string

    // Signature is the type signature (e.g., "func(ctx context.Context, query string) ([]SearchResult, error)")
    Signature string

    // Doc is the documentation comment (GoDoc)
    Doc string

    // Code is the full source code (optional)
    Code string

    // Calls lists symbols this symbol calls
    Calls []string

    // CalledBy lists symbols that call this symbol
    CalledBy []string

    // Implements lists interfaces this type implements
    Implements []string

    // ImplementedBy lists types that implement this interface
    ImplementedBy []string
}

// BuildSymbolEmbeddingText creates the text to embed for a symbol.
// The output combines documentation, signature, and relationship hints
// into a format optimized for semantic search.
//
// Index:
//   Purpose: Generate embedding-optimized text from symbol metadata
//   Related: SymbolInfo, SymbolTextOptions, NormalizeDoc
//   Keywords: symbol embedding, doc enriched, semantic text
func BuildSymbolEmbeddingText(info SymbolInfo, opts SymbolTextOptions) string {
    var parts []string

    // Header: Kind + Name + Package
    header := fmt.Sprintf("[%s] %s", info.Kind, info.Name)
    if info.Package != "" {
        header += fmt.Sprintf(" (package: %s)", info.Package)
    }
    parts = append(parts, header)

    // Signature
    if info.Signature != "" {
        parts = append(parts, "Signature: "+info.Signature)
    }

    // Documentation (normalized)
    if info.Doc != "" {
        doc := NormalizeDoc(info.Doc)
        parts = append(parts, "Documentation: "+doc)
    }

    // Relationship hints (sorted + deduped for stability)
    if opts.IncludeRelationships {
        if len(info.Calls) > 0 {
            calls := truncateList(sortDedup(info.Calls), opts.MaxRelationships)
            parts = append(parts, "Calls: "+strings.Join(calls, ", "))
        }
        if len(info.CalledBy) > 0 {
            calledBy := truncateList(sortDedup(info.CalledBy), opts.MaxRelationships)
            parts = append(parts, "Called by: "+strings.Join(calledBy, ", "))
        }
        if len(info.Implements) > 0 {
            impl := truncateList(sortDedup(info.Implements), opts.MaxRelationships)
            parts = append(parts, "Implements: "+strings.Join(impl, ", "))
        }
        if len(info.ImplementedBy) > 0 {
            implBy := truncateList(sortDedup(info.ImplementedBy), opts.MaxRelationships)
            parts = append(parts, "Implemented by: "+strings.Join(implBy, ", "))
        }
    }

    // Optional: Include code
    if opts.IncludeCode && info.Code != "" {
        code := truncateCode(info.Code, opts.MaxCodeLines)
        parts = append(parts, "Source:\n"+code)
    }

    return strings.Join(parts, "\n")
}

// sortDedup sorts and deduplicates a list for deterministic output.
func sortDedup(items []string) []string {
    if len(items) == 0 {
        return nil
    }
    seen := make(map[string]struct{}, len(items))
    out := make([]string, 0, len(items))
    for _, it := range items {
        if it == "" {
            continue
        }
        if _, ok := seen[it]; ok {
            continue
        }
        seen[it] = struct{}{}
        out = append(out, it)
    }
    sort.Strings(out)
    return out
}

// truncateList limits the number of items in a list.
func truncateList(items []string, max int) []string {
    if max <= 0 || len(items) <= max {
        return items
    }
    result := make([]string, max)
    copy(result, items[:max])
    return result
}

// truncateCode limits code to maxLines.
func truncateCode(code string, maxLines int) string {
    if maxLines <= 0 {
        return code
    }

    lines := strings.Split(code, "\n")
    if len(lines) <= maxLines {
        return code
    }

    return strings.Join(lines[:maxLines], "\n") + "\n// ... [truncated]"
}
```

### Testing Strategy

1. **Unit tests for normalization**:
   ```go
   func TestNormalizeDoc(t *testing.T) {
       cases := []struct {
           input, expected string
       }{
           {"// Simple comment", "Simple comment"},
           {"/* Block\n   comment */", "Block\ncomment"},
           {"Multiple   spaces", "Multiple spaces"},
           {"Line1\n\n\n\nLine2", "Line1\n\nLine2"},
       }
       // ...
   }
   ```

2. **Unit tests for digest stability**:
   ```go
   func TestDigestSHA256Stability(t *testing.T) {
       // Same content with different whitespace should produce same digest
       a := DigestSHA256("hello   world")
       b := DigestSHA256("hello world")
       assert.Equal(t, a, b)
   }
   ```

3. **Unit tests for symbol text building**:
   ```go
   func TestBuildSymbolEmbeddingText(t *testing.T) {
       info := SymbolInfo{
           Name:      "SearchHybrid",
           Kind:      "function",
           Package:   "internal/storage/memory",
           Signature: "func(ctx, query, vec, workspace, limit) ([]SearchResult, error)",
           Doc:       "SearchHybrid performs combined BM25 + vector search.",
           Calls:     []string{"SearchBM25", "SearchVector"},
       }
       text := BuildSymbolEmbeddingText(info, DefaultSymbolTextOptions())
       assert.Contains(t, text, "[function] SearchHybrid")
       assert.Contains(t, text, "Calls: SearchBM25, SearchVector")
   }
   ```

### Acceptance Criteria

- [ ] Package compiles with no errors
- [ ] `NormalizeDoc` handles common doc comment formats
- [ ] `DigestSHA256` produces stable digests across whitespace variations
- [ ] `BuildSymbolEmbeddingText` produces readable, searchable text
- [ ] Unit test coverage > 80% for all functions
- [ ] No external dependencies added (stdlib only)

---

## PR 0.3: Feature Flags / Config Knobs

### Summary

Add feature flags to control embedding behavior, enabling gradual rollout and A/B testing of embedding quality improvements.

### Files Touched

| File | Action | Description |
|------|--------|-------------|
| `internal/platform/config/embedding_flags.go` | Create | Feature flag definitions and resolution |

### Implementation Details

#### `internal/platform/config/embedding_flags.go`

```go
package config

import (
    "os"
    "strings"
)

// EmbedQueryMode controls how queries are embedded during retrieval.
//
// Index:
//   Purpose: Configure query embedding strategy
//   Related: EmbedSymbolTextMode, ResolveEmbedQueryMode
//   Keywords: query embedding, retrieval, feature flag
type EmbedQueryMode string

const (
    // EmbedQueryModeAuto uses EmbedQuery if the provider supports it,
    // otherwise falls back to Embed.
    EmbedQueryModeAuto EmbedQueryMode = "auto"

    // EmbedQueryModeEmbed always uses the standard Embed method.
    // Use this for providers without query-specific optimizations.
    EmbedQueryModeEmbed EmbedQueryMode = "embed"

    // EmbedQueryModeEmbedQuery always uses EmbedQuery.
    // Will error if the provider doesn't support it.
    EmbedQueryModeEmbedQuery EmbedQueryMode = "embed_query"
)

// EmbedSymbolTextMode controls how symbol text is prepared for embedding.
//
// Index:
//   Purpose: Configure symbol embedding text format
//   Related: EmbedQueryMode, ResolveEmbedSymbolTextMode
//   Keywords: symbol embedding, doc enriched, feature flag
type EmbedSymbolTextMode string

const (
    // EmbedSymbolTextModeRaw embeds the original symbol content as-is.
    // This is the legacy behavior.
    EmbedSymbolTextModeRaw EmbedSymbolTextMode = "raw"

    // EmbedSymbolTextModeDocEnriched combines doc comments, signatures,
    // and relationship hints for richer embeddings.
    EmbedSymbolTextModeDocEnriched EmbedSymbolTextMode = "doc_enriched"
)

// EmbedFileTextMode controls how file content is prepared for embedding.
//
// Index:
//   Purpose: Configure file embedding text format
//   Related: EmbedSymbolTextMode, ResolveEmbedFileTextMode
//   Keywords: file embedding, intent, TOC, feature flag
type EmbedFileTextMode string

const (
    // EmbedFileTextModeRaw embeds the file content as-is (or chunked).
    EmbedFileTextModeRaw EmbedFileTextMode = "raw"

    // EmbedFileTextModeIntent embeds a table-of-contents style summary
    // describing what the file contains and its purpose.
    EmbedFileTextModeIntent EmbedFileTextMode = "intent"
)

// EmbeddingFlags holds all embedding-related feature flags.
//
// Index:
//   Purpose: Aggregate embedding feature flag settings
//   Related: ResolveEmbeddingFlags, Config
//   Keywords: feature flags, embedding config
type EmbeddingFlags struct {
    // QueryMode controls query embedding strategy.
    // Env: EMBED_QUERY_MODE (auto|embed|embed_query)
    QueryMode EmbedQueryMode `mapstructure:"query_mode" json:"query_mode"`

    // SymbolTextMode controls symbol embedding text format.
    // Env: EMBED_SYMBOL_TEXT_MODE (raw|doc_enriched)
    SymbolTextMode EmbedSymbolTextMode `mapstructure:"symbol_text_mode" json:"symbol_text_mode"`

    // FileTextMode controls file embedding content format.
    // Env: EMBED_FILE_TEXT_MODE (raw|intent)
    FileTextMode EmbedFileTextMode `mapstructure:"file_text_mode" json:"file_text_mode"`
}

// DefaultEmbeddingFlags returns the default flag values.
// All flags default to their legacy/safe behavior.
//
// Index:
//   Purpose: Provide default embedding flag values
//   Related: EmbeddingFlags, ResolveEmbeddingFlags
func DefaultEmbeddingFlags() EmbeddingFlags {
    return EmbeddingFlags{
        QueryMode:      EmbedQueryModeAuto,
        SymbolTextMode: EmbedSymbolTextModeRaw,
        FileTextMode:   EmbedFileTextModeRaw,
    }
}

// ResolveEmbeddingFlags loads flags from environment variables,
// falling back to defaults if not set.
//
// Index:
//   Purpose: Load embedding flags from environment
//   Related: EmbeddingFlags, DefaultEmbeddingFlags
//   Keywords: env var, feature flag resolution
func ResolveEmbeddingFlags() EmbeddingFlags {
    flags := DefaultEmbeddingFlags()

    // Query mode
    if v := os.Getenv("EMBED_QUERY_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "embed":
            flags.QueryMode = EmbedQueryModeEmbed
        case "embed_query":
            flags.QueryMode = EmbedQueryModeEmbedQuery
        default:
            flags.QueryMode = EmbedQueryModeAuto
        }
    }

    // Symbol text mode
    if v := os.Getenv("EMBED_SYMBOL_TEXT_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "doc_enriched":
            flags.SymbolTextMode = EmbedSymbolTextModeDocEnriched
        default:
            flags.SymbolTextMode = EmbedSymbolTextModeRaw
        }
    }

    // File text mode
    if v := os.Getenv("EMBED_FILE_TEXT_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "intent":
            flags.FileTextMode = EmbedFileTextModeIntent
        default:
            flags.FileTextMode = EmbedFileTextModeRaw
        }
    }

    return flags
}

// ResolveEmbedQueryMode returns the effective query mode,
// checking env var first, then config, then default.
//
// Index:
//   Purpose: Resolve query mode from all sources
//   Related: EmbedQueryMode, EmbeddingFlags
func ResolveEmbedQueryMode(configMode EmbedQueryMode) EmbedQueryMode {
    // Env var takes precedence
    if v := os.Getenv("EMBED_QUERY_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "embed":
            return EmbedQueryModeEmbed
        case "embed_query":
            return EmbedQueryModeEmbedQuery
        case "auto":
            return EmbedQueryModeAuto
        }
    }

    // Then config
    if configMode != "" {
        return configMode
    }

    // Default
    return EmbedQueryModeAuto
}

// ResolveEmbedSymbolTextMode returns the effective symbol text mode.
//
// Index:
//   Purpose: Resolve symbol text mode from all sources
//   Related: EmbedSymbolTextMode, EmbeddingFlags
func ResolveEmbedSymbolTextMode(configMode EmbedSymbolTextMode) EmbedSymbolTextMode {
    if v := os.Getenv("EMBED_SYMBOL_TEXT_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "doc_enriched":
            return EmbedSymbolTextModeDocEnriched
        case "raw":
            return EmbedSymbolTextModeRaw
        }
    }

    if configMode != "" {
        return configMode
    }

    return EmbedSymbolTextModeRaw
}

// ResolveEmbedFileTextMode returns the effective file text mode.
//
// Index:
//   Purpose: Resolve file text mode from all sources
//   Related: EmbedFileTextMode, EmbeddingFlags
func ResolveEmbedFileTextMode(configMode EmbedFileTextMode) EmbedFileTextMode {
    if v := os.Getenv("EMBED_FILE_TEXT_MODE"); v != "" {
        switch strings.ToLower(v) {
        case "intent":
            return EmbedFileTextModeIntent
        case "raw":
            return EmbedFileTextModeRaw
        }
    }

    if configMode != "" {
        return configMode
    }

    return EmbedFileTextModeRaw
}
```

### Integration with Config

Update `internal/platform/config/config.go` to include the new flags:

```go
// In EmbeddingSettings struct, add:
type EmbeddingSettings struct {
    // ... existing fields ...

    // Flags controls embedding behavior feature flags.
    Flags EmbeddingFlags `mapstructure:"flags" json:"flags"`
}
```

### Testing Strategy

1. **Unit tests for flag resolution**:
   ```go
   func TestResolveEmbeddingFlags(t *testing.T) {
       // Test defaults
       flags := ResolveEmbeddingFlags()
       assert.Equal(t, EmbedQueryModeAuto, flags.QueryMode)

       // Test env override
       t.Setenv("EMBED_QUERY_MODE", "embed_query")
       flags = ResolveEmbeddingFlags()
       assert.Equal(t, EmbedQueryModeEmbedQuery, flags.QueryMode)
   }
   ```

2. **Test config precedence**:
   ```go
   func TestResolveEmbedQueryMode(t *testing.T) {
       // Env > config > default
       t.Setenv("EMBED_QUERY_MODE", "")
       mode := ResolveEmbedQueryMode(EmbedQueryModeEmbed)
       assert.Equal(t, EmbedQueryModeEmbed, mode) // Config wins

       t.Setenv("EMBED_QUERY_MODE", "embed_query")
       mode = ResolveEmbedQueryMode(EmbedQueryModeEmbed)
       assert.Equal(t, EmbedQueryModeEmbedQuery, mode) // Env wins
   }
   ```

### Acceptance Criteria

- [ ] Package compiles with no errors
- [ ] All three modes have proper constants and resolution functions
- [ ] Environment variables take precedence over config
- [ ] Defaults are safe/legacy behavior
- [ ] Unit test coverage > 90%
- [ ] Integration with `EmbeddingSettings` struct (if merged)

---

## Dependencies

```
PR 0.1 (doc-comments.md) ─────────────────────► Phase 6 (doc sweep)
                                               │
PR 0.2 (embeddingtext pkg) ──► Phase 2 (symbol embeds)
                                               │
PR 0.3 (feature flags) ───────────────────────► Phase 1, 2, 3
```

## Timeline Estimate

| PR | Effort | Dependencies |
|----|--------|--------------|
| PR 0.1 | 2-4 hours | None |
| PR 0.2 | 4-6 hours | None |
| PR 0.3 | 2-3 hours | None |

**Total**: 8-13 hours of implementation

All three PRs can be developed in parallel.
