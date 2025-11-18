package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
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

type todoTestEnv struct {
	ctx       context.Context
	storePath string
	rc        *runner.Context
}

func newTodoTestEnv(t *testing.T) *todoTestEnv {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newTestContext(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	return &todoTestEnv{
		ctx:       ctx,
		storePath: filepath.Join(tmp, "tasks.json"),
		rc:        rc,
	}
}

func (env *todoTestEnv) addTask(t *testing.T, req addRequest) map[string]any {
	t.Helper()
	data := env.run(t, input{Operation: "add", StorePath: env.storePath, Add: &req})
	return data
}

func (env *todoTestEnv) completeTask(t *testing.T, req completeRequest) map[string]any {
	t.Helper()
	return env.run(t, input{Operation: "complete", StorePath: env.storePath, Complete: &req})
}

func (env *todoTestEnv) listTasks(t *testing.T) []map[string]any {
	t.Helper()
	data := env.run(t, input{Operation: "list", StorePath: env.storePath})
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
	in.StorePath = env.storePath
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
	id, _ := task["id"].(string)
	if id == "" {
		t.Fatalf("task missing id: %#v", task)
	}
	return id
}

func newTestContext(t *testing.T, tmp string) *runner.Context {
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
	}
	rc, err := runner.NewContext(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func runSkill(ctx context.Context, t *testing.T, rc *runner.Context, in input) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	rc.Stdout = buf
	if err := run(ctx, rc, rc.Config, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf
}

func runExpectError(ctx context.Context, rc *runner.Context, in input) error {
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
