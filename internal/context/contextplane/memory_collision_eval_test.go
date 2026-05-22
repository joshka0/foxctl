package contextplane

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

type memoryCollisionEvalCorpus struct {
	Cases []memoryCollisionEvalCase `json:"cases"`
}

type memoryCollisionEvalCase struct {
	Name                string            `json:"name"`
	WorkspaceID         string            `json:"workspace_id"`
	Query               MechanismQuery    `json:"query"`
	Memories            []MechanismMemory `json:"memories"`
	Entropy             float64           `json:"entropy,omitempty"`
	Threshold           float64           `json:"threshold,omitempty"`
	Limit               int               `json:"limit,omitempty"`
	IncludeSameDomain   bool              `json:"include_same_domain,omitempty"`
	Strategy            string            `json:"strategy,omitempty"`
	WantTopMemoryID     string            `json:"want_top_memory_id"`
	WantAbsentMemoryIDs []string          `json:"want_absent_memory_ids,omitempty"`
}

func TestMemoryCollisionEvalCorpusTop1(t *testing.T) {
	corpus := loadMemoryCollisionEvalCorpus(t)
	if len(corpus.Cases) < 5 {
		t.Fatalf("eval corpus has %d cases, want at least 5", len(corpus.Cases))
	}

	top1 := 0
	for _, tc := range corpus.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			input := MemoryCollisionInput{
				WorkspaceID:       tc.WorkspaceID,
				Query:             tc.Query,
				Memories:          tc.Memories,
				Entropy:           tc.Entropy,
				Threshold:         tc.Threshold,
				Limit:             tc.Limit,
				IncludeSameDomain: tc.IncludeSameDomain,
				Strategy:          tc.Strategy,
			}
			first := PlanMemoryCollisionCells(input)
			second := PlanMemoryCollisionCells(input)
			if !reflect.DeepEqual(first, second) {
				t.Fatalf("plan is not stable across identical eval runs:\nfirst=%#v\nsecond=%#v", first, second)
			}
			if len(first.Cells) == 0 {
				t.Fatalf("no collision cells returned; skipped=%d", first.Skipped)
			}

			top := first.Cells[0]
			if top.MemoryID != tc.WantTopMemoryID {
				t.Fatalf("top memory=%q want %q; cells=%#v", top.MemoryID, tc.WantTopMemoryID, first.Cells)
			}
			top1++
			assertMemoryCollisionEvalDescriptor(t, top)
			if top.StructuralSimilarity <= top.LiteralSimilarity {
				t.Fatalf("top cell is not structurally dominant: structural=%v literal=%v cell=%#v", top.StructuralSimilarity, top.LiteralSimilarity, top)
			}
			for _, absentID := range tc.WantAbsentMemoryIDs {
				if memoryCollisionEvalContains(first, absentID) {
					t.Fatalf("memory %q should not appear in eval cells: %#v", absentID, first.Cells)
				}
			}
		})
	}

	if top1 != len(corpus.Cases) {
		t.Fatalf("top1=%d/%d want perfect deterministic eval score", top1, len(corpus.Cases))
	}
}

func assertMemoryCollisionEvalDescriptor(t *testing.T, cell MemoryCollisionCell) {
	t.Helper()
	switch {
	case !strings.HasPrefix(cell.CollisionID, "memory_collision:"):
		t.Fatalf("collision id is not collider-friendly: %#v", cell)
	case !strings.HasPrefix(cell.TextID, "text:"):
		t.Fatalf("text id is not collider-friendly: %#v", cell)
	case !strings.HasPrefix(cell.SetID, "set:"):
		t.Fatalf("set id is not collider-friendly: %#v", cell)
	case strings.TrimSpace(cell.Strategy) == "":
		t.Fatalf("strategy missing: %#v", cell)
	case strings.TrimSpace(cell.Reason) == "":
		t.Fatalf("reason missing: %#v", cell)
	case strings.TrimSpace(cell.QueryDomain) == "" || strings.TrimSpace(cell.MemoryDomain) == "":
		t.Fatalf("domains missing: %#v", cell)
	case len(cell.SourceRefs) < 2:
		t.Fatalf("source refs should include query and memory evidence: %#v", cell.SourceRefs)
	}
}

func memoryCollisionEvalContains(plan MemoryCollisionPlan, memoryID string) bool {
	for _, cell := range plan.Cells {
		if cell.MemoryID == memoryID {
			return true
		}
	}
	return false
}

func loadMemoryCollisionEvalCorpus(t *testing.T) memoryCollisionEvalCorpus {
	t.Helper()
	body, err := os.ReadFile("testdata/memory_collision_eval.json")
	if err != nil {
		t.Fatalf("read eval corpus: %v", err)
	}
	var corpus memoryCollisionEvalCorpus
	if err := json.Unmarshal(body, &corpus); err != nil {
		t.Fatalf("decode eval corpus: %v", err)
	}
	for i, tc := range corpus.Cases {
		if strings.TrimSpace(tc.Name) == "" {
			t.Fatalf("case %d missing name", i)
		}
		if strings.TrimSpace(tc.WantTopMemoryID) == "" {
			t.Fatalf("case %s missing want_top_memory_id", tc.Name)
		}
	}
	return corpus
}
