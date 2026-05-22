package contextplane

import (
	"reflect"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestPlanMemoryCollisionCellsStructuralMatchOutranksLiteralSameDomainMatch(t *testing.T) {
	query := MechanismQuery{
		ID:               "q-db-pressure",
		Domain:           "Database Operations",
		Text:             "Shard-local write pressure should drain without a central scheduler.",
		LiteralVector:    []float32{1, 0, 0},
		StructuralVector: []float32{1, 0, 0},
	}
	memories := []MechanismMemory{
		{
			ID:               "mem-db-literal",
			OriginalDomain:   "Database Operations",
			Summary:          "Shard pressure is routed through a coordinator.",
			AbstractSchema:   "Coordinator-mediated queue drain.",
			LiteralVector:    []float32{0.99, 0.1, 0},
			StructuralVector: []float32{0.8, 0.6, 0},
		},
		{
			ID:               "mem-watershed",
			OriginalDomain:   "Watershed Management",
			Summary:          "Local spillways shed pressure before the main channel backs up.",
			AbstractSchema:   "Decentralized pressure relief through local bypass paths.",
			LiteralVector:    []float32{0, 1, 0},
			StructuralVector: []float32{0.99, 0.1, 0},
		},
	}

	plan := PlanMemoryCollisionCells(MemoryCollisionInput{
		WorkspaceID:       "ws-behavior",
		Query:             query,
		Memories:          memories,
		Entropy:           0.5,
		Threshold:         0.6,
		IncludeSameDomain: true,
	})

	if len(plan.Cells) != 2 {
		t.Fatalf("expected both literal and structural candidates, got %d (skipped=%d)", len(plan.Cells), plan.Skipped)
	}
	top := plan.Cells[0]
	if top.MemoryID != "mem-watershed" {
		t.Fatalf("top MemoryID=%q want structural cross-domain memory", top.MemoryID)
	}
	if top.StructuralSimilarity <= plan.Cells[1].StructuralSimilarity {
		t.Fatalf("top StructuralSimilarity=%v want greater than literal candidate %v", top.StructuralSimilarity, plan.Cells[1].StructuralSimilarity)
	}
	if top.LiteralSimilarity >= plan.Cells[1].LiteralSimilarity {
		t.Fatalf("top LiteralSimilarity=%v want lower than literal candidate %v", top.LiteralSimilarity, plan.Cells[1].LiteralSimilarity)
	}
	if top.CollisionScore <= plan.Cells[1].CollisionScore {
		t.Fatalf("top CollisionScore=%v want greater than literal candidate %v", top.CollisionScore, plan.Cells[1].CollisionScore)
	}
}

func TestPlanMemoryCollisionCellsDescriptorContractIsStableAndColliderFriendly(t *testing.T) {
	input := MemoryCollisionInput{
		WorkspaceID: "ws-contract",
		Query: MechanismQuery{
			ID:               "q-release",
			Domain:           "Release Engineering",
			Text:             "Rollouts should isolate blast radius while keeping regional progress moving.",
			LiteralVector:    []float32{1, 0, 0},
			StructuralVector: []float32{0, 1, 0},
			SourceRefs: []contextengine.EvidenceRef{
				{Type: contextengine.RefTypeEvent, Ref: "retrieval_episode:release-7"},
			},
		},
		Memories: []MechanismMemory{
			{
				ID:               "mem-forest-firebreaks",
				OriginalDomain:   "Forest Management",
				Summary:          "Firebreaks isolate spread while crews continue controlled burns.",
				AbstractSchema:   "Compartmentalized propagation with bounded local failure.",
				LiteralVector:    []float32{0, 1, 0},
				StructuralVector: []float32{0, 1, 0},
				SourceRefs: []contextengine.EvidenceRef{
					{Type: contextengine.RefTypeMemoryClaim, Ref: "claim-firebreaks"},
				},
			},
		},
		Entropy:   0.4,
		Threshold: 0.7,
		Strategy:  "room_seed_collision",
	}

	first := PlanMemoryCollisionCells(input)
	second := PlanMemoryCollisionCells(input)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("plan changed across identical runs:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if len(first.Cells) != 1 {
		t.Fatalf("expected one collision cell, got %d (skipped=%d)", len(first.Cells), first.Skipped)
	}
	cell := first.Cells[0]
	if cell.CollisionID == "" || cell.TextID == "" || cell.SetID == "" {
		t.Fatalf("missing stable ids: collision=%q text=%q set=%q", cell.CollisionID, cell.TextID, cell.SetID)
	}
	if !strings.HasPrefix(cell.CollisionID, "memory_collision:") {
		t.Fatalf("CollisionID=%q want memory_collision prefix", cell.CollisionID)
	}
	if !strings.HasPrefix(cell.TextID, "text:") || !strings.HasPrefix(cell.SetID, "set:") {
		t.Fatalf("unexpected text/set ids: text=%q set=%q", cell.TextID, cell.SetID)
	}
	if cell.Strategy != "room_seed_collision" {
		t.Fatalf("Strategy=%q want room_seed_collision", cell.Strategy)
	}
	if len(cell.SourceRefs) != 2 {
		t.Fatalf("SourceRefs=%#v want query and memory refs", cell.SourceRefs)
	}
	if strings.TrimSpace(cell.Reason) == "" {
		t.Fatal("Reason is empty")
	}
	if cell.QueryText == "" || cell.QueryDomain == "" || cell.MemoryDomain == "" || cell.MemoryID == "" || cell.AbstractSchema == "" {
		t.Fatalf("descriptor missing orchestration fields: %#v", cell)
	}
}
