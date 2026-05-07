package evidence

type EvidencePlane string

const (
	EvidencePlaneStructural     EvidencePlane = "structural_graph"
	EvidencePlaneSemanticAnchor EvidencePlane = "semantic_anchor"
	EvidencePlaneGitEmpirical   EvidencePlane = "git_empirical"
	EvidencePlaneDurable        EvidencePlane = "durable_knowledge"
)

func (p EvidencePlane) IsValid() bool {
	switch p {
	case EvidencePlaneStructural, EvidencePlaneSemanticAnchor, EvidencePlaneGitEmpirical, EvidencePlaneDurable:
		return true
	default:
		return false
	}
}

type EvidenceAuthority string

const (
	EvidenceAuthorityFact          EvidenceAuthority = "fact"
	EvidenceAuthorityEvidenceOnly  EvidenceAuthority = "evidence_only"
	EvidenceAuthorityContextSignal EvidenceAuthority = "context_signal"
	EvidenceAuthorityReviewed      EvidenceAuthority = "reviewed"
	EvidenceAuthorityPolicy        EvidenceAuthority = "policy"
)

func (a EvidenceAuthority) IsValid() bool {
	switch a {
	case EvidenceAuthorityFact, EvidenceAuthorityEvidenceOnly, EvidenceAuthorityContextSignal, EvidenceAuthorityReviewed, EvidenceAuthorityPolicy:
		return true
	default:
		return false
	}
}

type EvidenceSource string

const (
	EvidenceSourceStructuralGraph EvidenceSource = "structural_graph"
	EvidenceSourceSemanticAnchor  EvidenceSource = "semantic_anchor"
	EvidenceSourceGitCoChange     EvidenceSource = "git_cochange"
	EvidenceSourceDurableMemory   EvidenceSource = "durable_memory"
)

func (s EvidenceSource) IsValid() bool {
	switch s {
	case EvidenceSourceStructuralGraph, EvidenceSourceSemanticAnchor, EvidenceSourceGitCoChange, EvidenceSourceDurableMemory:
		return true
	default:
		return false
	}
}

type EvidenceClass string

const (
	EvidenceClassCodeFact       EvidenceClass = "code_fact"
	EvidenceClassSourceComment  EvidenceClass = "source_comment"
	EvidenceClassContextSignal  EvidenceClass = "context_signal"
	EvidenceClassReviewedMemory EvidenceClass = "reviewed_memory"
)

func (c EvidenceClass) IsValid() bool {
	switch c {
	case EvidenceClassCodeFact, EvidenceClassSourceComment, EvidenceClassContextSignal, EvidenceClassReviewedMemory:
		return true
	default:
		return false
	}
}

type AuthorityEffect string

const (
	AuthorityEffectNone              AuthorityEffect = "none"
	AuthorityEffectRetrievalRanking  AuthorityEffect = "retrieval_ranking"
	AuthorityEffectReviewSignal      AuthorityEffect = "review_signal"
	AuthorityEffectReviewWarning     AuthorityEffect = "review_warning"
	AuthorityEffectInstructionSource AuthorityEffect = "instruction_source"
)

func (e AuthorityEffect) IsValid() bool {
	switch e {
	case AuthorityEffectNone, AuthorityEffectRetrievalRanking, AuthorityEffectReviewSignal, AuthorityEffectReviewWarning, AuthorityEffectInstructionSource:
		return true
	default:
		return false
	}
}

type RenderSurface string

const (
	RenderSurfaceEvidencePack      RenderSurface = "evidence_pack"
	RenderSurfaceReview            RenderSurface = "review"
	RenderSurfaceReviewWarning     RenderSurface = "review_warning"
	RenderSurfaceEvidenceHint      RenderSurface = "evidence_hint"
	RenderSurfaceInstruction       RenderSurface = "instruction"
	RenderSurfacePolicy            RenderSurface = "policy"
	RenderSurfaceHardConstraint    RenderSurface = "hard_constraint"
	RenderSurfaceToolAuthorization RenderSurface = "tool_authorization"
	RenderSurfaceRuntimeGuardrail  RenderSurface = "runtime_guardrail"
)

func (s RenderSurface) IsValid() bool {
	switch s {
	case RenderSurfaceEvidencePack, RenderSurfaceReview, RenderSurfaceReviewWarning,
		RenderSurfaceEvidenceHint, RenderSurfaceInstruction, RenderSurfacePolicy,
		RenderSurfaceHardConstraint, RenderSurfaceToolAuthorization, RenderSurfaceRuntimeGuardrail:
		return true
	default:
		return false
	}
}

type EvidenceMeta struct {
	Source                  EvidenceSource    `json:"source"`
	SourcePlane             EvidencePlane     `json:"source_plane"`
	EvidenceClass           EvidenceClass     `json:"evidence_class"`
	EvidenceAuthority       EvidenceAuthority `json:"evidence_authority"`
	AllowedAuthorityEffects []AuthorityEffect `json:"allowed_authority_effects"`
}
