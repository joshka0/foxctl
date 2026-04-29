package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRLMToolsQueryReturnsHandle(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	raw, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{
		"prompt":"child prompt",
		"max_iterations":3,
		"metadata":{"source":"test"}
	}`))
	if err != nil {
		t.Fatalf("Execute(rlm_query) error = %v", err)
	}

	var out struct {
		Child   int        `json:"child"`
		Status  NodeStatus `json:"status"`
		Message string     `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if out.Child != 1 {
		t.Fatalf("child = %d, want 1", out.Child)
	}
	if out.Status != NodeStatusQueued {
		t.Fatalf("status = %q, want queued", out.Status)
	}
	if out.Message == "" {
		t.Fatal("message is empty")
	}
}

func TestRLMToolsWaitReturnsStructuredResults(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store: store,
		Backend: NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
			switch input.Prompt {
			case "pending":
				select {
				case <-release:
				case <-ctx.Done():
					return NodeResult{}, ctx.Err()
				}
				return NodeResult{Summary: "released"}, nil
			case "fail":
				return NodeResult{}, errors.New("forced backend failure")
			default:
				return NodeResult{Summary: "done:" + input.Prompt, Metadata: map[string]any{
					"required_subcalls":         1,
					"required_subcall_attempts": 2,
					"recursive_subcalls_used":   1,
				}}, nil
			}
		}),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 3,
	})
	t.Cleanup(func() {
		close(release)
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	for _, prompt := range []string{"ok", "fail", "pending"} {
		if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"`+prompt+`"}`)); err != nil {
			t.Fatalf("Execute(rlm_query %q) error = %v", prompt, err)
		}
	}

	raw, err := executor.Execute(context.Background(), RLMWaitToolName, json.RawMessage(`{"timeout_ms":50}`))
	if err != nil {
		t.Fatalf("Execute(rlm_wait) error = %v", err)
	}

	var out struct {
		Completed []map[string]any `json:"completed"`
		Failed    []map[string]any `json:"failed"`
		Pending   []map[string]any `json:"pending"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(out.Completed) != 1 {
		t.Fatalf("completed = %d, want 1", len(out.Completed))
	}
	if out.Completed[0]["required_subcalls"] != float64(1) ||
		out.Completed[0]["required_subcall_attempts"] != float64(2) ||
		out.Completed[0]["recursive_subcalls_used"] != float64(1) {
		t.Fatalf("completed recursive summary fields missing: %#v", out.Completed[0])
	}
	if len(out.Failed) != 1 {
		t.Fatalf("failed = %d, want 1", len(out.Failed))
	}
	if len(out.Pending) != 1 {
		t.Fatalf("pending = %d, want 1", len(out.Pending))
	}
	for _, bucket := range [][]map[string]any{out.Completed, out.Failed, out.Pending} {
		for _, item := range bucket {
			if _, hasPrompt := item["prompt"]; hasPrompt {
				t.Fatalf("wait summary should not include prompt: %#v", item)
			}
			if _, hasNodeID := item["node_id"]; hasNodeID {
				t.Fatalf("wait summary should not expose node_id: %#v", item)
			}
			if _, hasChild := item["child"]; !hasChild {
				t.Fatalf("wait summary missing child number: %#v", item)
			}
		}
	}
}

func TestRLMToolsWaitUsesSubmittedChildrenWithoutIDs(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store: store,
		Backend: NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
			return NodeResult{Summary: "done:" + input.Prompt}, nil
		}),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 2,
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"alpha"}`)); err != nil {
		t.Fatalf("Execute(rlm_query alpha) error = %v", err)
	}
	if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"beta"}`)); err != nil {
		t.Fatalf("Execute(rlm_query beta) error = %v", err)
	}

	raw, err := executor.Execute(context.Background(), RLMWaitToolName, json.RawMessage(`{"timeout_ms":1000}`))
	if err != nil {
		t.Fatalf("Execute(rlm_wait) error = %v", err)
	}
	var out struct {
		Completed []struct {
			Child   int        `json:"child"`
			Status  NodeStatus `json:"status"`
			Summary string     `json:"summary"`
		} `json:"completed"`
		Pending []any  `json:"pending"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(out.Completed) != 2 {
		t.Fatalf("completed = %d, want 2: %s", len(out.Completed), raw)
	}
	if out.Completed[0].Child != 1 || out.Completed[1].Child != 2 {
		t.Fatalf("child numbers = %+v, want 1 and 2", out.Completed)
	}
	if len(out.Pending) != 0 {
		t.Fatalf("pending = %d, want 0", len(out.Pending))
	}
	if out.Message == "" {
		t.Fatal("message is empty")
	}
}

func TestRLMToolsWaitCompactsChildSummaries(t *testing.T) {
	t.Parallel()

	longSummary := "alpha\n\nbeta gamma delta epsilon zeta"
	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store: store,
		Backend: NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
			if input.SummaryMaxChars != 18 {
				t.Fatalf("SummaryMaxChars = %d, want 18", input.SummaryMaxChars)
			}
			return NodeResult{Summary: longSummary, Answer: longSummary}, nil
		}),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor, err := NewRLMToolsExecutor(RLMToolsConfig{
		Scheduler:       scheduler,
		Store:           store,
		RunID:           "run-1",
		ParentNodeID:    "root",
		SummaryMaxChars: 18,
	})
	if err != nil {
		t.Fatalf("NewRLMToolsExecutor() error = %v", err)
	}

	if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child"}`)); err != nil {
		t.Fatalf("Execute(rlm_query) error = %v", err)
	}
	raw, err := executor.Execute(context.Background(), RLMWaitToolName, json.RawMessage(`{"timeout_ms":1000}`))
	if err != nil {
		t.Fatalf("Execute(rlm_wait) error = %v", err)
	}
	var out struct {
		Completed []struct {
			Summary          string `json:"summary"`
			SummaryChars     int    `json:"summary_chars"`
			SummaryTruncated bool   `json:"summary_truncated"`
		} `json:"completed"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(out.Completed) != 1 {
		t.Fatalf("completed = %d, want 1", len(out.Completed))
	}
	if out.Completed[0].Summary != "alpha beta gamm..." {
		t.Fatalf("summary = %q", out.Completed[0].Summary)
	}
	if out.Completed[0].SummaryChars != 18 || !out.Completed[0].SummaryTruncated {
		t.Fatalf("summary metadata = %+v", out.Completed[0])
	}
}

func TestRLMToolsQueryRejectsRuntimeOwnedFields(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	_, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child","required_subcalls":1}`))
	if err == nil {
		t.Fatal("expected required_subcalls ownership error")
	}
	_, err = executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child","parent_node_id":"root.1"}`))
	if err == nil {
		t.Fatal("expected parent_node_id ownership error")
	}
}

