package symbol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/indexing/embeddingtext"
	"github.com/jkatigb/agentctl/internal/platform/config"
	platformsymbol "github.com/jkatigb/agentctl/internal/platform/symbolutil"
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

// FileSummaryType is the memory entry type for file-level summaries.
// Used by the semantic search tree ("smart TOC") feature.
// Entry names follow the format: "file://<workspace>/<file_path>"
const FileSummaryType = "file_summary"

// SymbolSummaryType is the memory entry type for symbol-level summaries.
// Entry names follow the format: "symbol-summary://<workspace>/<symbol_id>"
const SymbolSummaryType = "symbol_summary"

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

	// Force bypasses incremental checks and rewrites symbol/call entries even when
	// file and symbol digests appear unchanged. This is useful after indexer
	// behavior changes (e.g., call extraction tweaks) where content hashes are
	// insufficient to trigger updates.
	Force bool `json:"force,omitempty"`

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

	// EmbeddingEnabled controls whether symbol embeddings are enqueued.
	EmbeddingEnabled bool `json:"embedding_enabled,omitempty"`

	// EmbeddingStoreRoot is the filesystem root for the embedding queue store.
	EmbeddingStoreRoot string `json:"embedding_store_root,omitempty"`

	// EmbeddingModel overrides the embedding model for symbol jobs.
	EmbeddingModel string `json:"embedding_model,omitempty"`

	// EmbeddingTextMode controls how symbol text is prepared for embedding.
	EmbeddingTextMode config.EmbedSymbolTextMode `json:"embedding_text_mode,omitempty"`
}

