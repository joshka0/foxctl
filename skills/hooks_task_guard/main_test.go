package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

func TestTaskGuard_NonWriteOperation(t *testing.T) {
	env := newTestEnv(t)

	// Non-write operation should always approve
	in := hook.Input{
		Event:         "PreToolUse",
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Read",
	}

	output := env.run(t, in)
	if output.Decision != hook.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

func TestTaskGuard_AutoMode_CreatesTask(t *testing.T) {
	env := newTestEnv(t)

	// Write operation with no active task should auto-create
	in := hook.Input{
		Event:         "PreToolUse",
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path": "/path/to/file.go"}`),
	}

	output := env.run(t, in)
	if output.Decision != hook.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Meta["created"] != true {
		t.Error("expected task to be created")
	}
	if output.Meta["task_id"] == nil {
		t.Error("expected task_id in meta")
	}
}

func TestTaskGuard_AutoMode_UsesExistingTask(t *testing.T) {
	env := newTestEnv(t)

	// Create an active task first
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: env.workspaceRoot,
		Title:       "Existing Task",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, env.workspaceRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Write operation should use existing task
	in := hook.Input{
		Event:         "PreToolUse",
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Write",
	}

	output := env.run(t, in)
	if output.Decision != hook.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Meta["created"] != false {
		t.Error("expected task to not be created")
	}
	if output.Meta["task_id"] != task.ID {
		t.Errorf("expected task_id %s, got %v", task.ID, output.Meta["task_id"])
	}
}

func TestTaskGuard_StrictMode_BlocksWithoutTask(t *testing.T) {
	env := newTestEnv(t)

	// Set strict mode
	os.Setenv("AGENTCTL_TASK_GUARD_MODE", "strict")
	defer os.Unsetenv("AGENTCTL_TASK_GUARD_MODE")

	// Write operation with no active task should block
	in := hook.Input{
		Event:         "PreToolUse",
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
	}

	output := env.run(t, in)
	if output.Decision != hook.DecisionBlock {
		t.Errorf("expected block, got %s", output.Decision)
	}
	if output.Reason == "" {
		t.Error("expected reason for block")
	}
}

func TestTaskGuard_StrictMode_ApprovesWithTask(t *testing.T) {
	env := newTestEnv(t)

	// Set strict mode
	os.Setenv("AGENTCTL_TASK_GUARD_MODE", "strict")
	defer os.Unsetenv("AGENTCTL_TASK_GUARD_MODE")

	// Create an active task first
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: env.workspaceRoot,
		Title:       "Active Task",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, env.workspaceRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()

	// Write operation should approve
	in := hook.Input{
		Event:         "PreToolUse",
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "MultiEdit",
	}

	output := env.run(t, in)
	if output.Decision != hook.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

type testEnv struct {
	ctx           context.Context
	workspaceRoot string
	cfg           config.Config
	rc            *runner.RunnerContext
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

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
	t.Cleanup(func() { rc.Close() })

	return &testEnv{
		ctx:           ctx,
		workspaceRoot: filepath.Join(tmp, "workspace"),
		cfg:           cfg,
		rc:            rc,
	}
}

func (env *testEnv) run(t *testing.T, in hook.Input) hook.Output {
	t.Helper()
	buf := &bytes.Buffer{}
	env.rc.Stdout = buf

	if err := run(env.ctx, env.rc, env.cfg, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var e envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	data, ok := e.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", e.Data)
	}

	hookOutput, ok := data["hook_output"].(map[string]any)
	if !ok {
		t.Fatalf("expected hook_output map, got %T", data["hook_output"])
	}

	var output hook.Output
	output.Decision = hook.Decision(hookOutput["decision"].(string))
	if reason, ok := hookOutput["reason"].(string); ok {
		output.Reason = reason
	}
	if meta, ok := hookOutput["meta"].(map[string]any); ok {
		output.Meta = meta
	}

	return output
}
