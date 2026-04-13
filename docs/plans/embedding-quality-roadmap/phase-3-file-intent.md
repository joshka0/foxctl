# Phase 3: File-level Intent (TOC) Stabilization

> **Goal:** Stabilize file-level embeddings by normalizing documentation inputs and optionally shifting from raw file content to "intent text" (structural summary) for embedding, reducing churn from formatting changes while improving semantic retrieval quality.

---

## Overview

File embeddings currently use raw file bytes, which causes unnecessary re-embedding when formatting changes (whitespace, comment style) occur. This phase introduces normalization for documentation inputs used in summaries, and optionally replaces raw file embedding with "intent text" - a stable representation of file purpose and structure.

**Suggested approach:** Ship Option A (summary normalization) first; only add intent embeddings if needed.

**Total PRs:** 2

---

## PR 3.1: Normalize Doc Inputs into FileSummaryInput

### Intent Text Contract (optional, v1)

If we move to intent embeddings, the intent text should include only:
- File identity (path, language, package)
- First comment / package doc (first paragraph, normalized)
- Exported symbol signatures (top N, sorted)
- Imports (optional, capped)

Never include full file bodies in intent text.


### Summary

Apply normalization to `PackageDoc` and `FirstComment` fields before they're used in file summary generation and digest computation. This prevents summary churn from comment formatting changes while preserving semantic content.

### Files Touched

| File | Changes |
|------|---------|
| `internal/intelligence/retrieval/file_summary.go` | Normalize docs before processing |
| `internal/indexing/symbol/summary_input.go` | Add normalization to `FileSummaryInput` construction |
| `internal/indexing/embeddingtext/normalize.go` | Reuse normalization utilities |
| `internal/indexing/embeddingtext/normalize_test.go` | Add/extend normalization tests |
| `internal/intelligence/retrieval/file_summary_test.go` | Tests for normalized summary generation |

### Implementation Details

#### 1. Normalization Utilities

Use the canonical helpers from `internal/indexing/embeddingtext` introduced in Phase 0.2:

- `embeddingtext.NormalizeDoc`
- `embeddingtext.NormalizeFirstComment`

Do not add new normalization helpers in Phase 3.

#### 2. FileSummaryInput Construction

```go
// internal/indexing/symbol/summary_input.go

package symbol

// FileSummaryInput contains normalized inputs for file summary generation.
type FileSummaryInput struct {
    Path           string
    Language       string
    PackageDoc     string   // Normalized package/module documentation
    FirstComment   string   // Normalized file header comment
    ExportedTypes  []string
    ExportedFuncs  []string
    ImportedPkgs   []string
    
    // Digest is computed from normalized fields for change detection
    InputDigest    string
}

// NewFileSummaryInput constructs input with normalization applied.
func NewFileSummaryInput(path, lang, pkgDoc, firstComment string) *FileSummaryInput {
    input := &FileSummaryInput{
        Path:         path,
        Language:     lang,
        PackageDoc:   NormalizeDoc(pkgDoc),
        FirstComment: NormalizeFirstComment(firstComment),
    }
    input.computeDigest()
    return input
}

// AddExports adds exported symbols (already extracted, no normalization needed).
func (f *FileSummaryInput) AddExports(types, funcs []string) {
    f.ExportedTypes = types
    f.ExportedFuncs = funcs
    f.computeDigest() // Recompute after modification
}

// AddImports adds imported packages.
func (f *FileSummaryInput) AddImports(pkgs []string) {
    f.ImportedPkgs = pkgs
    f.computeDigest()
}

func (f *FileSummaryInput) computeDigest() {
    h := sha256.New()
    
    // Include all semantic content in digest
    h.Write([]byte(f.Path))
    h.Write([]byte{0})
    h.Write([]byte(f.Language))
    h.Write([]byte{0})
    h.Write([]byte(f.PackageDoc))
    h.Write([]byte{0})
    h.Write([]byte(f.FirstComment))
    h.Write([]byte{0})
    
    // Sorted exports for determinism
    sortedTypes := append([]string{}, f.ExportedTypes...)
    sort.Strings(sortedTypes)
    for _, t := range sortedTypes {
        h.Write([]byte(t))
        h.Write([]byte{0})
    }
    
    sortedFuncs := append([]string{}, f.ExportedFuncs...)
    sort.Strings(sortedFuncs)
    for _, fn := range sortedFuncs {
        h.Write([]byte(fn))
        h.Write([]byte{0})
    }
    
    f.InputDigest = fmt.Sprintf("sha256:%x", h.Sum(nil))
}

// HasChanged checks if the input has changed compared to a stored digest.
func (f *FileSummaryInput) HasChanged(storedDigest string) bool {
    return f.InputDigest != storedDigest
}
```

