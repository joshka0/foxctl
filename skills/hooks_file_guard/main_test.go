package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

type fileGuardTestEnv struct {
	ctx           context.Context
	workspaceRoot string
	cfg           config.Config
	rc            *skillmain.RunContext
}

func newFileGuardTestEnv(t *testing.T) *fileGuardTestEnv {
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

	rc, err := skillmain.BuildRunContext(cfg, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	t.Cleanup(func() { rc.Close() })

	return &fileGuardTestEnv{
		ctx:           ctx,
		workspaceRoot: filepath.Join(tmp, "workspace"),
		cfg:           cfg,
		rc:            rc,
	}
}

func (env *fileGuardTestEnv) run(t *testing.T, in hooks.Input) hooks.Output {
	t.Helper()
	buf := &bytes.Buffer{}
	env.rc.Stdout = buf

	if err := run(env.ctx, env.rc, in); err != nil {
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

	var output hooks.Output
	output.Decision = hooks.Decision(hookOutput["decision"].(string))
	if reason, ok := hookOutput["reason"].(string); ok {
		output.Reason = reason
	}
	if meta, ok := hookOutput["meta"].(map[string]any); ok {
		output.Meta = meta
	}

	return output
}

func TestFileGuard_ReservesPathForActiveTask(t *testing.T) {
	env := newFileGuardTestEnv(t)

	// Create an active task so the reservation can be associated with it.
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: env.workspaceRoot,
		Title:       "Guarded Task",
		Description: "Editing main.go",
	})
	if err != nil {
		// Cleanup on error; error is not actionable.
		_ = store.Close() //nolint:errcheck
		t.Fatal(err)
	}
	if _, err := store.SetActive(ctx, env.workspaceRoot, task.ID); err != nil {
		// Cleanup on error; error is not actionable.
		_ = store.Close() //nolint:errcheck
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Prepare a write operation for a file inside the workspace.
	filePath := filepath.Join(env.workspaceRoot, "main.go")
	input := json.RawMessage([]byte("{\"file_path\": \"" + filePath + "\"}"))

	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     input,
	}

	// Use a stable actor ID so we can assert on the reservation holder.
	t.Setenv("AGENTCTL_AGENT_NAME", "actor:test")

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Fatalf("expected approve, got %s (reason=%q)", output.Decision, output.Reason)
	}

	meta := output.Meta
	if meta == nil {
		t.Fatal("expected meta in hook output")
	}
	if _, ok := meta["reservation_id"]; !ok {
		t.Fatal("expected reservation_id in meta")
	}

	fileMeta, ok := meta["file_path"].(string)
	if !ok {
		t.Fatalf("expected file_path string in meta, got %T", meta["file_path"])
	}
	if fileMeta != "main.go" {
		t.Fatalf("expected file_path 'main.go', got %q", fileMeta)
	}

	if taskID, ok := meta["task_id"].(string); !ok || taskID != task.ID {
		t.Fatalf("expected task_id %q in meta, got %v", task.ID, meta["task_id"])
	}

	// Verify reservation persisted in the board store.
	board, err := blackboard.OpenBoardStore(env.ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer board.Close()

	reservations, err := board.ListReservations(env.ctx, env.workspaceRoot)
	if err != nil {
		t.Fatalf("list reservations: %v", err)
	}
	if len(reservations) != 1 {
		t.Fatalf("expected 1 reservation, got %d", len(reservations))
	}

	res := reservations[0]
	if res.Path != "main.go" {
		t.Fatalf("expected reservation path 'main.go', got %q", res.Path)
	}
	if res.Holder != "actor:test" {
		t.Fatalf("expected holder 'actor:test', got %q", res.Holder)
	}
	if res.TaskID != task.ID {
		t.Fatalf("expected task_id %q in reservation, got %q", task.ID, res.TaskID)
	}
}

func TestFileGuard_StrictMode_BlocksOnConflict(t *testing.T) {
	env := newFileGuardTestEnv(t)

	// Pre-create a conflicting reservation for the same file by another actor.
	board, err := blackboard.OpenBoardStore(env.ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer board.Close()

	conflict := &agent.FileReservation{
		WorkspaceID: env.workspaceRoot,
		Path:        "main.go",
		Holder:      "actor:other",
		Mode:        agent.ReservationModeExclusive,
		Reason:      "existing edit",
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	if err := board.Reserve(env.ctx, conflict); err != nil {
		t.Fatalf("reserve conflict: %v", err)
	}

	// Now run file_guard in strict mode as a different actor on the same file.
	t.Setenv("AGENTCTL_FILE_GUARD_MODE", "strict")
	t.Setenv("AGENTCTL_AGENT_NAME", "actor:self")

	filePath := filepath.Join(env.workspaceRoot, "main.go")
	input := json.RawMessage([]byte("{\"file_path\": \"" + filePath + "\"}"))

	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     input,
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionBlock {
		t.Fatalf("expected block, got %s (reason=%q)", output.Decision, output.Reason)
	}
	if !strings.Contains(output.Reason, "file conflict") {
		t.Fatalf("expected reason to mention file conflict, got %q", output.Reason)
	}

	meta := output.Meta
	if meta == nil {
		t.Fatal("expected meta in hook output")
	}
	fileMeta, ok := meta["file_path"].(string)
	if !ok || fileMeta != "main.go" {
		t.Fatalf("expected file_path 'main.go' in meta, got %v", meta["file_path"])
	}

	// Ensure conflicts metadata is present.
	if conflicts, ok := meta["conflicts"].([]any); !ok || len(conflicts) == 0 {
		t.Fatalf("expected non-empty conflicts in meta, got %T", meta["conflicts"])
	}
}
