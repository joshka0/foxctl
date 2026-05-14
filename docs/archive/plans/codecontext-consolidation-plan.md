# Code Context Consolidation Plan

> **Goal:** Unify code extraction logic into `internal/intelligence/codecontext/` so skills
> become thin wrappers. Eliminate 1000+ LOC of duplication across
> `code_swe_grep`, `code_context_ripgrep`, `code_semantic_search`, and
> `code_smart_write`.

## Current State (from exploration)

### Duplication Found

| Pattern | LOC | Files |
|---------|-----|-------|
| `detectLanguage()` | ~300 | 12 skills (code_swe_grep, code_context_ripgrep, code_smart_write, code_stats, code_symbols, code_imports, etc.) |
| Block boundary detection | ~500 | code_context_ripgrep/expander.go, code_swe_grep/main.go |
| File reading + truncation | ~200 | code_swe_grep, fs_read, code_semantic_search |
| Brace/indent end finding | ~150 | Identical in expander.go and code_swe_grep |
| Line splitting/grouping | ~100 | code_swe_grep, code_smart_write |

**Total: ~1250 LOC duplicated**

### Security Issues

1. **TOCTOU in code_semantic_search**: Uses `os.ReadFile()` without validation
2. **Silent failures**: `extractSnippet()` returns empty on error (no logging)
3. **Brace matching fragility**: Doesn't handle braces in strings/comments

### Architecture Problems

- No clear separation between "find candidates" vs "extract evidence" vs "analyze"
- Skills re-implement file reading instead of sharing
- `internal/platform/fsutil.DetectLanguage()` exists but skills don't use it

## Target Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Code Context Funnel                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  code/semantic_search ──► code/snippet_extract ──► code/counsel            │
│         │                      │                  │                 │
│    "where to look"      "what's relevant"  "what it means"          │
│         │                      │                  │                 │
│    Candidates[]           Evidence{}         Analysis{}             │
│                                                                     │
├─────────────────────────────────────────────────────────────────────┤
│                    internal/intelligence/codecontext/                            │
├─────────────────────────────────────────────────────────────────────┤
│  collect.go    │ Collect(query, candidates, opts) → Evidence        │
│  render.go     │ Render(evidence, mode) → string/ndjson             │
│  types.go      │ Candidate, Snippet, Evidence, RenderMode           │
├────────────────┼────────────────────────────────────────────────────┤
│  files/        │ SafeReader (TOCTOU-safe), LineMap, Truncation      │
│  lang/         │ DetectLanguage, CommentMarkers, Patterns           │
│  expander/     │ BlockExpander (Go, Python, JS/TS, GDScript, etc.)  │
│  selector/     │ Heuristic, LLM-assisted snippet selection          │
└────────────────┴────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: Foundation (internal/intelligence/codecontext/) — High Impact

**Goal:** Create the shared package with core types and safe file reading.

**Files to create:**

```
internal/intelligence/codecontext/
├── types.go          # Candidate, Snippet, Evidence, RenderMode
├── collect.go        # Collect() main entry point
├── render.go         # Render() with mode dispatch
├── files/
│   └── reader.go     # SafeReader (TOCTOU-safe, symlink-checked)
└── lang/
    └── detect.go     # Re-export from platform/fsutil + Language enum
```

**types.go:**
```go
package codecontext

type Candidate struct {
    Path     string  `json:"path"`
    SymbolID string  `json:"symbol_id,omitempty"`
    LineHint int     `json:"line,omitempty"`
    Priority float64 `json:"priority"`
}

type Snippet struct {
    File      string `json:"file"`
    SymbolID  string `json:"symbol_id,omitempty"`
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
    Text      string `json:"text"`
    Reason    string `json:"reason,omitempty"`
}

type Evidence struct {
    Snippets  []Snippet         `json:"snippets"`
    Stats     EvidenceStats     `json:"stats"`
    Truncated bool              `json:"truncated"`
}

type RenderMode string
const (
    ModeSnippets  RenderMode = "snippets"  // Disjoint code regions
    ModeMasked    RenderMode = "masked"    // Full file with redactions
    ModeStructure RenderMode = "structure" // Signatures/imports only
    ModeFlow      RenderMode = "flow"      // Control-flow excerpts
)
```

**files/reader.go:**
```go
package files

// SafeReader implements TOCTOU-safe file reading with validation
type SafeReader struct {
    validator  *pathvalidator.PathValidator
    maxBytes   int64
}

func (r *SafeReader) Read(ctx context.Context, path string) (*FileContent, error) {
    // 1. Validate path
    // 2. Open immediately
    // 3. Stat from descriptor
    // 4. Re-validate resolved symlink path
    // 5. Read from open descriptor
    // Pattern from code_swe_grep:1013-1119
}

type FileContent struct {
    Path       string
    Content    []byte
    Lines      []string    // Pre-split for efficiency
    LineMap    []int       // Byte offset of each line start
    Truncated  bool
    Language   string
}
```

