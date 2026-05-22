package contextplane

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestPlanMemoryCollisionCacheNoteRendersRetrievableNetwork(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	input.Collision.SourceRefs = []contextengine.EvidenceRef{{
		Type: contextengine.RefTypePath,
		Ref:  "internal/context/contextplane/memory_collision_cells.go",
	}}
	output := validMemoryCollisionAgentOutput()

	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID:   "workspace-foxctl",
		WorkspacePath: "/home/dev/repos/foxctl",
		Query:         input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentIndex:        1,
			AgentRole:         "constraint_translator",
			AgentProvider:     "openrouter",
			AgentModel:        "example/model",
			BisociationMode:   MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision:         input.Collision,
			Output:            output,
			Validation:        MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	if !strings.HasPrefix(note.DedupeKey, "memory_collision_cache:") {
		t.Fatalf("unexpected dedupe key: %s", note.DedupeKey)
	}
	if !strings.HasPrefix(note.NotePath, "inbox/drafted-from-foxctl/collisions/foxctl/2026-05-21/") || !strings.HasSuffix(note.NotePath, ".md") {
		t.Fatalf("unexpected note path: %s", note.NotePath)
	}
	if note.Title != "Collision cache: Clear emergency vehicles without central dispatch. (Urban Logistics)" {
		t.Fatalf("unexpected note title: %s", note.Title)
	}
	required := []string{
		`type: "memory_collision_cache"`,
		`title: "Collision cache: Clear emergency vehicles without central dispatch. (Urban Logistics)"`,
		"```foxctl-memory-collision-cache-v1",
		`agent_models:`,
		`"openrouter/example/model"`,
		`bisociation_modes:`,
		`"far-alien"`,
		`selection_modes:`,
		`"far"`,
		`prompt_abstractions:`,
		`"alien"`,
		"foxctl/collision-cache",
		"## Query Shape",
		"## Collision Network",
		"Agent model: `openrouter/example/model`",
		"Bisociation mode: `far-alien`",
		"Abstract schema:\n\n```text\n",
		"Use local queues as pressure valves.",
		"Decentralized edge-node response",
		"internal/context/contextplane/memory_collision_cells.go",
	}
	for _, needle := range required {
		if !strings.Contains(note.Content, needle) {
			t.Fatalf("note content missing %q:\n%s", needle, note.Content)
		}
	}
	for _, leaked := range []string{"literal_vector", "structural_vector"} {
		if strings.Contains(note.Content, leaked) {
			t.Fatalf("note content leaked vector field %q:\n%s", leaked, note.Content)
		}
	}
}

func TestParseMemoryCollisionCacheNoteRoundTripsMachineRecord(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentProvider:     "openrouter",
			AgentModel:        "example/model",
			BisociationMode:   MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision:         input.Collision,
			Output:            validMemoryCollisionAgentOutput(),
			Validation:        MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}

	record, err := ParseMemoryCollisionCacheNote(note.NotePath, note.Content)
	if err != nil {
		t.Fatalf("ParseMemoryCollisionCacheNote: %v", err)
	}
	if record.Version != 1 || record.WorkspaceID != "workspace-foxctl" || record.Query.ID != input.Query.ID {
		t.Fatalf("unexpected parsed record: %#v", record)
	}
	if len(record.Syntheses) != 1 {
		t.Fatalf("parsed %d syntheses, want 1", len(record.Syntheses))
	}
	synthesis := record.Syntheses[0]
	if synthesis.BisociationMode != MemoryCollisionAgentModeFarAlien || synthesis.SelectionMode != "far" || synthesis.PromptAbstraction != "alien" {
		t.Fatalf("parsed synthesis lost mode metadata: %#v", synthesis)
	}
	if synthesis.Output.NewCollision != "Use local queues as pressure valves." {
		t.Fatalf("parsed synthesis lost output: %#v", synthesis.Output)
	}
}

func TestParseMemoryCollisionCacheNoteRejectsMissingMachineBlock(t *testing.T) {
	content := `---
type: "memory_collision_cache"
---

# Legacy cache note
`
	_, err := ParseMemoryCollisionCacheNote("legacy.md", content)
	if err == nil || !strings.Contains(err.Error(), "missing foxctl-memory-collision-cache-v1 block") {
		t.Fatalf("expected missing machine block error, got %v", err)
	}
}

