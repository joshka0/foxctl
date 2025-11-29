// Package symbol implements the code symbol index as a post-review indexer.
// It stores symbol definitions, call relationships, and embeddings per
// code_symbol_index_and_swe_grep.md spec.
package symbol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SymbolType is the memory entry type for symbol entries.
const SymbolType = "code_symbol"

// CallEdgeType is the memory entry type for call graph edges.
const CallEdgeType = "code_symbol_call"

// FileMetaType is the memory entry type for file freshness tracking.
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
type Symbol struct {
	// ID is a stable identifier, e.g. "pkg/auth/login.go:Login".
	ID string `json:"id"`

	// FilePath is the relative path within the workspace.
	FilePath string `json:"file_path"`

	// Name is the symbol name (e.g. "Login", "CalculateGravity").
	Name string `json:"name"`

	// Language is the normalized language identifier.
	Language string `json:"language"`

	// Kind is the symbol kind (function, method, class, etc.).
	Kind Kind `json:"kind"`

	// StartByte is the byte offset where the symbol starts.
	StartByte int `json:"start_byte"`

	// EndByte is the byte offset where the symbol ends.
	EndByte int `json:"end_byte"`

	// StartLine is the 1-indexed line number where the symbol starts.
	StartLine int `json:"start_line,omitempty"`

	// EndLine is the 1-indexed line number where the symbol ends.
	EndLine int `json:"end_line,omitempty"`

	// Signature is the skeleton representation (optional).
	Signature string `json:"signature,omitempty"`

	// BodyDigest is sha256:<hex> digest of the symbol body.
	BodyDigest string `json:"body_digest,omitempty"`

	// FileDigest is sha256:<hex> digest of the entire file.
	FileDigest string `json:"file_digest,omitempty"`

	// Documentation is extracted doc comments (optional).
	Documentation string `json:"documentation,omitempty"`
}

// CallEdge represents a call relationship between two symbols.
type CallEdge struct {
	// SourceID is the caller symbol ID.
	SourceID string `json:"source_id"`

	// TargetID is the callee symbol ID.
	TargetID string `json:"target_id"`

	// Count is the number of observed callsites.
	Count int `json:"count,omitempty"`
}

// FileMeta tracks file freshness for incremental updates.
type FileMeta struct {
	// FilePath is the relative file path.
	FilePath string `json:"file_path"`

	// ContentHash is sha256:<hex> digest of file contents.
	ContentHash string `json:"content_hash"`

	// LastModTime is the last observed modification time (Unix).
	LastModTime int64 `json:"last_mod_time"`

	// Count is the number of symbols in this file.
	Count int `json:"symbol_count"`
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
func ID(filePath, symbolName string) string {
	return fmt.Sprintf("%s:%s", filePath, symbolName)
}

// EntryName generates the canonical name for a symbol memory entry.
// Format: symbol://<workspace>/<file_path>:<symbol_name>
func EntryName(workspace, filePath, symbolName string) string {
	return fmt.Sprintf("symbol://%s/%s:%s", workspace, filePath, symbolName)
}

// FileMetaEntryName generates the canonical name for a file meta entry.
// Format: symbol-meta://<workspace>/<file_path>
func FileMetaEntryName(workspace, filePath string) string {
	return fmt.Sprintf("symbol-meta://%s/%s", workspace, filePath)
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
