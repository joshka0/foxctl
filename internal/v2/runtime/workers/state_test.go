package workers

import (
	"context"
	"testing"
	"time"

	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

func TestStateComponent_AppliesEventsAndBuildsSnapshot(t *testing.T) {
	t.Parallel()

	component := NewStateComponent(Config{Buffer: 8})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := component.Run(ctx); err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}()

	now := time.Date(2026, time.April, 6, 16, 0, 0, 0, time.UTC)
	events := []coreworker.LifecycleEvent{
		{
			EventKind:      coreworker.EventWorkerSpawned,
			ObservedAt:     now,
			WorkerID:       "worker-child-1",
			BackendKind:    coreworker.BackendSubprocess,
			AgentID:        "agent:child-1",
			RunID:          "run-1",
			ParentAgentID:  "agent:parent",
			ParentWorkerID: "worker-parent-1",
			Status:         coreworker.StatusStarting,
			Role:           "worker",
		},
		{
			EventKind:  coreworker.EventWorkerStarted,
			ObservedAt: now.Add(time.Second),
			WorkerID:   "worker-child-1",
			Status:     coreworker.StatusRunning,
			PID:        "1234",
		},
		{
			EventKind:  coreworker.EventWorkerHeartbeat,
			ObservedAt: now.Add(2 * time.Second),
			WorkerID:   "worker-child-1",
			Status:     coreworker.StatusRunning,
		},
		{
			EventKind:  coreworker.EventWorkerCompleted,
			ObservedAt: now.Add(3 * time.Second),
			WorkerID:   "worker-child-1",
			Status:     coreworker.StatusCompleted,
			StopReason: "done",
		},
	}
	for _, evt := range events {
		if err := component.Publish(context.Background(), evt); err != nil {
			t.Fatalf("Publish(%s) error = %v", evt.EventKind, err)
		}
	}

	snapshot := waitForWorker(t, component, "worker-child-1")
	record := snapshot.Workers["worker-child-1"]
	if record.Status != coreworker.StatusCompleted {
		t.Fatalf("status=%q want %q", record.Status, coreworker.StatusCompleted)
	}
	if record.PID != "1234" {
		t.Fatalf("pid=%q want 1234", record.PID)
	}
	if record.StopReason != "done" {
		t.Fatalf("stop_reason=%q want done", record.StopReason)
	}
	parentChildren := snapshot.ChildrenByParent["worker:worker-parent-1"]
	if len(parentChildren) != 1 || parentChildren[0] != "worker-child-1" {
		t.Fatalf("children=%v want [worker-child-1]", parentChildren)
	}

	cancel()
	<-done
}

func TestStateComponent_TerminalStateIsMonotonic(t *testing.T) {
	t.Parallel()

	component := NewStateComponent(Config{Buffer: 4})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = component.Run(ctx)
	}()

	now := time.Date(2026, time.April, 6, 16, 10, 0, 0, time.UTC)
	if err := component.Publish(context.Background(), coreworker.LifecycleEvent{
		EventKind:  coreworker.EventWorkerCompleted,
		ObservedAt: now,
		WorkerID:   "worker-1",
		Status:     coreworker.StatusCompleted,
		StopReason: "done",
	}); err != nil {
		t.Fatalf("publish completed: %v", err)
	}
	if err := component.Publish(context.Background(), coreworker.LifecycleEvent{
		EventKind:  coreworker.EventWorkerStateChanged,
		ObservedAt: now.Add(time.Second),
		WorkerID:   "worker-1",
		Status:     coreworker.StatusRunning,
	}); err != nil {
		t.Fatalf("publish regression: %v", err)
	}

	snapshot := waitForWorker(t, component, "worker-1")
	if got := snapshot.Workers["worker-1"].Status; got != coreworker.StatusCompleted {
		t.Fatalf("status=%q want %q", got, coreworker.StatusCompleted)
	}
}

func TestStateComponent_DropNewestBackpressure(t *testing.T) {
	t.Parallel()

	component := NewStateComponent(Config{
		Buffer:         1,
		OverflowPolicy: OverflowDropNewest,
	})
	first := coreworker.LifecycleEvent{WorkerID: "worker-1", Status: coreworker.StatusPending}
	second := coreworker.LifecycleEvent{WorkerID: "worker-2", Status: coreworker.StatusPending}
	if err := component.Publish(context.Background(), first); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := component.Publish(context.Background(), second); err != nil {
		t.Fatalf("publish second: %v", err)
	}

	stats := component.Stats()
	if stats.Dropped != 1 {
		t.Fatalf("dropped=%d want 1", stats.Dropped)
	}
	if stats.QueueDepth != 1 {
		t.Fatalf("queue_depth=%d want 1", stats.QueueDepth)
	}
}

func TestStateComponent_BlockBackpressure(t *testing.T) {
	t.Parallel()

	component := NewStateComponent(Config{
		Buffer:         1,
		OverflowPolicy: OverflowBlock,
		PublishTimeout: 10 * time.Millisecond,
	})
	if err := component.Publish(context.Background(), coreworker.LifecycleEvent{WorkerID: "worker-1"}); err != nil {
		t.Fatalf("publish first: %v", err)
	}
	if err := component.Publish(context.Background(), coreworker.LifecycleEvent{WorkerID: "worker-2"}); err != ErrBackpressure {
		t.Fatalf("publish second err=%v want %v", err, ErrBackpressure)
	}
	if stats := component.Stats(); stats.Backpressure != 1 {
		t.Fatalf("backpressure=%d want 1", stats.Backpressure)
	}
}

func TestStateComponent_PublishAfterRunStopsReturnsClosed(t *testing.T) {
	t.Parallel()

	component := NewStateComponent(Config{Buffer: 1})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- component.Run(ctx)
	}()

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	err := component.Publish(context.Background(), coreworker.LifecycleEvent{WorkerID: "worker-after-stop"})
	if err != ErrClosed {
		t.Fatalf("Publish() error = %v want %v", err, ErrClosed)
	}
}

func waitForWorker(t *testing.T, component *StateComponent, workerID string) Snapshot {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		snapshot := component.Snapshot()
		if _, ok := snapshot.Workers[workerID]; ok {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("worker %q not found in snapshot", workerID)
	return Snapshot{}
}
