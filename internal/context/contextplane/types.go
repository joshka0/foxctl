package contextplane

import (
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/intelligence/evidence"
)

// PolicyKind is a typed enum for MemoryProposal.Kind.
type PolicyKind string

const (
	PolicyKindRetrievalPatch      PolicyKind = "retrieval_policy_patch"
	PolicyKindExternalImport      PolicyKind = "external_evidence_import"
	PolicyKindMethodologyDraft    PolicyKind = "methodology_draft"
	PolicyKindContradictionNote   PolicyKind = "contradiction_note"
	PolicyKindObservationPromote  PolicyKind = "observation_promote"
	PolicyKindTensionResolve      PolicyKind = "tension_resolve"
	PolicyKindMemoryDraft         PolicyKind = "memory_draft"
	PolicyKindSemanticAnchorPatch PolicyKind = "semantic_anchor_patch"
)

// IsValid reports whether k is a known PolicyKind.
func (k PolicyKind) IsValid() bool {
	switch k {
	case PolicyKindRetrievalPatch, PolicyKindExternalImport, PolicyKindMethodologyDraft,
		PolicyKindContradictionNote, PolicyKindObservationPromote, PolicyKindTensionResolve,
		PolicyKindMemoryDraft, PolicyKindSemanticAnchorPatch:
		return true
	default:
		return false
	}
}

// ParsePolicyKind parses a string into a PolicyKind. Returns error for unknown kinds.
func ParsePolicyKind(s string) (PolicyKind, error) {
	k := PolicyKind(s)
	if !k.IsValid() {
		return "", fmt.Errorf("unknown policy kind: %q", s)
	}
	return k, nil
}

// RecentDecision captures a bounded decision item in top-of-mind.
type RecentDecision struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Ref  string `json:"ref,omitempty"`
}

// TopOfMind is the derived orientation bundle for the current workspace frontier.
type TopOfMind struct {
	WorkspaceID         string                        `json:"workspace_id"`
	Objective           string                        `json:"objective"`
	Phase               string                        `json:"phase"`
	ActiveTaskIDs       []string                      `json:"active_task_ids,omitempty"`
	HardConstraints     []string                      `json:"hard_constraints,omitempty"`
	Blockers            []string                      `json:"blockers,omitempty"`
	RecentDecisions     []RecentDecision              `json:"recent_decisions,omitempty"`
	OpenLoops           []string                      `json:"open_loops,omitempty"`
	NextActions         []string                      `json:"next_actions,omitempty"`
	RelevantRefs        []contextengine.EvidenceRef   `json:"relevant_refs,omitempty"`
	ProjectionMeta      *contextengine.ProjectionMeta `json:"projection_meta,omitempty"`
	StaleWarnings       []string                      `json:"stale_warnings,omitempty"`
	KnownGaps           []string                      `json:"known_gaps,omitempty"`
	GeneratedFromEvents []string                      `json:"generated_from_events,omitempty"`
	UpdatedAt           time.Time                     `json:"updated_at"`
}

// Handoff captures the compact output of a bounded work phase.
type Handoff struct {
	TaskID              string                      `json:"task_id"`
	Phase               string                      `json:"phase"`
	Outcome             string                      `json:"outcome"`
	Summary             string                      `json:"summary"`
	EvidenceRefs        []contextengine.EvidenceRef `json:"evidence_refs,omitempty"`
	FileRefs            []contextengine.EvidenceRef `json:"file_refs,omitempty"`
	Observations        []string                    `json:"observations,omitempty"`
	Tensions            []string                    `json:"tensions,omitempty"`
	NextActions         []string                    `json:"next_actions,omitempty"`
	PromotionCandidates []string                    `json:"promotion_candidates,omitempty"`
	CreatedAt           time.Time                   `json:"created_at"`
}