**Acceptance criteria:**
- [ ] `SafeReader` passes TOCTOU security tests (symlink swap attack)
- [ ] `FileContent.Lines` computed once, reused everywhere
- [ ] Language detection delegates to `platform/fsutil.DetectLanguage()`

---

### Phase 2: Block Expansion (internal/intelligence/codecontext/expander/) — High Impact

**Goal:** Consolidate the two 500-LOC block expansion implementations.

**Source:** Merge logic from:
- `skills/code_context_ripgrep/expander.go` (comprehensive, language-aware)
- `skills/code_swe_grep/main.go:506-735` (Go AST integration)

**Files to create:**

```
internal/intelligence/codecontext/expander/
├── expander.go       # BlockExpander interface + registry
├── go.go             # Go: AST-based (from code_swe_grep)
├── python.go         # Python: indentation-based
├── jsts.go           # JavaScript/TypeScript: brace-based
├── gdscript.go       # GDScript: mixed
├── generic.go        # Fallback: blank-line boundaries
└── brace.go          # Shared brace/indent counting utilities
```

**expander.go:**
```go
package expander

type BlockExpander interface {
    // FindBlock returns the enclosing block for a given line
    FindBlock(content *files.FileContent, line int) (start, end int, symbol string, err error)

    // ExpandToSymbol finds a named symbol's body
    ExpandToSymbol(content *files.FileContent, symbolName string) (start, end int, err error)
}

var registry = map[string]BlockExpander{
    "go":         &GoExpander{},
    "python":     &PythonExpander{},
    "typescript": &JSTSExpander{},
    "javascript": &JSTSExpander{},
    "gdscript":   &GDScriptExpander{},
}

func Get(language string) BlockExpander {
    if exp, ok := registry[language]; ok {
        return exp
    }
    return &GenericExpander{}
}
```

**go.go (uses existing symbol.GoExtractor):**
```go
package expander

import "github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"

type GoExpander struct{}

func (e *GoExpander) FindBlock(content *files.FileContent, line int) (int, int, string, error) {
    // Use symbol.GoExtractor for AST-based extraction
    extractor := symbol.NewGoExtractor()
    symbols, err := extractor.Extract(ctx, content.Path, content.Content)
    // Find symbol containing line, return its bounds
}
```

**Acceptance criteria:**
- [ ] `GoExpander` uses AST (no regex fallback for Go)
- [ ] `PythonExpander` handles indentation correctly (including nested)
- [ ] `JSTSExpander` handles braces in strings/comments (regex for string literals)
- [ ] All expanders share `brace.go` utilities (no duplication)

---

### Phase 3: Selector Abstraction (internal/intelligence/codecontext/selector/) — Medium Impact

**Goal:** Make snippet selection pluggable (heuristic vs LLM-assisted).

**Files to create:**

```
internal/intelligence/codecontext/selector/
├── selector.go       # Selector interface
├── heuristic.go      # Keyword matching + symbol priority
└── llm.go            # LLM-assisted selection (for swe_grep)
```

**selector.go:**
```go
package selector

type Selector interface {
    // Select returns relevant spans from file content given a query
    Select(ctx context.Context, query string, content *files.FileContent, hints Hints) ([]Span, error)
}

type Span struct {
    StartLine int
    EndLine   int
    Reason    string
    Priority  float64
}

type Hints struct {
    SymbolID   string   // If targeting specific symbol
    LineHint   int      // If targeting specific line
    Keywords   []string // Pre-extracted keywords
}
```

**heuristic.go:**
```go
package selector

type HeuristicSelector struct {
    ContextLines int // Lines before/after match
}

func (s *HeuristicSelector) Select(ctx context.Context, query string, content *files.FileContent, hints Hints) ([]Span, error) {
    // Extract keywords from query (filter stop words, min length)
    // Find matching lines
    // Group into blocks with context
    // Sort by priority
    // Pattern from code_swe_grep:812-883
}
```

**Acceptance criteria:**
- [ ] `HeuristicSelector` produces same output as current `code_swe_grep` keyword matching
- [ ] `LLMSelector` interface allows future Qwen/local model integration
- [ ] Selectors are stateless and testable

---

### Phase 4: Skill Migration — Medium Impact

**Goal:** Convert skills to thin wrappers over `internal/intelligence/codecontext/`.

**Migration order (by impact):**

1. **code_swe_grep** (1320 LOC → ~300 LOC)
   - Use `codecontext.Collect()` with `ModeSnippets`
   - Keep input/output contract unchanged
   - Remove local `detectLanguage`, `findBraceEnd`, `extractSymbolBody`

