package generalsolver

import (
	"fmt"
	"strings"
	"testing"
	"testing/quick"
)

func TestValidProblemArchetype(t *testing.T) {
	valid := []ProblemArchetype{
		ArchetypeExplicitDAG, ArchetypeStateTransition, ArchetypeSymbolicTrace,
		ArchetypeGraphSearch, ArchetypeTableRecurrence, ArchetypeConstraintSolve,
		ArchetypeCandidateVerify, ArchetypeMixed,
	}
	for _, a := range valid {
		if !ValidProblemArchetype(a) {
			t.Errorf("expected %q to be valid", a)
		}
	}
	invalid := []ProblemArchetype{"unknown", "", "chemistry_solver", "chess_solver"}
	for _, a := range invalid {
		if ValidProblemArchetype(a) {
			t.Errorf("expected %q to be invalid", a)
		}
	}
}

func TestValidWorkItemStatus(t *testing.T) {
	valid := []WorkItemStatus{
		StatusPending, StatusReady, StatusSolving, StatusSolved, StatusBlocked, StatusFailed,
	}
	for _, s := range valid {
		if !ValidWorkItemStatus(s) {
			t.Errorf("expected %q to be valid", s)
		}
	}
	if ValidWorkItemStatus("unknown") {
		t.Error("expected unknown to be invalid")
	}
}

func TestAddWorkItem(t *testing.T) {
	state := NewSolverState()
	item := WorkItem{
		ID:        "n1",
		Goal:      "solve the first subproblem",
		Archetype: ArchetypeExplicitDAG,
		Priority:  1.0,
		Risk:      0.5,
	}
	if err := AddWorkItem(state, item); err != nil {
		t.Fatalf("AddWorkItem: %v", err)
	}
	if len(state.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(state.Items))
	}
	got := state.Items["n1"]
	if got.Status != StatusPending {
		t.Errorf("expected status %q, got %q", StatusPending, got.Status)
	}
	if got.MaxAttempts != defaultMaxAttempts {
		t.Errorf("expected max_attempts %d, got %d", defaultMaxAttempts, got.MaxAttempts)
	}
}

func TestAddWorkItemDuplicate(t *testing.T) {
	state := NewSolverState()
	item := WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	_ = AddWorkItem(state, item)
	err := AddWorkItem(state, item)
	if err == nil {
		t.Fatal("expected error for duplicate id")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestAddWorkItemEmptyID(t *testing.T) {
	state := NewSolverState()
	err := AddWorkItem(state, WorkItem{ID: "", Goal: "g", Archetype: ArchetypeExplicitDAG})
	if err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestAddWorkItemInvalidArchetype(t *testing.T) {
	state := NewSolverState()
	err := AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: "unknown"})
	if err == nil {
		t.Fatal("expected error for invalid archetype")
	}
}

func TestAddWorkItemRejectsInvalidInitialStatus(t *testing.T) {
	t.Parallel()

	state := NewSolverState()
	err := AddWorkItem(state, WorkItem{
		ID:        "n1",
		Goal:      "g",
		Archetype: ArchetypeExplicitDAG,
		Status:    "done",
	})
	if err == nil {
		t.Fatal("expected invalid status error")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("error=%v want invalid status", err)
	}
	if _, exists := state.Items["n1"]; exists {
		t.Fatalf("invalid status item was inserted: %+v", state.Items["n1"])
	}
}

func TestAddWorkItemRejectsGeneratedUnknownInitialStatuses(t *testing.T) {
	t.Parallel()

	unknownStatusesFailClosed := func(raw string) bool {
		state := NewSolverState()
		err := AddWorkItem(state, WorkItem{
			ID:        "n1",
			Goal:      "g",
			Archetype: ArchetypeExplicitDAG,
			Status:    WorkItemStatus("unknown:" + raw),
		})
		return err != nil && strings.Contains(err.Error(), "invalid status") && len(state.Items) == 0
	}

	if err := quick.Check(unknownStatusesFailClosed, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown status was accepted: %v", err)
	}
}

func TestAddWorkItemSelfDep(t *testing.T) {
	state := NewSolverState()
	err := AddWorkItem(state, WorkItem{
		ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG,
		DependsOn: []string{"n1"},
	})
	if err == nil {
		t.Fatal("expected error for self-dependency")
	}
}

func TestAddWorkItemWithDependencies(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeGraphSearch, DependsOn: []string{"n1"}})
	if len(state.ReverseDeps["n1"]) != 1 || state.ReverseDeps["n1"][0] != "n2" {
		t.Errorf("expected reverse dep n1 -> n2, got %v", state.ReverseDeps["n1"])
	}
}

