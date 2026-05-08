package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestTaskGuard_NonWriteOperation(t *testing.T) {
	env := newTestEnv(t)

	// Non-write operation should always approve
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Read",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

func TestTaskGuard_AutoMode_CreatesTask(t *testing.T) {
	env := newTestEnv(t)

	// Write operation with no active task should auto-create
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path": "/path/to/file.go"}`),
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
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
	workspaceID := workspace.ID(env.workspaceRoot)

	// Create an active task first
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: workspaceID,
		Title:       "Existing Task",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, workspaceID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Write operation should use existing task
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Write",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
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
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "strict")

	// Write operation with no active task should block
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionBlock {
		t.Errorf("expected block, got %s", output.Decision)
	}
	if output.Reason == "" {
		t.Error("expected reason for block")
	}
}

func TestTaskGuard_StrictMode_ApprovesWithTask(t *testing.T) {
	env := newTestEnv(t)
	workspaceID := workspace.ID(env.workspaceRoot)

	// Set strict mode
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "strict")

	// Create an active task first
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: workspaceID,
		Title:       "Active Task",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, workspaceID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Write operation should approve
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "MultiEdit",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
}

func TestTaskGuard_AutoMode_DirtiesReadyForReviewTask(t *testing.T) {
	env := newTestEnv(t)
	workspaceID := workspace.ID(env.workspaceRoot)

	// Create a ready_for_review task with passing review
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID:      workspaceID,
		Title:            "Ready for Review Task",
		Status:           tasks.StatusReadyForReview,
		LastReviewStatus: tasks.ReviewStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, workspaceID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Write operation should approve and dirty the task
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path": "/path/to/file.go"}`),
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Meta["dirtied"] != true {
		t.Error("expected task to be dirtied")
	}
	if output.Meta["task_status"] != tasks.StatusInProgress {
		t.Errorf("expected task_status %q, got %v", tasks.StatusInProgress, output.Meta["task_status"])
	}

	// Verify the task was actually dirtied in storage
	store, err = tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	updated, err := store.Get(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != tasks.StatusInProgress {
		t.Errorf("expected status %q, got %q", tasks.StatusInProgress, updated.Status)
	}
	if updated.LastReviewStatus != tasks.ReviewStatusStale {
		t.Errorf("expected last_review_status %q, got %q", tasks.ReviewStatusStale, updated.LastReviewStatus)
	}
}

func TestTaskGuard_StrictMode_DirtiesCompletedTask(t *testing.T) {
	env := newTestEnv(t)
	workspaceID := workspace.ID(env.workspaceRoot)

	// Set strict mode
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "strict")

	// Create a completed task with passing review
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID:      workspaceID,
		Title:            "Completed Task",
		Status:           tasks.StatusCompleted,
		LastReviewStatus: tasks.ReviewStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, workspaceID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Write operation should approve and dirty the task
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Write",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Meta["dirtied"] != true {
		t.Error("expected task to be dirtied")
	}
	if output.Meta["task_status"] != tasks.StatusInProgress {
		t.Errorf("expected task_status %q, got %v", tasks.StatusInProgress, output.Meta["task_status"])
	}
}

func TestTaskGuard_AutoMode_DoesNotDirtyInProgressTask(t *testing.T) {
	env := newTestEnv(t)

	// Create an in_progress task
	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID: env.workspaceRoot,
		Title:       "In Progress Task",
		Status:      tasks.StatusInProgress,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SetActive(ctx, env.workspaceRoot, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Test setup complete; error is not actionable.
	_ = store.Close() //nolint:errcheck

	// Write operation should approve but NOT dirty the task
	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
	}

	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Errorf("expected approve, got %s", output.Decision)
	}
	if output.Meta["dirtied"] != false {
		t.Error("expected task NOT to be dirtied")
	}
}

func TestTaskGuard_ProposalMode_NoActiveTask_RecordsProposalNoTask(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	if err := os.MkdirAll(env.workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		SessionID:     "session-proposal-1",
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path":"` + filepath.ToSlash(filepath.Join(env.workspaceRoot, "pkg/main.go")) + `"}`),
	}
	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Fatalf("expected approve, got %s", output.Decision)
	}
	if output.Meta["proposal_recorded"] != true {
		t.Fatalf("expected proposal_recorded=true, got %v", output.Meta["proposal_recorded"])
	}

	taskStore, err := tasks.Open(context.Background(), env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	defer taskStore.Close()
	_, found, err := taskStore.GetActive(context.Background(), workspace.ID(env.workspaceRoot))
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no active task in proposal mode when none existed")
	}

	store := contextplane.NewWorkspaceStore(env.workspaceRoot)
	states, err := store.ListControlProposalStates(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 proposal state, got %d", len(states))
	}
	if states[0].Proposal.Kind != contextplane.ProposalKindTaskProposal {
		t.Fatalf("proposal kind = %q", states[0].Proposal.Kind)
	}
	if states[0].Proposal.Count != 1 {
		t.Fatalf("proposal count = %d", states[0].Proposal.Count)
	}
	if got := states[0].Proposal.Payload["scope_path"]; got != "pkg/main.go" {
		t.Fatalf("payload.scope_path = %v", got)
	}
}