// Observation records a repeatable system learning.
type Observation struct {
	ID           string                      `json:"id"`
	Statement    string                      `json:"statement"`
	Confidence   float64                     `json:"confidence"`
	Count        int                         `json:"count"`
	Project      string                      `json:"project,omitempty"`
	Area         string                      `json:"area,omitempty"`
	EvidenceRefs []contextengine.EvidenceRef `json:"evidence_refs,omitempty"`
	FirstSeen    time.Time                   `json:"first_seen"`
	LastSeen     time.Time                   `json:"last_seen"`
}

// Tension records a contradiction or drag source.
type Tension struct {
	ID          string                      `json:"id"`
	Kind        string                      `json:"kind"`
	Statement   string                      `json:"statement"`
	Impact      string                      `json:"impact"`
	RelatedRefs []contextengine.EvidenceRef `json:"related_refs,omitempty"`
	Status      string                      `json:"status"`
	Count       int                         `json:"count,omitempty"`
	CreatedAt   time.Time                   `json:"created_at"`
	LastSeen    time.Time                   `json:"last_seen,omitempty"`
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
	ID         string              `json:"id"`
	Title      string              `json:"title"`
	Kind       string              `json:"kind"`
	Priority   int                 `json:"priority"`
	Reason     string              `json:"reason"`
	SourceRefs []string            `json:"source_refs,omitempty"`
	WorkPacket *ProposalWorkPacket `json:"work_packet,omitempty"`
	Status     string              `json:"status"`
	CreatedAt  time.Time           `json:"created_at"`
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

// MemoryProposal records one typed, deduped suggestion for evolving ACA memory state.
type MemoryProposal struct {
	ID               string                      `json:"id"`
	DedupeKey        string                      `json:"dedupe_key,omitempty"`
	Kind             PolicyKind                  `json:"kind"`
	Classification   string                      `json:"classification,omitempty"`
	Status           string                      `json:"status"`
	ReviewRequired   bool                        `json:"review_required"`
	Confidence       float64                     `json:"confidence"`
	BlastRadius      string                      `json:"blast_radius,omitempty"`
	Summary          string                      `json:"summary"`
	SourceRefs       []contextengine.EvidenceRef `json:"source_refs,omitempty"`
	ProposedChange   map[string]any              `json:"proposed_change,omitempty"`
	EvaluationStatus string                      `json:"evaluation_status,omitempty"`
	ApplyStatus      string                      `json:"apply_status,omitempty"`
	Count            int                         `json:"count"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

// EvidenceImportRun records one external-evidence intake into the ACA inbox.
type EvidenceImportRun struct {
	ID             string    `json:"id"`
	SourceKind     string    `json:"source_kind"`
	SourceRef      string    `json:"source_ref"`
	Title          string    `json:"title"`
	DraftPath      string    `json:"draft_path"`
	ArtifactDigest string    `json:"artifact_digest,omitempty"`
	ProcessorKind  string    `json:"processor_kind,omitempty"`
	ProcessorModel string    `json:"processor_model,omitempty"`
	Summary        string    `json:"summary"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

// ProposalWorkPacket is a stable machine-readable next-action packet for agents/hooks.
type ProposalWorkPacket struct {
	ProposalID        string `json:"proposal_id"`
	ProposalKind      string `json:"proposal_kind"`
	Action            string `json:"action"`
	Status            string `json:"status"`
	ReviewRequired    bool   `json:"review_required"`
	DraftPath         string `json:"draft_path,omitempty"`
	TargetPath        string `json:"target_path,omitempty"`
	Heading           string `json:"heading,omitempty"`
	PolicyPath        string `json:"policy_path,omitempty"`
	PromotionJobID    string `json:"promotion_job_id,omitempty"`
	RequiresVaultPath bool   `json:"requires_vault_path,omitempty"`
	VaultPath         string `json:"vault_path,omitempty"`
	NextCommand       string `json:"next_command,omitempty"`
}

// RetrievalHit is one ranked durable-knowledge candidate.
type RetrievalHit struct {
	Path              string              `json:"path"`
	Title             string              `json:"title"`
	Type              string              `json:"type,omitempty"`
	Trust             string              `json:"trust,omitempty"`
	Score             int                 `json:"score"`
	Snippet           string              `json:"snippet,omitempty"`
	PrimaryAnchorPath string              `json:"primary_anchor_path,omitempty"`
	RepoPaths         []string            `json:"repo_paths,omitempty"`
	AnchorPaths       []string            `json:"anchor_paths,omitempty"`
	AnchorRoles       map[string][]string `json:"anchor_roles,omitempty"`
	Symbols           []string            `json:"symbols,omitempty"`
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
	RepoMotif      int `json:"repo_motif"`
	CoChange       int `json:"co_change"`
	SemanticMatch  int `json:"semantic_match"`
}

// RetrievalOptions controls which ACA retrieval components participate in one query.
// The default path should remain behavior-compatible with existing Retrieve().
type RetrievalOptions struct {
	IncludeTopOfMindResult    bool     `json:"include_top_of_mind_result"`
	IncludeLatestHandoff      bool     `json:"include_latest_handoff"`
	IncludeVaultHits          bool     `json:"include_vault_hits"`
	UseRelevantRefBoost       bool     `json:"use_relevant_ref_boost"`
	UseHandoffRefBoost        bool     `json:"use_handoff_ref_boost"`
	UseCodeHints              bool     `json:"use_code_hints"`
	UseSemanticAnchors        bool     `json:"use_semantic_anchors"`
	UseSemanticVaultSearch    bool     `json:"use_semantic_vault_search"`
	UsePackageNoteFallback    bool     `json:"use_package_note_fallback"`
	UseRepoMotifPrior         bool     `json:"use_repo_motif_prior"`
	UseCoChangePrior          bool     `json:"use_co_change_prior"`
	CoChangeCommitLimit       int      `json:"co_change_commit_limit,omitempty"`
	CoChangeMaxFilesPerCommit int      `json:"co_change_max_files_per_commit,omitempty"`
	CoChangeHalfLifeDays      int      `json:"co_change_half_life_days,omitempty"`
	UseContinuityBundles      bool     `json:"use_continuity_bundles,omitempty"`
	UseQueryTypeBias          bool     `json:"use_query_type_bias"`
	AllowedTrusts             []string `json:"allowed_trusts,omitempty"`
	IncludeControlPlaneRefs   bool     `json:"include_control_plane_refs"`
}

type SemanticAnchorRetrievalHints struct {
	Paths    []string                          `json:"paths,omitempty"`
	Symbols  []string                          `json:"symbols,omitempty"`
	Targets  []string                          `json:"targets,omitempty"`
	Evidence []SemanticAnchorRetrievalEvidence `json:"evidence,omitempty"`
	Warnings []SemanticAnchorRetrievalWarning  `json:"warnings,omitempty"`
}

type SemanticAnchorRetrievalEvidence struct {
	EdgeID        string                `json:"edge_id"`
	OwnerNodeID   string                `json:"owner_node_id"`
	OwnerPath     string                `json:"owner_path,omitempty"`
	OwnerSymbol   string                `json:"owner_symbol,omitempty"`
	Relation      string                `json:"relation"`
	TargetID      string                `json:"target_id"`
	TargetDisplay string                `json:"target_display,omitempty"`
	EvidenceMeta  evidence.EvidenceMeta `json:"evidence_meta"`
}

type SemanticAnchorRetrievalWarning struct {
	EdgeID  string `json:"edge_id,omitempty"`
	Reason  string `json:"reason"`
	Message string `json:"message,omitempty"`
}

// RetrievalResult blends control-plane state with ranked vault hits.
type RetrievalResult struct {
	WorkspaceID         string                        `json:"workspace_id"`
	Query               string                        `json:"query"`
	TopOfMind           *TopOfMind                    `json:"top_of_mind,omitempty"`
	LatestHandoff       *HandoffRecord                `json:"latest_handoff,omitempty"`
	Observations        []Observation                 `json:"observations,omitempty"`
	Tensions            []Tension                     `json:"tensions,omitempty"`
	VaultHits           []RetrievalHit                `json:"vault_hits,omitempty"`
	RepoMotifHits       []RepoMotifSearchHit          `json:"repo_motif_hits,omitempty"`
	SemanticAnchorHints *SemanticAnchorRetrievalHints `json:"semantic_anchor_hints,omitempty"`
	Weights             RetrievalWeights              `json:"weights"`
	SemanticModel       string                        `json:"semantic_model,omitempty"`
	SemanticUsed        bool                          `json:"semantic_used"`
	GeneratedAt         time.Time                     `json:"generated_at"`
}

// ToEvidencePack converts the RetrievalResult into a canonical EvidencePack.
// All observations, tensions, and vault hits are converted to EvidenceNodes.
func (r *RetrievalResult) ToEvidencePack() contextengine.EvidencePack {
	nodes := make([]contextengine.EvidenceNode, 0)
	for _, obs := range r.Observations {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypeNote, Ref: obs.ID}
		if len(obs.EvidenceRefs) > 0 {
			ref = obs.EvidenceRefs[0]
		}
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          obs.ID,
			WorkspaceID: r.WorkspaceID,
			NodeType:    contextengine.EvidenceNodeTypeObservation,
			Ref:         ref,
			Statement:   obs.Statement,
			Confidence:  obs.Confidence,
			Count:       obs.Count,
			FirstSeen:   obs.FirstSeen,
			LastSeen:    obs.LastSeen,
			Grounding:   contextengine.GroundingValidated,
			Metadata: map[string]any{
				"project": obs.Project,
				"area":    obs.Area,
			},
		})
	}
	for _, t := range r.Tensions {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypeNote, Ref: t.ID}
		if len(t.RelatedRefs) > 0 {
			ref = t.RelatedRefs[0]
		}
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          t.ID,
			WorkspaceID: r.WorkspaceID,
			NodeType:    contextengine.EvidenceNodeTypeTension,
			Ref:         ref,
			Statement:   t.Statement,
			Count:       t.Count,
			FirstSeen:   t.CreatedAt,
			LastSeen:    t.LastSeen,
			Metadata: map[string]any{
				"kind":   t.Kind,
				"impact": t.Impact,
				"status": t.Status,
			},
		})
	}
	for _, hit := range r.VaultHits {
		ref := contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: hit.Path}
		if hit.PrimaryAnchorPath != "" {
			ref = contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: hit.PrimaryAnchorPath}
		}
		nodes = append(nodes, contextengine.EvidenceNode{
			ID:          fmt.Sprintf("hit_%s_%s", r.WorkspaceID, hit.Path),
			WorkspaceID: r.WorkspaceID,
			NodeType:    contextengine.EvidenceNodeTypeRetrieval,
			Ref:         ref,
			Statement:   hit.Snippet,
			Confidence:  float64(hit.Score) / 100.0,
			Grounding:   contextengine.GroundingIndexed,
			Metadata: map[string]any{
				"title":               hit.Title,
				"type":                hit.Type,
				"trust":               hit.Trust,
				"score":               hit.Score,
				"primary_anchor_path": hit.PrimaryAnchorPath,
				"repo_paths":          hit.RepoPaths,
				"anchor_paths":        hit.AnchorPaths,
				"anchor_roles":        hit.AnchorRoles,
				"symbols":             hit.Symbols,
			},
		})
	}
	if r.SemanticAnchorHints != nil {
		for _, hint := range r.SemanticAnchorHints.Evidence {
			if err := evidence.ValidateRenderSurface(hint.EvidenceMeta, evidence.RenderSurfaceEvidenceHint); err != nil {
				continue
			}
			ref := contextengine.EvidenceRef{Type: contextengine.RefTypeSymbol, Ref: hint.OwnerNodeID}
			if hint.OwnerPath != "" {
				ref = contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: hint.OwnerPath}
			}
			nodes = append(nodes, contextengine.EvidenceNode{
				ID:          "semantic_anchor_" + hint.EdgeID,
				WorkspaceID: r.WorkspaceID,
				NodeType:    contextengine.EvidenceNodeTypeCode,
				Ref:         ref,
				Statement:   fmt.Sprintf("%s %s %s", hint.OwnerSymbol, hint.Relation, hint.TargetDisplay),
				Confidence:  0.7,
				Grounding:   contextengine.GroundingValidated,
				Metadata: map[string]any{
					"edge_id":        hint.EdgeID,
					"owner_node_id":  hint.OwnerNodeID,
					"owner_path":     hint.OwnerPath,
					"owner_symbol":   hint.OwnerSymbol,
					"relation":       hint.Relation,
					"target_id":      hint.TargetID,
					"target_display": hint.TargetDisplay,
					"evidence_meta":  hint.EvidenceMeta,
				},
			})
		}
	}
	return contextengine.EvidencePack{
		ID:          fmt.Sprintf("retrieval_%s_%d", r.WorkspaceID, r.GeneratedAt.Unix()),
		WorkspaceID: r.WorkspaceID,
		Query:       r.Query,
		Lane:        contextengine.LaneContext,
		Nodes:       nodes,
		Telemetry: contextengine.EvidenceTelemetry{
			TokensUsed: r.Weights.BaseIndexScore,
		},
		Metadata: map[string]any{
			"semantic_model": r.SemanticModel,
			"semantic_used":  r.SemanticUsed,
			"generated_at":   r.GeneratedAt,
		},
	}
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
	WorkspaceID     string                      `json:"workspace_id"`
	Task            TaskCandidate               `json:"task"`
	Objective       string                      `json:"objective"`
	Phase           string                      `json:"phase"`
	HardConstraints []string                    `json:"hard_constraints,omitempty"`
	Blockers        []string                    `json:"blockers,omitempty"`
	RecentDecisions []RecentDecision            `json:"recent_decisions,omitempty"`
	NextActions     []string                    `json:"next_actions,omitempty"`
	RelevantRefs    []contextengine.EvidenceRef `json:"relevant_refs,omitempty"`
	LatestHandoff   *HandoffRecord              `json:"latest_handoff,omitempty"`
	GeneratedAt     time.Time                   `json:"generated_at"`
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

