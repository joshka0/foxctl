package repoindex

import (
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

	EdgeEnforces             EdgeType = "ENFORCES"
	EdgeProtectsAgainst      EdgeType = "PROTECTS_AGAINST"
	EdgeVerifiedBy           EdgeType = "VERIFIED_BY"
	EdgeDescribedBy          EdgeType = "DESCRIBED_BY"
	EdgeDecidedBy            EdgeType = "DECIDED_BY"
	EdgeImplementsProtocol   EdgeType = "IMPLEMENTS_PROTOCOL"
	EdgeParticipatesIn       EdgeType = "PARTICIPATES_IN"
	EdgeDeclaresAnchorTarget EdgeType = "DECLARES_ANCHOR_TARGET"

	// Reserved in narrow PR-B. No emitter should produce this edge until anchor
	// retrieval evals prove beacon anchors do not hijack broad queries.
	EdgeBeaconFor EdgeType = "BEACON_FOR"
	// Reserved in PR-B1. No emitter should produce this edge until PR-B.5.
	EdgeCoChangesWith EdgeType = "CO_CHANGES_WITH"
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
	ToolBuild           = "repo_index_build"
	ToolEnrichSummaries = "repo_index_enrich_summaries"
	ToolSearch          = "repo_index_search"
	ToolExpand          = "repo_index_expand"
	ToolOpen            = "repo_index_open"
	ToolDAGGrep         = "repo_index_dag_grep"

	// Legacy tool names (dot-delimited). Kept for backward compatibility.
	ToolBuildLegacy           = "repo.index.build"
	ToolEnrichSummariesLegacy = "repo.index.enrich_summaries"
	ToolSearchLegacy          = "repo.index.search"
	ToolExpandLegacy          = "repo.index.expand"
	ToolOpenLegacy            = "repo.index.open"
	ToolDAGGrepLegacy         = "repo.index.dag_grep"
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
	EdgeSetSemanticAnchors = []EdgeType{
		EdgeEnforces,
		EdgeProtectsAgainst,
		EdgeVerifiedBy,
		EdgeDescribedBy,
		EdgeDecidedBy,
		EdgeImplementsProtocol,
		EdgeParticipatesIn,
		EdgeDeclaresAnchorTarget,
	}
	EdgeSetEmpirical = []EdgeType{
		EdgeCoChangesWith,
	}
)

func CopyEdgeSet(values []EdgeType) []EdgeType {
	if len(values) == 0 {
		return nil
	}
	return append([]EdgeType(nil), values...)
}

func ConcatEdgeSets(sets ...[]EdgeType) []EdgeType {
	var out []EdgeType
	for _, set := range sets {
		out = append(out, set...)
	}
	return out
}

func DeduplicateEdgeTypes(values []EdgeType) []EdgeType {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[EdgeType]struct{}, len(values))
	out := make([]EdgeType, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func DefaultExpandEdgeTypes() []EdgeType {
	return CopyEdgeSet(EdgeSetStructural)
}

func AllEdgeTypes() []EdgeType {
	return DeduplicateEdgeTypes(ConcatEdgeSets(
		EdgeSetStructural,
		EdgeSetDoc,
		EdgeSetSemanticAnchors,
		EdgeSetEmpirical,
	))
}

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
	RepoRoot        string    `json:"repo_root"`
	HeadSHA         string    `json:"head_sha,omitempty"`
	WorktreeDirty   bool      `json:"worktree_dirty"`
	DirtyStatusHash string    `json:"dirty_status_hash,omitempty"`
	DefaultRef      string    `json:"default_ref,omitempty"`
	DefaultRefSHA   string    `json:"default_ref_sha,omitempty"`
	MergeBaseSHA    string    `json:"merge_base_sha,omitempty"`
	CommitsAhead    int       `json:"commits_ahead,omitempty"`
	CommitsBehind   int       `json:"commits_behind,omitempty"`
	SchemaVersion   int       `json:"schema_version"`
	IndexedAt       time.Time `json:"indexed_at"`
	Languages       []string  `json:"languages,omitempty"`
}

// FileState records the indexed content state for one repo-relative file.
type FileState struct {
	RepoKey         string    `json:"repo_key,omitempty"`
	Path            string    `json:"path"`
	ContentHash     string    `json:"content_hash"`
	SizeBytes       int64     `json:"size_bytes"`
	MTimeUnix       int64     `json:"mtime_unix"`
	Language        string    `json:"language,omitempty"`
	IndexedAt       time.Time `json:"indexed_at"`
	GitStatus       string    `json:"git_status,omitempty"`
	LastSeenHeadSHA string    `json:"last_seen_head_sha,omitempty"`
}

// WorkspaceDelta describes differences between the stored file_state table and
// the current workspace. It is the contract future partial builders consume.
type WorkspaceDelta struct {
	BaseHeadSHA     string   `json:"base_head_sha,omitempty"`
	CurrentHeadSHA  string   `json:"current_head_sha,omitempty"`
	DirtyStatusHash string   `json:"dirty_status_hash,omitempty"`
	Added           []string `json:"added,omitempty"`
	Modified        []string `json:"modified,omitempty"`
	Deleted         []string `json:"deleted,omitempty"`
	Untracked       []string `json:"untracked,omitempty"`
	Unchanged       int      `json:"unchanged,omitempty"`
}

