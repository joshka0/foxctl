package repoindex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/evidence"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
)

func TestNewSemanticAnchorEdgeValidSemanticEdge(t *testing.T) {
	edge := mustSemanticAnchorEdge(t, "[[foxctl:invariant/no-send-without-read]]", nil)
	if edge.Type != EdgeEnforces {
		t.Fatalf("edge.Type=%q want %q", edge.Type, EdgeEnforces)
	}
	if err := ValidateSemanticAnchorEdge(edge); err != nil {
		t.Fatal(err)
	}
	meta, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if err != nil || !present {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge()=(%+v,%v,%v)", meta, present, err)
	}
	if meta.Relation != semanticanchors.SemanticAnchorRelationEnforces || meta.OwnerNodeID != edge.Src || string(meta.TargetID) == "" {
		t.Fatalf("unexpected meta: %+v edge=%+v", meta, edge)
	}
}

func TestNewSemanticAnchorEdgeValidMissingTargetEdge(t *testing.T) {
	edge := mustSemanticAnchorEdge(t, "[[doc:docs/missing.md#Nope]]", semanticAnchorTestResolver{status: semanticanchors.AnchorValidationMissingTarget})
	if edge.Type != EdgeDeclaresAnchorTarget {
		t.Fatalf("edge.Type=%q want %q", edge.Type, EdgeDeclaresAnchorTarget)
	}
	meta, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if err != nil || !present {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge()=(%+v,%v,%v)", meta, present, err)
	}
	if meta.Relation != semanticanchors.SemanticAnchorRelationDeclaresTarget || meta.ValidationStatus != semanticanchors.AnchorValidationMissingTarget {
		t.Fatalf("unexpected missing target meta: %+v", meta)
	}
}

func TestNewSemanticAnchorEdgeNoneReturnsNoEdge(t *testing.T) {
	owner := semanticAnchorTestOwner()
	res := semanticAnchorResolution(t, "[[doc:docs/bad.md]]", semanticAnchorTestResolver{status: semanticanchors.AnchorValidationLintError})
	if res.EdgeAction != semanticanchors.AnchorEdgeNone {
		t.Fatalf("EdgeAction=%q want none", res.EdgeAction)
	}
	if _, err := NewSemanticAnchorEdge(res, owner); err == nil {
		t.Fatal("NewSemanticAnchorEdge accepted AnchorEdgeNone")
	}
}

func TestDecodeAndValidateSemanticAnchorEdgeMalformedMetadata(t *testing.T) {
	edge := Edge{Src: "foxctl::symbol:Guard", Dst: "foxctl::anchor:repo:invariant:x", Type: EdgeEnforces, Meta: []byte(`{"meta_kind":`)}
	_, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if !present || err == nil {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge() present=%v err=%v, want present error", present, err)
	}
	projection := ProjectSemanticAnchorEdges([]Edge{edge})
	assertWarningReason(t, projection.Warnings, SemanticProjectionWarningInvalidMeta)
}

func TestDecodeAndValidateSemanticAnchorEdgeIgnoresNonSemanticMetadata(t *testing.T) {
	raw := Edge{Src: "a", Dst: "b", Type: EdgeCalls, Meta: []byte(`{"meta_kind":"doc_index"}`)}
	_, present, err := DecodeAndValidateSemanticAnchorEdge(raw)
	if err != nil || present {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge() present=%v err=%v, want ignored", present, err)
	}
	projection := ProjectSemanticAnchorEdges([]Edge{raw})
	if len(projection.Edges) != 0 || len(projection.Warnings) != 0 {
		t.Fatalf("projection=%+v, want ignored non-semantic metadata", projection)
	}
}

func TestDecodeAndValidateSemanticAnchorEdgeWarnsSemanticTypeWithoutMetadataKind(t *testing.T) {
	edge := Edge{Src: "a", Dst: "b", Type: EdgeEnforces, Meta: []byte(`{"meta_kind":"doc_index"}`)}
	_, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if !present || err == nil {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge() present=%v err=%v, want semantic invalid meta", present, err)
	}
	projection := ProjectSemanticAnchorEdges([]Edge{edge})
	assertWarningReason(t, projection.Warnings, SemanticProjectionWarningInvalidMeta)
}

