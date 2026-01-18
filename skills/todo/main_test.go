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
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
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

func TestTodoAdd_DedupesByNormalizedTitleAndParent(t *testing.T) {
	env := newTodoTestEnv(t)

	first := env.addTask(t, addRequest{Title: "Ship feature", Description: "Do the work"})
	firstID := taskID(t, first)

	second := env.addTask(t, addRequest{Title: "  ship   FEATURE  ", Description: "Do the work"})
	secondID := taskID(t, second)

	if firstID != secondID {
		t.Fatalf("expected dedupe to reuse same task id, got %s and %s", firstID, secondID)
	}
	if second["total_tasks"].(float64) != 1 {
		t.Fatalf("expected total tasks = 1 got %v", second["total_tasks"])
	}
}

func TestTodoAdd_DoesNotDedupeAcrossDifferentSessions(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()

	rc1 := newTestRunnerContext(t, tmp)
	rc1.SessionID = "session-a"
	t.Cleanup(func() {
		if err := rc1.Close(); err != nil {
			t.Fatalf("close rc1: %v", err)
		}
	})

	rc2 := newTestRunnerContext(t, tmp)
	rc2.SessionID = "session-b"
	t.Cleanup(func() {
		if err := rc2.Close(); err != nil {
			t.Fatalf("close rc2: %v", err)
		}
	})

	env1 := &todoTestEnv{ctx: ctx, workspaceID: "test-workspace", rc: rc1}
	env2 := &todoTestEnv{ctx: ctx, workspaceID: "test-workspace", rc: rc2}

	first := env1.addTask(t, addRequest{Title: "Ship feature", Description: "Do the work"})
	firstID := taskID(t, first)

	second := env2.addTask(t, addRequest{Title: "Ship feature", Description: "Do the work"})
	secondID := taskID(t, second)

	if firstID == secondID {
		t.Fatalf("expected different task ids across sessions")
	}
	if second["total_tasks"].(float64) != 2 {
		t.Fatalf("expected total tasks = 2 got %v", second["total_tasks"])
	}

	task, ok := second["task"].(map[string]any)
	if !ok {
		t.Fatalf("expected task map, got %T", second["task"])
	}
	if gotSID, ok := task["session_id"].(string); ok {
		if gotSID != "session-b" {
			t.Fatalf("expected session_id=session-b, got %q", gotSID)
		}
	}
}

func TestTodoAdd_DoesNotDedupeAcrossDifferentParents(t *testing.T) {
	env := newTodoTestEnv(t)

	p1 := env.addTask(t, addRequest{Title: "Parent 1"})
	p2 := env.addTask(t, addRequest{Title: "Parent 2"})
	p1ID := taskID(t, p1)
	p2ID := taskID(t, p2)

	c1 := env.addTask(t, addRequest{Title: "Child", ParentID: p1ID})
	c2 := env.addTask(t, addRequest{Title: "Child", ParentID: p2ID})
	c1ID := taskID(t, c1)
	c2ID := taskID(t, c2)

	if c1ID == c2ID {
		t.Fatalf("expected different child ids for different parents")
	}
	if c2["total_tasks"].(float64) != 4 {
		t.Fatalf("expected total tasks = 4 got %v", c2["total_tasks"])
	}
}

func TestTodoAdd_DoesNotDedupeWhenScopePathDiffers(t *testing.T) {
	env := newTodoTestEnv(t)

	first := env.addTask(t, addRequest{Title: "Same", ScopePath: "/a"})
	firstID := taskID(t, first)

	second := env.addTask(t, addRequest{Title: "Same", ScopePath: "/b"})
	secondID := taskID(t, second)

	if firstID == secondID {
		t.Fatalf("expected different ids when scope_path differs")
	}
	if second["total_tasks"].(float64) != 2 {
		t.Fatalf("expected total tasks = 2 got %v", second["total_tasks"])
	}
}

