// Package symbol implements the code symbol index as a post-review indexer.
// It stores symbol definitions, call relationships, and embeddings per
// code_symbol_index_and_swe_grep.md spec.
//
// # Named Memory Type Mapping
//
// This package maps conceptual tables from the spec to named memory entry types:
//
//   - [SymbolType] ("code_symbol") → conceptual `symbols` rows (spec §3.1)
//   - [CallEdgeType] ("code_symbol_call") → conceptual `calls` rows (spec §3.2)
//   - [FileMetaType] ("code_symbol_file_meta") → conceptual `file_meta` rows (spec §3.3)
//
// Symbols are stored as named memory entries with type="code_symbol", where:
//   - Entry.Name follows the [EntryName] format: "symbol://<workspace>/<file_path>:<symbol_name>"
//   - Entry.Result contains a JSON-serialized [Result] struct with the [Symbol] and provenance
//   - Entry.Embedding (when vector support is enabled) holds the symbol embedding
//
// Call edges are stored with type="code_symbol_call", keyed by source and target symbol IDs.
// File metadata is stored with type="code_symbol_file_meta" to track freshness for incremental updates.
package symbol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SymbolType is the memory entry type for symbol entries.
// Maps to the conceptual `symbols` table in code_symbol_index_and_swe_grep.md §3.1.
const SymbolType = "code_symbol"

// CallEdgeType is the memory entry type for call graph edges.
// Maps to the conceptual `calls` table in code_symbol_index_and_swe_grep.md §3.2.
const CallEdgeType = "code_symbol_call"

// FileMetaType is the memory entry type for file freshness tracking.
// Maps to the conceptual `file_meta` table in code_symbol_index_and_swe_grep.md §3.3.
const FileMetaType = "code_symbol_file_meta"

// Kind represents the kind of code symbol.
type Kind string

// Symbol kinds.
const (
	KindFunction    Kind = "function"
	KindMethod      Kind = "method"
	KindClass       Kind = "class"
	KindStruct      Kind = "struct"
	KindInterface   Kind = "interface"
	KindVariable    Kind = "variable"
	KindConstant    Kind = "constant"
	KindType        Kind = "type"
	KindFileSummary Kind = "file_summary"
)

// Symbol represents a code symbol (function, method, class, etc.).
// See code_symbol_index_and_swe_grep.md §3.1 for the conceptual data model.
//
// Note: The `embedding` field from the spec is stored in the named memory entry's
// Embedding column, not in this struct. StartLine, EndLine, and Documentation are
// implementation additions permitted by §3.1 ("Implementations MAY add additional columns").
type Symbol struct {
	// ID is a stable identifier for the symbol, e.g. "pkg/auth/login.go:Login".
	// Per spec §3.1: MUST remain stable across re-indexing as long as the symbol
	// logically exists at that path. See [ID] for the generation function.
	//
	// ID Stability: In v1, the ID is derived from (file_path, symbol_name). This means:
	//   - File renames cause ID changes (known limitation; see §3.1 design notes)
	//   - Symbol renames are treated as deletions + additions (new ID created)
	//   - Unchanged symbols at the same path retain stable IDs across re-indexing
	ID string `json:"id"`

	// FilePath is the relative path within the workspace (spec §3.1: not null).
	FilePath string `json:"file_path"`

	// Name is the symbol name, e.g. "Login", "CalculateGravity" (spec §3.1: not null).
	Name string `json:"name"`

	// Language is the normalized language identifier, e.g. "go", "python" (spec §3.1: not null).
	Language string `json:"language"`

	// Kind is the symbol kind: function, method, class, struct, interface, file_summary, etc.
	// (spec §3.1: not null).
	Kind Kind `json:"kind"`

	// StartByte is the byte offset into FilePath where the symbol starts (spec §3.1).
	StartByte int `json:"start_byte"`

	// EndByte is the byte offset into FilePath where the symbol ends (spec §3.1).
	EndByte int `json:"end_byte"`

	// StartLine is the 1-indexed line number where the symbol starts.
	// Implementation addition (not in spec §3.1) for navigation convenience.
	StartLine int `json:"start_line,omitempty"`

	// EndLine is the 1-indexed line number where the symbol ends.
	// Implementation addition (not in spec §3.1) for navigation convenience.
	EndLine int `json:"end_line,omitempty"`

	// Signature is the skeleton representation, e.g. "func Login(ctx context.Context, in Input) error".
	// (spec §3.1: optional).
	Signature string `json:"signature,omitempty"`

	// BodyDigest is sha256:<hex> digest of the symbol body contents.
	// Used to detect changes and avoid unnecessary re-embeds (spec §3.1: optional).
	// Per spec §4.3: unchanged body_digest means embeddings can be reused.
	BodyDigest string `json:"body_digest,omitempty"`

	// FileDigest is sha256:<hex> digest of the entire file contents at indexing time.
	// (spec §3.1: optional).
	FileDigest string `json:"file_digest,omitempty"`

	// Documentation is extracted doc comments.
	// Implementation addition (not in spec §3.1) permitted by "Implementations MAY add additional columns".
	Documentation string `json:"documentation,omitempty"`
}

