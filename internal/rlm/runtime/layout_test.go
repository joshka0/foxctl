package runtime

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanNodeLayoutRootAndChildren(t *testing.T) {
	t.Parallel()

	rootLayout, err := PlanNodeLayout("/tmp/out", "run-main", "root")
	if err != nil {
		t.Fatalf("PlanNodeLayout(root) error = %v", err)
	}
	childLayout, err := PlanNodeLayout("/tmp/out", "run-main", "root.1")
	if err != nil {
		t.Fatalf("PlanNodeLayout(child) error = %v", err)
	}

	wantRunRoot := filepath.Join("/tmp/out", "runs", "run-main")
	if rootLayout.RunRoot != wantRunRoot {
		t.Fatalf("root RunRoot = %q, want %q", rootLayout.RunRoot, wantRunRoot)
	}
	if rootLayout.RunJSON != filepath.Join(wantRunRoot, "run.json") {
		t.Fatalf("root RunJSON = %q", rootLayout.RunJSON)
	}
	if rootLayout.TreeJSON != filepath.Join(wantRunRoot, "tree.json") {
		t.Fatalf("root TreeJSON = %q", rootLayout.TreeJSON)
	}
	if rootLayout.NodeDir != filepath.Join(wantRunRoot, "nodes", "root") {
		t.Fatalf("root NodeDir = %q", rootLayout.NodeDir)
	}
	if rootLayout.ResultJSON != filepath.Join(rootLayout.NodeDir, "result.json") {
		t.Fatalf("root ResultJSON = %q", rootLayout.ResultJSON)
	}
	if rootLayout.TrajectoryJSONL != filepath.Join(rootLayout.NodeDir, "trajectory.jsonl") {
		t.Fatalf("root TrajectoryJSONL = %q", rootLayout.TrajectoryJSONL)
	}
	if rootLayout.ArtifactsDir != filepath.Join(rootLayout.NodeDir, "artifacts") {
		t.Fatalf("root ArtifactsDir = %q", rootLayout.ArtifactsDir)
	}
	if rootLayout.ScratchDir != filepath.Join(rootLayout.NodeDir, "scratch") {
		t.Fatalf("root ScratchDir = %q", rootLayout.ScratchDir)
	}

	if childLayout.RunRoot != rootLayout.RunRoot {
		t.Fatalf("child RunRoot = %q, want %q", childLayout.RunRoot, rootLayout.RunRoot)
	}
	if childLayout.NodeID != "root-1" {
		t.Fatalf("child NodeID = %q, want root-1", childLayout.NodeID)
	}
	if childLayout.NodeDir != filepath.Join(wantRunRoot, "nodes", "root-1") {
		t.Fatalf("child NodeDir = %q", childLayout.NodeDir)
	}
}

func TestPlanNodeLayoutSanitizesIDs(t *testing.T) {
	t.Parallel()

	layout, err := PlanNodeLayout("/tmp/out", " Run.ID/2026 ", " Root.Agent#2 ")
	if err != nil {
		t.Fatalf("PlanNodeLayout error = %v", err)
	}

	if layout.RunID != "run-id-2026" {
		t.Fatalf("RunID = %q, want run-id-2026", layout.RunID)
	}
	if layout.NodeID != "root-agent-2" {
		t.Fatalf("NodeID = %q, want root-agent-2", layout.NodeID)
	}
	if layout.RunRoot != filepath.Join("/tmp/out", "runs", "run-id-2026") {
		t.Fatalf("RunRoot = %q", layout.RunRoot)
	}
	if layout.NodeDir != filepath.Join(layout.RunRoot, "nodes", "root-agent-2") {
		t.Fatalf("NodeDir = %q", layout.NodeDir)
	}

	_, err = PlanNodeLayout("/tmp/out", "ok", "***")
	if err == nil {
		t.Fatal("PlanNodeLayout with empty normalized node ID error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "node ID") {
		t.Fatalf("node ID error = %q, want mention of node ID", err)
	}
}

func TestPlanNodeLayoutRejectsEmptyRunID(t *testing.T) {
	t.Parallel()

	_, err := PlanNodeLayout("/tmp/out", "...", "root")
	if err == nil {
		t.Fatal("PlanNodeLayout error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "run ID") {
		t.Fatalf("error = %q, want mention of run ID", err)
	}
}
