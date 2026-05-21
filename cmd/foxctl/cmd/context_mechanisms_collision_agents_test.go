package cmd

import (
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextplane"
)

func TestNormalizeCollisionAgentCountCapsAtThree(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "default", in: 0, want: defaultMemoryCollisionAgentCount},
		{name: "one", in: 1, want: 1},
		{name: "three", in: 3, want: 3},
		{name: "capped", in: 8, want: maxMemoryCollisionAgentCount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeCollisionAgentCount(tt.in); got != tt.want {
				t.Fatalf("normalizeCollisionAgentCount(%d)=%d want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeCollisionPoolLimitDefaultsAboveAgentCount(t *testing.T) {
	if got := normalizeCollisionPoolLimit(0, 2); got != defaultMemoryCollisionPoolLimit {
		t.Fatalf("normalizeCollisionPoolLimit default=%d want %d", got, defaultMemoryCollisionPoolLimit)
	}
	if got := normalizeCollisionPoolLimit(1, 3); got != 3 {
		t.Fatalf("normalizeCollisionPoolLimit should not go below agent count, got %d", got)
	}
}

func TestMemoryCollisionAgentTargetsParseExplicitModelRuns(t *testing.T) {
	targets, err := memoryCollisionAgentTargetsFromOptions(repoSymbolCollisionAgentsOptions{
		AgentRuns: []string{
			"openrouter/minimax/minimax-2.7=5",
			"openrouter:moonshotai/kimi-2.6=5",
			"openai/gpt-5.5-codex=5",
		},
	})
	if err != nil {
		t.Fatalf("memoryCollisionAgentTargetsFromOptions: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("got %d targets, want 3", len(targets))
	}
	if targets[0].Provider != "openrouter" || targets[0].Model != "minimax/minimax-2.7" || targets[0].Count != 5 {
		t.Fatalf("unexpected first target: %#v", targets[0])
	}
	if targets[1].Provider != "openrouter" || targets[1].Model != "moonshotai/kimi-2.6" || targets[1].Count != 5 {
		t.Fatalf("unexpected second target: %#v", targets[1])
	}
	if targets[2].Provider != "openai" || targets[2].Model != "gpt-5.5-codex" || targets[2].Count != 5 {
		t.Fatalf("unexpected third target: %#v", targets[2])
	}
	runs := expandMemoryCollisionAgentRuns(targets)
	if len(runs) != 15 {
		t.Fatalf("expanded %d runs, want 15", len(runs))
	}
	if runs[5].Provider != "openrouter" || runs[5].Model != "moonshotai/kimi-2.6" {
		t.Fatalf("unexpected expanded run: %#v", runs[5])
	}
}

func TestMemoryCollisionAgentTargetsRejectTooManyRuns(t *testing.T) {
	_, err := memoryCollisionAgentTargetsFromOptions(repoSymbolCollisionAgentsOptions{
		AgentRuns: []string{"openrouter/minimax/minimax-2.7=25"},
	})
	if err == nil {
		t.Fatal("expected too many explicit model runs to fail")
	}
}

func TestNormalizeMemoryCollisionAgentConcurrency(t *testing.T) {
	if got := normalizeMemoryCollisionAgentConcurrency(0, 20); got != defaultMemoryCollisionConcurrency {
		t.Fatalf("default concurrency=%d want %d", got, defaultMemoryCollisionConcurrency)
	}
	if got := normalizeMemoryCollisionAgentConcurrency(99, 20); got != maxMemoryCollisionConcurrency {
		t.Fatalf("capped concurrency=%d want %d", got, maxMemoryCollisionConcurrency)
	}
	if got := normalizeMemoryCollisionAgentConcurrency(5, 2); got != 2 {
		t.Fatalf("concurrency should not exceed total, got %d", got)
	}
}

func TestSelectMemoryCollisionCellsForAgentsPrefersDistantDomains(t *testing.T) {
	cells := []contextplane.MemoryCollisionCell{
		{
			CollisionID:    "memory_collision:near-top",
			MemoryID:       "memory-near-top",
			MemoryDomain:   "go:internal/context/contextplane/taskhistory",
			CollisionScore: 1.20,
		},
		{
			CollisionID:    "memory_collision:near-duplicate",
			MemoryID:       "memory-near-duplicate",
			MemoryDomain:   "go:internal/context/contextplane/taskhistory",
			CollisionScore: 1.19,
		},
		{
			CollisionID:    "memory_collision:far",
			MemoryID:       "memory-far",
			MemoryDomain:   "go:internal/storage/memory",
			CollisionScore: 1.10,
		},
	}

	selected, report := selectMemoryCollisionCellsForAgents("go:internal/context/contextplane", cells, 2, repoSymbolCollisionAgentsOptions{
		DomainDiversity:   true,
		MinDomainDistance: 2,
	})
	if len(selected) != 2 {
		t.Fatalf("selected %d cells, want 2", len(selected))
	}
	if selected[0].CollisionID != "memory_collision:near-top" {
		t.Fatalf("first selected cell should keep highest score, got %s", selected[0].CollisionID)
	}
	if selected[1].CollisionID != "memory_collision:far" {
		t.Fatalf("second selected cell should prefer far domain, got %s", selected[1].CollisionID)
	}
	if report.PoolCount != 3 || report.SelectedCount != 2 || report.FallbackCount != 1 {
		t.Fatalf("unexpected selection report: %#v", report)
	}
	if got := memoryDomainDistance("go:internal/context/contextplane", "go:internal/storage/memory"); got < 2 {
		t.Fatalf("expected storage domain to be distant, got %d", got)
	}
}

func TestSelectMemoryCollisionCellsForAgentsCanDisableDomainDiversity(t *testing.T) {
	cells := []contextplane.MemoryCollisionCell{
		{CollisionID: "memory_collision:one", MemoryID: "memory-one", MemoryDomain: "go:internal/context/contextplane/taskhistory"},
		{CollisionID: "memory_collision:two", MemoryID: "memory-two", MemoryDomain: "go:internal/context/contextplane/taskhistory"},
		{CollisionID: "memory_collision:three", MemoryID: "memory-three", MemoryDomain: "go:internal/storage/memory"},
	}

	selected, report := selectMemoryCollisionCellsForAgents("go:internal/context/contextplane", cells, 2, repoSymbolCollisionAgentsOptions{
		DomainDiversity:   false,
		MinDomainDistance: 2,
	})
	if len(selected) != 2 {
		t.Fatalf("selected %d cells, want 2", len(selected))
	}
	if selected[0].CollisionID != "memory_collision:one" || selected[1].CollisionID != "memory_collision:two" {
		t.Fatalf("domain diversity disabled should keep score order, got %#v", selected)
	}
	if report.DomainDiversity {
		t.Fatalf("report should mark domain diversity disabled: %#v", report)
	}
}

func TestRankMemoryCollisionCellsForFarBisociationPrefersDistantLowLiteralMatches(t *testing.T) {
	cells := []contextplane.MemoryCollisionCell{
		{
			CollisionID:          "memory_collision:near-literal",
			DedupeKey:            "near-literal",
			MemoryDomain:         "go:internal/context/contextplane/taskhistory",
			LiteralSimilarity:    0.95,
			StructuralSimilarity: 0.99,
			CollisionScore:       1.30,
		},
		{
			CollisionID:          "memory_collision:far-structural",
			DedupeKey:            "far-structural",
			MemoryDomain:         "go:internal/storage/memory",
			LiteralSimilarity:    0.10,
			StructuralSimilarity: 0.96,
			CollisionScore:       1.05,
		},
	}

	ranked := rankMemoryCollisionCellsForBisociationMode("go:internal/context/contextplane", cells, contextplane.MemoryCollisionAgentModeFar)
	if ranked[0].CollisionID != "memory_collision:far-structural" {
		t.Fatalf("far ranking should prefer distant low-literal structural match, got %#v", ranked)
	}
	balanced := rankMemoryCollisionCellsForBisociationMode("go:internal/context/contextplane", cells, contextplane.MemoryCollisionAgentModeBalanced)
	if balanced[0].CollisionID != "memory_collision:near-literal" {
		t.Fatalf("balanced ranking should keep existing score order, got %#v", balanced)
	}
}

func TestSelectMemoryCollisionCellsForAgentsReportsFarAlienMode(t *testing.T) {
	cells := []contextplane.MemoryCollisionCell{
		{
			CollisionID:          "memory_collision:one",
			MemoryID:             "memory-one",
			MemoryDomain:         "go:internal/storage/memory",
			LiteralSimilarity:    0.10,
			StructuralSimilarity: 0.98,
			CollisionScore:       1.10,
		},
	}

	selected, report := selectMemoryCollisionCellsForAgents("go:internal/context/contextplane", cells, 1, repoSymbolCollisionAgentsOptions{
		BisociationMode:   contextplane.MemoryCollisionAgentModeFarAlien,
		DomainDiversity:   true,
		MinDomainDistance: 2,
	})
	if len(selected) != 1 {
		t.Fatalf("selected %d cells, want 1", len(selected))
	}
	if report.BisociationMode != contextplane.MemoryCollisionAgentModeFarAlien || report.SelectionMode != "far" || report.PromptAbstraction != "alien" {
		t.Fatalf("unexpected far-alien report: %#v", report)
	}
	if len(report.SelectedScores) != 1 || report.SelectedScores[0] <= 0 {
		t.Fatalf("expected far mode selected score, got %#v", report)
	}
}

func TestBuildMemoryCollisionModeComparisonReportsAllModes(t *testing.T) {
	cells := []contextplane.MemoryCollisionCell{
		{
			CollisionID:          "memory_collision:near-literal",
			MemoryID:             "memory-near",
			MemoryDomain:         "go:internal/context/contextplane/taskhistory",
			MemorySummary:        "near implementation detail",
			LiteralSimilarity:    0.95,
			StructuralSimilarity: 0.99,
			CollisionScore:       1.30,
		},
		{
			CollisionID:          "memory_collision:far-structural",
			MemoryID:             "memory-far",
			MemoryDomain:         "go:cmd/foxctl/cmd/sessionscmd",
			MemorySummary:        "distant orchestration shape",
			LiteralSimilarity:    0.05,
			StructuralSimilarity: 0.96,
			CollisionScore:       1.05,
		},
	}

	reports := buildMemoryCollisionModeComparison("go:internal/context/contextplane", cells, 1, repoSymbolCollisionAgentsOptions{
		DomainDiversity:   true,
		MinDomainDistance: 2,
	})
	if len(reports) != 4 {
		t.Fatalf("got %d mode reports, want 4", len(reports))
	}
	wantModes := []struct {
		mode        string
		selection   string
		abstraction string
		selected    string
	}{
		{contextplane.MemoryCollisionAgentModeBalanced, "balanced", "grounded", "memory_collision:near-literal"},
		{contextplane.MemoryCollisionAgentModeFar, "far", "grounded", "memory_collision:far-structural"},
		{contextplane.MemoryCollisionAgentModeAlien, "balanced", "alien", "memory_collision:near-literal"},
		{contextplane.MemoryCollisionAgentModeFarAlien, "far", "alien", "memory_collision:far-structural"},
	}
	for i, want := range wantModes {
		report := reports[i]
		if report.BisociationMode != want.mode || report.SelectionMode != want.selection || report.PromptAbstraction != want.abstraction {
			t.Fatalf("unexpected mode report[%d]: %#v", i, report)
		}
		if report.SelectedCount != 1 || len(report.Selected) != 1 || report.Selected[0].CollisionID != want.selected {
			t.Fatalf("unexpected selected cell for report[%d]: %#v", i, report)
		}
		if want.selection == "far" && (len(report.SelectedScores) != 1 || report.Selected[0].BisociationScore <= 0) {
			t.Fatalf("far mode should include bisociation scores: %#v", report)
		}
		if want.selection == "balanced" && len(report.SelectedScores) != 0 {
			t.Fatalf("balanced mode should not include bisociation scores: %#v", report)
		}
	}
}

func TestMemoryCollisionSynthesesFromViewsKeepsOnlySuccessfulAgents(t *testing.T) {
	views := []repoSymbolCollisionAgentView{
		{
			AgentIndex:        1,
			AgentRole:         "structural_translator",
			AgentProvider:     "openrouter",
			AgentModel:        "model-one",
			BisociationMode:   contextplane.MemoryCollisionAgentModeFarAlien,
			SelectionMode:     "far",
			PromptAbstraction: "alien",
			Collision: contextplane.MemoryCollisionCell{
				CollisionID:    "memory_collision:one",
				MemoryID:       "memory-one",
				MemoryDomain:   "domain-one",
				AbstractSchema: "local pressure relief",
			},
			AgentOutput: contextplane.MemoryCollisionAgentOutput{
				BridgeSchema:  "pressure valve",
				NewCollision:  "route local pressure through a bypass",
				TransferSteps: []string{"detect pressure"},
				Confidence:    0.8,
			},
			Validation: contextplane.MemoryCollisionAgentValidation{Valid: true},
		},
		{
			AgentIndex: 2,
			AgentRole:  "constraint_mapper",
			Error:      "agent failed",
			Validation: contextplane.MemoryCollisionAgentValidation{Valid: true},
		},
		{
			AgentIndex: 3,
			AgentRole:  "failure_mode_scout",
			Validation: contextplane.MemoryCollisionAgentValidation{Valid: false},
		},
	}

	got := memoryCollisionSynthesesFromViews(views)
	if len(got) != 1 {
		t.Fatalf("memoryCollisionSynthesesFromViews returned %d syntheses, want 1", len(got))
	}
	if got[0].AgentIndex != 1 || got[0].Output.NewCollision == "" {
		t.Fatalf("unexpected synthesis: %#v", got[0])
	}
	if got[0].AgentProvider != "openrouter" || got[0].AgentModel != "model-one" {
		t.Fatalf("synthesis lost model metadata: %#v", got[0])
	}
	if got[0].BisociationMode != contextplane.MemoryCollisionAgentModeFarAlien || got[0].SelectionMode != "far" || got[0].PromptAbstraction != "alien" {
		t.Fatalf("synthesis lost mode metadata: %#v", got[0])
	}
}
