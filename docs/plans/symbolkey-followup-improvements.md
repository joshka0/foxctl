# Implementation Plan: SymbolKey Follow-Up Improvements

> Follow-up fixes from the Stable Symbol IDs feature (`feature/stable-symbol-ids`).
> All existing SymbolKey work is already merged. This plan covers four specific follow-up items only.

## Problem Statement

1. **Package-scoped keys in memory entry names** (CRITICAL) — Key-based entry names (`symbol://<ws>/key:<symbolKey>`) collide across packages when two packages have identically-named symbols (e.g., `Helper` in `pkg/a` and `pkg/b`).
2. **Go non-exported symbol collision** (MEDIUM) — `GoSymbolKey(name)` produces identical keys for non-exported functions with the same name in different files within a package (e.g., `helper` in `store.go` and `builder.go`).
3. **Incremental index skill dual-write alignment** (HIGH) — The incremental skill writes only legacy format entries while the main indexer does dual-write. Needs alignment to key-only writes.
4. **Missing Python key assignment** (LOW) — `setSymbolKeys()` handles Go and TS but not Python.

## Architecture Decision

- **Package in entry name, NOT in SymbolKey**: `SymbolKey` remains a symbol-local identity (language-level semantics). Package disambiguation is a storage-layer concern handled in entry names only. This preserves stable key semantics while avoiding cross-package collisions.
- **Entry name format**: `symbol://<workspace>/<pkg>::<symbolKey>` using `::` as the unambiguous delimiter between pkg and symbolKey (neither contains `::`)
- **Shared package derivation**: A single `DeriveSymbolPackage(filePath, lang)` function used by ALL components to ensure consistent pkg values across the indexer, incremental skill, CLI, and builder summary lookups.
- **Clean re-index**: Bump `CurrentFileMetaSchema` to 3. Remove all legacy dual-write. Key-only persistence going forward.
- **Schema versions**: `CurrentFileMetaSchema`: 2→3, `repoindex schemaVersion`: stays 3, embedding digest: stays v2

---

## Step 1: Shared Package Derivation and Keyed Entry-Name Contracts

### `internal/platform/symbolutil/symbolutil.go` (modified)

Add shared package derivation and update entry name signatures:

```go
// DeriveSymbolPackage produces a deterministic package identifier from file path and language.
// Used by ALL components for consistent key entry names.
func DeriveSymbolPackage(filePath, lang string) string {
    dir := filepath.ToSlash(filepath.Dir(filePath))
    if dir == "." || dir == "" {
        dir = "root"
    }
    switch lang {
    case "go":
        return "go:" + dir
    case "typescript", "javascript":
        return "ts:local:" + dir
    case "python":
        return "py:" + dir
    case "elixir":
        return "ex:" + dir
    default:
        return "file:" + dir
    }
}

// KeyEntryName builds a package-scoped key entry name.
// Format: symbol://<workspace>/<pkg>::<symbolKey>
func KeyEntryName(workspace, pkg, symbolKey string) string
```

### `internal/intelligence/indexing/symbol/types.go` (modified)

- Bump `CurrentFileMetaSchema = 3`
- Update `SymbolSummaryKeyEntryName(workspace, pkg, symbolKey string) string` — format: `symbol-summary://<workspace>/<pkg>::<symbolKey>`
- Add `Pkg string` to `SymbolSummaryInput` and `SymbolSummaryResult`

### `internal/intelligence/retrieval/file_summary.go` (modified)

- Update `symbolSummaryEntryName` to use `symbol.SymbolSummaryKeyEntryName(workspace, input.Pkg, input.SymbolKey)` when both present
- Fallback to legacy name only for compatibility reads

---

## Step 2: Go Non-Exported Constructors, Python Keys, and Export Detection

### `internal/intelligence/indexing/symbol/symbolkey.go` (modified)

