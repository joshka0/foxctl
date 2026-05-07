package semanticanchors

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/joshka0/foxctl/internal/intelligence/evidence"
)

const (
	DefaultParserVersion          = "semantic-anchors-pr-a1"
	DefaultExtractionVersion      = 1
	DefaultMaxTargetBytes         = 512
	DefaultMaxAnchorsPerOwner     = 6
	DefaultWarnAnchorsPerOwner    = 4
	DefaultMaxBeaconsPerOwner     = 1
	DefaultMaxBlankLinesToOwner   = 1
	DefaultMaxOwnerLookaheadLines = 3
	RepoLocalAnchorScope          = AnchorScope("repo")

	SemanticAnchorEdgeMetaSchemaVersion = 1
	semanticAnchorEdgeMetaKind          = "semantic_anchor_edge"
)

type (
	AnchorType            string
	AnchorScope           string
	AnchorTargetID        string
	RepoIndexAnchorNodeID string
)

const (
	AnchorTypeInvariant    AnchorType = "invariant"
	AnchorTypeRisk         AnchorType = "risk"
	AnchorTypeProtocol     AnchorType = "protocol"
	AnchorTypeDoc          AnchorType = "doc"
	AnchorTypeTest         AnchorType = "test"
	AnchorTypeTestContract AnchorType = "test-contract"
	AnchorTypeDomain       AnchorType = "domain"
	AnchorTypeDecision     AnchorType = "decision"
	AnchorTypeBeacon       AnchorType = "beacon"
)

type AnchorFindingReason string

const (
	AnchorFindingUnsafeURL            AnchorFindingReason = "unsafe_url"
	AnchorFindingAbsolutePath         AnchorFindingReason = "absolute_path"
	AnchorFindingPathTraversal        AnchorFindingReason = "path_traversal"
	AnchorFindingBackslashPath        AnchorFindingReason = "backslash_path"
	AnchorFindingControlChar          AnchorFindingReason = "control_char"
	AnchorFindingEnvVarExpansion      AnchorFindingReason = "env_var_expansion"
	AnchorFindingSecretLike           AnchorFindingReason = "secret_like"
	AnchorFindingSessionLike          AnchorFindingReason = "session_like"
	AnchorFindingTooLong              AnchorFindingReason = "too_long"
	AnchorFindingPIILike              AnchorFindingReason = "pii_like"
	AnchorFindingNamespaceCollision   AnchorFindingReason = "namespace_collision"
	AnchorFindingScopedPathAnchor     AnchorFindingReason = "scoped_path_anchor"
	AnchorFindingUnknownScope         AnchorFindingReason = "unknown_scope"
	AnchorFindingUnknownType          AnchorFindingReason = "unknown_type"
	AnchorFindingMalformedTarget      AnchorFindingReason = "malformed_target"
	AnchorFindingMissingTarget        AnchorFindingReason = "missing_target"
	AnchorFindingUnresolvedFragment   AnchorFindingReason = "unresolved_fragment"
	AnchorFindingDuplicateOwnerTarget AnchorFindingReason = "duplicate_owner_target"
	AnchorFindingUnboundOwner         AnchorFindingReason = "unbound_owner"
	AnchorFindingUnsupportedOwner     AnchorFindingReason = "unsupported_owner"
	AnchorFindingTooManyAnchors       AnchorFindingReason = "too_many_anchors"
	AnchorFindingTooManyBeacons       AnchorFindingReason = "too_many_beacons"
	AnchorFindingBeaconWithoutSupport AnchorFindingReason = "beacon_without_support"
	AnchorFindingGeneratedOrVendor    AnchorFindingReason = "generated_or_vendor"
	AnchorFindingLongFormAdjacentNote AnchorFindingReason = "long_form_adjacent_note"
)

type AnchorFindingSeverity string

const (
	AnchorFindingInfo    AnchorFindingSeverity = "info"
	AnchorFindingWarning AnchorFindingSeverity = "warning"
	AnchorFindingError   AnchorFindingSeverity = "error"
)

type SemanticAnchorRelation string