func TestTodoAdd_MergesDependsOnWhenReused(t *testing.T) {
	env := newTodoTestEnv(t)

	dep1 := env.addTask(t, addRequest{Title: "Dep 1"})
	dep2 := env.addTask(t, addRequest{Title: "Dep 2"})
	dep1ID := taskID(t, dep1)
	dep2ID := taskID(t, dep2)

	first := env.addTask(t, addRequest{Title: "Main", DependsOn: []string{dep1ID}})
	mainID := taskID(t, first)

	second := env.addTask(t, addRequest{Title: "Main", DependsOn: []string{dep2ID}})
	secondID := taskID(t, second)
	if secondID != mainID {
		t.Fatalf("expected same main id, got %s and %s", mainID, secondID)
	}

	task := taskFromData(t, second)
	deps, ok := task["depends_on"].([]any)
	if !ok {
		t.Fatalf("expected depends_on slice, got %T", task["depends_on"])
	}
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}
}

func TestTodoDedupe_DryRunAndApply(t *testing.T) {
	env := newTodoTestEnv(t)

	store, err := tasks.Open(env.ctx, env.rc.Config.Storage.Root)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	older, err := store.Add(env.ctx, tasks.Task{WorkspaceID: env.workspaceID, Title: "Dup"})
	if err != nil {
		t.Fatalf("add older: %v", err)
	}
	newer, err := store.Add(env.ctx, tasks.Task{WorkspaceID: env.workspaceID, Title: "dup"})
	if err != nil {
		t.Fatalf("add newer: %v", err)
	}
	blocker, err := store.Add(env.ctx, tasks.Task{WorkspaceID: env.workspaceID, Title: "Blocker", DependsOn: []string{older.ID}})
	if err != nil {
		t.Fatalf("add blocker: %v", err)
	}

	dry := env.run(t, input{Operation: "dedupe", WorkspaceID: env.workspaceID, Dedupe: &dedupeReq{Apply: false}})
	if dry["groups_count"].(float64) != 1 {
		t.Fatalf("expected 1 group, got %v", dry["groups_count"])
	}

	applied := env.run(t, input{Operation: "dedupe", WorkspaceID: env.workspaceID, Dedupe: &dedupeReq{Apply: true}})
	if applied["applied"].(bool) != true {
		t.Fatalf("expected applied=true")
	}

	canceled := env.run(t, input{Operation: "list", WorkspaceID: env.workspaceID, List: &listReq{Status: tasks.StatusCanceled}})
	items, ok := canceled["tasks"].([]any)
	if !ok {
		t.Fatalf("expected tasks slice, got %T", canceled["tasks"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 canceled task, got %d", len(items))
	}

	updatedBlocker, err := store.Get(env.ctx, blocker.ID)
	if err != nil {
		t.Fatalf("get blocker: %v", err)
	}
	if len(updatedBlocker.DependsOn) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(updatedBlocker.DependsOn))
	}
	if updatedBlocker.DependsOn[0] != newer.ID {
		t.Fatalf("expected dependency rewritten to %s, got %s", newer.ID, updatedBlocker.DependsOn[0])
	}
}

func TestTodoList_TitleContainsAndLimit(t *testing.T) {
	env := newTodoTestEnv(t)

	env.addTask(t, addRequest{Title: "Ship feature", Description: "Do the work"})
	env.addTask(t, addRequest{Title: "Ship docs", Description: "Docs"})
	env.addTask(t, addRequest{Title: "Fix bug", Description: "Bug"})

	data := env.run(t, input{
		Operation:   "list",
		WorkspaceID: env.workspaceID,
		List: &listReq{
			TitleContains: "SHIP",
			Limit:         1,
		},
	})

	items, ok := data["tasks"].([]any)
	if !ok {
		t.Fatalf("expected tasks slice, got %T", data["tasks"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 task, got %d", len(items))
	}
	task, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected task map, got %T", items[0])
	}
	title, ok := task["title"].(string)
	if !ok {
		t.Fatalf("task title is not a string: %#v", task)
	}
	if !strings.Contains(strings.ToLower(title), "ship") {
		t.Fatalf("unexpected title: %v", title)
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
	if task["status"].(string) != tasks.StatusCompleted {
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
	if task["status"].(string) != tasks.StatusCompleted {
		t.Fatalf("expected status %q, got %q", tasks.StatusCompleted, task["status"])
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
	defer store.Close()

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
	defer store.Close()

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

func runSkill(ctx context.Context, t *testing.T, rc *skillmain.RunContext, in input) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	rc.Stdout = buf
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf
}

func runExpectError(ctx context.Context, rc *skillmain.RunContext, in input) error {
	rc.Stdout = &bytes.Buffer{}
	return run(ctx, rc, in)
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
