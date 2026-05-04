package contextengine

import (
	"fmt"
	"time"
)

// ContextBundleStatus describes whether gathered context is usable for an answer.
type ContextBundleStatus string

const (
	ContextBundleStatusSufficient ContextBundleStatus = "sufficient"
	ContextBundleStatusPartial    ContextBundleStatus = "partial"
	ContextBundleStatusBlocked    ContextBundleStatus = "blocked"
)

// IsValid reports whether s is a known ContextBundleStatus.
func (s ContextBundleStatus) IsValid() bool {
	switch s {
	case ContextBundleStatusSufficient, ContextBundleStatusPartial, ContextBundleStatusBlocked:
		return true
	default:
		return false
	}
}

// ContextFactStatus describes the support level of a fact in a bundle.
type ContextFactStatus string

const (
	ContextFactStatusSupported    ContextFactStatus = "supported"
	ContextFactStatusCandidate    ContextFactStatus = "candidate"
	ContextFactStatusUnsupported  ContextFactStatus = "unsupported"
	ContextFactStatusContradicted ContextFactStatus = "contradicted"
	ContextFactStatusStale        ContextFactStatus = "stale"
)

// IsValid reports whether s is a known ContextFactStatus.
func (s ContextFactStatus) IsValid() bool {
	switch s {
	case ContextFactStatusSupported, ContextFactStatusCandidate, ContextFactStatusUnsupported,
		ContextFactStatusContradicted, ContextFactStatusStale:
		return true
	default:
		return false
	}
}

// ContextCertificateStatus describes the result of runtime bundle certification.
type ContextCertificateStatus string

const (
	ContextCertificateStatusCertified ContextCertificateStatus = "certified"
	ContextCertificateStatusPartial   ContextCertificateStatus = "partial"
	ContextCertificateStatusFailed    ContextCertificateStatus = "failed"
)

// IsValid reports whether s is a known ContextCertificateStatus.
func (s ContextCertificateStatus) IsValid() bool {
	switch s {
	case ContextCertificateStatusCertified, ContextCertificateStatusPartial, ContextCertificateStatusFailed:
		return true
	default:
		return false
	}
}

// ContextFact is a reduced claim tied back to source evidence nodes.
type ContextFact struct {
	ID            string            `json:"id"`
	WorkspaceID   string            `json:"workspace_id"`
	Kind          EvidenceNodeType  `json:"kind"`
	Fact          string            `json:"fact"`
	Refs          []EvidenceRef     `json:"refs,omitempty"`
	EvidenceIDs   []string          `json:"evidence_ids"`
	Confidence    float64           `json:"confidence,omitempty"`
	Grounding     Grounding         `json:"grounding,omitempty"`
	Status        ContextFactStatus `json:"status"`
	Staleness     StalenessStatus   `json:"staleness,omitempty"`
	SourcePackIDs []string          `json:"source_pack_ids,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
}

// Validate checks the fact contract.
func (f ContextFact) Validate() error {
	if f.ID == "" {
		return fmt.Errorf("context fact: missing id")
	}
	if f.WorkspaceID == "" {
		return fmt.Errorf("context fact: missing workspace_id")
	}
	if !f.Kind.IsValid() {
		return fmt.Errorf("context fact: unknown kind %q", f.Kind)
	}
	if f.Fact == "" {
		return fmt.Errorf("context fact: missing fact")
	}
	if len(f.EvidenceIDs) == 0 {
		return fmt.Errorf("context fact: missing evidence_ids")
	}
	if !f.Status.IsValid() {
		return fmt.Errorf("context fact: unknown status %q", f.Status)
	}
	if f.Grounding != "" && !f.Grounding.IsValid() {
		return fmt.Errorf("context fact: unknown grounding %q", f.Grounding)
	}
	if f.Staleness != "" && !f.Staleness.IsValid() {
		return fmt.Errorf("context fact: unknown staleness %q", f.Staleness)
	}
	for i, ref := range f.Refs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("context fact: ref[%d]: %w", i, err)
		}
	}
	return nil
}

// ContextConflict records contradictory or mutually suspicious evidence.
type ContextConflict struct {
	ID          string   `json:"id"`
	Claim       string   `json:"claim"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	Explanation string   `json:"explanation,omitempty"`
}

