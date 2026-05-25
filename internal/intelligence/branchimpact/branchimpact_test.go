package branchimpact

import (
	"context"
	"testing"
)

func TestAnalyzeRanksChangedFilesAndGraphNeighbors(t *testing.T) {
	diff := fakeDiff{changes: []Change{
		{Path: "internal/runtime/runner.go", Status: "M", Additions: 8, Deletions: 2},
	}}
	graph := fakeGraph{result: GraphResult{
		Available: true,
		Candidates: []GraphCandidate{
			{Path: "internal/runtime/runner.go", Symbol: "Runner.Run", LineHint: 12, Depth: 0, EdgeTypes: []string{"CONTAINS"}},
			{Path: "internal/runtime/runner_test.go", Symbol: "TestRunnerRun", LineHint: 20, Depth: 1, EdgeTypes: []string{"CALLS"}},
			{Path: "docs/runtime.md", Depth: 2, EdgeTypes: []string{"DESCRIBED_BY"}},
		},
	}}

	got, err := Analyze(context.Background(), Input{Limit: 10}, Providers{Diff: diff, Graph: graph})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("candidate count=%d want 3: %#v", len(got.Candidates), got.Candidates)
	}
	assertCandidate(t, got.Candidates[0], "internal/runtime/runner.go", MustReview, true, []string{"git_diff", "repoindex_graph"})
	assertCandidate(t, got.Candidates[1], "internal/runtime/runner_test.go", ShouldReview, false, []string{"repoindex_graph"})
	assertCandidate(t, got.Candidates[2], "docs/runtime.md", ContextOnly, false, []string{"repoindex_graph"})
	if got.Summary.MustReviewCount != 1 || got.Summary.ShouldReviewCount != 1 || got.Summary.ContextOnlyCount != 1 {
		t.Fatalf("summary=%+v want 1/1/1 ranks", got.Summary)
	}
}

func TestAnalyzeAggregatesRenameSourceAndDestination(t *testing.T) {
	diff := fakeDiff{changes: []Change{
		{Path: "internal/new.go", OldPath: "internal/old.go", Status: "R100"},
	}}

	got, err := Analyze(context.Background(), Input{}, Providers{Diff: diff})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(got.Candidates) != 2 {
		t.Fatalf("candidate count=%d want 2: %#v", len(got.Candidates), got.Candidates)
	}
	byPath := candidatesByPath(got.Candidates)
	if byPath["internal/new.go"].Rank != MustReview {
		t.Fatalf("new rank=%s want must_review", byPath["internal/new.go"].Rank)
	}
	if byPath["internal/old.go"].Rank != ShouldReview {
		t.Fatalf("old rank=%s want should_review", byPath["internal/old.go"].Rank)
	}
	if got.Lanes[1].Status != LaneUnavailable {
		t.Fatalf("graph lane status=%s want unavailable", got.Lanes[1].Status)
	}
}

func TestAnalyzeAddsSemanticNeighborsWithoutFailingOnUnavailableGraph(t *testing.T) {
	diff := fakeDiff{changes: []Change{
		{Path: "internal/runtime/runner.go", Status: "M"},
		{Path: "internal/runtime/queue.go", Status: "M"},
	}}
	graph := fakeGraph{err: assertErr("repoindex offline")}
	semantic := fakeSemantic{result: SemanticResult{
		Available: true,
		Candidates: []SemanticCandidate{
			{Path: "internal/runtime/scheduler.go", Symbol: "Schedule", LineHint: 44, Similarity: 0.91, Source: "vector"},
		},
	}}

	got, err := Analyze(context.Background(), Input{Limit: 10, MaxChanged: 1}, Providers{Diff: diff, Graph: graph, Semantic: semantic})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if len(got.Changes) != 1 {
		t.Fatalf("changed count=%d want capped to 1", len(got.Changes))
	}
	byPath := candidatesByPath(got.Candidates)
	candidate := byPath["internal/runtime/scheduler.go"]
	if candidate.Rank != ShouldReview || !containsString(candidate.Sources, "semantic_neighbors") {
		t.Fatalf("semantic candidate=%+v want should_review from semantic lane", candidate)
	}
	if got.Lanes[1].Status != LaneUnavailable || got.Lanes[2].Status != LaneAvailable {
		t.Fatalf("lanes=%+v want graph unavailable and semantic available", got.Lanes)
	}
}

type fakeDiff struct {
	changes []Change
}

func (f fakeDiff) ChangedFiles(context.Context, Input) ([]Change, error) {
	return f.changes, nil
}

type fakeGraph struct {
	result GraphResult
	err    error
}

func (f fakeGraph) BlastRadius(context.Context, []Change, GraphOptions) (GraphResult, error) {
	return f.result, f.err
}

type fakeSemantic struct {
	result SemanticResult
	err    error
}

func (f fakeSemantic) Neighbors(context.Context, []Change, SemanticOptions) (SemanticResult, error) {
	return f.result, f.err
}

type assertErr string

func (e assertErr) Error() string {
	return string(e)
}

func assertCandidate(t *testing.T, got Candidate, path string, rank Rank, changed bool, sources []string) {
	t.Helper()
	if got.Path != path || got.Rank != rank || got.Changed != changed {
		t.Fatalf("candidate=%+v want path=%s rank=%s changed=%t", got, path, rank, changed)
	}
	for _, source := range sources {
		if !containsString(got.Sources, source) {
			t.Fatalf("candidate sources=%v missing %s", got.Sources, source)
		}
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("candidate=%+v has no reasons", got)
	}
}

func candidatesByPath(candidates []Candidate) map[string]Candidate {
	out := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		out[candidate.Path] = candidate
	}
	return out
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}