// CallEdge represents a directed call relationship between two symbols.
// See code_symbol_index_and_swe_grep.md §3.2 for the conceptual data model.
//
// The call graph is intentionally conservative per spec §3.2: heuristics (e.g., name-based
// resolution, imports) MAY introduce extra edges, but explicit confidence scores are not
// required in v1. Primary key is (source_id, target_id).
type CallEdge struct {
	// SourceID is the caller symbol ID; foreign key to Symbol.ID (spec §3.2: not null).
	SourceID string `json:"source_id"`

	// TargetID is the callee symbol ID; foreign key to Symbol.ID (spec §3.2: not null).
	TargetID string `json:"target_id"`

	// Count is the number of observed callsites (spec §3.2: default 1, primarily advisory).
	Count int `json:"count,omitempty"`
}

// FileMeta tracks file freshness for incremental updates.
// See code_symbol_index_and_swe_grep.md §3.3 for the conceptual data model.
//
// Per spec §3.3: Indexers MUST consult file_meta to avoid unnecessary work,
// but MAY still force re-indexing under certain conditions (e.g. configuration changes).
type FileMeta struct {
	// FilePath is the relative file path (spec §3.3: primary key).
	FilePath string `json:"file_path"`

	// ContentHash is sha256:<hex> digest of file contents (spec §3.3).
	ContentHash string `json:"content_hash"`

	// LastModTime is the last observed modification time as Unix timestamp (spec §3.3).
	LastModTime int64 `json:"last_mod_time"`

	// Count is the number of symbols in this file.
	// Implementation addition (not in spec §3.3) for diagnostics and incremental update tracking.
	Count int `json:"symbol_count"`

	// SymbolDigests maps symbol IDs to their body_digest values.
	// Implementation addition for per-symbol incremental updates per spec §4.3:
	// unchanged body_digest means the symbol's embedding can be reused without re-computation.
	SymbolDigests map[string]string `json:"symbol_digests,omitempty"`
}

// Result is the structured metadata stored in NamedEntry.Result
// for code_symbol entries.
type Result struct {
	Symbol Symbol   `json:"symbol"`
	Source *Source  `json:"source,omitempty"`
	Calls  []string `json:"calls,omitempty"` // IDs of called symbols
}

// Source tracks provenance of the symbol indexing.
type Source struct {
	// TaskID is the task that triggered (re)indexing.
	TaskID string `json:"task_id,omitempty"`

	// ReviewID is the review record if triggered post-review.
	ReviewID string `json:"review_id,omitempty"`

	// Actor is the actor that created the entry.
	Actor string `json:"actor,omitempty"`

	// Reason describes why the symbol was indexed.
	Reason string `json:"reason,omitempty"`
}

