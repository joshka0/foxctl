package generalsolver

import (
	"strings"
	"testing"
)

func TestRenderStateMarkdownBasic(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Priority: 1.0})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeGraphSearch, Priority: 1.0})

	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = CommitArtifact(state, "n1", WorkArtifact{Status: "solved", Answer: "42"})

	md := RenderStateMarkdown(state)
	if !strings.Contains(md, "# Solver State") {
		t.Error("expected heading")
	}
	if !strings.Contains(md, "Total items: 2") {
		t.Error("expected total items count")
	}
	if !strings.Contains(md, "Solved: 1") {
		t.Error("expected solved count")
	}
	if !strings.Contains(md, "n1") {
		t.Error("expected n1 in solved list")
	}
	if !strings.Contains(md, "explicit_dag") {
		t.Error("expected archetype in summary")
	}
}

func TestRenderStateMarkdownNil(t *testing.T) {
	md := RenderStateMarkdown(nil)
	if !strings.Contains(md, "nil state") {
		t.Error("expected nil state marker")
	}
}

func TestRenderStateMarkdownWithFailures(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 1})
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = RecordFailure(state, "n1", "test failure", nil)

	md := RenderStateMarkdown(state)
	if !strings.Contains(md, "Blocked/Failed") {
		t.Error("expected blocked/failed section")
	}
	if !strings.Contains(md, "n1") {
		t.Error("expected n1 in blocked list")
	}
}

func TestRenderStateMarkdownWithDigests(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 5})
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = RecordFailure(state, "n1", "failure", nil)
	CompactFailureDigest(state)

	md := RenderStateMarkdown(state)
	if !strings.Contains(md, "Compaction Digests") {
		t.Error("expected compaction digests section")
	}
}

func TestRenderWorkItemMarkdown(t *testing.T) {
	item := WorkItem{
		ID:          "n1",
		Goal:        "solve the DAG",
		Archetype:   ArchetypeExplicitDAG,
		Status:      StatusReady,
		Priority:    1.5,
		Risk:        0.3,
		Attempts:    0,
		MaxAttempts: 5,
		DependsOn:   []string{"n0"},
	}
	md := RenderWorkItemMarkdown(item)
	if !strings.Contains(md, "n1") {
		t.Error("expected item id")
	}
	if !strings.Contains(md, "solve the DAG") {
		t.Error("expected goal")
	}
	if !strings.Contains(md, "explicit_dag") {
		t.Error("expected archetype")
	}
	if !strings.Contains(md, "ready") {
		t.Error("expected status")
	}
	if !strings.Contains(md, "n0") {
		t.Error("expected dependency")
	}
}

func TestRenderArtifactMarkdown(t *testing.T) {
	artifact := WorkArtifact{
		WorkItemID: "n1",
		Status:     "solved",
		Confidence: 0.95,
		Answer:     "42",
		Code:       "def solve(): return 42",
		Checks:     []string{"type check passed", "value check passed"},
		Counterexamples: []map[string]any{
			{"input": 3, "expected": 9},
		},
	}
	md := RenderArtifactMarkdown(artifact)
	if !strings.Contains(md, "n1") {
		t.Error("expected work item id")
	}
	if !strings.Contains(md, "solved") {
		t.Error("expected status")
	}
	if !strings.Contains(md, "42") {
		t.Error("expected answer")
	}
	if !strings.Contains(md, "def solve") {
		t.Error("expected code")
	}
	if !strings.Contains(md, "type check passed") {
		t.Error("expected checks")
	}
	if !strings.Contains(md, "input") {
		t.Error("expected counterexample")
	}
}

func TestRenderArtifactMarkdownNilAnswer(t *testing.T) {
	artifact := WorkArtifact{
		WorkItemID: "n1",
		Status:     "partial",
		Confidence: 0.5,
	}
	md := RenderArtifactMarkdown(artifact)
	if !strings.Contains(md, "no answer") {
		t.Error("expected no answer marker")
	}
}
