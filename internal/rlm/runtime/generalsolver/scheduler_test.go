package generalsolver

import (
	"fmt"
	"strings"
	"testing"
)

func TestSchedulerPickNext(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Priority: 1.0})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeGraphSearch, Priority: 2.0})

	sched := NewScheduler(state)
	item, ok := sched.PickNext()
	if !ok {
		t.Fatal("expected to pick an item")
	}
	if item.ID != "n2" {
		t.Errorf("expected n2 (higher priority), got %s", item.ID)
	}
	if state.Items["n2"].Status != StatusSolving {
		t.Errorf("expected status solving, got %q", state.Items["n2"].Status)
	}
}

func TestSchedulerPickNextEmpty(t *testing.T) {
	state := NewSolverState()
	sched := NewScheduler(state)
	_, ok := sched.PickNext()
	if ok {
		t.Error("expected no item from empty state")
	}
}

func TestSchedulerCommit(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG})
	sched := NewScheduler(state)
	item, _ := sched.PickNext()
	artifact := WorkArtifact{Status: "solved", Answer: "result", Confidence: 0.95}
	if err := sched.Commit(item.ID, artifact); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if state.Items["n1"].Status != StatusSolved {
		t.Errorf("expected solved, got %q", state.Items["n1"].Status)
	}
}

func TestSchedulerRecordFailure(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 3})
	sched := NewScheduler(state)
	_, _ = sched.PickNext()
	if err := sched.RecordFailure("n1", "bad output", nil); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	if state.Items["n1"].Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", state.Items["n1"].Attempts)
	}
}

func TestSchedulerIsComplete(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG})
	sched := NewScheduler(state)
	if sched.IsComplete() {
		t.Error("expected not complete with pending item")
	}
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = sched.Commit("n1", WorkArtifact{Status: "solved", Answer: "42"})
	if !sched.IsComplete() {
		t.Error("expected complete after all solved")
	}
}

func TestSchedulerHasBlockedOrFailed(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 1})
	sched := NewScheduler(state)
	if sched.HasBlockedOrFailed() {
		t.Error("expected no blocked/failed initially")
	}
	item := state.Items["n1"]
	item.Status = StatusSolving
	state.Items["n1"] = item
	_ = sched.RecordFailure("n1", "maxed out", nil)
	if !sched.HasBlockedOrFailed() {
		t.Error("expected blocked/failed after max attempts")
	}
}

func TestSchedulerRunToCompletion(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g1", Archetype: ArchetypeExplicitDAG, Priority: 1.0})
	_ = AddWorkItem(state, WorkItem{ID: "n2", Goal: "g2", Archetype: ArchetypeGraphSearch, Priority: 2.0, DependsOn: []string{"n1"}})

	sched := NewScheduler(state)
	report, err := sched.RunToCompletion(func(item WorkItem) (WorkArtifact, WorkVerdict, error) {
		return WorkArtifact{
				Status:     "solved",
				Answer:     "answer_for_" + item.ID,
				Confidence: 0.9,
			}, WorkVerdict{
				Accept:     true,
				Confidence: 0.9,
			}, nil
	})
	if err != nil {
		t.Fatalf("RunToCompletion: %v", err)
	}
	if report.Committed != 2 {
		t.Errorf("expected 2 committed, got %d", report.Committed)
	}
	if !sched.IsComplete() {
		t.Error("expected complete")
	}
}

func TestSchedulerRunToCompletionWithRepair(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 3})

	sched := NewScheduler(state)
	callCount := 0
	report, err := sched.RunToCompletion(func(item WorkItem) (WorkArtifact, WorkVerdict, error) {
		callCount++
		if callCount == 1 {
			return WorkArtifact{Status: "partial", Confidence: 0.3}, WorkVerdict{
				Accept:     false,
				Repairable: true,
				Confidence: 0.3,
			}, nil
		}
		return WorkArtifact{Status: "solved", Answer: "recovered", Confidence: 0.95}, WorkVerdict{
			Accept:     true,
			Confidence: 0.95,
		}, nil
	})
	if err != nil {
		t.Fatalf("RunToCompletion: %v", err)
	}
	if report.Committed != 1 {
		t.Errorf("expected 1 committed, got %d", report.Committed)
	}
	if report.Repairs != 1 {
		t.Errorf("expected 1 repair, got %d", report.Repairs)
	}
}

func TestSchedulerRunToCompletionError(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG, MaxAttempts: 2})

	sched := NewScheduler(state)
	report, err := sched.RunToCompletion(func(item WorkItem) (WorkArtifact, WorkVerdict, error) {
		return WorkArtifact{}, WorkVerdict{}, fmt.Errorf("helper crash")
	})
	if err != nil {
		t.Fatalf("RunToCompletion should not return error for item failures: %v", err)
	}
	if report.ItemsProcessed < 1 {
		t.Errorf("expected at least 1 processed, got %d", report.ItemsProcessed)
	}
	if len(report.Errors) == 0 {
		t.Error("expected errors in report")
	}
}

func TestSchedulerNilState(t *testing.T) {
	sched := NewScheduler(nil)
	_, ok := sched.PickNext()
	if ok {
		t.Error("expected no item from nil state")
	}
	if !sched.IsComplete() {
		t.Error("nil state should be complete")
	}
}

func TestValidateSolverStateValid(t *testing.T) {
	state := NewSolverState()
	_ = AddWorkItem(state, WorkItem{ID: "a", Goal: "g", Archetype: ArchetypeExplicitDAG})
	_ = AddWorkItem(state, WorkItem{ID: "b", Goal: "g", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"a"}})
	_ = AddWorkItem(state, WorkItem{ID: "c", Goal: "g", Archetype: ArchetypeExplicitDAG, DependsOn: []string{"a", "b"}})
	if err := ValidateSolverState(state); err != nil {
		t.Fatalf("ValidateSolverState: %v", err)
	}
}

func TestValidateSolverStateKeyMismatch(t *testing.T) {
	state := NewSolverState()
	state.Items["wrong_key"] = WorkItem{ID: "n1", Goal: "g", Archetype: ArchetypeExplicitDAG}
	err := ValidateSolverState(state)
	if err == nil {
		t.Fatal("expected key mismatch error")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Errorf("unexpected error: %v", err)
	}
}