func TestDecodeAndValidateSemanticAnchorEdgeRejectsInstructionSource(t *testing.T) {
	edge := mustSemanticAnchorEdge(t, "[[foxctl:invariant/no-send-without-read]]", nil)
	meta := decodeSemanticAnchorTestMeta(t, edge)
	meta.AllowedAuthorityEffects = append(meta.AllowedAuthorityEffects, evidence.AuthorityEffectInstructionSource)
	edge.Meta = marshalSemanticAnchorTestMeta(t, meta)
	_, present, err := DecodeAndValidateSemanticAnchorEdge(edge)
	if !present || err == nil {
		t.Fatalf("DecodeAndValidateSemanticAnchorEdge() present=%v err=%v, want illegal authority", present, err)
	}
	projection := ProjectSemanticAnchorEdges([]Edge{edge})
	assertWarningReason(t, projection.Warnings, SemanticProjectionWarningIllegalAuthority)
}

func TestValidateSemanticAnchorEdgeRejectsEdgeMismatches(t *testing.T) {
	valid := mustSemanticAnchorEdge(t, "[[foxctl:invariant/no-send-without-read]]", nil)
	tests := []struct {
		name string
		edge Edge
	}{
		{name: "type", edge: withSemanticAnchorTestType(valid, EdgeProtectsAgainst)},
		{name: "src", edge: withSemanticAnchorTestSrc(valid, "foxctl::symbol:internal/other.go:Guard")},
		{name: "dst", edge: withSemanticAnchorTestDst(valid, "foxctl::anchor:repo:invariant:other")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSemanticAnchorEdge(tt.edge); err == nil {
				t.Fatal("ValidateSemanticAnchorEdge accepted mismatched edge")
			}
			projection := ProjectSemanticAnchorEdges([]Edge{tt.edge})
			assertWarningReason(t, projection.Warnings, SemanticProjectionWarningMismatchedEdge)
		})
	}
}

func TestProjectionWarnsMissingTargetMismatch(t *testing.T) {
	edge := mustSemanticAnchorEdge(t, "[[doc:docs/missing.md#Nope]]", semanticAnchorTestResolver{status: semanticanchors.AnchorValidationMissingTarget})
	meta := decodeSemanticAnchorTestMeta(t, edge)
	meta.Relation = semanticanchors.SemanticAnchorRelationDescribedBy
	edge.Type = EdgeDescribedBy
	edge.Meta = marshalSemanticAnchorTestMeta(t, meta)
	projection := ProjectSemanticAnchorEdges([]Edge{edge})
	if len(projection.Edges) != 0 {
		t.Fatalf("projection kept invalid missing-target edge: %+v", projection.Edges)
	}
	assertWarningReason(t, projection.Warnings, SemanticProjectionWarningMissingTargetEdge)
}

func TestProjectSemanticAnchorEdgesReturnsValidEdgesAndStableWarnings(t *testing.T) {
	valid := mustSemanticAnchorEdge(t, "[[foxctl:invariant/no-send-without-read]]", nil)
	badMeta := withSemanticAnchorTestMeta(valid, []byte(`{`))
	badDst := withSemanticAnchorTestDst(valid, "foxctl::anchor:repo:invariant:other")
	raw := Edge{Src: "a", Dst: "b", Type: EdgeCalls, Meta: []byte(`{"meta_kind":"doc_index"}`)}
	projection := ProjectSemanticAnchorEdges([]Edge{valid, badMeta, badDst, raw})
	if len(projection.Edges) != 1 || projection.Edges[0].Src != valid.Src || projection.Edges[0].Dst != valid.Dst {
		t.Fatalf("projection.Edges=%+v want only valid semantic edge", projection.Edges)
	}
	if got := SemanticProjectionEdgeID(valid); got != valid.Src+"|"+valid.Dst+"|"+string(valid.Type) {
		t.Fatalf("SemanticProjectionEdgeID()=%q", got)
	}
	if len(projection.Warnings) != 2 {
		t.Fatalf("warnings=%+v want 2", projection.Warnings)
	}
	assertWarningReason(t, projection.Warnings[:1], SemanticProjectionWarningInvalidMeta)
	assertWarningReason(t, projection.Warnings[1:], SemanticProjectionWarningMismatchedEdge)
}