const (
	SemanticAnchorRelationEnforces           SemanticAnchorRelation = "ENFORCES"
	SemanticAnchorRelationProtectsAgainst    SemanticAnchorRelation = "PROTECTS_AGAINST"
	SemanticAnchorRelationVerifiedBy         SemanticAnchorRelation = "VERIFIED_BY"
	SemanticAnchorRelationDescribedBy        SemanticAnchorRelation = "DESCRIBED_BY"
	SemanticAnchorRelationDecidedBy          SemanticAnchorRelation = "DECIDED_BY"
	SemanticAnchorRelationImplementsProtocol SemanticAnchorRelation = "IMPLEMENTS_PROTOCOL"
	SemanticAnchorRelationParticipatesIn     SemanticAnchorRelation = "PARTICIPATES_IN"
	SemanticAnchorRelationDeclaresTarget     SemanticAnchorRelation = "DECLARES_ANCHOR_TARGET"
)

type AnchorValidationStatus string

const (
	AnchorValidationEvidenceOnly   AnchorValidationStatus = "evidence_only"
	AnchorValidationValidReference AnchorValidationStatus = "valid_reference"
	AnchorValidationMissingTarget  AnchorValidationStatus = "missing_target"
	AnchorValidationLintError      AnchorValidationStatus = "lint_error"
)

type AnchorEdgeAction string

const (
	AnchorEdgeNone          AnchorEdgeAction = "none"
	AnchorEdgeSemantic      AnchorEdgeAction = "semantic"
	AnchorEdgeMissingTarget AnchorEdgeAction = "missing_target"
)

type LanguageAnchorSupport string

const (
	AnchorSupportLintOnly     LanguageAnchorSupport = "lint_only"
	AnchorSupportGraphBinding LanguageAnchorSupport = "graph_binding"
)

type SourceSpan struct {
	Path      string
	LineStart int
	LineEnd   int
}

type Span struct {
	LineStart int
	LineEnd   int
	ColStart  int
	ColEnd    int
}

type CommentSpan struct {
	Path string `json:"path"`
	Span Span   `json:"span"`
	Text string `json:"-"`
}

type AnchorOwner struct {
	NodeID    string
	Kind      string
	StableKey string
	Path      string
	Name      string
	StartLine int
	EndLine   int
}

type AnchorOwnerBinding struct {
	OwnerNodeID    string
	OwnerKind      string
	OwnerStableKey string
}

type Finding struct {
	ID       string
	Reason   AnchorFindingReason
	Severity AnchorFindingSeverity
	Message  string
}

type AnchorTypePolicy struct {
	Type       AnchorType
	Relation   SemanticAnchorRelation
	FileScope  bool
	SymbolOnly bool
	Indexable  bool
}

type AnchorPolicy struct {
	RepoKey                string
	AllowedScopes          map[AnchorScope]struct{}
	TypePolicies           map[AnchorType]AnchorTypePolicy
	MaxTargetBytes         int
	MaxAnchorsPerOwner     int
	WarnAnchorsPerOwner    int
	MaxBeaconsPerOwner     int
	MaxBlankLinesToOwner   int
	MaxOwnerLookaheadLines int
	ParserVersion          string
	ExtractionVersion      int
}

type AnchorOccurrence struct {
	Type             AnchorType
	Scope            AnchorScope
	Target           string
	TargetID         AnchorTargetID
	DisplaySyntax    string
	TargetDisplay    string
	Span             SourceSpan
	OwnerBinding     AnchorOwnerBinding
	OccurrenceID     string
	SourceHash       string
	ValidationStatus AnchorValidationStatus
	Findings         []Finding
}

type AnchorResolution struct {
	Occurrence       AnchorOccurrence
	Relation         SemanticAnchorRelation
	IntendedRelation SemanticAnchorRelation
	EdgeAction       AnchorEdgeAction
}

type TargetResolver interface {
	ResolveAnchorTarget(ctx context.Context, occurrence AnchorOccurrence) (TargetResolution, error)
}

type TargetResolution struct {
	Status  AnchorValidationStatus
	Finding *Finding
}

type RepoScopedAnchorKey struct {
	RepoKey  string
	TargetID AnchorTargetID
}