#### 3. Integration with File Summary Generation

```go
// internal/intelligence/retrieval/file_summary.go

package retrieval

import (
    "github.com/jkatigb/agentctl/internal/indexing/symbol"
)

// GenerateFileSummary creates a summary for a file using normalized inputs.
func (g *FileSummaryGenerator) GenerateFileSummary(ctx context.Context, path string) (*FileSummary, error) {
    // Parse file to extract docs
    pkgDoc, firstComment, exports, imports, err := g.parser.ParseFile(path)
    if err != nil {
        return nil, err
    }
    
    // Build normalized input
    input := symbol.NewFileSummaryInput(path, g.detectLanguage(path), pkgDoc, firstComment)
    input.AddExports(exports.Types, exports.Functions)
    input.AddImports(imports)
    
    // Check if regeneration needed
    existing, err := g.store.GetFileSummary(ctx, path)
    if err == nil && !input.HasChanged(existing.InputDigest) {
        return existing, nil // Return cached, unchanged
    }
    
    // Generate summary (LLM or template-based)
    summary := g.generateSummaryText(ctx, input)
    
    return &FileSummary{
        Path:        path,
        Summary:     summary,
        InputDigest: input.InputDigest,
        GeneratedAt: time.Now(),
    }, nil
}
```

### Testing Strategy

1. **Unit tests for `NormalizeDoc()`**
   - Go-style comments (`//`) stripped correctly
   - C-style block comments (`/* */`) stripped correctly
   - Shell-style comments (`#`) stripped correctly
   - Multiple whitespace collapsed
   - Paragraph boundaries preserved
   - Empty input returns empty
   - Unicode preserved

2. **Unit tests for `NormalizeFirstComment()`**
   - File header comments normalized
   - Consecutive empty lines collapsed
   - Indentation normalized

3. **Digest stability tests**
   - Same semantic content with different formatting produces same digest
   - Different content produces different digest
   - Order of exports doesn't affect digest (sorted)

4. **Integration tests**
   - File summary regeneration skipped when only formatting changes
   - File summary regenerated when semantic content changes
   - Round-trip through storage preserves digest

### Acceptance Criteria

- [ ] `NormalizeDoc()` handles Go, C, and shell comment styles
- [ ] `NormalizeFirstComment()` preserves file header structure
- [ ] `FileSummaryInput.InputDigest` stable across formatting changes
- [ ] Digest changes when semantic content changes
- [ ] File summary generation uses normalized inputs
- [ ] Regeneration skipped when digest unchanged
- [ ] All existing file summary tests pass
- [ ] New normalization tests achieve >95% coverage

---

## PR 3.2 (Optional): Embed Intent Text Instead of Raw File Bytes

### Summary

Change the semantic file indexer to optionally embed "intent text" - a structured representation of file purpose and contents - instead of raw file bytes. This improves embedding quality and stability. Gated behind `EMBED_FILE_TEXT_MODE` flag with two strategies: safe (keep raw, prioritize summaries in retrieval) or clean (embed intent text directly).

### Files Touched

| File | Changes |
|------|---------|
| `internal/indexing/semantic/indexer.go` | Add intent text mode support |
| `internal/indexing/semantic/intent_text.go` | **New file** - intent text builder |
| `internal/indexing/semantic/intent_text_test.go` | **New file** - intent text tests |
| `internal/intelligence/retrieval/semantic_search.go` | Option A: prioritize summaries in retrieval |
| `internal/platform/config/config.go` | Add `EmbedFileTextMode` config |

### Implementation Details

#### Option A: Safe - Keep Raw Embedding, Prioritize Summaries in Retrieval

This approach keeps the existing file embedding unchanged but modifies retrieval to prioritize file summaries and symbols over raw file matches.

```go
// internal/intelligence/retrieval/semantic_search.go

type SearchOptions struct {
    Query       string
    Limit       int
    Scope       []string // "symbols", "files", "summaries", "memory"
    
    // Option A: Retrieval prioritization
    PrioritizeSummaries bool // When true, boost summary/symbol matches over raw file matches
    SummaryBoostFactor  float64 // Default: 1.5
}

func (s *SemanticSearcher) Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
    // Search all configured scopes
    results, err := s.searchScopes(ctx, opts)
    if err != nil {
        return nil, err
    }
    
    // Option A: Apply summary prioritization
    if opts.PrioritizeSummaries {
        results = s.boostSummaryResults(results, opts.SummaryBoostFactor)
    }
    
    // Sort by boosted score
    sort.Slice(results, func(i, j int) bool {
        return results[i].BoostedScore > results[j].BoostedScore
    })
    
    return results[:min(len(results), opts.Limit)], nil
}

func (s *SemanticSearcher) boostSummaryResults(results []SearchResult, factor float64) []SearchResult {
    for i := range results {
        switch results[i].Type {
        case ResultTypeFileSummary, ResultTypeSymbol:
            results[i].BoostedScore = results[i].Score * factor
        default:
            results[i].BoostedScore = results[i].Score
        }
    }
    return results
}
```

