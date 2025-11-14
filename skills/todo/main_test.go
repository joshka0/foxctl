package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	"github.com/jkatigb/agentctl/internal/skillslib"
)

func TestTodoAddAndList(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newRunner(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	storePath := filepath.Join(tmp, "tasks.json")

	addInput := input{
		Operation: "add",
		StorePath: storePath,
		Add: &addRequest{
			Title:       "Ship feature",
			Description: "Do the work",
		},
	}
	out := runSkill(ctx, t, rc, addInput)
	data := decodeData(t, out)
	if data["total_tasks"].(float64) != 1 {
		t.Fatalf("expected total tasks = 1 got %v", data["total_tasks"])
	}

	listBuf := runSkill(ctx, t, rc, input{
		Operation: "list",
		StorePath: storePath,
	})
	listData := decodeData(t, listBuf)
	tasks := listData["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	first := tasks[0].(map[string]any)
	if first["title"].(string) != "Ship feature" {
		t.Fatalf("unexpected title: %v", first["title"])
	}
}

func TestTodoChildAndComplete(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newRunner(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	storePath := filepath.Join(tmp, "tasks.json")
	parentID := extractTaskID(t, runSkill(ctx, t, rc, input{
		Operation: "add",
		StorePath: storePath,
		Add: &addRequest{
			Title: "Parent",
		},
	}))

	childID := extractTaskID(t, runSkill(ctx, t, rc, input{
		Operation: "add",
		StorePath: storePath,
		Add: &addRequest{
			Title:     "Child",
			ParentID:  parentID,
			DependsOn: []string{parentID},
		},
	}))

	// Completing child should fail because parent incomplete
	err := runExpectError(ctx, rc, input{
		Operation: "complete",
		StorePath: storePath,
		Complete: &completeRequest{
			ID: childID,
		},
	})
	if err == nil {
		t.Fatalf("expected dependency error")
	}

	runSkill(ctx, t, rc, input{
		Operation: "complete",
		StorePath: storePath,
		Complete: &completeRequest{
			ID: parentID,
		},
	})

	out := runSkill(ctx, t, rc, input{
		Operation: "complete",
		StorePath: storePath,
		Complete: &completeRequest{
			ID:      childID,
			Notes:   "done",
			Gotchas: "remember config",
		},
	})
	data := decodeData(t, out)
	task := data["task"].(map[string]any)
	if task["status"].(string) != statusDone {
		t.Fatalf("expected child complete, got %s", task["status"])
	}
	if task["gotchas"].(string) != "remember config" {
		t.Fatalf("expected gotchas captured")
	}
}

func TestTodoRejectsBackticks(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	rc := newRunner(t, tmp)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})
	storePath := filepath.Join(tmp, "tasks.json")

	if err := runExpectError(ctx, rc, input{
		Operation: "add",
		StorePath: storePath,
		Add: &addRequest{
			Title:       "bad ` title",
			Description: "desc",
		},
	}); err == nil {
		t.Fatalf("expected title validation error")
	}
}

func newRunner(t *testing.T, tmp string) *skillslib.RunnerContext {
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
	rc, err := skillslib.NewRunnerContext(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func runSkill(ctx context.Context, t *testing.T, rc *skillslib.RunnerContext, in input) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	rc.Stdout = buf
	if err := run(ctx, rc, rc.Config, in); err != nil {
		t.Fatalf("run: %v", err)
	}
	return buf
}

func runExpectError(ctx context.Context, rc *skillslib.RunnerContext, in input) error {
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

func extractTaskID(t *testing.T, buf *bytes.Buffer) string {
	data := decodeData(t, buf)
	task := data["task"].(map[string]any)
	id, _ := task["id"].(string)
	if id == "" {
		t.Fatalf("task missing id: %#v", task)
	}
	return id
}