// ContextGap records missing evidence needed for a stronger bundle.
type ContextGap struct {
	ID       string `json:"id"`
	Required string `json:"required"`
	Reason   string `json:"reason,omitempty"`
}

// ContextCheck records one runtime certification check.
type ContextCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ContextTrustGate records one deterministic gate contributing to answer trust.
type ContextTrustGate struct {
	Name    string   `json:"name"`
	Status  string   `json:"status"`
	Score   float64  `json:"score,omitempty"`
	Message string   `json:"message,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

// ContextTrustReport distinguishes internally valid evidence from context that
// is complete enough for answer-time use.
type ContextTrustReport struct {
	Status             string             `json:"status"`
	InternalEvidenceOK bool               `json:"internal_evidence_ok"`
	RequiredEvidenceOK bool               `json:"required_evidence_ok"`
	AnswerContextOK    bool               `json:"answer_context_ok"`
	GraphRecommended   bool               `json:"graph_recommended,omitempty"`
	GraphConfidence    float64            `json:"graph_confidence,omitempty"`
	CoverageScore      float64            `json:"coverage_score,omitempty"`
	FreshnessScore     float64            `json:"freshness_score,omitempty"`
	Gates              []ContextTrustGate `json:"gates,omitempty"`
}

// ContextSelectedPath is a runtime-ranked file path candidate that an answerer
// can copy directly without re-inferring the file set from raw evidence.
type ContextSelectedPath struct {
	Path        string         `json:"path"`
	EvidenceIDs []string       `json:"evidence_ids"`
	Refs        []EvidenceRef  `json:"refs,omitempty"`
	CoverageIDs []string       `json:"coverage_ids,omitempty"`
	Confidence  float64        `json:"confidence,omitempty"`
	Rank        int            `json:"rank,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Validate checks the selected path contract.
func (p ContextSelectedPath) Validate() error {
	if p.Path == "" {
		return fmt.Errorf("context selected path: missing path")
	}
	if len(p.EvidenceIDs) == 0 {
		return fmt.Errorf("context selected path: missing evidence_ids")
	}
	for i, ref := range p.Refs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("context selected path: ref[%d]: %w", i, err)
		}
	}
	return nil
}

// ContextAnswerCandidate is a runtime-ranked answer primitive tied to evidence.
type ContextAnswerCandidate struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"` // path, fact, symbol, note, memory, session, task
	Value       string         `json:"value"`
	EvidenceIDs []string       `json:"evidence_ids"`
	Refs        []EvidenceRef  `json:"refs,omitempty"`
	Confidence  float64        `json:"confidence,omitempty"`
	Rank        int            `json:"rank,omitempty"`
	Reason      string         `json:"reason,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Validate checks the answer candidate contract.
func (c ContextAnswerCandidate) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("context answer candidate: missing id")
	}
	if c.Kind == "" {
		return fmt.Errorf("context answer candidate: missing kind")
	}
	if c.Value == "" {
		return fmt.Errorf("context answer candidate: missing value")
	}
	if len(c.EvidenceIDs) == 0 {
		return fmt.Errorf("context answer candidate: missing evidence_ids")
	}
	for i, ref := range c.Refs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("context answer candidate: ref[%d]: %w", i, err)
		}
	}
	return nil
}

// ContextCategory groups selected paths into a package or evidence-signal bucket
// for architecture/subsystem map answers.
type ContextCategory struct {
	Name        string         `json:"name"`
	Role        string         `json:"role,omitempty"`
	Paths       []string       `json:"paths"`
	EvidenceIDs []string       `json:"evidence_ids,omitempty"`
	Signals     []string       `json:"signals,omitempty"`
	Rank        int            `json:"rank,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Validate checks the category contract.
func (c ContextCategory) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("context category: missing name")
	}
	if len(c.Paths) == 0 {
		return fmt.Errorf("context category: missing paths")
	}
	return nil
}

