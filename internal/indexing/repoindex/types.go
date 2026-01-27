// Package repoindex implements a lightweight repo graph index for navigation.
package repoindex

import (
	"context"
	"encoding/json"
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
	EdgeRefersTo   EdgeType = "REFERS_TO"
	EdgeCalls      EdgeType = "CALLS"
	EdgeImplements EdgeType = "IMPLEMENTS"
	EdgeEmbeds     EdgeType = "EMBEDS"
	EdgeTests      EdgeType = "TESTS"
)

const (
	ToolSearch = "repo.index.search"
	ToolExpand = "repo.index.expand"
	ToolOpen   = "repo.index.open"
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
	ID        string    `json:"id"`
	Kind      NodeKind  `json:"kind"`
	Pkg       string    `json:"pkg,omitempty"`
	File      string    `json:"file,omitempty"`
	Name      string    `json:"name,omitempty"`
	Signature string    `json:"signature,omitempty"`
	SpanStart int       `json:"span_start,omitempty"`
	SpanEnd   int       `json:"span_end,omitempty"`
	Exported  bool      `json:"exported,omitempty"`
	Doc       string    `json:"doc,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	Hash      string    `json:"hash,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
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
type SymbolSummaryProvider interface {
	Summary(ctx context.Context, symbolID string) (string, error)
}

// BuildOptions configure repoindex build behavior.
type BuildOptions struct {
	RepoRoot              string
	Patterns              []string
	IncludeTests          bool
	IncludeGo             bool
	IncludeTypescript     bool
	IncludeElixir         bool
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

// PackageID returns the stable node ID for a package.
func PackageID(pkg string) string {
	return "pkg:" + pkg
}

// FileID returns the stable node ID for a file.
func FileID(pkg, file string) string {
	return "file:" + pkg + ":" + file
}

// SymbolID returns the stable node ID for a symbol.
func SymbolID(pkg, symbolID string) string {
	return "sym:" + pkg + ":" + symbolID
}
