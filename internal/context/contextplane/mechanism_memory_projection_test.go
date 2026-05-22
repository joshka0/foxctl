package contextplane

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage"
)

func TestPlanMechanismMemoryArtifactsSeparatesLiteralAndStructuralEmbeddingText(t *testing.T) {
	projection := MechanismProjection{
		ID:             "immune-edge-response",
		WorkspaceID:    "ws-foxctl",
		OriginalDomain: "Immunology",
		Summary:        "White cells respond near an infection without central approval.",
		LiteralText:    "White blood cells detect a virus in tissue, clone locally, and deploy antibodies.",
		AbstractSchema: "Decentralized edge-node threat response with local replication and bounded escalation.",
		MechanismTags:  []string{"local_response", "edge_defense"},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeNote, Ref: "notes/memory/immunology.md"},
		},
		Tags: []string{"mechanism", "edge-response"},
	}

	artifacts, err := PlanMechanismMemoryArtifacts(projection)
	if err != nil {
		t.Fatalf("PlanMechanismMemoryArtifacts: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected literal and structural artifacts, got %d", len(artifacts))
	}

	literal := artifacts[0]
	structural := artifacts[1]
	if literal.Type != MechanismMemoryLiteralType || literal.View != MechanismMemoryViewLiteral {
		t.Fatalf("unexpected literal artifact: %#v", literal)
	}
	if structural.Type != MechanismMemoryStructuralType || structural.View != MechanismMemoryViewStructural {
		t.Fatalf("unexpected structural artifact: %#v", structural)
	}
	if !strings.Contains(literal.EmbeddingText, "Immunology") || !strings.Contains(literal.EmbeddingText, "White blood cells") {
		t.Fatalf("literal embedding text lost domain details:\n%s", literal.EmbeddingText)
	}
	if strings.Contains(structural.EmbeddingText, "Immunology") || strings.Contains(structural.EmbeddingText, "White blood cells") || strings.Contains(structural.EmbeddingText, "virus") {
		t.Fatalf("structural embedding text leaked literal domain details:\n%s", structural.EmbeddingText)
	}
	if !strings.Contains(structural.EmbeddingText, projection.AbstractSchema) {
		t.Fatalf("structural embedding text missing abstract schema:\n%s", structural.EmbeddingText)
	}
	decoded, view, ok := DecodeMechanismMemoryArtifact(storage.NamedEntry{
		Type:   structural.Type,
		Result: structural.Result,
	})
	if !ok || view != MechanismMemoryViewStructural {
		t.Fatalf("decode structural artifact failed: view=%q ok=%v", view, ok)
	}
	if !containsString(decoded.MechanismTags, "local_response") || !containsString(decoded.MechanismTags, "edge_defense") {
		t.Fatalf("mechanism tags not persisted: %#v", decoded.MechanismTags)
	}
}

func TestMechanismMemoryFromArtifactsJoinsViewsAndPreservesVectorsAndRefs(t *testing.T) {
	projection := MechanismProjection{
		ID:             "firebreak-rollout",
		WorkspaceID:    "ws-foxctl",
		OriginalDomain: "Forest Management",
		Summary:        "Firebreaks isolate spread while controlled burns continue.",
		AbstractSchema: "Compartmentalized propagation with bounded local failure.",
		MechanismTags:  []string{"compartmentalized_propagation"},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeMemoryClaim, Ref: "claim-firebreaks"},
		},
	}
	artifacts, err := PlanMechanismMemoryArtifacts(projection)
	if err != nil {
		t.Fatalf("PlanMechanismMemoryArtifacts: %v", err)
	}
	literalEntry := storage.NamedEntry{
		Name:    artifacts[0].Name,
		Type:    artifacts[0].Type,
		Summary: artifacts[0].Summary,
		Result:  artifacts[0].Result,
	}
	structuralEntry := storage.NamedEntry{
		Name:    artifacts[1].Name,
		Type:    artifacts[1].Type,
		Summary: artifacts[1].Summary,
		Result:  artifacts[1].Result,
	}

	memory, ok := MechanismMemoryFromArtifacts(literalEntry, structuralEntry, []float32{1, 0}, []float32{0, 1})
	if !ok {
		t.Fatal("expected joined mechanism memory")
	}
	if memory.ID != projection.ID || memory.OriginalDomain != projection.OriginalDomain {
		t.Fatalf("unexpected memory identity: %#v", memory)
	}
	if len(memory.LiteralVector) != 2 || len(memory.StructuralVector) != 2 {
		t.Fatalf("vectors not preserved: %#v", memory)
	}
	if len(memory.SourceRefs) != 1 || memory.SourceRefs[0].Ref != "claim-firebreaks" {
		t.Fatalf("source refs not preserved: %#v", memory.SourceRefs)
	}
	if !containsString(memory.MechanismTags, "compartmentalized_propagation") {
		t.Fatalf("mechanism tags not preserved: %#v", memory.MechanismTags)
	}
}

func TestPlanMechanismMemoryArtifactsRequiresEvidence(t *testing.T) {
	_, err := PlanMechanismMemoryArtifacts(MechanismProjection{
		ID:             "missing-evidence",
		OriginalDomain: "Operations",
		Summary:        "Summary",
		AbstractSchema: "Structure",
	})
	if err == nil || !strings.Contains(err.Error(), "source_refs") {
		t.Fatalf("expected source_refs error, got %v", err)
	}
}
