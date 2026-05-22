package contextplane

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestBlurMemoryProjectionBuildsStructuralSchemaWithoutLiteralDetails(t *testing.T) {
	projection, err := BlurMemoryProjection(MemoryBlurInput{
		ID:             "immune-edge-response",
		WorkspaceID:    "ws",
		OriginalDomain: "Immunology",
		Summary:        "White blood cells clone near a virus infection.",
		LiteralText:    "White blood cells detect a virus and deploy antibodies.",
		Shape: MemoryStructuralShape{
			Mechanism:   "distributed local anomaly response",
			Actors:      []string{"edge detector", "local responder"},
			Operations:  []string{"detect local anomaly", "replicate capacity near event", "resolve without central coordination"},
			Flows:       []string{"local signal triggers nearby response"},
			Constraints: []string{"central coordination unavailable"},
		},
		SourceRefs: []contextengine.EvidenceRef{{Type: contextengine.RefTypeNote, Ref: "immune-note"}},
	})
	if err != nil {
		t.Fatalf("BlurMemoryProjection: %v", err)
	}

	for _, literal := range []string{"Immunology", "White blood", "virus", "antibodies"} {
		if strings.Contains(projection.AbstractSchema, literal) {
			t.Fatalf("abstract schema leaked literal detail %q:\n%s", literal, projection.AbstractSchema)
		}
	}
	if !strings.Contains(projection.LiteralText, "White blood cells") {
		t.Fatalf("literal text should preserve provenance details: %q", projection.LiteralText)
	}
	if !containsString(projection.Tags, memoryBlurTag) {
		t.Fatalf("projection tags missing %q: %v", memoryBlurTag, projection.Tags)
	}

	artifacts, err := PlanMechanismMemoryArtifacts(projection)
	if err != nil {
		t.Fatalf("PlanMechanismMemoryArtifacts: %v", err)
	}
	structural := artifacts[1]
	if structural.Type != MechanismMemoryStructuralType {
		t.Fatalf("artifact[1].type=%q want %q", structural.Type, MechanismMemoryStructuralType)
	}
	if strings.Contains(structural.EmbeddingText, "White blood") || strings.Contains(structural.EmbeddingText, "virus") {
		t.Fatalf("structural embedding text leaked literal details:\n%s", structural.EmbeddingText)
	}
}

func TestBlurMemoryProjectionRequiresTypedShape(t *testing.T) {
	_, err := BlurMemoryProjection(MemoryBlurInput{
		ID:             "empty",
		OriginalDomain: "Domain",
		Summary:        "summary",
		SourceRefs:     []contextengine.EvidenceRef{{Type: contextengine.RefTypeNote, Ref: "note"}},
	})
	if err == nil {
		t.Fatalf("expected missing structural shape error")
	}
}

func TestGraphShapeVectorIsStableAndDirectional(t *testing.T) {
	shape := MemoryGraphShape{
		NodeKind: "symbol",
		Outgoing: map[string]int{
			"CALLS":     3,
			"REFERS_TO": 1,
		},
		Incoming: map[string]int{
			"CALLS": 1,
		},
		NeighborMix: 4,
	}
	order := []string{"CALLS", "REFERS_TO"}
	first := GraphShapeVector(shape, order)
	second := GraphShapeVector(shape, order)
	if len(first) != len(order)*2+5 {
		t.Fatalf("vector len=%d want %d", len(first), len(order)*2+5)
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("vector not stable at %d: %v vs %v", i, first, second)
		}
	}
	if first[0] == first[len(order)] {
		t.Fatalf("directional features collapsed: %v", first)
	}
}
