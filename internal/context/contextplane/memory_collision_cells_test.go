package contextplane

import (
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestPlanMemoryCollisionCellsPrefersCrossDomainStructuralMatch(t *testing.T) {
	query := MechanismQuery{
		ID:               "q-traffic",
		Domain:           "Urban Logistics",
		Text:             "Emergency vehicles need local intersection clearance without a central controller.",
		LiteralVector:    []float32{1, 0},
		StructuralVector: []float32{0, 1},
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "retrieval_episode:ep-traffic"},
		},
	}
	memories := []MechanismMemory{
		{
			ID:               "mem-immunology",
			OriginalDomain:   "Immunology",
			Summary:          "White cells coordinate local response without central approval.",
			AbstractSchema:   "Decentralized edge-node threat response with local replication.",
			LiteralVector:    []float32{0, 1},
			StructuralVector: []float32{0, 1},
			SourceRefs: []contextengine.EvidenceRef{
				{Type: contextengine.RefTypeMemoryClaim, Ref: "claim-immunology"},
			},
		},
		{
			ID:               "mem-traffic",
			OriginalDomain:   "Urban Logistics",
			Summary:          "Traffic lights coordinate emergency lanes.",
			AbstractSchema:   "Same-domain route clearance.",
			LiteralVector:    []float32{1, 0},
			StructuralVector: []float32{0, 1},
		},
		{
			ID:               "mem-weak",
			OriginalDomain:   "Mycology",
			Summary:          "Weak structural match.",
			AbstractSchema:   "Centralized scheduling.",
			LiteralVector:    []float32{0, 1},
			StructuralVector: []float32{1, 0},
		},
	}

	plan := PlanMemoryCollisionCells(MemoryCollisionInput{
		WorkspaceID: "ws-foxctl",
		Query:       query,
		Memories:    memories,
		Entropy:     0.4,
		Threshold:   0.70,
	})

	if len(plan.Cells) != 1 {
		t.Fatalf("expected one collision cell, got %d (skipped=%d)", len(plan.Cells), plan.Skipped)
	}
	cell := plan.Cells[0]
	if cell.MemoryID != "mem-immunology" {
		t.Fatalf("MemoryID=%q want mem-immunology", cell.MemoryID)
	}
	if cell.QueryDomain != "Urban Logistics" || cell.MemoryDomain != "Immunology" {
		t.Fatalf("unexpected domains: %#v", cell)
	}
	if cell.Strategy != defaultMemoryCollisionStrategy {
		t.Fatalf("Strategy=%q want %q", cell.Strategy, defaultMemoryCollisionStrategy)
	}
	if cell.StructuralSimilarity != 1 {
		t.Fatalf("StructuralSimilarity=%v want 1", cell.StructuralSimilarity)
	}
	if cell.LiteralSimilarity >= 0.2 {
		t.Fatalf("LiteralSimilarity=%v, expected distant literal match", cell.LiteralSimilarity)
	}
	if cell.CollisionScore < 1.3 {
		t.Fatalf("CollisionScore=%v, expected entropy-boosted score", cell.CollisionScore)
	}
	if !strings.HasPrefix(cell.CollisionID, "memory_collision:") {
		t.Fatalf("unexpected CollisionID %q", cell.CollisionID)
	}
	if !strings.HasPrefix(cell.TextID, "text:") || !strings.HasPrefix(cell.SetID, "set:") {
		t.Fatalf("unexpected text/set ids: text=%q set=%q", cell.TextID, cell.SetID)
	}
	if len(cell.SourceRefs) != 2 {
		t.Fatalf("SourceRefs=%#v, want query and memory refs", cell.SourceRefs)
	}
}

func TestPlanMemoryCollisionCellsDedupesAndLimitsStableOrder(t *testing.T) {
	query := MechanismQuery{
		ID:               "q",
		Domain:           "Software",
		Text:             "Need low-latency routing around failed nodes.",
		LiteralVector:    []float32{1, 0, 0},
		StructuralVector: []float32{0, 1, 0},
	}
	strong := MechanismMemory{
		ID:               "mem-strong",
		OriginalDomain:   "Ecology",
		AbstractSchema:   "Local routing around failed nodes.",
		LiteralVector:    []float32{0, 1, 0},
		StructuralVector: []float32{0, 1, 0},
	}
	duplicate := strong
	weaker := MechanismMemory{
		ID:               "mem-weaker",
		OriginalDomain:   "Logistics",
		AbstractSchema:   "Partial local routing around failed hubs.",
		LiteralVector:    []float32{0, 0.8, 0.2},
		StructuralVector: []float32{0, 0.8, 0.2},
	}

	plan := PlanMemoryCollisionCells(MemoryCollisionInput{
		WorkspaceID: "ws",
		Query:       query,
		Memories:    []MechanismMemory{weaker, strong, duplicate},
		Limit:       1,
		Threshold:   0.1,
	})

	if len(plan.Cells) != 1 {
		t.Fatalf("expected one limited cell, got %d", len(plan.Cells))
	}
	if got := plan.Cells[0].MemoryID; got != "mem-strong" {
		t.Fatalf("top MemoryID=%q want mem-strong", got)
	}
	if plan.Skipped != 2 {
		t.Fatalf("Skipped=%d want duplicate plus limit skip", plan.Skipped)
	}
}

func TestPlanMemoryCollisionCellsRejectsInvalidInputs(t *testing.T) {
	query := MechanismQuery{
		ID:               "q",
		Domain:           "Software",
		Text:             "query",
		LiteralVector:    []float32{1, 0},
		StructuralVector: []float32{0, 1},
	}
	memories := []MechanismMemory{
		{
			ID:               "same-domain",
			OriginalDomain:   "Software",
			AbstractSchema:   "same domain",
			LiteralVector:    []float32{0, 1},
			StructuralVector: []float32{0, 1},
		},
		{
			ID:               "bad-dims",
			OriginalDomain:   "Biology",
			AbstractSchema:   "bad dims",
			LiteralVector:    []float32{0, 1, 0},
			StructuralVector: []float32{0, 1},
		},
	}

	plan := PlanMemoryCollisionCells(MemoryCollisionInput{
		WorkspaceID: "ws",
		Query:       query,
		Memories:    memories,
		Threshold:   0.1,
	})
	if len(plan.Cells) != 0 {
		t.Fatalf("expected no cells, got %#v", plan.Cells)
	}
	if plan.Skipped != len(memories) {
		t.Fatalf("Skipped=%d want %d", plan.Skipped, len(memories))
	}
}
