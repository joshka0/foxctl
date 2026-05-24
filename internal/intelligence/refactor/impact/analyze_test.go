package impact

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestAnalyzeRejectsInvalidCanonicalInputs(t *testing.T) {
	tests := []struct {
		name string
		in   Input
	}{
		{
			name: "invalid target kind",
			in: Input{
				Intent:  IntentBehaviorPreservingCleanup,
				Targets: []Target{{Kind: "path", Path: "internal/a.go"}},
			},
		},
		{
			name: "invalid intent",
			in: Input{
				Intent:  "cleanup",
				Targets: []Target{{Kind: TargetFile, Path: "internal/a.go"}},
			},
		},
		{
			name: "symbol requires symbol",
			in: Input{
				Intent:  IntentRename,
				Targets: []Target{{Kind: TargetSymbol, Path: "internal/a.go"}},
			},
		},
		{
			name: "no target",
			in: Input{
				Intent: IntentRename,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Analyze(context.Background(), tt.in, Providers{}); err == nil {
				t.Fatal("Analyze succeeded, want validation error")
			}
		})
	}
}

func TestAnalyzeNormalizesDiffAndExplicitTargetsIntoOneCanonicalTarget(t *testing.T) {
	packet, err := Analyze(context.Background(), Input{
		Targets: []Target{{Kind: TargetFile, Path: "./internal/core.go"}},
		Diff:    &DiffInput{BaseRef: "main", HeadRef: "HEAD"},
		Intent:  IntentAPIContractChange,
	}, Providers{
		Diff: fakeDiffProvider{changes: []Change{{Path: "internal/core.go", Status: "M", Additions: 3}}},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(packet.Targets) != 1 {
		t.Fatalf("target count=%d want 1: %+v", len(packet.Targets), packet.Targets)
	}
	gotSources := packet.Targets[0].Sources
	wantSources := []Source{SourceExplicitTargets, SourceGitDiff}
	if !reflect.DeepEqual(gotSources, wantSources) {
		t.Fatalf("target sources=%v want %v", gotSources, wantSources)
	}
	if len(packet.MustUpdate) != 1 || packet.MustUpdate[0].Path != "internal/core.go" {
		t.Fatalf("must_update=%+v want internal/core.go", packet.MustUpdate)
	}
	if packet.Summary.TargetCount != 1 || packet.Summary.MustUpdateCount != 1 {
		t.Fatalf("summary=%+v want target and must_update counts", packet.Summary)
	}
}

func TestAnalyzeGroupsStructuralAndSemanticEvidence(t *testing.T) {
	packet, err := Analyze(context.Background(), Input{
		Targets:      []Target{{Kind: TargetSymbol, Path: "internal/core.go", Symbol: "Plan"}},
		Intent:       IntentAPIContractChange,
		IncludeTests: true,
		IncludeDocs:  true,
		Limit:        20,
	}, Providers{
		Structural: fakeStructuralProvider{result: StructuralResult{
			Available: true,
			Candidates: []StructuralCandidate{
				{Path: "cmd/root.go", Symbol: "run", Section: SectionCaller, EdgeTypes: []string{"CALLS"}, Depth: 1, TargetKey: "target", TargetLabel: "Plan"},
				{Path: "internal/core_test.go", Symbol: "TestPlan", Section: SectionTest, EdgeTypes: []string{"TESTS"}, Depth: 1, TargetKey: "target", TargetLabel: "Plan"},
				{Path: "docs/core.md", Section: SectionDoc, EdgeTypes: []string{"DESCRIBED_BY"}, Depth: 1, TargetKey: "target", TargetLabel: "Plan"},
				{Path: "internal/adapter.go", Symbol: "Adapter", Section: SectionCallee, EdgeTypes: []string{"CALLS"}, Depth: 1, TargetKey: "target", TargetLabel: "Plan"},
			},
		}},
		Semantic: fakeSemanticProvider{result: SemanticResult{
			Available: true,
			Source:    SourceTurboVec,
			Candidates: []SemanticCandidate{
				{Path: "internal/adapter.go", Symbol: "Adapter", Similarity: 0.91, Summary: "nearby adapter", TargetKey: "target", TargetLabel: "Plan"},
				{Path: "internal/notes.go", Symbol: "Notes", Similarity: 0.60, TargetKey: "target", TargetLabel: "Plan"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(packet.MustUpdate) != 2 {
		t.Fatalf("must_update=%+v want direct target plus caller", packet.MustUpdate)
	}
	assertGroupHasPath(t, packet.MustUpdate, "cmd/root.go")
	assertGroupHasPath(t, packet.TestsToRun, "internal/core_test.go")
	assertGroupHasPath(t, packet.DocsToUpdate, "docs/core.md")
	assertGroupHasPath(t, packet.ShouldInspect, "internal/adapter.go")
	assertGroupHasPath(t, packet.ContextOnly, "internal/notes.go")

	adapter := findCandidate(packet.ShouldInspect, "internal/adapter.go")
	if adapter == nil {
		t.Fatal("adapter candidate missing")
	}
	if !reflect.DeepEqual(adapter.Sources, []Source{SourceRepoindexGraph, SourceTurboVec}) {
		t.Fatalf("adapter sources=%v want graph+turbovec", adapter.Sources)
	}
	if adapter.Summary != "nearby adapter" {
		t.Fatalf("adapter summary=%q want semantic summary", adapter.Summary)
	}
}

func TestAnalyzeConsolidatePromotesStrongSemanticNeighborToLikelyDuplicate(t *testing.T) {
	packet, err := Analyze(context.Background(), Input{
		Targets: []Target{{Kind: TargetFile, Path: "internal/a.go"}},
		Intent:  IntentConsolidate,
	}, Providers{
		Semantic: fakeSemanticProvider{result: SemanticResult{
			Available: true,
			Source:    SourceSearchIndex,
			Candidates: []SemanticCandidate{
				{Path: "internal/b.go", Symbol: "SameShape", Similarity: 0.92},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	assertGroupHasPath(t, packet.LikelyDuplicate, "internal/b.go")
}

func TestAnalyzeReportsUnavailableLanesWithoutDroppingTargets(t *testing.T) {
	packet, err := Analyze(context.Background(), Input{
		Targets: []Target{{Kind: TargetFile, Path: "internal/a.go"}},
		Intent:  IntentMove,
	}, Providers{
		Structural: fakeStructuralProvider{err: errors.New("repoindex unavailable")},
		Semantic: fakeSemanticProvider{result: SemanticResult{
			Available: false,
			Reason:    "searchindex has no documents for workspace",
		}},
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	assertGroupHasPath(t, packet.MustUpdate, "internal/a.go")
	statuses := map[Source]LaneStatus{}
	reasons := map[Source]string{}
	for _, lane := range packet.Lanes {
		statuses[lane.Name] = lane.Status
		reasons[lane.Name] = lane.Reason
	}
	if statuses[SourceRepoindexGraph] != LaneUnavailable || reasons[SourceRepoindexGraph] == "" {
		t.Fatalf("repoindex lane status/reason=%s/%q want unavailable reason", statuses[SourceRepoindexGraph], reasons[SourceRepoindexGraph])
	}
	if statuses[SourceSemanticNeighbor] != LaneUnavailable || reasons[SourceSemanticNeighbor] == "" {
		t.Fatalf("semantic lane status/reason=%s/%q want unavailable reason", statuses[SourceSemanticNeighbor], reasons[SourceSemanticNeighbor])
	}
}

type fakeDiffProvider struct {
	changes []Change
}

func (p fakeDiffProvider) ChangedFiles(context.Context, DiffInput) ([]Change, error) {
	return append([]Change(nil), p.changes...), nil
}

type fakeStructuralProvider struct {
	result StructuralResult
	err    error
}

func (p fakeStructuralProvider) Candidates(context.Context, []Target, StructuralOptions) (StructuralResult, error) {
	if p.err != nil {
		return StructuralResult{}, p.err
	}
	return p.result, nil
}

type fakeSemanticProvider struct {
	result SemanticResult
	err    error
}

func (p fakeSemanticProvider) Neighbors(context.Context, SemanticNeighborRequest) (SemanticResult, error) {
	if p.err != nil {
		return SemanticResult{}, p.err
	}
	return p.result, nil
}

func assertGroupHasPath(t *testing.T, candidates []Candidate, path string) {
	t.Helper()
	if findCandidate(candidates, path) == nil {
		t.Fatalf("candidates=%+v missing path %s", candidates, path)
	}
}

func findCandidate(candidates []Candidate, path string) *Candidate {
	for i := range candidates {
		if candidates[i].Path == path {
			return &candidates[i]
		}
	}
	return nil
}