```go
// GoNonExportedSymbolKey creates a key for Go non-exported symbols.
// Format: <fileBasename>/<name> (matches TS non-exported pattern)
func GoNonExportedSymbolKey(name, fileBasename string) SymbolKey

// PythonSymbolKey creates a key for Python symbols.
// Uses qualified name (class.method) similar to Go.
func PythonSymbolKey(name string) SymbolKey
```

### `internal/intelligence/indexing/repoindex/builder.go` (modified)

- Update `goSymbolKeyFromName(name, fileRelPath string)`:
  - `init` → `GoInitSymbolKey(filepath.Base(fileRelPath))`
  - non-exported (Unicode `unicode.IsUpper` check) → `GoNonExportedSymbolKey(name, filepath.Base(fileRelPath))`
  - exported → `GoSymbolKey(name)`

---

## Step 3: Key-Only Writes in Main Indexer and Incremental Skill

### `internal/intelligence/indexing/symbol/indexer.go` (modified)

**Key assignment** — In `indexFile`, compute `pkg := symbolutil.DeriveSymbolPackage(file.Path, lang)` and update key assignment:
- Go: `init` → `GoInitSymbolKey`, exported → `GoSymbolKey`, non-exported → `GoNonExportedSymbolKey`
- Python: `sym.Key = PythonSymbolKey(sym.Name)`
- TS/JS and Elixir: unchanged

**Save path** — `saveSymbol` writes ONLY key-based entry:
```go
name := symbolutil.KeyEntryName(event.WorkspaceID, pkg, sym.EffectiveID())
```

**Delete path** — `deleteSymbol` accepts `pkg` parameter, deletes only key-based entry name. Keep best-effort legacy cleanup as non-fatal fallback.

**File meta** — `updateFileMetaFull` writes `IndexSchema: CurrentFileMetaSchema` (now 3).

### `skills/code_incremental_index/main.go` (modified)

- Import `symbolutil`, call `DeriveSymbolPackage(filePath, lang)` for consistent pkg
- `setSymbolKeys`: Add Go exported/non-exported logic with Unicode check, add Python case
- `upsertSymbols`: Write key-only entries via `symbolutil.KeyEntryName(workspaceID, pkg, sym.EffectiveID())`
- Remove legacy `symbol.EntryName` writes
- Stale cleanup: use pkg-scoped key names for file-scope deletion

---

## Step 4: Downstream Consumers Thread Package Through Lookups

### `internal/intelligence/indexing/repoindex/types.go` (modified)

- `SymbolSummaryProvider.Summary(ctx, symbolID, symbolKey, pkg string) (string, error)`

### `internal/intelligence/indexing/repoindex/builder.go` (modified)

- `applySymbolSummary` passes `pkg := symbolutil.DeriveSymbolPackage(sym.FilePath, lang)` — NOT repoindex `pkgID`
- Repoindex node IDs continue using `pkgID` (unchanged)

### `cmd/agentctl/cmd/index.go` (modified)

- `buildSymbolSummaryInput` sets `Pkg: symbolutil.DeriveSymbolPackage(sym.FilePath, sym.Language)`
- Cache checks use pkg-scoped key names

### `cmd/agentctl/cmd/index_repo.go` (modified)

- `memorySymbolSummaryProvider.Summary(ctx, symbolID, symbolKey, pkg)` derives pkg from file path, NOT repoindex `pkgID`

### `internal/intelligence/retrieval/semantic_search.go` (modified)

- `extractFilePath` treats `<pkg>::<symbolKey>` format as no-file-path → payload fallback
- BM25 fallback dedup unchanged

### `skills/code_semantic_search/main.go` (modified)

- `extractSymbolName` parses `<pkg>::<symbolKey>` format, returns key display segment

### Cross-component validation

These four call sites MUST all use `symbolutil.DeriveSymbolPackage`:
1. `internal/intelligence/indexing/symbol/indexer.go`
2. `skills/code_incremental_index/main.go`
3. `cmd/agentctl/cmd/index.go`
4. `cmd/agentctl/cmd/index_repo.go`