func TestLoadMemoryCollisionCacheRecordsFiltersTypedFields(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	input.Collision.MemoryDomain = "go:cmd/foxctl/cmd/sessionscmd"
	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID:   "workspace-foxctl",
		WorkspacePath: "/home/dev/repos/foxctl",
		Query:         input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentProvider:     "openrouter",
			AgentModel:        "example/model",
			BisociationMode:   MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision:         input.Collision,
			Output:            validMemoryCollisionAgentOutput(),
			Validation:        MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	vaultRoot := t.TempDir()
	if err := WriteMemoryCollisionCacheNote(context.Background(), vaultRoot, note); err != nil {
		t.Fatalf("WriteMemoryCollisionCacheNote: %v", err)
	}

	records, err := LoadMemoryCollisionCacheRecords(context.Background(), vaultRoot, MemoryCollisionCacheLoadOptions{
		WorkspaceID:     "workspace-foxctl",
		QueryID:         input.Query.ID,
		BisociationMode: MemoryCollisionAgentModeFarAlien,
		MemoryDomain:    "go:cmd/foxctl/cmd/sessionscmd",
		AgentModel:      "openrouter/example/model",
	})
	if err != nil {
		t.Fatalf("LoadMemoryCollisionCacheRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("loaded %d records, want 1", len(records))
	}
	records, err = LoadMemoryCollisionCacheRecords(context.Background(), vaultRoot, MemoryCollisionCacheLoadOptions{
		MemoryDomain: "go:internal/other",
	})
	if err != nil {
		t.Fatalf("LoadMemoryCollisionCacheRecords unmatched: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("loaded %d unmatched records, want 0", len(records))
	}
}

func TestMemoryCollisionCacheRecordsToCellsConvertsSynthesisAsSecondaryCandidate(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			BisociationMode:   MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision:         input.Collision,
			Output:            validMemoryCollisionAgentOutput(),
			Validation:        MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	record, err := ParseMemoryCollisionCacheNote(note.NotePath, note.Content)
	if err != nil {
		t.Fatalf("ParseMemoryCollisionCacheNote: %v", err)
	}
	cells := MemoryCollisionCacheRecordsToCells("workspace-foxctl", input.Query, []MemoryCollisionCacheRecord{record})
	if len(cells) != 1 {
		t.Fatalf("converted %d cells, want 1", len(cells))
	}
	cell := cells[0]
	if cell.Strategy != memoryCollisionCacheStrategy || !strings.HasPrefix(cell.CollisionID, "memory_collision_cache:") {
		t.Fatalf("unexpected cache cell identity: %#v", cell)
	}
	if cell.MemorySummary != "Use local queues as pressure valves." || cell.AbstractSchema == "" {
		t.Fatalf("cache cell did not use synthesis output: %#v", cell)
	}
	if cell.CollisionScore > 0.89 {
		t.Fatalf("cache cell should be capped as secondary evidence: %#v", cell)
	}
}

func TestPlanMemoryCollisionCacheNoteUsesFunctionNameInTitle(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	input.Query.ID = "repo-symbol:workspace::sym:go:example/package:BuildRepoMotifArtifacts"
	input.Query.Domain = "go:internal/context/contextplane"
	input.Query.Text = "func BuildRepoMotifArtifacts(ctx context.Context, workspacePath string) ([]RepoMotif, error)"
	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentIndex: 1,
			AgentRole:  "constraint_translator",
			Collision:  input.Collision,
			Output:     validMemoryCollisionAgentOutput(),
			Validation: MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	if note.Title != "Collision cache: BuildRepoMotifArtifacts (contextplane)" {
		t.Fatalf("unexpected function-title note: %s", note.Title)
	}
	if strings.Contains(note.NotePath, "ctx-context") {
		t.Fatalf("note path should not include full function signature: %s", note.NotePath)
	}
}

func TestPlanMemoryCollisionCacheNoteDedupeIncludesMode(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	base := MemoryCollisionSynthesis{
		AgentIndex: 1,
		AgentRole:  "constraint_translator",
		Collision:  input.Collision,
		Output:     validMemoryCollisionAgentOutput(),
		Validation: MemoryCollisionAgentValidation{Valid: true},
	}
	balanced, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses:   []MemoryCollisionSynthesis{base},
		CreatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote balanced: %v", err)
	}
	farAlienSynthesis := base
	farAlienSynthesis.BisociationMode = MemoryCollisionAgentModeFarAlien
	farAlien, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses:   []MemoryCollisionSynthesis{farAlienSynthesis},
		CreatedAt:   time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote far-alien: %v", err)
	}
	if balanced.DedupeKey == farAlien.DedupeKey {
		t.Fatalf("dedupe key should include mode, got %s", balanced.DedupeKey)
	}
}

func TestWriteMemoryCollisionCacheNoteWritesDirectVaultFile(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	note, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentIndex: 1,
			AgentRole:  "constraint_translator",
			Collision:  input.Collision,
			Output:     validMemoryCollisionAgentOutput(),
			Validation: MemoryCollisionAgentValidation{Valid: true},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("PlanMemoryCollisionCacheNote: %v", err)
	}
	vaultRoot := t.TempDir()
	if err := WriteMemoryCollisionCacheNote(context.Background(), vaultRoot, note); err != nil {
		t.Fatalf("WriteMemoryCollisionCacheNote: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(vaultRoot, filepath.FromSlash(note.NotePath)))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if string(body) != note.Content {
		t.Fatalf("written note content mismatch")
	}
}

func TestPlanMemoryCollisionCacheNoteRequiresValidSynthesis(t *testing.T) {
	input := validMemoryCollisionAgentPromptInput()
	_, err := PlanMemoryCollisionCacheNote(MemoryCollisionCacheInput{
		WorkspaceID: "workspace-foxctl",
		Query:       input.Query,
		Syntheses: []MemoryCollisionSynthesis{{
			AgentIndex: 1,
			AgentRole:  "constraint_translator",
			Collision:  input.Collision,
			Output:     validMemoryCollisionAgentOutput(),
			Validation: MemoryCollisionAgentValidation{Valid: false, Errors: []string{"bad"}},
		}},
		CreatedAt: time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("expected invalid synthesis to be rejected")
	}
}

func validMemoryCollisionAgentOutput() MemoryCollisionAgentOutput {
	return MemoryCollisionAgentOutput{
		BridgeSchema:      "Local pressure relief creates bounded bypasses without central coordination.",
		NewCollision:      "Use local queues as pressure valves.",
		TransferSteps:     []string{"detect local pressure", "grant bounded bypass"},
		StressTest:        "central outage",
		Risks:             []string{"overrouting"},
		Confidence:        0.83,
		NoveltyConfidence: 0.72,
	}
}
