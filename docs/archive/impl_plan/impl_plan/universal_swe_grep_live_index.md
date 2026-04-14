# Implementation Plan: Universal SWE Grep + Live Index

Status: Draft
Created: 2024-12-23
Scope: Live indexing hook, shared retrieval package, universal SWE grep skill and tool enhancement

This plan builds on `universal_swe_grep_and_agents.md` to add:
- **Live indexing** during edits (not just post-review)
- **Auto-candidate generation** for SWE grep
- **Unified retrieval** across symbol + semantic indexes

---

## Overview

### Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Edit Flow (Live Index)                        │
├─────────────────────────────────────────────────────────────────────┤
│  Claude Code Edit/Write                                              │
│         ↓                                                            │
│  PostToolUse: live-index.sh                                         │
│         ↓                                                            │
│  code/incremental_index skill                                        │
│         ↓                                                            │
│  ┌─────────────────────┐    ┌─────────────────────┐                 │
│  │   Symbol Index      │    │   Embedding Queue   │                 │
│  │   (sync, fast)      │    │   (async, batched)  │                 │
│  └─────────────────────┘    └─────────────────────┘                 │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                       Query Flow (Universal SWE Grep)                │
├─────────────────────────────────────────────────────────────────────┤
│  Agent/Hook/CLI: "How does authentication work?"                     │
│         ↓                                                            │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  internal/intelligence/retrieval - Candidate Generator                    │    │
│  │  ├── Symbol Index (BM25)                                    │    │
│  │  ├── Semantic Index (Hybrid if embeddings available)        │    │
│  │  └── Ripgrep Fallback (if too few candidates)               │    │
│  └─────────────────────────────────────────────────────────────┘    │
│         ↓                                                            │
│  code/snippet_extract skill (existing)                                      │
│         ↓                                                            │
│  High-signal code snippets                                           │
└─────────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Type | Purpose |
|-----------|------|---------|
| `internal/intelligence/retrieval/` | Go package | Shared candidate generation logic |
| `code/incremental_index` | Exec skill | Index single file (symbols + optional embed) |
| `live-index.sh` | Hook | Trigger indexing on edit |
| `code/smart_search` | Exec skill | Standalone auto-candidate SWE grep |
| `code.swe_grep` tool | Agent tool | Enhanced with `auto_candidates` flag |

---

## Phase 1: Shared Retrieval Package

**Goal**: Create reusable candidate generation logic for both skill and tool.

**Location**: `internal/intelligence/retrieval/`

### Files

```
internal/intelligence/retrieval/
├── candidates.go      # CandidateGenerator implementation
├── candidates_test.go # Unit tests
├── merge.go           # Candidate merging and ranking
├── options.go         # Configuration options
└── doc.go             # Package documentation
```

### Key Types

```go
// internal/intelligence/retrieval/candidates.go

package retrieval

// Candidate represents a file/symbol candidate for SWE grep.
type Candidate struct {
    Path     string  `json:"path"`
    SymbolID string  `json:"symbol_id,omitempty"`
    Name     string  `json:"name,omitempty"`
    Kind     string  `json:"kind,omitempty"`
    Score    float64 `json:"score"`
    Source   string  `json:"source"` // "symbol", "semantic", "ripgrep"
}

// Options controls candidate generation behavior.
type Options struct {
    MaxSymbolCandidates   int     // Max from symbol index (default: 30)
    MaxSemanticCandidates int     // Max from semantic index (default: 20)
    MaxRipgrepCandidates  int     // Max from ripgrep fallback (default: 10)
    MinTotalCandidates    int     // Minimum before ripgrep kicks in (default: 5)
    MaxTotalCandidates    int     // Final limit after merge (default: 50)
    SemanticWeight        float64 // Weight for semantic scores (default: 0.7)
    SymbolWeight          float64 // Weight for symbol scores (default: 1.0)
}

// Generator generates candidates from multiple sources.
type Generator struct {
    memoryStore   storage.MemoryStore
    embedProvider semantic.EmbeddingProvider // nil = skip semantic search
    workspaceRoot string
    logger        zerolog.Logger
}

// Generate produces ranked candidates for a question.
func (g *Generator) Generate(ctx context.Context, workspaceID, question string, opts Options) ([]Candidate, error)
```

