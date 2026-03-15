package contextplane

import "time"

// RecentDecision captures a bounded decision item in top-of-mind.
type RecentDecision struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
}

// TopOfMind is the derived orientation bundle for the current workspace frontier.
type TopOfMind struct {
	WorkspaceID     string           `json:"workspace_id"`
	Objective       string           `json:"objective"`
	Phase           string           `json:"phase"`
	ActiveTaskIDs   []string         `json:"active_task_ids,omitempty"`
	HardConstraints []string         `json:"hard_constraints,omitempty"`
	Blockers        []string         `json:"blockers,omitempty"`
	RecentDecisions []RecentDecision `json:"recent_decisions,omitempty"`
	OpenLoops       []string         `json:"open_loops,omitempty"`
	NextActions     []string         `json:"next_actions,omitempty"`
	RelevantRefs    []string         `json:"relevant_refs,omitempty"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// Handoff captures the compact output of a bounded work phase.
type Handoff struct {
	TaskID              string    `json:"task_id"`
	Phase               string    `json:"phase"`
	Outcome             string    `json:"outcome"`
	Summary             string    `json:"summary"`
	EvidenceRefs        []string  `json:"evidence_refs,omitempty"`
	FilesTouched        []string  `json:"files_touched,omitempty"`
	Observations        []string  `json:"observations,omitempty"`
	Tensions            []string  `json:"tensions,omitempty"`
	NextActions         []string  `json:"next_actions,omitempty"`
	PromotionCandidates []string  `json:"promotion_candidates,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

// Observation records a repeatable system learning.
type Observation struct {
	ID           string    `json:"id"`
	Statement    string    `json:"statement"`
	Confidence   float64   `json:"confidence"`
	Count        int       `json:"count"`
	Project      string    `json:"project,omitempty"`
	Area         string    `json:"area,omitempty"`
	EvidenceRefs []string  `json:"evidence_refs,omitempty"`
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
}

// Tension records a contradiction or drag source.
type Tension struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Statement   string    `json:"statement"`
	Impact      string    `json:"impact"`
	RelatedRefs []string  `json:"related_refs,omitempty"`
	Status      string    `json:"status"`
	Count       int       `json:"count,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
}

// HandoffRecord wraps a handoff with its persisted path.
type HandoffRecord struct {
	Path    string  `json:"path"`
	Handoff Handoff `json:"handoff"`
}

// PromotionJob records a draft promotion event.
type PromotionJob struct {
	ID         string    `json:"id"`
	SourceRef  string    `json:"source_ref"`
	SourceKind string    `json:"source_kind"`
	NoteType   string    `json:"note_type"`
	Title      string    `json:"title"`
	DraftPath  string    `json:"draft_path"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// PromotionDraftResult describes a generated draft note.
type PromotionDraftResult struct {
	DraftPath string       `json:"draft_path"`
	Job       PromotionJob `json:"job"`
}

// PromotionMergeResult describes an explicit reviewed merge of a draft into the knowledge plane.
type PromotionMergeResult struct {
	DraftPath   string       `json:"draft_path"`
	TargetPath  string       `json:"target_path"`
	Heading     string       `json:"heading,omitempty"`
	MergedAs    string       `json:"merged_as"`
	Job         PromotionJob `json:"job"`
	MergedAtUTC time.Time    `json:"merged_at_utc"`
}

// Report is a synthesized current-view projection of the control plane.
type Report struct {
	WorkspaceID        string         `json:"workspace_id"`
	Objective          string         `json:"objective"`
	Phase              string         `json:"phase"`
	ActiveTaskIDs      []string       `json:"active_task_ids,omitempty"`
	LatestHandoff      *HandoffRecord `json:"latest_handoff,omitempty"`
	TopObservations    []Observation  `json:"top_observations,omitempty"`
	OpenTensions       []Tension      `json:"open_tensions,omitempty"`
	RecommendedActions []string       `json:"recommended_actions,omitempty"`
	GeneratedAt        time.Time      `json:"generated_at"`
}

// MaintenanceTask captures a generated maintenance action derived from control-plane drift.
type MaintenanceTask struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Priority   int       `json:"priority"`
	Reason     string    `json:"reason"`
	SourceRefs []string  `json:"source_refs,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// RetrievalCorrectionRun records one persisted ACA retrieval correction report.
type RetrievalCorrectionRun struct {
	ID              string                          `json:"id"`
	Suite           string                          `json:"suite"`
	ControlSuite    string                          `json:"control_suite,omitempty"`
	ArtifactDigest  string                          `json:"artifact_digest"`
	Summary         RetrievalInspectionBatchSummary `json:"summary"`
	PolicyCandidate bool                            `json:"policy_candidate"`
	PolicyApplied   bool                            `json:"policy_applied"`
	PolicyAccepted  bool                            `json:"policy_accepted"`
	PolicyReverted  bool                            `json:"policy_reverted"`
	DraftCount      int                             `json:"draft_count"`
	CreatedAt       time.Time                       `json:"created_at"`
}

// GraphCorrectionRun records one persisted repoindex search or DAG correction report.
type GraphCorrectionRun struct {
	ID             string    `json:"id"`
	Method         string    `json:"method"`
	Suite          string    `json:"suite"`
	ArtifactDigest string    `json:"artifact_digest"`
	Queries        int       `json:"queries"`
	Matched        int       `json:"matched"`
	Misses         int       `json:"misses"`
	Classification string    `json:"classification,omitempty"`
	RecommendedFix string    `json:"recommended_fix,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// RetrievalHit is one ranked durable-knowledge candidate.
type RetrievalHit struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Type      string   `json:"type,omitempty"`
	Trust     string   `json:"trust,omitempty"`
	Score     int      `json:"score"`
	Snippet   string   `json:"snippet,omitempty"`
	RepoPaths []string `json:"repo_paths,omitempty"`
	Symbols   []string `json:"symbols,omitempty"`
}