**Pros:**
- Non-breaking change
- Easy to A/B test
- No re-embedding required

**Cons:**
- Still stores/computes raw file embeddings (waste)
- Doesn't improve embedding quality directly

#### Option B: Clean - Embed Intent Text Instead of Raw Bytes

This approach changes what gets embedded for files, using structured "intent text" that captures file purpose and structure.

```go
// internal/indexing/semantic/intent_text.go

package semantic

import (
    "strings"
    
    "github.com/jkatigb/agentctl/internal/indexing/symbol"
)

// IntentText represents the semantic essence of a file for embedding.
// Format:
//   [file] <relative_path>
//   [language] <detected_language>
//   [purpose] <first_paragraph_of_doc>
//   [package] <package_name>
//   [exports]
//   - type: <type_name>
//   - func: <func_name>
//   [imports]
//   - <package>
type IntentText struct {
    FilePath     string
    Language     string
    Purpose      string // First paragraph of file/package doc
    Package      string
    Exports      []ExportedSymbol
    Imports      []string
}

type ExportedSymbol struct {
    Kind string // "type", "func", "const", "var"
    Name string
}

// Build constructs intent text from file analysis.
func BuildIntentText(path string, analysis *FileAnalysis) *IntentText {
    return &IntentText{
        FilePath: path,
        Language: analysis.Language,
        Purpose:  symbol.NormalizeDoc(firstParagraph(analysis.Doc)),
        Package:  analysis.Package,
        Exports:  extractExports(analysis.Symbols),
        Imports:  analysis.Imports,
    }
}

// String renders intent text for embedding.
func (it *IntentText) String() string {
    var b strings.Builder
    
    b.WriteString("[file] ")
    b.WriteString(it.FilePath)
    b.WriteString("\n")
    
    if it.Language != "" {
        b.WriteString("[language] ")
        b.WriteString(it.Language)
        b.WriteString("\n")
    }
    
    if it.Purpose != "" {
        b.WriteString("[purpose] ")
        b.WriteString(it.Purpose)
        b.WriteString("\n")
    }
    
    if it.Package != "" {
        b.WriteString("[package] ")
        b.WriteString(it.Package)
        b.WriteString("\n")
    }
    
    if len(it.Exports) > 0 {
        b.WriteString("[exports]\n")
        for _, exp := range it.Exports {
            b.WriteString("- ")
            b.WriteString(exp.Kind)
            b.WriteString(": ")
            b.WriteString(exp.Name)
            b.WriteString("\n")
        }
    }
    
    if len(it.Imports) > 0 {
        b.WriteString("[imports]\n")
        for _, imp := range it.Imports {
            b.WriteString("- ")
            b.WriteString(imp)
            b.WriteString("\n")
        }
    }
    
    return b.String()
}

// Digest computes a stable hash of the intent text.
func (it *IntentText) Digest() string {
    return computeDigest(it.String())
}

func firstParagraph(doc string) string {
    if doc == "" {
        return ""
    }
    parts := strings.SplitN(doc, "\n\n", 2)
    return strings.TrimSpace(parts[0])
}

func extractExports(symbols []Symbol) []ExportedSymbol {
    var exports []ExportedSymbol
    for _, sym := range symbols {
        if sym.Exported {
            exports = append(exports, ExportedSymbol{
                Kind: string(sym.Kind),
                Name: sym.Name,
            })
        }
    }
    // Sort for determinism
    sort.Slice(exports, func(i, j int) bool {
        if exports[i].Kind != exports[j].Kind {
            return exports[i].Kind < exports[j].Kind
        }
        return exports[i].Name < exports[j].Name
    })
    return exports
}
```

#### Indexer Integration (Option B)