### Dependencies

- `internal/storage/memory` - Memory store access
- `internal/intelligence/indexing/symbol` - Symbol types
- `internal/intelligence/indexing/semantic` - Embedding provider interface

### Tasks

- [ ] Create `internal/intelligence/retrieval/options.go` with Options type and defaults
- [ ] Create `internal/intelligence/retrieval/candidates.go` with Generator struct
- [ ] Implement `searchSymbolIndex()` - reuse logic from `code_tools.go`
- [ ] Implement `searchSemanticIndex()` - use SearchableStore
- [ ] Implement `ripgrepFallback()` - simple keyword grep
- [ ] Create `internal/intelligence/retrieval/merge.go` with candidate merging/ranking
- [ ] Write unit tests with mock stores
- [ ] Integration test with real SQLite store

---

## Phase 2: Live Index Skill

**Goal**: Create skill to index a single file on-demand.

**Location**: `skills/code_incremental_index/`

### Input Contract

```json
{
  "file": "path/to/file.go",
  "workspace_id": "default",
  "symbols": true,
  "embed": false,
  "embed_queue": false
}
```

### Output Contract

```json
{
  "version": 1,
  "status": "ok",
  "command": "code/incremental_index",
  "data": {
    "file": "path/to/file.go",
    "language": "go",
    "symbols_extracted": 12,
    "symbols_updated": 8,
    "symbols_deleted": 2,
    "embedding_queued": false,
    "duration_ms": 45
  }
}
```

### Files

```
skills/code_incremental_index/
├── main.go        # Skill entry point
├── main_test.go   # Unit tests
├── skill.yaml     # Manifest
└── testdata/      # Test fixtures
```

### Implementation

```go
// skills/code_incremental_index/main.go

func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
    start := time.Now()

    // 1. Validate and read file
    absPath, err := rc.PathValidator.ValidatePath(in.File)
    if err != nil {
        return fail(ErrCodePolicy, err)
    }
    content, err := os.ReadFile(absPath)
    if err != nil {
        return fail(ErrCodeIO, err)
    }

    // 2. Detect language
    lang := detectLanguage(in.File)
    if lang == "" {
        return emit(skippedResult("unsupported file type"))
    }

    // 3. Extract symbols (if enabled)
    var symbolsExtracted, symbolsUpdated, symbolsDeleted int
    if in.Symbols {
        extractor := symbol.NewExtractor(lang)
        symbols, err := extractor.Extract(in.File, content)
        if err != nil {
            // Log but don't fail - partial indexing is okay
            log.Warn().Err(err).Msg("symbol extraction failed")
        } else {
            symbolsExtracted = len(symbols)
            updated, deleted, err := upsertSymbols(ctx, store, in.WorkspaceID, in.File, symbols)
            if err != nil {
                return fail(ErrCodeRuntime, err)
            }
            symbolsUpdated = updated
            symbolsDeleted = deleted
        }
    }

    // 4. Queue embedding (if enabled)
    embeddingQueued := false
    if in.EmbedQueue {
        if err := queueEmbeddingJob(in.WorkspaceID, in.File); err != nil {
            log.Warn().Err(err).Msg("failed to queue embedding job")
        } else {
            embeddingQueued = true
        }
    }

    // 5. Emit result
    return rc.Emit(Command, map[string]any{
        "file":              in.File,
        "language":          lang,
        "symbols_extracted": symbolsExtracted,
        "symbols_updated":   symbolsUpdated,
        "symbols_deleted":   symbolsDeleted,
        "embedding_queued":  embeddingQueued,
        "duration_ms":       time.Since(start).Milliseconds(),
    }, "application/json", envelope.Meta{})
}
```

