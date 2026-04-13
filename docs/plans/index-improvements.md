# Implementation Plan: Stable Symbol IDs Across the Whole System

## Problem Statement

Symbol IDs currently encode `filePath:symbolName` (e.g., `builder.go:Builder.Build`). This file path is baked into identity at **every layer** — repoindex nodes, embeddings, summaries, and named memory entries. When a file moves:

- All symbol node IDs in the repoindex graph change
- All embeddings for those symbols are invalidated (expensive re-computation)
- Named memory entries (symbol summaries, call edges) become orphaned
- Links stored in trails/tasks/notes break

**Goal**: Separate "identity" (what a symbol *is*) from "locator" (where it *lives*), making symbol IDs stable across file moves. This enables durable links, better caching, and a foundation for the semantic address space.

**Design choices** (locked):

- Rename = new identity (SymbolKey includes qualified name; rename creates new key)
- Go + TS together in one stage
- Locator table in existing repoindex DB
- Full reindex required (clean slate, no alias layer)

---

## Architecture Decision

**Approach**: Introduce a `SymbolKey` type that encodes only the qualified symbol name (no file path). The `pkgID` already provides language+package context (`go:importPath`, `ts:local:moduleRoot`), so SymbolKey only needs symbol-level disambiguation.

**Key invariants**:

1. `sym.EffectiveID()` is the canonical identity: prefers `sym.Key` (stable), falls back to `sym.ID` (legacy path-based)
2. Repoindex node IDs use `EffectiveID` as suffix: `<repoKey>::sym:<pkgID>:<symbolKey>`
3. A new `symbol_locator` table maps SymbolKey → current file/span/hash
4. Memory entries use `symbol://<ws>/key:<symbolKey>` format (with legacy dual-write during migration)
5. Embedding digest version bumps to `v2` (includes `symbol_key` instead of `filePath`), forcing re-embed

**Why not alternatives**:

- Content-hash-based identity (rename = same identity) was rejected as too complex and fragile
- Alias/fallback layer was rejected in favor of clean-slate reindex for simplicity
- Separate durable locator DB was rejected since co-location with repoindex is simpler and they rebuild together

---

## SymbolKey Format

| Language | Format | Example |
|----------|--------|---------|
| Go | `<qualifiedName>` | `Builder.addGoReferenceEdges` |
| Go (init) | `init@<filename>` | `init@store.go` |
| TS (exported) | `<name>` | `ConversationsList` |
| TS (non-exported) | `<fileBasename>/<name>` | `utils.tsx/helperFunc` |
| Elixir | `<qualifiedName>` | `MyApp.Server.handle_call` |

Full node ID transformation:

- **Before**: `rk::sym:go:github.com/.../repoindex:builder.go:Builder.Build`
- **After**: `rk::sym:go:github.com/.../repoindex:Builder.Build`

---

## File Changes

### `internal/intelligence/indexing/symbol/symbolkey.go` (new)

SymbolKey type, constructors for each language, and helpers.

```go
package symbol

import (
    "fmt"
    "strings"
)

type SymbolKey string

func (k SymbolKey) String() string { return strings.TrimSpace(string(k)) }

func (k SymbolKey) Name() string {
    v := k.String()
    if idx := strings.LastIndex(v, "/"); idx != -1 {
        return v[idx+1:]
    }
    return v
}

func GoSymbolKey(name string) SymbolKey {
    return SymbolKey(strings.TrimSpace(name))
}

func GoInitSymbolKey(filename string) SymbolKey {
    return SymbolKey(fmt.Sprintf("init@%s", strings.TrimSpace(filename)))
}

func TSSymbolKey(name string, exported bool, fileBasename string) SymbolKey {
    name = strings.TrimSpace(name)
    if name == "" { return "" }
    if exported { return SymbolKey(name) }
    fileBasename = strings.TrimSpace(fileBasename)
    if fileBasename == "" { return SymbolKey(name) }
    return SymbolKey(fmt.Sprintf("%s/%s", fileBasename, name))
}

func ElixirSymbolKey(name string) SymbolKey {
    return SymbolKey(strings.TrimSpace(name))
}
```

