package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

func TestTodoAddAndList(t *testing.T) {
	env := newTodoTestEnv(t)

	data := env.addTask(t, addRequest{
		Title:       "Ship feature",
		Description: "Do the work",
	})
	if data["total_tasks"].(float64) != 1 {
		t.Fatalf("expected total tasks = 1 got %v", data["total_tasks"])
	}

	tasks := env.listTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0]["title"].(string) != "Ship feature" {
		t.Fatalf("unexpected title: %v", tasks[0]["title"])
	}
}

func TestTodoChildAndComplete(t *testing.T) {
	env := newTodoTestEnv(t)

	parentTask := env.addTask(t, addRequest{Title: "Parent"})
	parentID := taskID(t, parentTask)

	childTask := env.addTask(t, addRequest{
		Title:     "Child",
		ParentID:  parentID,
		DependsOn: []string{parentID},
	})
	childID := taskID(t, childTask)

	if err := env.expectError(t, input{
		Operation: "complete",
		Complete:  &completeRequest{ID: childID},
	}); err == nil {
		t.Fatalf("expected dependency error")
	}

	env.completeTask(t, completeRequest{ID: parentID})

	result := env.completeTask(t, completeRequest{
		ID:      childID,
		Notes:   "done",
		Gotchas: "remember config",
	})
	task := taskFromData(t, result)
	if task["status"].(string) != statusDone {
		t.Fatalf("expected child complete, got %s", task["status"])
	}
	if task["gotchas"].(string) != "remember config" {
		t.Fatalf("expected gotchas captured")
	}
}

func TestTodoRejectsBackticks(t *testing.T) {
	env := newTodoTestEnv(t)

	if err := env.expectError(t, input{
		Operation: "add",
		Add: &addRequest{
			Title:       "bad ` title",
			Description: "desc",
		},
	}); err == nil {
		t.Fatalf("expected title validation error")
	}
}

func TestTodoReviewRequest_InProgressTask(t *testing.T) {
	env := newTodoTestEnv(t)

	// Add a task (starts as pending)
	addData := env.addTask(t, addRequest{Title: "Review me"})
	id := taskID(t, addData)

	// Set task to in_progress via store
	env.setTaskStatus(t, id, tasks.StatusInProgress)

	// Request review should succeed
	data := env.run(t, input{
		Operation:     "review_request",
		WorkspaceID:   env.workspaceID,
		ReviewRequest: &reviewRequestReq{TaskID: id},
	})

	task := taskFromData(t, data)
	if task["status"].(string) != tasks.StatusReadyForReview {
		t.Errorf("expected status %q, got %q", tasks.StatusReadyForReview, task["status"])
	}
	if task["last_review_status"] == nil || task["last_review_status"].(string) == "" {
		t.Error("expected last_review_status to be set")
	}
	if task["last_review_id"] == nil || task["last_review_id"].(string) == "" {
		t.Error("expected last_review_id to be set")
	}
}

func TestTodoReviewRequest_PendingTask_Rejected(t *testing.T) {
	env := newTodoTestEnv(t)

	// Add a task (pending by default)
	addData := env.addTask(t, addRequest{Title: "Pending task"})
	id := taskID(t, addData)

	// Request review on pending task should fail
	err := env.expectError(t, input{
		Operation:     "review_request",
		ReviewRequest: &reviewRequestReq{TaskID: id},
	})
	if err == nil {
		t.Fatal("expected error for review_request on pending task")
	}
	if !strings.Contains(err.Error(), "pending") {
		t.Errorf("expected error to mention 'pending', got: %v", err)
	}
}