### Tasks

- [ ] Create `skills/code_incremental_index/skill.yaml` manifest
- [ ] Create `skills/code_incremental_index/main.go` entry point
- [ ] Implement `detectLanguage()` based on file extension
- [ ] Implement `upsertSymbols()` - upsert to named memory, delete stale
- [ ] Wire up existing Go extractor (`internal/intelligence/indexing/symbol/extractor_go.go`)
- [ ] Add support for Python (tree-sitter or simple regex)
- [ ] Add support for TypeScript/JavaScript (tree-sitter or simple regex)
- [ ] Implement `queueEmbeddingJob()` stub (Phase 6)
- [ ] Write tests with fixture files
- [ ] Add to Makefile skills-build target

---

## Phase 3: Live Index Hook

**Goal**: Trigger incremental indexing on file edits.

**Location**: `.claude/hooks/live-index.sh`

### Implementation

```bash
#!/usr/bin/env bash
# live-index.sh - Index edited files for symbol search
#
# PostToolUse hook for Edit|Write|MultiEdit|NotebookEdit
# Extracts symbols from edited files into the memory store
# for faster code search and universal SWE grep.
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_LIVE_INDEX_DISABLED - Set to "1" to disable

set -euo pipefail

# Check if disabled
[[ "${AGENTCTL_LIVE_INDEX_DISABLED:-}" == "1" ]] && echo '{}' && exit 0

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"

# Read hook input
INPUT=$(cat)

# Extract file path from tool_input
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only index supported file types
case "$file_path" in
  *.go|*.py|*.ts|*.tsx|*.js|*.jsx|*.gd)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip vendor/node_modules/generated
case "$file_path" in
  */vendor/*|*/node_modules/*|*/.git/*|*/dist/*|*/build/*)
    echo '{}'
    exit 0
    ;;
esac

# Run incremental index (symbols only - fast)
result=$("$AGENTCTL_BIN" run code/incremental_index \
  --input "{\"file\":\"$file_path\",\"symbols\":true,\"embed\":false}" \
  2>/dev/null) || {
  # Don't block on indexing failures
  echo '{}'
  exit 0
}

# Check if successful
status=$(echo "$result" | jq -r '.status // "error"')
if [[ "$status" != "ok" ]]; then
  echo '{}'
  exit 0
fi

# Extract stats for context message
symbols_updated=$(echo "$result" | jq -r '.data.symbols_updated // 0')
duration_ms=$(echo "$result" | jq -r '.data.duration_ms // 0')

# Only show context if we indexed something
if [[ "$symbols_updated" -gt 0 ]]; then
  filename=$(basename "$file_path")
  jq -n --arg ctx "Indexed **$symbols_updated** symbols from \`$filename\` (${duration_ms}ms)" '{
    decision: "approve",
    context: $ctx
  }'
else
  echo '{}'
fi
```

