package generalsolver

import (
	"strings"
	"testing"
)

func TestBraidToWorkItems(t *testing.T) {
	state := NewSolverState()
	nodes := []BraidNodeLike{
		{ID: "n_extract", Kind: "extract", Question: "Extract facts", MaxSummaryChars: 4000},
		{ID: "n_solve", Kind: "solve", Question: "Solve the problem", DependsOn: []string{"n_extract"}, HelperPolicy: "preferred"},
		{ID: "n_verify", Kind: "verify", Question: "Verify the answer", DependsOn: []string{"n_solve"}},
		{ID: "n_reduce", Kind: "reduce", Question: "Final answer", DependsOn: []string{"n_verify"}},
	}
	if err := BraidToWorkItems(state, nodes, "n_reduce"); err != nil {
		t.Fatalf("BraidToWorkItems: %v", err)
	}
	if len(state.Items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(state.Items))
	}
	// Root node (no deps) should be ready.
	if state.Items["n_extract"].Status != StatusReady {
		t.Errorf("expected n_extract to be ready, got %q", state.Items["n_extract"].Status)
	}
	// Dependent nodes should be pending.
	if state.Items["n_solve"].Status != StatusPending {
		t.Errorf("expected n_solve to be pending, got %q", state.Items["n_solve"].Status)
	}
	// Archetype mapping.
	if state.Items["n_extract"].Archetype != ArchetypeExplicitDAG {
		t.Errorf("expected explicit_dag for extract, got %q", state.Items["n_extract"].Archetype)
	}
	if state.Items["n_verify"].Archetype != ArchetypeCandidateVerify {
		t.Errorf("expected candidate_verify for verify, got %q", state.Items["n_verify"].Archetype)
	}
	// Final node should have higher priority than a non-final reduce would.
	reducePriority := state.Items["n_reduce"].Priority
	if reducePriority <= 0.5 {
		t.Errorf("expected n_reduce (final) to have elevated priority above base 0.5, got %.2f", reducePriority)
	}
	// Payload should include braid metadata.
	if state.Items["n_solve"].Payload["braid_kind"] != "solve" {
		t.Errorf("expected braid_kind=solve, got %v", state.Items["n_solve"].Payload["braid_kind"])
	}
}

func TestBraidToWorkItemsNilState(t *testing.T) {
	err := BraidToWorkItems(nil, []BraidNodeLike{{ID: "n1", Kind: "solve", Question: "q"}}, "n1")
	if err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestBraidToWorkItemsDuplicateID(t *testing.T) {
	state := NewSolverState()
	nodes := []BraidNodeLike{
		{ID: "n1", Kind: "solve", Question: "q1"},
		{ID: "n1", Kind: "solve", Question: "q2"},
	}
	err := BraidToWorkItems(state, nodes, "n1")
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
}

func TestBraidToWorkItemsDependencyPropagation(t *testing.T) {
	state := NewSolverState()
	nodes := []BraidNodeLike{
		{ID: "n1", Kind: "extract", Question: "q1"},
		{ID: "n2", Kind: "solve", Question: "q2", DependsOn: []string{"n1"}},
		{ID: "n3", Kind: "reduce", Question: "q3", DependsOn: []string{"n2"}},
	}
	_ = BraidToWorkItems(state, nodes, "n3")

	// Solve n1, then n2 should become ready.
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = CommitArtifact(state, "n1", WorkArtifact{Status: "solved", Answer: "facts extracted"})

	if state.Items["n2"].Status != StatusReady {
		t.Errorf("expected n2 ready after n1 solved, got %q", state.Items["n2"].Status)
	}
	if state.Items["n3"].Status != StatusPending {
		t.Errorf("expected n3 still pending, got %q", state.Items["n3"].Status)
	}

	// Solve n2, then n3 should become ready.
	item = state.Items["n2"]
	item.Status = StatusSolving
	state.Items["n2"] = item
	_ = CommitArtifact(state, "n2", WorkArtifact{Status: "solved", Answer: "solution = 42"})

	if state.Items["n3"].Status != StatusReady {
		t.Errorf("expected n3 ready after n2 solved, got %q", state.Items["n3"].Status)
	}
}

func TestBraidSummaryToArtifact(t *testing.T) {
	tests := []struct {
		name         string
		nodeID       string
		summary      string
		wantStatus   string
		wantAnswer   string
	}{
		{
			name:       "solved with solution",
			nodeID:     "n_solve",
			summary:    "status: completed summary: status: solved answer: solution = 42 checks: helper verified",
			wantStatus: "solved",
			wantAnswer: "solution = 42",
		},
		{
			name:       "pass true",
			nodeID:     "n_verify",
			summary:    "status: pass summary: answer: pass: true checks: simulation passed",
			wantStatus: "solved",
			wantAnswer: "pass: true",
		},
		{
			name:       "blocked",
			nodeID:     "n_solve",
			summary:    "status: blocked summary: needs simulation checks: helper timeout",
			wantStatus: "blocked",
			wantAnswer: "",
		},
		{
			name:       "partial",
			nodeID:     "n_solve",
			summary:    "status: partial answer: 42 checks: incomplete verification",
			wantStatus: "partial",
			wantAnswer: "42",
		},
		{
			name:       "verified solve",
			nodeID:     "n_solve",
			summary:    "status: completed summary: status: solved answer: solution = [[1,0,1]] checks: ephemeral_helper_solve verified candidate with a runtime scaffold verifier.",
			wantStatus: "solved",
			wantAnswer: "solution = [[1,0,1]]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artifact := BraidSummaryToArtifact(tt.nodeID, tt.summary)
			if artifact.WorkItemID != tt.nodeID {
				t.Errorf("work_item_id: got %q, want %q", artifact.WorkItemID, tt.nodeID)
			}
			if artifact.Status != tt.wantStatus {
				t.Errorf("status: got %q, want %q", artifact.Status, tt.wantStatus)
			}
			if tt.wantAnswer != "" {
				answer, ok := artifact.Answer.(string)
				if !ok {
					t.Fatalf("answer is not a string: %v", artifact.Answer)
				}
				if !strings.Contains(answer, tt.wantAnswer) {
					t.Errorf("answer: got %q, want to contain %q", answer, tt.wantAnswer)
				}
			}
		})
	}
}