func TestTodoReviewRequest_CompletedTask_Rejected(t *testing.T) {
	env := newTodoTestEnv(t)

	// Add and complete a task
	addData := env.addTask(t, addRequest{Title: "Completed task"})
	id := taskID(t, addData)
	env.completeTask(t, completeRequest{ID: id})

	// Request review on completed task should fail
	err := env.expectError(t, input{
		Operation:     "review_request",
		ReviewRequest: &reviewRequestReq{TaskID: id},
	})
	if err == nil {
		t.Fatal("expected error for review_request on completed task")
	}
	if !strings.Contains(err.Error(), "completed") {
		t.Errorf("expected error to mention 'completed', got: %v", err)
	}
}

func TestTodoReviewStatus_ReturnsFields(t *testing.T) {
	env := newTodoTestEnv(t)

	// Add a task and set it to in_progress
	addData := env.addTask(t, addRequest{Title: "Status check"})
	id := taskID(t, addData)
	env.setTaskStatus(t, id, tasks.StatusInProgress)

	// Request review to populate review fields
	env.run(t, input{
		Operation:     "review_request",
		WorkspaceID:   env.workspaceID,
		ReviewRequest: &reviewRequestReq{TaskID: id},
	})

	// Get review status
	data := env.run(t, input{
		Operation:    "review_status",
		WorkspaceID:  env.workspaceID,
		ReviewStatus: &reviewStatusReq{TaskID: id},
	})

	if data["task_id"].(string) != id {
		t.Errorf("expected task_id %q, got %q", id, data["task_id"])
	}
	if data["last_review_status"].(string) != tasks.ReviewStatusPending {
		t.Errorf("expected last_review_status %q, got %q", tasks.ReviewStatusPending, data["last_review_status"])
	}
	if data["last_review_id"].(string) == "" {
		t.Error("expected last_review_id to be set")
	}
	if data["last_review_at"].(string) == "" {
		t.Error("expected last_review_at to be set")
	}
}

func TestTodoComplete_WithReviewGate_AllowsReviewedTask(t *testing.T) {
	env := newTodoTestEnv(t)
	t.Setenv("AGENTCTL_TODO_REVIEW_GATE", "on")

	addData := env.addTask(t, addRequest{Title: "Gate ok"})
	id := taskID(t, addData)

	env.setTaskStatus(t, id, tasks.StatusReadyForReview)
	env.setTaskReview(t, id, tasks.ReviewStatusOK, "review-1")

	data := env.completeTask(t, completeRequest{ID: id})
	task := taskFromData(t, data)
	if task["status"].(string) != statusDone {
		t.Fatalf("expected status %q, got %q", statusDone, task["status"])
	}
}

func TestTodoComplete_WithReviewGate_RejectsWithoutOkReview(t *testing.T) {
	env := newTodoTestEnv(t)
	t.Setenv("AGENTCTL_TODO_REVIEW_GATE", "on")

	addData := env.addTask(t, addRequest{Title: "No review"})
	id := taskID(t, addData)

	env.setTaskStatus(t, id, tasks.StatusReadyForReview)

	err := env.expectError(t, input{
		Operation:   "complete",
		Complete:    &completeRequest{ID: id},
		WorkspaceID: env.workspaceID,
	})
	if err == nil {
		t.Fatal("expected error for complete without ok review")
	}
	if !strings.Contains(err.Error(), "requires an 'ok' review") {
		t.Fatalf("expected error to mention ok review, got %v", err)
	}
}

func TestTodoComplete_WithReviewGate_RejectsWhenNotReadyForReview(t *testing.T) {
	env := newTodoTestEnv(t)
	t.Setenv("AGENTCTL_TODO_REVIEW_GATE", "on")

	addData := env.addTask(t, addRequest{Title: "Wrong status"})
	id := taskID(t, addData)

	env.setTaskStatus(t, id, tasks.StatusInProgress)
	env.setTaskReview(t, id, tasks.ReviewStatusOK, "review-2")

	err := env.expectError(t, input{
		Operation:   "complete",
		Complete:    &completeRequest{ID: id},
		WorkspaceID: env.workspaceID,
	})
	if err == nil {
		t.Fatal("expected error for complete when not ready_for_review")
	}
	if !strings.Contains(err.Error(), "must be ready_for_review") {
		t.Fatalf("expected error to mention ready_for_review, got %v", err)
	}
}