type SemanticAnchorEdgeMeta struct {
	MetaKind                string                     `json:"meta_kind"`
	SchemaVersion           int                        `json:"schema_version"`
	RepoKey                 string                     `json:"repo_key"`
	Source                  evidence.EvidenceSource    `json:"source"`
	SourcePlane             evidence.EvidencePlane     `json:"source_plane"`
	EvidenceClass           evidence.EvidenceClass     `json:"evidence_class"`
	EvidenceAuthority       evidence.EvidenceAuthority `json:"evidence_authority"`
	AllowedAuthorityEffects []evidence.AuthorityEffect `json:"allowed_authority_effects"`
	DisplaySyntax           string                     `json:"display_syntax,omitempty"`
	OccurrenceID            string                     `json:"occurrence_id"`
	ExtractionVersion       int                        `json:"extraction_version"`
	ParserVersion           string                     `json:"parser_version"`
	Path                    string                     `json:"path"`
	LineStart               int                        `json:"line_start"`
	LineEnd                 int                        `json:"line_end"`
	OwnerNodeID             string                     `json:"owner_node_id"`
	OwnerKind               string                     `json:"owner_kind"`
	OwnerStableKey          string                     `json:"owner_stable_key,omitempty"`
	Relation                SemanticAnchorRelation     `json:"relation"`
	TargetDisplay           string                     `json:"target_display,omitempty"`
	TargetID                AnchorTargetID             `json:"target_id"`
	TargetType              string                     `json:"target_type"`
	TargetSlug              string                     `json:"target_slug,omitempty"`
	Scope                   string                     `json:"scope,omitempty"`
	ValidationStatus        AnchorValidationStatus     `json:"validation_status"`
	LintFindingIDs          []string                   `json:"lint_finding_ids,omitempty"`
	MaxFindingSeverity      AnchorFindingSeverity      `json:"max_finding_severity,omitempty"`
	SourceHash              string                     `json:"source_hash,omitempty"`
}

type AnchorError struct {
	Reason AnchorFindingReason
}

func (e AnchorError) Error() string {
	if e.Reason == "" {
		return "semantic anchor validation failed"
	}
	return "semantic anchor validation failed: " + string(e.Reason)
}

func DefaultAnchorPolicy(repoKey string, configuredScopes []AnchorScope) AnchorPolicy {
	scopes := map[AnchorScope]struct{}{}
	if slug := repoKeySlug(repoKey); slug != "" {
		scopes[AnchorScope(slug)] = struct{}{}
	}
	for _, scope := range configuredScopes {
		if scope != "" {
			scopes[scope] = struct{}{}
		}
	}
	typePolicies := map[AnchorType]AnchorTypePolicy{
		AnchorTypeInvariant:    {Type: AnchorTypeInvariant, Relation: SemanticAnchorRelationEnforces, SymbolOnly: true, Indexable: true},
		AnchorTypeRisk:         {Type: AnchorTypeRisk, Relation: SemanticAnchorRelationProtectsAgainst, SymbolOnly: true, Indexable: true},
		AnchorTypeProtocol:     {Type: AnchorTypeProtocol, Relation: SemanticAnchorRelationImplementsProtocol, FileScope: true, Indexable: true},
		AnchorTypeDoc:          {Type: AnchorTypeDoc, Relation: SemanticAnchorRelationDescribedBy, FileScope: true, Indexable: true},
		AnchorTypeTest:         {Type: AnchorTypeTest, Relation: SemanticAnchorRelationVerifiedBy, SymbolOnly: true, Indexable: true},
		AnchorTypeTestContract: {Type: AnchorTypeTestContract, Relation: SemanticAnchorRelationVerifiedBy, SymbolOnly: true, Indexable: true},
		AnchorTypeDomain:       {Type: AnchorTypeDomain, Relation: SemanticAnchorRelationParticipatesIn, FileScope: true, Indexable: true},
		AnchorTypeDecision:     {Type: AnchorTypeDecision, Relation: SemanticAnchorRelationDecidedBy, FileScope: true, Indexable: true},
		AnchorTypeBeacon:       {Type: AnchorTypeBeacon, Relation: SemanticAnchorRelationParticipatesIn, FileScope: true},
	}
	return AnchorPolicy{
		RepoKey:                repoKey,
		AllowedScopes:          scopes,
		TypePolicies:           typePolicies,
		MaxTargetBytes:         DefaultMaxTargetBytes,
		MaxAnchorsPerOwner:     DefaultMaxAnchorsPerOwner,
		WarnAnchorsPerOwner:    DefaultWarnAnchorsPerOwner,
		MaxBeaconsPerOwner:     DefaultMaxBeaconsPerOwner,
		MaxBlankLinesToOwner:   DefaultMaxBlankLinesToOwner,
		MaxOwnerLookaheadLines: DefaultMaxOwnerLookaheadLines,
		ParserVersion:          DefaultParserVersion,
		ExtractionVersion:      DefaultExtractionVersion,
	}
}