// DefaultConfig returns sensible defaults for symbol indexing.
func DefaultConfig() Config {
	return Config{
		Enabled:           false,
		MaxFileLOC:        500,
		MaxFileKB:         512,
		EmbeddingEnabled:  false,
		EmbeddingTextMode: config.EmbedSymbolTextModeRaw,
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
	return platformsymbol.EntryName(workspace, filePath, symbolName)
}

// FileMetaEntryName generates the canonical name for a file meta entry.
// Format: symbol-meta://<workspace>/<file_path>
//
// This name is used as the unique key in named memory for file freshness entries
// with type="code_symbol_file_meta". Keyed by file path (not symbol), as file_meta
// tracks per-file freshness rather than per-symbol state.
func FileMetaEntryName(workspace, filePath string) string {
	return platformsymbol.FileMetaEntryName(workspace, filePath)
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

// FileSummaryResult is the structured metadata stored in NamedEntry.Result
// for file_summary entries. These entries are used by the semantic search tree
// ("smart TOC") feature to provide file-level descriptions.
type FileSummaryResult struct {
	// FilePath is the relative path within the workspace.
	FilePath string `json:"file_path"`

	// Package is the package/module name (language-dependent).
	Package string `json:"package,omitempty"`

	// Symbols is the list of top exported symbol names in the file.
	Symbols []string `json:"symbols,omitempty"`

	// Digest is the sha256 hash of the summary generation inputs.
	// Used for cache invalidation.
	Digest string `json:"digest"`

	// Language is the detected programming language.
	Language string `json:"language,omitempty"`

	// LineCount is the number of lines in the file.
	LineCount int `json:"line_count,omitempty"`
}

// FileSummaryEntryName generates the canonical name for a file summary entry.
// Format: file://<workspace>/<file_path>
//
// This name is used as the unique key in named memory for file summary entries
// with type="file_summary". Compatible with extractFilePath in retrieval package.
func FileSummaryEntryName(workspace, filePath string) string {
	return fmt.Sprintf("file://%s/%s", workspace, filePath)
}

// FileSummaryInput represents the inputs used to generate a file summary.
// The digest of this struct (JSON-encoded) serves as the cache key.
type FileSummaryInput struct {
	// FilePath is the relative path within the workspace.
	FilePath string `json:"file_path"`

	// SymbolsHash is the sha256 hash of the file's exported symbols.
	// This ensures regeneration only when structural content changes
	// (functions added/removed/renamed), not comments or implementation details.
	SymbolsHash string `json:"symbols_hash"`

	// Package is the package/module name.
	Package string `json:"package,omitempty"`

	// PackageDoc is the package-level documentation comment.
	PackageDoc string `json:"package_doc,omitempty"`

	// FirstComment is the first comment block in the file.
	FirstComment string `json:"first_comment,omitempty"`

	// TopSymbols are the top N exported symbol signatures.
	TopSymbols []string `json:"top_symbols,omitempty"`
}

// NormalizeFileSummaryInput applies deterministic normalization for digesting and prompting.
func NormalizeFileSummaryInput(input FileSummaryInput) FileSummaryInput {
	normalized := input
	normalized.Package = strings.TrimSpace(input.Package)
	normalized.PackageDoc = embeddingtext.NormalizeDoc(input.PackageDoc)
	normalized.FirstComment = embeddingtext.NormalizeFirstComment(input.FirstComment)
	normalized.TopSymbols = normalizeSummarySymbols(input.TopSymbols)
	return normalized
}

func normalizeSummarySymbols(symbols []string) []string {
	if len(symbols) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(symbols))
	cleaned := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		clean := strings.TrimSpace(symbol)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		cleaned = append(cleaned, clean)
	}
	sort.Strings(cleaned)
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// ComputeFileSummaryDigest computes a digest of the summary inputs for caching.
func ComputeFileSummaryDigest(input FileSummaryInput) string {
	normalized := NormalizeFileSummaryInput(input)
	data, _ := json.Marshal(normalized)
	return ComputeDigest(data)
}

// ComputeSymbolsHash extracts exported symbols from file content and returns a hash.
// This hash changes only when the file's structural content changes (symbols added/
// removed/renamed), not when comments or implementation details change.
func ComputeSymbolsHash(content []byte, filePath string) string {
	symbols := ExtractSymbolSignatures(content, filePath)
	// Sort for consistent hashing
	sort.Strings(symbols)
	data, _ := json.Marshal(symbols)
	return ComputeDigest(data)
}

// ExtractSymbolSignatures extracts exported symbol signatures from file content.
// Returns a list of "name:kind" strings for each exported symbol.
func ExtractSymbolSignatures(content []byte, filePath string) []string {
	ext := filepath.Ext(filePath)
	switch ext {
	case ".go":
		return extractGoSymbolSignatures(content)
	case ".ts", ".tsx", ".js", ".jsx":
		return extractJSSymbolSignatures(content)
	case ".py":
		return extractPythonSymbolSignatures(content)
	case ".ex", ".exs":
		return extractElixirSymbolSignatures(content)
	default:
		return extractGenericSymbolSignatures(content)
	}
}

// MarshalFileSummaryResult serializes a FileSummaryResult to JSON bytes.
func MarshalFileSummaryResult(result FileSummaryResult) ([]byte, error) {
	return json.Marshal(result)
}

// UnmarshalFileSummaryResult deserializes a FileSummaryResult from JSON bytes.
func UnmarshalFileSummaryResult(data []byte) (*FileSummaryResult, error) {
	var result FileSummaryResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SymbolSummaryResult stores metadata used to validate symbol summaries.
type SymbolSummaryResult struct {
	SymbolID  string `json:"symbol_id"`
	FilePath  string `json:"file_path,omitempty"`
	Name      string `json:"name"`
	Kind      Kind   `json:"kind,omitempty"`
	Signature string `json:"signature,omitempty"`
	Digest    string `json:"digest"`
	Language  string `json:"language,omitempty"`
}

// SymbolSummaryEntryName returns the named memory entry for a symbol summary.
func SymbolSummaryEntryName(workspace, symbolID string) string {
	return fmt.Sprintf("symbol-summary://%s/%s", workspace, symbolID)
}

// SymbolSummaryInput captures the fields used to generate a symbol summary.
type SymbolSummaryInput struct {
	SymbolID      string `json:"symbol_id"`
	FilePath      string `json:"file_path,omitempty"`
	Name          string `json:"name"`
	Kind          Kind   `json:"kind,omitempty"`
	Signature     string `json:"signature,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	BodyDigest    string `json:"body_digest,omitempty"`
	Language      string `json:"language,omitempty"`
}

// ComputeSymbolSummaryDigest hashes the summary generation inputs for caching.
func ComputeSymbolSummaryDigest(input SymbolSummaryInput) string {
	data, _ := json.Marshal(input)
	return ComputeDigest(data)
}

// MarshalSymbolSummaryResult serializes the symbol summary result payload.
func MarshalSymbolSummaryResult(result SymbolSummaryResult) ([]byte, error) {
	return json.Marshal(result)
}

// UnmarshalSymbolSummaryResult parses the symbol summary result payload.
func UnmarshalSymbolSummaryResult(data []byte) (*SymbolSummaryResult, error) {
	var result SymbolSummaryResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// extractGoSymbolSignatures extracts exported symbol signatures from Go source.
// Uses the Go AST parser for accurate extraction.
func extractGoSymbolSignatures(content []byte) []string {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, 0) // No comments needed
	if err != nil {
		return nil
	}

	var symbols []string

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Name == nil || !ast.IsExported(d.Name.Name) {
				continue
			}
			kind := "func"
			if d.Recv != nil {
				kind = "method"
			}
			// Include signature for methods to differentiate receivers
			sig := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				sig = goExprString(d.Recv.List[0].Type) + "." + d.Name.Name
			}
			symbols = append(symbols, sig+":"+kind)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !ast.IsExported(s.Name.Name) {
						continue
					}
					kind := "type"
					switch s.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					symbols = append(symbols, s.Name.Name+":"+kind)

				case *ast.ValueSpec:
					for _, name := range s.Names {
						if !ast.IsExported(name.Name) {
							continue
						}
						kind := d.Tok.String() // "const" or "var"
						symbols = append(symbols, name.Name+":"+kind)
					}
				}
			}
		}
	}

	return symbols
}

// goExprString converts a Go AST expression to a string (for receiver types).
func goExprString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return "*" + goExprString(e.X)
	default:
		return ""
	}
}

// extractJSSymbolSignatures extracts exported symbols from JS/TS source.
func extractJSSymbolSignatures(content []byte) []string {
	var symbols []string
	lines := strings.Split(string(content), "\n")

	// Patterns for exports
	exportFunc := regexp.MustCompile(`^export\s+(?:async\s+)?function\s+(\w+)`)
	exportConst := regexp.MustCompile(`^export\s+(?:const|let|var)\s+(\w+)`)
	exportClass := regexp.MustCompile(`^export\s+(?:abstract\s+)?class\s+(\w+)`)
	exportInterface := regexp.MustCompile(`^export\s+(?:interface|type)\s+(\w+)`)
	exportDefault := regexp.MustCompile(`^export\s+default\s+(?:function|class)\s+(\w+)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		if m := exportFunc.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":function")
		} else if m := exportConst.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":const")
		} else if m := exportClass.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":class")
		} else if m := exportInterface.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":type")
		} else if m := exportDefault.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":default")
		}
	}

	return symbols
}

