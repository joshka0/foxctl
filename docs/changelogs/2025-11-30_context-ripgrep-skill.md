# code/context_ripgrep Skill

**Date:** 2025-11-30\
**Branch:** `codex/context-ripgrep-skill`

## Summary

Added a new skill `code/context_ripgrep` that combines ripgrep pattern searches
with heuristic block expansion to return full code units (functions, methods,
classes) instead of just matching lines.

## Motivation

Traditional grep returns line-level matches, which loses context when reasoning
about code. This skill:

- Returns **whole functions/methods/classes** containing matches
- Uses **language-specific heuristics** instead of full AST parsing
- Provides **structured output** with symbol names, kinds, and line ranges
- Deduplicates overlapping matches into single blocks

This is particularly useful for:

- Agent workflows that need to reason about full code units
- Refactoring tools that need to understand function boundaries
- LLM-based code analysis that benefits from complete context

## Implementation

### Algorithm: "Ripgrep + Expand"

1. Run `rg --json` to find pattern matches
2. Group matches by file
3. For each match:
   - Detect language from file extension
   - Walk upward to find block start (function/class header)
   - Walk downward to find block end (brace match or dedent)
4. Deduplicate overlapping blocks
5. Return structured results with full source

### Language Support

| Language   | Detection                     | Header Pattern                  | End Detection  |
| ---------- | ----------------------------- | ------------------------------- | -------------- |
| Go         | `.go`                         | `func`, `type`                  | Brace matching |
| Python     | `.py`, `.pyw`, `.pyi`         | `def`, `async def`, `class`     | Indentation    |
| JavaScript | `.js`, `.jsx`, `.mjs`, `.cjs` | `function`, `const =`, `class`  | Brace matching |
| TypeScript | `.ts`, `.tsx`, `.mts`, `.cts` | `function`, `interface`, `type` | Brace matching |
| GDScript   | `.gd`                         | `func`, `class`, `class_name`   | Indentation    |
| Generic    | (fallback)                    | First non-empty line            | Blank lines    |

### Input Schema

```yaml
pattern: string (required) - Regex pattern for ripgrep
path: string - Directory or file to search (default: workspace)
    case_insensitive: boolean - Case insensitive search
    glob: array<string> - Include glob patterns
    glob_not: array<string> - Exclude glob patterns
    max_matches: integer - Max ripgrep matches (default: 10000)
        max_blocks: integer - Max code blocks to return (default: 50)
            max_block_lines: integer - Max lines per block (default: 400)
                hidden: boolean - Include hidden files
```

### Output Schema

Inline `data`:

- `pattern`, `case_insensitive`
- `match_count` (ripgrep hits)
- `block_count` (expanded blocks)
- `files_touched`
- `preview`: array of
  `{file, language, start_line, end_line, header_line, symbol_name, symbol_kind, match_count}`
- `top_files`: sorted by match count
- `artifact`: CAS digest (when applicable)

CAS artifact (NDJSON):

- Full `Block` objects with `source` field containing complete block text

## Files Changed

- `skills/code_context_ripgrep/main.go` - Skill entry point, ripgrep
  orchestration
- `skills/code_context_ripgrep/expander.go` - Language detection and block
  expansion heuristics
- `skills/code_context_ripgrep/expander_test.go` - Tests for all supported
  languages
- `skills/code_context_ripgrep/main_test.go` - Input parsing and helper function
  tests
- `skills/code_context_ripgrep/skill.yaml` - Skill manifest

## Example Usage

```bash
# Find all functions using PathValidator in Go files
./agentctl run code/context_ripgrep --input '{
  "pattern": "PathValidator",
  "glob": ["*.go"],
  "max_blocks": 20
}'

# Find error handling in Python
./agentctl run code/context_ripgrep --input '{
  "pattern": "except.*Exception",
  "glob": ["*.py"],
  "case_insensitive": true
}'

# Find GDScript signal emissions
./agentctl run code/context_ripgrep --input '{
  "pattern": "\\.emit\\(",
  "glob": ["*.gd"]
}'
```

## Testing

Comprehensive test coverage for:

- Language detection from file extensions
- Block expansion for Go (func, method, type)
- Block expansion for Python (def, async def, class)
- Block expansion for JS/TS (function, arrow, class, interface, type)
- Block expansion for GDScript (func, class, inner class)
- Generic fallback (blank-line blocks)
- Match deduplication within blocks
- Multi-function files
- Max block lines enforcement
- Input parsing and validation
- Ripgrep argument building

## Future Improvements

- AST-backed expansion for Go (optional precision mode)
- Integration with `internal/intelligence/indexing/symbol` for index-backed lookups
- Support for additional languages (Rust, C/C++, Java)
- Method-level extraction within classes