func ParseInlineAnchor(policy AnchorPolicy, syntax string) (AnchorOccurrence, []Finding) {
	body, ok := strings.CutPrefix(syntax, "[[")
	if !ok || !strings.HasSuffix(body, "]]") {
		return invalidOccurrence(AnchorFindingMalformedTarget), []Finding{newFinding(AnchorFindingMalformedTarget, AnchorFindingError)}
	}
	body = strings.TrimSuffix(body, "]]")
	if body == "" {
		return invalidOccurrence(AnchorFindingMalformedTarget), []Finding{newFinding(AnchorFindingMalformedTarget, AnchorFindingError)}
	}
	left, right, ok := strings.Cut(body, ":")
	if !ok || left == "" || right == "" {
		return invalidOccurrence(AnchorFindingMalformedTarget), []Finding{newFinding(AnchorFindingMalformedTarget, AnchorFindingError)}
	}
	if strings.Contains(body, "::") {
		return invalidOccurrence(AnchorFindingNamespaceCollision), []Finding{newFinding(AnchorFindingNamespaceCollision, AnchorFindingError)}
	}
	var scope AnchorScope
	var typ AnchorType
	target := right
	if _, isType := policy.TypePolicies[AnchorType(left)]; isType {
		typ = AnchorType(left)
		scope = RepoLocalAnchorScope
	} else {
		scope = AnchorScope(left)
		nextType, nextTarget, scopedOK := strings.Cut(right, "/")
		if !scopedOK || nextType == "" || nextTarget == "" {
			return invalidOccurrence(AnchorFindingUnknownScope), []Finding{newFinding(AnchorFindingUnknownScope, AnchorFindingError)}
		}
		typ = AnchorType(nextType)
		target = nextTarget
	}
	occ := AnchorOccurrence{Type: typ, Scope: scope, Target: target, DisplaySyntax: syntax, TargetDisplay: target}
	if typ == AnchorTypeDoc || typ == AnchorTypeTest {
		if scope != RepoLocalAnchorScope {
			return occ, []Finding{newFinding(AnchorFindingScopedPathAnchor, AnchorFindingError)}
		}
	} else if _, ok := policy.AllowedScopes[scope]; scope != RepoLocalAnchorScope && !ok {
		return occ, []Finding{newFinding(AnchorFindingUnknownScope, AnchorFindingError)}
	}
	if _, ok := policy.TypePolicies[typ]; !ok {
		return occ, []Finding{newFinding(AnchorFindingUnknownType, AnchorFindingError)}
	}
	if finding, ok := ValidateAnchorTarget(policy, typ, target); !ok {
		return occ, []Finding{finding}
	}
	canon, finding, ok := CanonicalizeAnchor(policy, occ)
	if !ok {
		return occ, []Finding{finding}
	}
	return canon, nil
}

func ValidateAnchorTarget(policy AnchorPolicy, typ AnchorType, target string) (Finding, bool) {
	if len(target) > maxTargetBytes(policy) {
		return newFinding(AnchorFindingTooLong, AnchorFindingError), false
	}
	if reason, bad := unsafeTargetReason(target); bad {
		return newFinding(reason, AnchorFindingError), false
	}
	if typ == AnchorTypeDoc || typ == AnchorTypeTest {
		if strings.Contains(target, ":") {
			return newFinding(AnchorFindingUnsafeURL, AnchorFindingError), false
		}
		clean := path.Clean(strings.Split(target, "#")[0])
		if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || clean == ".." {
			return newFinding(AnchorFindingPathTraversal, AnchorFindingError), false
		}
		return Finding{}, true
	}
	if !conceptSlugRE.MatchString(target) {
		return newFinding(AnchorFindingMalformedTarget, AnchorFindingError), false
	}
	return Finding{}, true
}