### `internal/intelligence/indexing/symbol/types.go` (modified)

**Add to Symbol struct:**
```go
type Symbol struct {
    ID  string    `json:"id"`
    Key SymbolKey `json:"key,omitempty"`  // NEW: stable, file-path-independent identity
    // ... rest unchanged
}

func (s Symbol) EffectiveID() string {
    if id := s.Key.String(); id != "" { return id }
    return strings.TrimSpace(s.ID)
}
```

**Add FileMeta schema guard:**
```go
const CurrentFileMetaSchema = 2

type FileMeta struct {
    FilePath      string            `json:"file_path"`
    ContentHash   string            `json:"content_hash"`
    LastModTime   int64             `json:"last_mod_time"`
    IndexSchema   int               `json:"index_schema"`     // NEW: gates migration skip
    Count         int               `json:"symbol_count"`
    SymbolDigests map[string]string `json:"symbol_digests,omitempty"`
}
```

**Add keyed summary entry name:**
```go
func SymbolSummaryKeyEntryName(workspace, symbolKey string) string {
    return fmt.Sprintf("symbol-summary://%s/key:%s", workspace, symbolKey)
}
```

**Extend summary input/result structs:**
```go
type SymbolSummaryInput struct {
    SymbolID  string `json:"symbol_id"`
    SymbolKey string `json:"symbol_key,omitempty"`  // NEW
    // ... rest unchanged
}

type SymbolSummaryResult struct {
    SymbolID  string `json:"symbol_id"`
    SymbolKey string `json:"symbol_key,omitempty"`  // NEW
    // ... rest unchanged
}
```

### `internal/platform/symbolutil/symbolutil.go` (modified)

**Add keyed entry name helper (keep existing `EntryName` for legacy compat):**
```go
func KeyEntryName(workspace, symbolKey string) string {
    return fmt.Sprintf("symbol://%s/key:%s", workspace, symbolKey)
}
```

### `internal/intelligence/indexing/repoindex/types.go` (modified)

**Add LocatorEntry type:**
```go
type LocatorEntry struct {
    SymbolKey string `json:"symbol_key"`
    Pkg       string `json:"pkg"`
    FilePath  string `json:"file_path"`
    Name      string `json:"name"`
    Kind      string `json:"kind"`
    Exported  bool   `json:"exported"`
    SpanStart int    `json:"span_start"`
    SpanEnd   int    `json:"span_end"`
    BodyHash  string `json:"body_hash"`
    UpdatedAt string `json:"updated_at"`
}
```

**Update SymbolSummaryProvider interface to accept full input:**
```go
type SymbolSummaryProvider interface {
    Summary(ctx context.Context, input symbol.SymbolSummaryInput) (string, error)
}
```

### `internal/intelligence/indexing/repoindex/store.go` (modified)

**Bump schema version** (triggers auto-reset via existing `resetSchema` at line 484):
```go
const schemaVersion = 3
```

**Add to `migrate()` DDL:**
```sql
CREATE TABLE IF NOT EXISTS symbol_locator (
    symbol_key TEXT NOT NULL,
    pkg TEXT NOT NULL,
    file_path TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT,
    exported INTEGER NOT NULL DEFAULT 0,
    span_start INTEGER,
    span_end INTEGER,
    body_hash TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (symbol_key, pkg)
);
CREATE INDEX IF NOT EXISTS idx_locator_file ON symbol_locator(file_path);
```

**Add to `resetSchema()`:**
```sql
DROP TABLE IF EXISTS symbol_locator;
```

**Add locator CRUD methods:**
- `UpsertLocator(ctx, loc LocatorEntry) error` — INSERT ON CONFLICT UPDATE
- `LookupLocator(ctx, symbolKey, pkg string) (*LocatorEntry, error)`
- `LookupLocatorsByFile(ctx, filePath string) ([]LocatorEntry, error)`

### `internal/intelligence/indexing/repoindex/builder.go` (modified)