// Config holds configuration for the symbol indexer.
type Config struct {
	// Enabled controls whether symbol indexing is active.
	Enabled bool `json:"enabled"`

	// MaxFileLOC is the threshold for "large file" handling.
	// Files above this are indexed per-symbol, not as a whole.
	MaxFileLOC int `json:"max_file_loc,omitempty"`

	// MaxFileKB is the maximum file size in KB to index (0 = no limit).
	MaxFileKB int `json:"max_file_kb,omitempty"`

	// IncludeGlobs are glob patterns for files to include.
	IncludeGlobs []string `json:"include_globs,omitempty"`

	// ExcludeGlobs are glob patterns for files to exclude.
	ExcludeGlobs []string `json:"exclude_globs,omitempty"`

	// Languages lists the languages to index (empty = all supported).
	Languages []string `json:"languages,omitempty"`
}

// DefaultConfig returns sensible defaults for symbol indexing.
func DefaultConfig() Config {
	return Config{
		Enabled:    false,
		MaxFileLOC: 500,
		MaxFileKB:  512,
	}
}

// ID generates a stable symbol identifier.
// Format: <file_path>:<symbol_name>
//
// Per spec §3.1: ID MUST remain stable across re-indexing as long as the symbol
// logically exists at that path.
//
// # ID Stability and Renames (v1 behavior)
//
// In v1, symbol IDs are derived purely from (file_path, symbol_name):
//
//   - File path changes: If a file is renamed or moved, all symbol IDs in that file
//     change. This is a known limitation; future versions may detect renames via
//     content_hash matching and remap IDs.
//
//   - Symbol renames: Renaming a symbol (e.g. "Login" → "Authenticate") is treated
//     as a deletion of the old ID plus creation of a new ID. Embeddings are not
//     preserved across renames.
//
//   - Symbol modifications: Changing a symbol's body while keeping its name preserves
//     the ID. The indexer uses [Symbol.BodyDigest] to detect whether re-embedding is
//     needed (per spec §4.3).
//
//   - Symbol deletions: Removing a symbol from a file causes its ID (and call edges)
//     to be removed from the index.
func ID(filePath, symbolName string) string {
	return fmt.Sprintf("%s:%s", filePath, symbolName)
}

// EntryName generates the canonical name for a symbol memory entry.
// Format: symbol://<workspace>/<file_path>:<symbol_name>
//
// This name is used as the unique key in named memory for symbol entries
// with type="code_symbol". The format mirrors the semantic file index naming
// convention (see semantic_file_index.md §3.2).
func EntryName(workspace, filePath, symbolName string) string {
	return fmt.Sprintf("symbol://%s/%s:%s", workspace, filePath, symbolName)
}

// FileMetaEntryName generates the canonical name for a file meta entry.
// Format: symbol-meta://<workspace>/<file_path>
//
// This name is used as the unique key in named memory for file freshness entries
// with type="code_symbol_file_meta". Keyed by file path (not symbol), as file_meta
// tracks per-file freshness rather than per-symbol state.
func FileMetaEntryName(workspace, filePath string) string {
	return fmt.Sprintf("symbol-meta://%s/%s", workspace, filePath)
}

func callEdgeEntryName(workspace, sourceID, targetID string) string {
	return fmt.Sprintf("call://%s/%s->%s", workspace, sourceID, targetID)
}

// ComputeDigest computes a sha256 digest of content.
func ComputeDigest(content []byte) string {
	hash := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// MarshalResult serializes a result struct to JSON bytes.
func MarshalResult(v any) ([]byte, error) {
	return json.Marshal(v)
}

// UnmarshalResult deserializes a Result from JSON bytes.
func UnmarshalResult(data []byte) (*Result, error) {
	var result Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// UnmarshalFileMeta deserializes a FileMeta from JSON bytes.
func UnmarshalFileMeta(data []byte) (*FileMeta, error) {
	var result FileMeta
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