// extractPythonSymbolSignatures extracts public symbols from Python source.
func extractPythonSymbolSignatures(content []byte) []string {
	var symbols []string
	lines := strings.Split(string(content), "\n")

	funcDef := regexp.MustCompile(`^def\s+(\w+)\s*\(`)
	classDef := regexp.MustCompile(`^class\s+(\w+)`)

	for _, line := range lines {
		// Only top-level definitions (no leading whitespace)
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		if m := funcDef.FindStringSubmatch(line); m != nil {
			name := m[1]
			if !strings.HasPrefix(name, "_") { // Public only
				symbols = append(symbols, name+":function")
			}
		} else if m := classDef.FindStringSubmatch(line); m != nil {
			name := m[1]
			if !strings.HasPrefix(name, "_") { // Public only
				symbols = append(symbols, name+":class")
			}
		}
	}

	return symbols
}

// extractElixirSymbolSignatures extracts public symbols from Elixir source.
func extractElixirSymbolSignatures(content []byte) []string {
	var symbols []string
	lines := strings.Split(string(content), "\n")

	moduleDef := regexp.MustCompile(`^\s*defmodule\s+([A-Z][A-Za-z0-9_.]*)\s+do`)
	funcDef := regexp.MustCompile(`^\s*def\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)
	macroDef := regexp.MustCompile(`^\s*defmacro\s+([a-z_][a-z0-9_?!]*)\s*(?:\(|,|\s+do)`)
	typeDef := regexp.MustCompile(`^\s*@type\s+([a-z_][a-z0-9_]*)\s*::`)
	callbackDef := regexp.MustCompile(`^\s*@callback\s+([a-z_][a-z0-9_?!]*)\s*\(`)

	for _, line := range lines {
		if m := moduleDef.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":module")
		} else if m := funcDef.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":function")
		} else if m := macroDef.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":macro")
		} else if m := typeDef.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":type")
		} else if m := callbackDef.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":callback")
		}
	}

	return symbols
}

// extractGenericSymbolSignatures extracts symbols using generic patterns.
// Fallback for unsupported languages.
func extractGenericSymbolSignatures(content []byte) []string {
	var symbols []string
	lines := strings.Split(string(content), "\n")

	// Generic patterns that work across many languages
	funcPattern := regexp.MustCompile(`(?:func|function|def|fn)\s+([A-Z]\w*)`)
	typePattern := regexp.MustCompile(`(?:type|class|struct|interface)\s+([A-Z]\w*)`)

	for _, line := range lines {
		if m := funcPattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":function")
		}
		if m := typePattern.FindStringSubmatch(line); m != nil {
			symbols = append(symbols, m[1]+":type")
		}
	}

	return symbols
}