2. **code_context_ripgrep** (1007 LOC → ~200 LOC)
   - Use `codecontext.Collect()` with ripgrep matches as candidates
   - Remove entire `expander.go` (610 LOC)
   - Keep ripgrep integration, delegate expansion

3. **code_semantic_search** (2026 LOC → ~1800 LOC)
   - Use `codecontext.SafeReader` for snippet extraction
   - Fix TOCTOU vulnerability
   - Keep vector search logic (not duplicated)

4. **code_smart_write** (~800 LOC → ~600 LOC)
   - Use `codecontext/expander` for symbol detection
   - Remove local `detectLanguage`, `findSymbols`

5. **New: smart_read_file** (alias)
   - Thin wrapper: `swe_grep` with `mode=masked`, explicit files

6. **New: code_counsel** (~200 LOC new)
   - Analysis-only skill
   - Input: `evidence_artifact` (sha256 digest) or `snippets_inline`
   - Never reads files

**Per-skill migration template:**
```go
func run(ctx context.Context, rc *skillmain.RunContext, input Input) error {
    // 1. Build candidates from input
    candidates := buildCandidates(input)

    // 2. Collect evidence (delegates to codecontext)
    evidence, err := codecontext.Collect(ctx, codecontext.CollectOpts{
        Candidates:   candidates,
        Query:        input.Question,
        Mode:         codecontext.ModeSnippets,
        MaxFiles:     input.Limits.MaxFiles,
        MaxSnippets:  input.Limits.MaxSnippets,
        MaxBytes:     input.Limits.MaxBytesPerFile,
        PathValidator: rc.PathValidator(),
    })
    if err != nil { return err }

    // 3. Render output (inline or artifact)
    return emitEvidence(rc, evidence, input.Limits.MaxInlineKB)
}
```

---

### Phase 5: Cleanup & Documentation — Low Impact

**Goal:** Remove dead code, update documentation.

**Tasks:**
1. Delete `skills/code_context_ripgrep/expander.go` after migration
2. Delete duplicate `detectLanguage()` from all skills
3. Remove `findBraceEnd`, `findIndentEnd` from `code_swe_grep`
4. Update `docs/skills/` with new architecture
5. Add `internal/intelligence/codecontext/README.md`

---

## Risk Mitigations

| Risk | Mitigation |
|------|------------|
| Regression in snippet quality | Golden tests: compare output before/after for sample repos |
| Performance degradation | Benchmark: measure latency per skill before/after |
| Breaking skill input/output | Keep contracts unchanged; internal refactor only |
| Go AST slower than regex | Benchmark: GoExpander vs regex on large files |

## Metrics

**Success criteria:**
- [ ] 1000+ LOC removed from skills/
- [ ] Zero duplicate `detectLanguage()` implementations
- [ ] Zero duplicate `findBraceEnd()` implementations
- [ ] code_semantic_search passes TOCTOU security test
- [ ] All existing skill tests pass unchanged

## Dependencies

- `internal/intelligence/indexing/symbol/` - GoExtractor (already exists, good quality)
- `internal/platform/fsutil/` - DetectLanguage (already exists, comprehensive)
- `internal/adapters/skillslib/pathvalidator/` - PathValidator (already exists)

## Files Summary

**New packages:**
```
internal/intelligence/codecontext/
├── types.go              # ~50 LOC
├── collect.go            # ~150 LOC
├── render.go             # ~100 LOC
├── files/
│   └── reader.go         # ~120 LOC
├── lang/
│   └── detect.go         # ~30 LOC (re-export)
├── expander/
│   ├── expander.go       # ~50 LOC
│   ├── go.go             # ~80 LOC
│   ├── python.go         # ~100 LOC
│   ├── jsts.go           # ~120 LOC
│   ├── gdscript.go       # ~100 LOC
│   ├── generic.go        # ~50 LOC
│   └── brace.go          # ~80 LOC
└── selector/
    ├── selector.go       # ~30 LOC
    └── heuristic.go      # ~100 LOC

Total new: ~1160 LOC
```

**Deleted after migration:**
- `skills/code_context_ripgrep/expander.go` (~610 LOC)
- Duplicate functions in `code_swe_grep` (~400 LOC)
- Duplicate `detectLanguage()` across 12 skills (~200 LOC)

**Net change: -1210 + 1160 = -50 LOC** (with better organization)

## Implementation Order

1. **Phase 1** (Foundation) - Can start immediately
2. **Phase 2** (Expander) - Depends on Phase 1
3. **Phase 3** (Selector) - Depends on Phase 1
4. **Phase 4** (Skill Migration) - Depends on Phases 1-3
5. **Phase 5** (Cleanup) - After Phase 4

Phases 2 and 3 can run in parallel after Phase 1.