### Settings Registration

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write|MultiEdit|NotebookEdit",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/live-index.sh",
            "timeout": 5
          }
        ]
      }
    ]
  }
}
```

### Tasks

- [ ] Create `.claude/hooks/live-index.sh`
- [ ] Make executable: `chmod +x .claude/hooks/live-index.sh`
- [ ] Register in `.claude/settings.json` (add to existing PostToolUse array)
- [ ] Test with manual file edit
- [ ] Verify symbols appear in `agentctl memory list`

---

## Phase 4: Universal SWE Grep Skill (Option B)

**Goal**: Standalone skill for auto-candidate SWE grep.

**Location**: `skills/code_universal_swe_grep/`

### Input Contract

```json
{
  "workspace_id": "default",
  "question": "How does user authentication work?",
  "sources": ["symbols", "semantic", "ripgrep"],
  "limits": {
    "max_candidates": 50,
    "max_snippets": 20,
    "max_bytes_per_file": 65536
  }
}
```

### Output Contract

```json
{
  "version": 1,
  "status": "ok",
  "command": "code/smart_search",
  "data": {
    "summary": {
      "candidates_generated": 45,
      "candidates_from_symbols": 30,
      "candidates_from_semantic": 12,
      "candidates_from_ripgrep": 3,
      "files_relevant": 12,
      "snippets_emitted": 8,
      "duration_ms": 234
    },
    "candidates": [
      {"path": "auth/login.go", "symbol_id": "auth/login.go:Login", "score": 0.95, "source": "symbols"},
      {"path": "auth/session.go", "score": 0.82, "source": "semantic"}
    ],
    "snippets_inline": [
      {"file": "auth/login.go", "start_line": 15, "end_line": 45, "preview": "func Login..."}
    ],
    "artifact": "sha256:abc123..."
  }
}
```

### Files

```
skills/code_universal_swe_grep/
├── main.go        # Skill entry point
├── main_test.go   # Unit tests
├── skill.yaml     # Manifest
└── testdata/      # Test fixtures
```

### Implementation Flow

```go
func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
    start := time.Now()

    // 1. Open memory store
    store, err := openMemoryStore(ctx)
    if err != nil {
        return fail(ErrCodeRuntime, err)
    }
    defer store.Close()

    // 2. Create candidate generator (uses internal/intelligence/retrieval)
    generator := retrieval.NewGenerator(store, embedProvider, workspaceRoot, logger)

    // 3. Generate candidates
    opts := retrieval.Options{
        MaxSymbolCandidates:   in.Limits.MaxCandidates,
        MaxSemanticCandidates: in.Limits.MaxCandidates / 2,
        MaxTotalCandidates:    in.Limits.MaxCandidates,
    }
    candidates, err := generator.Generate(ctx, in.WorkspaceID, in.Question, opts)
    if err != nil {
        return fail(ErrCodeRuntime, err)
    }

    // 4. Convert to swe_grep input format
    sweGrepCandidates := make([]map[string]any, len(candidates))
    for i, c := range candidates {
        sweGrepCandidates[i] = map[string]any{
            "path":      c.Path,
            "symbol_id": c.SymbolID,
            "priority":  c.Score,
        }
    }

    // 5. Invoke code/snippet_extract skill
    sweGrepInput := map[string]any{
        "workspace_id": in.WorkspaceID,
        "question":     in.Question,
        "candidates":   sweGrepCandidates,
        "limits": map[string]any{
            "max_snippets":       in.Limits.MaxSnippets,
            "max_bytes_per_file": in.Limits.MaxBytesPerFile,
        },
    }
    snippets, err := invokeSweGrep(ctx, rc, sweGrepInput)
    if err != nil {
        return fail(ErrCodeRuntime, err)
    }

    // 6. Emit combined result
    return rc.Emit(Command, map[string]any{
        "summary": map[string]any{
            "candidates_generated":    len(candidates),
            "candidates_from_symbols": countBySource(candidates, "symbols"),
            "candidates_from_semantic": countBySource(candidates, "semantic"),
            "candidates_from_ripgrep": countBySource(candidates, "ripgrep"),
            "files_relevant":          snippets.FilesRelevant,
            "snippets_emitted":        len(snippets.Inline),
            "duration_ms":             time.Since(start).Milliseconds(),
        },
        "candidates":      candidates,
        "snippets_inline": snippets.Inline,
        "artifact":        snippets.Artifact,
    }, "application/json", envelope.Meta{})
}
```

### Tasks

- [ ] Create `skills/code_universal_swe_grep/skill.yaml` manifest
- [ ] Create `skills/code_universal_swe_grep/main.go` entry point
- [ ] Import and use `internal/intelligence/retrieval.Generator`
- [ ] Implement `invokeSweGrep()` - invoke existing skill
- [ ] Handle case when no candidates found
- [ ] Write integration tests
- [ ] Add to Makefile skills-build target

---

## Phase 5: Enhance code.swe_grep Tool (Option A)

**Goal**: Add `auto_candidates` flag to existing tool for in-process retrieval.

**Location**: `internal/agent/tools/code_tools.go`

### Updated Tool Schema

```go
sweGrepTool := dstools.NewFuncTool(
    "code.swe_grep",
    "Extract high-signal code snippets. Use auto_candidates=true to automatically find relevant files, or provide candidate_files explicitly.",
    models.InputSchema{
        Type: "object",
        Properties: map[string]models.ParameterSchema{
            "workspace_id": {
                Type:        "string",
                Description: "Workspace identifier",
                Required:    true,
            },
            "question": {
                Type:        "string",
                Description: "Natural language question to guide snippet extraction",
                Required:    true,
            },
            "auto_candidates": {
                Type:        "boolean",
                Description: "If true, automatically generate candidates from symbol and semantic indexes. Mutually exclusive with candidate_files.",
            },
            "candidate_files": {
                Type:        "array",
                Description: "Array of candidate files (if auto_candidates is false)",
            },
            "max_candidates": {
                Type:        "integer",
                Description: "Maximum candidates to generate when auto_candidates=true (default 50)",
            },
        },
    },
    r.wrapWithTelemetry("code.swe_grep", r.codeSweGrep),
)
```

### Implementation Changes

```go
func (r *Registry) codeSweGrep(ctx context.Context, args map[string]any) (*models.CallToolResult, error) {
    // ... existing validation ...

    autoCandidates, _ := args["auto_candidates"].(bool)
    candidatesRaw, hasCandidates := args["candidate_files"].([]any)

    // Validate mutual exclusivity
    if autoCandidates && hasCandidates && len(candidatesRaw) > 0 {
        return errorResult("auto_candidates and candidate_files are mutually exclusive"), nil
    }

    var candidates []SweGrepCandidate

    if autoCandidates {
        // Auto-generate candidates using retrieval package
        maxCandidates := 50
        if m, ok := args["max_candidates"].(float64); ok && m > 0 {
            maxCandidates = int(m)
        }

        generated, err := r.generateCandidates(ctx, workspaceID, question, maxCandidates)
        if err != nil {
            return errorResult(fmt.Sprintf("generate candidates: %v", err)), nil
        }
        candidates = generated
    } else {
        // Use provided candidates (existing logic)
        // ... parse candidatesRaw ...
    }

    if len(candidates) == 0 {
        return successResult(map[string]any{
            "snippets": []SweGrepSnippet{},
            "count":    0,
            "message":  "no candidates found",
        }), nil
    }

    // ... rest of existing skill invocation ...
}