func CanonicalizeAnchor(policy AnchorPolicy, occ AnchorOccurrence) (AnchorOccurrence, Finding, bool) {
	targetID, err := NewAnchorTargetID(string(occ.Scope), string(occ.Type), occ.Target)
	if err != nil {
		var anchorErr AnchorError
		if errors.As(err, &anchorErr) {
			return occ, newFinding(anchorErr.Reason, AnchorFindingError), false
		}
		return occ, newFinding(AnchorFindingMalformedTarget, AnchorFindingError), false
	}
	occ.TargetID = targetID
	if occ.DisplaySyntax == "" {
		if occ.Scope == RepoLocalAnchorScope {
			occ.DisplaySyntax = fmt.Sprintf("[[%s:%s]]", occ.Type, occ.Target)
		} else {
			occ.DisplaySyntax = fmt.Sprintf("[[%s:%s/%s]]", occ.Scope, occ.Type, occ.Target)
		}
	}
	if occ.TargetDisplay == "" {
		occ.TargetDisplay = occ.Target
	}
	occ.SourceHash = sourceHash(policy.RepoKey, occ.Span.Path, occ.DisplaySyntax)
	if occ.OccurrenceID == "" {
		occ.OccurrenceID = sourceHash(policy.RepoKey, occ.Span.Path, fmt.Sprintf("%d:%d:%s", occ.Span.LineStart, occ.Span.LineEnd, occ.DisplaySyntax))
	}
	return occ, Finding{}, true
}

func ResolveAnchorOccurrence(ctx context.Context, policy AnchorPolicy, occ AnchorOccurrence, resolver TargetResolver) (AnchorResolution, error) {
	p, ok := policy.TypePolicies[occ.Type]
	if !ok {
		return lintResolution(occ, AnchorFindingUnknownType), AnchorError{Reason: AnchorFindingUnknownType}
	}
	if occ.TargetID == "" {
		canon, finding, ok := CanonicalizeAnchor(policy, occ)
		if !ok {
			canon.Findings = append(canon.Findings, finding)
			return lintResolution(canon, finding.Reason), AnchorError{Reason: finding.Reason}
		}
		occ = canon
	}
	if hasErrorFinding(occ.Findings) {
		occ.ValidationStatus = AnchorValidationLintError
		return AnchorResolution{Occurrence: occ, Relation: SemanticAnchorRelationDeclaresTarget, IntendedRelation: p.Relation, EdgeAction: AnchorEdgeNone}, nil
	}
	occ.ValidationStatus = AnchorValidationEvidenceOnly
	relation := p.Relation
	action := AnchorEdgeSemantic
	if !p.Indexable {
		occ.OccurrenceID = stableOccurrenceID(policy.RepoKey, occ, relation, occ.OwnerBinding.OwnerStableKey)
		return AnchorResolution{Occurrence: occ, Relation: relation, IntendedRelation: p.Relation, EdgeAction: AnchorEdgeNone}, nil
	}
	if occ.Type == AnchorTypeDoc || occ.Type == AnchorTypeTest {
		if resolver == nil {
			occ.ValidationStatus = AnchorValidationValidReference
		} else {
			target, err := resolver.ResolveAnchorTarget(ctx, occ)
			if err != nil {
				return AnchorResolution{}, err
			}
			if target.Finding != nil {
				occ.Findings = append(occ.Findings, *target.Finding)
			}
			switch target.Status {
			case AnchorValidationMissingTarget:
				occ.ValidationStatus = AnchorValidationMissingTarget
				relation = SemanticAnchorRelationDeclaresTarget
				action = AnchorEdgeMissingTarget
			case AnchorValidationLintError:
				occ.ValidationStatus = AnchorValidationLintError
				action = AnchorEdgeNone
			default:
				occ.ValidationStatus = AnchorValidationValidReference
			}
		}
	}
	occ.OccurrenceID = stableOccurrenceID(policy.RepoKey, occ, relation, occ.OwnerBinding.OwnerStableKey)
	return AnchorResolution{Occurrence: occ, Relation: relation, IntendedRelation: p.Relation, EdgeAction: action}, nil
}

