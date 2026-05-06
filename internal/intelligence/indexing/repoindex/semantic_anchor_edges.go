package repoindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/evidence"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
)

type semanticAnchorEdgeValidationReason string

const (
	semanticAnchorEdgeMetaKind                                             = "semantic_anchor_edge"
	semanticAnchorEdgeInvalidMeta       semanticAnchorEdgeValidationReason = "invalid_meta"
	semanticAnchorEdgeIllegalAuthority  semanticAnchorEdgeValidationReason = "illegal_authority"
	semanticAnchorEdgeMismatchedEdge    semanticAnchorEdgeValidationReason = "mismatched_edge"
	semanticAnchorEdgeMissingTargetEdge semanticAnchorEdgeValidationReason = "missing_target_edge"
)

type semanticAnchorEdgeValidationError struct {
	reason semanticAnchorEdgeValidationReason
	err    error
}

func (e semanticAnchorEdgeValidationError) Error() string {
	if e.err != nil {
		return fmt.Sprintf("semantic anchor edge validation failed: %s: %v", e.reason, e.err)
	}
	return "semantic anchor edge validation failed: " + string(e.reason)
}

func (e semanticAnchorEdgeValidationError) Unwrap() error {
	return e.err
}

// EdgeTypeForSemanticAnchorRelation maps the semantic-anchor relation contract
// to repoindex edge vocabulary.
func EdgeTypeForSemanticAnchorRelation(rel semanticanchors.SemanticAnchorRelation) (EdgeType, bool) {
	switch rel {
	case semanticanchors.SemanticAnchorRelationEnforces:
		return EdgeEnforces, true
	case semanticanchors.SemanticAnchorRelationProtectsAgainst:
		return EdgeProtectsAgainst, true
	case semanticanchors.SemanticAnchorRelationVerifiedBy:
		return EdgeVerifiedBy, true
	case semanticanchors.SemanticAnchorRelationDescribedBy:
		return EdgeDescribedBy, true
	case semanticanchors.SemanticAnchorRelationDecidedBy:
		return EdgeDecidedBy, true
	case semanticanchors.SemanticAnchorRelationImplementsProtocol:
		return EdgeImplementsProtocol, true
	case semanticanchors.SemanticAnchorRelationParticipatesIn:
		return EdgeParticipatesIn, true
	case semanticanchors.SemanticAnchorRelationDeclaresTarget:
		return EdgeDeclaresAnchorTarget, true
	default:
		return "", false
	}
}

// NewSemanticAnchorEdge converts a resolved semantic anchor into a repo graph
// edge. It consumes the resolved relation/action and does not re-resolve the raw
// anchor type.
func NewSemanticAnchorEdge(res semanticanchors.AnchorResolution, owner semanticanchors.AnchorOwner) (Edge, error) {
	switch res.EdgeAction {
	case semanticanchors.AnchorEdgeSemantic, semanticanchors.AnchorEdgeMissingTarget:
	case semanticanchors.AnchorEdgeNone:
		return Edge{}, fmt.Errorf("semantic anchor resolution has no edge action")
	default:
		return Edge{}, fmt.Errorf("semantic anchor resolution has unknown edge action %q", res.EdgeAction)
	}
	edgeType, ok := EdgeTypeForSemanticAnchorRelation(res.Relation)
	if !ok || edgeType == EdgeBeaconFor {
		return Edge{}, fmt.Errorf("semantic anchor relation %q cannot be emitted as repoindex edge", res.Relation)
	}
	meta, err := semanticanchors.NewSemanticAnchorEdgeMeta(res, owner)
	if err != nil {
		return Edge{}, err
	}
	dst, err := semanticanchors.AnchorTargetNodeID(meta.RepoKey, meta.TargetID)
	if err != nil {
		return Edge{}, err
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return Edge{}, err
	}
	edge := Edge{
		Src:    owner.NodeID,
		Dst:    string(dst),
		Type:   edgeType,
		Weight: 0.75,
		Meta:   raw,
	}
	if err := ValidateSemanticAnchorEdge(edge); err != nil {
		return Edge{}, err
	}
	return edge, nil
}

func DecodeAndValidateSemanticAnchorEdge(edge Edge) (semanticanchors.SemanticAnchorEdgeMeta, bool, error) {
	if len(edge.Meta) == 0 {
		if isSemanticAnchorEdgeType(edge.Type) {
			return semanticanchors.SemanticAnchorEdgeMeta{}, true, semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeInvalidMeta, err: fmt.Errorf("missing semantic anchor metadata")}
		}
		return semanticanchors.SemanticAnchorEdgeMeta{}, false, nil
	}
	present, err := semanticAnchorMetadataPresent(edge)
	if err != nil {
		return semanticanchors.SemanticAnchorEdgeMeta{}, present, err
	}
	if !present {
		return semanticanchors.SemanticAnchorEdgeMeta{}, false, nil
	}
	meta, err := semanticanchors.DecodeSemanticAnchorEdgeMeta(edge.Meta)
	if err != nil {
		return semanticanchors.SemanticAnchorEdgeMeta{}, true, semanticAnchorEdgeValidationError{reason: classifySemanticAnchorMetaError(err), err: err}
	}
	if err := validateDecodedSemanticAnchorEdge(edge, meta); err != nil {
		return semanticanchors.SemanticAnchorEdgeMeta{}, true, err
	}
	return meta, true, nil
}

func ValidateSemanticAnchorEdge(edge Edge) error {
	_, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if !present {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeInvalidMeta, err: fmt.Errorf("missing semantic anchor metadata")}
	}
	return err
}

type SemanticProjectionWarningReason string

