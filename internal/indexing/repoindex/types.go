package repoindex

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// NodeKind identifies the type of node in the repo graph.
type NodeKind string

const (
	NodePackage NodeKind = "package"
	NodeFile    NodeKind = "file"
	NodeSymbol  NodeKind = "symbol"
	NodeConcept NodeKind = "concept"
)

// EdgeType identifies the relationship between nodes.
type EdgeType string

const (
	EdgeContains   EdgeType = "CONTAINS"
	EdgeImports    EdgeType = "IMPORTS"
	EdgeUsesSymbol EdgeType = "USES_SYMBOL"
	EdgeRefersTo   EdgeType = "REFERS_TO"
	EdgeCalls      EdgeType = "CALLS"
	EdgeImplements EdgeType = "IMPLEMENTS"
	EdgeEmbeds     EdgeType = "EMBEDS"
	EdgeTests      EdgeType = "TESTS"

	// Comment-derived edges (soft edges, weight < 1.0)
	EdgeHasKeyword      EdgeType = "HAS_KEYWORD"
	EdgeHasOutputField  EdgeType = "HAS_OUTPUT_FIELD"
	EdgeTouchesResource EdgeType = "TOUCHES_RESOURCE"
	EdgeEmitsEvent      EdgeType = "EMITS_EVENT"
	EdgeDocRelated      EdgeType = "DOC_RELATED"
	EdgeDocFlow         EdgeType = "DOC_FLOW"
)

// Concept node prefixes (repo-key namespacing added at ID creation).
const (
	ConceptKeyword  = "kw:"
	ConceptField    = "field:"
	ConceptResource = "res:"
	ConceptEvent    = "event:"
	ConceptCommand  = "cmd:"
	ConceptEnvVar   = "env:"
	ConceptChart    = "chart:"
	ConceptApp      = "app:"
)

const (
	ToolSearch  = "repo_index_search"
	ToolExpand  = "repo_index_expand"
	ToolOpen    = "repo_index_open"
	ToolDAGGrep = "repo_index_dag_grep"

	// Legacy tool names (dot-delimited). Kept for backward compatibility.
	ToolSearchLegacy  = "repo.index.search"
	ToolExpandLegacy  = "repo.index.expand"
	ToolOpenLegacy    = "repo.index.open"
	ToolDAGGrepLegacy = "repo.index.dag_grep"
)

// Edge sets for ergonomic selection in tools.
var (
	EdgeSetStructural = []EdgeType{
		EdgeContains,
		EdgeImports,
		EdgeUsesSymbol,
		EdgeRefersTo,
		EdgeCalls,
		EdgeImplements,
		EdgeEmbeds,
		EdgeTests,
	}
	EdgeSetDoc = []EdgeType{
		EdgeHasKeyword,
		EdgeHasOutputField,
		EdgeTouchesResource,
		EdgeEmitsEvent,
		EdgeDocRelated,
		EdgeDocFlow,
	}
)

// Direction controls edge traversal direction.
type Direction string

const (
	DirOut Direction = "out"
	DirIn  Direction = "in"
)