// generateCandidates uses the retrieval package for auto-candidate generation.
func (r *Registry) generateCandidates(ctx context.Context, workspaceID, question string, maxCandidates int) ([]SweGrepCandidate, error) {
    if r.openMemory == nil {
        return nil, fmt.Errorf("memory store not configured")
    }

    store, err := r.openMemory(ctx)
    if err != nil {
        return nil, err
    }
    defer store.Close()

    generator := retrieval.NewGenerator(store, r.embedProvider, r.config.WorkspaceRoot, r.logger)
    opts := retrieval.DefaultOptions()
    opts.MaxTotalCandidates = maxCandidates

    candidates, err := generator.Generate(ctx, workspaceID, question, opts)
    if err != nil {
        return nil, err
    }

    // Convert to SweGrepCandidate format
    result := make([]SweGrepCandidate, len(candidates))
    for i, c := range candidates {
        result[i] = SweGrepCandidate{
            Path:     c.Path,
            SymbolID: c.SymbolID,
            Priority: c.Score,
        }
    }
    return result, nil
}
```

### Tasks

- [ ] Update tool schema to include `auto_candidates` and `max_candidates`
- [ ] Add `embedProvider` field to Registry (optional)
- [ ] Implement `generateCandidates()` method
- [ ] Update tool description for agents
- [ ] Write unit tests for auto-candidate mode
- [ ] Update any agent configs that use this tool

---

## Phase 6: AST Support for Python/TypeScript (tree-sitter)

**Goal**: Replace regex-based extraction with proper AST parsing for Python and TypeScript.

**Location**: `internal/intelligence/indexing/symbol/`

### Current State

- **Go**: Full AST support via `go/ast` parser (accurate, fast)
- **Python/JavaScript/TypeScript**: Simple regex-based extraction (misses nested symbols, decorators, arrow functions)
- **GDScript**: Not yet implemented

### Options

| Approach | Pros | Cons |
|----------|------|------|
| **Tree-sitter (Go bindings)** | Unified parser for all languages, fast, incremental | CGO dependency, binary size |
| **Language-specific parsers** | No CGO for some (e.g., Python AST in Go) | Multiple deps, inconsistent APIs |
| **External tools** | Leverage existing tools (ctags, etc.) | Process overhead, installation required |

### Recommended: Tree-sitter

Tree-sitter provides fast, accurate parsing with incremental updates - ideal for live indexing.

**Go bindings**: `github.com/tree-sitter/go-tree-sitter`

**Language grammars**:
- `github.com/tree-sitter/tree-sitter-python`
- `github.com/tree-sitter/tree-sitter-typescript`
- `github.com/tree-sitter/tree-sitter-javascript`
- Custom GDScript grammar (if available) or fallback to regex

### Files

```
internal/intelligence/indexing/symbol/
├── extractor.go          # Common interface
├── extractor_go.go       # Go AST (existing)
├── extractor_treesitter.go # Tree-sitter wrapper
├── extractor_python.go   # Python via tree-sitter
├── extractor_typescript.go # TypeScript via tree-sitter
├── extractor_javascript.go # JavaScript via tree-sitter
├── extractor_gdscript.go # GDScript (regex or tree-sitter)
└── testdata/             # Test fixtures for each language
```

### Implementation

```go
// internal/intelligence/indexing/symbol/extractor_treesitter.go