const (
	SemanticProjectionWarningInvalidMeta       SemanticProjectionWarningReason = "invalid_meta"
	SemanticProjectionWarningIllegalAuthority  SemanticProjectionWarningReason = "illegal_authority"
	SemanticProjectionWarningMismatchedEdge    SemanticProjectionWarningReason = "mismatched_edge"
	SemanticProjectionWarningMissingTargetEdge SemanticProjectionWarningReason = "missing_target_edge"
)

type SemanticProjectionWarning struct {
	EdgeID  string                          `json:"edge_id"`
	Reason  SemanticProjectionWarningReason `json:"reason"`
	Message string                          `json:"message,omitempty"`
}

type ValidatedSemanticProjection struct {
	Edges    []Edge                      `json:"edges"`
	Warnings []SemanticProjectionWarning `json:"warnings,omitempty"`
}

func SemanticProjectionEdgeID(edge Edge) string {
	return edge.Src + "|" + edge.Dst + "|" + string(edge.Type)
}

func FilterValidSemanticAnchorEdges(edges []Edge) ([]Edge, []SemanticProjectionWarning) {
	valid := make([]Edge, 0, len(edges))
	var warnings []SemanticProjectionWarning
	for _, edge := range edges {
		_, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
		if !present {
			continue
		}
		if err != nil {
			warnings = append(warnings, SemanticProjectionWarning{
				EdgeID:  SemanticProjectionEdgeID(edge),
				Reason:  semanticProjectionReason(err),
				Message: err.Error(),
			})
			continue
		}
		valid = append(valid, edge)
	}
	return valid, warnings
}

func ProjectSemanticAnchorEdges(edges []Edge) ValidatedSemanticProjection {
	valid, warnings := FilterValidSemanticAnchorEdges(edges)
	return ValidatedSemanticProjection{Edges: valid, Warnings: warnings}
}

func validateDecodedSemanticAnchorEdge(edge Edge, meta semanticanchors.SemanticAnchorEdgeMeta) error {
	wantType, ok := EdgeTypeForSemanticAnchorRelation(meta.Relation)
	if !ok || edge.Type != wantType {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeMismatchedEdge}
	}
	if edge.Src != meta.OwnerNodeID {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeMismatchedEdge}
	}
	wantDst, err := semanticanchors.AnchorTargetNodeID(meta.RepoKey, meta.TargetID)
	if err != nil {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeInvalidMeta, err: err}
	}
	if edge.Dst != string(wantDst) {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeMismatchedEdge}
	}
	if meta.SourcePlane != evidence.EvidencePlaneSemanticAnchor ||
		meta.EvidenceAuthority != evidence.EvidenceAuthorityEvidenceOnly {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeIllegalAuthority}
	}
	for _, effect := range meta.AllowedAuthorityEffects {
		if effect == evidence.AuthorityEffectInstructionSource {
			return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeIllegalAuthority}
		}
	}
	if meta.ValidationStatus == semanticanchors.AnchorValidationMissingTarget &&
		(meta.Relation != semanticanchors.SemanticAnchorRelationDeclaresTarget || edge.Type != EdgeDeclaresAnchorTarget) {
		return semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeMissingTargetEdge}
	}
	return nil
}

func semanticAnchorMetadataPresent(edge Edge) (bool, error) {
	var header struct {
		MetaKind string `json:"meta_kind"`
	}
	if err := json.Unmarshal(edge.Meta, &header); err != nil {
		if isSemanticAnchorEdgeType(edge.Type) {
			return true, semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeInvalidMeta, err: err}
		}
		return false, nil
	}
	if header.MetaKind == semanticAnchorEdgeMetaKind {
		return true, nil
	}
	if isSemanticAnchorEdgeType(edge.Type) {
		return true, semanticAnchorEdgeValidationError{reason: semanticAnchorEdgeInvalidMeta, err: fmt.Errorf("missing semantic anchor metadata kind")}
	}
	return false, nil
}

func isSemanticAnchorEdgeType(edgeType EdgeType) bool {
	switch edgeType {
	case EdgeEnforces, EdgeProtectsAgainst, EdgeVerifiedBy, EdgeDescribedBy,
		EdgeDecidedBy, EdgeImplementsProtocol, EdgeParticipatesIn, EdgeDeclaresAnchorTarget:
		return true
	default:
		return false
	}
}

func classifySemanticAnchorMetaError(err error) semanticAnchorEdgeValidationReason {
	var evidenceErr evidence.ValidationError
	if errors.As(err, &evidenceErr) {
		return semanticAnchorEdgeIllegalAuthority
	}
	return semanticAnchorEdgeInvalidMeta
}

func semanticProjectionReason(err error) SemanticProjectionWarningReason {
	var edgeErr semanticAnchorEdgeValidationError
	if errors.As(err, &edgeErr) {
		switch edgeErr.reason {
		case semanticAnchorEdgeIllegalAuthority:
			return SemanticProjectionWarningIllegalAuthority
		case semanticAnchorEdgeMismatchedEdge:
			return SemanticProjectionWarningMismatchedEdge
		case semanticAnchorEdgeMissingTargetEdge:
			return SemanticProjectionWarningMissingTargetEdge
		default:
			return SemanticProjectionWarningInvalidMeta
		}
	}
	return SemanticProjectionWarningInvalidMeta
}

// RawNodeID returns the un-namespaced repo graph node ID.
func RawNodeID(id string) string {
	_, raw := SplitNamespacedID(id)
	return raw
}

// IsAnchorConceptNode reports whether node is a semantic-anchor concept node.
func IsAnchorConceptNode(node Node) bool {
	return node.Kind == NodeConcept && strings.HasPrefix(RawNodeID(node.ID), "anchor:")
}