// ContextIntegrationEdge describes an observed link between selected path
// groups. It is evidence-shaped for answer surfaces and intentionally shallow;
// deeper graph expansion remains a retrieval concern.
type ContextIntegrationEdge struct {
	From        string         `json:"from"`
	To          string         `json:"to"`
	Paths       []string       `json:"paths,omitempty"`
	EvidenceIDs []string       `json:"evidence_ids,omitempty"`
	Signals     []string       `json:"signals,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Validate checks the integration edge contract.
func (e ContextIntegrationEdge) Validate() error {
	if e.From == "" {
		return fmt.Errorf("context integration edge: missing from")
	}
	if e.To == "" {
		return fmt.Errorf("context integration edge: missing to")
	}
	return nil
}

// ContextCertificate is produced by runtime validation, not by the model.
type ContextCertificate struct {
	ID                 string                   `json:"id"`
	WorkspaceID        string                   `json:"workspace_id"`
	BundleID           string                   `json:"bundle_id"`
	Status             ContextCertificateStatus `json:"status"`
	Checks             []ContextCheck           `json:"checks,omitempty"`
	UnsupportedFacts   []string                 `json:"unsupported_facts,omitempty"`
	StaleEvidenceIDs   []string                 `json:"stale_evidence_ids,omitempty"`
	UnloadableRefs     []EvidenceRef            `json:"unloadable_refs,omitempty"`
	ConflictIDs        []string                 `json:"conflict_ids,omitempty"`
	MissingEvidence    []string                 `json:"missing_evidence,omitempty"`
	RequiredEvidenceOK bool                     `json:"required_evidence_ok"`
	InternalEvidenceOK bool                     `json:"internal_evidence_ok"`
	AnswerContextOK    bool                     `json:"answer_context_ok"`
	Trust              *ContextTrustReport      `json:"trust,omitempty"`
	IssuedAt           time.Time                `json:"issued_at"`
	ExpiresAt          time.Time                `json:"expires_at,omitempty"`
}

// Validate checks the certificate contract.
func (c ContextCertificate) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("context certificate: missing id")
	}
	if c.WorkspaceID == "" {
		return fmt.Errorf("context certificate: missing workspace_id")
	}
	if c.BundleID == "" {
		return fmt.Errorf("context certificate: missing bundle_id")
	}
	if !c.Status.IsValid() {
		return fmt.Errorf("context certificate: unknown status %q", c.Status)
	}
	for i, ref := range c.UnloadableRefs {
		if err := ValidateEvidenceRef(ref); err != nil {
			return fmt.Errorf("context certificate: unloadable_ref[%d]: %w", i, err)
		}
	}
	return nil
}

// ContextBundle is the reduced, certified context surface passed to answerers.
type ContextBundle struct {
	ID               string                   `json:"id"`
	WorkspaceID      string                   `json:"workspace_id"`
	Query            string                   `json:"query"`
	Goal             string                   `json:"goal,omitempty"`
	Status           ContextBundleStatus      `json:"status"`
	Answerable       bool                     `json:"answerable"`
	Summary          string                   `json:"summary,omitempty"`
	Categories       []ContextCategory        `json:"categories,omitempty"`
	IntegrationEdges []ContextIntegrationEdge `json:"integration_edges,omitempty"`
	SelectedPaths    []ContextSelectedPath    `json:"selected_paths,omitempty"`
	CoverageReport   *CoverageReport          `json:"coverage_report,omitempty"`
	AnswerCandidates []ContextAnswerCandidate `json:"answer_candidates,omitempty"`
	Facts            []ContextFact            `json:"facts,omitempty"`
	Evidence         []EvidenceNode           `json:"evidence,omitempty"`
	Conflicts        []ContextConflict        `json:"conflicts,omitempty"`
	Missing          []ContextGap             `json:"missing,omitempty"`
	SourceCoverage   map[string]int           `json:"source_coverage,omitempty"`
	SourcePackIDs    []string                 `json:"source_pack_ids,omitempty"`
	SourceEpisodeIDs []string                 `json:"source_episode_ids,omitempty"`
	Telemetry        EvidenceTelemetry        `json:"telemetry,omitempty"`
	Certificate      *ContextCertificate      `json:"certificate,omitempty"`
	Trust            *ContextTrustReport      `json:"trust,omitempty"`
	CreatedAt        time.Time                `json:"created_at,omitempty"`
	Metadata         map[string]any           `json:"metadata,omitempty"`
}

// Validate checks that a bundle is internally consistent.
func (b ContextBundle) Validate() error {
	if b.ID == "" {
		return fmt.Errorf("context bundle: missing id")
	}
	if b.WorkspaceID == "" {
		return fmt.Errorf("context bundle: missing workspace_id")
	}
	if b.Query == "" {
		return fmt.Errorf("context bundle: missing query")
	}
	if !b.Status.IsValid() {
		return fmt.Errorf("context bundle: unknown status %q", b.Status)
	}
	evidenceIDs := make(map[string]struct{}, len(b.Evidence))
	for i, node := range b.Evidence {
		if err := node.Validate(); err != nil {
			return fmt.Errorf("context bundle: evidence[%d]: %w", i, err)
		}
		if _, ok := evidenceIDs[node.ID]; ok {
			return fmt.Errorf("context bundle: duplicate evidence id %q", node.ID)
		}
		evidenceIDs[node.ID] = struct{}{}
	}
	for i, fact := range b.Facts {
		if err := fact.Validate(); err != nil {
			return fmt.Errorf("context bundle: fact[%d]: %w", i, err)
		}
		for _, id := range fact.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return fmt.Errorf("context bundle: fact[%d] references missing evidence id %q", i, id)
			}
		}
	}
	for i, path := range b.SelectedPaths {
		if err := path.Validate(); err != nil {
			return fmt.Errorf("context bundle: selected_path[%d]: %w", i, err)
		}
		for _, id := range path.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return fmt.Errorf("context bundle: selected_path[%d] references missing evidence id %q", i, id)
			}
		}
	}
	if b.CoverageReport != nil {
		requirementIDs := map[string]struct{}{}
		for i, req := range b.CoverageReport.Requirements {
			if err := req.Validate(); err != nil {
				return fmt.Errorf("context bundle: coverage_requirement[%d]: %w", i, err)
			}
			requirementIDs[req.ID] = struct{}{}
		}
		for i, covered := range b.CoverageReport.Covered {
			if covered.RequirementID == "" {
				return fmt.Errorf("context bundle: coverage[%d]: missing requirement_id", i)
			}
			if _, ok := requirementIDs[covered.RequirementID]; !ok {
				return fmt.Errorf("context bundle: coverage[%d] references missing requirement %q", i, covered.RequirementID)
			}
			if covered.Path == "" {
				return fmt.Errorf("context bundle: coverage[%d]: missing path", i)
			}
			for _, id := range covered.EvidenceIDs {
				if _, ok := evidenceIDs[id]; !ok {
					return fmt.Errorf("context bundle: coverage[%d] references missing evidence id %q", i, id)
				}
			}
		}
	}
	for i, candidate := range b.AnswerCandidates {
		if err := candidate.Validate(); err != nil {
			return fmt.Errorf("context bundle: answer_candidate[%d]: %w", i, err)
		}
		for _, id := range candidate.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return fmt.Errorf("context bundle: answer_candidate[%d] references missing evidence id %q", i, id)
			}
		}
	}
	for i, category := range b.Categories {
		if err := category.Validate(); err != nil {
			return fmt.Errorf("context bundle: category[%d]: %w", i, err)
		}
		for _, id := range category.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return fmt.Errorf("context bundle: category[%d] references missing evidence id %q", i, id)
			}
		}
	}
	for i, edge := range b.IntegrationEdges {
		if err := edge.Validate(); err != nil {
			return fmt.Errorf("context bundle: integration_edge[%d]: %w", i, err)
		}
		for _, id := range edge.EvidenceIDs {
			if _, ok := evidenceIDs[id]; !ok {
				return fmt.Errorf("context bundle: integration_edge[%d] references missing evidence id %q", i, id)
			}
		}
	}
	if b.Certificate != nil {
		if err := b.Certificate.Validate(); err != nil {
			return fmt.Errorf("context bundle: %w", err)
		}
		if b.Certificate.BundleID != b.ID {
			return fmt.Errorf("context bundle: certificate bundle_id %q does not match %q", b.Certificate.BundleID, b.ID)
		}
	}
	return nil
}

// Ensure context bundle types satisfy the shared validator contract.
var (
	_ Validator = ContextFact{}
	_ Validator = ContextSelectedPath{}
	_ Validator = ContextAnswerCandidate{}
	_ Validator = ContextCategory{}
	_ Validator = ContextIntegrationEdge{}
	_ Validator = ContextCertificate{}
	_ Validator = ContextBundle{}
)