func TestArtifactToBraidSummary(t *testing.T) {
	tests := []struct {
		name     string
		artifact WorkArtifact
		contains string
	}{
		{
			name: "solved artifact",
			artifact: WorkArtifact{
				Status:     ArtifactStatusSolved,
				Answer:     "solution = 42",
				Confidence: 0.9,
			},
			contains: "solution = 42",
		},
		{
			name: "partial artifact",
			artifact: WorkArtifact{
				Status:     ArtifactStatusPartial,
				Answer:     "maybe 42",
				Confidence: 0.5,
			},
			contains: "partial",
		},
		{
			name: "blocked no answer",
			artifact: WorkArtifact{
				Status: ArtifactStatusBlocked,
			},
			contains: "blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := ArtifactToBraidSummary(tt.artifact)
			if !strings.Contains(summary, tt.contains) {
				t.Errorf("summary %q should contain %q", summary, tt.contains)
			}
		})
	}
}

func TestBraidRoundTrip(t *testing.T) {
	// Build state from braid nodes, execute via scheduler, convert back.
	state := NewSolverState()
	nodes := []BraidNodeLike{
		{ID: "n_extract", Kind: "extract", Question: "Extract constraints"},
		{ID: "n_solve", Kind: "solve", Question: "Solve", DependsOn: []string{"n_extract"}, HelperPolicy: "preferred"},
	}
	_ = BraidToWorkItems(state, nodes, "n_solve")

	sched := NewScheduler(state)
	report, err := sched.RunToCompletion(func(item WorkItem) (WorkArtifact, WorkVerdict, error) {
		artifact := WorkArtifact{
			Status:     ArtifactStatusSolved,
			Answer:     "solution = " + item.ID,
			Confidence: 0.85,
		}
		return artifact, WorkVerdict{Accept: true, Confidence: 0.85}, nil
	})
	if err != nil {
		t.Fatalf("RunToCompletion: %v", err)
	}
	if report.Committed != 2 {
		t.Errorf("expected 2 committed, got %d", report.Committed)
	}

	// Convert artifacts back to summaries.
	for _, id := range []string{"n_extract", "n_solve"} {
		artifact := state.Artifacts[id]
		summary := ArtifactToBraidSummary(artifact)
		if !strings.Contains(summary, "solved") {
			t.Errorf("summary for %s should contain solved: %q", id, summary)
		}
	}
}

func TestBraidKindToArchetype(t *testing.T) {
	tests := []struct {
		kind      string
		archetype ProblemArchetype
	}{
		{"extract", ArchetypeExplicitDAG},
		{"solve", ArchetypeExplicitDAG},
		{"cycle_solve", ArchetypeConstraintSolve},
		{"verify", ArchetypeCandidateVerify},
		{"reduce", ArchetypeExplicitDAG},
		{"unknown", ArchetypeMixed},
	}
	for _, tt := range tests {
		got := braidKindToArchetype(tt.kind)
		if got != tt.archetype {
			t.Errorf("braidKindToArchetype(%q) = %q, want %q", tt.kind, got, tt.archetype)
		}
	}
}

func TestExtractBraidNodeLikes(t *testing.T) {
	nodes := ExtractBraidNodeLikes(
		[]string{"n1", "n2"},
		[]string{"extract", "solve"},
		[]string{"Extract facts", "Solve problem"},
		[][]string{{}, {"n1"}},
		[]int{4000, 8000},
		[]string{"", "preferred"},
		[]string{"", "symbolic_trace"},
		[]string{"", "symbolic_trace"},
		[]string{"", "type_inference_v1"},
	)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].ID != "n1" || nodes[0].Kind != "extract" {
		t.Errorf("first node: %+v", nodes[0])
	}
	if nodes[1].ID != "n2" || nodes[1].Kind != "solve" || len(nodes[1].DependsOn) != 1 || nodes[1].Archetype != "symbolic_trace" {
		t.Errorf("second node: %+v", nodes[1])
	}
}