**1. `Build()` orchestrates locator batch-write:**
```go
locators := make([]LocatorEntry, 0)
// pass &locators to buildGo/buildTS/buildElixir
// after ReplaceAll succeeds:
for _, loc := range locators {
    if err := b.store.UpsertLocator(ctx, loc); err != nil { return result, err }
}
```

**2. `addSymbol()` signature gains locator sink:**
```go
func addSymbol(ctx context.Context, opts BuildOptions, nodes map[string]Node,
    edges map[string]Edge, pkgID, fileID string, sym symbol.Symbol,
    locators *[]LocatorEntry) {
    // ...
    nodeID := SymbolID(opts.RepoKey, pkgID, sym.EffectiveID())
    // ...
    applySymbolSummary(ctx, opts, &node, sym.ID, sym.EffectiveID())
    // ...
    if locators != nil {
        *locators = append(*locators, LocatorEntry{
            SymbolKey: sym.EffectiveID(), Pkg: pkgID,
            FilePath: sym.FilePath, Name: sym.Name,
            Kind: string(sym.Kind), Exported: isExportedSymbol(sym),
            SpanStart: spanStart, SpanEnd: spanEnd,
            BodyHash: sym.BodyDigest,
            UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
        })
    }
}
```

**Callers to update** (all in `builder.go`, package-private):
- `buildGo` — set `sym.Key = GoSymbolKey(sym.Name)` (or `GoInitSymbolKey` for init)
- `buildTS` — set `sym.Key = TSSymbolKey(sym.Name, isExportedSymbol(sym), filepath.Base(fileRelPath))`
- `buildElixir` — set `sym.Key = ElixirSymbolKey(sym.Name)`

**3. `addGoReferenceEdges()` (line ~540) and `goObjectNodeID()` (line ~665):**
```go
// Before:
srcID := SymbolID(opts.RepoKey, pkgID, symbol.ID(fileRelPath, name))
// After:
srcID := SymbolID(opts.RepoKey, pkgID, goSymbolKeyFromName(name, fileRelPath).String())
```

**4. New helper (standardized signature):**
```go
func goSymbolKeyFromName(name, fileRelPath string) symbol.SymbolKey {
    if strings.TrimSpace(name) == "init" {
        return symbol.GoInitSymbolKey(filepath.Base(fileRelPath))
    }
    return symbol.GoSymbolKey(name)
}
```

**5. `applySymbolSummary` updated to pass both ID flavors:**
```go
func applySymbolSummary(ctx context.Context, opts BuildOptions, node *Node, symbolID, symbolKey string) {
    input := symbol.SymbolSummaryInput{
        SymbolID:  symbolID,
        SymbolKey: symbolKey,
        Name:      node.Name,
        FilePath:  node.File,
        Signature: node.Signature,
    }
    summary, err := opts.SymbolSummaryProvider.Summary(ctx, input)
    // ...
}
```

**6. `pendingNameEdge` resolution** (for TS/Elixir call edges):
Build `nameToKeys[pkg][name]` index during symbol ingestion. Resolver uses `SymbolID(repoKey, pkgID, dstKey)` where `dstKey` comes from this index.

### `internal/intelligence/indexing/symbol/indexer.go` (modified)

**1. `indexFile()` map keys use `EffectiveID()`:**
```go
// File-skip now checks schema:
if !idx.config.Force && oldMeta != nil &&
    oldMeta.IndexSchema == symbol.CurrentFileMetaSchema &&
    oldMeta.ContentHash == fileDigest {
    return ErrUnchanged
}

// Symbol loop:
symID := sym.EffectiveID()
newDigests[symID] = skipDigest
newSymbolIDs[symID] = true
nameToID[sym.Name] = symID
if oldDigest, ok := oldDigests[symID]; ok && oldDigest == skipDigest { /* skip */ }
```

**2. `saveSymbol()` — key-first with schema-gated legacy dual-write:**
```go
func (idx *Indexer) saveSymbol(ctx, event, sym, calls, writeLegacy bool) error {
    primary := symbolutil.KeyEntryName(event.WorkspaceID, sym.EffectiveID())
    legacy := symbolutil.EntryName(event.WorkspaceID, sym.FilePath, sym.Name)
    writeNames := []string{primary}
    if writeLegacy && primary != legacy {
        writeNames = append(writeNames, legacy)
    }
    for _, name := range writeNames { /* save entry */ }
}
```