// Empty reports whether no indexed file changed.
func (d WorkspaceDelta) Empty() bool {
	return len(d.Added) == 0 && len(d.Modified) == 0 && len(d.Deleted) == 0 && len(d.Untracked) == 0
}

// GitSnapshot captures commit state for freshness comparisons.
type GitSnapshot struct {
	HeadSHA         string `json:"head_sha,omitempty"`
	WorktreeDirty   bool   `json:"worktree_dirty"`
	DirtyStatusHash string `json:"dirty_status_hash,omitempty"`
	DefaultRef      string `json:"default_ref,omitempty"`
	DefaultRefSHA   string `json:"default_ref_sha,omitempty"`
	MergeBaseSHA    string `json:"merge_base_sha,omitempty"`
	CommitsAhead    int    `json:"commits_ahead,omitempty"`
	CommitsBehind   int    `json:"commits_behind,omitempty"`
}

// FreshnessLevel classifies how trustworthy an index is for current workspace context.
type FreshnessLevel string

const (
	FreshnessUnknown FreshnessLevel = "unknown"
	FreshnessCurrent FreshnessLevel = "current"
	FreshnessDirty   FreshnessLevel = "dirty"
	FreshnessBehind  FreshnessLevel = "behind"
	FreshnessStale   FreshnessLevel = "stale"
)

// IndexFreshnessStatus compares an indexed baseline against the current repo state.
type IndexFreshnessStatus struct {
	Level            FreshnessLevel `json:"level"`
	IndexHeadSHA     string         `json:"index_head_sha,omitempty"`
	CurrentHeadSHA   string         `json:"current_head_sha,omitempty"`
	IndexDirty       bool           `json:"index_worktree_dirty"`
	CurrentDirty     bool           `json:"current_worktree_dirty"`
	IndexDirtyHash   string         `json:"index_dirty_status_hash,omitempty"`
	CurrentDirtyHash string         `json:"current_dirty_status_hash,omitempty"`
	DefaultRef       string         `json:"default_ref,omitempty"`
	DefaultRefSHA    string         `json:"default_ref_sha,omitempty"`
	MergeBaseSHA     string         `json:"merge_base_sha,omitempty"`
	CommitsAhead     int            `json:"commits_ahead,omitempty"`
	CommitsBehind    int            `json:"commits_behind,omitempty"`
	Reasons          []string       `json:"reasons,omitempty"`
}

// Stats summarizes node and edge counts.
type Stats struct {
	NodesTotal  int              `json:"nodes_total"`
	EdgesTotal  int              `json:"edges_total"`
	NodesByKind map[NodeKind]int `json:"nodes_by_kind"`
}

// BuildOptions configure repoindex build behavior.
type BuildOptions struct {
	RepoRoot               string
	RepoKey                string
	Patterns               []string
	IncludeTests           bool
	IncludeGo              bool
	IncludePython          bool
	IncludeRust            bool
	IncludeCSharp          bool
	IncludeTypescript      bool
	IncludeElixir          bool
	IncludeTerraform       bool
	IncludeKubernetes      bool
	IncludeShell           bool
	IncludeSemanticAnchors bool
	IncludeCoChange        bool
	DryRun                 bool
	Progress               func(BuildProgress)
}

// BuildResult captures build statistics.
type BuildResult struct {
	Packages int `json:"packages"`
	Files    int `json:"files"`
	Symbols  int `json:"symbols"`
	Nodes    int `json:"nodes"`
	Edges    int `json:"edges"`
}

// DeltaBuildMode describes how an incremental build request was handled.
type DeltaBuildMode string

const (
	DeltaBuildModeNoop         DeltaBuildMode = "noop"
	DeltaBuildModeIncremental  DeltaBuildMode = "incremental"
	DeltaBuildModeFullFallback DeltaBuildMode = "full_fallback"
)

// BuildDeltaResult captures the result and handling mode for an incremental
// build request.
type BuildDeltaResult struct {
	Result       BuildResult    `json:"result"`
	Delta        WorkspaceDelta `json:"delta"`
	Mode         DeltaBuildMode `json:"mode"`
	Reason       string         `json:"reason,omitempty"`
	FullFallback bool           `json:"full_fallback,omitempty"`
}

// BuildProgress reports coarse repoindex build progress.
type BuildProgress struct {
	Phase     string      `json:"phase"`
	Message   string      `json:"message,omitempty"`
	ElapsedMs int64       `json:"elapsed_ms"`
	Time      time.Time   `json:"time"`
	Result    BuildResult `json:"result"`
}

// ExpandOptions controls graph expansion.
type ExpandOptions struct {
	Direction              Direction
	EdgeTypes              []EdgeType
	Depth                  int
	Budget                 int
	PerNodeCap             int
	IncludeSemanticAnchors bool
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
