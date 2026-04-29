package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSchedulerSubmitReturnsBeforeChildCompletes(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	release := make(chan struct{})
	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		<-release
		return NodeResult{Summary: "done", Answer: input.Prompt}, nil
	})

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    backend,
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	handle, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "child prompt"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if handle.NodeID != "root.1" {
		t.Fatalf("handle.NodeID = %q, want root.1", handle.NodeID)
	}

	waitResult, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		MinComplete: 1,
		Timeout:     30 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(waitResult.Completed) != 0 {
		t.Fatalf("completed = %d, want 0 before backend release", len(waitResult.Completed))
	}
	if len(waitResult.Pending) != 1 {
		t.Fatalf("pending = %d, want 1 before backend release", len(waitResult.Pending))
	}

	close(release)
	waitResult, err = scheduler.Wait(context.Background(), "root", WaitRequest{
		MinComplete: 1,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("Wait() after release error = %v", err)
	}
	if len(waitResult.Completed) != 1 {
		t.Fatalf("completed after release = %d, want 1", len(waitResult.Completed))
	}
}

func TestSchedulerWaitCollectsMultipleChildren(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		return NodeResult{Summary: fmt.Sprintf("done:%s", input.Prompt), Answer: input.Prompt}, nil
	})

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    backend,
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 2,
	})

	for i := 0; i < 3; i++ {
		_, err := scheduler.Submit(context.Background(), "root", QueryRequest{
			Prompt: fmt.Sprintf("job-%d", i+1),
		})
		if err != nil {
			t.Fatalf("Submit(%d) error = %v", i+1, err)
		}
	}

	waitResult, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(waitResult.Completed) != 3 {
		t.Fatalf("completed = %d, want 3", len(waitResult.Completed))
	}
	if len(waitResult.Failed) != 0 {
		t.Fatalf("failed = %d, want 0", len(waitResult.Failed))
	}
	if len(waitResult.Pending) != 0 {
		t.Fatalf("pending = %d, want 0", len(waitResult.Pending))
	}
}

func TestSchedulerWaitMinComplete(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	blockSlow := make(chan struct{})
	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		if input.Prompt == "slow" {
			<-blockSlow
		}
		return NodeResult{Summary: input.Prompt, Answer: input.Prompt}, nil
	})

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    backend,
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	if _, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "fast"}); err != nil {
		t.Fatalf("Submit(fast) error = %v", err)
	}
	if _, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "slow"}); err != nil {
		t.Fatalf("Submit(slow) error = %v", err)
	}

	waitResult, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		MinComplete: 1,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("Wait(min_complete=1) error = %v", err)
	}
	if len(waitResult.Completed) != 1 {
		t.Fatalf("completed = %d, want 1", len(waitResult.Completed))
	}
	if len(waitResult.Pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(waitResult.Pending))
	}

	close(blockSlow)
	waitResult, err = scheduler.Wait(context.Background(), "root", WaitRequest{
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait() second call error = %v", err)
	}
	if len(waitResult.Completed) != 2 {
		t.Fatalf("completed final = %d, want 2", len(waitResult.Completed))
	}
}

func TestSchedulerWaitTimeoutReturnsPending(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	block := make(chan struct{})
	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		<-block
		return NodeResult{Summary: "done"}, nil
	})

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    backend,
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	if _, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "blocked"}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	waitResult, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		MinComplete: 1,
		Timeout:     40 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Wait(timeout) error = %v", err)
	}
	if len(waitResult.Completed) != 0 {
		t.Fatalf("completed = %d, want 0", len(waitResult.Completed))
	}
	if len(waitResult.Pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(waitResult.Pending))
	}

	close(block)
}

func TestSchedulerWaitAfterCloseReturnsPendingSnapshot(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	if _, err := store.CreateNode(context.Background(), Node{
		RunID:        "run-1",
		ID:           "root.1",
		ParentNodeID: "root",
		Depth:        1,
		Status:       NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateNode(root.1) error = %v", err)
	}

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	if err := scheduler.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waitResult, err := scheduler.Wait(ctx, "root", WaitRequest{})
	if err != nil {
		t.Fatalf("Wait() after Close error = %v", err)
	}
	if len(waitResult.Pending) != 0 {
		t.Fatalf("pending = %d, want 0", len(waitResult.Pending))
	}
	if len(waitResult.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(waitResult.Failed))
	}
	if waitResult.Failed[0].ID != "root.1" {
		t.Fatalf("failed node = %q, want root.1", waitResult.Failed[0].ID)
	}
	if waitResult.Failed[0].Status != NodeStatusCanceled {
		t.Fatalf("failed status = %q, want canceled", waitResult.Failed[0].Status)
	}
}

