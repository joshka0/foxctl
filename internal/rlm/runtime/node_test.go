package runtime

import (
	"errors"
	"testing"
	"testing/quick"
	"time"
)

func TestNodeStatusTransitionTable(t *testing.T) {
	t.Parallel()

	allowed := map[NodeStatus]map[NodeStatus]bool{
		NodeStatusQueued: {
			NodeStatusQueued:   true,
			NodeStatusRunning:  true,
			NodeStatusWaiting:  true,
			NodeStatusCanceled: true,
		},
		NodeStatusRunning: {
			NodeStatusRunning:   true,
			NodeStatusWaiting:   true,
			NodeStatusCompleted: true,
			NodeStatusFailed:    true,
			NodeStatusCanceled:  true,
		},
		NodeStatusWaiting: {
			NodeStatusRunning:   true,
			NodeStatusWaiting:   true,
			NodeStatusCompleted: true,
			NodeStatusFailed:    true,
			NodeStatusCanceled:  true,
		},
		NodeStatusCompleted: {
			NodeStatusCompleted: true,
		},
		NodeStatusFailed: {
			NodeStatusFailed: true,
		},
		NodeStatusCanceled: {
			NodeStatusCanceled: true,
		},
	}

	for _, from := range allNodeStatuses() {
		for _, to := range allNodeStatuses() {
			want := allowed[from][to]
			if got := CanTransitionNodeStatus(from, to); got != want {
				t.Fatalf("CanTransitionNodeStatus(%s, %s)=%v want %v", from, to, got, want)
			}

			err := ValidateNodeStatusTransition(from, to)
			if want && err != nil {
				t.Fatalf("ValidateNodeStatusTransition(%s, %s) error=%v, want nil", from, to, err)
			}
			if !want && !errors.Is(err, ErrInvalidNodeStatusTransition) {
				t.Fatalf("ValidateNodeStatusTransition(%s, %s) error=%v, want ErrInvalidNodeStatusTransition", from, to, err)
			}
		}
	}
}

func TestNodeStatusTerminalSet(t *testing.T) {
	t.Parallel()

	terminal := map[NodeStatus]bool{
		NodeStatusCompleted: true,
		NodeStatusFailed:    true,
		NodeStatusCanceled:  true,
	}
	for _, status := range allNodeStatuses() {
		if got := status.IsTerminal(); got != terminal[status] {
			t.Fatalf("%s.IsTerminal()=%v want %v", status, got, terminal[status])
		}
	}
}

func TestNodeStatusRejectsGeneratedUnknowns(t *testing.T) {
	t.Parallel()

	unknownsAreInvalid := func(raw string) bool {
		status := NodeStatus("unknown:" + raw)
		if status.IsValid() {
			return false
		}
		if CanTransitionNodeStatus(status, NodeStatusQueued) || CanTransitionNodeStatus(NodeStatusQueued, status) {
			return false
		}
		return errors.Is(ValidateNodeStatusTransition(status, NodeStatusQueued), ErrInvalidNodeStatus) &&
			errors.Is(ValidateNodeStatusTransition(NodeStatusQueued, status), ErrInvalidNodeStatus)
	}
	if err := quick.Check(unknownsAreInvalid, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown node status was accepted: %v", err)
	}
}

func TestApplyNodeStatusTransitionSetsLifecycleTimestampsOnce(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.April, 5, 10, 0, 0, 0, time.UTC)
	wait := start.Add(time.Minute)
	resume := wait.Add(time.Minute)
	finish := resume.Add(time.Minute)
	later := finish.Add(time.Minute)

	node := Node{RunID: "run-1", ID: "root", Status: NodeStatusQueued}

	running, err := ApplyNodeStatusTransition(node, NodeStatusRunning, start)
	if err != nil {
		t.Fatalf("queued -> running error=%v", err)
	}
	if running.Status != NodeStatusRunning || !running.StartedAt.Equal(start) || !running.UpdatedAt.Equal(start) || !running.FinishedAt.IsZero() {
		t.Fatalf("running lifecycle timestamps invalid: %+v", running)
	}

	waiting, err := ApplyNodeStatusTransition(running, NodeStatusWaiting, wait)
	if err != nil {
		t.Fatalf("running -> waiting error=%v", err)
	}
	if !waiting.StartedAt.Equal(start) || !waiting.UpdatedAt.Equal(wait) || !waiting.FinishedAt.IsZero() {
		t.Fatalf("waiting lifecycle timestamps invalid: %+v", waiting)
	}

	runningAgain, err := ApplyNodeStatusTransition(waiting, NodeStatusRunning, resume)
	if err != nil {
		t.Fatalf("waiting -> running error=%v", err)
	}
	if !runningAgain.StartedAt.Equal(start) || !runningAgain.UpdatedAt.Equal(resume) || !runningAgain.FinishedAt.IsZero() {
		t.Fatalf("resumed running lifecycle timestamps invalid: %+v", runningAgain)
	}

	completed, err := ApplyNodeStatusTransition(runningAgain, NodeStatusCompleted, finish)
	if err != nil {
		t.Fatalf("running -> completed error=%v", err)
	}
	if completed.Status != NodeStatusCompleted || !completed.StartedAt.Equal(start) || !completed.UpdatedAt.Equal(finish) || !completed.FinishedAt.Equal(finish) {
		t.Fatalf("completed lifecycle timestamps invalid: %+v", completed)
	}

	completedAgain, err := ApplyNodeStatusTransition(completed, NodeStatusCompleted, later)
	if err != nil {
		t.Fatalf("completed -> completed error=%v", err)
	}
	if !completedAgain.UpdatedAt.Equal(finish) || !completedAgain.FinishedAt.Equal(finish) {
		t.Fatalf("self-transition changed terminal timestamps: before=%+v after=%+v", completed, completedAgain)
	}
}

func allNodeStatuses() []NodeStatus {
	return []NodeStatus{
		NodeStatusQueued,
		NodeStatusRunning,
		NodeStatusWaiting,
		NodeStatusCompleted,
		NodeStatusFailed,
		NodeStatusCanceled,
	}
}