package symbol

import (
    sitter "github.com/tree-sitter/go-tree-sitter"
    python "github.com/tree-sitter/tree-sitter-python/bindings/go"
    typescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// TreeSitterExtractor provides AST-based symbol extraction.
type TreeSitterExtractor struct {
    parser   *sitter.Parser
    language *sitter.Language
    lang     string
    queries  map[string]*sitter.Query // Cached queries per symbol type
}

// NewTreeSitterExtractor creates an extractor for the given language.
func NewTreeSitterExtractor(lang string) (*TreeSitterExtractor, error) {
    parser := sitter.NewParser()

    var language *sitter.Language
    switch lang {
    case "python":
        language = python.GetLanguage()
    case "typescript", "javascript":
        language = typescript.GetLanguage() // TypeScript grammar handles both
    default:
        return nil, fmt.Errorf("unsupported language: %s", lang)
    }

    parser.SetLanguage(language)

    return &TreeSitterExtractor{
        parser:   parser,
        language: language,
        lang:     lang,
        queries:  make(map[string]*sitter.Query),
    }, nil
}

// Extract parses the file and extracts symbols.
func (e *TreeSitterExtractor) Extract(ctx context.Context, path string, content []byte) ([]Symbol, error) {
    tree := e.parser.Parse(content, nil)
    defer tree.Close()

    root := tree.RootNode()

    var symbols []Symbol

    switch e.lang {
    case "python":
        symbols = e.extractPythonSymbols(root, path, content)
    case "typescript", "javascript":
        symbols = e.extractTSSymbols(root, path, content)
    }

    return symbols, nil
}

// extractPythonSymbols walks the AST for Python symbols.
func (e *TreeSitterExtractor) extractPythonSymbols(root *sitter.Node, path string, content []byte) []Symbol {
    var symbols []Symbol

    // Query for function definitions
    // (function_definition name: (identifier) @name)
    funcQuery := `(function_definition name: (identifier) @name)`
    e.querySymbols(root, funcQuery, content, func(node *sitter.Node, name string) {
        symbols = append(symbols, Symbol{
            ID:        ID(path, name),
            FilePath:  path,
            Name:      name,
            Language:  "python",
            Kind:      KindFunction,
            StartLine: int(node.StartPosition().Row) + 1,
            EndLine:   int(node.EndPosition().Row) + 1,
        })
    })

    // Query for class definitions
    classQuery := `(class_definition name: (identifier) @name)`
    e.querySymbols(root, classQuery, content, func(node *sitter.Node, name string) {
        symbols = append(symbols, Symbol{
            ID:        ID(path, name),
            FilePath:  path,
            Name:      name,
            Language:  "python",
            Kind:      KindClass,
            StartLine: int(node.StartPosition().Row) + 1,
            EndLine:   int(node.EndPosition().Row) + 1,
        })
    })

    // Query for decorated functions (captures decorators)
    decoratedQuery := `(decorated_definition (decorator)+ (function_definition name: (identifier) @name))`
    // ... similar processing ...

    return symbols
}
```

### Python Symbol Types

| Node Type | Symbol Kind | Notes |
|-----------|-------------|-------|
| `function_definition` | Function | Top-level and nested |
| `class_definition` | Class | With methods as nested |
| `decorated_definition` | Function/Class | Preserves decorator info |
| `async_function_definition` | Function | Async functions |

### TypeScript Symbol Types

| Node Type | Symbol Kind | Notes |
|-----------|-------------|-------|
| `function_declaration` | Function | Named functions |
| `class_declaration` | Class | Classes with methods |
| `interface_declaration` | Interface | TypeScript interfaces |
| `type_alias_declaration` | Type | Type aliases |
| `method_definition` | Method | Class methods |
| `arrow_function` | Function | Arrow functions (with assignment) |
| `variable_declarator` | Variable | const/let/var exports |

### Tasks

- [ ] Add tree-sitter dependency: `go get github.com/tree-sitter/go-tree-sitter`
- [ ] Add Python grammar: `go get github.com/tree-sitter/tree-sitter-python/bindings/go`
- [ ] Add TypeScript grammar: `go get github.com/tree-sitter/tree-sitter-typescript/bindings/go`
- [ ] Create `internal/intelligence/indexing/symbol/extractor_treesitter.go` base
- [ ] Implement Python extractor with queries for functions, classes, decorators
- [ ] Implement TypeScript extractor with queries for all symbol types
- [ ] Handle method extraction (Parent.method format)
- [ ] Add signature extraction from node text
- [ ] Update `code/incremental_index` skill to use tree-sitter extractors
- [ ] Write comprehensive tests with edge cases
- [ ] Performance benchmark vs regex approach
- [ ] Consider lazy initialization of parsers (memory optimization)

### GDScript Considerations

Tree-sitter grammar for GDScript:
- Check if community grammar exists: `tree-sitter-gdscript`
- If not, options:
  1. Create custom grammar (significant effort)
  2. Use enhanced regex with indentation tracking
  3. Use Godot LSP for symbol extraction (already have `lsp/godot`)

### Build Considerations

Tree-sitter uses CGO for the core parser. Ensure:

```makefile
# Makefile update
build-cgo:
    CGO_ENABLED=1 go build -o bin/agentctl ./cmd/agentctl

skills-build-cgo:
    CGO_ENABLED=1 go build -o skills/code_incremental_index/code_incremental_index ./skills/code_incremental_index
```

---

## Phase 7: Background Embedding Queue (Future)

**Goal**: Async embedding for edited files without blocking hooks.

**Location**: `internal/intelligence/indexing/embedding/queue.go`

### Design

```
┌─────────────────────────────────────────────────────────────┐
│  live-index.sh (hook)                                       │
│  └── agentctl run code/incremental_index --embed-queue=true │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Embedding Queue (SQLite table: embedding_queue)            │
│  ├── file_path                                              │
│  ├── workspace_id                                           │
│  ├── queued_at                                              │
│  ├── status (pending, processing, done, failed)             │
│  └── attempts                                               │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Background Worker (agentctl embed-worker)                  │
│  ├── Polls queue every N seconds                            │
│  ├── Batches files for efficiency                           │
│  ├── Calls embedding provider                               │
│  └── Updates semantic index                                 │
└─────────────────────────────────────────────────────────────┘
```

### Tasks (Deferred)

- [ ] Create `embedding_queue` table schema
- [ ] Implement queue operations (enqueue, dequeue, mark done/failed)
- [ ] Create `agentctl embed-worker` command
- [ ] Implement batch processing with rate limiting
- [ ] Add retry logic with exponential backoff
- [ ] Wire into `code/incremental_index` skill

---

## Implementation Order

```
Phase 1 ──────────────────────────────────────────────────────────────
   │
   ├── internal/intelligence/retrieval/options.go
   ├── internal/intelligence/retrieval/candidates.go
   ├── internal/intelligence/retrieval/merge.go
   └── internal/intelligence/retrieval/candidates_test.go
   │
Phase 2 ──────────────────────────────────────────────────────────────
   │                                    (depends on Phase 1)
   ├── skills/code_incremental_index/skill.yaml
   ├── skills/code_incremental_index/main.go
   └── skills/code_incremental_index/main_test.go
   │
Phase 3 ──────────────────────────────────────────────────────────────
   │                                    (depends on Phase 2)
   ├── .claude/hooks/live-index.sh
   └── .claude/settings.json (update)
   │
Phase 4 ──────────────────────────────────────────────────────────────
   │                                    (depends on Phase 1)
   ├── skills/code_universal_swe_grep/skill.yaml
   ├── skills/code_universal_swe_grep/main.go
   └── skills/code_universal_swe_grep/main_test.go
   │
Phase 5 ──────────────────────────────────────────────────────────────
   │                                    (depends on Phase 1)
   └── internal/agent/tools/code_tools.go (update)
   │
Phase 6 ──────────────────────────────────────────────────────────────
   │                                    (enhances Phase 2)
   ├── go get tree-sitter dependencies
   ├── internal/intelligence/indexing/symbol/extractor_treesitter.go
   ├── internal/intelligence/indexing/symbol/extractor_python.go
   ├── internal/intelligence/indexing/symbol/extractor_typescript.go
   └── skills/code_incremental_index/main.go (update)
   │
Phase 7 ──────────────────────────────────────────────────────────────
                                        (future, independent)
```

---

## Testing Strategy

### Unit Tests

- `internal/intelligence/retrieval/` - Mock memory store, test scoring/merging
- `code/incremental_index` - Test with fixture Go/Python/TS files
- `code/smart_search` - Mock retrieval, test flow

### Integration Tests

- End-to-end: Edit file → hook triggers → symbols indexed → query returns them
- `test/integration/live_index_test.go`
- `test/integration/universal_swe_grep_test.go`

### Manual Testing

```bash
# Test live index
echo 'package foo\nfunc Bar() {}' > /tmp/test.go
agentctl run code/incremental_index --input '{"file":"/tmp/test.go","symbols":true}'

# Test universal swe grep
agentctl run code/smart_search --input '{"question":"How does Bar work?"}'

# Test tool via agent
# (requires agent runtime)
```

---

## Success Criteria

1. **Live Index**: Editing a Go file indexes symbols within 100ms
2. **Symbol Search**: `code.symbol_search` returns newly indexed symbols
3. **Universal Skill**: `code/smart_search` works standalone via CLI
4. **Tool Enhancement**: `code.swe_grep` with `auto_candidates=true` works for agents
5. **Hook Integration**: Claude Code edits trigger live indexing automatically

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Hook timeout (>5s) | Index skipped | Symbol-only mode is ~50ms |
| Large files slow to parse | Poor UX | Skip files >100KB in hook |
| No embeddings available | Degraded search | Graceful fallback to BM25 |
| Memory store not initialized | Skill fails | Clear error message, skip gracefully |
