package view

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/tooling/evolve/model"
)

func TestBuildStatusSummaryDeterministicCountsAndFrontier(t *testing.T) {
	now := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	run := model.Run{
		ID:               "run-1",
		WorkspacePath:    "/repo",
		TargetPath:       "/repo/pkg",
		BenchmarkCommand: "go test ./...",
		Metric:           model.MetricMax,
		Status:           model.RunStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	nodes := []model.Node{
		{ID: "root", RunID: run.ID, Status: model.NodeStatusRoot, CreatedAt: now, UpdatedAt: now},
		{ID: "c1", RunID: run.ID, ParentID: "root", Status: model.NodeStatusPending, CreatedAt: now.Add(2 * time.Minute), UpdatedAt: now},
		{ID: "c2", RunID: run.ID, ParentID: "root", Status: model.NodeStatusFailed, CreatedAt: now.Add(3 * time.Minute), UpdatedAt: now},
		{ID: "c3", RunID: run.ID, ParentID: "root", Status: model.NodeStatusEvaluated, CreatedAt: now.Add(4 * time.Minute), UpdatedAt: now},
	}
	frontier := []model.Node{nodes[2], nodes[1], nodes[3]}

	summary := BuildStatusSummary(run, nodes, frontier)
	if summary.TotalNodes != 4 {
		t.Fatalf("total nodes = %d, want 4", summary.TotalNodes)
	}
	if summary.FrontierCount != 3 {
		t.Fatalf("frontier count = %d, want 3", summary.FrontierCount)
	}
	if got, want := summary.FrontierNodeIDs, []string{"c1", "c2", "c3"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("frontier ids = %#v, want %#v", got, want)
	}

	expected := map[model.NodeStatus]int{
		model.NodeStatusRoot:      1,
		model.NodeStatusPending:   1,
		model.NodeStatusActive:    0,
		model.NodeStatusCommitted: 0,
		model.NodeStatusEvaluated: 1,
		model.NodeStatusFailed:    1,
		model.NodeStatusDiscarded: 0,
		model.NodeStatusPruned:    0,
	}
	for _, bucket := range summary.NodeCounts {
		if expected[bucket.Status] != bucket.Count {
			t.Fatalf("status %s count = %d, want %d", bucket.Status, bucket.Count, expected[bucket.Status])
		}
	}
}

func TestBuildTreeViewDeterministicOrderingAndRendering(t *testing.T) {
	now := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	score := 1.5
	nodes := []model.Node{
		{
			ID:        "root",
			RunID:     "run-1",
			Status:    model.NodeStatusRoot,
			CreatedAt: now,
			UpdatedAt: now,
		},
		{
			ID:        "child-b",
			RunID:     "run-1",
			ParentID:  "root",
			Status:    model.NodeStatusPending,
			CreatedAt: now.Add(2 * time.Minute),
			UpdatedAt: now,
		},
		{
			ID:        "child-a",
			RunID:     "run-1",
			ParentID:  "root",
			Status:    model.NodeStatusEvaluated,
			Score:     &score,
			CreatedAt: now.Add(1 * time.Minute),
			UpdatedAt: now,
		},
	}

	tree := BuildTreeView("run-1", nodes, nodes[1:])
	if tree.NodeCount != 3 {
		t.Fatalf("node count = %d, want 3", tree.NodeCount)
	}
	if len(tree.Roots) != 1 || tree.Roots[0].ID != "root" {
		t.Fatalf("roots = %#v", tree.Roots)
	}
	children := tree.Roots[0].Children
	if len(children) != 2 {
		t.Fatalf("child count = %d, want 2", len(children))
	}
	if children[0].ID != "child-a" || children[1].ID != "child-b" {
		t.Fatalf("child order = [%s, %s], want [child-a, child-b]", children[0].ID, children[1].ID)
	}

	expected := "root [root]\n  child-a [evaluated] score=1.5000\n  child-b [pending]"
	if tree.Rendered != expected {
		t.Fatalf("rendered tree:\n%s\nwant:\n%s", tree.Rendered, expected)
	}
}