func TestTaskGuard_ProposalMode_DedupeEquivalentEvents(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	if err := os.MkdirAll(env.workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	first := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		SessionID:     "session-proposal-dedupe",
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path":"` + filepath.ToSlash(filepath.Join(env.workspaceRoot, "pkg/./main.go")) + `"}`),
	}
	second := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		SessionID:     "session-proposal-dedupe",
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path":"` + filepath.ToSlash(filepath.Join(env.workspaceRoot, "pkg/main.go")) + `"}`),
	}

	_ = env.run(t, first)
	_ = env.run(t, second)

	store := contextplane.NewWorkspaceStore(env.workspaceRoot)
	states, err := store.ListControlProposalStates(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 proposal state, got %d", len(states))
	}
	if states[0].Proposal.Count != 2 {
		t.Fatalf("proposal count = %d, want 2", states[0].Proposal.Count)
	}
}

func TestTaskGuard_ProposalMode_DedupesProviderAliases(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	if err := os.MkdirAll(env.workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	path := filepath.ToSlash(filepath.Join(env.workspaceRoot, "pkg/main.go"))

	claudeStyle := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		SessionID:     "session-provider-dedupe",
		ToolName:      "Edit",
		ToolKind:      hooks.ToolKindWrite,
		ToolInput:     json.RawMessage(`{"file_path":"` + path + `"}`),
	}
	canonicalStyle := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		SessionID:     "session-provider-dedupe",
		ToolCanonical: "edit.apply_patch",
		ToolKind:      hooks.ToolKindWrite,
		ToolInput:     json.RawMessage(`{"file_path":"` + path + `"}`),
	}

	_ = env.run(t, claudeStyle)
	_ = env.run(t, canonicalStyle)

	store := contextplane.NewWorkspaceStore(env.workspaceRoot)
	states, err := store.ListControlProposalStates(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("expected 1 proposal state, got %d", len(states))
	}
	if states[0].Proposal.Count != 2 {
		t.Fatalf("proposal count = %d, want 2", states[0].Proposal.Count)
	}
}

func TestTaskGuard_ProposalMode_WithActiveTask_DirtiesAndNoProposal(t *testing.T) {
	env := newTestEnv(t)
	workspaceID := workspace.ID(env.workspaceRoot)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	if err := os.MkdirAll(env.workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	ctx := context.Background()
	store, err := tasks.Open(ctx, env.cfg.Storage.Root)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.Add(ctx, tasks.Task{
		WorkspaceID:      workspaceID,
		Title:            "Ready For Review",
		Status:           tasks.StatusReadyForReview,
		LastReviewStatus: tasks.ReviewStatusOK,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetActive(ctx, workspaceID, task.ID); err != nil {
		t.Fatal(err)
	}
	_ = store.Close() //nolint:errcheck

	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Write",
		ToolInput:     json.RawMessage(`{"file_path":"` + filepath.ToSlash(filepath.Join(env.workspaceRoot, "pkg/main.go")) + `"}`),
	}
	output := env.run(t, in)
	if output.Decision != hooks.DecisionApprove {
		t.Fatalf("expected approve, got %s", output.Decision)
	}
	if output.Meta["proposal_recorded"] != false {
		t.Fatalf("expected proposal_recorded=false, got %v", output.Meta["proposal_recorded"])
	}
	if output.Meta["dirtied"] != true {
		t.Fatalf("expected dirtied=true, got %v", output.Meta["dirtied"])
	}

	controlStore := contextplane.NewWorkspaceStore(env.workspaceRoot)
	states, err := controlStore.ListControlProposalStates(ctx, 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("expected no control proposals, got %d", len(states))
	}
}

func TestTaskGuard_ProposalMode_BlocksUnsafeScope(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	if err := os.MkdirAll(env.workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	in := hooks.Input{
		Event:         hooks.EventPreToolUse,
		WorkspaceRoot: env.workspaceRoot,
		ToolName:      "Edit",
		ToolInput:     json.RawMessage(`{"file_path":"/tmp/outside.txt"}`),
	}
	output := env.run(t, in)
	if output.Decision != hooks.DecisionBlock {
		t.Fatalf("expected block, got %s", output.Decision)
	}

	controlStore := contextplane.NewWorkspaceStore(env.workspaceRoot)
	states, err := controlStore.ListControlProposalStates(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListControlProposalStates: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("expected no control proposals, got %d", len(states))
	}
}

func TestTaskGuard_ProposalMode_RequiresExplicitWorkspaceContext(t *testing.T) {
	env := newTestEnv(t)
	t.Setenv("FOXCTL_TASK_GUARD_MODE", "proposal")
	t.Setenv("FOXCTL_WORKSPACE", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")

	in := hooks.Input{
		Event:     hooks.EventPreToolUse,
		ToolName:  "Edit",
		ToolInput: json.RawMessage(`{"file_path":"pkg/main.go"}`),
	}
	err := env.runError(in)
	if err == nil {
		t.Fatal("expected missing workspace context error")
	}
	if got := err.Error(); got != "proposal mode requires workspace_root, cwd, FOXCTL_WORKSPACE, or CLAUDE_PROJECT_DIR" {
		t.Fatalf("unexpected error: %s", got)
	}
}

type testEnv struct {
	ctx           context.Context
	workspaceRoot string
	cfg           config.Config
	rc            *skillmain.RunContext
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

	rc, err := skillmain.BuildRunContext(cfg, &bytes.Buffer{})
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

func (env *testEnv) run(t *testing.T, in hooks.Input) hooks.Output {
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

func (env *testEnv) runError(in hooks.Input) error {
	buf := &bytes.Buffer{}
	env.rc.Stdout = buf
	return run(env.ctx, env.rc, in)
}
