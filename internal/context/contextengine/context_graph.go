package contextengine

// ContextGraphDirection controls dependency graph traversal around selected roots.
type ContextGraphDirection string

const (
	ContextGraphDirectionBoth ContextGraphDirection = "both"
	ContextGraphDirectionOut  ContextGraphDirection = "out"
	ContextGraphDirectionIn   ContextGraphDirection = "in"
)

// ContextGraphRequest describes a bounded graph expansion around already-selected evidence roots.
type ContextGraphRequest struct {
	WorkspaceID          string                `json:"workspace_id,omitempty"`
	Query                string                `json:"query,omitempty"`
	TaskType             string                `json:"task_type,omitempty"`
	Roots                []EvidenceRef         `json:"roots,omitempty"`
	RootPaths            []string              `json:"root_paths,omitempty"`
	SourceProfiles       []SourceProfile       `json:"source_profiles,omitempty"`
	CoverageRequirements []CoverageRequirement `json:"coverage_requirements,omitempty"`
	Depth                int                   `json:"depth,omitempty"`
	Direction            ContextGraphDirection `json:"direction,omitempty"`
	IncludeTests         bool                  `json:"include_tests,omitempty"`
	IncludeAdjacent      bool                  `json:"include_adjacent,omitempty"`
	PathPrefixes         []string              `json:"path_prefixes,omitempty"`
	ExcludedPaths        []string              `json:"excluded_paths,omitempty"`
	Budget               ContextGraphBudget    `json:"budget,omitempty"`
}

// ContextGraphBudget bounds graph expansion and fallback local scans.
type ContextGraphBudget struct {
	MaxRoots      int   `json:"max_roots,omitempty"`
	MaxNodes      int   `json:"max_nodes,omitempty"`
	MaxEdges      int   `json:"max_edges,omitempty"`
	MaxDepth      int   `json:"max_depth,omitempty"`
	PerNodeCap    int   `json:"per_node_cap,omitempty"`
	MaxLocalFiles int   `json:"max_local_files,omitempty"`
	MaxLocalBytes int64 `json:"max_local_bytes,omitempty"`
	MaxDurationMs int   `json:"max_duration_ms,omitempty"`
}

// GraphGrounding explains how a graph node or edge was observed.
type GraphGrounding struct {
	Source     string  `json:"source,omitempty"`
	Method     string  `json:"method,omitempty"`
	Static     bool    `json:"static,omitempty"`
	Heuristic  bool    `json:"heuristic,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// ContextGraphNode is a compact graph node suitable for model-facing context.
type ContextGraphNode struct {
	ID          string         `json:"id"`
	Path        string         `json:"path,omitempty"`
	Symbol      string         `json:"symbol,omitempty"`
	Kind        string         `json:"kind,omitempty"`
	Role        string         `json:"role,omitempty"`
	Language    string         `json:"language,omitempty"`
	Ref         EvidenceRef    `json:"ref"`
	LoadRef     string         `json:"load_ref,omitempty"`
	Confidence  float64        `json:"confidence,omitempty"`
	Grounding   GraphGrounding `json:"grounding,omitempty"`
	Distance    int            `json:"distance,omitempty"`
	EvidenceIDs []string       `json:"evidence_ids,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// ContextGraphEdge is a compact, grounded relationship between graph nodes.
type ContextGraphEdge struct {
	From       string         `json:"from"`
	To         string         `json:"to"`
	Type       string         `json:"type"`
	Grounding  GraphGrounding `json:"grounding,omitempty"`
	Confidence float64        `json:"confidence,omitempty"`
	Distance   int            `json:"distance,omitempty"`
	Refs       []EvidenceRef  `json:"refs,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// ContextGraphGap records why the graph should be treated as partial.
type ContextGraphGap struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Severity string   `json:"severity,omitempty"`
	Message  string   `json:"message,omitempty"`
	Roots    []string `json:"roots,omitempty"`
}

// ContextGraphConfidence decomposes completeness/trust for the graph result.
type ContextGraphConfidence struct {
	Overall            float64 `json:"overall"`
	Completeness       string  `json:"completeness"`
	RootResolution     float64 `json:"root_resolution"`
	CandidateAdmission float64 `json:"candidate_admission,omitempty"`
	ReductionCoverage  float64 `json:"reduction_coverage,omitempty"`
	GraphCoverage      float64 `json:"graph_coverage"`
	EdgeGrounding      float64 `json:"edge_grounding"`
	Loadability        float64 `json:"loadability"`
	Freshness          float64 `json:"freshness"`
	SourceDiversity    float64 `json:"source_diversity,omitempty"`
	ConflictCount      int     `json:"conflict_count,omitempty"`
	TrustedForProceed  bool    `json:"trusted_for_proceed"`
}

// ContextGraphReport is the graph/certification surface returned by expand_context_graph.
type ContextGraphReport struct {
	ID          string                 `json:"id"`
	WorkspaceID string                 `json:"workspace_id,omitempty"`
	Query       string                 `json:"query,omitempty"`
	Roots       []ContextGraphNode     `json:"roots"`
	Nodes       []ContextGraphNode     `json:"nodes"`
	Edges       []ContextGraphEdge     `json:"edges"`
	Missing     []ContextGraphGap      `json:"missing,omitempty"`
	Confidence  ContextGraphConfidence `json:"confidence"`
	Telemetry   EvidenceTelemetry      `json:"telemetry,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
}
