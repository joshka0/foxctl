package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestCoordinatorProcessRequiresWorkspace(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	cmd := newCoordinatorProcessCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--limit", "20"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when --workspace is omitted")
	}
	if !strings.Contains(err.Error(), "--workspace is required") {
		t.Fatalf("error=%q", err)
	}
}

func TestCoordinatorProcessSeedsLowRiskTaskProposal(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspacePath := mustMakeWorkspace(t)
	workspaceID := workspace.ID(workspacePath)

	proposal := seedLowRiskTaskProposal(t, workspacePath, "alpha", "")
	env := mustRunCoordinatorProcess(t, cfg, "--workspace", workspacePath, "--limit", "20")
	data := envelopeDataMap(t, env)
	if got := intFromAny(t, data["processed_count"]); got != 1 {
		t.Fatalf("processed_count=%d want 1", got)
	}
	if got := intFromAny(t, data["decisions"]); got != 1 {
		t.Fatalf("decisions=%d want 1", got)
	}
	if got := intFromAny(t, data["applies"]); got != 1 {
		t.Fatalf("applies=%d want 1", got)
	}
	if got := intFromAny(t, data["skipped_count"]); got != 0 {
		t.Fatalf("skipped_count=%d want 0", got)
	}
	if got := intFromAny(t, data["escalated_count"]); got != 0 {
		t.Fatalf("escalated_count=%d want 0", got)
	}

	store, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open tasks store: %v", err)
	}
	defer requireClose(t, store, "tasks store")
	tasks, err := store.ListByWorkspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("list tasks by workspace: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d want 1", len(tasks))
	}
	controlStore := contextplane.NewWorkspaceStore(workspacePath)
	state, err := controlStore.GetControlProposalState(context.Background(), proposal.ID)
	if err != nil {
		t.Fatalf("get control proposal state: %v", err)
	}
	if state == nil || state.LatestDecision == nil || state.LatestApplyResult == nil {
		t.Fatalf("state missing decision/apply: %#v", state)
	}
	if tasks[0].ID != state.LatestApplyResult.TargetID {
		t.Fatalf("task id=%q want apply target %q", tasks[0].ID, state.LatestApplyResult.TargetID)
	}
}

func TestCoordinatorProcessIsIdempotent(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspacePath := mustMakeWorkspace(t)
	workspaceID := workspace.ID(workspacePath)
	seedLowRiskTaskProposal(t, workspacePath, "beta", "")

	_ = mustRunCoordinatorProcess(t, cfg, "--workspace", workspacePath, "--limit", "20")
	second := mustRunCoordinatorProcess(t, cfg, "--workspace", workspacePath, "--limit", "20")
	data := envelopeDataMap(t, second)
	if got := intFromAny(t, data["decisions"]); got != 0 {
		t.Fatalf("second decisions=%d want 0", got)
	}
	if got := intFromAny(t, data["applies"]); got != 0 {
		t.Fatalf("second applies=%d want 0", got)
	}
	if got := intFromAny(t, data["skipped_count"]); got != 1 {
		t.Fatalf("second skipped_count=%d want 1", got)
	}

	store, err := taskstore.Open(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open tasks store: %v", err)
	}
	defer requireClose(t, store, "tasks store")
	tasks, err := store.ListByWorkspace(context.Background(), workspaceID)
	if err != nil {
		t.Fatalf("list tasks by workspace: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d want 1", len(tasks))
	}
}

func TestCoordinatorProcessPersistsRoomMessageWhenRoomIDPresent(t *testing.T) {
	cfg := setupMemoryTestEnv(t)
	workspacePath := mustMakeWorkspace(t)
	roomID := "alpha"
	proposal := seedLowRiskTaskProposal(t, workspacePath, "gamma", roomID)

	env := mustRunCoordinatorProcess(t, cfg, "--workspace", workspacePath, "--limit", "20")
	data := envelopeDataMap(t, env)
	if got := intFromAny(t, data["room_message_count"]); got != 1 {
		t.Fatalf("room_message_count=%d want 1", got)
	}

	boardStore, err := blackboard.OpenBoardStore(context.Background(), cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open board store: %v", err)
	}
	defer requireClose(t, boardStore, "board store")

	absWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		t.Fatalf("abs workspace: %v", err)
	}
	messages, err := boardStore.ListRoomMessages(context.Background(), absWorkspace, roomID, 20)
	if err != nil {
		t.Fatalf("list room messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("room messages=%d want 1", len(messages))
	}
	if messages[0].Kind != agent.BoardMessageKindTaskUpdate {
		t.Fatalf("room message kind=%q want %q", messages[0].Kind, agent.BoardMessageKindTaskUpdate)
	}
	if !strings.Contains(messages[0].Body, proposal.ID) {
		t.Fatalf("room message body=%q missing proposal id %s", messages[0].Body, proposal.ID)
	}
}

func mustMakeWorkspace(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	return path
}

func seedLowRiskTaskProposal(t *testing.T, workspacePath, suffix, roomID string) contextplane.ControlProposal {
	t.Helper()
	store := contextplane.NewWorkspaceStore(workspacePath)
	workspaceID := workspace.ID(workspacePath)
	proposal, err := store.RecordControlProposal(context.Background(), contextplane.ControlProposal{
		DedupeKey:      "task-proposal-" + suffix,
		Kind:           contextplane.ProposalKindTaskProposal,
		Status:         contextplane.ProposalStatusOpen,
		WorkspaceID:    workspaceID,
		RoomID:         roomID,
		Summary:        "task proposal: Coordinator test task " + suffix,
		Confidence:     0.9,
		BlastRadius:    "low",
		ReviewRequired: true,
		SourceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypeEvent, Ref: "event:task-guard:" + suffix},
		},
		EvidenceRefs: []contextengine.EvidenceRef{
			{Type: contextengine.RefTypePath, Ref: "cmd/foxctl/cmd/coordinator.go"},
		},
		Payload: map[string]any{
			"title":          "Coordinator test task " + suffix,
			"scope_path":     "cmd/foxctl/cmd/coordinator.go",
			"workspace_root": workspacePath,
			"tool_name":      "Edit",
			"tool_canonical": "edit.apply_patch",
			"tool_kind":      "write",
			"intent":         "write cmd/foxctl/cmd/coordinator.go",
		},
	})
	if err != nil {
		t.Fatalf("seed proposal: %v", err)
	}
	return proposal
}

func mustRunCoordinatorProcess(t *testing.T, cfg config.Config, args ...string) envelope.Envelope {
	t.Helper()
	cmd := newCoordinatorProcessCommand()
	cmd.SetContext(config.WithContext(context.Background(), cfg))
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("coordinator process command failed: %v", err)
	}
	return decodeTestEnvelope(t, stdout.Bytes())
}

func intFromAny(t *testing.T, value any) int {
	t.Helper()
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		t.Fatalf("value %T is not numeric", value)
		return 0
	}
}