type semanticAnchorTestResolver struct {
	status semanticanchors.AnchorValidationStatus
}

func (r semanticAnchorTestResolver) ResolveAnchorTarget(context.Context, semanticanchors.AnchorOccurrence) (semanticanchors.TargetResolution, error) {
	if r.status == semanticanchors.AnchorValidationMissingTarget {
		finding := semanticanchors.Finding{ID: "missing", Reason: semanticanchors.AnchorFindingMissingTarget, Severity: semanticanchors.AnchorFindingWarning}
		return semanticanchors.TargetResolution{Status: r.status, Finding: &finding}, nil
	}
	return semanticanchors.TargetResolution{Status: r.status}, nil
}

func mustSemanticAnchorEdge(t *testing.T, syntax string, resolver semanticanchors.TargetResolver) Edge {
	t.Helper()
	edge, err := NewSemanticAnchorEdge(semanticAnchorResolution(t, syntax, resolver), semanticAnchorTestOwner())
	if err != nil {
		t.Fatal(err)
	}
	return edge
}

func semanticAnchorResolution(t *testing.T, syntax string, resolver semanticanchors.TargetResolver) semanticanchors.AnchorResolution {
	t.Helper()
	policy := semanticanchors.DefaultAnchorPolicy("foxctl", nil)
	occ, findings := semanticanchors.ParseInlineAnchor(policy, syntax)
	if len(findings) != 0 {
		t.Fatalf("ParseInlineAnchor findings=%+v", findings)
	}
	owner := semanticAnchorTestOwner()
	occ.OwnerBinding = semanticanchors.AnchorOwnerBinding{OwnerNodeID: owner.NodeID, OwnerKind: owner.Kind, OwnerStableKey: owner.StableKey}
	res, err := semanticanchors.ResolveAnchorOccurrence(context.Background(), policy, occ, resolver)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func semanticAnchorTestOwner() semanticanchors.AnchorOwner {
	return semanticanchors.AnchorOwner{NodeID: "foxctl::symbol:internal/foo.go:Guard", Kind: "symbol", StableKey: "Guard"}
}

func decodeSemanticAnchorTestMeta(t *testing.T, edge Edge) semanticanchors.SemanticAnchorEdgeMeta {
	t.Helper()
	meta, err := semanticanchors.DecodeSemanticAnchorEdgeMeta(edge.Meta)
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func marshalSemanticAnchorTestMeta(t *testing.T, meta semanticanchors.SemanticAnchorEdgeMeta) []byte {
	t.Helper()
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func withSemanticAnchorTestType(edge Edge, edgeType EdgeType) Edge {
	edge.Type = edgeType
	return edge
}

func withSemanticAnchorTestSrc(edge Edge, src string) Edge {
	edge.Src = src
	return edge
}

func withSemanticAnchorTestDst(edge Edge, dst string) Edge {
	edge.Dst = dst
	return edge
}

func withSemanticAnchorTestMeta(edge Edge, meta []byte) Edge {
	edge.Meta = meta
	return edge
}

func assertWarningReason(t *testing.T, warnings []SemanticProjectionWarning, want SemanticProjectionWarningReason) {
	t.Helper()
	if len(warnings) != 1 {
		t.Fatalf("warnings=%+v want one %s warning", warnings, want)
	}
	if warnings[0].Reason != want {
		t.Fatalf("warning reason=%q want %q; warning=%+v", warnings[0].Reason, want, warnings[0])
	}
	if warnings[0].EdgeID == "" {
		t.Fatalf("warning missing stable edge ID: %+v", warnings[0])
	}
}
