package impact

import "context"

const (
	DefaultBaseRef      = "HEAD"
	DefaultDepth        = 2
	DefaultLimit        = 80
	DefaultPerTargetCap = 20
	DefaultMaxTargets   = 100
)

type TargetKind string

const (
	TargetFile     TargetKind = "file"
	TargetSymbol   TargetKind = "symbol"
	TargetPackage  TargetKind = "package"
	TargetContract TargetKind = "contract"
	TargetDiff     TargetKind = "diff"
)

type RefactorIntent string

const (
	IntentRename                    RefactorIntent = "rename"
	IntentMove                      RefactorIntent = "move"
	IntentDelete                    RefactorIntent = "delete"
	IntentConsolidate               RefactorIntent = "consolidate"
	IntentTypeTighten               RefactorIntent = "type_tighten"
	IntentAPIContractChange         RefactorIntent = "api_contract_change"
	IntentBehaviorPreservingCleanup RefactorIntent = "behavior_preserving_cleanup"
)

type LaneStatus string

const (
	LaneAvailable   LaneStatus = "available"
	LaneUnavailable LaneStatus = "unavailable"
)

type Bucket string

const (
	BucketMustUpdate       Bucket = "must_update"
	BucketShouldInspect    Bucket = "should_inspect"
	BucketLikelyDuplicate  Bucket = "likely_duplicate"
	BucketContractBoundary Bucket = "contract_boundary"
	BucketTestsToRun       Bucket = "tests_to_run"
	BucketDocsToUpdate     Bucket = "docs_to_update"
	BucketContextOnly      Bucket = "context_only"
)

type Source string

const (
	SourceExplicitTargets  Source = "explicit_targets"
	SourceGitDiff          Source = "git_diff"
	SourceRepoindexGraph   Source = "repoindex_graph"
	SourceSemanticNeighbor Source = "semantic_neighbors"
	SourceSearchIndex      Source = "searchindex_vector"
	SourceTurboVec         Source = "turbovec_vector"
)

type StructuralSection string

const (
	SectionDirectTarget  StructuralSection = "direct_target"
	SectionCaller        StructuralSection = "caller"
	SectionCallee        StructuralSection = "callee"
	SectionContainer     StructuralSection = "container"
	SectionChild         StructuralSection = "child"
	SectionImportRef     StructuralSection = "import_ref"
	SectionTest          StructuralSection = "test"
	SectionDoc           StructuralSection = "doc"
	SectionContract      StructuralSection = "contract_boundary"
	SectionCochange      StructuralSection = "cochange"
	SectionGraphNeighbor StructuralSection = "graph_neighbor"
)

type Input struct {
	Workspace    string         `json:"workspace,omitempty"`
	Targets      []Target       `json:"targets,omitempty"`
	Diff         *DiffInput     `json:"diff,omitempty"`
	Intent       RefactorIntent `json:"intent,omitempty"`
	Depth        int            `json:"depth,omitempty"`
	Limit        int            `json:"limit,omitempty"`
	PerTargetCap int            `json:"per_target_cap,omitempty"`
	MaxTargets   int            `json:"max_targets,omitempty"`
	IncludeTests bool           `json:"include_tests,omitempty"`
	IncludeDocs  bool           `json:"include_docs,omitempty"`
}

type DiffInput struct {
	BaseRef string `json:"base_ref,omitempty"`
	HeadRef string `json:"head_ref,omitempty"`
}

type Target struct {
	Kind        TargetKind `json:"kind"`
	Path        string     `json:"path,omitempty"`
	OldPath     string     `json:"old_path,omitempty"`
	Symbol      string     `json:"symbol,omitempty"`
	Package     string     `json:"package,omitempty"`
	Contract    string     `json:"contract,omitempty"`
	Status      string     `json:"status,omitempty"`
	Description string     `json:"description,omitempty"`
	Additions   int        `json:"additions,omitempty"`
	Deletions   int        `json:"deletions,omitempty"`
	IsDeleted   bool       `json:"is_deleted,omitempty"`
	IsTest      bool       `json:"is_test,omitempty"`
	Sources     []Source   `json:"sources,omitempty"`
}

type Change struct {
	Path        string `json:"path"`
	OldPath     string `json:"old_path,omitempty"`
	Status      string `json:"status"`
	Additions   int    `json:"additions,omitempty"`
	Deletions   int    `json:"deletions,omitempty"`
	Description string `json:"description,omitempty"`
}