type todoTestEnv struct {
	ctx         context.Context
	workspaceID string
	rc          *runner.RunnerContext
}

func newTodoTestEnv(t *testing.T) *todoTestEnv {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newTestRunnerContext(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	return &todoTestEnv{
		ctx:         ctx,
		workspaceID: "test-workspace",
		rc:          rc,
	}
}

func (env *todoTestEnv) addTask(t *testing.T, req addRequest) map[string]any {
	t.Helper()
	data := env.run(t, input{Operation: "add", WorkspaceID: env.workspaceID, Add: &req})
	return data
}

func (env *todoTestEnv) completeTask(t *testing.T, req completeRequest) map[string]any {
	t.Helper()
	return env.run(t, input{Operation: "complete", WorkspaceID: env.workspaceID, Complete: &req})
}

// setTaskStatus directly updates a task's status via the store.
// This is a test helper to set up specific task states for testing.
func (env *todoTestEnv) setTaskStatus(t *testing.T, taskID, status string) {
	t.Helper()
	store, err := tasks.Open(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		// Test cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}()

	task, err := store.Get(env.ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	task.Status = status
	if _, err := store.Update(env.ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
}

// setTaskReview updates the review fields for a task via the store.
func (env *todoTestEnv) setTaskReview(t *testing.T, taskID, reviewStatus, reviewID string) {
	t.Helper()
	store, err := tasks.Open(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() {
		// Test cleanup; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}()

	task, err := store.Get(env.ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	now := time.Now().UTC()
	task.LastReviewStatus = reviewStatus
	task.LastReviewID = reviewID
	task.LastReviewAt = &now
	if _, err := store.Update(env.ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
}

func (env *todoTestEnv) listTasks(t *testing.T) []map[string]any {
	t.Helper()
	data := env.run(t, input{Operation: "list", WorkspaceID: env.workspaceID})
	items, ok := data["tasks"].([]any)
	if !ok {
		t.Fatalf("expected tasks slice, got %T", data["tasks"])
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		task, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected task map, got %T", item)
		}
		result = append(result, task)
	}
	return result
}

func (env *todoTestEnv) expectError(t *testing.T, in input) error {
	t.Helper()
	in.WorkspaceID = env.workspaceID
	return runExpectError(env.ctx, env.rc, in)
}

func (env *todoTestEnv) run(t *testing.T, in input) map[string]any {
	t.Helper()
	buf := runSkill(env.ctx, t, env.rc, in)
	return decodeData(t, buf)
}

func taskFromData(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	task, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("expected task map, got %T", data["task"])
	}
	return task
}

func taskID(t *testing.T, data map[string]any) string {
	t.Helper()
	task := taskFromData(t, data)
	id, ok := task["id"].(string)
	if !ok {
		t.Fatalf("task id is not a string: %#v", task)
	}
	if id == "" {
		t.Fatalf("task missing id: %#v", task)
	}
	return id
}

func newTestRunnerContext(t *testing.T, tmp string) *runner.RunnerContext {
	t.Helper()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: config.DefaultInlineOutputKB,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:   filepath.Join(tmp, "cas"),
			Jobs:  filepath.Join(tmp, "jobs"),
			Cache: filepath.Join(tmp, "cache"),
		},
		Storage: config.StorageSettings{
			Root: filepath.Join(tmp, "storage"),
		},
	}
	rc, err := runner.NewRunnerContext(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func runSkill(ctx context.Context, t *testing.T, rc *runner.RunnerContext, in input) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	rc.Stdout = buf
	if err := run(ctx, rc, rc.Config, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf
}

func runExpectError(ctx context.Context, rc *runner.RunnerContext, in input) error {
	rc.Stdout = &bytes.Buffer{}
	return run(ctx, rc, rc.Config, in)
}

func decodeData(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	return data
}