func TestAddWorkItemMaxCount(t *testing.T) {
	state := NewSolverState()
	for i := 0; i < maxWorkItems; i++ {
		_ = AddWorkItem(state, WorkItem{
			ID:        fmt.Sprintf("n%d", i),
			Goal:      "g",
			Archetype: ArchetypeExplicitDAG,
		})
	}
	err := AddWorkItem(state, WorkItem{ID: "extra", Goal: "g", Archetype: ArchetypeExplicitDAG})
	if err == nil {
		t.Fatal("expected error for exceeding max items")
	}
}

func TestCommitArtifact(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG})
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item

	artifact := WorkArtifact{Status: "solved", Answer: "42", Confidence: 0.9}
	if err := CommitArtifact(state, "n1", artifact); err != nil {
		t.Fatalf("CommitArtifact: %v", err)
	}
	if state.Items["n1"].Status != StatusSolved {
		t.Errorf("expected status %q, got %q", StatusSolved, state.Items["n1"].Status)
	}
	if state.Artifacts["n1"].Answer != "42" {
		t.Errorf("expected answer 42, got %v", state.Artifacts["n1"].Answer)
	}
}

func TestCommitArtifactWrongStatus(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG})
	err := CommitArtifact(state, "n1", WorkArtifact{})
	if err == nil {
		t.Fatal("expected error for committing non-solving item")
	}
}

func TestCommitArtifactPropagation(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"n1"}})

	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = CommitArtifact(state, "n1", WorkArtifact{Status: "solved", Answer: "a1"})

	if state.Items["n2"].Status != StatusReady {
		t.Errorf("expected n2 status ready after n1 solved, got %q", state.Items["n2"].Status)
	}
}

func TestRecordFailure(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 2})
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item

	if err := RecordFailure(state, "n1", "timeout", map[string]any{"duration_ms": 5000}); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if state.Items["n1"].Status != StatusReady {
		t.Errorf("expected status ready after first failure, got %q", state.Items["n1"].Status)
	}
	if state.Items["n1"].Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", state.Items["n1"].Attempts)
	}
	if len(state.FailureLog) != 1 {
		t.Fatalf("expected 1 failure log entry, got %d", len(state.FailureLog))
	}

	item = state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = RecordFailure(state, "n1", "timeout again", nil)
	if state.Items["n1"].Status != StatusFailed {
		t.Errorf("expected status failed after max attempts, got %q", state.Items["n1"].Status)
	}
}

func TestComputeReadyQueue(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Priority: 2.0})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeExplicitDAG, Priority: 1.0, DependsOn: []string{"n1"}})
	_ = AddWorkItem(state, WorkItem{ID: "n3", Goal: "g3", Archetype: ArchetypeGraphSearch, Priority: 3.0})

	queue := ComputeReadyQueue(state)
	if len(queue) != 2 {
		t.Fatalf("expected 2 ready items, got %d: %v", len(queue), queue)
	}
	if queue[0] != "n3" {
		t.Errorf("expected n3 first (higher priority), got %s", queue[0])
	}
	if queue[1] != "n1" {
		t.Errorf("expected n1 second, got %s", queue[1])
	}
}

func TestComputeReadyQueueOrdersByPriorityRiskThenID(t *testing.T) {
	t.Parallel()

	state := NewSolverState()
	for _, item := range []WorkItem{
		{ID: "low-risk", Goal: "g", Archetype: ArchetypeExplicitDAG, Priority: 2.0, Risk: 0.1},
		{ID: "risk-b", Goal: "g", Archetype: ArchetypeExplicitDAG, Priority: 2.0, Risk: 0.9},
		{ID: "risk-a", Goal: "g", Archetype: ArchetypeExplicitDAG, Priority: 2.0, Risk: 0.9},
		{ID: "high", Goal: "g", Archetype: ArchetypeExplicitDAG, Priority: 3.0, Risk: 0.0},
	} {
		if err := AddWorkItem(state, item); err != nil {
			t.Fatalf("AddWorkItem(%s): %v", item.ID, err)
		}
	}

	want := []string{"high", "risk-a", "risk-b", "low-risk"}
	if got := ComputeReadyQueue(state); !sameStringIDs(got, want) {
		t.Fatalf("ready queue=%v want %v", got, want)
	}
	if !sameStringIDs(state.ReadyQueue, want) {
		t.Fatalf("state ready queue=%v want %v", state.ReadyQueue, want)
	}
}