func TestSchedulerCancelsChildrenOnRootCancel(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	started := make(chan struct{}, 2)
	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		started <- struct{}{}
		<-ctx.Done()
		return NodeResult{}, ctx.Err()
	})

	rootCtx, cancelRoot := context.WithCancel(context.Background())
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:       store,
		Backend:     backend,
		RunID:       "run-1",
		RootNodeID:  "root",
		MaxWorkers:  1,
		RootContext: rootCtx,
	})

	first, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "first"})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	second, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "second"})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("backend did not start first child in time")
	}

	cancelRoot()

	waitResult, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("Wait() after root cancel error = %v", err)
	}
	if len(waitResult.Pending) != 0 {
		t.Fatalf("pending = %d, want 0 after root cancel", len(waitResult.Pending))
	}
	if len(waitResult.Failed) != 2 {
		t.Fatalf("failed = %d, want 2 after root cancel", len(waitResult.Failed))
	}
	for _, failed := range waitResult.Failed {
		if failed.Status != NodeStatusCanceled {
			t.Fatalf("failed status = %q, want canceled", failed.Status)
		}
	}

	firstNode, err := store.GetNode(context.Background(), "run-1", first.NodeID)
	if err != nil {
		t.Fatalf("GetNode(first) error = %v", err)
	}
	if firstNode.Status != NodeStatusCanceled {
		t.Fatalf("first node status = %q, want canceled", firstNode.Status)
	}
	secondNode, err := store.GetNode(context.Background(), "run-1", second.NodeID)
	if err != nil {
		t.Fatalf("GetNode(second) error = %v", err)
	}
	if secondNode.Status != NodeStatusCanceled {
		t.Fatalf("second node status = %q, want canceled", secondNode.Status)
	}
}

func TestSchedulerRejectsDepthExceeded(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	if _, err := store.CreateNode(context.Background(), Node{
		RunID:        "run-1",
		ID:           "root.1",
		ParentNodeID: "root",
		Depth:        1,
		Status:       NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateNode(root.1) error = %v", err)
	}

	budget, err := NewBudget(BudgetConfig{
		MaxDepth:      1,
		MaxTotalNodes: 10,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	backend := NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
		t.Fatalf("backend should not run for depth-exceeded submit")
		return NodeResult{}, nil
	})
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Budget:     budget,
		Backend:    backend,
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	_, err = scheduler.Submit(context.Background(), "root.1", QueryRequest{Prompt: "too deep"})
	var limitErr LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("Submit() error = %v, want LimitExceededError", err)
	}
	if limitErr.Limit != LimitDepth {
		t.Fatalf("limit = %q, want %q", limitErr.Limit, LimitDepth)
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("Submit() error should unwrap ErrBudgetExceeded, got %v", err)
	}

	children, err := store.ListChildren(context.Background(), "run-1", "root.1")
	if err != nil {
		t.Fatalf("ListChildren(root.1) error = %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("children under root.1 = %d, want 0", len(children))
	}
}

func TestSchedulerRejectsBudgetMaxChildren(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	budget, err := NewBudget(BudgetConfig{
		MaxChildren:   1,
		MaxTotalNodes: 10,
	})
	if err != nil {
		t.Fatalf("NewBudget() error = %v", err)
	}

	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Budget:     budget,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	if _, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "first"}); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	_, err = scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "second"})
	var limitErr LimitExceededError
	if !errors.As(err, &limitErr) {
		t.Fatalf("Submit(second) error = %v, want LimitExceededError", err)
	}
	if limitErr.Limit != LimitChildren {
		t.Fatalf("limit = %q, want %q", limitErr.Limit, LimitChildren)
	}
}

func TestSchedulerRecordsNodeAndWaitEvents(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	recorder := NewRecorder()
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Recorder:   recorder,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})

	handle, err := scheduler.Submit(context.Background(), "root", QueryRequest{Prompt: "first"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if _, err := scheduler.Wait(context.Background(), "root", WaitRequest{ChildNodeIDs: []string{handle.NodeID}, Timeout: time.Second}); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	var sawQueued, sawStarted, sawCompleted, sawWaitStarted, sawWaitCompleted bool
	for _, event := range recorder.Events() {
		switch event.Type {
		case EventTypeNodeQueued:
			sawQueued = event.Node != nil && event.Node.NodeID == handle.NodeID && event.Node.Status == NodeStatusQueued
		case EventTypeNodeStarted:
			sawStarted = event.Node != nil && event.Node.NodeID == handle.NodeID && event.Node.Status == NodeStatusRunning
		case EventTypeNodeCompleted:
			sawCompleted = event.Node != nil && event.Node.NodeID == handle.NodeID && event.Node.Status == NodeStatusCompleted
		case EventTypeNodeWaitStarted:
			sawWaitStarted = event.Wait != nil && len(event.Wait.ChildIDs) == 1 && event.Wait.ChildIDs[0] == handle.NodeID
		case EventTypeNodeWaitCompleted:
			sawWaitCompleted = event.Wait != nil && event.Wait.Completed == 1 && event.Wait.Pending == 0
		}
	}
	if !sawQueued || !sawStarted || !sawCompleted || !sawWaitStarted || !sawWaitCompleted {
		t.Fatalf("missing scheduler events queued=%v started=%v completed=%v wait_started=%v wait_completed=%v events=%#v", sawQueued, sawStarted, sawCompleted, sawWaitStarted, sawWaitCompleted, recorder.Events())
	}
}

func newSchedulerTestStore(t *testing.T) *MemoryNodeStore {
	t.Helper()

	store := NewMemoryNodeStore()
	if _, err := store.CreateRun(context.Background(), Run{
		ID:         "run-1",
		RootNodeID: "root",
		Status:     NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := store.CreateNode(context.Background(), Node{
		RunID:  "run-1",
		ID:     "root",
		Depth:  0,
		Status: NodeStatusQueued,
	}); err != nil {
		t.Fatalf("CreateNode(root) error = %v", err)
	}
	return store
}

func newSchedulerForTest(t *testing.T, cfg SchedulerConfig) *Scheduler {
	t.Helper()

	scheduler, err := NewScheduler(cfg)
	if err != nil {
		t.Fatalf("NewScheduler() error = %v", err)
	}
	t.Cleanup(func() {
		_ = scheduler.Close()
	})
	return scheduler
}