// FilesTouched returns the file paths from FileRefs as plain strings.
// This is a convenience accessor for the renamed FileRefs field.
func (h Handoff) FilesTouched() []string {
	return EvidenceRefsToStrings(h.FileRefs)
}

// EvidenceRefsToStrings converts []EvidenceRef to []string using FormatEvidenceRef.
func EvidenceRefsToStrings(refs []contextengine.EvidenceRef) []string {
	if len(refs) == 0 {
		return nil
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if s := contextengine.FormatEvidenceRef(ref); s != "" {
			out = append(out, s)
		} else {
			out = append(out, ref.Ref)
		}
	}
	return out
}

// StringsToEvidenceRefs converts []string to []EvidenceRef.
// Each string is parsed as "type:value"; bare values get RefTypePath.
func StringsToEvidenceRefs(items []string) []contextengine.EvidenceRef {
	if len(items) == 0 {
		return nil
	}
	out := make([]contextengine.EvidenceRef, 0, len(items))
	for _, s := range items {
		if s == "" {
			continue
		}
		ref, err := contextengine.ParseEvidenceRef(s)
		if err == nil {
			out = append(out, ref)
		} else {
			out = append(out, contextengine.EvidenceRef{Type: contextengine.RefTypePath, Ref: s})
		}
	}
	return out
}

// UniqueEvidenceRefs deduplicates EvidenceRef by identity (Type+Ref).
func UniqueEvidenceRefs(refs []contextengine.EvidenceRef) []contextengine.EvidenceRef {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	out := make([]contextengine.EvidenceRef, 0, len(refs))
	for _, ref := range refs {
		key := string(ref.Type) + ":" + ref.Ref
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

// Internal aliases for backward compatibility with callers in this package.
var (
	uniqueEvidenceRefs    = UniqueEvidenceRefs
	evidenceRefsToStrings = EvidenceRefsToStrings
	stringsToEvidenceRefs = StringsToEvidenceRefs
)