func NewAnchorTargetID(scope, typ, target string) (AnchorTargetID, error) {
	if scope == "" || typ == "" || target == "" || strings.Contains(scope+typ+target, "::") {
		return "", AnchorError{Reason: AnchorFindingNamespaceCollision}
	}
	return AnchorTargetID("anchor:" + scope + ":" + typ + ":" + target), nil
}

func RepoScopedAnchorKeyFor(repoKey string, target AnchorTargetID) RepoScopedAnchorKey {
	return RepoScopedAnchorKey{RepoKey: repoKey, TargetID: target}
}

func AnchorTargetNodeID(repoKey string, target AnchorTargetID) (RepoIndexAnchorNodeID, error) {
	if repoKey == "" || target == "" || strings.Contains(repoKey, "::") || strings.Contains(string(target), "::") {
		return "", AnchorError{Reason: AnchorFindingNamespaceCollision}
	}
	if !strings.HasPrefix(string(target), "anchor:") {
		return "", AnchorError{Reason: AnchorFindingMalformedTarget}
	}
	return RepoIndexAnchorNodeID(repoKey + "::" + string(target)), nil
}

func DecodeAnchorTargetNodeID(id string) (string, AnchorTargetID, bool) {
	repoKey, rawTarget, ok := strings.Cut(id, "::")
	if !ok || repoKey == "" || rawTarget == "" || strings.Contains(rawTarget, "::") || strings.Contains(repoKey, "::") {
		return "", "", false
	}
	if !strings.HasPrefix(rawTarget, "anchor:") {
		return "", "", false
	}
	return repoKey, AnchorTargetID(rawTarget), true
}

func NewSemanticAnchorEdgeMeta(res AnchorResolution, owner AnchorOwner) (SemanticAnchorEdgeMeta, error) {
	binding := res.Occurrence.OwnerBinding
	if owner.NodeID == "" || owner.Kind == "" || binding.OwnerNodeID != owner.NodeID || binding.OwnerKind != owner.Kind {
		return SemanticAnchorEdgeMeta{}, fmt.Errorf("semantic anchor owner mismatch")
	}
	if binding.OwnerStableKey != "" && owner.StableKey != "" && binding.OwnerStableKey != owner.StableKey {
		return SemanticAnchorEdgeMeta{}, fmt.Errorf("semantic anchor owner mismatch")
	}
	evidenceMeta := semanticAnchorEvidenceMeta()
	meta := SemanticAnchorEdgeMeta{
		MetaKind:                semanticAnchorEdgeMetaKind,
		SchemaVersion:           SemanticAnchorEdgeMetaSchemaVersion,
		RepoKey:                 res.OccurrenceOwnerRepoKey(),
		Source:                  evidenceMeta.Source,
		SourcePlane:             evidenceMeta.SourcePlane,
		EvidenceClass:           evidenceMeta.EvidenceClass,
		EvidenceAuthority:       evidenceMeta.EvidenceAuthority,
		AllowedAuthorityEffects: evidenceMeta.AllowedAuthorityEffects,
		DisplaySyntax:           res.Occurrence.DisplaySyntax,
		OccurrenceID:            res.Occurrence.OccurrenceID,
		ExtractionVersion:       DefaultExtractionVersion,
		ParserVersion:           DefaultParserVersion,
		Path:                    res.Occurrence.Span.Path,
		LineStart:               res.Occurrence.Span.LineStart,
		LineEnd:                 res.Occurrence.Span.LineEnd,
		OwnerNodeID:             owner.NodeID,
		OwnerKind:               owner.Kind,
		OwnerStableKey:          owner.StableKey,
		Relation:                res.Relation,
		TargetDisplay:           res.Occurrence.TargetDisplay,
		TargetID:                res.Occurrence.TargetID,
		TargetType:              string(res.Occurrence.Type),
		Scope:                   string(res.Occurrence.Scope),
		ValidationStatus:        res.Occurrence.ValidationStatus,
		LintFindingIDs:          findingIDs(res.Occurrence.Findings),
		MaxFindingSeverity:      maxSeverity(res.Occurrence.Findings),
		SourceHash:              res.Occurrence.SourceHash,
	}
	if res.Occurrence.Type != AnchorTypeDoc && res.Occurrence.Type != AnchorTypeTest {
		meta.TargetSlug = res.Occurrence.Target
	}
	if meta.RepoKey == "" {
		meta.RepoKey = repoKeyFromNodeID(owner.NodeID)
	}
	if err := ValidateSemanticAnchorEdgeMeta(meta); err != nil {
		return SemanticAnchorEdgeMeta{}, err
	}
	return meta, nil
}