Legacy write gate: `writeLegacy = oldMeta != nil && oldMeta.IndexSchema < symbol.CurrentFileMetaSchema`

**3. `deleteSymbol()` — handles both keyed and legacy entries:**
```go
func (idx *Indexer) deleteSymbol(ctx, workspace, filePath, symbolID string) error {
    var errs []error
    errs = append(errs, idx.deleteIfPresent(ctx, symbolutil.KeyEntryName(workspace, symbolID), workspace))
    if legacyFile, legacyName, ok := splitLegacySymbolID(symbolID); ok {
        errs = append(errs, idx.deleteIfPresent(ctx, symbolutil.EntryName(workspace, legacyFile, legacyName), workspace))
    }
    errs = append(errs, idx.deleteOutgoingCallEdges(ctx, workspace, symbolID))
    return errors.Join(errs...)
}
```

**4. `deleteFileSymbols()` — iterates stored FileMeta digests:**
```go
if oldMeta := idx.loadFileMeta(ctx, workspace, filePath); oldMeta != nil {
    for oldID := range oldMeta.SymbolDigests {
        _ = idx.deleteSymbol(ctx, workspace, filePath, oldID)
    }
}
// Keep prefix-delete as best-effort legacy cleanup
symbolPrefix := fmt.Sprintf("symbol://%s/%s:", workspace, filePath)
_, _ = idx.memoryStore.DeleteByNamePrefix(ctx, workspace, symbolPrefix)
```

**5. `updateFileMetaFull()` stores schema marker:**
```go
meta := FileMeta{
    FilePath:      filePath,
    ContentHash:   digest,
    IndexSchema:   symbol.CurrentFileMetaSchema,
    Count:         symbolCount,
    SymbolDigests: symbolDigests,
}
```

**6. `buildEmbeddingPayload()` uses keyed identity:**
```go
embedding.SymbolInput{
    SymbolID:      sym.EffectiveID(),  // was sym.ID
    // ...
}
```

### `internal/intelligence/indexing/embeddingtext/digest.go` (modified)

**Add `SymbolKey` field, bump digest version:**
```go
type SymbolDigestInput struct {
    Model      string
    Kind       string
    Name       string
    SymbolKey  string   // NEW: replaces FilePath in digest
    FilePath   string   // kept for fallback if SymbolKey empty
    Signature  string
    Doc        string
    BodyDigest string
    Calls      []string
}
```

```go
// Before:
builder.WriteString("v1\n")
// ...
builder.WriteString("\nfile:")
builder.WriteString(filePath)

// After:
builder.WriteString("v2\n")
// ...
symbolKey := strings.TrimSpace(input.SymbolKey)
if symbolKey == "" { symbolKey = strings.TrimSpace(input.FilePath) }
builder.WriteString("\nkey:")
builder.WriteString(symbolKey)
```

### `internal/intelligence/indexing/embedding/store.go` (modified)

No schema change needed — `symbol_id TEXT` column now stores SymbolKey strings. `file_path` column continues to be populated for file-based cleanup queries. `dedupeKeyForSymbol()` works as-is since it hashes `symbolID` opaquely — add defensive trim/normalize.

### `internal/intelligence/retrieval/semantic_search.go` (modified)

**`extractFilePath()` recognizes `key:` prefix:**
```go
if strings.HasPrefix(rest, "key:") {
    return ""  // caller falls back to result payload
}
```

**Callers (`memoryResultsToCandidates`, `semanticBM25Fallback`) add payload fallback:**
```go
filePath := extractFilePath(r.Entry.Name)
if filePath == "" {
    filePath = extractFilePathFromEntryPayload(r.Entry.Result)
}
if filePath == "" { continue }
```