func TestComputeReadyQueueExcludesNonRunnableItems(t *testing.T) {
	t.Parallel()

	state := NewSolverState()
	items := []WorkItem{
		{ID: "ready-root", Goal: "g", Archetype: ArchetypeExplicitDAG, Priority: 1.0},
		{ID: "solved-dep", Goal: "g", Archetype: ArchetypeExplicitDAG, Status: StatusSolved},
		{ID: "waiting-on-solved", Goal: "g", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"solved-dep"}, Priority: 2.0},
		{ID: "unsolved-dep", Goal: "g", Archetype: ArchetypeExplicitDAG},
		{ID: "waiting-on-unsolved", Goal: "g", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"unsolved-dep"}, Priority: 9.0},
		{ID: "currently-solving", Goal: "g", Archetype: ArchetypeExplicitDAG, Status: StatusSolving, Priority: 10.0},
		{ID: "already-solved", Goal: "g", Archetype: ArchetypeExplicitDAG, Status: StatusSolved, Priority: 10.0},
		{ID: "blocked", Goal: "g", Archetype: ArchetypeExplicitDAG, Status: StatusBlocked, Priority: 10.0},
		{ID: "failed", Goal: "g", Archetype: ArchetypeExplicitDAG, Status: StatusFailed, Priority: 10.0},
	}
	for _, item := range items {
		if err := AddWorkItem(state, item); err != nil {
			t.Fatalf("AddWorkItem(%s): %v", item.ID, err)
		}
	}

	want := []string{"waiting-on-solved", "ready-root", "unsolved-dep"}
	if got := ComputeReadyQueue(state); !sameStringIDs(got, want) {
		t.Fatalf("ready queue=%v want %v", got, want)
	}
}

func TestCompactFailureDigest(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 5})
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = RecordFailure(state, "n1", "helper timeout", nil)
	_ = RecordFailure(state, "n1", "helper failed again", nil)

	digest := CompactFailureDigest(state)
	if digest == "" {
		t.Fatal("expected non-empty digest")
	}
	if !strings.Contains(digest, "n1") {
		t.Errorf("expected digest to mention n1, got: %s", digest)
	}
	if !strings.Contains(digest, "2 failures") {
		t.Errorf("expected digest to mention 2 failures, got: %s", digest)
	}
	if len(state.FailureLog) != 0 {
		t.Errorf("expected failure log to be cleared after compaction, got %d entries", len(state.FailureLog))
	}
	if len(state.Digests) != 1 {
		t.Errorf("expected 1 digest, got %d", len(state.Digests))
	}
}

func TestSummarizeState(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Priority: 1.0})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeGraphSearch, Priority: 1.0})

	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = CommitArtifact(state, "n1", WorkArtifact{Status: "solved", Answer: "42"})

	summary := SummarizeState(state)
	if summary.TotalItems != 2 {
		t.Errorf("expected 2 items, got %d", summary.TotalItems)
	}
	if len(summary.SolvedIDs) != 1 || summary.SolvedIDs[0] != "n1" {
		t.Errorf("expected solved=[n1], got %v", summary.SolvedIDs)
	}
	if summary.ByStatus[StatusSolved] != 1 {
		t.Errorf("expected 1 solved, got %d", summary.ByStatus[StatusSolved])
	}
	if summary.ReadyCount < 1 {
		t.Errorf("expected at least 1 ready, got %d", summary.ReadyCount)
	}
}

func TestValidateSolverState(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"n1"}})
	if err := ValidateSolverState(state); err != nil {
		t.Fatalf("ValidateSolverState: %v", err)
	}
}

func TestValidateSolverStateCycle(t *testing.T) {
	state := NewSolverState()
	state.Items["n1"] = WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Status: StatusPending, DependsOn: []string{"n2"}}
	state.Items["n2"] = WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeExplicitDAG, Status: StatusPending, DependsOn: []string{"n1"}}
	err := ValidateSolverState(state)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected cycle error, got: %v", err)
	}
}

func TestValidateSolverStateUnknownDep(t *testing.T) {
	state := NewSolverState()
	state.Items["n1"] = WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Status: StatusPending, DependsOn: []string{"n99"}}
	err := ValidateSolverState(state)
	if err == nil {
		t.Fatal("expected unknown dep error")
	}
}

func sameStringIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