func TestRLMToolsQueryAppliesRuntimeRequiredSubcallRule(t *testing.T) {
	t.Parallel()

	seen := make(chan NodeInput, 1)
	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store: store,
		Backend: NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
			seen <- input
			return NodeResult{Summary: "ok"}, nil
		}),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor, err := NewRLMToolsExecutor(RLMToolsConfig{
		Scheduler:    scheduler,
		Store:        store,
		RunID:        "run-1",
		ParentNodeID: "root",
		RequiredSubcallRules: []RequiredSubcallRule{
			{Child: 1, RequiredSubcalls: 1},
		},
	})
	if err != nil {
		t.Fatalf("NewRLMToolsExecutor() error = %v", err)
	}

	if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"must recurse"}`)); err != nil {
		t.Fatalf("Execute(rlm_query) error = %v", err)
	}
	select {
	case input := <-seen:
		if input.RequiredSubcalls != 1 {
			t.Fatalf("RequiredSubcalls = %d, want 1", input.RequiredSubcalls)
		}
	case <-time.After(time.Second):
		t.Fatal("backend did not receive child input")
	}
}

func TestRLMToolsResultFetchesCompletedChild(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store: store,
		Backend: NodeBackendFunc(func(ctx context.Context, node Node, input NodeInput) (NodeResult, error) {
			return NodeResult{Summary: "child summary", Answer: "child answer"}, nil
		}),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	_, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"child task"}`))
	if err != nil {
		t.Fatalf("Execute(rlm_query) error = %v", err)
	}

	if _, err := scheduler.Wait(context.Background(), "root", WaitRequest{
		MinComplete: 1,
		Timeout:     time.Second,
	}); err != nil {
		t.Fatalf("scheduler.Wait() error = %v", err)
	}

	raw, err := executor.Execute(context.Background(), RLMResultToolName, json.RawMessage(`{"child":1}`))
	if err != nil {
		t.Fatalf("Execute(rlm_result) error = %v", err)
	}

	var out struct {
		Child  int        `json:"child"`
		Status NodeStatus `json:"status"`
		Result *struct {
			Status  NodeStatus `json:"status"`
			Summary string     `json:"summary"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode result output: %v", err)
	}
	if out.Child != 1 {
		t.Fatalf("child = %d, want 1", out.Child)
	}
	if out.Status != NodeStatusCompleted {
		t.Fatalf("status = %q, want completed", out.Status)
	}
	if out.Result == nil {
		t.Fatalf("result is nil")
	}
	if out.Result.Status != NodeStatusCompleted {
		t.Fatalf("result.status = %q, want completed", out.Result.Status)
	}
	if out.Result.Summary != "child summary" {
		t.Fatalf("result.summary = %q, want child summary", out.Result.Summary)
	}
}

func TestRLMToolsWaitAndResultRejectRuntimeOwnedFields(t *testing.T) {
	t.Parallel()

	store := newSchedulerTestStore(t)
	scheduler := newSchedulerForTest(t, SchedulerConfig{
		Store:      store,
		Backend:    NodeBackendFunc(func(context.Context, Node, NodeInput) (NodeResult, error) { return NodeResult{Summary: "ok"}, nil }),
		RunID:      "run-1",
		RootNodeID: "root",
		MaxWorkers: 1,
	})
	executor := newRLMToolsExecutorForTest(t, scheduler, store)

	if _, err := executor.Execute(context.Background(), RLMQueryToolName, json.RawMessage(`{"prompt":"parent child"}`)); err != nil {
		t.Fatalf("Execute(rlm_query root child) error = %v", err)
	}
	children, err := store.ListChildren(context.Background(), "run-1", "root")
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("root children = %d, want 1", len(children))
	}

	_, err = executor.Execute(context.Background(), RLMWaitToolName, json.RawMessage(`{
		"parent_node_id":"root",
		"timeout_ms":10
	}`))
	if err == nil {
		t.Fatal("expected rlm_wait parent_node_id ownership error")
	}
	_, err = executor.Execute(context.Background(), RLMWaitToolName, json.RawMessage(`{"child_ids":["root.1"],"timeout_ms":10}`))
	if err == nil {
		t.Fatal("expected rlm_wait child_ids ownership error")
	}
	_, err = executor.Execute(context.Background(), RLMResultToolName, json.RawMessage(`{"node_id":"root.1"}`))
	if err == nil {
		t.Fatal("expected rlm_result node_id ownership error")
	}
	_, err = executor.Execute(context.Background(), RLMResultToolName, json.RawMessage(`{"run_id":"run-1"}`))
	if err == nil {
		t.Fatal("expected rlm_result run_id ownership error")
	}
}

func newRLMToolsExecutorForTest(t *testing.T, scheduler *Scheduler, store NodeStore) *RLMToolsExecutor {
	t.Helper()

	executor, err := NewRLMToolsExecutor(RLMToolsConfig{
		Scheduler:    scheduler,
		Store:        store,
		RunID:        "run-1",
		ParentNodeID: "root",
	})
	if err != nil {
		t.Fatalf("NewRLMToolsExecutor() error = %v", err)
	}
	return executor
}