```go
// internal/indexing/semantic/indexer.go

type IndexerConfig struct {
    // ... existing fields ...
    FileTextMode string // "raw" | "intent" (default: "raw")
}

func (idx *Indexer) IndexFile(ctx context.Context, path string) error {
    switch idx.cfg.FileTextMode {
    case "intent":
        return idx.indexFileIntent(ctx, path)
    default:
        return idx.indexFileRaw(ctx, path)
    }
}

func (idx *Indexer) indexFileIntent(ctx context.Context, path string) error {
    // Analyze file structure
    analysis, err := idx.analyzer.Analyze(path)
    if err != nil {
        return err
    }
    
    // Build intent text
    intent := BuildIntentText(path, analysis)
    
    // Check if re-embed needed
    currentDigest := intent.Digest()
    stored, err := idx.store.GetFileEntry(ctx, path)
    if err == nil && stored.IntentDigest == currentDigest {
        return nil // Skip, unchanged
    }
    
    // Embed intent text
    embedding, err := idx.embedder.Embed(ctx, intent.String())
    if err != nil {
        return err
    }
    
    // Store with intent digest
    return idx.store.PutFileEntry(ctx, FileEntry{
        Path:         path,
        IntentDigest: currentDigest,
        Embedding:    embedding,
        IndexedAt:    time.Now(),
    })
}
```

#### Config Addition

```go
// internal/platform/config/config.go

type IndexingConfig struct {
    // ... existing fields ...
    FileTextMode string `env:"EMBED_FILE_TEXT_MODE" default:"raw"` // "raw" | "intent"
}
```

### Testing Strategy

#### Option A Tests

1. **Retrieval prioritization tests**
   - Summary results boosted above raw file matches
   - Boost factor configurable
   - Without flag, no boosting applied

2. **Search quality tests**
   - Queries for concepts return summary matches first
   - Queries for specific code return symbol matches first

#### Option B Tests

1. **IntentText builder tests**
   - Empty file produces minimal valid intent
   - Full file produces complete intent
   - Exports sorted deterministically
   - Imports included

2. **Intent digest stability tests**
   - Same structure with different formatting: same digest
   - Added export: different digest
   - Reordered exports: same digest (sorted)

3. **Indexer mode switching tests**
   - `raw` mode uses existing path
   - `intent` mode uses new path
   - Config change triggers re-index

4. **Search quality comparison**
   - Benchmark query relevance: intent vs raw
   - Measure embedding dimensions utilization

### Acceptance Criteria

#### Option A (Safe)

- [ ] `PrioritizeSummaries` option added to search
- [ ] Summary/symbol results boosted by configurable factor
- [ ] Default behavior unchanged without flag
- [ ] Search quality improved for concept queries

#### Option B (Clean)

- [ ] `BuildIntentText()` produces stable, semantic representation
- [ ] Intent text includes: path, language, purpose, package, exports, imports
- [ ] `EMBED_FILE_TEXT_MODE=intent` enables new path
- [ ] Intent digest stable across formatting changes
- [ ] Re-embedding triggered on structural changes
- [ ] Default behavior (`raw`) unchanged
- [ ] Backfill command extended to support file intent (future PR)

---

## Recommendation

**Start with Option A** (safe retrieval prioritization) as it:
1. Is non-breaking
2. Can be deployed immediately
3. Provides measurable quality improvement
4. Doesn't require re-embedding

**Evaluate Option B** based on:
1. Option A results
2. Embedding cost considerations (intent text is smaller)
3. Storage requirements
4. Re-indexing time for existing repos

If Option A shows significant improvement and costs justify it, proceed with Option B in a subsequent release.

---

## Dependencies

```
PR 3.1 (normalize docs) ──► PR 3.2 (intent text)
                              │
                              ├── Option A (retrieval)
                              └── Option B (indexing)
```

- PR 3.2 depends on PR 3.1 for `NormalizeDoc()` function
- Option A and Option B are mutually exclusive paths (choose one)

---

## Rollout Plan

### Phase 3.1 (Normalization)

1. **Merge PR 3.1** - Normalization applied to all new summaries
2. **Monitor** - Check summary generation rates, verify no churn
3. **Backfill** - Regenerate summaries for existing files (optional, summaries are small)

### Phase 3.2 (Intent Embedding)

**If Option A:**
1. **Merge Option A** - Retrieval prioritization enabled
2. **A/B test** - Compare search quality with/without prioritization
3. **Make default** - Enable `PrioritizeSummaries` by default

**If Option B:**
1. **Internal testing** - Enable `EMBED_FILE_TEXT_MODE=intent` on test repos
2. **Benchmark** - Compare search quality and embedding costs
3. **Merge Option B** - Intent text embedding available
4. **Gradual rollout** - Enable per-workspace, monitor quality
5. **Backfill** - Re-embed existing files with intent text
6. **Make default** - Change default in future release