type DiffProvider interface {
	ChangedFiles(ctx context.Context, in DiffInput) ([]Change, error)
}

type StructuralProvider interface {
	Candidates(ctx context.Context, targets []Target, opts StructuralOptions) (StructuralResult, error)
}

type StructuralOptions struct {
	Depth        int
	Limit        int
	PerTargetCap int
	Intent       RefactorIntent
	IncludeTests bool
	IncludeDocs  bool
}

type StructuralResult struct {
	Available  bool
	Reason     string
	Candidates []StructuralCandidate
}

type StructuralCandidate struct {
	Path        string            `json:"path"`
	Symbol      string            `json:"symbol,omitempty"`
	LineHint    int               `json:"line_hint,omitempty"`
	Depth       int               `json:"depth,omitempty"`
	EdgeTypes   []string          `json:"edge_types,omitempty"`
	Section     StructuralSection `json:"section,omitempty"`
	TargetKey   string            `json:"target_key,omitempty"`
	TargetLabel string            `json:"target,omitempty"`
	Description string            `json:"description,omitempty"`
}

type SemanticProvider interface {
	Neighbors(ctx context.Context, req SemanticNeighborRequest) (SemanticResult, error)
}

type SemanticNeighborRequest struct {
	WorkspaceRoot string
	WorkspaceID   string
	Targets       []Target
	ExcludePaths  []string
	Limit         int
	PerTargetCap  int
	MinScore      float64
}

type SemanticResult struct {
	Available  bool
	Reason     string
	Source     Source
	Candidates []SemanticCandidate
}

type SemanticCandidate struct {
	Path        string  `json:"path"`
	Symbol      string  `json:"symbol,omitempty"`
	LineHint    int     `json:"line_hint,omitempty"`
	Similarity  float64 `json:"similarity,omitempty"`
	Summary     string  `json:"summary,omitempty"`
	Source      Source  `json:"source,omitempty"`
	TargetKey   string  `json:"target_key,omitempty"`
	TargetLabel string  `json:"target,omitempty"`
}

type Providers struct {
	Diff       DiffProvider
	Structural StructuralProvider
	Semantic   SemanticProvider
}

type Lane struct {
	Name   Source     `json:"name"`
	Status LaneStatus `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type Summary struct {
	TargetCount           int `json:"target_count"`
	MustUpdateCount       int `json:"must_update_count"`
	ShouldInspectCount    int `json:"should_inspect_count"`
	LikelyDuplicateCount  int `json:"likely_duplicate_count"`
	ContractBoundaryCount int `json:"contract_boundary_count"`
	TestsToRunCount       int `json:"tests_to_run_count"`
	DocsToUpdateCount     int `json:"docs_to_update_count"`
	ContextOnlyCount      int `json:"context_only_count"`
}

type TargetRelationship struct {
	TargetKey string            `json:"target_key,omitempty"`
	Target    string            `json:"target,omitempty"`
	Section   StructuralSection `json:"section,omitempty"`
	Depth     int               `json:"depth,omitempty"`
	EdgeTypes []string          `json:"edge_types,omitempty"`
}

type Candidate struct {
	Path                string               `json:"path"`
	Symbols             []string             `json:"symbols,omitempty"`
	LineHints           []int                `json:"line_hints,omitempty"`
	Rank                Bucket               `json:"rank"`
	Score               int                  `json:"score"`
	Sources             []Source             `json:"sources"`
	Reasons             []string             `json:"reasons"`
	TargetRelationships []TargetRelationship `json:"target_relationships,omitempty"`
	Summary             string               `json:"summary,omitempty"`
}

type Packet struct {
	Workspace        string      `json:"workspace,omitempty"`
	Intent           string      `json:"intent"`
	Summary          Summary     `json:"summary"`
	Targets          []Target    `json:"targets"`
	Lanes            []Lane      `json:"lanes"`
	MustUpdate       []Candidate `json:"must_update"`
	ShouldInspect    []Candidate `json:"should_inspect"`
	LikelyDuplicate  []Candidate `json:"likely_duplicate"`
	ContractBoundary []Candidate `json:"contract_boundary"`
	TestsToRun       []Candidate `json:"tests_to_run"`
	DocsToUpdate     []Candidate `json:"docs_to_update"`
	ContextOnly      []Candidate `json:"context_only"`
}