func DecodeSemanticAnchorEdgeMeta(raw json.RawMessage) (SemanticAnchorEdgeMeta, error) {
	var meta SemanticAnchorEdgeMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return SemanticAnchorEdgeMeta{}, err
	}
	return meta, ValidateSemanticAnchorEdgeMeta(meta)
}

func ValidateSemanticAnchorEdgeMeta(meta SemanticAnchorEdgeMeta) error {
	if meta.MetaKind != semanticAnchorEdgeMetaKind || meta.SchemaVersion != SemanticAnchorEdgeMetaSchemaVersion {
		return fmt.Errorf("invalid semantic anchor edge metadata schema")
	}
	if err := evidence.ValidateEvidenceMeta(evidence.EvidenceMeta{
		Source:                  meta.Source,
		SourcePlane:             meta.SourcePlane,
		EvidenceClass:           meta.EvidenceClass,
		EvidenceAuthority:       meta.EvidenceAuthority,
		AllowedAuthorityEffects: meta.AllowedAuthorityEffects,
	}); err != nil {
		return err
	}
	if meta.RepoKey == "" || strings.Contains(meta.RepoKey, "::") || meta.OwnerNodeID == "" || meta.OwnerKind == "" || meta.TargetID == "" {
		return fmt.Errorf("semantic anchor edge metadata missing identity")
	}
	if !validRelation(meta.Relation) || !validValidationStatus(meta.ValidationStatus) {
		return fmt.Errorf("semantic anchor edge metadata invalid enum")
	}
	if _, _, ok := DecodeAnchorTargetNodeID(meta.RepoKey + "::" + string(meta.TargetID)); !ok {
		return fmt.Errorf("semantic anchor edge metadata invalid target")
	}
	return nil
}

func (r AnchorResolution) OccurrenceOwnerRepoKey() string {
	return repoKeyFromNodeID(r.Occurrence.OwnerBinding.OwnerNodeID)
}

func semanticAnchorEvidenceMeta() evidence.EvidenceMeta {
	return evidence.EvidenceMeta{
		Source:            evidence.EvidenceSourceSemanticAnchor,
		SourcePlane:       evidence.EvidencePlaneSemanticAnchor,
		EvidenceClass:     evidence.EvidenceClassSourceComment,
		EvidenceAuthority: evidence.EvidenceAuthorityEvidenceOnly,
		AllowedAuthorityEffects: []evidence.AuthorityEffect{
			evidence.AuthorityEffectRetrievalRanking,
			evidence.AuthorityEffectReviewSignal,
		},
	}
}

var (
	conceptSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)
	scopeSlugRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	emailRE       = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	tokenRE       = regexp.MustCompile(`(?i)(api[_-]?key|secret|token|bearer|ghp_|sk-[a-z0-9])`)
	sessionRE     = regexp.MustCompile(`(?i)(session|sess|conversation|thread)[_-]?[a-z0-9]{8,}`)
)