// Node represents a repo graph node.
// SpanStart and SpanEnd are 1-based line numbers (0 when unknown).
type Node struct {
	ID        string          `json:"id"`
	Kind      NodeKind        `json:"kind"`
	Pkg       string          `json:"pkg,omitempty"`
	File      string          `json:"file,omitempty"`
	Name      string          `json:"name,omitempty"`
	Signature string          `json:"signature,omitempty"`
	SpanStart int             `json:"span_start,omitempty"`
	SpanEnd   int             `json:"span_end,omitempty"`
	Exported  bool            `json:"exported,omitempty"`
	Doc       string          `json:"doc,omitempty"`
	Summary   string          `json:"summary,omitempty"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	Hash      string          `json:"hash,omitempty"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ScoredNode captures a node plus a relevance score.
// For FTS/BM25 searches, lower scores are better; callers may normalize as needed.
type ScoredNode struct {
	Node  Node    `json:"node"`
	Score float64 `json:"score"`
}

// Edge represents a directed relationship between nodes.
type Edge struct {
	Src    string          `json:"src"`
	Dst    string          `json:"dst"`
	Type   EdgeType        `json:"type"`
	Weight float64         `json:"weight"`
	Meta   json.RawMessage `json:"meta,omitempty"`
}

// IndexMeta tracks the repo graph index state.
type IndexMeta struct {
	RepoRoot      string    `json:"repo_root"`
	HeadSHA       string    `json:"head_sha,omitempty"`
	SchemaVersion int       `json:"schema_version"`
	IndexedAt     time.Time `json:"indexed_at"`
	Languages     []string  `json:"languages,omitempty"`
}

// Stats summarizes node and edge counts.
type Stats struct {
	NodesTotal  int              `json:"nodes_total"`
	EdgesTotal  int              `json:"edges_total"`
	NodesByKind map[NodeKind]int `json:"nodes_by_kind"`
}

// FileSummaryProvider supplies file summaries for repoindex nodes.
type FileSummaryProvider interface {
	Summary(ctx context.Context, filePath string) (string, error)
}

// SymbolSummaryProvider resolves summaries for symbol nodes.
// Accepts symbol package, stable ID and stable key for lookup.
type SymbolSummaryProvider interface {
	Summary(ctx context.Context, symbolID, symbolKey, pkg string) (string, error)
}

// BuildOptions configure repoindex build behavior.
type BuildOptions struct {
	RepoRoot              string
	RepoKey               string
	Patterns              []string
	IncludeTests          bool
	IncludeGo             bool
	IncludePython         bool
	IncludeRust           bool
	IncludeTypescript     bool
	IncludeElixir         bool
	IncludeTerraform      bool
	IncludeKubernetes     bool
	IncludeShell          bool
	DryRun                bool
	SummaryProvider       FileSummaryProvider
	SymbolSummaryProvider SymbolSummaryProvider
}

// BuildResult captures build statistics.
type BuildResult struct {
	Packages int `json:"packages"`
	Files    int `json:"files"`
	Symbols  int `json:"symbols"`
	Nodes    int `json:"nodes"`
	Edges    int `json:"edges"`
}

// ExpandOptions controls graph expansion.
type ExpandOptions struct {
	Direction  Direction
	EdgeTypes  []EdgeType
	Depth      int
	Budget     int
	PerNodeCap int
}

// ExpandResult contains expanded nodes and edges.
type ExpandResult struct {
	Nodes []Node   `json:"nodes"`
	Edges []Edge   `json:"edges"`
	Trail []string `json:"trail,omitempty"`
}

// LocatorEntry maps a stable SymbolKey to its current file location and metadata.
// This table is the "phone book" that resolves a key to where the symbol currently lives.
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

// NamespacedID prefixes a raw node ID with the repo key namespace.
// It panics if repoKey is empty to enforce repo-scoped identifiers.
func NamespacedID(repoKey, raw string) string {
	repoKey = strings.TrimSpace(repoKey)
	if repoKey == "" {
		panic("repoindex: repoKey is required")
	}
	if raw == "" {
		return ""
	}
	return repoKey + "::" + raw
}

// SplitNamespacedID splits a repo-key namespaced ID into repoKey and raw ID.
// If the delimiter is missing, the repoKey is empty and the raw ID is returned.
func SplitNamespacedID(id string) (string, string) {
	parts := strings.SplitN(id, "::", 2)
	if len(parts) != 2 {
		return "", id
	}
	return parts[0], parts[1]
}

// PackageID returns the stable node ID for a package.
func PackageID(repoKey, pkg string) string {
	return NamespacedID(repoKey, "pkg:"+pkg)
}

// FileID returns the stable node ID for a file.
func FileID(repoKey, pkg, file string) string {
	return NamespacedID(repoKey, "file:"+pkg+":"+file)
}

// SymbolID returns the stable node ID for a symbol.
func SymbolID(repoKey, pkg, symbolID string) string {
	return NamespacedID(repoKey, "sym:"+pkg+":"+symbolID)
}