---

## Testing Strategy

### Unit Tests
- `internal/platform/symbolutil`: `DeriveSymbolPackage` output matrix for go/ts/python/elixir, `KeyEntryName` format with `::` separator
- `internal/intelligence/indexing/symbol/symbolkey_test.go`: `GoNonExportedSymbolKey`, `PythonSymbolKey`, Unicode export detection
- `internal/intelligence/indexing/symbol/indexer_test.go`: key-only writes, stale deletion with pkg, `IndexSchema=3` behavior
- `internal/intelligence/retrieval/semantic_search_test.go`: `<pkg>::<key>` parsing and payload fallback
- `skills/code_semantic_search`: `extractSymbolName` for new format

### Integration Tests
- `internal/intelligence/indexing/repoindex/builder_test.go`: summary lookup uses shared pkg derivation
- `skills/code_incremental_index`: `setSymbolKeys` handles Go exported/non-exported + Python
- `cmd/agentctl/cmd`: summary cache uses pkg-scoped names

### Edge Cases
- Two non-exported Go symbols with same name in different files (must get different keys)
- `init` functions in multiple files (already handled by `init@filename.go`)
- Python class methods vs top-level functions
- Empty package derivation (root-level files)
- SymbolKey containing special characters

---

## Error Handling

- `DeriveSymbolPackage` never returns empty — uses `"file:root"` fallback
- Legacy cleanup deletions are non-fatal and logged as best-effort
- `SymbolSummaryInput.Pkg` may be empty in pre-migration data — summary cache falls back to legacy key format
- Invalid key strings in parsers are ignored, never fail request processing
- Unicode decoding for empty/blank Go symbol names is guarded

---

## Migration Notes

1. Bump `CurrentFileMetaSchema` to 3 → forces full re-index of all files
2. Embedding digest stays v2 → non-exported Go symbols get new digests naturally (different key = different digest)
3. Run `agentctl index init --scope symbols` after deployment to re-embed
4. Old legacy entries coexist until garbage collected; no migration needed

---

## Files Changed

| File | Status | Step |
|------|--------|------|
| `internal/platform/symbolutil/symbolutil.go` | MODIFY | 1 |
| `internal/intelligence/indexing/symbol/types.go` | MODIFY | 1 |
| `internal/intelligence/indexing/symbol/symbolkey.go` | MODIFY | 2 |
| `internal/intelligence/indexing/repoindex/builder.go` | MODIFY | 2, 4 |
| `internal/intelligence/indexing/symbol/indexer.go` | MODIFY | 3 |
| `skills/code_incremental_index/main.go` | MODIFY | 3 |
| `internal/intelligence/retrieval/file_summary.go` | MODIFY | 1, 4 |
| `internal/intelligence/indexing/repoindex/types.go` | MODIFY | 4 |
| `internal/intelligence/retrieval/semantic_search.go` | MODIFY | 4 |
| `cmd/agentctl/cmd/index.go` | MODIFY | 4 |
| `cmd/agentctl/cmd/index_repo.go` | MODIFY | 4 |
| `skills/code_semantic_search/main.go` | MODIFY | 4 |
| Test files (various) | NEW/MODIFY | all |

## Implementation Order

1. **Shared contracts** (Step 1): `symbolutil.DeriveSymbolPackage`, `KeyEntryName(ws, pkg, key)`, schema bump, summary input/result fields
2. **Key constructors** (Step 2): `GoNonExportedSymbolKey`, `PythonSymbolKey`, Unicode export detection, builder updates
3. **Writers** (Step 3): Main indexer + incremental skill — key-only writes with pkg, remove legacy dual-write
4. **Readers** (Step 4): Summary provider, CLI commands, semantic search parsing, `code_semantic_search` display