**New helper:**
```go
func extractFilePathFromEntryPayload(raw json.RawMessage) string {
    var payload struct {
        Symbol struct { FilePath string `json:"file_path"` } `json:"symbol"`
    }
    if err := json.Unmarshal(raw, &payload); err != nil { return "" }
    return strings.TrimSpace(payload.Symbol.FilePath)
}
```

### `internal/intelligence/retrieval/file_summary.go` (modified)

**Key-aware summary entry name selector:**
```go
func symbolSummaryEntryName(workspace string, input symbol.SymbolSummaryInput) string {
    if strings.TrimSpace(input.SymbolKey) != "" {
        return symbol.SymbolSummaryKeyEntryName(workspace, input.SymbolKey)
    }
    return symbol.SymbolSummaryEntryName(workspace, input.SymbolID)
}
```

Applied in both `GetOrCreateSummary()` and `storeSummary()`.

### `cmd/agentctl/cmd/index.go` (modified)

`symbol-summaries` command (~line 2327): use `sym.Key` for entry name when available. Prefer `SymbolSummaryKeyEntryName` when symbol key exists.

### `cmd/agentctl/cmd/index_repo.go` (modified)

Symbol summary lookups (~line 683): use key-based entry name via the same key-aware selector.

### `skills/code_incremental_index/main.go` (modified)

Compute SymbolKey for each symbol. For Go: derive import path from `go.mod` + relative dir. For TS: use existing module root detection. Pass `sym.EffectiveID()` as stable identifier downstream.

### `skills/code_semantic_search/main.go` (modified)

Update `extractSymbolName()` to handle SymbolKey format from `key:` entry names. Use `SymbolKey.Name()` for display.

---

## Testing Strategy

### Unit tests

**`internal/intelligence/indexing/symbol/symbolkey_test.go`** (new):
- `GoSymbolKey` produces correct keys for functions, methods, types
- `GoInitSymbolKey` handles `init` with filename
- `TSSymbolKey` with `exported=true` produces name-only key
- `TSSymbolKey` with `exported=false` produces `basename/name` key
- `TSSymbolKey` with empty basename falls back to name-only
- `ElixirSymbolKey` produces correct keys
- `SymbolKey.Name()` extracts human-readable name from all formats
- Go test functions (`TestFoo`) work correctly (no special handling needed)

**`internal/intelligence/indexing/repoindex/store_test.go`** (modified):
- UpsertLocator: insert new, upsert same (symbol_key, pkg) updates fields
- LookupLocator: by key+pkg
- LookupLocatorsByFile: returns all locators for a file path

**`internal/intelligence/indexing/embeddingtext/digest_test.go`** (modified):
- Assert version prefix is `v2`
- Assert digest uses `symbol_key` field
- Assert file move with same symbol key produces same digest

### Integration tests

1. Build index on fixture repo → verify symbol node IDs don't contain file path segments
2. Move a file, rebuild → verify same SymbolKey maps to new file location via locator
3. Full semantic search round-trip → verify keyed entries resolve to correct file paths

### Existing tests needing format updates

- `internal/intelligence/indexing/repoindex/builder_test.go` — expected node IDs drop file path segment
- `internal/intelligence/retrieval/candidates_test.go` — expected entry names include `key:` format
- `internal/intelligence/indexing/embeddingtext/digest_test.go` — v1→v2 assertions

---

## Edge Cases

| Case | Handling |
|------|----------|
| Go `init()` (multiple per package) | `init@filename.go` as key |
| TS same name, different files, both exported | First indexed wins; second gets warning (already a TS module conflict) |
| TS same basename in different dirs, non-exported | Basename collision accepted (rare); get new keys on reindex |
| Symbol rename | New identity. Old key orphaned, cleaned up on reindex |
| File move | SymbolKey stays the same. Locator updates. Embeddings/summaries preserved |
| File rename | Go: key unchanged (pkg+name stable). TS non-exported: new key (basename changed) |
| Go test functions | Package-scoped, unique by Go rules. `GoSymbolKey(sym.Name)` works without special handling |

---

## Migration