// RetrievalWeights documents the scoring inputs for blended retrieval.
type RetrievalWeights struct {
	BaseIndexScore int `json:"base_index_score"`
	ADR            int `json:"adr"`
	Pattern        int `json:"pattern"`
	Incident       int `json:"incident"`
	Investigation  int `json:"investigation"`
	Map            int `json:"map"`
	Canonical      int `json:"canonical"`
	Reviewed       int `json:"reviewed"`
	Raw            int `json:"raw"`
	RelevantRef    int `json:"relevant_ref"`
	HandoffRef     int `json:"handoff_ref"`
	CodePath       int `json:"code_path"`
	CodeSymbol     int `json:"code_symbol"`
	SemanticMatch  int `json:"semantic_match"`
}

// RetrievalOptions controls which ACA retrieval components participate in one query.
// The default path should remain behavior-compatible with existing Retrieve().
type RetrievalOptions struct {
	IncludeTopOfMindResult  bool     `json:"include_top_of_mind_result"`
	IncludeLatestHandoff    bool     `json:"include_latest_handoff"`
	IncludeVaultHits        bool     `json:"include_vault_hits"`
	UseRelevantRefBoost     bool     `json:"use_relevant_ref_boost"`
	UseHandoffRefBoost      bool     `json:"use_handoff_ref_boost"`
	UseCodeHints            bool     `json:"use_code_hints"`
	UseSemanticVaultSearch  bool     `json:"use_semantic_vault_search"`
	UsePackageNoteFallback  bool     `json:"use_package_note_fallback"`
	UseQueryTypeBias        bool     `json:"use_query_type_bias"`
	AllowedTrusts           []string `json:"allowed_trusts,omitempty"`
	IncludeControlPlaneRefs bool     `json:"include_control_plane_refs"`
}

// RetrievalResult blends control-plane state with ranked vault hits.
type RetrievalResult struct {
	WorkspaceID   string           `json:"workspace_id"`
	Query         string           `json:"query"`
	TopOfMind     *TopOfMind       `json:"top_of_mind,omitempty"`
	LatestHandoff *HandoffRecord   `json:"latest_handoff,omitempty"`
	Observations  []Observation    `json:"observations,omitempty"`
	Tensions      []Tension        `json:"tensions,omitempty"`
	VaultHits     []RetrievalHit   `json:"vault_hits,omitempty"`
	Weights       RetrievalWeights `json:"weights"`
	SemanticModel string           `json:"semantic_model,omitempty"`
	SemanticUsed  bool             `json:"semantic_used"`
	GeneratedAt   time.Time        `json:"generated_at"`
}

// ContradictionFinding links an open tension to potentially conflicting or relevant durable notes.
type ContradictionFinding struct {
	Tension          Tension        `json:"tension"`
	Query            string         `json:"query"`
	SupportingNotes  []RetrievalHit `json:"supporting_notes,omitempty"`
	BlockedPromotion bool           `json:"blocked_promotion"`
}

// TaskCandidate is a selected task from the workspace task store.
type TaskCandidate struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Status      string   `json:"status"`
	ScopePath   string   `json:"scope_path,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	PlanFile    string   `json:"plan_file,omitempty"`
	PlanSection string   `json:"plan_section,omitempty"`
	SessionID   string   `json:"session_id,omitempty"`
}

// TaskPacket is the bounded dispatch payload for a worker phase.
type TaskPacket struct {
	WorkspaceID     string           `json:"workspace_id"`
	Task            TaskCandidate    `json:"task"`
	Objective       string           `json:"objective"`
	Phase           string           `json:"phase"`
	HardConstraints []string         `json:"hard_constraints,omitempty"`
	Blockers        []string         `json:"blockers,omitempty"`
	RecentDecisions []RecentDecision `json:"recent_decisions,omitempty"`
	NextActions     []string         `json:"next_actions,omitempty"`
	RelevantRefs    []string         `json:"relevant_refs,omitempty"`
	LatestHandoff   *HandoffRecord   `json:"latest_handoff,omitempty"`
	GeneratedAt     time.Time        `json:"generated_at"`
}

// Layout describes the workspace-local ACA runtime scaffold.
type Layout struct {
	WorkspacePath          string
	RootDir                string
	RuntimeDir             string
	QueueDir               string
	HandoffsDir            string
	SessionsDir            string
	PolicyDir              string
	ExportsDir             string
	TemplatesDir           string
	TopOfMindPath          string
	CurrentRunPath         string
	TasksQueuePath         string
	BlockedQueuePath       string
	ObservationsPath       string
	TensionsPath           string
	PromotionJobsPath      string
	MaintenanceQueuePath   string
	EventsPath             string
	RetrievalPolicyPath    string
	PromotionPolicyPath    string
	TaskTypesPolicyPath    string
	OrientationExportPath  string
	ObsidianHomeIndexPath  string
	ObsidianFrontierPath   string
	ObsidianAtlasPath      string
	ObsidianProjectMOCPath string
	ObsidianInboxDraftsDir string
}