func unsafeTargetReason(target string) (AnchorFindingReason, bool) {
	if strings.ContainsAny(target, "\x00\n\r\t") || strings.IndexFunc(target, unicode.IsControl) >= 0 {
		return AnchorFindingControlChar, true
	}
	if strings.Contains(target, `\`) {
		return AnchorFindingBackslashPath, true
	}
	if strings.Contains(target, "$") || strings.Contains(target, "${") || strings.Contains(target, "%") {
		return AnchorFindingEnvVarExpansion, true
	}
	if strings.HasPrefix(target, "/") || regexp.MustCompile(`^[A-Za-z]:`).MatchString(target) {
		return AnchorFindingAbsolutePath, true
	}
	if strings.Contains(target, "../") || strings.HasPrefix(target, "..") || strings.Contains(target, "/..") {
		return AnchorFindingPathTraversal, true
	}
	if strings.Contains(target, "::") {
		return AnchorFindingNamespaceCollision, true
	}
	if u, err := url.Parse(target); err == nil && u.Scheme != "" {
		return AnchorFindingUnsafeURL, true
	}
	if emailRE.MatchString(target) {
		return AnchorFindingPIILike, true
	}
	if tokenRE.MatchString(target) {
		return AnchorFindingSecretLike, true
	}
	if sessionRE.MatchString(target) {
		return AnchorFindingSessionLike, true
	}
	return "", false
}

func invalidOccurrence(reason AnchorFindingReason) AnchorOccurrence {
	return AnchorOccurrence{
		DisplaySyntax:    fmt.Sprintf("[[redacted:%s]]", reason),
		TargetDisplay:    fmt.Sprintf("[redacted:%s]", reason),
		ValidationStatus: AnchorValidationLintError,
	}
}

func newFinding(reason AnchorFindingReason, severity AnchorFindingSeverity) Finding {
	return Finding{ID: "anchor-finding:" + string(reason), Reason: reason, Severity: severity, Message: "semantic anchor " + string(reason)}
}

func lintResolution(occ AnchorOccurrence, reason AnchorFindingReason) AnchorResolution {
	occ.ValidationStatus = AnchorValidationLintError
	occ.Findings = append(occ.Findings, newFinding(reason, AnchorFindingError))
	return AnchorResolution{Occurrence: occ, Relation: SemanticAnchorRelationDeclaresTarget, EdgeAction: AnchorEdgeNone}
}

func maxTargetBytes(policy AnchorPolicy) int {
	if policy.MaxTargetBytes <= 0 {
		return DefaultMaxTargetBytes
	}
	return policy.MaxTargetBytes
}

func repoKeySlug(repoKey string) string {
	slug := strings.ToLower(strings.TrimSpace(repoKey))
	slug = strings.Trim(slug, "/")
	if strings.Contains(slug, "/") {
		parts := strings.Split(slug, "/")
		slug = parts[len(parts)-1]
	}
	slug = strings.TrimSuffix(slug, ".git")
	if scopeSlugRE.MatchString(slug) {
		return slug
	}
	return ""
}

func sourceHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func stableOccurrenceID(repoKey string, occ AnchorOccurrence, relation SemanticAnchorRelation, ownerStableKey string) string {
	if repoKey == "" || ownerStableKey == "" || occ.TargetID == "" {
		return occ.OccurrenceID
	}
	return sourceHash(repoKey, ownerStableKey, string(relation), string(occ.TargetID), occ.DisplaySyntax, fmt.Sprintf("%d", SemanticAnchorEdgeMetaSchemaVersion))
}

func findingIDs(findings []Finding) []string {
	ids := make([]string, 0, len(findings))
	for _, finding := range findings {
		if finding.ID != "" {
			ids = append(ids, finding.ID)
		}
	}
	return ids
}

func maxSeverity(findings []Finding) AnchorFindingSeverity {
	if len(findings) == 0 {
		return ""
	}
	if slices.ContainsFunc(findings, func(f Finding) bool { return f.Severity == AnchorFindingError }) {
		return AnchorFindingError
	}
	if slices.ContainsFunc(findings, func(f Finding) bool { return f.Severity == AnchorFindingWarning }) {
		return AnchorFindingWarning
	}
	return AnchorFindingInfo
}

func repoKeyFromNodeID(nodeID string) string {
	repoKey, _, ok := strings.Cut(nodeID, "::")
	if !ok {
		return ""
	}
	return repoKey
}

func validRelation(rel SemanticAnchorRelation) bool {
	switch rel {
	case SemanticAnchorRelationEnforces, SemanticAnchorRelationProtectsAgainst, SemanticAnchorRelationVerifiedBy,
		SemanticAnchorRelationDescribedBy, SemanticAnchorRelationDecidedBy, SemanticAnchorRelationImplementsProtocol,
		SemanticAnchorRelationParticipatesIn, SemanticAnchorRelationDeclaresTarget:
		return true
	default:
		return false
	}
}

func validValidationStatus(status AnchorValidationStatus) bool {
	switch status {
	case AnchorValidationEvidenceOnly, AnchorValidationValidReference, AnchorValidationMissingTarget, AnchorValidationLintError:
		return true
	default:
		return false
	}
}