1. Bump repoindex `schemaVersion` to `3` → auto-reset on next open
2. Bump embedding digest version to `v2` → all digests mismatch, full re-embed
3. `FileMeta.IndexSchema` guard prevents stale file-level skip on unchanged files
4. Dual-write (key + legacy entry names) is schema-gated per-file: stops automatically when a file is reindexed at current schema
5. Run: `agentctl index repo build` → `agentctl index init --scope symbols` → `agentctl index symbol-summaries`
6. Old named memory entries with legacy format coexist until GC'd or prefix-deleted

---

## Implementation Order

1. **SymbolKey type + Symbol.EffectiveID()** — `symbolkey.go`, `types.go`
2. **Keyed entry name helpers** — `symbolutil.go`, `types.go` (SymbolSummaryKeyEntryName)
3. **Repoindex schema + locator table** — `repoindex/types.go`, `repoindex/store.go`
4. **Builder pipeline** — `builder.go`: addSymbol signature, key assignment in buildGo/buildTS/buildElixir, goSymbolKeyFromName, pendingNameEdge resolution, locator collection, applySymbolSummary update
5. **Symbol indexer** — `indexer.go`: EffectiveID maps, schema-gated skip, saveSymbol dual-write, deleteSymbol dual-format, updateFileMetaFull schema marker, embedding payload
6. **Embedding digest** — `embeddingtext/digest.go`: v2 with symbol_key
7. **Retrieval layer** — `semantic_search.go` (extractFilePath + payload fallback), `file_summary.go` (key-aware entry selector)
8. **CLI + skills** — `index.go`, `index_repo.go`, `code_incremental_index`, `code_semantic_search`
9. **Tests** — new symbolkey tests, locator CRUD tests, update existing test assertions

Each step compiles and passes tests independently.

---

## Verification

```bash
# Build
make build

# Type check
go vet ./internal/intelligence/indexing/...

# Unit tests
go test ./internal/intelligence/indexing/symbol/... -run TestSymbolKey -v
go test ./internal/intelligence/indexing/repoindex/... -v
go test ./internal/intelligence/indexing/embeddingtext/... -v
go test ./internal/intelligence/retrieval/... -v

# Integration: rebuild index and verify stable IDs
agentctl index repo build --workspace . --go --typescript
agentctl index repo search --workspace . --query "Builder" --limit 5
# Verify node IDs no longer contain file paths

# Integration: verify embeddings use new keys
agentctl index init --workspace . --scope symbols
agentctl run code/semantic_search --input '{"query": "symbol extraction", "limit": 5}'
# Verify results still resolve to correct file paths

# Integration: verify locator
# Move a file, rebuild, confirm same SymbolKey maps to new location
```

---

## Files Summary

| File | Status | Step |
|------|--------|------|
| `internal/intelligence/indexing/symbol/symbolkey.go` | NEW | 1 |
| `internal/intelligence/indexing/symbol/symbolkey_test.go` | NEW | 9 |
| `internal/intelligence/indexing/symbol/types.go` | MODIFY | 1, 2 |
| `internal/platform/symbolutil/symbolutil.go` | MODIFY | 2 |
| `internal/intelligence/indexing/repoindex/types.go` | MODIFY | 3 |
| `internal/intelligence/indexing/repoindex/store.go` | MODIFY | 3 |
| `internal/intelligence/indexing/repoindex/builder.go` | MODIFY | 4 |
| `internal/intelligence/indexing/symbol/indexer.go` | MODIFY | 5 |
| `internal/intelligence/indexing/embeddingtext/digest.go` | MODIFY | 6 |
| `internal/intelligence/indexing/embedding/store.go` | MODIFY | 6 |
| `internal/intelligence/retrieval/semantic_search.go` | MODIFY | 7 |
| `internal/intelligence/retrieval/file_summary.go` | MODIFY | 7 |
| `cmd/agentctl/cmd/index.go` | MODIFY | 8 |
| `cmd/agentctl/cmd/index_repo.go` | MODIFY | 8 |
| `skills/code_incremental_index/main.go` | MODIFY | 8 |
| `skills/code_semantic_search/main.go` | MODIFY | 8 |
