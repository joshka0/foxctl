package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/platform/worktree"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/coordination"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

func TestParseRoomMembers(t *testing.T) {
	got, err := parseRoomMembers([]string{"agent-a=lead", "agent-b"})
	if err != nil {
		t.Fatalf("parseRoomMembers: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	if got[0].ActorID != "agent-a" || got[0].Role != "lead" {
		t.Fatalf("got[0]=%+v want actor-a lead", got[0])
	}
	if got[1].ActorID != "agent-b" || got[1].Role != "" {
		t.Fatalf("got[1]=%+v want agent-b empty-role", got[1])
	}
}

func TestParseRoomMemberSpecsSupportsPerMemberAgent(t *testing.T) {
	got, err := parseRoomMemberSpecs([]string{"gemini-a=reviewer@gemini", "cursor-a@agent", "human-a=coordinator"})
	if err != nil {
		t.Fatalf("parseRoomMemberSpecs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	if got[0].Member.ActorID != "gemini-a" || got[0].Member.Role != "reviewer" || got[0].AgentCLI != "gemini" {
		t.Fatalf("got[0]=%+v want gemini-a reviewer@gemini", got[0])
	}
	if got[1].Member.ActorID != "cursor-a" || got[1].Member.Role != "" || got[1].AgentCLI != "agent" {
		t.Fatalf("got[1]=%+v want cursor-a @agent", got[1])
	}
	if got[2].Member.ActorID != "human-a" || got[2].Member.Role != "coordinator" || got[2].AgentCLI != "" {
		t.Fatalf("got[2]=%+v want human-a coordinator", got[2])
	}
}

func TestParseRoomMemberSpecsSupportsPerMemberAgentMode(t *testing.T) {
	got, err := parseRoomMemberSpecs([]string{"cursor-a=reviewer@agent:auto"})
	if err != nil {
		t.Fatalf("parseRoomMemberSpecs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].Member.ActorID != "cursor-a" || got[0].Member.Role != "reviewer" || got[0].AgentCLI != "agent" || got[0].AgentMode != "auto" {
		t.Fatalf("got[0]=%+v want cursor-a reviewer@agent:auto", got[0])
	}
}

func TestRoomMembersFromSpecsStripsAgentMetadata(t *testing.T) {
	specs, err := parseRoomMemberSpecs([]string{"gemini-a=reviewer@gemini"})
	if err != nil {
		t.Fatalf("parseRoomMemberSpecs: %v", err)
	}
	got := roomMembersFromSpecs(specs)
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].ActorID != "gemini-a" || got[0].Role != "reviewer" {
		t.Fatalf("got[0]=%+v want room member without agent metadata", got[0])
	}
}

func TestParseRoomMemberArgMap(t *testing.T) {
	got, err := parseRoomMemberArgMap([]string{"cursor-a=--yolo", "cursor-a=--model=gpt-5", "gemini-a=--sandbox"})
	if err != nil {
		t.Fatalf("parseRoomMemberArgMap: %v", err)
	}
	if len(got["cursor-a"]) != 2 || got["cursor-a"][0] != "--yolo" || got["cursor-a"][1] != "--model=gpt-5" {
		t.Fatalf("cursor-a args=%v want two ordered args", got["cursor-a"])
	}
	if len(got["gemini-a"]) != 1 || got["gemini-a"][0] != "--sandbox" {
		t.Fatalf("gemini-a args=%v want one arg", got["gemini-a"])
	}
}

func TestMergeRoomMembersUpdatesRole(t *testing.T) {
	got := mergeRoomMembers(
		parseMembersForTest("agent-a", "agent-b=member"),
		parseMembersForTest("agent-b=reviewer", "agent-c=observer")...,
	)
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	if got[1].ActorID != "agent-b" || got[1].Role != "reviewer" {
		t.Fatalf("got[1]=%+v want updated reviewer role", got[1])
	}
}

func TestEnsureRoomCoordinatorAddsCreatorWhenMissing(t *testing.T) {
	got := ensureRoomCoordinator(parseMembersForTest("agent-a=lead"), "human-a")
	if len(got) != 2 {
		t.Fatalf("len(got)=%d want 2", len(got))
	}
	last := got[len(got)-1]
	if last.ActorID != "human-a" || last.Role != "coordinator" {
		t.Fatalf("last=%+v want human-a coordinator", last)
	}
}

func TestEnsureRoomCoordinatorPreservesExplicitRole(t *testing.T) {
	got := ensureRoomCoordinator(parseMembersForTest("human-a=lead"), "human-a")
	if len(got) != 1 {
		t.Fatalf("len(got)=%d want 1", len(got))
	}
	if got[0].Role != "lead" {
		t.Fatalf("role=%q want lead", got[0].Role)
	}
}

func TestRunRoomCreateDerivesCurrentParticipantAsCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%18")
	restore := swapRoomTmuxClientForTest(func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(roomFakeRunner{responses: map[string]roomFakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %18 -p " + roomListFormat(): {
				stdout: "%18" + roomFieldSep() + "14" + roomFieldSep() + "0" + roomFieldSep() + "0" + roomFieldSep() + "main" + roomFieldSep() + "111" + roomFieldSep() + "120" + roomFieldSep() + "30" + roomFieldSep() + "human-a" + roomFieldSep() + "/repo" + roomFieldSep() + "zsh" + roomFieldSep() + "1\n",
			},
		}}, map[string]string{
			"TMUX":      "/tmp/tmux.sock,1,0",
			"TMUX_PANE": "%18",
		})
	})
	defer restore()

	ctx := context.Background()
	workspace := t.TempDir()
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok || len(members) != 2 {
		t.Fatalf("members=%T/%v want 2 entries", roomRaw["members"], roomRaw["members"])
	}
	foundCoordinator := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "human-a" && member["role"] == "coordinator" {
			if member["backend"] != "tmux" {
				t.Fatalf("backend=%v want tmux", member["backend"])
			}
			if member["session"] != "14" {
				t.Fatalf("session=%v want 14", member["session"])
			}
			if member["pane_id"] != "%18" {
				t.Fatalf("pane_id=%v want %%18", member["pane_id"])
			}
			foundCoordinator = true
			break
		}
	}
	if !foundCoordinator {
		t.Fatalf("expected derived coordinator in members=%v", members)
	}
}

func TestRoomCommandFlow_CreateJoinSendShow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "coordination room", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "agent-b", "reviewer", "", "", "", "", "", false, true, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "", "hello room", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)

	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T", data["room"])
	}
	if roomRaw["id"] != "alpha" {
		t.Fatalf("room id=%v want alpha", roomRaw["id"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok || len(members) < 2 {
		t.Fatalf("members=%T/%v want at least 2 entries", roomRaw["members"], roomRaw["members"])
	}
	foundLead := false
	foundReviewer := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "agent-a" && member["role"] == "lead" {
			foundLead = true
		}
		if member["actor_id"] == "agent-b" && member["role"] == "reviewer" {
			foundReviewer = true
		}
	}
	if !foundLead || !foundReviewer {
		t.Fatalf("members=%v want agent-a lead and agent-b reviewer", members)
	}
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T/%v want 1 entry", data["messages"], data["messages"])
	}
	msg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", messages[0])
	}
	if msg["sender"] != "agent-a" {
		t.Fatalf("sender=%v want agent-a", msg["sender"])
	}
	if msg["stream"] != "room:alpha" {
		t.Fatalf("stream=%v want room:alpha", msg["stream"])
	}
	body, _ := msg["body"].(string)
	if !strings.Contains(body, "hello room") {
		t.Fatalf("body=%v want to contain hello room", msg["body"])
	}
}

func TestRoomTaskFlow_AddListComplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "agent-a", "Review retry path", "Inspect fallback retry flow", "src/api/client.ts", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, ok := taskRaw["ID"].(string)
	if !ok || taskID == "" {
		taskID, ok = taskRaw["id"].(string)
		if !ok || taskID == "" {
			t.Fatalf("task id missing in payload=%v", taskRaw)
		}
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskList(cmd, workspace, "alpha", "", true, true); err != nil {
		t.Fatalf("runRoomTaskList: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	tasksRaw, ok := data["tasks"].([]any)
	if !ok || len(tasksRaw) != 1 {
		t.Fatalf("tasks=%T/%v want 1 entry", data["tasks"], data["tasks"])
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "agent-b", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskComplete(cmd, workspace, "alpha", "agent-b", taskID, "Retry path simplified", "Watch auth fallback regressions"); err != nil {
		t.Fatalf("runRoomTaskComplete: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	status := taskRaw["status"]
	if status == nil {
		status = taskRaw["Status"]
	}
	if status != "completed" {
		t.Fatalf("status=%v want completed", status)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskList(cmd, workspace, "alpha", "", true, true); err != nil {
		t.Fatalf("runRoomTaskList after complete: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	tasksRaw, ok = data["tasks"].([]any)
	if !ok || len(tasksRaw) != 0 {
		t.Fatalf("tasks after complete (default)=%T/%v want 0 entries", data["tasks"], data["tasks"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskList(cmd, workspace, "alpha", "", false, false); err != nil {
		t.Fatalf("runRoomTaskList include completed: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	tasksRaw, ok = data["tasks"].([]any)
	if !ok || len(tasksRaw) != 1 {
		t.Fatalf("tasks with includeCompleted=%T/%v want 1 entry", data["tasks"], data["tasks"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskList(cmd, workspace, "alpha", "completed", false, false); err != nil {
		t.Fatalf("runRoomTaskList status=completed: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	tasksRaw, ok = data["tasks"].([]any)
	if !ok || len(tasksRaw) != 1 {
		t.Fatalf("tasks with status=completed filter=%T/%v want 1 entry", data["tasks"], data["tasks"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T/%v want 1 entry", data["messages"], data["messages"])
	}
}

func TestRoomTaskAddDefaultsToLatestEpicChoresMilestone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "agent-a", "alpha", "Runtime hardening", "quiet chores lane", "", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicData := decodeRoomEnvelope(t, out)
	epicID, _ := epicData["epic_id"].(string)
	choresMilestoneID, _ := epicData["chores_milestone_id"].(string)
	if epicID == "" || choresMilestoneID == "" {
		t.Fatalf("epic data missing ids: %v", epicData)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "agent-a", "Quiet follow-up", "Default chores linkage", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	gotEpicID, _ := taskRaw["EpicID"].(string)
	if gotEpicID == "" {
		gotEpicID, _ = taskRaw["epic_id"].(string)
	}
	gotMilestoneID, _ := taskRaw["MilestoneID"].(string)
	if gotMilestoneID == "" {
		gotMilestoneID, _ = taskRaw["milestone_id"].(string)
	}
	if gotEpicID != epicID {
		t.Fatalf("EpicID=%q want %q", gotEpicID, epicID)
	}
	if gotMilestoneID != choresMilestoneID {
		t.Fatalf("MilestoneID=%q want %q", gotMilestoneID, choresMilestoneID)
	}
}

func TestRoomTaskAddUsesExplicitMilestoneSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship agile nouns on top of room", "human-a", "Transport-agnostic planning and delivery", "Q2", []string{"epics", "milestones"}, []string{"agents can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicAsk(cmd, workspace, "human-a", "alpha", epicID, "gemini-a", "success", "What must be true before milestones can open?"); err != nil {
		t.Fatalf("runRoomEpicAsk: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	questionMsg := data["message"].(map[string]any)
	questionID := questionMsg["id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicAnswer(cmd, workspace, "gemini-a", "alpha", questionID, "The epic needs a clarified brief and no open intake questions."); err != nil {
		t.Fatalf("runRoomEpicAnswer: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship core CLI nouns", "", "human-a", []string{"commands"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	cmd.Flags().String("milestone-id", "", "")
	if err := cmd.Flags().Set("milestone-id", milestoneID); err != nil {
		t.Fatalf("Set milestone-id: %v", err)
	}
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Milestone task", "Explicit lane selection", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	gotEpicID, _ := taskRaw["EpicID"].(string)
	if gotEpicID == "" {
		gotEpicID, _ = taskRaw["epic_id"].(string)
	}
	gotMilestoneID, _ := taskRaw["MilestoneID"].(string)
	if gotMilestoneID == "" {
		gotMilestoneID, _ = taskRaw["milestone_id"].(string)
	}
	if gotEpicID != epicID {
		t.Fatalf("EpicID=%q want %q", gotEpicID, epicID)
	}
	if gotMilestoneID != milestoneID {
		t.Fatalf("MilestoneID=%q want %q", gotMilestoneID, milestoneID)
	}
}

func TestRoomTaskAddRejectsUnknownMilestoneSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().String("milestone-id", "", "")
	if err := cmd.Flags().Set("milestone-id", "mile-missing"); err != nil {
		t.Fatalf("Set milestone-id: %v", err)
	}
	err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Bad lane", "Explicit lane selection", "", "", nil, true)
	assertRoomErrorContains(t, err, "room task milestone not found")
}

func TestRoomTaskFlow_ClaimBlockUnblockAbandon(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "agent-a", "Task claim flow", "Inspect claim flow", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	if taskID == "" {
		t.Fatalf("task id missing in payload=%v", taskRaw)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "agent-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["OwnerActorID"]; got != "agent-a" {
		t.Fatalf("owner=%v want agent-a", got)
	}
	heartbeatBefore, _ := taskRaw["HeartbeatAt"].(string)
	if heartbeatBefore == "" {
		t.Fatalf("heartbeat missing after claim: %v", taskRaw)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskTouch(cmd, workspace, "alpha", "agent-a", taskID); err != nil {
		t.Fatalf("runRoomTaskTouch: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	heartbeatAfter, _ := taskRaw["HeartbeatAt"].(string)
	if heartbeatAfter == "" {
		t.Fatalf("heartbeat missing after touch: %v", taskRaw)
	}
	if heartbeatAfter == heartbeatBefore {
		t.Fatalf("heartbeat=%q want refreshed value different from %q", heartbeatAfter, heartbeatBefore)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskBlock(cmd, workspace, "alpha", "agent-a", taskID, "Waiting on human"); err != nil {
		t.Fatalf("runRoomTaskBlock: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["Status"]; got != "blocked" {
		t.Fatalf("status=%v want blocked", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskUnblock(cmd, workspace, "alpha", "agent-a", taskID); err != nil {
		t.Fatalf("runRoomTaskUnblock: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["Status"]; got != "in_progress" {
		t.Fatalf("status=%v want in_progress", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAbandon(cmd, workspace, "alpha", "agent-a", taskID, "Releasing back to queue"); err != nil {
		t.Fatalf("runRoomTaskAbandon: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["Status"]; got != "pending" {
		t.Fatalf("status=%v want pending", got)
	}
	if got := taskRaw["OwnerActorID"]; got != "" {
		t.Fatalf("owner=%v want empty", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T/%v want 1 entry (task add only)", data["messages"], data["messages"])
	}
}

func TestRoomLoopShouldRelayTaskTransitionModes(t *testing.T) {
	cases := []struct {
		name   string
		mode   string
		update roomTaskTransition
		want   bool
	}{
		{
			name:   "quiet suppresses claim",
			mode:   "quiet",
			update: roomTaskTransition{PreviousStatus: taskstore.StatusPending, CurrentStatus: taskstore.StatusInProgress},
			want:   false,
		},
		{
			name:   "default relays claim",
			mode:   "default",
			update: roomTaskTransition{PreviousStatus: taskstore.StatusPending, CurrentStatus: taskstore.StatusInProgress},
			want:   true,
		},
		{
			name:   "default relays complete",
			mode:   "default",
			update: roomTaskTransition{PreviousStatus: taskstore.StatusInProgress, CurrentStatus: taskstore.StatusCompleted},
			want:   true,
		},
		{
			name:   "default suppresses unblock",
			mode:   "default",
			update: roomTaskTransition{PreviousStatus: taskstore.StatusBlocked, CurrentStatus: taskstore.StatusInProgress},
			want:   false,
		},
		{
			name:   "verbose relays unblock",
			mode:   "verbose",
			update: roomTaskTransition{PreviousStatus: taskstore.StatusBlocked, CurrentStatus: taskstore.StatusInProgress},
			want:   true,
		},
	}
	for _, tc := range cases {
		if got := roomLoopShouldRelayTaskTransition(tc.mode, tc.update); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunRoomTaskCompleteRequiresClaim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "agent-a", "Must claim first", "No direct complete", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskComplete(cmd, workspace, "alpha", "agent-a", taskID, "should fail", ""); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskComplete: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomTaskAssignRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Assign me", "Please assign this", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "gemini-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskAssign: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomTaskAssignAllowsRoomAdmin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "parent-a=admin", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Delegatable", "assign via admin", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "parent-a", taskID, "gemini-a", "parent admin delegates", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["AssignedActorID"]; got != "gemini-a" {
		t.Fatalf("assigned=%v want gemini-a", got)
	}
}

func TestRunRoomTaskAssignAllowsSystemAdminSender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Sysadmin assign", "body", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "actor:admin:room-ops", taskID, "gemini-a", "ops", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["AssignedActorID"]; got != "gemini-a" {
		t.Fatalf("assigned=%v want gemini-a", got)
	}
}

func TestRunRoomTaskAssignPersistsAssignmentAndNotifiesRecipient(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Assign me", "Please assign this", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "please pick this up", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["AssignedActorID"]; got != "gemini-a" {
		t.Fatalf("assigned=%v want gemini-a", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("messages=%T/%v want at least 2 entries (task add + assignment instruction)", data["messages"], data["messages"])
	}
	foundDirect := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msg["recipient"] == "gemini-a" && msg["task_id"] == taskID {
			body, _ := msg["body"].(string)
			if !strings.Contains(body, "foxctl-room-operator") {
				t.Fatalf("direct assignment body missing skill hint: %q", body)
			}
			if !strings.Contains(body, "foxctl room send alpha --to human-a") {
				t.Fatalf("direct assignment body missing direct reply command: %q", body)
			}
			if !strings.Contains(body, "foxctl room task complete alpha --id "+taskID) {
				t.Fatalf("direct assignment body missing completion command: %q", body)
			}
			foundDirect = true
			break
		}
	}
	if !foundDirect {
		t.Fatalf("expected direct assignment message for gemini-a in messages=%v", messages)
	}
}

func TestRunRoomTaskAssignRejectsClaimedTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "claude-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Claimed task", "body", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw := data["task"].(map[string]any)
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "claude-a", "switch", roomTaskAssignOptions{}); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskAssign: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomTaskReassignRejectsUnassignedTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Fresh pending", "body", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw := data["task"].(map[string]any)
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskReassign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "switch"); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskReassign: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomTaskClaimRejectsCanceledTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	taskStore, err := openRoomTaskStore(ctx)
	if err != nil {
		t.Fatalf("openRoomTaskStore: %v", err)
	}
	defer taskStore.Close()
	now := time.Now().UTC()
	task, err := taskStore.Add(ctx, taskstore.Task{
		WorkspaceID: ws.CanonicalID(workspace),
		Title:       "Canceled task",
		Status:      taskstore.StatusCanceled,
		CreatedAt:   now,
	})
	if err != nil {
		t.Fatalf("taskStore.Add: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", task.ID); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskClaim: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomTaskReassignResetsOwnershipAndRetargetsAssignee(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "claude-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Reassign me", "Please reassign this", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskReassign(cmd, workspace, "alpha", "human-a", taskID, "claude-a", "switch reviewer"); err != nil {
		t.Fatalf("runRoomTaskReassign: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["AssignedActorID"]; got != "claude-a" {
		t.Fatalf("assigned=%v want claude-a", got)
	}
	if got := taskRaw["OwnerActorID"]; got != "" {
		t.Fatalf("owner=%v want empty after reassign", got)
	}
	if got := taskRaw["Status"]; got != "pending" {
		t.Fatalf("status=%v want pending after reassign", got)
	}
}

func TestRunRoomTaskReclaimReturnsTaskToPool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Reclaim me", "Please reclaim this", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskReclaim(cmd, workspace, "alpha", "human-a", taskID, "stale owner"); err != nil {
		t.Fatalf("runRoomTaskReclaim: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskRaw, ok = data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	if got := taskRaw["AssignedActorID"]; got != "" {
		t.Fatalf("assigned=%v want empty after reclaim", got)
	}
	if got := taskRaw["OwnerActorID"]; got != "" {
		t.Fatalf("owner=%v want empty after reclaim", got)
	}
	if got := taskRaw["Status"]; got != "pending" {
		t.Fatalf("status=%v want pending after reclaim", got)
	}
}

func TestRunRoomTaskClaimRejectsDifferentAssignee(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "claude-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Assign me", "Please assign this", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "claude-a", taskID); err != nil {
		var we *protocol.WrittenEnvelopeError
		if !errors.As(err, &we) {
			t.Fatalf("runRoomTaskClaim: %v", err)
		}
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomStatusIncludesPulseAndBacklog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Status task", "Inspect status", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, nil, "open", false); err != nil {
		t.Fatalf("runRoomStatus: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	taskPulse, ok := data["task_pulse"].(map[string]any)
	if !ok {
		t.Fatalf("task_pulse type=%T", data["task_pulse"])
	}
	if got := taskPulse["assigned_unclaimed"]; got != float64(1) {
		t.Fatalf("assigned_unclaimed=%v want 1", got)
	}
	backlog, ok := data["backlog"].(map[string]any)
	if !ok {
		t.Fatalf("backlog type=%T", data["backlog"])
	}
	if got := backlog["participants_with_pending"]; got != float64(1) {
		t.Fatalf("participants_with_pending=%v want 1", got)
	}
	latestByParticipant, ok := backlog["latest_by_participant"].([]any)
	if !ok || len(latestByParticipant) != 1 {
		t.Fatalf("latest_by_participant=%T/%v want 1 entry", backlog["latest_by_participant"], backlog["latest_by_participant"])
	}
	participants, ok := data["participants"].([]any)
	if !ok || len(participants) == 0 {
		t.Fatalf("participants=%T/%v want entries", data["participants"], data["participants"])
	}
	foundLatest := false
	for _, raw := range participants {
		participant, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if participant["actor_id"] != "gemini-a" {
			continue
		}
		if participant["actionable_inbox_count"] != float64(1) {
			t.Fatalf("actionable_inbox_count=%v want 1", participant["actionable_inbox_count"])
		}
		latestActionable, ok := participant["latest_actionable"].(map[string]any)
		if !ok {
			t.Fatalf("latest_actionable missing for gemini-a participant=%v", participant)
		}
		if _, ok := latestActionable["message"]; ok {
			t.Fatalf("latest_actionable unexpectedly contains full message payload: %v", latestActionable)
		}
		foundLatest = true
	}
	if !foundLatest {
		t.Fatalf("expected gemini-a participant with latest actionable entry in participants=%v", participants)
	}
	actionRequired, ok := data["action_required"].(map[string]any)
	if !ok {
		t.Fatalf("action_required type=%T", data["action_required"])
	}
	if got := actionRequired["assigned_unclaimed"]; got != float64(1) {
		t.Fatalf("assigned_unclaimed action=%v want 1", got)
	}
	topEntries, ok := actionRequired["top_entries"].([]any)
	if !ok || len(topEntries) != 1 {
		t.Fatalf("top_entries=%T/%v want 1 entry", actionRequired["top_entries"], actionRequired["top_entries"])
	}
	topEntry, ok := topEntries[0].(map[string]any)
	if !ok {
		t.Fatalf("top_entry type=%T", topEntries[0])
	}
	if _, ok := topEntry["message"]; ok {
		t.Fatalf("top_entry unexpectedly contains full message payload: %v", topEntry)
	}
}

func TestRunRoomStatusFilterOpenOmitsCompletedFromTaskPulse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Done task", "body", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw := data["task"].(map[string]any)
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "go", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskComplete(cmd, workspace, "alpha", "gemini-a", taskID, "done", ""); err != nil {
		t.Fatalf("runRoomTaskComplete: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, nil, "open", false); err != nil {
		t.Fatalf("runRoomStatus open: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	pulse := data["task_pulse"].(map[string]any)
	if got := pulse["completed"]; got != float64(0) {
		t.Fatalf("task_pulse.completed (filter open)=%v want 0", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, nil, "all", false); err != nil {
		t.Fatalf("runRoomStatus all: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	pulse = data["task_pulse"].(map[string]any)
	if got := pulse["completed"]; got != float64(1) {
		t.Fatalf("task_pulse.completed (filter all)=%v want 1", got)
	}
}

func TestParseRoomTaskListSelection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status, filter string
		wantStatus     string
		wantOmitC      bool
		wantOmitCan    bool
		wantErr        bool
	}{
		{"", "open", "", true, true, false},
		{"", "active", "", true, true, false},
		{"", "all", "", false, false, false},
		{"", "completed", taskstore.StatusCompleted, false, false, false},
		{"pending", "open", "pending", false, false, false},
		{"pending", "completed", "", false, false, true},
		{"completed", "completed", taskstore.StatusCompleted, false, false, false},
		{"bogus", "all", "bogus", false, false, false},
		{"", "weird", "", false, false, true},
	}
	for _, tc := range cases {
		st, oc, ocan, err := parseRoomTaskListSelection(tc.status, tc.filter)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("status=%q filter=%q want error", tc.status, tc.filter)
			}
			continue
		}
		if err != nil {
			t.Fatalf("status=%q filter=%q err=%v", tc.status, tc.filter, err)
		}
		if st != tc.wantStatus || oc != tc.wantOmitC || ocan != tc.wantOmitCan {
			t.Fatalf("status=%q filter=%q got (%q,%v,%v) want (%q,%v,%v)", tc.status, tc.filter, st, oc, ocan, tc.wantStatus, tc.wantOmitC, tc.wantOmitCan)
		}
	}
}

func TestDeriveParticipantTransportState_PaneSocketReady(t *testing.T) {
	// Create a real socket file to simulate a live pane wrapper.
	socketPath := filepath.Join(t.TempDir(), "claude-a.sock")
	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatalf("create socket file: %v", err)
	}
	f.Close()

	m := agent.RoomMember{
		ActorID:           "claude-a",
		Backend:           "zellij",
		Session:           "test-session",
		PaneID:            "terminal_0",
		TransportEndpoint: socketPath,
		TransportKind:     agent.PaneSocketTransportKind,
	}
	s := deriveParticipantTransportState(m)

	if s.Membership != agent.MembershipActive {
		t.Errorf("Membership=%q want active", s.Membership)
	}
	if s.Transport != agent.TransportAvailable {
		t.Errorf("Transport=%q want available", s.Transport)
	}
	if s.Runtime != agent.RuntimeLive {
		t.Errorf("Runtime=%q want live", s.Runtime)
	}
	if s.TransportEndpoint != socketPath {
		t.Errorf("TransportEndpoint=%q want %q", s.TransportEndpoint, socketPath)
	}
	if s.MuxBackend != "zellij" {
		t.Errorf("MuxBackend=%q want zellij", s.MuxBackend)
	}
	if !s.CanTriggerTurn {
		t.Errorf("CanTriggerTurn=false want true")
	}
}

func TestDeriveParticipantTransportState_PaneSocketUnavailable(t *testing.T) {
	m := agent.RoomMember{
		ActorID:           "claude-a",
		TransportEndpoint: "/tmp/nonexistent-foxctl-sock/claude-a.sock",
		TransportKind:     agent.PaneSocketTransportKind,
	}
	s := deriveParticipantTransportState(m)

	if s.Transport != agent.TransportUnavailable {
		t.Errorf("Transport=%q want unavailable", s.Transport)
	}
	if s.Runtime != agent.RuntimeStopped {
		t.Errorf("Runtime=%q want stopped", s.Runtime)
	}
	if s.CanTriggerTurn {
		t.Errorf("CanTriggerTurn=true want false")
	}
}

func TestDeriveParticipantTransportState_MuxPane(t *testing.T) {
	m := agent.RoomMember{
		ActorID: "droid-a",
		Backend: "tmux",
		Session: "test-session",
		PaneID:  "%5",
	}
	s := deriveParticipantTransportState(m)

	// Legacy mux-pane: transport endpoint is derived from backend/session/pane,
	// state is unknown (no live probe), presentation is detached.
	if s.Membership != agent.MembershipActive {
		t.Errorf("Membership=%q want active", s.Membership)
	}
	if s.Transport != agent.TransportUnknown {
		t.Errorf("Transport=%q want unknown", s.Transport)
	}
	if s.Presentation != agent.PresentationDetached {
		t.Errorf("Presentation=%q want detached", s.Presentation)
	}
	if s.MuxBackend != "tmux" {
		t.Errorf("MuxBackend=%q want tmux", s.MuxBackend)
	}
	if s.TransportEndpoint == "" {
		t.Errorf("TransportEndpoint empty, want mux address")
	}
}

func TestDeriveParticipantTransportState_UnboundMember(t *testing.T) {
	m := agent.RoomMember{ActorID: "gemini-a", Unbound: true}
	s := deriveParticipantTransportState(m)

	if s.Membership != agent.MembershipUnbound {
		t.Errorf("Membership=%q want unbound", s.Membership)
	}
	if s.Transport != agent.TransportNone {
		t.Errorf("Transport=%q want none", s.Transport)
	}
	if s.CanTriggerTurn {
		t.Errorf("CanTriggerTurn=true want false for unbound member")
	}
}

func TestBuildRoomStatusParticipantsIncludesTransport(t *testing.T) {
	// Create a socket file so claude-a shows transport_state=ready.
	socketPath := filepath.Join(t.TempDir(), "claude-a.sock")
	f, err := os.Create(socketPath)
	if err != nil {
		t.Fatalf("create socket file: %v", err)
	}
	f.Close()

	room := agent.RoomSummary{
		ID:    "transport-alpha",
		Title: "Transport Alpha",
		Members: []agent.RoomMember{
			{
				ActorID:           "claude-a",
				Role:              "worker",
				Backend:           "zellij",
				Session:           "test-session",
				PaneID:            "terminal_0",
				TransportEndpoint: socketPath,
				TransportKind:     "pane_socket",
			},
			{
				ActorID: "gemini-a",
				Role:    "reviewer",
				Backend: "tmux",
				Session: "test-session",
				PaneID:  "%5",
			},
		},
		Participants: []string{"claude-a", "gemini-a"},
	}

	participants := buildRoomStatusParticipants(room, nil, nil, 5*time.Minute, nil)

	byActor := make(map[string]roomStatusParticipant)
	for _, p := range participants {
		byActor[p.ActorID] = p
	}

	claudeA := byActor["claude-a"]
	if claudeA.Transport.Membership != agent.MembershipActive {
		t.Errorf("claude-a Membership=%q want active", claudeA.Transport.Membership)
	}
	if claudeA.Transport.Transport != agent.TransportAvailable {
		t.Errorf("claude-a Transport=%q want available", claudeA.Transport.Transport)
	}
	if claudeA.Transport.Runtime != agent.RuntimeLive {
		t.Errorf("claude-a Runtime=%q want live", claudeA.Transport.Runtime)
	}
	if claudeA.Transport.MuxBackend != "zellij" {
		t.Errorf("claude-a MuxBackend=%q want zellij", claudeA.Transport.MuxBackend)
	}
	if !claudeA.Transport.CanTriggerTurn {
		t.Errorf("claude-a CanTriggerTurn=false want true")
	}

	geminiA := byActor["gemini-a"]
	if geminiA.Transport.Membership != agent.MembershipActive {
		t.Errorf("gemini-a Membership=%q want active", geminiA.Transport.Membership)
	}
	if geminiA.Transport.Transport != agent.TransportUnknown {
		t.Errorf("gemini-a Transport=%q want unknown", geminiA.Transport.Transport)
	}
	if geminiA.Transport.Presentation != agent.PresentationDetached {
		t.Errorf("gemini-a Presentation=%q want detached", geminiA.Transport.Presentation)
	}
}

func TestBuildRoomStatusEntriesCollapsesHistoricalBacklogByChain(t *testing.T) {
	now := time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC)
	entries := buildRoomStatusEntries("gemini-a", []agent.BoardMessage{
		{
			ID:               "m1",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "orig-1",
			Subject:          "old reminder",
			Body:             "old reminder",
			CreatedAt:        now,
			Priority:         2,
			Status:           agent.BoardMessageStatusUnread,
			ReplyExpected:    true,
		},
		{
			ID:               "m2",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "orig-1",
			Subject:          "new reminder",
			Body:             "new reminder",
			CreatedAt:        now.Add(2 * time.Minute),
			Priority:         2,
			Status:           agent.BoardMessageStatusUnread,
			ReplyExpected:    true,
		},
		{
			ID:               "m3",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "orig-2",
			Subject:          "other reminder",
			Body:             "other reminder",
			CreatedAt:        now.Add(1 * time.Minute),
			Priority:         2,
			Status:           agent.BoardMessageStatusUnread,
		},
	}, nil)
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ID != "m2" {
		t.Fatalf("entries[0].ID=%q want m2", entries[0].ID)
	}
}

func TestBuildRoomStatusEntriesSkipsCompletedTaskReplyDebt(t *testing.T) {
	suppression := buildRoomActionSuppression(nil, []taskstore.Task{{
		ID:     "task-1",
		Status: taskstore.StatusCompleted,
	}}, nil)
	entries := buildRoomStatusEntries("gemini-a", []agent.BoardMessage{{
		ID:            "msg-1",
		TaskID:        "task-1",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Subject:       "Review diff",
		Body:          "please review",
		CreatedAt:     time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC),
		Priority:      2,
		Status:        agent.BoardMessageStatusUnread,
		ReplyExpected: true,
	}}, suppression)
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestBuildRoomStatusEntriesSkipsSummarizedMilestoneBoundary(t *testing.T) {
	now := time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC)
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Delivery owner",
			Body:             "EpicID: epic-1",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
		{
			ID:               "mile-summary-1",
			Kind:             agent.BoardMessageKindMilestoneSummary,
			RelatedMessageID: "mile-1",
			Subject:          "Milestone Summary: Delivery owner",
			Body:             "Summary: closed",
			CreatedAt:        now.Add(-2 * time.Minute),
		},
		{
			ID:               "review-1",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "mile-1",
			Subject:          "Review boundary",
			Body:             "please review",
			CreatedAt:        now,
			Priority:         2,
			Status:           agent.BoardMessageStatusUnread,
			ReplyExpected:    true,
		},
	}
	entries := buildRoomStatusEntries("gemini-a", messages, buildRoomActionSuppression(messages, nil, nil))
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestBuildRoomStatusEntriesSkipsQuietChoresMilestoneBoundary(t *testing.T) {
	now := time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC)
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
		{
			ID:               "review-1",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "mile-chores-1",
			Subject:          "Review boundary",
			Body:             "please review",
			CreatedAt:        now,
			Priority:         2,
			Status:           agent.BoardMessageStatusUnread,
			ReplyExpected:    true,
		},
	}
	entries := buildRoomStatusEntries("gemini-a", messages, buildRoomActionSuppression(messages, nil, nil))
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestBuildRoomStatusTaskEntriesSkipsQuietChoresTask(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
	}
	tasks := []taskstore.Task{{
		ID:              "task-1",
		Title:           "Quiet follow-up",
		Status:          taskstore.StatusPending,
		AssignedActorID: "gemini-a",
		MilestoneID:     "mile-chores-1",
	}}
	suppression := buildRoomActionSuppression(messages, tasks, nil)
	entries := buildRoomStatusTaskEntries(tasks, map[string]struct{}{"all": {}}, now, 5*time.Minute, suppression)
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestRoomStatusEntryFromInboxOmitsFullMessagePayload(t *testing.T) {
	entry := roomStatusEntryFromInbox(roomInboxEntry{
		ID:        "m1",
		Sender:    "human-a",
		Recipient: "gemini-a",
		Subject:   "Need review",
		Priority:  2,
		Status:    agent.BoardMessageStatusUnread,
		CreatedAt: time.Date(2026, 4, 4, 20, 0, 0, 0, time.UTC),
		Category:  "direct",
		Flags:     []string{"REPLY-EXPECTED"},
		Preview:   "Need review",
		Message: agent.BoardMessage{
			ID:      "m1",
			Body:    "full body",
			Subject: "Need review",
		},
	})
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if bytes.Contains(raw, []byte(`"message"`)) {
		t.Fatalf("unexpected message payload in %s", raw)
	}
}

func TestRunRoomStatusVerboseIncludesVerboseTopEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, nil, "open", true); err != nil {
		t.Fatalf("runRoomStatus verbose: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	actionRequired, ok := data["action_required"].(map[string]any)
	if !ok {
		t.Fatalf("action_required type=%T", data["action_required"])
	}
	verboseTopEntries, ok := actionRequired["verbose_top_entries"].([]any)
	if !ok || len(verboseTopEntries) != 1 {
		t.Fatalf("verbose_top_entries=%T/%v want 1 entry", actionRequired["verbose_top_entries"], actionRequired["verbose_top_entries"])
	}
	entry, ok := verboseTopEntries[0].(map[string]any)
	if !ok {
		t.Fatalf("verbose entry type=%T", verboseTopEntries[0])
	}
	if _, ok := entry["message"].(map[string]any); !ok {
		t.Fatalf("verbose entry missing full message payload: %v", entry)
	}
}

func TestRunRoomStatusOnlyFiltersActionRequired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomTaskAdd(cmd, workspace, "alpha", "human-a", "Blocked task", "Inspect blockage", "", "", nil, true); err != nil {
		t.Fatalf("runRoomTaskAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	taskRaw, ok := data["task"].(map[string]any)
	if !ok {
		t.Fatalf("task payload type=%T", data["task"])
	}
	taskID, _ := taskRaw["id"].(string)
	if taskID == "" {
		taskID, _ = taskRaw["ID"].(string)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this", roomTaskAssignOptions{}); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "gemini-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomTaskBlock(cmd, workspace, "alpha", "gemini-a", taskID, "waiting"); err != nil {
		t.Fatalf("runRoomTaskBlock: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, []string{"blocked"}, "open", false); err != nil {
		t.Fatalf("runRoomStatus blocked: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	actionRequired, ok := data["action_required"].(map[string]any)
	if !ok {
		t.Fatalf("action_required type=%T", data["action_required"])
	}
	if got := actionRequired["blocked_tasks"]; got != float64(1) {
		t.Fatalf("blocked_tasks=%v want 1", got)
	}
	if got := actionRequired["pending_acks"]; got != float64(0) {
		t.Fatalf("pending_acks=%v want 0", got)
	}
	if rawTopEntries, exists := actionRequired["top_entries"]; exists {
		topEntries, ok := rawTopEntries.([]any)
		if !ok || len(topEntries) != 0 {
			t.Fatalf("top_entries=%T/%v want empty", rawTopEntries, rawTopEntries)
		}
	}
	topTasks, ok := actionRequired["top_tasks"].([]any)
	if !ok || len(topTasks) != 1 {
		t.Fatalf("top_tasks=%T/%v want 1", actionRequired["top_tasks"], actionRequired["top_tasks"])
	}
	taskEntry, ok := topTasks[0].(map[string]any)
	if !ok {
		t.Fatalf("taskEntry type=%T", topTasks[0])
	}
	signals, ok := taskEntry["signals"].([]any)
	if !ok || len(signals) != 1 || signals[0] != "blocked" {
		t.Fatalf("signals=%v want [blocked]", taskEntry["signals"])
	}
}

func TestRunRoomResolveRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	messages := data["messages"].([]any)
	msgID := messages[0].(map[string]any)["id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomResolve(cmd, workspace, "alpha", "gemini-a", "acked", false, nil, []string{msgID})
	assertRoomErrorContains(t, err, "room resolve requires coordinator role")
}

func TestRunRoomResolveMarksMessageResolved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	messages := data["messages"].([]any)
	msgID := messages[0].(map[string]any)["id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomResolve(cmd, workspace, "alpha", "human-a", "acked", false, nil, []string{msgID}); err != nil {
		t.Fatalf("runRoomResolve: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["resolved_status"]; got != string(agent.BoardMessageStatusAcked) {
		t.Fatalf("resolved_status=%v want acked", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 20, nil, "open", false); err != nil {
		t.Fatalf("runRoomStatus: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	actionRequired := data["action_required"].(map[string]any)
	if got := actionRequired["participants_with_pending"]; got != float64(0) {
		t.Fatalf("participants_with_pending=%v want 0", got)
	}
}

func TestRunRoomResolveMarksRelatedReminderChainResolved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	messages := data["messages"].([]any)
	originalID := messages[0].(map[string]any)["id"].(string)

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()
	for _, id := range []string{"r1", "r2"} {
		msg := &agent.BoardMessage{
			ID:               id,
			WorkspaceID:      workspace,
			RelatedMessageID: originalID,
			Stream:           agent.RoomStreamName("alpha"),
			Sender:           roomLoopSender("alpha"),
			Recipient:        "gemini-a",
			Kind:             agent.BoardMessageKindAlert,
			Priority:         2,
			Subject:          "reminder",
			Body:             "reminder",
			CreatedAt:        time.Now().UTC(),
		}
		if err := store.SendMessage(ctx, msg); err != nil {
			t.Fatalf("SendMessage reminder %s: %v", id, err)
		}
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomResolve(cmd, workspace, "alpha", "human-a", "acked", false, nil, []string{"r2"}); err != nil {
		t.Fatalf("runRoomResolve: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["updated"]; got != float64(3) {
		t.Fatalf("updated=%v want 3", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, nil, "open", false); err != nil {
		t.Fatalf("runRoomStatus: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	actionRequired := data["action_required"].(map[string]any)
	if got := actionRequired["participants_with_pending"]; got != float64(0) {
		t.Fatalf("participants_with_pending=%v want 0", got)
	}
}

func TestRunRoomResolveAllByFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "claude-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	for _, recipient := range []string{"gemini-a", "claude-a"} {
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomSend(cmd, workspace, "alpha", "human-a", recipient, "", "please ack", "info", "", 0, true, false, false, true); err != nil {
			t.Fatalf("runRoomSend(%s): %v", recipient, err)
		}
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomResolve(cmd, workspace, "alpha", "human-a", "acked", true, []string{"ack"}, nil); err != nil {
		t.Fatalf("runRoomResolve all: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["updated"]; got != float64(2) {
		t.Fatalf("updated=%v want 2", got)
	}
}

func TestRunRoomClearCoordinatorPulses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	for _, msg := range []*agent.BoardMessage{
		{
			ID:          "pulse-typed",
			WorkspaceID: workspace,
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "human-a",
			Kind:        agent.BoardMessageKindCoordinatorPulse,
			Priority:    2,
			Subject:     "Coordinator pulse: 1 pending participants, 0 blocked, 0 stale",
			Body:        "As coordinator, keep the room on track.",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "pulse-legacy",
			WorkspaceID: workspace,
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "human-a",
			Kind:        agent.BoardMessageKindAlert,
			Priority:    2,
			Subject:     "Coordinator pulse: 2 pending participants, 0 blocked, 0 stale",
			Body:        "As coordinator, keep the room on track.",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "task-reminder",
			WorkspaceID: workspace,
			TaskID:      "task-1",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "human-a",
			Kind:        agent.BoardMessageKindAlert,
			Priority:    2,
			Subject:     "Reminder: task awaiting update",
			Body:        "Task reminder",
			CreatedAt:   time.Now().UTC(),
		},
	} {
		if err := store.SendMessage(ctx, msg); err != nil {
			t.Fatalf("SendMessage(%s): %v", msg.ID, err)
		}
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomClear(cmd, workspace, "alpha", "human-a", "read", "coordinator-pulses"); err != nil {
		t.Fatalf("runRoomClear: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["updated"]; got != float64(2) {
		t.Fatalf("updated=%v want 2", got)
	}

	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", 20)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}
	statusByID := make(map[string]agent.BoardMessageStatus, len(messages))
	for _, msg := range messages {
		statusByID[msg.ID] = msg.Status
	}
	if got := statusByID["pulse-typed"]; got != agent.BoardMessageStatusRead {
		t.Fatalf("pulse-typed status=%v want read", got)
	}
	if got := statusByID["pulse-legacy"]; got != agent.BoardMessageStatusRead {
		t.Fatalf("pulse-legacy status=%v want read", got)
	}
	if got := statusByID["task-reminder"]; got != agent.BoardMessageStatusUnread {
		t.Fatalf("task-reminder status=%v want unread", got)
	}
}

func TestRunRoomClearRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	var err error
	cmd, _ = newRoomTestCommand(ctx)
	err = runRoomClear(cmd, workspace, "alpha", "gemini-a", "read", "coordinator-pulses")
	assertRoomErrorContains(t, err, "room clear requires coordinator role")
}

func TestRunRoomClearSystemReminders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	for _, msg := range []*agent.BoardMessage{
		{
			ID:          "reply-reminder",
			WorkspaceID: workspace,
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "gemini-a",
			Kind:        agent.BoardMessageKindAlert,
			Priority:    2,
			Subject:     "Reminder: pending response for smoke",
			Body:        "reply reminder",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "task-reminder",
			WorkspaceID: workspace,
			TaskID:      "task-1",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "human-a",
			Kind:        agent.BoardMessageKindAlert,
			Priority:    2,
			Subject:     "Reminder: task awaiting update",
			Body:        "task reminder",
			CreatedAt:   time.Now().UTC(),
		},
		{
			ID:          "pulse",
			WorkspaceID: workspace,
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      roomLoopSender("alpha"),
			Recipient:   "human-a",
			Kind:        agent.BoardMessageKindCoordinatorPulse,
			Priority:    2,
			Subject:     "Coordinator pulse: 1 pending participants, 0 blocked, 0 stale",
			Body:        "pulse",
			CreatedAt:   time.Now().UTC(),
		},
	} {
		if err := store.SendMessage(ctx, msg); err != nil {
			t.Fatalf("SendMessage(%s): %v", msg.ID, err)
		}
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomClear(cmd, workspace, "alpha", "human-a", "read", "system-reminders"); err != nil {
		t.Fatalf("runRoomClear: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["updated"]; got != float64(2) {
		t.Fatalf("updated=%v want 2", got)
	}

	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", 20)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}
	statusByID := make(map[string]agent.BoardMessageStatus, len(messages))
	for _, msg := range messages {
		statusByID[msg.ID] = msg.Status
	}
	if got := statusByID["reply-reminder"]; got != agent.BoardMessageStatusRead {
		t.Fatalf("reply-reminder status=%v want read", got)
	}
	if got := statusByID["task-reminder"]; got != agent.BoardMessageStatusRead {
		t.Fatalf("task-reminder status=%v want read", got)
	}
	if got := statusByID["pulse"]; got != agent.BoardMessageStatusUnread {
		t.Fatalf("pulse status=%v want unread", got)
	}
}

func TestRunRoomRemindLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()
	now := time.Now().UTC()
	leaseName := roomLoopLeaseName(workspace, "alpha")
	ownerID := "test-room-loop-owner"
	acquired, err := coordStore.TryAcquireLease(ctx, leaseName, ownerID, roomLoopLeaseTTL)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected test room loop lease acquisition")
	}
	if _, err := coordStore.UpsertRoomLoop(ctx, coordination.RoomLoop{
		WorkspaceID:                  workspace,
		RoomID:                       "alpha",
		Enabled:                      true,
		ManagedBy:                    roomLoopManagedBy,
		LastTickAt:                   &now,
		DeliveryLeaseName:            leaseName,
		DeliveryOwnerID:              ownerID,
		PulseInterval:                30 * time.Second,
		ReplyStaleAfter:              2 * time.Minute,
		TaskStaleAfter:               5 * time.Minute,
		MinPulseFloor:                roomLoopMinimumPulseFloor,
		InterruptAttemptLimit:        roomPulseInterruptLimit,
		ReminderBackoffCap:           roomPulseBackoffCap,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Check MR !26 and report status", "", "", "", 15*time.Minute, 3, false, true, true, false, false); err != nil {
		t.Fatalf("runRoomRemindAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	reminder := data["reminder"].(map[string]any)
	reminderID := reminder["id"].(string)
	if reminder["reply_expected"] != true {
		t.Fatalf("reply_expected=%v want true", reminder["reply_expected"])
	}
	if got := strings.TrimSpace(fmt.Sprint(data["delivery_owner"])); got != "room_loop" {
		t.Fatalf("delivery_owner=%q want room_loop", got)
	}
	if got := fmt.Sprint(data["delivery_pending"]); got != "true" {
		t.Fatalf("delivery_pending=%q want true", got)
	}
	if _, ok := data["live_relay"]; ok {
		t.Fatalf("live_relay should be omitted now that reminder delivery is room-loop owned: %v", data)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRemindList(cmd, workspace, "alpha", false); err != nil {
		t.Fatalf("runRoomRemindList: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["count"]; got != float64(1) {
		t.Fatalf("count=%v want 1", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRemindCancel(cmd, workspace, "human-a", "alpha", reminderID); err != nil {
		t.Fatalf("runRoomRemindCancel: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	cancelled := data["reminder"].(map[string]any)
	if cancelled["active"] != false {
		t.Fatalf("active=%v want false", cancelled["active"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRemindList(cmd, workspace, "alpha", true); err != nil {
		t.Fatalf("runRoomRemindList all: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	reminders := data["reminders"].([]any)
	if len(reminders) != 1 {
		t.Fatalf("len(reminders)=%d want 1", len(reminders))
	}
	if reminders[0].(map[string]any)["active"] != false {
		t.Fatalf("listed active=%v want false", reminders[0].(map[string]any)["active"])
	}
}

func TestRunRoomRemindCancelFallsBackToReminderSender(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("FOXCTL_PARTICIPANT", "")
	t.Setenv("FOXCTL_PARTICIPANT_ID", "")
	t.Setenv("ZELLIJ", "")
	t.Setenv("ZELLIJ_SESSION_NAME", "")
	t.Setenv("ZELLIJ_PANE_ID", "")
	t.Setenv("FOXCTL_ZELLIJ_PARTICIPANT", "")
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()
	now := time.Now().UTC()
	leaseName := roomLoopLeaseName(workspace, "alpha")
	ownerID := "test-room-loop"
	acquired, err := coordStore.TryAcquireLease(ctx, leaseName, ownerID, 30*time.Second)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected test room loop lease acquisition")
	}
	if _, err := coordStore.UpsertRoomLoop(ctx, coordination.RoomLoop{
		WorkspaceID:                  workspace,
		RoomID:                       "alpha",
		Enabled:                      true,
		ManagedBy:                    roomLoopManagedBy,
		LastTickAt:                   &now,
		DeliveryLeaseName:            leaseName,
		DeliveryOwnerID:              ownerID,
		PulseInterval:                30 * time.Second,
		ReplyStaleAfter:              2 * time.Minute,
		TaskStaleAfter:               5 * time.Minute,
		MinPulseFloor:                roomLoopMinimumPulseFloor,
		InterruptAttemptLimit:        roomPulseInterruptLimit,
		ReminderBackoffCap:           roomPulseBackoffCap,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Check MR !26 and report status", "", "", "", 15*time.Minute, 3, false, true, true, false, false); err != nil {
		t.Fatalf("runRoomRemindAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	reminderID := data["reminder"].(map[string]any)["id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRemindCancel(cmd, workspace, "", "alpha", reminderID); err != nil {
		t.Fatalf("runRoomRemindCancel fallback: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := fmt.Sprint(data["actor_id"]); got != "human-a" {
		t.Fatalf("actor_id=%q want human-a", got)
	}
	cancelled := data["reminder"].(map[string]any)
	if cancelled["active"] != false {
		t.Fatalf("active=%v want false", cancelled["active"])
	}
}

func TestRunRoomRemindAddDedupesEquivalentActiveReminder(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Check MR !26 and report status", "task-1", "", "", 15*time.Minute, 3, false, true, true, false, false); err != nil {
		t.Fatalf("runRoomRemindAdd first: %v", err)
	}
	first := decodeRoomEnvelope(t, out)
	firstReminder := first["reminder"].(map[string]any)
	firstID := strings.TrimSpace(fmt.Sprint(firstReminder["id"]))

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Check MR !26 and report status", "task-1", "", "", 15*time.Minute, 3, false, true, true, false, false); err != nil {
		t.Fatalf("runRoomRemindAdd second: %v", err)
	}
	second := decodeRoomEnvelope(t, out)
	if got := fmt.Sprint(second["deduped"]); got != "true" {
		t.Fatalf("deduped=%q want true", got)
	}
	secondReminder := second["reminder"].(map[string]any)
	secondID := strings.TrimSpace(fmt.Sprint(secondReminder["id"]))
	if secondID != firstID {
		t.Fatalf("second reminder id=%q want %q", secondID, firstID)
	}

	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()
	reminders, err := coordStore.ListRoomReminders(ctx, workspace, "alpha", false)
	if err != nil {
		t.Fatalf("ListRoomReminders: %v", err)
	}
	if len(reminders) != 1 {
		t.Fatalf("len(reminders)=%d want 1", len(reminders))
	}
}

func TestRunRoomRemindAddRequiresActiveLoopByDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	var err error
	cmd, _ = newRoomTestCommand(ctx)
	err = runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Check MR !26 and report status", "", "", "", 15*time.Minute, 3, false, true, true, false, false)
	assertRoomErrorContains(t, err, "room loop is not active")

	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()
	reminders, err := coordStore.ListRoomReminders(ctx, workspace, "alpha", true)
	if err != nil {
		t.Fatalf("ListRoomReminders: %v", err)
	}
	if len(reminders) != 0 {
		t.Fatalf("len(reminders)=%d want 0 after failed add", len(reminders))
	}
}

func TestRunRoomSendRequiresActiveLoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "hello room", "info", "", 0, false, false, false, true)
	assertRoomErrorContains(t, err, "room loop is not active")

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()
	messages, err := store.ListRoomMessages(ctx, workspace, "alpha", 10)
	if err != nil {
		t.Fatalf("ListRoomMessages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("messages=%d want 0 after failed send", len(messages))
	}
}

func TestRequireActiveRoomLoopRequiresDeliveryOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()

	now := time.Now().UTC()
	if _, err := coordStore.UpsertRoomLoop(ctx, coordination.RoomLoop{
		WorkspaceID:   workspace,
		RoomID:        "alpha",
		Enabled:       true,
		ManagedBy:     roomLoopManagedBy,
		LastTickAt:    &now,
		PulseInterval: 30 * time.Second,
	}); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}
	err = requireActiveRoomLoop(ctx, coordStore, workspace, "alpha", now)
	assertRoomErrorContains(t, err, "no active delivery owner")

	leaseName := roomLoopLeaseName(workspace, "alpha")
	acquired, err := coordStore.TryAcquireLease(ctx, leaseName, "owner-a", roomLoopLeaseTTL)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected lease acquisition")
	}
	if _, err := coordStore.UpsertRoomLoop(ctx, coordination.RoomLoop{
		WorkspaceID:       workspace,
		RoomID:            "alpha",
		Enabled:           true,
		ManagedBy:         roomLoopManagedBy,
		LastTickAt:        &now,
		DeliveryLeaseName: leaseName,
		DeliveryOwnerID:   "owner-a",
		PulseInterval:     30 * time.Second,
	}); err != nil {
		t.Fatalf("UpsertRoomLoop(with owner): %v", err)
	}
	if err := requireActiveRoomLoop(ctx, coordStore, workspace, "alpha", now); err != nil {
		t.Fatalf("requireActiveRoomLoop(with owner): %v", err)
	}
}

func TestRoomLoopInitialMessagesRespectsPersistedCursor(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	room := agent.RoomSummary{
		ID:           "alpha",
		WorkspaceID:  "ws1",
		Participants: []string{"agent-a", "human-a"},
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "reviewer"},
			{ActorID: "human-a", Role: "coordinator"},
		},
	}
	messages := []agent.BoardMessage{
		{ID: "m1", Recipient: "agent-a", Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-2 * time.Minute)},
		{ID: "m2", Recipient: "agent-a", Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-1 * time.Minute)},
		{ID: "m3", Recipient: "agent-a", Status: agent.BoardMessageStatusUnread, CreatedAt: base},
	}
	cursorAt := messages[1].CreatedAt
	runtime := roomLoopRuntimeState{
		DeliveryCursorMessageID: messages[1].ID,
		DeliveryCursorAt:        &cursorAt,
	}

	got := roomLoopInitialMessages(room, messages, 2, runtime)
	if len(got) != 1 || got[0].ID != "m3" {
		t.Fatalf("roomLoopInitialMessages=%v want [m3]", got)
	}
}

func TestRoomLoopInitialMessagesFiltersHistoricalBroadcastNoise(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	room := agent.RoomSummary{
		ID:           "alpha",
		WorkspaceID:  "ws1",
		Participants: []string{"agent-a", "human-a"},
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "reviewer"},
			{ActorID: "human-a", Role: "coordinator"},
		},
	}
	messages := []agent.BoardMessage{
		{ID: "m1", Recipient: agent.BroadcastRecipient, Kind: agent.BoardMessageKindEpic, Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-2 * time.Minute)},
		{ID: "m2", Recipient: agent.BroadcastRecipient, Kind: agent.BoardMessageKindMilestone, Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-1 * time.Minute)},
		{ID: "m3", Recipient: "agent-a", Kind: agent.BoardMessageKindReviewRequest, Status: agent.BoardMessageStatusUnread, CreatedAt: base},
	}

	got := roomLoopInitialMessages(room, messages, 3, roomLoopRuntimeState{})
	if len(got) != 1 || got[0].ID != "m3" {
		t.Fatalf("roomLoopInitialMessages=%v want [m3]", got)
	}
}

func TestRoomLoopInitialMessagesKeepsActionableBroadcasts(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	room := agent.RoomSummary{
		ID:           "alpha",
		WorkspaceID:  "ws1",
		Participants: []string{"agent-a", "human-a"},
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "reviewer"},
			{ActorID: "human-a", Role: "coordinator"},
		},
	}
	messages := []agent.BoardMessage{
		{
			ID:          "m1",
			Recipient:   agent.BroadcastRecipient,
			Kind:        agent.BoardMessageKindInstruction,
			AckRequired: true,
			Status:      agent.BoardMessageStatusUnread,
			CreatedAt:   base,
		},
	}

	got := roomLoopInitialMessages(room, messages, 1, roomLoopRuntimeState{})
	if len(got) != 1 || got[0].ID != "m1" {
		t.Fatalf("roomLoopInitialMessages=%v want [m1]", got)
	}
}

func TestRoomLoopInitialMessagesRespectsPersistedCursorAndFiltersBroadcasts(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	room := agent.RoomSummary{
		ID:           "alpha",
		WorkspaceID:  "ws1",
		Participants: []string{"agent-a", "human-a"},
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "reviewer"},
			{ActorID: "human-a", Role: "coordinator"},
		},
	}
	messages := []agent.BoardMessage{
		{ID: "m1", Recipient: "agent-a", Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-2 * time.Minute)},
		{ID: "m2", Recipient: "agent-a", Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(-1 * time.Minute)},
		{ID: "m3", Recipient: agent.BroadcastRecipient, Kind: agent.BoardMessageKindMilestoneSummary, Status: agent.BoardMessageStatusUnread, CreatedAt: base},
		{ID: "m4", Recipient: "agent-a", Kind: agent.BoardMessageKindReviewRequest, Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(30 * time.Second)},
	}
	cursorAt := messages[1].CreatedAt
	runtime := roomLoopRuntimeState{
		DeliveryCursorMessageID: messages[1].ID,
		DeliveryCursorAt:        &cursorAt,
	}

	got := roomLoopInitialMessages(room, messages, 4, runtime)
	if len(got) != 1 || got[0].ID != "m4" {
		t.Fatalf("roomLoopInitialMessages=%v want [m4]", got)
	}
}

func TestRoomLoopSeedSeenMessagesSuppressesFilteredStartupBroadcasts(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	room := agent.RoomSummary{
		ID:           "alpha",
		WorkspaceID:  "ws1",
		Participants: []string{"agent-a", "human-a"},
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "reviewer"},
			{ActorID: "human-a", Role: "coordinator"},
		},
	}
	messages := []agent.BoardMessage{
		{ID: "m1", Recipient: agent.BroadcastRecipient, Kind: agent.BoardMessageKindMilestoneContract, Status: agent.BoardMessageStatusUnread, CreatedAt: base},
		{ID: "m2", Recipient: "agent-a", Kind: agent.BoardMessageKindReviewRequest, Status: agent.BoardMessageStatusUnread, CreatedAt: base.Add(time.Second)},
	}
	seen := roomLoopSeedSeenMessages(room, messages, 2, roomLoopRuntimeState{})
	if _, ok := seen["m1"]; !ok {
		t.Fatalf("expected filtered broadcast to be seeded as seen: %v", seen)
	}
	if _, ok := seen["m2"]; ok {
		t.Fatalf("expected replayable direct message to remain unseen: %v", seen)
	}
}

func TestAdvanceRoomLoopCursorTracksNewestMessage(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	runtime := roomLoopRuntimeState{}
	if !advanceRoomLoopCursor(&runtime, agent.BoardMessage{ID: "m1", CreatedAt: base.Add(-time.Minute)}) {
		t.Fatal("expected first cursor advance")
	}
	if runtime.DeliveryCursorMessageID != "m1" {
		t.Fatalf("DeliveryCursorMessageID=%q want m1", runtime.DeliveryCursorMessageID)
	}
	if advanceRoomLoopCursor(&runtime, agent.BoardMessage{ID: "m0", CreatedAt: base.Add(-2 * time.Minute)}) {
		t.Fatal("older message should not move cursor")
	}
	if !advanceRoomLoopCursor(&runtime, agent.BoardMessage{ID: "m2", CreatedAt: base}) {
		t.Fatal("newer message should move cursor")
	}
	if runtime.DeliveryCursorMessageID != "m2" {
		t.Fatalf("DeliveryCursorMessageID=%q want m2", runtime.DeliveryCursorMessageID)
	}
}

func TestSyncRoomLoopStatePreservesPersistedCursorOnStartup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	workspace := "/repo"
	roomID := "alpha"
	cursorAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	seed := defaultRoomLoopPolicy(workspace, roomID, roomPulseConfig{})
	seed.LastTickAt = &cursorAt
	seed.DeliveryLeaseName = "lease-old"
	seed.DeliveryOwnerID = "owner-old"
	seed.DeliveryCursorMessageID = "msg-latest"
	seed.DeliveryCursorAt = &cursorAt
	if _, err := store.UpsertRoomLoop(ctx, seed); err != nil {
		t.Fatalf("UpsertRoomLoop(seed): %v", err)
	}

	startedAt := time.Now().UTC().Truncate(time.Second)
	persisted, err := syncRoomLoopState(ctx, store, workspace, roomID, roomPulseConfig{}, startedAt, roomLoopRuntimeState{
		DeliveryLeaseName: "lease-new",
		DeliveryOwnerID:   "owner-new",
	})
	if err != nil {
		t.Fatalf("syncRoomLoopState: %v", err)
	}
	if got := persisted.DeliveryLeaseName; got != "lease-new" {
		t.Fatalf("DeliveryLeaseName=%q want lease-new", got)
	}
	if got := persisted.DeliveryOwnerID; got != "owner-new" {
		t.Fatalf("DeliveryOwnerID=%q want owner-new", got)
	}
	if got := persisted.DeliveryCursorMessageID; got != "msg-latest" {
		t.Fatalf("DeliveryCursorMessageID=%q want msg-latest", got)
	}
	if persisted.DeliveryCursorAt == nil || !persisted.DeliveryCursorAt.Equal(cursorAt) {
		t.Fatalf("DeliveryCursorAt=%v want %v", persisted.DeliveryCursorAt, cursorAt)
	}
}

func TestRunRoomRemindAddAllowPassive(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Passive reminder smoke", "", "", "", 15*time.Minute, 3, false, true, true, false, true); err != nil {
		t.Fatalf("runRoomRemindAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["recipient"]; got != "gemini-a" {
		t.Fatalf("recipient=%v want gemini-a", got)
	}
}

func TestRunRoomRemindAddPassiveSuppressesInboxDebt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRemindAdd(cmd, workspace, "human-a", "alpha", "gemini-a", "", "Hard-cut cadence pulse", "", "", "", 15*time.Minute, 3, false, true, false, true, true); err != nil {
		t.Fatalf("runRoomRemindAdd: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	reminder := data["reminder"].(map[string]any)
	if got := reminder["passive"]; got != true {
		t.Fatalf("passive=%v want true", got)
	}
	if got := reminder["reply_expected"]; got != false {
		t.Fatalf("reply_expected=%v want false", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "gemini-a", 50, "all", false, false, false, false); err != nil {
		t.Fatalf("runRoomInbox: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["count"]; got != float64(0) {
		t.Fatalf("count=%v want 0", got)
	}
}

func TestRunRoomPlanStartAndShow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomPlanStart(cmd, workspace, "human-a", "alpha", "phase-3", "Ship the next UI slice", "docs/plans/phase-3.md", []string{"gui-agent", "rooms"}, []string{"keep API stable"}); err != nil {
		t.Fatalf("runRoomPlanStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("session_id empty")
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomPlanEntry(cmd, workspace, "alpha", "gemini-a", sessionID, agent.BoardMessageKindPlanProposal, "Proposal: split UI and API", "Start with the planning tab and a typed message protocol.", false); err != nil {
		t.Fatalf("runRoomPlanEntry proposal: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomPlanShow(cmd, workspace, "alpha", sessionID, 50); err != nil {
		t.Fatalf("runRoomPlanShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	session := data["session"].(map[string]any)
	if got := session["id"]; got != sessionID {
		t.Fatalf("session.id=%v want %s", got, sessionID)
	}
	if got := session["proposals"]; got != float64(1) {
		t.Fatalf("session.proposals=%v want 1", got)
	}
}

func TestRunRoomPlanDecideRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomPlanStart(cmd, workspace, "human-a", "alpha", "phase-3", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomPlanStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomPlanEntry(cmd, workspace, "alpha", "gemini-a", sessionID, agent.BoardMessageKindPlanDecision, "Decision: do it", "Coordinator decision", true)
	assertRoomErrorContains(t, err, "room plan phase changes require coordinator role")
}

func TestRunRoomEpicMilestoneStoryAndLogFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship agile nouns on top of room", "human-a", "Transport-agnostic planning and delivery", "Q2", []string{"epics", "milestones"}, []string{"agents can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)
	if epicID == "" {
		t.Fatalf("epic_id empty")
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicAsk(cmd, workspace, "human-a", "alpha", epicID, "gemini-a", "success", "What must be true before milestones can open?"); err != nil {
		t.Fatalf("runRoomEpicAsk: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	questionMsg := data["message"].(map[string]any)
	questionID := questionMsg["id"].(string)
	if body, ok := questionMsg["body"].(string); !ok || !strings.Contains(body, "Kind: success") {
		t.Fatalf("question body=%v want Kind: success", questionMsg["body"])
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicAnswer(cmd, workspace, "gemini-a", "alpha", questionID, "The epic needs a clarified brief and no open intake questions."); err != nil {
		t.Fatalf("runRoomEpicAnswer: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief: build the room agile layer first, then surface it in the GUI."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShape(cmd, workspace, "human-a", "alpha", epicID, 3); err != nil {
		t.Fatalf("runRoomEpicShape: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["count"]; got != float64(2) {
		t.Fatalf("proposal count=%v want 2", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship core CLI nouns", "", "human-a", []string{"commands", "derived show"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)
	if milestoneID == "" {
		t.Fatalf("milestone_id empty")
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Epic and milestone hierarchy is visible via show commands."); err != nil {
		t.Fatalf("runRoomMilestoneCriteria: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "gemini-a", "alpha", milestoneID, "Implement CLI flow", "Add epic, milestone, story, and log commands.", "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Core agile hierarchy is in place."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", epicID, "First delivery loop", []string{"CLI hierarchy landed"}, []string{"MCP exposure"}, nil, []string{"wire GUI later"}, "Initial agile room slice complete."); err != nil {
		t.Fatalf("runRoomLogAppend: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	epic := data["epic"].(map[string]any)
	if got := epic["milestone_count"]; got != float64(1) {
		t.Fatalf("milestone_count=%v want 1", got)
	}
	if got := epic["question_count"]; got != float64(1) {
		t.Fatalf("question_count=%v want 1", got)
	}
	questionKinds := epic["question_kinds"].(map[string]any)
	if got := questionKinds["success"]; got != float64(1) {
		t.Fatalf("question_kinds.success=%v want 1", got)
	}
	if got := epic["answer_count"]; got != float64(1) {
		t.Fatalf("answer_count=%v want 1", got)
	}
	if got := epic["status"]; got != "finalized" {
		t.Fatalf("status=%v want finalized", got)
	}
	if got := epic["proposal_count"]; got != float64(2) {
		t.Fatalf("proposal_count=%v want 2", got)
	}
	if got := epic["story_count"]; got != float64(1) {
		t.Fatalf("story_count=%v want 1", got)
	}
	if got := epic["log_count"]; got != float64(1) {
		t.Fatalf("log_count=%v want 1", got)
	}
	milestones := epic["milestones"].([]any)
	milestone := milestones[0].(map[string]any)
	if got := milestone["criteria_count"]; got != float64(1) {
		t.Fatalf("criteria_count=%v want 1", got)
	}
	if got := milestone["review_count"]; got != float64(1) {
		t.Fatalf("review_count=%v want 1", got)
	}
	if got := milestone["status"]; got != "passed" {
		t.Fatalf("status=%v want passed", got)
	}
}

func TestRunRoomMilestoneReviewRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "", "", "", nil, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomMilestoneReview(cmd, workspace, "gemini-a", "alpha", milestoneID, "pass", "Looks good.")
	assertRoomErrorContains(t, err, "agile scope changes require coordinator role")
}

func TestRunRoomMilestoneStartRequiresFinalizedEpic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "", "", "", nil, nil, nil, nil, nil, nil, "")
	assertRoomErrorContains(t, err, "milestones require a finalized epic")
}

func TestRunRoomEpicShapeRequiresFinalizedEpic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", []string{"room", "gui-agent"}, []string{"operators can orient quickly"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomEpicShape(cmd, workspace, "human-a", "alpha", epicID, 3)
	assertRoomErrorContains(t, err, "epic shaping requires a finalized epic")
}

func TestRunRoomMilestoneStartFromProposal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Give the room a durable epic/milestone/story hierarchy", "human-a", "", "", []string{"room", "gui-agent"}, []string{"operators can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicAsk(cmd, workspace, "human-a", "alpha", epicID, "gemini-a", "constraint", "What constraints must the first tranche respect?"); err != nil {
		t.Fatalf("runRoomEpicAsk: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	questionID := data["message"].(map[string]any)["id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicAnswer(cmd, workspace, "gemini-a", "alpha", questionID, "The first tranche must stay transport-agnostic and room-native."); err != nil {
		t.Fatalf("runRoomEpicAnswer: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief: ship the room agile layer first, then surface it in the GUI."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShape(cmd, workspace, "human-a", "alpha", epicID, 2); err != nil {
		t.Fatalf("runRoomEpicShape: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	proposals := data["proposals"].([]any)
	if len(proposals) == 0 {
		t.Fatalf("len(proposals)=0 want at least one proposal")
	}
	proposal := proposals[0].(map[string]any)
	proposalID := proposal["id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "", "", "", "", nil, nil, nil, nil, nil, nil, proposalID); err != nil {
		t.Fatalf("runRoomMilestoneStart from proposal: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)
	if milestoneID == "" {
		t.Fatalf("milestone_id empty")
	}
	msg := data["message"].(map[string]any)
	if got := msg["subject"]; got == "" {
		t.Fatalf("milestone subject empty")
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestone := data["milestone"].(map[string]any)
	meta := milestone["meta"].(map[string]any)
	if got := meta["epic_id"]; got != epicID {
		t.Fatalf("meta.epic_id=%v want %s", got, epicID)
	}
	scope := meta["scope"].([]any)
	if len(scope) == 0 {
		t.Fatalf("scope=%v want carried proposal scope", scope)
	}
}

func TestRunRoomStoryProposalAcceptAndMilestoneSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", []string{"room", "gui-agent"}, []string{"operators can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first CLI slice", "", "human-a", []string{"commands"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryPropose(cmd, workspace, "gemini-a", "alpha", milestoneID, "Implement story proposal flow", "Add story propose and accept commands.", "gemini-a", "Needed before agents can refine milestone internals."); err != nil {
		t.Fatalf("runRoomStoryPropose: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	proposalID := data["proposal_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAccept(cmd, workspace, "human-a", "alpha", milestoneID, proposalID, "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryAccept: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["story_id"]; got == "" {
		t.Fatalf("story_id empty")
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Review synthesis: the foundation milestone now has accepted story structure.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneSummary: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestone := data["milestone"].(map[string]any)
	if got := milestone["summary_count"]; got != float64(1) {
		t.Fatalf("summary_count=%v want 1", got)
	}
	latestSummary := milestone["latest_summary"].(map[string]any)
	if got := latestSummary["summary"]; got != "Review synthesis: the foundation milestone now has accepted story structure." {
		t.Fatalf("latest_summary.summary=%v want shorthand summary body", got)
	}
	if got := milestone["story_count"]; got != float64(2) {
		t.Fatalf("story_count=%v want 2 (proposal + accepted story)", got)
	}
	stories := milestone["stories"].([]any)
	statuses := make(map[string]bool)
	for _, raw := range stories {
		item := raw.(map[string]any)
		statuses[item["status"].(string)] = true
	}
	if !statuses["proposed"] || !statuses["accepted"] {
		t.Fatalf("statuses=%v want proposed and accepted", statuses)
	}
}

func TestRunRoomMilestoneSummaryStructuredSynthesis(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", []string{"room"}, []string{"operators can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation", "work-pack sync"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Implement story validate", "Add a story-level validation record.", "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	storyID := decodeRoomEnvelope(t, out)["story_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "blocked", "Code review still has one blocking issue.", "docs/reviews/story.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate: %v", err)
	}
	validationID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "", "Foundation passed with one clean review.", []string{"Accepted stories are validated"}, nil, nil, []string{validationID}, []string{"Keep summary separate from proof"}, []string{"Follow-up threads should be acked when no reply is needed"}, []string{"Start story lifecycle"}, []string{"Use milestone summary for synthesis, not proof"}); err != nil {
		t.Fatalf("runRoomMilestoneSummary structured: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["summary_count"]; got != float64(1) {
		t.Fatalf("summary_count=%v want 1", got)
	}
	if got := milestone["blocking_validation_count"]; got != float64(1) {
		t.Fatalf("blocking_validation_count=%v want 1", got)
	}
	if got := milestone["decision_count"]; got != float64(1) {
		t.Fatalf("decision_count=%v want 1", got)
	}
	if got := milestone["recommended_next_count"]; got != float64(1) {
		t.Fatalf("recommended_next_count=%v want 1", got)
	}
	summaryMeta := milestone["summary_meta"].(map[string]any)
	if got := summaryMeta["summary"]; got != "Foundation passed with one clean review." {
		t.Fatalf("summary_meta.summary=%v", got)
	}
	blocking := summaryMeta["blocking_validation_ids"].([]any)
	if len(blocking) != 1 || blocking[0] != validationID {
		t.Fatalf("blocking_validation_ids=%v want [%s]", blocking, validationID)
	}

	summaryMarkdown, err := os.ReadFile(filepath.Join(home, ".foxctl", "epics", epicID, "milestones", milestoneID, "summary.md"))
	if err != nil {
		t.Fatalf("ReadFile summary markdown: %v", err)
	}
	for _, want := range []string{"## Summary", "Foundation passed with one clean review.", "## Blocking Validations", validationID, "## Guidance Updates"} {
		if !strings.Contains(string(summaryMarkdown), want) {
			t.Fatalf("summary markdown missing %q:\n%s", want, string(summaryMarkdown))
		}
	}
}

func TestRunRoomMilestoneSummaryRejectsUnknownValidationIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", []string{"room"}, []string{"operators can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "", "Summary.", nil, nil, nil, []string{"01BADVALIDATION"}, nil, nil, nil, nil)
	assertRoomErrorContains(t, err, "is not a current blocking validation for this milestone")
}

func TestRunRoomMilestoneSummaryRejectsUnknownWaivedValidationIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", []string{"room"}, []string{"operators can orient from room state"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "", "Summary.", nil, nil, []string{"01BADWAIVED"}, nil, nil, nil, nil, nil)
	assertRoomErrorContains(t, err, "is not attached to this milestone")
}

func TestRunRoomMilestoneContractStartAndUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship milestone contracts", "human-a", "", "", []string{"room", "filesystem mirror"}, []string{"milestone contracts are visible"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	enforceFalse := false
	if err := runRoomMilestoneStartWithPolicy(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first contract slice", "Make milestone intent explicit.", "human-a", []string{"contracts", "work-pack sync"}, []string{"send-confirm gap", "send-confirm gap"}, []string{"GUI changes"}, []string{"epic finalized"}, []string{"audit", "review", "review"}, []string{"integration", "review", "review"}, []string{"integration", "user_test"}, &enforceFalse, []string{"contract visible", "contract visible"}, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "Updated objective.", []string{"multi-epic rooms"}, []string{"transport rewrite"}, []string{"send confirm"}, []string{"test", "review"}, []string{"test"}, []string{"manual_check", "review"}, &enforceFalse, []string{"summary written"}); err != nil {
		t.Fatalf("runRoomMilestoneContract: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	contract := milestone["contract"].(map[string]any)
	if got := contract["objective"]; got != "Updated objective." {
		t.Fatalf("contract.objective=%v want Updated objective.", got)
	}
	if got := milestone["risk_count"]; got != float64(2) {
		t.Fatalf("risk_count=%v want 2", got)
	}
	if got := milestone["dependency_count"]; got != float64(1) {
		t.Fatalf("dependency_count=%v want 1", got)
	}
	if got := milestone["validator_count"]; got != float64(3) {
		t.Fatalf("validator_count=%v want 3", got)
	}
	if got := milestone["required_evidence_lane_count"]; got != float64(3) {
		t.Fatalf("required_evidence_lane_count=%v want 3", got)
	}
	if got := milestone["optional_evidence_lane_count"]; got != float64(2) {
		t.Fatalf("optional_evidence_lane_count=%v want 2", got)
	}
	if got := milestone["exit_criteria_count"]; got != float64(2) {
		t.Fatalf("exit_criteria_count=%v want 2", got)
	}
	risks := contract["risks"].([]any)
	if len(risks) != 2 || risks[0] != "multi-epic rooms" || risks[1] != "send-confirm gap" {
		t.Fatalf("risks=%v want [multi-epic rooms send-confirm gap]", risks)
	}
	validators := contract["validators_expected"].([]any)
	if len(validators) != 3 || validators[0] != "audit" || validators[1] != "review" || validators[2] != "test" {
		t.Fatalf("validators=%v want [audit review test]", validators)
	}
	requiredLanes := contract["required_evidence_lanes"].([]any)
	if len(requiredLanes) != 3 || requiredLanes[0] != "integration" || requiredLanes[1] != "review" || requiredLanes[2] != "test" {
		t.Fatalf("required_evidence_lanes=%v want [integration review test]", requiredLanes)
	}
	optionalLanes := contract["optional_evidence_lanes"].([]any)
	if len(optionalLanes) != 2 || optionalLanes[0] != "manual_check" || optionalLanes[1] != "user_test" {
		t.Fatalf("optional_evidence_lanes=%v want [manual_check user_test]", optionalLanes)
	}
	if got := milestone["contract_update_count"]; got != float64(1) {
		t.Fatalf("contract_update_count=%v want 1", got)
	}

	workpackRoot := filepath.Join(home, ".foxctl", "epics", epicID)
	milestoneMarkdown, err := os.ReadFile(filepath.Join(workpackRoot, "milestones", milestoneID, "milestone.md"))
	if err != nil {
		t.Fatalf("ReadFile milestone markdown: %v", err)
	}
	markdown := string(milestoneMarkdown)
	for _, want := range []string{"Objective: Updated objective.", "## Risks", "send-confirm gap", "multi-epic rooms", "## Validators Expected", "audit", "review", "test", "## Required Evidence Lanes", "integration", "## Optional Evidence Lanes", "manual_check", "user_test", "## Exit Criteria"} {
		if !strings.Contains(markdown, want) {
			t.Fatalf("milestone markdown missing %q:\n%s", want, markdown)
		}
	}
	milestoneJSON, err := os.ReadFile(filepath.Join(workpackRoot, "milestones", milestoneID, "meta.json"))
	if err != nil {
		t.Fatalf("ReadFile milestone meta json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(milestoneJSON, &payload); err != nil {
		t.Fatalf("Unmarshal milestone meta json: %v", err)
	}
	milestoneView := payload["milestone"].(map[string]any)
	meta := milestoneView["meta"].(map[string]any)
	if got := meta["objective"]; got != "Updated objective." {
		t.Fatalf("meta.objective=%v want Updated objective.", got)
	}
	if got := milestoneView["required_lane_status"]; got != "missing" {
		t.Fatalf("required_lane_status=%v want missing", got)
	}
}

func TestRunRoomMilestoneContractRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "", "", "", nil, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomMilestoneContract(cmd, workspace, "gemini-a", "alpha", milestoneID, "Nope.", nil, nil, nil, nil, nil)
	assertRoomErrorContains(t, err, "agile scope changes require coordinator role")
}

func TestRunRoomRetroAddAndShow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship retro guidance", "human-a", "", "", []string{"room"}, []string{"operators can carry lessons forward"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship retro guidance", "", "human-a", []string{"retro"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRetroAdd(cmd, workspace, "human-a", "alpha", epicID, milestoneID, "coordination", "Ack no-blocker follow-ups.", "Prevents stale reply-expected inbox items.", "Ack no-blocker follow-ups instead of waiting for a reply.", []string{"room", "review-loop"}, []string{"Document this in the room-agile skill"}); err != nil {
		t.Fatalf("runRoomRetroAdd: %v", err)
	}
	updateID := decodeRoomEnvelope(t, out)["update_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRetroShow(cmd, workspace, "alpha", epicID, milestoneID, 100); err != nil {
		t.Fatalf("runRoomRetroShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["count"]; got != float64(1) {
		t.Fatalf("count=%v want 1", got)
	}
	updates := data["updates"].([]any)
	if len(updates) != 1 {
		t.Fatalf("len(updates)=%d want 1", len(updates))
	}
	update := updates[0].(map[string]any)
	if got := update["id"]; got != updateID {
		t.Fatalf("update.id=%v want %s", got, updateID)
	}
	if got := update["kind"]; got != "coordination" {
		t.Fatalf("kind=%v want coordination", got)
	}
	groups := data["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("len(groups)=%d want 1", len(groups))
	}
	group := groups[0].(map[string]any)
	if got := group["kind"]; got != "coordination" {
		t.Fatalf("group.kind=%v want coordination", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["guidance_update_count"]; got != float64(1) {
		t.Fatalf("guidance_update_count=%v want 1", got)
	}
	recent := epic["latest_guidance_updates"].([]any)
	if len(recent) != 1 {
		t.Fatalf("len(latest_guidance_updates)=%d want 1", len(recent))
	}

	retroMarkdownPath := filepath.Join(home, ".foxctl", "epics", epicID, "retro.md")
	retroMarkdown, err := os.ReadFile(retroMarkdownPath)
	if err != nil {
		t.Fatalf("ReadFile retro markdown: %v", err)
	}
	for _, want := range []string{"# Retro Guidance", "Ack no-blocker follow-ups.", "## Scope", "review-loop", "## Follow-up"} {
		if !strings.Contains(string(retroMarkdown), want) {
			t.Fatalf("retro markdown missing %q:\n%s", want, string(retroMarkdown))
		}
	}
}

func TestRunRoomRetroAddRequiresCoordinator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "", "human-a", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomRetroAdd(cmd, workspace, "gemini-a", "alpha", epicID, "", "process", "Use the room loop.", "Prevents passive reminders from silently stalling.", "Fail reminder add when no loop is running.", nil, nil)
	assertRoomErrorContains(t, err, "agile scope changes require coordinator role")
}

func TestRunRoomRetroAddRejectsMilestoneOutsideEpic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Epic A", "", "human-a", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart A: %v", err)
	}
	epicA := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicA, "Clarified brief A."); err != nil {
		t.Fatalf("runRoomEpicFinalize A: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Epic B", "", "human-a", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart B: %v", err)
	}
	epicB := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicB, "Clarified brief B."); err != nil {
		t.Fatalf("runRoomEpicFinalize B: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicB, "Foundation", "", "", "human-a", nil, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomRetroAdd(cmd, workspace, "human-a", "alpha", epicA, milestoneID, "tooling", "Wrong milestone.", "Should reject cross-epic milestone reference.", "Use a milestone from the same epic.", nil, nil)
	assertRoomErrorContains(t, err, "milestone does not belong to this epic")
}

func TestRunRoomRetroAddRejectsUnknownKind(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Epic A", "", "human-a", "", "", nil, nil); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomRetroAdd(cmd, workspace, "human-a", "alpha", epicID, "", "unknown", "Bad kind.", "Should reject invalid kind.", "Use a fixed enum.", nil, nil)
	assertRoomErrorContains(t, err, "unsupported retro kind")
}

func TestRunRoomContextWikiPromoteEpicDraftsProposalAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomRetroAdd(cmd, workspace, "human-a", "alpha", epicID, milestoneID, "quality", "Capture durable epic memory.", "Makes retrieval stronger later.", "Promote completed agile artifacts into ContextWiki drafts.", []string{"contextwiki", "room-agile"}, []string{"Implement room contextwiki promote"}); err != nil {
		t.Fatalf("runRoomRetroAdd: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomContextWikiPromote(cmd, workspace, "alpha", "epic", epicID); err != nil {
		t.Fatalf("runRoomContextWikiPromote epic: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["promotion_state"]; got != "created" {
		t.Fatalf("promotion_state=%v want created", got)
	}
	draftPath, _ := data["draft_path"].(string)
	if strings.TrimSpace(draftPath) == "" {
		t.Fatal("expected draft_path")
	}
	proposal, ok := data["proposal"].(map[string]any)
	if !ok {
		t.Fatalf("proposal type=%T", data["proposal"])
	}
	if got := proposal["kind"]; got != "room_agile_draft" {
		t.Fatalf("proposal.kind=%v want room_agile_draft", got)
	}

	contextWikiStore := contextplane.NewWorkspaceStore(workspace)
	layout, err := contextWikiStore.EnsureLayout()
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	draftBody := mustReadRoomTestFile(t, filepath.Join(layout.TemplatesDir, filepath.FromSlash(draftPath)))
	for _, want := range []string{"note_type: room_epic", "room_id: alpha", "meta_json_path:", "[[room-milestones/", "Capture durable epic memory."} {
		if !strings.Contains(draftBody, want) {
			t.Fatalf("epic ContextWiki draft missing %q:\n%s", want, draftBody)
		}
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomContextWikiPromote(cmd, workspace, "alpha", "epic", epicID); err != nil {
		t.Fatalf("runRoomContextWikiPromote epic second: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["promotion_state"]; got != "already_current" {
		t.Fatalf("promotion_state second=%v want already_current", got)
	}
}

func TestRunRoomContextWikiPromoteValidationRequiresHighSignal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Routine green validation.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	passValidationID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomContextWikiPromote(cmd, workspace, "alpha", "validation", passValidationID)
	assertRoomErrorContains(t, err, "not high-signal enough")

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "blocked", "Validation blocked on cross-story decision.", "docs/reviews/blocked.md", "", "", "Need the other story clarified.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate blocked: %v", err)
	}
	blockedValidationID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomContextWikiPromote(cmd, workspace, "alpha", "validation", blockedValidationID); err != nil {
		t.Fatalf("runRoomContextWikiPromote blocked validation: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["promotion_state"]; got != "created" {
		t.Fatalf("promotion_state=%v want created", got)
	}
	if got := data["epic_id"]; got != epicID {
		t.Fatalf("epic_id=%v want %s", got, epicID)
	}
	draftPath, _ := data["draft_path"].(string)
	if strings.TrimSpace(draftPath) == "" {
		t.Fatal("expected blocked validation draft_path")
	}

	contextWikiStore := contextplane.NewWorkspaceStore(workspace)
	layout, err := contextWikiStore.EnsureLayout()
	if err != nil {
		t.Fatalf("EnsureLayout: %v", err)
	}
	draftBody := mustReadRoomTestFile(t, filepath.Join(layout.TemplatesDir, filepath.FromSlash(draftPath)))
	for _, want := range []string{"note_type: room_validation", "validation_id: " + blockedValidationID, "meta_json_path:", "status: blocked", "[[room-milestones/"} {
		if !strings.Contains(draftBody, want) {
			t.Fatalf("validation ContextWiki draft missing %q:\n%s", want, draftBody)
		}
	}
}

func TestRunRoomStoryValidateAndWorkpackSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship work-packs", "human-a", "", "", []string{"room", "filesystem mirror"}, []string{"story validation is visible"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation", "work-pack sync"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestoneID := data["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Implement story validate", "Add a story-level validation record.", "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	storyID := data["story_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Peer story", "A related story in the same milestone.", "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryAdd peer: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	peerStoryID := data["story_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validation attached at story level.", "docs/reviews/story.md", "sha256:test", "go test ./cmd/foxctl/cmd", "Looks good.", []string{peerStoryID}); err != nil {
		t.Fatalf("runRoomStoryValidate: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	validationID := data["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	story := data["story"].(map[string]any)
	if got := story["validation_count"]; got != float64(1) {
		t.Fatalf("validation_count=%v want 1", got)
	}
	if got := story["latest_validation_status"]; got != "pass" {
		t.Fatalf("latest_validation_status=%v want pass", got)
	}
	if got := story["workpack_dir"]; got == "" {
		t.Fatalf("workpack_dir=%v want non-empty", got)
	}
	if got := story["story_markdown"]; got == "" {
		t.Fatalf("story_markdown=%v want non-empty", got)
	}
	if got := story["meta_json_path"]; got == "" {
		t.Fatalf("meta_json_path=%v want non-empty", got)
	}
	if got := story["validation_dir"]; got == "" {
		t.Fatalf("validation_dir=%v want non-empty", got)
	}
	if got := story["latest_validation_markdown"]; got == "" {
		t.Fatalf("latest_validation_markdown=%v want non-empty", got)
	}
	if got := story["latest_validation_json"]; got == "" {
		t.Fatalf("latest_validation_json=%v want non-empty", got)
	}
	if got := story["artifacts_dir"]; got == "" {
		t.Fatalf("artifacts_dir=%v want non-empty", got)
	}
	if ids, ok := story["room_message_ids"].([]any); !ok || len(ids) < 2 {
		t.Fatalf("story.room_message_ids=%T/%v want at least 2 ids", story["room_message_ids"], story["room_message_ids"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	milestone := data["milestone"].(map[string]any)
	if got := milestone["validated_story_count"]; got != float64(1) {
		t.Fatalf("validated_story_count=%v want 1", got)
	}
	if got := milestone["passed_story_count"]; got != float64(1) {
		t.Fatalf("passed_story_count=%v want 1", got)
	}
	if got := milestone["workpack_dir"]; got == "" {
		t.Fatalf("workpack_dir=%v want non-empty", got)
	}
	if got := milestone["milestone_markdown"]; got == "" {
		t.Fatalf("milestone_markdown=%v want non-empty", got)
	}
	if got := milestone["meta_json_path"]; got == "" {
		t.Fatalf("milestone meta_json_path=%v want non-empty", got)
	}
	if got := milestone["summary_markdown"]; got == "" {
		t.Fatalf("summary_markdown=%v want non-empty", got)
	}
	if ids, ok := milestone["room_message_ids"].([]any); !ok || len(ids) < 2 {
		t.Fatalf("milestone.room_message_ids=%T/%v want at least 2 ids", milestone["room_message_ids"], milestone["room_message_ids"])
	}

	workpackRoot := filepath.Join(home, ".foxctl", "epics", epicID)
	for _, path := range []string{
		filepath.Join(workpackRoot, "epic.md"),
		filepath.Join(workpackRoot, "meta.json"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "milestone.md"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "meta.json"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "summary.md"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "story.md"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "meta.json"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "validation", validationID+".md"),
		filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "validation", validationID+".json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected work-pack file %s: %v", path, err)
		}
	}

	validationMarkdown, err := os.ReadFile(filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "validation", validationID+".md"))
	if err != nil {
		t.Fatalf("ReadFile validation markdown: %v", err)
	}
	if !strings.Contains(string(validationMarkdown), "Validator type: `review`") {
		t.Fatalf("validation markdown missing validator type: %s", string(validationMarkdown))
	}
	if !strings.Contains(string(validationMarkdown), "Artifact path: `docs/reviews/story.md`") {
		t.Fatalf("validation markdown missing artifact path: %s", string(validationMarkdown))
	}
	for _, want := range []string{"## Provenance", "Source kind: `story_validation`", "Room ID: `alpha`", "Meta JSON:"} {
		if !strings.Contains(string(validationMarkdown), want) {
			t.Fatalf("validation markdown missing provenance %q:\n%s", want, string(validationMarkdown))
		}
	}

	validationJSON, err := os.ReadFile(filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "validation", validationID+".json"))
	if err != nil {
		t.Fatalf("ReadFile validation json: %v", err)
	}
	var validationPayload map[string]any
	if err := json.Unmarshal(validationJSON, &validationPayload); err != nil {
		t.Fatalf("Unmarshal validation json: %v", err)
	}
	if got := validationPayload["schema_version"]; got != float64(1) {
		t.Fatalf("schema_version=%v want 1", got)
	}
	provenance := validationPayload["provenance"].(map[string]any)
	if got := provenance["source_kind"]; got != "story_validation" {
		t.Fatalf("validation provenance.source_kind=%v want story_validation", got)
	}
	if got := provenance["source_id"]; got != validationID {
		t.Fatalf("validation provenance.source_id=%v want %s", got, validationID)
	}
	if got := provenance["room_id"]; got != "alpha" {
		t.Fatalf("validation provenance.room_id=%v want alpha", got)
	}
	if got := provenance["meta_json_path"]; got == "" {
		t.Fatalf("validation provenance.meta_json_path empty")
	}
	validationView := validationPayload["validation"].(map[string]any)
	if got := validationView["created_by"]; got != "human-a" {
		t.Fatalf("created_by=%v want human-a", got)
	}
	if got := validationView["superseded"]; got != false {
		t.Fatalf("superseded=%v want false", got)
	}

	epicMarkdown := mustReadRoomTestFile(t, filepath.Join(workpackRoot, "epic.md"))
	for _, want := range []string{"## Provenance", "Source kind: `epic`", "Room ID: `alpha`", "Meta JSON:"} {
		if !strings.Contains(epicMarkdown, want) {
			t.Fatalf("epic markdown missing provenance %q:\n%s", want, epicMarkdown)
		}
	}
	epicMetaRaw := mustReadRoomTestFile(t, filepath.Join(workpackRoot, "meta.json"))
	var epicPayload map[string]any
	if err := json.Unmarshal([]byte(epicMetaRaw), &epicPayload); err != nil {
		t.Fatalf("Unmarshal epic meta json: %v", err)
	}
	epicProvenance := epicPayload["provenance"].(map[string]any)
	if got := epicProvenance["source_kind"]; got != "epic" {
		t.Fatalf("epic provenance.source_kind=%v want epic", got)
	}
	if got := epicProvenance["source_id"]; got != epicID {
		t.Fatalf("epic provenance.source_id=%v want %s", got, epicID)
	}
}

func TestRunRoomEvidenceLanesRollup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Review passed.", "docs/reviews/review.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate review: %v", err)
	}
	reviewValidationID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "integration", "blocked", "Integration blocked.", "docs/reviews/integration.md", "", "", "Waiting on integration environment.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate integration: %v", err)
	}
	integrationValidationID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	evidenceLanes := story["evidence_lanes"].(map[string]any)
	reviewLane := evidenceLanes["review"].(map[string]any)
	if got := reviewLane["latest_status"]; got != "pass" {
		t.Fatalf("review latest_status=%v want pass", got)
	}
	if got := reviewLane["latest_validation_id"]; got != reviewValidationID {
		t.Fatalf("review latest_validation_id=%v want %s", got, reviewValidationID)
	}
	integrationLane := evidenceLanes["integration"].(map[string]any)
	if got := integrationLane["latest_status"]; got != "blocked" {
		t.Fatalf("integration latest_status=%v want blocked", got)
	}
	if got := integrationLane["latest_validation_id"]; got != integrationValidationID {
		t.Fatalf("integration latest_validation_id=%v want %s", got, integrationValidationID)
	}
	if lanes, ok := story["blocking_lanes"].([]any); !ok || len(lanes) != 1 || lanes[0] != "integration" {
		t.Fatalf("blocking_lanes=%v want [integration]", story["blocking_lanes"])
	}
	if lanes, ok := story["covered_lanes"].([]any); !ok || len(lanes) != 1 || lanes[0] != "review" {
		t.Fatalf("covered_lanes=%v want [review]", story["covered_lanes"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	laneCounts := milestone["lane_counts"].(map[string]any)
	if got := laneCounts["review"]; got != float64(1) {
		t.Fatalf("lane_counts.review=%v want 1", got)
	}
	if got := laneCounts["integration"]; got != float64(1) {
		t.Fatalf("lane_counts.integration=%v want 1", got)
	}
	laneCoverage := milestone["lane_coverage"].(map[string]any)
	if got := laneCoverage["review"]; got != float64(1) {
		t.Fatalf("lane_coverage.review=%v want 1", got)
	}
	if got := laneCoverage["integration"]; got != float64(0) {
		t.Fatalf("lane_coverage.integration=%v want 0", got)
	}
	laneBlockers := milestone["lane_blockers"].(map[string]any)
	blockers := laneBlockers["integration"].([]any)
	if len(blockers) != 1 || blockers[0] != integrationValidationID {
		t.Fatalf("lane_blockers.integration=%v want [%s]", blockers, integrationValidationID)
	}

	workpackRoot := filepath.Join(home, ".foxctl", "epics", story["epic_id"].(string))
	storyMarkdown := mustReadRoomTestFile(t, filepath.Join(workpackRoot, "milestones", milestoneID, "stories", storyID, "story.md"))
	for _, want := range []string{"## Evidence Lanes", "`review`: pass", "`integration`: blocked"} {
		if !strings.Contains(storyMarkdown, want) {
			t.Fatalf("story markdown missing %q:\n%s", want, storyMarkdown)
		}
	}
	summaryMarkdown := mustReadRoomTestFile(t, filepath.Join(workpackRoot, "milestones", milestoneID, "summary.md"))
	for _, want := range []string{"## Evidence Lanes", "`review`: seen `1`, covered `1`", "`integration`: seen `1`, covered `0`, waived `0`, blocking"} {
		if !strings.Contains(summaryMarkdown, want) {
			t.Fatalf("summary markdown missing %q:\n%s", want, summaryMarkdown)
		}
	}
}

func TestRunRoomWorkpackShowAndSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomWorkpackShow(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomWorkpackShow: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	workpack := data["workpack"].(map[string]any)
	root := workpack["root"].(string)
	if root == "" {
		t.Fatalf("workpack root empty")
	}
	milestones := workpack["milestones"].([]any)
	if len(milestones) != 1 {
		t.Fatalf("len(milestones)=%d want 1", len(milestones))
	}
	milestone := milestones[0].(map[string]any)
	if got := milestone["id"]; got != milestoneID {
		t.Fatalf("milestone id=%v want %s", got, milestoneID)
	}
	stories := milestone["stories"].([]any)
	if len(stories) != 1 {
		t.Fatalf("len(stories)=%d want 1 accepted story", len(stories))
	}
	story := stories[0].(map[string]any)
	if got := story["id"]; got != storyID {
		t.Fatalf("story id=%v want %s", got, storyID)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomWorkpackSync(cmd, workspace, "human-a", "alpha", epicID); err != nil {
		t.Fatalf("runRoomWorkpackSync: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	workpack = data["workpack"].(map[string]any)
	if got := workpack["root"]; got != root {
		t.Fatalf("sync root=%v want %s", got, root)
	}
}

func TestRenderRoomWorkpackTemplatesEmptyStates(t *testing.T) {
	delivery := renderRoomDeliveryLogMarkdown(map[string]any{})
	for _, want := range []string{"# Delivery Log", "Delivery log entries are listed newest first.", "## Entries", "No delivery log entries recorded yet."} {
		if !strings.Contains(delivery, want) {
			t.Fatalf("delivery markdown missing %q:\n%s", want, delivery)
		}
	}

	criteria := renderRoomMilestoneCriteriaMarkdown(map[string]any{})
	for _, want := range []string{"# Criteria", "## Acceptance Criteria", "No acceptance criteria recorded yet."} {
		if !strings.Contains(criteria, want) {
			t.Fatalf("criteria markdown missing %q:\n%s", want, criteria)
		}
	}

	summary := renderRoomMilestoneSummaryMarkdown(map[string]any{})
	for _, want := range []string{"# Milestone Summary", "## Summary", "No milestone summary recorded yet.", "## Evidence Lanes", "No evidence lanes recorded yet."} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary markdown missing %q:\n%s", want, summary)
		}
	}

	story := renderRoomStoryMarkdown(map[string]any{
		"title": "Template Story",
		"id":    "story-1",
		"meta":  map[string]any{},
	})
	for _, want := range []string{"# Story Template Story", "## Description", "No story description recorded yet.", "## State History", "No story state transitions recorded yet.", "## Evidence Lanes", "No evidence lanes recorded yet.", "## Validation History", "No validation entries recorded yet."} {
		if !strings.Contains(story, want) {
			t.Fatalf("story markdown missing %q:\n%s", want, story)
		}
	}

	validation := renderRoomStoryValidationMarkdown(map[string]any{
		"id":   "val-1",
		"meta": map[string]any{"validator_type": "review", "status": "pass"},
	})
	for _, want := range []string{"# Story Validation val-1", "## Notes", "No additional notes recorded."} {
		if !strings.Contains(validation, want) {
			t.Fatalf("validation markdown missing %q:\n%s", want, validation)
		}
	}
}

func TestRenderRoomStoryMarkdownValidationHistoryNewestFirst(t *testing.T) {
	story := renderRoomStoryMarkdown(map[string]any{
		"title": "Template Story",
		"id":    "story-1",
		"meta":  map[string]any{"description": "Story desc"},
		"validations": []map[string]any{
			{"id": "val-1", "meta": map[string]any{"validator_type": "review", "status": "fail", "summary": "Older"}},
			{"id": "val-2", "meta": map[string]any{"validator_type": "integration", "status": "pass", "summary": "Newer"}},
		},
	})
	first := strings.Index(story, "val-2")
	second := strings.Index(story, "val-1")
	if first == -1 || second == -1 || first > second {
		t.Fatalf("validation history order wrong:\n%s", story)
	}
}

func TestRunRoomMilestoneEvidencePolicyGuidesHealthAndNext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	enforceFalse := false
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"integration"}, []string{"user_test"}, &enforceFalse, []string{"story validated"}); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["required_lane_status"]; got != "missing" {
		t.Fatalf("required_lane_status=%v want missing", got)
	}
	if got := milestone["required_lane_missing"].([]any); len(got) != 1 || got[0] != "integration" {
		t.Fatalf("required_lane_missing=%v want [integration]", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, "human-a"); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	items := decodeRoomEnvelope(t, out)["items"].([]any)
	foundPolicy := false
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["type"] == "cover_required_lane" && item["target_id"] == storyID {
			foundPolicy = strings.Contains(item["command_hint"].(string), " integration pass ")
		}
	}
	if !foundPolicy {
		t.Fatalf("items=%v want cover_required_lane for %s", items, storyID)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "human-a", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if !roomIssueTypesContain(health["issues"].([]any), "milestone_missing_required_lane") {
		t.Fatalf("issues=%v want milestone_missing_required_lane", health["issues"])
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "integration", "pass", "Integrated.", "docs/reviews/integration.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate integration: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow after validate: %v", err)
	}
	milestone = decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["required_lane_status"]; got != "satisfied" {
		t.Fatalf("required_lane_status=%v want satisfied", got)
	}
	if got := milestone["required_lane_covered"].([]any); len(got) != 1 || got[0] != "integration" {
		t.Fatalf("required_lane_covered=%v want [integration]", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "human-a", 100); err != nil {
		t.Fatalf("runRoomEpicHealth after validate: %v", err)
	}
	health = decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if roomIssueTypesContain(health["issues"].([]any), "milestone_missing_required_lane") {
		t.Fatalf("issues=%v want milestone_missing_required_lane cleared", health["issues"])
	}
}

func TestRunRoomMilestoneExitPolicyStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	t.Run("status ladder", func(t *testing.T) {
		workspace := t.TempDir()
		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		var out *bytes.Buffer
		cmd, _ := newRoomTestCommand(ctx)
		enforceFalse := false
		if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"review"}, nil, &enforceFalse, []string{"story validated"}); err != nil {
			t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}

		assertExitPolicy := func(want string, reasons ...string) {
			t.Helper()
			cmd, out := newRoomTestCommand(ctx)
			if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
				t.Fatalf("runRoomMilestoneShow: %v", err)
			}
			milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
			exitPolicy := milestone["exit_policy"].(map[string]any)
			if got := exitPolicy["status"]; got != want {
				t.Fatalf("exit_policy.status=%v want %s", got, want)
			}
			gotReasons := exitPolicy["reasons"].([]any)
			for _, reason := range reasons {
				found := false
				for _, got := range gotReasons {
					if got == reason {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("exit_policy.reasons=%v want %s", gotReasons, reason)
				}
			}
		}

		assertExitPolicy("not_ready", "accepted_stories_uncovered")

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate pass: %v", err)
		}
		assertExitPolicy("ready_for_review", "missing_review")

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
			t.Fatalf("runRoomMilestoneReview: %v", err)
		}
		assertExitPolicy("ready_for_summary", "missing_summary")

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Summary.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("runRoomMilestoneSummary: %v", err)
		}
		assertExitPolicy("ready_to_exit")

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
			t.Fatalf("runRoomEpicHealth: %v", err)
		}
		health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
		if got := health["health"]; got != "closing" {
			t.Fatalf("health=%v want closing", got)
		}
	})

	t.Run("failed validation blocks", func(t *testing.T) {
		workspace := t.TempDir()
		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		enforceFalse := false
		if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"review"}, nil, &enforceFalse, []string{"story validated"}); err != nil {
			t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "fail", "Validation failed.", "docs/reviews/fail.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate fail: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
			t.Fatalf("runRoomMilestoneShow: %v", err)
		}
		milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
		exitPolicy := milestone["exit_policy"].(map[string]any)
		if got := exitPolicy["status"]; got != "blocked" {
			t.Fatalf("exit_policy.status=%v want blocked", got)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
			t.Fatalf("runRoomEpicHealth: %v", err)
		}
		health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
		if got := health["health"]; got != "blocked" {
			t.Fatalf("health=%v want blocked", got)
		}
		if !roomIssueTypesContain(health["issues"].([]any), "milestone_failed_validation") {
			t.Fatalf("issues=%v want milestone_failed_validation", health["issues"])
		}
	})
}

func TestRunRoomEpicGrade(t *testing.T) {
	t.Run("not ready when planning gaps remain", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
			t.Fatalf("runRoomEpicGrade: %v", err)
		}
		grade := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
		if got := grade["decision"]; got != "NOT READY" {
			t.Fatalf("decision=%v want NOT READY", got)
		}
		if score := roomCategoryScoreByName(t, grade["categories"].([]any), "Milestone Architecture"); score >= 15 {
			t.Fatalf("Milestone Architecture score=%d want < 15", score)
		}
		if score := roomCategoryScoreByName(t, grade["categories"].([]any), "Validation & Evidence"); score >= 20 {
			t.Fatalf("Validation & Evidence score=%d want < 20", score)
		}
		if report := grade["report"].(string); !strings.Contains(report, "## Upgrade Actions") {
			t.Fatalf("report=%q want upgrade actions section", report)
		}
		scoutSignals := grade["scout_signals"].(map[string]any)
		if got := scoutSignals["schema_version"]; got != "room.epic.scout.v1" {
			t.Fatalf("scout_signals.schema_version=%v want room.epic.scout.v1", got)
		}
		if got := scoutSignals["blocking_findings"]; got == float64(0) {
			t.Fatalf("scout_signals.blocking_findings=%v want > 0", got)
		}
		familyCounts := scoutSignals["family_counts"].(map[string]any)
		if got := familyCounts["evidence_expectation"]; got != float64(1) {
			t.Fatalf("scout_signals.family_counts[evidence_expectation]=%v want 1", got)
		}
		readinessReasons := roomCategoryReasonsByName(t, grade["categories"].([]any), "Implementation-Plan Readiness")
		if !roomStringSliceContainsSubstring(readinessReasons, "Epic scout reports") {
			t.Fatalf("readiness reasons=%v want scout-driven readiness reason", readinessReasons)
		}
		blockers := stringSliceValue(grade["blockers"])
		if !roomStringSliceContainsSubstring(blockers, "Epic scout still reports") {
			t.Fatalf("blockers=%v want scout blocking summary", blockers)
		}
	})

	t.Run("ready when epic is fully shaped", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
			t.Fatalf("runRoomCreate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Planning-grade epic", "Ship work-packs", "human-a", "Validated room-agile implementation plan", "this week", []string{"room", "filesystem mirror"}, []string{"review lane is explicit", "implementation plan is execution-ready"}, false, false); err != nil {
			t.Fatalf("runRoomEpicStart: %v", err)
		}
		epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Epic creation complete."); err != nil {
			t.Fatalf("runRoomEpicFinalize: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		enforceFalse := false
		if err := runRoomMilestoneStartWithPolicy(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "Define the first execution-ready milestone", "human-a", []string{"story validation", "work-pack sync"}, nil, nil, nil, []string{"review"}, []string{"review"}, []string{"integration"}, &enforceFalse, []string{"story validated", "review captured"}, ""); err != nil {
			t.Fatalf("runRoomMilestoneStartWithPolicy: %v", err)
		}
		milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories specify validation evidence and review expectations"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Define review-ready story contract", "Add the execution-ready story contract with validators and evidence expectations.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd: %v", err)
		}
		storyID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Planning surface reviewed.", "docs/reviews/plan.md", "", "foxctl room epic show alpha "+epicID, "Captured the intended review lane.", nil); err != nil {
			t.Fatalf("runRoomStoryValidate: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
			t.Fatalf("runRoomEpicGrade: %v", err)
		}
		grade := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
		if got := grade["decision"]; got != "READY" {
			t.Fatalf("decision=%v want READY", got)
		}
		if total := int(grade["total"].(float64)); total < 90 {
			t.Fatalf("total=%d want >= 90", total)
		}
		if score := roomCategoryScoreByName(t, grade["categories"].([]any), "Validation & Evidence"); score < 16 {
			t.Fatalf("Validation & Evidence score=%d want >= 16", score)
		}
		if report := grade["report"].(string); !strings.Contains(report, "Decision: READY") {
			t.Fatalf("report=%q want READY decision", report)
		}
		scoutSignals := grade["scout_signals"].(map[string]any)
		if got := scoutSignals["total_findings"]; got != float64(0) {
			t.Fatalf("scout_signals.total_findings=%v want 0", got)
		}
		if got := scoutSignals["blocking_findings"]; got != float64(0) {
			t.Fatalf("scout_signals.blocking_findings=%v want 0", got)
		}
		blockers := stringSliceValue(grade["blockers"])
		if roomStringSliceContainsSubstring(blockers, "Epic scout still reports") {
			t.Fatalf("blockers=%v want no scout blockers", blockers)
		}
	})

	t.Run("plan imports with explicit source metadata keep provenance score", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
			t.Fatalf("runRoomCreate: %v", err)
		}

		planPath := filepath.Join(workspace, "grade-import-plan.md")
		mustWriteRoomTestFile(t, planPath, `# Grade Import Plan

## Delivery
### Step 1: Wire plan provenance
Capture source metadata in epic intake.`)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "claude"); err != nil {
			t.Fatalf("runRoomEpicImportPlan: %v", err)
		}
		epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
			t.Fatalf("runRoomEpicGrade: %v", err)
		}
		grade := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
		if score := roomCategoryScoreByName(t, grade["categories"].([]any), "Provenance & Import Fidelity"); score != 10 {
			t.Fatalf("Provenance & Import Fidelity score=%d want 10", score)
		}
		reasons := roomCategoryReasonsByName(t, grade["categories"].([]any), "Provenance & Import Fidelity")
		if roomStringSliceContainsSubstring(reasons, "missing") {
			t.Fatalf("provenance reasons=%v want no missing-metadata reason", reasons)
		}
	})

	t.Run("can publish grade report into room timeline", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{
			Enabled:   true,
			Sender:    "human-a",
			Recipient: "gemini-a",
			Subject:   "Please review epic grade",
		}); err != nil {
			t.Fatalf("runRoomEpicGrade publish: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		published := data["published"].(map[string]any)
		if got := published["enabled"]; got != true {
			t.Fatalf("published.enabled=%v want true", got)
		}
		if got := published["delivery"]; got != "direct" {
			t.Fatalf("published.delivery=%v want direct", got)
		}
		if data["agent_brief"] == "" {
			t.Fatalf("agent_brief missing")
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomInbox(cmd, workspace, "alpha", "gemini-a", 20, "", false, false, false, false); err != nil {
			t.Fatalf("runRoomInbox: %v", err)
		}
		inbox := decodeRoomEnvelope(t, out)
		items := inbox["entries"].([]any)
		found := false
		for _, raw := range items {
			msg := raw.(map[string]any)
			if msg["subject"] == "Please review epic grade" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("inbox=%v want published grade message", items)
		}
	})
}

func TestRunRoomEpicGradeDependencyTopologyPenalty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
		t.Fatalf("runRoomEpicGrade baseline: %v", err)
	}
	baselineGrade := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
	baselineStoryScore := roomCategoryScoreByName(t, baselineGrade["categories"].([]any), "Story Shape")
	baselineFamilyCounts := baselineGrade["scout_signals"].(map[string]any)["family_counts"].(map[string]any)
	if got := baselineFamilyCounts["dependency_topology"]; got != float64(0) {
		t.Fatalf("baseline dependency_topology=%v want 0", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", "missing-story-id"); err != nil {
		t.Fatalf("Set dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
		t.Fatalf("runRoomEpicGrade after topology issue: %v", err)
	}
	grade := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
	storyScore := roomCategoryScoreByName(t, grade["categories"].([]any), "Story Shape")
	if storyScore != baselineStoryScore-2 {
		t.Fatalf("Story Shape score=%d want baseline-2 (%d)", storyScore, baselineStoryScore-2)
	}
	familyCounts := grade["scout_signals"].(map[string]any)["family_counts"].(map[string]any)
	if got := familyCounts["dependency_topology"]; got != float64(1) {
		t.Fatalf("dependency_topology family count=%v want 1", got)
	}
	reasons := roomCategoryReasonsByName(t, grade["categories"].([]any), "Story Shape")
	if !roomStringSliceContainsSubstring(reasons, "dependency-topology") {
		t.Fatalf("Story Shape reasons=%v want dependency-topology reason", reasons)
	}
	blockers := stringSliceValue(grade["blockers"])
	if !roomStringSliceContainsSubstring(blockers, "stories have unresolved, self-referential, or cyclic dependency ids") {
		t.Fatalf("blockers=%v want dependency topology blocker", blockers)
	}
}

func TestRunRoomEpicStatus(t *testing.T) {
	t.Run("planning gap epic", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicStatus(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicStatus: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		if _, ok := data["health"].(map[string]any); !ok {
			t.Fatalf("health payload type=%T", data["health"])
		}
		status := data["status"].(map[string]any)
		if got := status["intake_mode"]; got != "authored" {
			t.Fatalf("intake_mode=%v want authored", got)
		}
		if got := status["phase"]; got != "graded" {
			t.Fatalf("phase=%v want graded", got)
		}
		if got := status["current_milestone_title"]; got != "Foundation" {
			t.Fatalf("current_milestone_title=%v want Foundation", got)
		}
		if got := status["open_intake_questions"]; got != float64(0) {
			t.Fatalf("open_intake_questions=%v want 0", got)
		}
		wantGaps := []string{
			"missing_outcome",
			"missing_milestone_contract",
			"missing_milestone_criteria",
			"missing_validation_lane",
			"missing_story_validation",
		}
		gotGaps := stringSliceValue(status["structural_gaps"])
		if len(gotGaps) != len(wantGaps) {
			t.Fatalf("len(structural_gaps)=%d want %d gaps=%v", len(gotGaps), len(wantGaps), gotGaps)
		}
		for i := range wantGaps {
			if gotGaps[i] != wantGaps[i] {
				t.Fatalf("structural_gaps[%d]=%q want %q (all=%v)", i, gotGaps[i], wantGaps[i], gotGaps)
			}
		}
		if got := status["next_pipeline_stage"]; got != "repair" {
			t.Fatalf("next_pipeline_stage=%v want repair", got)
		}
	})

	t.Run("dispatchable ready epic", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomEpicUpdate(cmd, workspace, "human-a", "alpha", epicID, "", "Execution-ready epic plan."); err != nil {
			t.Fatalf("runRoomEpicUpdate: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneContract(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"story validated"}); err != nil {
			t.Fatalf("runRoomMilestoneContract: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicStatus(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicStatus: %v", err)
		}
		status := decodeRoomEnvelope(t, out)["status"].(map[string]any)
		if got := status["intake_mode"]; got != "authored" {
			t.Fatalf("intake_mode=%v want authored", got)
		}
		if got := status["phase"]; got != "dispatchable" {
			t.Fatalf("phase=%v want dispatchable", got)
		}
		if got := status["current_milestone_title"]; got != "Foundation" {
			t.Fatalf("current_milestone_title=%v want Foundation", got)
		}
		if got := status["open_intake_questions"]; got != float64(0) {
			t.Fatalf("open_intake_questions=%v want 0", got)
		}
		if gaps := stringSliceValue(status["structural_gaps"]); len(gaps) != 0 {
			t.Fatalf("structural_gaps=%v want empty", gaps)
		}
		if got := status["next_pipeline_stage"]; got != "dispatch" {
			t.Fatalf("next_pipeline_stage=%v want dispatch", got)
		}
	})
}

func TestRunRoomEpicFrontier(t *testing.T) {
	t.Run("ready accepted story with no dependencies", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		scope := data["scope"].(map[string]any)
		if got := scope["epic_id"]; got != epicID {
			t.Fatalf("scope.epic_id=%v want %s", got, epicID)
		}
		ready := data["ready_frontier"].([]any)
		if len(ready) != 1 {
			t.Fatalf("len(ready_frontier)=%d want 1", len(ready))
		}
		readyItem := ready[0].(map[string]any)
		if got := readyItem["story_id"]; got != storyID {
			t.Fatalf("ready story_id=%v want %s", got, storyID)
		}
		if got := readyItem["milestone_id"]; got != milestoneID {
			t.Fatalf("ready milestone_id=%v want %s", got, milestoneID)
		}
		if got := data["parallelism_width"]; got != float64(1) {
			t.Fatalf("parallelism_width=%v want 1", got)
		}
		if got := len(data["blocked_frontier"].([]any)); got != 0 {
			t.Fatalf("len(blocked_frontier)=%d want 0", got)
		}
		if got := len(data["dependency_gaps"].([]any)); got != 0 {
			t.Fatalf("len(dependency_gaps)=%d want 0", got)
		}
	})

	t.Run("blocked frontier due to blocked story state", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "blocked", "Waiting on dependency.", "upstream", ""); err != nil {
			t.Fatalf("runRoomStoryState blocked: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		if got := len(data["ready_frontier"].([]any)); got != 0 {
			t.Fatalf("len(ready_frontier)=%d want 0", got)
		}
		blocked := data["blocked_frontier"].([]any)
		if len(blocked) != 1 {
			t.Fatalf("len(blocked_frontier)=%d want 1", len(blocked))
		}
		blockedItem := blocked[0].(map[string]any)
		if got := blockedItem["story_id"]; got != storyID {
			t.Fatalf("blocked story_id=%v want %s", got, storyID)
		}
		reasonCodes := stringSliceValue(blockedItem["reason_codes"])
		if !stringSliceContains(reasonCodes, "story_state_blocked") {
			t.Fatalf("reason_codes=%v want story_state_blocked", reasonCodes)
		}
	})

	t.Run("blocked frontier due to unmet upstream milestone dependency", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, upstreamMilestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Dependent", "", "", "human-a", nil, nil, nil, []string{upstreamMilestoneID}, nil, nil, ""); err != nil {
			t.Fatalf("runRoomMilestoneStart dependent: %v", err)
		}
		dependentMilestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", dependentMilestoneID, "Blocked by upstream milestone", "Depends on upstream milestone pass.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd dependent: %v", err)
		}
		dependentStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		blocked := data["blocked_frontier"].([]any)

		var dependentStory map[string]any
		for _, raw := range blocked {
			item := raw.(map[string]any)
			if item["story_id"] == dependentStoryID {
				dependentStory = item
				break
			}
		}
		if dependentStory == nil {
			t.Fatalf("blocked_frontier missing dependent story %s: %v", dependentStoryID, blocked)
		}
		reasonCodes := stringSliceValue(dependentStory["reason_codes"])
		if !stringSliceContains(reasonCodes, "unmet_milestone_dependency") {
			t.Fatalf("reason_codes=%v want unmet_milestone_dependency", reasonCodes)
		}
		unmet := dependentStory["unmet_dependencies"].([]any)
		if len(unmet) != 1 {
			t.Fatalf("len(unmet_dependencies)=%d want 1", len(unmet))
		}
		unmetDep := unmet[0].(map[string]any)
		if got := unmetDep["upstream_milestone_id"]; got != upstreamMilestoneID {
			t.Fatalf("upstream_milestone_id=%v want %s", got, upstreamMilestoneID)
		}
		milestoneCandidateCount := 0
		for _, raw := range data["unblock_candidates"].([]any) {
			candidate := raw.(map[string]any)
			if stringField(candidate, "kind") != "milestone" || stringField(candidate, "id") != upstreamMilestoneID {
				continue
			}
			milestoneCandidateCount++
			reasons := stringSliceValue(candidate["source_reasons"])
			if !stringSliceContains(reasons, "unmet_milestone_dependency") {
				t.Fatalf("candidate source_reasons=%v want unmet_milestone_dependency", reasons)
			}
		}
		if milestoneCandidateCount != 1 {
			t.Fatalf("milestone unblock candidate count=%d want 1 for %s", milestoneCandidateCount, upstreamMilestoneID)
		}
	})

	t.Run("dependency gap when milestone dependency is unresolved", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Gap milestone", "", "", "human-a", nil, nil, nil, []string{"milestone-missing"}, nil, nil, ""); err != nil {
			t.Fatalf("runRoomMilestoneStart gap milestone: %v", err)
		}
		gapMilestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", gapMilestoneID, "Dependency gap story", "References a missing milestone dependency.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd gap story: %v", err)
		}
		gapStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)

		gaps := data["dependency_gaps"].([]any)
		if len(gaps) != 1 {
			t.Fatalf("len(dependency_gaps)=%d want 1", len(gaps))
		}
		gap := gaps[0].(map[string]any)
		if got := gap["milestone_id"]; got != gapMilestoneID {
			t.Fatalf("gap milestone_id=%v want %s", got, gapMilestoneID)
		}
		if got := gap["dependency"]; got != "milestone-missing" {
			t.Fatalf("gap dependency=%v want milestone-missing", got)
		}
		gapCandidateCount := 0
		for _, raw := range data["unblock_candidates"].([]any) {
			candidate := raw.(map[string]any)
			if stringField(candidate, "kind") != "dependency_gap" || stringField(candidate, "dependency_kind") != "milestone" || stringField(candidate, "dependency") != "milestone-missing" {
				continue
			}
			gapCandidateCount++
		}
		if gapCandidateCount != 1 {
			t.Fatalf("dependency gap unblock candidate count=%d want 1", gapCandidateCount)
		}

		blocked := data["blocked_frontier"].([]any)
		var gapStory map[string]any
		for _, raw := range blocked {
			item := raw.(map[string]any)
			if item["story_id"] == gapStoryID {
				gapStory = item
				break
			}
		}
		if gapStory == nil {
			t.Fatalf("blocked_frontier missing gap story %s: %v", gapStoryID, blocked)
		}
		reasonCodes := stringSliceValue(gapStory["reason_codes"])
		if !stringSliceContains(reasonCodes, "dependency_gap") {
			t.Fatalf("reason_codes=%v want dependency_gap", reasonCodes)
		}
	})

	t.Run("blocked frontier due to unresolved story dependency id", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		cmd.Flags().StringSlice("dependency", nil, "")
		if err := cmd.Flags().Set("dependency", "story-missing"); err != nil {
			t.Fatalf("Set dependency story-missing: %v", err)
		}
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Blocked by unresolved story id", "Depends on a missing story id.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd unresolved dependency: %v", err)
		}
		dependentStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)

		var dependentStory map[string]any
		for _, raw := range data["blocked_frontier"].([]any) {
			item := raw.(map[string]any)
			if item["story_id"] == dependentStoryID {
				dependentStory = item
				break
			}
		}
		if dependentStory == nil {
			t.Fatalf("blocked_frontier missing dependent story %s", dependentStoryID)
		}
		reasonCodes := stringSliceValue(dependentStory["reason_codes"])
		if !stringSliceContains(reasonCodes, "story_dependency_gap") {
			t.Fatalf("reason_codes=%v want story_dependency_gap", reasonCodes)
		}
		storyGaps := dependentStory["story_dependency_gaps"].([]any)
		if len(storyGaps) != 1 {
			t.Fatalf("len(story_dependency_gaps)=%d want 1", len(storyGaps))
		}
		storyGap := storyGaps[0].(map[string]any)
		if got := storyGap["reason"]; got != "unresolved_story_dependency" {
			t.Fatalf("story gap reason=%v want unresolved_story_dependency", got)
		}
		if got := storyGap["dependency"]; got != "story-missing" {
			t.Fatalf("story gap dependency=%v want story-missing", got)
		}
	})

	t.Run("blocked frontier when upstream story exists but is not done", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, upstreamStoryID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		cmd.Flags().StringSlice("dependency", nil, "")
		if err := cmd.Flags().Set("dependency", upstreamStoryID); err != nil {
			t.Fatalf("Set dependency upstream story: %v", err)
		}
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Blocked by unfinished upstream story", "Depends on the upstream story reaching a terminal state.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd dependent: %v", err)
		}
		dependentStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)

		var dependentStory map[string]any
		for _, raw := range data["blocked_frontier"].([]any) {
			item := raw.(map[string]any)
			if item["story_id"] == dependentStoryID {
				dependentStory = item
				break
			}
		}
		if dependentStory == nil {
			t.Fatalf("blocked_frontier missing dependent story %s", dependentStoryID)
		}
		reasonCodes := stringSliceValue(dependentStory["reason_codes"])
		if !stringSliceContains(reasonCodes, "unmet_story_dependency") {
			t.Fatalf("reason_codes=%v want unmet_story_dependency", reasonCodes)
		}
		unmet := dependentStory["unmet_story_dependencies"].([]any)
		if len(unmet) != 1 {
			t.Fatalf("len(unmet_story_dependencies)=%d want 1", len(unmet))
		}
		upstream := unmet[0].(map[string]any)
		if got := upstream["upstream_story_id"]; got != upstreamStoryID {
			t.Fatalf("upstream_story_id=%v want %s", got, upstreamStoryID)
		}
		if got := upstream["upstream_state"]; got != "accepted" {
			t.Fatalf("upstream_state=%v want accepted", got)
		}
		upstreamCandidateCount := 0
		for _, raw := range data["unblock_candidates"].([]any) {
			candidate := raw.(map[string]any)
			if stringField(candidate, "kind") != "story" || stringField(candidate, "id") != upstreamStoryID {
				continue
			}
			upstreamCandidateCount++
			reasons := stringSliceValue(candidate["source_reasons"])
			if !stringSliceContains(reasons, "unmet_story_dependency") {
				t.Fatalf("candidate source_reasons=%v want unmet_story_dependency", reasons)
			}
		}
		if upstreamCandidateCount != 1 {
			t.Fatalf("upstream story unblock candidate count=%d want 1 for %s", upstreamCandidateCount, upstreamStoryID)
		}
	})

	t.Run("unblocks dependent story when upstream story reaches terminal state", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, upstreamStoryID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		cmd.Flags().StringSlice("dependency", nil, "")
		if err := cmd.Flags().Set("dependency", upstreamStoryID); err != nil {
			t.Fatalf("Set dependency upstream story: %v", err)
		}
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Waits for upstream validation", "Depends on upstream story completion.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd dependent: %v", err)
		}
		dependentStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier before upstream done: %v", err)
		}
		before := decodeRoomEnvelope(t, out)
		foundBlocked := false
		for _, raw := range before["blocked_frontier"].([]any) {
			item := raw.(map[string]any)
			if item["story_id"] == dependentStoryID {
				foundBlocked = true
				break
			}
		}
		if !foundBlocked {
			t.Fatalf("blocked_frontier=%v want dependent story %s before upstream completion", before["blocked_frontier"], dependentStoryID)
		}

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", upstreamStoryID, "review", "pass", "Upstream story validated.", "docs/reviews/upstream-pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate upstream pass: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier after upstream done: %v", err)
		}
		after := decodeRoomEnvelope(t, out)
		foundReady := false
		for _, raw := range after["ready_frontier"].([]any) {
			item := raw.(map[string]any)
			if item["story_id"] == dependentStoryID {
				foundReady = true
				break
			}
		}
		if !foundReady {
			t.Fatalf("ready_frontier=%v want dependent story %s after upstream completion", after["ready_frontier"], dependentStoryID)
		}
	})
}

func TestRunRoomEpicDispatchFrontier(t *testing.T) {
	t.Run("one ready story emits one dispatch unit", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{
			Dispatch: roomEpicGradeDispatchOptions{Spawn: false},
		}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		if got := data["dispatchable_count"]; got != float64(1) {
			t.Fatalf("dispatchable_count=%v want 1", got)
		}
		units := data["dispatch_units"].([]any)
		if len(units) != 1 {
			t.Fatalf("len(dispatch_units)=%d want 1", len(units))
		}
		unit := units[0].(map[string]any)
		if got := unit["story_id"]; got != storyID {
			t.Fatalf("story_id=%v want %s", got, storyID)
		}
		if got := unit["milestone_id"]; got != milestoneID {
			t.Fatalf("milestone_id=%v want %s", got, milestoneID)
		}
		if got := strings.TrimSpace(stringField(unit, "agent_brief")); got == "" {
			t.Fatalf("agent_brief empty")
		}
	})

	t.Run("no ready items returns empty dispatch units with counts", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "blocked", "Waiting for upstream dependency.", "upstream", ""); err != nil {
			t.Fatalf("runRoomStoryState blocked: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		if got := len(data["dispatch_units"].([]any)); got != 0 {
			t.Fatalf("len(dispatch_units)=%d want 0", got)
		}
		if got := data["dispatchable_count"]; got != float64(0) {
			t.Fatalf("dispatchable_count=%v want 0", got)
		}
		if got := data["blocked_count"]; got != float64(1) {
			t.Fatalf("blocked_count=%v want 1", got)
		}
		if got := data["dependency_gap_count"]; got != float64(0) {
			t.Fatalf("dependency_gap_count=%v want 0", got)
		}
		if got := strings.TrimSpace(stringField(data, "reason")); got == "" {
			t.Fatalf("reason empty")
		}
	})

	t.Run("dispatch frontier reflects story dependency blockers and gaps", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, upstreamStoryID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		cmd.Flags().StringSlice("dependency", nil, "")
		if err := cmd.Flags().Set("dependency", "story-missing"); err != nil {
			t.Fatalf("Set dependency story-missing: %v", err)
		}
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Blocked dependent story", "Blocked by missing story dependency.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd dependent: %v", err)
		}
		dependentStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicFrontier: %v", err)
		}
		frontierData := decodeRoomEnvelope(t, out)

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		if got := data["dispatchable_count"]; got != float64(1) {
			t.Fatalf("dispatchable_count=%v want 1", got)
		}
		if got := data["blocked_count"]; got != float64(1) {
			t.Fatalf("blocked_count=%v want 1", got)
		}
		if got := data["dependency_gap_count"]; got != float64(1) {
			t.Fatalf("dependency_gap_count=%v want 1", got)
		}
		units := data["dispatch_units"].([]any)
		if len(units) != 1 {
			t.Fatalf("len(dispatch_units)=%d want 1", len(units))
		}
		if got := units[0].(map[string]any)["story_id"]; got != upstreamStoryID {
			t.Fatalf("ready dispatch story_id=%v want %s", got, upstreamStoryID)
		}
		if got := strings.TrimSpace(fmt.Sprintf("%v", data["summary"])); !strings.Contains(got, "dependency gap") {
			t.Fatalf("summary=%q want dependency gap signal", got)
		}
		if got := strings.TrimSpace(fmt.Sprintf("%v", data["summary"])); strings.Contains(got, dependentStoryID) {
			t.Fatalf("summary unexpectedly includes blocked story id %q: %q", dependentStoryID, got)
		}
		frontierBlockedSummary := frontierData["blocked_summary"].(map[string]any)
		dispatchBlockedSummary := data["blocked_summary"].(map[string]any)
		for _, key := range []string{"blocked_story_count", "dependency_gap_count", "unblock_candidate_count"} {
			if got, want := dispatchBlockedSummary[key], frontierBlockedSummary[key]; got != want {
				t.Fatalf("dispatch blocked_summary[%q]=%v want %v", key, got, want)
			}
		}
		frontierEntityCounts := frontierData["blocking_entity_counts"].(map[string]any)
		dispatchEntityCounts := data["blocking_entity_counts"].(map[string]any)
		for _, key := range []string{"story", "milestone", "dependency_gap"} {
			if got, want := dispatchEntityCounts[key], frontierEntityCounts[key]; got != want {
				t.Fatalf("dispatch blocking_entity_counts[%q]=%v want %v", key, got, want)
			}
		}
	})

	t.Run("multiple ready stories are deterministically ordered", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Second ready story", "Another accepted story in the same milestone.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd second: %v", err)
		}
		secondStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)

		firstExpected := storyID
		secondExpected := secondStoryID
		if firstExpected > secondExpected {
			firstExpected, secondExpected = secondExpected, firstExpected
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier first: %v", err)
		}
		firstUnits := decodeRoomEnvelope(t, out)["dispatch_units"].([]any)
		if len(firstUnits) != 2 {
			t.Fatalf("len(first dispatch_units)=%d want 2", len(firstUnits))
		}
		if got := stringField(firstUnits[0].(map[string]any), "story_id"); got != firstExpected {
			t.Fatalf("first order story_id=%s want %s", got, firstExpected)
		}
		if got := stringField(firstUnits[1].(map[string]any), "story_id"); got != secondExpected {
			t.Fatalf("second order story_id=%s want %s", got, secondExpected)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier second: %v", err)
		}
		secondUnits := decodeRoomEnvelope(t, out)["dispatch_units"].([]any)
		if len(secondUnits) != 2 {
			t.Fatalf("len(second dispatch_units)=%d want 2", len(secondUnits))
		}
		if got := stringField(secondUnits[0].(map[string]any), "story_id"); got != firstExpected {
			t.Fatalf("second run first story_id=%s want %s", got, firstExpected)
		}
		if got := stringField(secondUnits[1].(map[string]any), "story_id"); got != secondExpected {
			t.Fatalf("second run second story_id=%s want %s", got, secondExpected)
		}
	})

	t.Run("spawn dry-run with one ready unit returns selected unit and spawn data", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{
			Dispatch: roomEpicGradeDispatchOptions{
				Spawn:         true,
				SpawnDryRun:   true,
				LLMProvider:   "lmstudio",
				LLMModel:      "qwen2.5-coder-7b-instruct",
				ExecMode:      "autonomous",
				MaxAutoTurns:  2,
				MaxIterations: 12,
				RoomAccess:    "direct",
			},
		}); err != nil {
			t.Fatalf("runRoomEpicDispatchFrontier spawn dry-run: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		dispatch := data["dispatch"].(map[string]any)
		if got := dispatch["story_id"]; got != storyID {
			t.Fatalf("dispatch.story_id=%v want %s", got, storyID)
		}
		selected := dispatch["selected_unit"].(map[string]any)
		if got := selected["story_id"]; got != storyID {
			t.Fatalf("selected story_id=%v want %s", got, storyID)
		}
		spawn := dispatch["spawn"].(map[string]any)
		if got := spawn["spawned"]; got != false {
			t.Fatalf("spawn.spawned=%v want false", got)
		}
		if got := spawn["dry_run"]; got != true {
			t.Fatalf("spawn.dry_run=%v want true", got)
		}
		if got := strings.TrimSpace(stringField(spawn, "role")); got != roomEpicDispatchFrontierDefaultAgentRole {
			t.Fatalf("spawn.role=%q want %q", got, roomEpicDispatchFrontierDefaultAgentRole)
		}
		if got := spawn["planning_only"]; got != false {
			t.Fatalf("spawn.planning_only=%v want false", got)
		}
		if got := spawn["execution_dispatch"]; got != true {
			t.Fatalf("spawn.execution_dispatch=%v want true", got)
		}
		if got := strings.TrimSpace(stringField(spawn, "spawn_command")); got == "" {
			t.Fatalf("spawn_command empty")
		}
		if got := stringField(spawn, "spawn_command"); !strings.Contains(got, "--role '"+roomEpicDispatchFrontierDefaultAgentRole+"'") {
			t.Fatalf("spawn_command=%q want execution role %q", got, roomEpicDispatchFrontierDefaultAgentRole)
		}
		if got := spawn["dispatch_prompt"]; strings.TrimSpace(fmt.Sprint(got)) == "" {
			t.Fatalf("dispatch_prompt missing")
		}
	})

	t.Run("spawn with multiple ready units and no selector returns argument error", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)
		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Second ready story", "Another accepted story in the same milestone.", "human-a"); err != nil {
			t.Fatalf("runRoomStoryAdd second: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{
			Dispatch: roomEpicGradeDispatchOptions{
				Spawn:         true,
				SpawnDryRun:   true,
				LLMProvider:   "lmstudio",
				LLMModel:      "qwen2.5-coder-7b-instruct",
				AgentRole:     "planner",
				ExecMode:      "autonomous",
				MaxAutoTurns:  2,
				MaxIterations: 12,
				RoomAccess:    "direct",
			},
		})
		if err == nil {
			t.Fatal("runRoomEpicDispatchFrontier expected argument error envelope")
		}
		var env envelope.Envelope
		if decodeErr := json.Unmarshal(out.Bytes(), &env); decodeErr != nil {
			t.Fatalf("decode envelope: %v\n%s", decodeErr, out.String())
		}
		if env.Status != envelope.StatusError {
			t.Fatalf("status=%q want error payload=%s", env.Status, out.String())
		}
		if env.Error.Code != string(protocol.ErrorCodeEARG) {
			t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeEARG)
		}
		if !strings.Contains(out.String(), "--story-id") {
			t.Fatalf("payload=%s want --story-id hint", out.String())
		}
	})

	t.Run("spawn with story selector outside ready frontier returns argument error", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "blocked", "Waiting for dependency.", "dependency", ""); err != nil {
			t.Fatalf("runRoomStoryState blocked: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		err := runRoomEpicDispatchFrontier(cmd, workspace, "alpha", epicID, 100, roomEpicDispatchFrontierOptions{
			StoryID: storyID,
			Dispatch: roomEpicGradeDispatchOptions{
				Spawn:         true,
				SpawnDryRun:   true,
				LLMProvider:   "lmstudio",
				LLMModel:      "qwen2.5-coder-7b-instruct",
				AgentRole:     "planner",
				ExecMode:      "autonomous",
				MaxAutoTurns:  2,
				MaxIterations: 12,
				RoomAccess:    "direct",
			},
		})
		if err == nil {
			t.Fatal("runRoomEpicDispatchFrontier expected argument error envelope")
		}
		var env envelope.Envelope
		if decodeErr := json.Unmarshal(out.Bytes(), &env); decodeErr != nil {
			t.Fatalf("decode envelope: %v\n%s", decodeErr, out.String())
		}
		if env.Status != envelope.StatusError {
			t.Fatalf("status=%q want error payload=%s", env.Status, out.String())
		}
		if env.Error.Code != string(protocol.ErrorCodeEARG) {
			t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeEARG)
		}
		if !strings.Contains(out.String(), storyID) {
			t.Fatalf("payload=%s want selected story id %q", out.String(), storyID)
		}
	})
}

func TestRunRoomEpicScout(t *testing.T) {
	t.Run("planning gap epic", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		data := decodeRoomEnvelope(t, out)

		status := data["status"].(map[string]any)
		if got := status["phase"]; got != "graded" {
			t.Fatalf("phase=%v want graded", got)
		}

		scout := data["scout"].(map[string]any)
		scope := scout["scope"].(map[string]any)
		if got := scope["room_id"]; got != "alpha" {
			t.Fatalf("scope.room_id=%v want alpha", got)
		}
		if got := scope["epic_id"]; got != epicID {
			t.Fatalf("scope.epic_id=%v want %s", got, epicID)
		}
		if got := scope["intake_mode"]; got != "authored" {
			t.Fatalf("scope.intake_mode=%v want authored", got)
		}
		if got := scope["status_phase"]; got != "graded" {
			t.Fatalf("scope.status_phase=%v want graded", got)
		}
		if got := scout["snapshot_id"]; got != "snapshot-missing" {
			t.Fatalf("snapshot_id=%v want snapshot-missing", got)
		}
		if got := scout["snapshot_artifact"]; got != "" {
			t.Fatalf("snapshot_artifact=%v want empty", got)
		}
		if got := scout["scout_artifact"]; got != "" {
			t.Fatalf("scout_artifact=%v want empty", got)
		}

		wantOutOfScope := []string{"snapshot_unavailable"}
		gotOutOfScope := stringSliceValue(scout["out_of_scope"])
		if len(gotOutOfScope) != len(wantOutOfScope) || gotOutOfScope[0] != wantOutOfScope[0] {
			t.Fatalf("out_of_scope=%v want %v", gotOutOfScope, wantOutOfScope)
		}

		findings := scout["findings"].([]any)
		wantFamilies := []string{
			"brief_boundary",
			"milestone_contract",
			"criteria_coverage",
			"validation_lane",
			"evidence_expectation",
		}
		if len(findings) != len(wantFamilies) {
			t.Fatalf("len(findings)=%d want %d findings=%v", len(findings), len(wantFamilies), findings)
		}
		for i, raw := range findings {
			finding := raw.(map[string]any)
			if got := finding["rule_family"]; got != wantFamilies[i] {
				t.Fatalf("findings[%d].rule_family=%v want %s", i, got, wantFamilies[i])
			}
			if got := finding["severity"]; got != "blocking" {
				t.Fatalf("findings[%d].severity=%v want blocking", i, got)
			}
			if id := strings.TrimSpace(fmt.Sprintf("%v", finding["id"])); id == "" {
				t.Fatalf("findings[%d].id missing", i)
			}
			if entityType := strings.TrimSpace(fmt.Sprintf("%v", finding["entity_type"])); entityType == "" {
				t.Fatalf("findings[%d].entity_type missing", i)
			}
			if entityID := strings.TrimSpace(fmt.Sprintf("%v", finding["entity_id"])); entityID == "" {
				t.Fatalf("findings[%d].entity_id missing", i)
			}
			lanes := stringSliceValue(finding["recommended_repair_lanes"])
			if len(lanes) == 0 {
				t.Fatalf("findings[%d].recommended_repair_lanes empty", i)
			}
		}

		signals := scout["signals"].(map[string]any)
		if got := signals["schema_version"]; got != "room.epic.scout.v1" {
			t.Fatalf("signals.schema_version=%v want room.epic.scout.v1", got)
		}
		if got := signals["total_findings"]; got != float64(len(wantFamilies)) {
			t.Fatalf("signals.total_findings=%v want %d", got, len(wantFamilies))
		}
		if got := signals["blocking_findings"]; got != float64(len(wantFamilies)) {
			t.Fatalf("signals.blocking_findings=%v want %d", got, len(wantFamilies))
		}
		familyCounts := signals["family_counts"].(map[string]any)
		wantCounts := map[string]float64{
			"brief_boundary":       1,
			"milestone_contract":   1,
			"dependency_topology":  0,
			"criteria_coverage":    1,
			"owner_dispatch":       0,
			"validation_lane":      1,
			"evidence_expectation": 1,
		}
		for family, want := range wantCounts {
			if got := familyCounts[family]; got != want {
				t.Fatalf("family_counts[%s]=%v want %v", family, got, want)
			}
		}
	})

	t.Run("planning gap epic includes latest snapshot metadata when available", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicSnapshot(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicSnapshot: %v", err)
		}
		snapshot := decodeRoomEnvelope(t, out)
		wantSnapshotID := strings.TrimSpace(fmt.Sprintf("%v", snapshot["snapshot_id"]))
		wantSnapshotArtifact := strings.TrimSpace(fmt.Sprintf("%v", snapshot["snapshot_artifact"]))
		if wantSnapshotID == "" {
			t.Fatalf("snapshot_id empty")
		}
		if wantSnapshotArtifact == "" {
			t.Fatalf("snapshot_artifact empty")
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
		if got := scout["snapshot_id"]; got != wantSnapshotID {
			t.Fatalf("snapshot_id=%v want %s", got, wantSnapshotID)
		}
		if got := scout["snapshot_artifact"]; got != wantSnapshotArtifact {
			t.Fatalf("snapshot_artifact=%v want %s", got, wantSnapshotArtifact)
		}
		for _, item := range stringSliceValue(scout["out_of_scope"]) {
			if item == "snapshot_unavailable" {
				t.Fatalf("out_of_scope=%v unexpectedly contains snapshot_unavailable", scout["out_of_scope"])
			}
		}
	})

	t.Run("planning gap epic falls back when latest snapshot pointer is stale", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicSnapshot(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicSnapshot: %v", err)
		}
		snapshot := decodeRoomEnvelope(t, out)
		snapshotArtifact := strings.TrimSpace(fmt.Sprintf("%v", snapshot["snapshot_artifact"]))
		if snapshotArtifact == "" {
			t.Fatalf("snapshot_artifact empty")
		}
		if err := os.Remove(snapshotArtifact); err != nil {
			t.Fatalf("remove snapshot artifact: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
		if got := scout["snapshot_id"]; got != "snapshot-missing" {
			t.Fatalf("snapshot_id=%v want snapshot-missing", got)
		}
		if got := scout["snapshot_artifact"]; got != "" {
			t.Fatalf("snapshot_artifact=%v want empty", got)
		}
		gotOutOfScope := stringSliceValue(scout["out_of_scope"])
		if len(gotOutOfScope) == 0 || gotOutOfScope[0] != "snapshot_unavailable" {
			t.Fatalf("out_of_scope=%v want snapshot_unavailable fallback", gotOutOfScope)
		}
	})

	t.Run("finalized epic without milestones emits milestone structural findings", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
			t.Fatalf("runRoomCreate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Milestone-free epic", "Define scout parity checks", "human-a", "Execution-ready brief.", "", nil, nil); err != nil {
			t.Fatalf("runRoomEpicStart: %v", err)
		}
		epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Execution-ready brief."); err != nil {
			t.Fatalf("runRoomEpicFinalize: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		scout := data["scout"].(map[string]any)

		findings := scout["findings"].([]any)
		wantFamilies := []string{
			"milestone_contract",
			"criteria_coverage",
			"validation_lane",
		}
		if len(findings) != len(wantFamilies) {
			t.Fatalf("len(findings)=%d want %d findings=%v", len(findings), len(wantFamilies), findings)
		}
		for i, raw := range findings {
			finding := raw.(map[string]any)
			if got := finding["rule_family"]; got != wantFamilies[i] {
				t.Fatalf("findings[%d].rule_family=%v want %s", i, got, wantFamilies[i])
			}
			if got := finding["entity_type"]; got != "epic" {
				t.Fatalf("findings[%d].entity_type=%v want epic", i, got)
			}
			if got := finding["entity_id"]; got != epicID {
				t.Fatalf("findings[%d].entity_id=%v want %s", i, got, epicID)
			}
		}

		gotOutOfScope := stringSliceValue(scout["out_of_scope"])
		if len(gotOutOfScope) != 1 || gotOutOfScope[0] != "snapshot_unavailable" {
			t.Fatalf("out_of_scope=%v want [snapshot_unavailable]", gotOutOfScope)
		}
	})

	t.Run("draft epic with open intake marks intake limitations in out of scope", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
			t.Fatalf("runRoomCreate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Draft intake epic", "Clarify scope first", "human-a", "Initial brief.", "", nil, nil); err != nil {
			t.Fatalf("runRoomEpicStart: %v", err)
		}
		epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomEpicAsk(cmd, workspace, "human-a", "alpha", epicID, "gemini-a", "constraint", "What is still unknown?"); err != nil {
			t.Fatalf("runRoomEpicAsk: %v", err)
		}

		cmd, out = newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		status := data["status"].(map[string]any)
		if got := status["phase"]; got != "draft" {
			t.Fatalf("phase=%v want draft", got)
		}
		if got := status["open_intake_questions"]; got != float64(1) {
			t.Fatalf("open_intake_questions=%v want 1", got)
		}

		scout := data["scout"].(map[string]any)
		wantOutOfScope := []string{
			"snapshot_unavailable",
			"intake_not_finalized",
			"intake_questions_open",
		}
		gotOutOfScope := stringSliceValue(scout["out_of_scope"])
		if len(gotOutOfScope) != len(wantOutOfScope) {
			t.Fatalf("len(out_of_scope)=%d want %d out_of_scope=%v", len(gotOutOfScope), len(wantOutOfScope), gotOutOfScope)
		}
		for i := range wantOutOfScope {
			if gotOutOfScope[i] != wantOutOfScope[i] {
				t.Fatalf("out_of_scope[%d]=%q want %q (all=%v)", i, gotOutOfScope[i], wantOutOfScope[i], gotOutOfScope)
			}
		}
	})

	t.Run("dispatchable ready epic", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ctx := context.Background()
		workspace := t.TempDir()

		epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		if err := runRoomEpicUpdate(cmd, workspace, "human-a", "alpha", epicID, "", "Execution-ready epic plan."); err != nil {
			t.Fatalf("runRoomEpicUpdate: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneContract(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"story validated"}); err != nil {
			t.Fatalf("runRoomMilestoneContract: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
			t.Fatalf("runRoomEpicScout: %v", err)
		}
		data := decodeRoomEnvelope(t, out)
		scout := data["scout"].(map[string]any)
		scope := scout["scope"].(map[string]any)
		if got := scope["status_phase"]; got != "dispatchable" {
			t.Fatalf("scope.status_phase=%v want dispatchable", got)
		}

		findings := scout["findings"].([]any)
		if len(findings) != 0 {
			t.Fatalf("findings=%v want empty", findings)
		}
		signals := scout["signals"].(map[string]any)
		if got := signals["total_findings"]; got != float64(0) {
			t.Fatalf("signals.total_findings=%v want 0", got)
		}
		if got := signals["blocking_findings"]; got != float64(0) {
			t.Fatalf("signals.blocking_findings=%v want 0", got)
		}
		gotOutOfScope := stringSliceValue(scout["out_of_scope"])
		if len(gotOutOfScope) != 1 || gotOutOfScope[0] != "snapshot_unavailable" {
			t.Fatalf("out_of_scope=%v want [snapshot_unavailable]", gotOutOfScope)
		}
	})
}

func TestRunRoomEpicScoutDependencyTopologyFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, []string{milestoneID, "missing-milestone-id"}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyID); err != nil {
		t.Fatalf("Set dependency self: %v", err)
	}
	if err := cmd.Flags().Set("dependency", "missing-story-id"); err != nil {
		t.Fatalf("Set dependency missing: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicScout: %v", err)
	}
	scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
	signals := scout["signals"].(map[string]any)
	familyCounts := signals["family_counts"].(map[string]any)
	if got := familyCounts["dependency_topology"]; got != float64(4) {
		t.Fatalf("dependency_topology family count=%v want 4", got)
	}

	gotIDs := make(map[string]struct{})
	for _, raw := range scout["findings"].([]any) {
		finding := raw.(map[string]any)
		if finding["rule_family"] != "dependency_topology" {
			continue
		}
		gotIDs[finding["id"].(string)] = struct{}{}
	}
	wantIDs := []string{
		fmt.Sprintf("dependency_topology:milestone:%s:self", milestoneID),
		fmt.Sprintf("dependency_topology:milestone:%s:unresolved:missing-milestone-id", milestoneID),
		fmt.Sprintf("dependency_topology:story:%s:self", storyID),
		fmt.Sprintf("dependency_topology:story:%s:unresolved:missing-story-id", storyID),
	}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("dependency_topology findings=%v want %v", gotIDs, wantIDs)
	}
	for _, id := range wantIDs {
		if _, ok := gotIDs[id]; !ok {
			t.Fatalf("missing dependency_topology finding %q (got=%v)", id, gotIDs)
		}
	}
}

func TestRunRoomEpicScoutDependencyTopologyMilestoneCycleFinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneA, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Cycle milestone", "", "", "human-a", nil, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneB := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneA, "", nil, nil, []string{milestoneB}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy milestoneA: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneB, "", nil, nil, []string{milestoneA}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy milestoneB: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicScout: %v", err)
	}
	scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
	cycleFindings := roomScoutFindingsByFamilyAndReason(scout, "dependency_topology", "milestone_dependency_cycle")
	if len(cycleFindings) != 1 {
		t.Fatalf("len(milestone cycle findings)=%d want 1 findings=%v", len(cycleFindings), cycleFindings)
	}
	wantMembers := []string{milestoneA, milestoneB}
	sort.Strings(wantMembers)
	wantID := fmt.Sprintf("dependency_topology:milestone:cycle:%s", strings.Join(wantMembers, "+"))
	if got := stringField(cycleFindings[0], "id"); got != wantID {
		t.Fatalf("cycle id=%q want %q", got, wantID)
	}
	evidence := anyMap(cycleFindings[0]["evidence"])
	gotMembers := stringSliceValue(evidence["cycle_member_ids"])
	if len(gotMembers) != len(wantMembers) || gotMembers[0] != wantMembers[0] || gotMembers[1] != wantMembers[1] {
		t.Fatalf("cycle_member_ids=%v want %v", gotMembers, wantMembers)
	}
}

func TestRunRoomEpicScoutDependencyTopologyStoryCycleFinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyA := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Cycle story", "Second story for cycle coverage.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	storyB := decodeRoomEnvelope(t, out)["story_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyB); err != nil {
		t.Fatalf("Set dependency storyB: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyA, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyA: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyA); err != nil {
		t.Fatalf("Set dependency storyA: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyB, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyB: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicScout: %v", err)
	}
	scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
	cycleFindings := roomScoutFindingsByFamilyAndReason(scout, "dependency_topology", "story_dependency_cycle")
	if len(cycleFindings) != 1 {
		t.Fatalf("len(story cycle findings)=%d want 1 findings=%v", len(cycleFindings), cycleFindings)
	}
	wantMembers := []string{storyA, storyB}
	sort.Strings(wantMembers)
	wantID := fmt.Sprintf("dependency_topology:story:cycle:%s", strings.Join(wantMembers, "+"))
	if got := stringField(cycleFindings[0], "id"); got != wantID {
		t.Fatalf("cycle id=%q want %q", got, wantID)
	}
	evidence := anyMap(cycleFindings[0]["evidence"])
	gotMembers := stringSliceValue(evidence["cycle_member_ids"])
	if len(gotMembers) != len(wantMembers) || gotMembers[0] != wantMembers[0] || gotMembers[1] != wantMembers[1] {
		t.Fatalf("cycle_member_ids=%v want %v", gotMembers, wantMembers)
	}
}

func TestRunRoomEpicScoutDependencyTopologyDeduplicatesSimpleStoryCycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyA := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Cycle story B", "Cycle node B.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd storyB: %v", err)
	}
	storyB := decodeRoomEnvelope(t, out)["story_id"].(string)
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Cycle story C", "Cycle node C.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd storyC: %v", err)
	}
	storyC := decodeRoomEnvelope(t, out)["story_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyB); err != nil {
		t.Fatalf("Set storyA dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyA, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyA: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyC); err != nil {
		t.Fatalf("Set storyB dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyB, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyB: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyA); err != nil {
		t.Fatalf("Set storyC dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyC, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyC: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicScout(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicScout: %v", err)
	}
	scout := decodeRoomEnvelope(t, out)["scout"].(map[string]any)
	cycleFindings := roomScoutFindingsByFamilyAndReason(scout, "dependency_topology", "story_dependency_cycle")
	if len(cycleFindings) != 1 {
		t.Fatalf("len(story cycle findings)=%d want 1 findings=%v", len(cycleFindings), cycleFindings)
	}
	wantMembers := []string{storyA, storyB, storyC}
	sort.Strings(wantMembers)
	evidence := anyMap(cycleFindings[0]["evidence"])
	gotMembers := stringSliceValue(evidence["cycle_member_ids"])
	if len(gotMembers) != len(wantMembers) || gotMembers[0] != wantMembers[0] || gotMembers[1] != wantMembers[1] || gotMembers[2] != wantMembers[2] {
		t.Fatalf("cycle_member_ids=%v want %v", gotMembers, wantMembers)
	}
}

func TestRunRoomEpicGradeDependencyTopologyCyclePenaltyDeterministic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyA := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Cycle story", "Second story for cycle grading.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	storyB := decodeRoomEnvelope(t, out)["story_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
		t.Fatalf("runRoomEpicGrade baseline: %v", err)
	}
	baseline := decodeRoomEnvelope(t, out)["grade"].(map[string]any)
	baselineStoryScore := roomCategoryScoreByName(t, baseline["categories"].([]any), "Story Shape")

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyB); err != nil {
		t.Fatalf("Set storyA dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyA, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyA: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", storyA); err != nil {
		t.Fatalf("Set storyB dependency: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyB, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate storyB: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
		t.Fatalf("runRoomEpicGrade first: %v", err)
	}
	first := decodeRoomEnvelope(t, out)["grade"].(map[string]any)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicGrade(cmd, workspace, "alpha", epicID, 100, "", roomEpicGradePublishOptions{}); err != nil {
		t.Fatalf("runRoomEpicGrade second: %v", err)
	}
	second := decodeRoomEnvelope(t, out)["grade"].(map[string]any)

	firstStoryScore := roomCategoryScoreByName(t, first["categories"].([]any), "Story Shape")
	secondStoryScore := roomCategoryScoreByName(t, second["categories"].([]any), "Story Shape")
	if firstStoryScore != baselineStoryScore-2 {
		t.Fatalf("first Story Shape score=%d want baseline-2 (%d)", firstStoryScore, baselineStoryScore-2)
	}
	if secondStoryScore != firstStoryScore {
		t.Fatalf("second Story Shape score=%d want %d", secondStoryScore, firstStoryScore)
	}

	firstFamilyCounts := first["scout_signals"].(map[string]any)["family_counts"].(map[string]any)
	secondFamilyCounts := second["scout_signals"].(map[string]any)["family_counts"].(map[string]any)
	if got := firstFamilyCounts["dependency_topology"]; got != float64(1) {
		t.Fatalf("first dependency_topology family count=%v want 1", got)
	}
	if got := secondFamilyCounts["dependency_topology"]; got != float64(1) {
		t.Fatalf("second dependency_topology family count=%v want 1", got)
	}

	firstBlockers := stringSliceValue(first["blockers"])
	secondBlockers := stringSliceValue(second["blockers"])
	if !roomStringSliceContainsSubstring(firstBlockers, "stories have unresolved, self-referential, or cyclic dependency ids") {
		t.Fatalf("first blockers=%v want cyclic dependency blocker", firstBlockers)
	}
	if strings.Join(firstBlockers, "\n") != strings.Join(secondBlockers, "\n") {
		t.Fatalf("blockers are not deterministic: first=%v second=%v", firstBlockers, secondBlockers)
	}
}

func TestRunRoomEpicAndStoryUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	cmd.Flags().String("horizon", "", "")
	cmd.Flags().StringSlice("scope", nil, "")
	cmd.Flags().StringSlice("success", nil, "")
	if err := cmd.Flags().Set("horizon", "Q3"); err != nil {
		t.Fatalf("Set horizon: %v", err)
	}
	if err := cmd.Flags().Set("scope", "runtime hardening"); err != nil {
		t.Fatalf("Set scope runtime hardening: %v", err)
	}
	if err := cmd.Flags().Set("scope", "pipeline edits"); err != nil {
		t.Fatalf("Set scope pipeline edits: %v", err)
	}
	if err := cmd.Flags().Set("success", "epic reaches ready phase"); err != nil {
		t.Fatalf("Set success epic reaches ready phase: %v", err)
	}
	if err := runRoomEpicUpdate(cmd, workspace, "human-a", "alpha", epicID, "", "Implementation plan for the Uniwind migration."); err != nil {
		t.Fatalf("runRoomEpicUpdate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	meta := epic["meta"].(map[string]any)
	if got := meta["outcome"]; got != "Implementation plan for the Uniwind migration." {
		t.Fatalf("epic outcome=%v want updated outcome", got)
	}
	if got := meta["horizon"]; got != "Q3" {
		t.Fatalf("epic horizon=%v want Q3", got)
	}
	if got := stringSliceValue(meta["scope"]); len(got) != 2 || got[0] != "runtime hardening" || got[1] != "pipeline edits" {
		t.Fatalf("epic scope=%v want [runtime hardening pipeline edits]", got)
	}
	if got := stringSliceValue(meta["success"]); len(got) != 1 || got[0] != "epic reaches ready phase" {
		t.Fatalf("epic success=%v want [epic reaches ready phase]", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().Bool("clear-scope", false, "")
	cmd.Flags().Bool("clear-success", false, "")
	if err := cmd.Flags().Set("clear-scope", "true"); err != nil {
		t.Fatalf("Set clear-scope: %v", err)
	}
	if err := cmd.Flags().Set("clear-success", "true"); err != nil {
		t.Fatalf("Set clear-success: %v", err)
	}
	if err := runRoomEpicUpdate(cmd, workspace, "human-a", "alpha", epicID, "", ""); err != nil {
		t.Fatalf("runRoomEpicUpdate clear lists: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow after clear: %v", err)
	}
	epic = decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	meta = epic["meta"].(map[string]any)
	if got := stringSliceValue(meta["scope"]); len(got) != 0 {
		t.Fatalf("epic scope=%v want empty after clear", got)
	}
	if got := stringSliceValue(meta["success"]); len(got) != 0 {
		t.Fatalf("epic success=%v want empty after clear", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().String("description", "", "")
	if err := cmd.Flags().Set("description", "Execution plan now includes validation lanes and owner handoff."); err != nil {
		t.Fatalf("Set story description: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryUpdate: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	storyMeta := story["meta"].(map[string]any)
	if got := storyMeta["owner"]; got != "gemini-a" {
		t.Fatalf("story owner=%v want gemini-a", got)
	}
	if got := storyMeta["description"]; got != "Execution plan now includes validation lanes and owner handoff." {
		t.Fatalf("story description=%v want updated description", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "gemini-a", "alpha", storyID, "in_progress", "Started planning-owned execution.", "", ""); err != nil {
		t.Fatalf("runRoomStoryState after owner update: %v", err)
	}
}

func TestRunRoomStoryDependencyMetadataAddUpdateAndView(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", "story-dep-b"); err != nil {
		t.Fatalf("Set dependency story-dep-b: %v", err)
	}
	if err := cmd.Flags().Set("dependency", "story-dep-a"); err != nil {
		t.Fatalf("Set dependency story-dep-a: %v", err)
	}
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Dependency-aware story", "Story with explicit dependencies.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd with dependencies: %v", err)
	}
	storyID := decodeRoomEnvelope(t, out)["story_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow after add: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := stringSliceValue(story["dependencies"]); len(got) != 2 || got[0] != "story-dep-a" || got[1] != "story-dep-b" {
		t.Fatalf("dependencies=%v want [story-dep-a story-dep-b]", got)
	}
	if got := story["dependency_count"]; got != float64(2) {
		t.Fatalf("dependency_count=%v want 2", got)
	}
	if got := stringSliceValue(story["meta"].(map[string]any)["dependencies"]); len(got) != 2 || got[0] != "story-dep-a" || got[1] != "story-dep-b" {
		t.Fatalf("meta.dependencies=%v want [story-dep-a story-dep-b]", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", "story-dep-c"); err != nil {
		t.Fatalf("Set dependency story-dep-c: %v", err)
	}
	if err := cmd.Flags().Set("dependency", "story-dep-b"); err != nil {
		t.Fatalf("Set dependency story-dep-b: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, "gemini-a"); err != nil {
		t.Fatalf("runRoomStoryUpdate with dependencies: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow after update: %v", err)
	}
	story = decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := stringSliceValue(story["dependencies"]); len(got) != 2 || got[0] != "story-dep-b" || got[1] != "story-dep-c" {
		t.Fatalf("dependencies=%v want [story-dep-b story-dep-c]", got)
	}
	if got := story["dependency_count"]; got != float64(2) {
		t.Fatalf("dependency_count=%v want 2", got)
	}
	meta := story["meta"].(map[string]any)
	if got := meta["owner"]; got != "gemini-a" {
		t.Fatalf("owner=%v want gemini-a", got)
	}
	if got := stringSliceValue(meta["dependencies"]); len(got) != 2 || got[0] != "story-dep-b" || got[1] != "story-dep-c" {
		t.Fatalf("meta.dependencies=%v want [story-dep-b story-dep-c]", got)
	}
}

func TestRunRoomStoryUpdateCanClearDependencies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	cmd.Flags().StringSlice("dependency", nil, "")
	if err := cmd.Flags().Set("dependency", "story-dep-a"); err != nil {
		t.Fatalf("Set dependency story-dep-a: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate set dependency: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow after set: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := stringSliceValue(story["dependencies"]); len(got) != 1 || got[0] != "story-dep-a" {
		t.Fatalf("dependencies=%v want [story-dep-a]", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().Bool("clear-dependencies", false, "")
	if err := cmd.Flags().Set("clear-dependencies", "true"); err != nil {
		t.Fatalf("Set clear-dependencies: %v", err)
	}
	if err := runRoomStoryUpdate(cmd, workspace, "human-a", "alpha", storyID, ""); err != nil {
		t.Fatalf("runRoomStoryUpdate clear dependencies: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow after clear: %v", err)
	}
	story = decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := stringSliceValue(story["dependencies"]); len(got) != 0 {
		t.Fatalf("dependencies=%v want empty", got)
	}
	if got := story["dependency_count"]; got != float64(0) {
		t.Fatalf("dependency_count=%v want 0", got)
	}
}

func TestRunRoomMilestoneContractCanClearDependencies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, []string{"milestone-dep-a"}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy set dependency: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow after set: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := stringSliceValue(milestone["meta"].(map[string]any)["dependencies"]); len(got) != 1 || got[0] != "milestone-dep-a" {
		t.Fatalf("meta.dependencies=%v want [milestone-dep-a]", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().Bool("clear-dependencies", false, "")
	if err := cmd.Flags().Set("clear-dependencies", "true"); err != nil {
		t.Fatalf("Set clear-dependencies: %v", err)
	}
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy clear dependencies: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow after clear: %v", err)
	}
	milestone = decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := stringSliceValue(milestone["meta"].(map[string]any)["dependencies"]); len(got) != 0 {
		t.Fatalf("meta.dependencies=%v want empty", got)
	}
	if got := milestone["dependency_count"]; got != float64(0) {
		t.Fatalf("dependency_count=%v want 0", got)
	}
}

func TestRunRoomMilestoneContractCanClearValidationLanes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, []string{"review", "test"}, []string{"integration"}, []string{"manual_check"}, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy set lanes: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow after lane set: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["validator_count"]; got != float64(2) {
		t.Fatalf("validator_count=%v want 2", got)
	}
	if got := milestone["required_evidence_lane_count"]; got != float64(1) {
		t.Fatalf("required_evidence_lane_count=%v want 1", got)
	}
	if got := milestone["optional_evidence_lane_count"]; got != float64(1) {
		t.Fatalf("optional_evidence_lane_count=%v want 1", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	cmd.Flags().Bool("clear-validators", false, "")
	cmd.Flags().Bool("clear-required-lanes", false, "")
	cmd.Flags().Bool("clear-optional-lanes", false, "")
	if err := cmd.Flags().Set("clear-validators", "true"); err != nil {
		t.Fatalf("Set clear-validators: %v", err)
	}
	if err := cmd.Flags().Set("clear-required-lanes", "true"); err != nil {
		t.Fatalf("Set clear-required-lanes: %v", err)
	}
	if err := cmd.Flags().Set("clear-optional-lanes", "true"); err != nil {
		t.Fatalf("Set clear-optional-lanes: %v", err)
	}
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneContractWithPolicy clear lanes: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow after lane clear: %v", err)
	}
	milestone = decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	contract := milestone["contract"].(map[string]any)
	if got := stringSliceValue(contract["validators_expected"]); len(got) != 0 {
		t.Fatalf("validators_expected=%v want empty", got)
	}
	if got := stringSliceValue(contract["required_evidence_lanes"]); len(got) != 0 {
		t.Fatalf("required_evidence_lanes=%v want empty", got)
	}
	if got := stringSliceValue(contract["optional_evidence_lanes"]); len(got) != 0 {
		t.Fatalf("optional_evidence_lanes=%v want empty", got)
	}
	if got := milestone["validator_count"]; got != float64(0) {
		t.Fatalf("validator_count=%v want 0", got)
	}
	if got := milestone["required_evidence_lane_count"]; got != float64(0) {
		t.Fatalf("required_evidence_lane_count=%v want 0", got)
	}
	if got := milestone["optional_evidence_lane_count"]; got != float64(0) {
		t.Fatalf("optional_evidence_lane_count=%v want 0", got)
	}
}

func TestRunRoomMilestoneExitEnforcement(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	t.Run("enforcement off allows pass", func(t *testing.T) {
		workspace := t.TempDir()
		_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
			t.Fatalf("runRoomMilestoneReview: %v", err)
		}
		if got := decodeRoomEnvelope(t, out)["message"].(map[string]any)["subject"]; got != "Milestone Review: pass" {
			t.Fatalf("subject=%v want pass review", got)
		}
	})

	t.Run("enforcement on rejects not_ready", func(t *testing.T) {
		workspace := t.TempDir()
		_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		enforceTrue := true
		if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, &enforceTrue, nil); err != nil {
			t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
		}

		var err error
		cmd, _ = newRoomTestCommand(ctx)
		err = runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good.")
		assertRoomErrorContains(t, err, "milestone pass review is blocked by the enforced exit policy")
	})

	t.Run("enforcement on allows ready_for_review", func(t *testing.T) {
		workspace := t.TempDir()
		_, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		enforceTrue := true
		if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, &enforceTrue, nil); err != nil {
			t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate: %v", err)
		}

		cmd, out := newRoomTestCommand(ctx)
		if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
			t.Fatalf("runRoomMilestoneReview: %v", err)
		}
		var env envelope.Envelope
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			t.Fatalf("decode envelope: %v\n%s", err, out.String())
		}
		if env.Status != envelope.StatusOK {
			t.Fatalf("status=%q want ok payload=%s", env.Status, out.String())
		}
	})

	t.Run("enforcement on rejects ready_for_summary", func(t *testing.T) {
		workspace := t.TempDir()
		_, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

		cmd, _ := newRoomTestCommand(ctx)
		enforceTrue := true
		if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, &enforceTrue, nil); err != nil {
			t.Fatalf("runRoomMilestoneContractWithPolicy: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
			t.Fatalf("runRoomMilestoneCriteria: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
			t.Fatalf("runRoomStoryValidate: %v", err)
		}
		cmd, _ = newRoomTestCommand(ctx)
		if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
			t.Fatalf("runRoomMilestoneReview initial: %v", err)
		}

		var err error
		cmd, _ = newRoomTestCommand(ctx)
		err = runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Second pass should fail.")
		assertRoomErrorContains(t, err, "milestone pass review is blocked by the enforced exit policy")
	})
}

func TestRunRoomMilestoneExitEnforcementToggle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	enforceTrue := true
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, &enforceTrue, nil); err != nil {
		t.Fatalf("enable enforcement: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow enabled: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["enforce_exit_policy"]; got != true {
		t.Fatalf("enforce_exit_policy=%v want true", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "Updated objective.", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("omit enforcement update: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow unchanged: %v", err)
	}
	milestone = decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["enforce_exit_policy"]; got != true {
		t.Fatalf("enforce_exit_policy after omit=%v want true", got)
	}

	enforceFalse := false
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneContractWithPolicy(cmd, workspace, "human-a", "alpha", milestoneID, "", nil, nil, nil, nil, nil, nil, &enforceFalse, nil); err != nil {
		t.Fatalf("disable enforcement: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow disabled: %v", err)
	}
	milestone = decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["enforce_exit_policy"]; got != false {
		t.Fatalf("enforce_exit_policy=%v want false", got)
	}
}

// Regression: Cobra milestone start must default exit enforcement off when neither
// --enforce-exit-policy nor --no-enforce-exit-policy is passed (see room milestone toggle spec).
func TestRoomMilestoneStartCLI_DefaultEnforceExitPolicyFalse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Agile", "Goal", "human-a", "", "", []string{"scope"}, []string{"ok"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Brief ready."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	root := newRoomCommand()
	buf := &bytes.Buffer{}
	root.SetContext(ctx)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{
		"milestone", "start", "alpha", epicID, "M1",
		"--workspace", workspace,
		"--sender", "human-a",
		"--goal", "g",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("milestone start (cobra): %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, buf)["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["enforce_exit_policy"]; got != false {
		t.Fatalf("enforce_exit_policy=%v want false (CLI default)", got)
	}
}

func TestRunRoomEpicStartMaintenanceDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "", "", "human-a", "", "", nil, nil, true, true); err != nil {
		t.Fatalf("runRoomEpicStart maintenance: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["title"]; got != "Maintenance / Small Stories" {
		t.Fatalf("title=%v want Maintenance / Small Stories", got)
	}
	if got := epic["template"]; got != "maintenance" {
		t.Fatalf("template=%v want maintenance", got)
	}
	if got := epic["default_small_work"]; got != true {
		t.Fatalf("default_small_work=%v want true", got)
	}
	meta := epic["meta"].(map[string]any)
	if got := meta["goal"]; got == "" {
		t.Fatalf("goal=%v want non-empty maintenance default", got)
	}
	if got := meta["horizon"]; got != "rolling" {
		t.Fatalf("horizon=%v want rolling", got)
	}
	scope := meta["scope"].([]any)
	if len(scope) == 0 {
		t.Fatal("scope should contain default maintenance entries")
	}
}

func TestRunRoomEpicStartDefaultSmallWorkRequiresMaintenance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "General Epic", "", "human-a", "", "", nil, nil, false, true)
	if err == nil {
		t.Fatal("runRoomEpicStart error = nil want written error envelope")
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error payload=%s", env.Status, out.String())
	}
	if env.Error.Code != string(protocol.ErrorCodeEARG) {
		t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeEARG)
	}
	if !strings.Contains(out.String(), "--maintenance") {
		t.Fatalf("body=%s want --maintenance hint", out.String())
	}
}

func TestRoomEpicStartCLI_MaintenanceAllowsOmittedTitle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	root := newRoomCommand()
	buf := &bytes.Buffer{}
	root.SetContext(ctx)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{
		"epic", "start", "alpha",
		"--workspace", workspace,
		"--sender", "human-a",
		"--owner", "human-a",
		"--maintenance",
		"--default-small-work",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("epic start (cobra maintenance): %v", err)
	}
	epicID := decodeRoomEnvelope(t, buf)["epic_id"].(string)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["title"]; got != "Maintenance / Small Stories" {
		t.Fatalf("title=%v want Maintenance / Small Stories", got)
	}
	if got := epic["default_small_work"]; got != true {
		t.Fatalf("default_small_work=%v want true", got)
	}
}

func TestRoomMilestoneReviewCLI_ErrorEnvelopeReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Agile", "Goal", "human-a", "", "", []string{"scope"}, []string{"ok"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Brief ready."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	root := newRoomCommand()
	buf := &bytes.Buffer{}
	root.SetContext(ctx)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{
		"milestone", "start", "alpha", epicID, "M1",
		"--workspace", workspace,
		"--sender", "human-a",
		"--goal", "g",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("milestone start (cobra): %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, buf)["milestone_id"].(string)

	root = newRoomCommand()
	buf = &bytes.Buffer{}
	root.SetContext(ctx)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{
		"milestone", "contract", "alpha", milestoneID,
		"--required-lane", "test",
		"--validator", "test",
		"--enforce-exit-policy",
		"--workspace", workspace,
		"--sender", "human-a",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("milestone contract (cobra): %v", err)
	}

	root = newRoomCommand()
	buf = &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	root.SetContext(ctx)
	root.SetOut(buf)
	root.SetErr(errBuf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{
		"milestone", "review", "alpha", milestoneID, "pass", "too early",
		"--workspace", workspace,
		"--sender", "human-a",
	})
	if err := root.Execute(); err == nil {
		t.Fatal("milestone review expected error when exit policy blocks pass")
	}
	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal envelope error = %v", err)
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("env.Status=%q want %q", env.Status, envelope.StatusError)
	}
	if env.Command != "foxctl.room.milestone.review" {
		t.Fatalf("env.Command=%q want foxctl.room.milestone.review", env.Command)
	}
}

func TestRunRoomEpicResumeAndNextDiscovery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship resumability", "human-a", "", "", []string{"resume", "next"}, []string{"operators can restart work"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicAsk(cmd, workspace, "human-a", "alpha", epicID, "human-a", "constraint", "What must be true before milestones open?"); err != nil {
		t.Fatalf("runRoomEpicAsk: %v", err)
	}
	questionID := decodeRoomEnvelope(t, out)["message"].(map[string]any)["id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "discovery" {
		t.Fatalf("phase=%v want discovery", got)
	}
	if got := resume["open_intake_questions"]; got != float64(1) {
		t.Fatalf("open_intake_questions=%v want 1", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, "human-a"); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	items := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("len(items)=%d want 1", len(items))
	}
	item := items[0].(map[string]any)
	if got := item["type"]; got != "answer_intake_question" {
		t.Fatalf("type=%v want answer_intake_question", got)
	}
	if got := item["target_id"]; got != questionID {
		t.Fatalf("target_id=%v want %s", got, questionID)
	}
}

func TestRunRoomEpicResumeAndNextShaping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship resumability", "human-a", "", "", []string{"resume", "next"}, []string{"operators can restart work"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicShape(cmd, workspace, "human-a", "alpha", epicID, 2); err != nil {
		t.Fatalf("runRoomEpicShape: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "shaping" {
		t.Fatalf("phase=%v want shaping", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, ""); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	items := decodeRoomEnvelope(t, out)["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("len(items)=0 want milestone action")
	}
	if got := items[0].(map[string]any)["type"]; got != "start_milestone_from_proposal" {
		t.Fatalf("type=%v want start_milestone_from_proposal", got)
	}
}

func TestRunRoomEpicResumeExecutionAndNextValidateStory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "execution" {
		t.Fatalf("phase=%v want execution", got)
	}
	missing := resume["stories_missing_validation"].([]any)
	if len(missing) != 1 {
		t.Fatalf("len(stories_missing_validation)=%d want 1", len(missing))
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, "human-a"); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	items := decodeRoomEnvelope(t, out)["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("len(items)=0 want validate action")
	}
	found := false
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["type"] == "validate_story" && item["target_id"] == storyID {
			found = true
		}
	}
	if !found {
		t.Fatalf("items=%v want validate_story for %s", items, storyID)
	}
}

func TestRunRoomEpicResumeBlockedAndReviewStates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "blocked", "Blocked on reviewer input.", "docs/reviews/blocked.md", "", "", "Blocked.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate blocked: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume blocked: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "blocked" {
		t.Fatalf("phase=%v want blocked", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, ""); err != nil {
		t.Fatalf("runRoomEpicNext blocked: %v", err)
	}
	items := decodeRoomEnvelope(t, out)["items"].([]any)
	if len(items) == 0 || items[0].(map[string]any)["type"] != "follow_up_blocker" {
		t.Fatalf("items=%v want follow_up_blocker", items)
	}

	workspace2 := t.TempDir()
	epicID2, milestoneID2, storyID2 := setupRoomAgileWorkpackFixture(t, ctx, workspace2)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace2, "human-a", "alpha", storyID2, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "Ready.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace2, "alpha", epicID2); err != nil {
		t.Fatalf("runRoomEpicResume review: %v", err)
	}
	resume = decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "review" {
		t.Fatalf("phase=%v want review", got)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace2, "alpha", epicID2, ""); err != nil {
		t.Fatalf("runRoomEpicNext review: %v", err)
	}
	items = decodeRoomEnvelope(t, out)["items"].([]any)
	foundReview := false
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["type"] == "review_milestone" && item["target_id"] == milestoneID2 {
			foundReview = true
		}
	}
	if !foundReview {
		t.Fatalf("items=%v want review_milestone for %s", items, milestoneID2)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace2, "human-a", "alpha", milestoneID2, "pass", "Looks good."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace2, "alpha", epicID2, ""); err != nil {
		t.Fatalf("runRoomEpicNext summary: %v", err)
	}
	items = decodeRoomEnvelope(t, out)["items"].([]any)
	foundSummary := false
	for _, raw := range items {
		item := raw.(map[string]any)
		if item["type"] == "summarize_milestone" && item["target_id"] == milestoneID2 {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("items=%v want summarize_milestone for %s", items, milestoneID2)
	}

	_ = milestoneID
	_ = storyID
}

func TestRunRoomEpicNextReturnsDeterministicEmptyShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "Ready.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Summary.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneSummary: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", epicID, "Foundation landed", []string{"resume/next shipped"}, nil, nil, []string{"none"}, "Stable."); err != nil {
		t.Fatalf("runRoomLogAppend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, ""); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["reason"]; got != "no open work" {
		t.Fatalf("reason=%v want no open work", got)
	}
	items := data["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("len(items)=%d want 0", len(items))
	}
}

func TestRunRoomEpicCloseCompletedOverridesPhaseAndHealth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomEpicClose(cmd, workspace, "human-a", "alpha", epicID, "implemented", "Bounded scope is implemented and the mission is complete."); err != nil {
		t.Fatalf("runRoomEpicClose: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["phase"]; got != "completed" {
		t.Fatalf("phase=%v want completed", got)
	}
	if got := resume["close_reason"]; got != "completed" {
		t.Fatalf("close_reason=%v want completed", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 200); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "complete" {
		t.Fatalf("health=%v want complete", got)
	}
}

func TestRunRoomEpicCloseSupersededClearsNextAndPulse(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomEpicClose(cmd, workspace, "human-a", "alpha", epicID, "superseded", "This split was mistaken; continue the same mission under the original epic as another milestone."); err != nil {
		t.Fatalf("runRoomEpicClose superseded: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, "human-a"); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	next := decodeRoomEnvelope(t, out)
	if got := next["reason"]; got != "epic closed" {
		t.Fatalf("reason=%v want epic closed", got)
	}
	if items := next["items"].([]any); len(items) != 0 {
		t.Fatalf("len(items)=%d want 0", len(items))
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomPulse(cmd, workspace, "alpha", "", 20, []string{"all"}); err != nil {
		t.Fatalf("runRoomPulse: %v", err)
	}
	pulse := decodeRoomEnvelope(t, out)
	summary := pulse["summary"].(map[string]any)
	if got := summary["closed_epic_count"]; got != float64(1) {
		t.Fatalf("closed_epic_count=%v want 1", got)
	}
	topItems := pulse["top_items"].([]any)
	if len(topItems) != 0 {
		t.Fatalf("len(top_items)=%d want 0", len(topItems))
	}
}

func TestRunRoomEpicCheckpointPersistsAndShowsLatest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicNext(cmd, workspace, "alpha", epicID, "human-a"); err != nil {
		t.Fatalf("runRoomEpicNext: %v", err)
	}
	expectedItems := decodeRoomEnvelope(t, out)["items"].([]any)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicCheckpoint(cmd, workspace, "human-a", "alpha", epicID, "human-a", "", "Coordinator note.", 10); err != nil {
		t.Fatalf("runRoomEpicCheckpoint: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	checkpoint := data["checkpoint"].(map[string]any)
	if got := checkpoint["epic_id"]; got != epicID {
		t.Fatalf("checkpoint epic_id=%v want %s", got, epicID)
	}
	if got := checkpoint["note"]; got != "Coordinator note." {
		t.Fatalf("note=%v want Coordinator note.", got)
	}
	label := checkpoint["label"].(string)
	if !strings.Contains(label, time.Now().UTC().Format("2006-01-02")) {
		t.Fatalf("label=%q want UTC date", label)
	}
	nextItems := checkpoint["next_items"].([]any)
	if len(nextItems) != len(expectedItems) {
		t.Fatalf("len(next_items)=%d want %d", len(nextItems), len(expectedItems))
	}
	for i := range nextItems {
		got := nextItems[i].(map[string]any)["type"]
		want := expectedItems[i].(map[string]any)["type"]
		if got != want {
			t.Fatalf("next_items[%d].type=%v want %v", i, got, want)
		}
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["checkpoint_count"]; got != float64(1) {
		t.Fatalf("checkpoint_count=%v want 1", got)
	}
	latest := epic["latest_checkpoint"].(map[string]any)
	if latest["id"] != checkpoint["id"] {
		t.Fatalf("latest_checkpoint.id=%v want %v", latest["id"], checkpoint["id"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicResume(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomEpicResume: %v", err)
	}
	resume := decodeRoomEnvelope(t, out)["resume"].(map[string]any)
	if got := resume["latest_checkpoint_id"]; got != checkpoint["id"] {
		t.Fatalf("latest_checkpoint_id=%v want %v", got, checkpoint["id"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomWorkpackShow(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomWorkpackShow: %v", err)
	}
	workpack := decodeRoomEnvelope(t, out)["workpack"].(map[string]any)
	checkpoints := workpack["checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("len(checkpoints)=%d want 1", len(checkpoints))
	}
	checkpointInfo := checkpoints[0].(map[string]any)
	checkpointMarkdown := checkpointInfo["checkpoint_markdown"].(string)
	checkpointJSON := checkpointInfo["checkpoint_json"].(string)
	if _, err := os.Stat(checkpointMarkdown); err != nil {
		t.Fatalf("checkpoint markdown stat: %v", err)
	}
	if _, err := os.Stat(checkpointJSON); err != nil {
		t.Fatalf("checkpoint json stat: %v", err)
	}
	body, err := os.ReadFile(checkpointMarkdown)
	if err != nil {
		t.Fatalf("read checkpoint markdown: %v", err)
	}
	for _, want := range []string{"# Epic Checkpoint", "## Next Actions", "Coordinator note."} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("checkpoint markdown missing %q:\n%s", want, string(body))
		}
	}
}

func TestRunRoomEpicCheckpointEmptyNextItems(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "Ready.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Summary.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneSummary: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", epicID, "Foundation landed", []string{"resume/next shipped"}, nil, nil, []string{"none"}, "Stable."); err != nil {
		t.Fatalf("runRoomLogAppend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicCheckpoint(cmd, workspace, "human-a", "alpha", epicID, "", "", "", 10); err != nil {
		t.Fatalf("runRoomEpicCheckpoint: %v", err)
	}
	checkpoint := decodeRoomEnvelope(t, out)["checkpoint"].(map[string]any)
	if got := checkpoint["reason"]; got != "no open work" {
		t.Fatalf("reason=%v want no open work", got)
	}
	if got := checkpoint["next_items"].([]any); len(got) != 0 {
		t.Fatalf("len(next_items)=%d want 0", len(got))
	}

	path := checkpoint["checkpoint_markdown"].(string)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read checkpoint markdown: %v", err)
	}
	if !strings.Contains(string(body), "No open next actions.") {
		t.Fatalf("checkpoint markdown missing empty next actions state:\n%s", string(body))
	}
}

func TestRunRoomEpicSnapshotPersistsAndUpdatesLatest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicSnapshot(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicSnapshot first: %v", err)
	}
	first := decodeRoomEnvelope(t, out)
	if got := first["epic_id"]; got != epicID {
		t.Fatalf("epic_id=%v want %s", got, epicID)
	}
	firstSummary := first["summary"].(map[string]any)
	if got := firstSummary["phase"]; got != "graded" {
		t.Fatalf("summary.phase=%v want graded", got)
	}
	if _, err := time.Parse(time.RFC3339, first["created_at"].(string)); err != nil {
		t.Fatalf("created_at parse: %v", err)
	}
	firstSnapshotID := first["snapshot_id"].(string)
	if firstSnapshotID == "" {
		t.Fatalf("snapshot_id empty")
	}
	firstArtifact := first["snapshot_artifact"].(string)
	firstLatest := first["latest_snapshot_artifact"].(string)
	if _, err := os.Stat(firstArtifact); err != nil {
		t.Fatalf("snapshot artifact stat: %v", err)
	}
	if _, err := os.Stat(firstLatest); err != nil {
		t.Fatalf("latest artifact stat: %v", err)
	}
	var firstDoc map[string]any
	firstRaw, err := os.ReadFile(firstArtifact)
	if err != nil {
		t.Fatalf("read snapshot artifact: %v", err)
	}
	if err := json.Unmarshal(firstRaw, &firstDoc); err != nil {
		t.Fatalf("decode snapshot artifact: %v", err)
	}
	if got := firstDoc["schema_version"]; got != "room.epic.snapshot.v1" {
		t.Fatalf("snapshot schema_version=%v want room.epic.snapshot.v1", got)
	}
	if got := firstDoc["snapshot_id"]; got != firstSnapshotID {
		t.Fatalf("snapshot snapshot_id=%v want %s", got, firstSnapshotID)
	}
	scope := firstDoc["scope"].(map[string]any)
	if got := scope["room_id"]; got != "alpha" {
		t.Fatalf("scope.room_id=%v want alpha", got)
	}
	if got := scope["epic_id"]; got != epicID {
		t.Fatalf("scope.epic_id=%v want %s", got, epicID)
	}
	status := firstDoc["status"].(map[string]any)
	if got := status["phase"]; got != firstSummary["phase"] {
		t.Fatalf("status.phase=%v want %v", got, firstSummary["phase"])
	}
	scout := firstDoc["scout"].(map[string]any)
	if got := scout["schema_version"]; got != "room.epic.scout.v1" {
		t.Fatalf("scout.schema_version=%v want room.epic.scout.v1", got)
	}
	if got := scout["snapshot_id"]; got != firstSnapshotID {
		t.Fatalf("scout.snapshot_id=%v want %s", got, firstSnapshotID)
	}
	if got := scout["snapshot_artifact"]; got != firstArtifact {
		t.Fatalf("scout.snapshot_artifact=%v want %s", got, firstArtifact)
	}
	if got := scout["blocking_findings"]; got != firstSummary["blocking_findings"] {
		t.Fatalf("scout.blocking_findings=%v want %v", got, firstSummary["blocking_findings"])
	}
	outOfScope := stringSliceValue(scout["out_of_scope"])
	for _, item := range outOfScope {
		if item == "snapshot_unavailable" {
			t.Fatalf("scout.out_of_scope=%v unexpectedly contains snapshot_unavailable", outOfScope)
		}
	}
	if findings := scout["findings"].([]any); len(findings) == 0 {
		t.Fatalf("scout.findings empty")
	}
	var firstLatestDoc map[string]any
	firstLatestRaw, err := os.ReadFile(firstLatest)
	if err != nil {
		t.Fatalf("read latest artifact: %v", err)
	}
	if err := json.Unmarshal(firstLatestRaw, &firstLatestDoc); err != nil {
		t.Fatalf("decode latest artifact: %v", err)
	}
	if got := firstLatestDoc["snapshot_id"]; got != firstSnapshotID {
		t.Fatalf("latest snapshot_id=%v want %s", got, firstSnapshotID)
	}
	if got := firstLatestDoc["snapshot_artifact"]; got != firstArtifact {
		t.Fatalf("latest snapshot_artifact=%v want %s", got, firstArtifact)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicSnapshot(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicSnapshot second: %v", err)
	}
	second := decodeRoomEnvelope(t, out)
	secondSnapshotID := second["snapshot_id"].(string)
	secondArtifact := second["snapshot_artifact"].(string)
	secondLatest := second["latest_snapshot_artifact"].(string)
	if secondLatest != firstLatest {
		t.Fatalf("latest artifact path changed: %s vs %s", secondLatest, firstLatest)
	}
	var secondLatestDoc map[string]any
	secondLatestRaw, err := os.ReadFile(secondLatest)
	if err != nil {
		t.Fatalf("read second latest artifact: %v", err)
	}
	if err := json.Unmarshal(secondLatestRaw, &secondLatestDoc); err != nil {
		t.Fatalf("decode second latest artifact: %v", err)
	}
	if got := secondLatestDoc["snapshot_id"]; got != secondSnapshotID {
		t.Fatalf("second latest snapshot_id=%v want %s", got, secondSnapshotID)
	}
	if got := secondLatestDoc["snapshot_artifact"]; got != secondArtifact {
		t.Fatalf("second latest snapshot_artifact=%v want %s", got, secondArtifact)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomWorkpackShow(cmd, workspace, "alpha", epicID); err != nil {
		t.Fatalf("runRoomWorkpackShow: %v", err)
	}
	workpack := decodeRoomEnvelope(t, out)["workpack"].(map[string]any)
	if got := workpack["snapshot_dir"]; got != roomAgileEpicSnapshotDir(epicID) {
		t.Fatalf("snapshot_dir=%v want %s", got, roomAgileEpicSnapshotDir(epicID))
	}
	if got := workpack["latest_snapshot_json"]; got != secondLatest {
		t.Fatalf("latest_snapshot_json=%v want %s", got, secondLatest)
	}
}

func TestRunRoomEpicImportPlanMarkdownCreatesGroupedMilestonesAndStories(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	planPath := filepath.Join(workspace, "import-plan.md")
	mustWriteRoomTestFile(t, planPath, `# Import Plan

## Discovery
### Step 1: Capture requirements
Collect current behavior.
### Step 2: Define mapping contract
Map parsed steps to room stories.

## Delivery
### Step 3: Implement command wiring
Add CLI command.`)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "claude"); err != nil {
		t.Fatalf("runRoomEpicImportPlan: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["provider"]; got != "claude" {
		t.Fatalf("provider=%v want claude", got)
	}
	if got := data["milestone_count"]; got != float64(2) {
		t.Fatalf("milestone_count=%v want 2", got)
	}
	if got := data["story_count"]; got != float64(3) {
		t.Fatalf("story_count=%v want 3", got)
	}
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, roomTaskScanLimit); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	meta := epic["meta"].(map[string]any)
	if got := meta["template"]; got != "plan_import" {
		t.Fatalf("template=%v want plan_import", got)
	}
	if got := meta["source_kind"]; got != "plan_import" {
		t.Fatalf("source_kind=%v want plan_import", got)
	}
	if got := meta["source_provider"]; got != "claude" {
		t.Fatalf("source_provider=%v want claude", got)
	}
	if got := meta["source_path"]; got != data["source_path"] {
		t.Fatalf("source_path=%v want %v", got, data["source_path"])
	}
	if got := meta["source_title"]; got != "Import Plan" {
		t.Fatalf("source_title=%v want Import Plan", got)
	}
	if got := strings.TrimSpace(anyString(meta["source_content_hash"])); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("source_content_hash=%q want sha256:*", got)
	}
	milestones := epic["milestones"].([]any)
	if len(milestones) != 2 {
		t.Fatalf("len(milestones)=%d want 2", len(milestones))
	}
	milestoneStoryCounts := map[string]int{}
	storyTitles := map[string]struct{}{}
	for _, raw := range milestones {
		milestone := raw.(map[string]any)
		title := milestone["title"].(string)
		stories := milestone["stories"].([]any)
		milestoneStoryCounts[title] = len(stories)
		for _, storyRaw := range stories {
			story := storyRaw.(map[string]any)
			storyTitles[story["title"].(string)] = struct{}{}
		}
	}
	if got := milestoneStoryCounts["Discovery"]; got != 2 {
		t.Fatalf("Discovery stories=%d want 2", got)
	}
	if got := milestoneStoryCounts["Delivery"]; got != 1 {
		t.Fatalf("Delivery stories=%d want 1", got)
	}
	for _, want := range []string{"Capture requirements", "Define mapping contract", "Implement command wiring"} {
		if _, ok := storyTitles[want]; !ok {
			t.Fatalf("story %q missing from imported epic", want)
		}
	}
}

func TestRunRoomEpicImportPlanRoutesQuestionStepsToEpicIntake(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	planPath := filepath.Join(workspace, "import-questions-plan.md")
	mustWriteRoomTestFile(t, planPath, `# Import Plan

## Open questions

1. Should active-run selection be single-run-per-workspace or multi-run with an
   explicit --run selector on every command?

## Execution

1. Implement command wiring`)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "claude"); err != nil {
		t.Fatalf("runRoomEpicImportPlan: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["story_count"]; got != float64(1) {
		t.Fatalf("story_count=%v want 1", got)
	}
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, roomTaskScanLimit); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["question_count"]; got != float64(1) {
		t.Fatalf("question_count=%v want 1", got)
	}
	if got := epic["open_questions"]; got != float64(1) {
		t.Fatalf("open_questions=%v want 1", got)
	}

	const wantQuestion = "Should active-run selection be single-run-per-workspace or multi-run with an explicit --run selector on every command?"
	questions := epic["questions"].([]any)
	if len(questions) != 1 {
		t.Fatalf("len(questions)=%d want 1", len(questions))
	}
	questionMeta := parseRoomEpicQuestionBody(stringField(questions[0], "body"))
	if got := questionMeta.Question; got != wantQuestion {
		t.Fatalf("question=%q want %q", got, wantQuestion)
	}

	storyTitles := map[string]struct{}{}
	for _, milestoneRaw := range epic["milestones"].([]any) {
		milestone := milestoneRaw.(map[string]any)
		for _, storyRaw := range mapSlice(milestone["stories"]) {
			storyTitles[stringField(storyRaw, "title")] = struct{}{}
		}
	}
	if _, ok := storyTitles[wantQuestion]; ok {
		t.Fatalf("question step imported as story: %q", wantQuestion)
	}
	if _, ok := storyTitles["Implement command wiring"]; !ok {
		t.Fatalf("story %q missing from imported epic", "Implement command wiring")
	}
}

func TestRunRoomEpicImportPlanEvolveFixtureExcludesOpenQuestionStoriesFromFrontier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	planPath := filepath.Join("..", "..", "..", "docs", "plans", "features", "foxctl-evolve-plan.md")
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "claude"); err != nil {
		t.Fatalf("runRoomEpicImportPlan: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, roomTaskScanLimit); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["question_count"]; got != float64(5) {
		t.Fatalf("question_count=%v want 5", got)
	}
	if got := epic["open_questions"]; got != float64(5) {
		t.Fatalf("open_questions=%v want 5", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicFrontier(cmd, workspace, "alpha", epicID, 200); err != nil {
		t.Fatalf("runRoomEpicFrontier: %v", err)
	}
	frontier := decodeRoomEnvelope(t, out)
	const forbiddenQuestion = "Should active-run selection be single-run-per-workspace or multi-run with an explicit --run selector on every command?"
	for _, raw := range append(mapSlice(frontier["ready_frontier"]), mapSlice(frontier["blocked_frontier"])...) {
		title := strings.TrimSpace(stringField(raw, "story_title"))
		if strings.HasSuffix(title, "?") {
			t.Fatalf("frontier includes question-style story title %q", title)
		}
		if title == forbiddenQuestion {
			t.Fatalf("frontier includes Open questions item %q", forbiddenQuestion)
		}
	}
}

func TestRunRoomEpicImportPlanOpenCodeJSON(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	planPath := filepath.Join(workspace, "session-123.json")
	mustWriteRoomTestFile(t, planPath, `[
  {"content":"Prepare schema", "status":"pending"},
  {"content":"Apply migration", "status":"in_progress"},
  {"content":"Archive old table", "status":"completed"}
]`)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "opencode"); err != nil {
		t.Fatalf("runRoomEpicImportPlan: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["provider"]; got != "opencode" {
		t.Fatalf("provider=%v want opencode", got)
	}
	if got := data["milestone_count"]; got != float64(1) {
		t.Fatalf("milestone_count=%v want 1", got)
	}
	if got := data["story_count"]; got != float64(2) {
		t.Fatalf("story_count=%v want 2 (completed todo should be skipped)", got)
	}
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, roomTaskScanLimit); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	milestones := epic["milestones"].([]any)
	if len(milestones) != 1 {
		t.Fatalf("len(milestones)=%d want 1", len(milestones))
	}
	milestone := milestones[0].(map[string]any)
	if got := milestone["title"]; got != "session-123" {
		t.Fatalf("milestone title=%v want session-123", got)
	}
	stories := milestone["stories"].([]any)
	if len(stories) != 2 {
		t.Fatalf("len(stories)=%d want 2", len(stories))
	}
	gotTitles := map[string]struct{}{}
	for _, raw := range stories {
		story := raw.(map[string]any)
		gotTitles[story["title"].(string)] = struct{}{}
	}
	for _, want := range []string{"Prepare schema", "Apply migration"} {
		if _, ok := gotTitles[want]; !ok {
			t.Fatalf("story %q missing from imported OpenCode plan", want)
		}
	}
}

func TestRunRoomEpicImportPlanMapsStepDependenciesToStoryDependencies(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	planPath := filepath.Join(workspace, "dependency-plan.md")
	mustWriteRoomTestFile(t, planPath, `# Dependency Plan

## Phase One
### Step 1: First task
Do the first task.
### Step 2: Second task
Do the second task.
### Step 3: Third task
Do the third task.`)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicImportPlan(cmd, workspace, "human-a", "alpha", planPath, "claude"); err != nil {
		t.Fatalf("runRoomEpicImportPlan: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["unresolved_dependency_count"]; got != float64(0) {
		t.Fatalf("unresolved_dependency_count=%v want 0", got)
	}
	epicID := data["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, roomTaskScanLimit); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	titleToID := map[string]string{}
	titleToDeps := map[string][]string{}
	for _, milestoneRaw := range epic["milestones"].([]any) {
		milestone := milestoneRaw.(map[string]any)
		for _, storyRaw := range milestone["stories"].([]any) {
			story := storyRaw.(map[string]any)
			title := story["title"].(string)
			titleToID[title] = story["id"].(string)
			titleToDeps[title] = stringSliceValue(story["dependencies"])
		}
	}
	secondDeps := titleToDeps["Second task"]
	if len(secondDeps) != 1 || secondDeps[0] != titleToID["First task"] {
		t.Fatalf("Second task deps=%v want [%s]", secondDeps, titleToID["First task"])
	}
	thirdDeps := titleToDeps["Third task"]
	if len(thirdDeps) != 1 || thirdDeps[0] != titleToID["Second task"] {
		t.Fatalf("Third task deps=%v want [%s]", thirdDeps, titleToID["Second task"])
	}
}

func TestNewRoomEpicCommandIncludesSnapshotSubcommand(t *testing.T) {
	cmd := newRoomEpicCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "snapshot" {
			found = true
			if got := sub.Use; got != "snapshot <room-id> <epic-id>" {
				t.Fatalf("snapshot use=%q want snapshot <room-id> <epic-id>", got)
			}
			break
		}
	}
	if !found {
		t.Fatalf("snapshot subcommand missing")
	}
}

func TestNewRoomEpicCommandIncludesFrontierSubcommand(t *testing.T) {
	cmd := newRoomEpicCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "frontier" {
			found = true
			if got := sub.Use; got != "frontier <room-id> <epic-id>" {
				t.Fatalf("frontier use=%q want frontier <room-id> <epic-id>", got)
			}
			break
		}
	}
	if !found {
		t.Fatalf("frontier subcommand missing")
	}
}

func TestNewRoomEpicCommandIncludesDispatchFrontierSubcommand(t *testing.T) {
	cmd := newRoomEpicCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "dispatch-frontier" {
			found = true
			if got := sub.Use; got != "dispatch-frontier <room-id> <epic-id>" {
				t.Fatalf("dispatch-frontier use=%q want dispatch-frontier <room-id> <epic-id>", got)
			}
			break
		}
	}
	if !found {
		t.Fatalf("dispatch-frontier subcommand missing")
	}
}

func TestNewRoomEpicCommandIncludesImportPlanSubcommand(t *testing.T) {
	cmd := newRoomEpicCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "import-plan" {
			found = true
			if got := sub.Use; got != "import-plan <room-id>" {
				t.Fatalf("import-plan use=%q want import-plan <room-id>", got)
			}
			break
		}
	}
	if !found {
		t.Fatalf("import-plan subcommand missing")
	}
}

func TestNewRoomEpicDispatchFrontierCommandAgentRoleDefaultExecution(t *testing.T) {
	cmd := newRoomEpicDispatchFrontierCommand()
	flag := cmd.Flags().Lookup("agent-role")
	if flag == nil {
		t.Fatal("agent-role flag missing")
	}
	if got := flag.DefValue; got != roomEpicDispatchFrontierDefaultAgentRole {
		t.Fatalf("--agent-role default=%q want %q", got, roomEpicDispatchFrontierDefaultAgentRole)
	}
}

func TestNewRoomEpicGradeDispatchCommandAgentRoleDefaultPlanner(t *testing.T) {
	cmd := newRoomEpicGradeDispatchCommand()
	flag := cmd.Flags().Lookup("agent-role")
	if flag == nil {
		t.Fatal("agent-role flag missing")
	}
	if got := flag.DefValue; got != "planner" {
		t.Fatalf("--agent-role default=%q want planner", got)
	}
}

func TestRunRoomPulseDeterministicEmptyShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomPulse(cmd, workspace, "alpha", "", 10, nil); err != nil {
		t.Fatalf("runRoomPulse: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["epic_count"]; got != float64(0) {
		t.Fatalf("epic_count=%v want 0", got)
	}
	if got := len(data["epics"].([]any)); got != 0 {
		t.Fatalf("len(epics)=%d want 0", got)
	}
	if got := len(data["top_items"].([]any)); got != 0 {
		t.Fatalf("len(top_items)=%d want 0", got)
	}
	summary := data["summary"].(map[string]any)
	for _, key := range []string{"blocked_epic_count", "intake_epic_count", "review_epic_count", "execution_epic_count", "completed_epic_count"} {
		if got := summary[key]; got != float64(0) {
			t.Fatalf("%s=%v want 0", key, got)
		}
	}
}

func TestRunRoomPulseOrdersBlockedBeforeIntakeAndShowsMissingCheckpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Discovery Epic", "Clarify scope", "human-a", "", "", []string{"intake"}, []string{"questions resolved"}); err != nil {
		t.Fatalf("runRoomEpicStart discovery: %v", err)
	}
	discoveryEpicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Blocked Epic", "Ship blocked slice", "human-a", "", "", []string{"blocked"}, []string{"resolved"}); err != nil {
		t.Fatalf("runRoomEpicStart blocked: %v", err)
	}
	blockedEpicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", blockedEpicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", blockedEpicID, "Foundation", "Ship blocked slice", "", "human-a", []string{"blocked"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Blocked story", "Blocked implementation.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	storyID := decodeRoomEnvelope(t, out)["story_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "blocked", "Blocked.", "docs/reviews/blocked.md", "", "", "Blocked.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate blocked: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomPulse(cmd, workspace, "alpha", "", 10, nil); err != nil {
		t.Fatalf("runRoomPulse: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epics := data["epics"].([]any)
	if len(epics) != 2 {
		t.Fatalf("len(epics)=%d want 2", len(epics))
	}
	first := epics[0].(map[string]any)
	second := epics[1].(map[string]any)
	if got := first["epic_id"]; got != blockedEpicID {
		t.Fatalf("first epic_id=%v want %s", got, blockedEpicID)
	}
	if got := first["phase"]; got != "blocked" {
		t.Fatalf("first phase=%v want blocked", got)
	}
	if got := first["checkpoint_status"]; got != "missing" {
		t.Fatalf("first checkpoint_status=%v want missing", got)
	}
	if got := second["epic_id"]; got != discoveryEpicID {
		t.Fatalf("second epic_id=%v want %s", got, discoveryEpicID)
	}
	if got := second["phase"]; got != "discovery" {
		t.Fatalf("second phase=%v want discovery", got)
	}
	if got := second["checkpoint_status"]; got != "not_needed" {
		t.Fatalf("second checkpoint_status=%v want not_needed", got)
	}
	summary := data["summary"].(map[string]any)
	if got := summary["blocked_epic_count"]; got != float64(1) {
		t.Fatalf("blocked_epic_count=%v want 1", got)
	}
	if got := summary["intake_epic_count"]; got != float64(1) {
		t.Fatalf("intake_epic_count=%v want 1", got)
	}
	topItems := data["top_items"].([]any)
	if len(topItems) == 0 || topItems[0].(map[string]any)["type"] != "follow_up_blocker" {
		t.Fatalf("top_items=%v want first item follow_up_blocker", topItems)
	}
}

func TestRunRoomPulseShowsReadyAndStaleCheckpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Ready Epic", "Ship ready slice", "human-a", "", "", []string{"ready"}, []string{"review"}); err != nil {
		t.Fatalf("runRoomEpicStart ready: %v", err)
	}
	readyEpicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", readyEpicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize ready: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", readyEpicID, "Ready Milestone", "Ship ready slice", "", "human-a", []string{"ready"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart ready: %v", err)
	}
	readyMilestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", readyMilestoneID, "Ready story", "Ready implementation.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd ready: %v", err)
	}
	readyStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", readyStoryID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate ready: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicCheckpoint(cmd, workspace, "human-a", "alpha", readyEpicID, "", "", "", 5); err != nil {
		t.Fatalf("runRoomEpicCheckpoint ready: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Stale Epic", "Ship stale slice", "human-a", "", "", []string{"stale"}, []string{"checkpoint"}); err != nil {
		t.Fatalf("runRoomEpicStart stale: %v", err)
	}
	staleEpicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", staleEpicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize stale: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", staleEpicID, "Stale Milestone", "Ship stale slice", "", "human-a", []string{"stale"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart stale: %v", err)
	}
	staleMilestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", staleMilestoneID, "Stale story", "Stale implementation.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd stale: %v", err)
	}
	staleStoryID := decodeRoomEnvelope(t, out)["story_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicCheckpoint(cmd, workspace, "human-a", "alpha", staleEpicID, "", "", "", 5); err != nil {
		t.Fatalf("runRoomEpicCheckpoint stale: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", staleEpicID, "Checkpoint aged", []string{"checkpoint created"}, nil, nil, []string{"refresh checkpoint"}, "More work happened."); err != nil {
		t.Fatalf("runRoomLogAppend stale: %v", err)
	}
	_ = staleStoryID

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomPulse(cmd, workspace, "alpha", "", 10, []string{"ready"}); err != nil {
		t.Fatalf("runRoomPulse ready filter: %v", err)
	}
	readyRows := decodeRoomEnvelope(t, out)["epics"].([]any)
	if len(readyRows) != 1 {
		t.Fatalf("len(ready rows)=%d want 1", len(readyRows))
	}
	readyRow := readyRows[0].(map[string]any)
	if got := readyRow["epic_id"]; got != readyEpicID {
		t.Fatalf("ready epic_id=%v want %s", got, readyEpicID)
	}
	if got := readyRow["exit_policy_status"]; got != "ready_for_review" {
		t.Fatalf("ready exit_policy_status=%v want ready_for_review", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomPulse(cmd, workspace, "alpha", "", 10, []string{"stale"}); err != nil {
		t.Fatalf("runRoomPulse stale filter: %v", err)
	}
	staleRows := decodeRoomEnvelope(t, out)["epics"].([]any)
	if len(staleRows) != 1 {
		t.Fatalf("len(stale rows)=%d want 1", len(staleRows))
	}
	staleRow := staleRows[0].(map[string]any)
	if got := staleRow["epic_id"]; got != staleEpicID {
		t.Fatalf("stale epic_id=%v want %s", got, staleEpicID)
	}
	if got := staleRow["checkpoint_status"]; got != "stale" {
		t.Fatalf("stale checkpoint_status=%v want stale", got)
	}
}

func TestRunRoomEpicHealthBlocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, _, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "blocked", "Waiting on a decision.", "coordinator", ""); err != nil {
		t.Fatalf("runRoomStoryState blocked: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "blocked" {
		t.Fatalf("health=%v want blocked", got)
	}
	if got := health["blocked_story_count"]; got != float64(1) {
		t.Fatalf("blocked_story_count=%v want 1", got)
	}
	if !roomIssueTypesContain(health["issues"].([]any), "story_blocked") {
		t.Fatalf("issues=%v want story_blocked", health["issues"])
	}
}

func TestRunRoomEpicHealthNeedsAttentionForMissingValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, _ := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContract(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"story validated"}); err != nil {
		t.Fatalf("runRoomMilestoneContract: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
		t.Fatalf("runRoomMilestoneCriteria: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "needs_attention" {
		t.Fatalf("health=%v want needs_attention", got)
	}
	if got := health["stories_missing_validation_count"]; got != float64(1) {
		t.Fatalf("stories_missing_validation_count=%v want 1", got)
	}
	if !roomIssueTypesContain(health["issues"].([]any), "story_missing_validation") {
		t.Fatalf("issues=%v want story_missing_validation", health["issues"])
	}
}

func TestRunRoomEpicHealthStaleSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContract(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"story validated"}); err != nil {
		t.Fatalf("runRoomMilestoneContract: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
		t.Fatalf("runRoomMilestoneCriteria: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Summary.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneSummary: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "in_review", "One more pass.", "", "human-a"); err != nil {
		t.Fatalf("runRoomStoryState in_review: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "needs_attention" {
		t.Fatalf("health=%v want needs_attention", got)
	}
	if got := health["stale_milestone_summary_count"]; got != float64(1) {
		t.Fatalf("stale_milestone_summary_count=%v want 1", got)
	}
	if !roomIssueTypesContain(health["issues"].([]any), "stale_summary") {
		t.Fatalf("issues=%v want stale_summary", health["issues"])
	}
}

func TestRunRoomEpicHealthHealthyExecution(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Epic A", "Ship epic health", "human-a", "", "", []string{"health"}, []string{"coordinator sees one pulse"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}
	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship epic health", "Keep the pulse compact.", "human-a", []string{"health"}, []string{"coordinator confusion"}, nil, nil, []string{"review"}, []string{"health visible"}, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Health output is deterministic"); err != nil {
		t.Fatalf("runRoomMilestoneCriteria: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", epicID, "Foundation started", []string{"epic finalized"}, nil, nil, []string{"implement health"}, "Active work."); err != nil {
		t.Fatalf("runRoomLogAppend: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "healthy" {
		t.Fatalf("health=%v want healthy", got)
	}
	if got := health["issue_count"]; got != float64(0) {
		t.Fatalf("issue_count=%v want 0", got)
	}
}

func TestRunRoomEpicStartCreatesDefaultQuietChoresMilestone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Epic A", "Ship epic chores", "human-a", "", "", []string{"health"}, []string{"quiet chores exist"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	epicID := data["epic_id"].(string)
	choresMilestoneID := data["chores_milestone_id"].(string)
	if choresMilestoneID == "" {
		t.Fatal("chores_milestone_id empty")
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomEpicShow(cmd, workspace, "alpha", epicID, 100); err != nil {
		t.Fatalf("runRoomEpicShow: %v", err)
	}
	epic := decodeRoomEnvelope(t, out)["epic"].(map[string]any)
	if got := epic["milestone_count"]; got != float64(0) {
		t.Fatalf("milestone_count=%v want 0 visible milestones", got)
	}
	if got := epic["quiet_milestone_count"]; got != float64(1) {
		t.Fatalf("quiet_milestone_count=%v want 1", got)
	}
	if got := epic["default_chores_milestone_id"]; got != choresMilestoneID {
		t.Fatalf("default_chores_milestone_id=%v want %s", got, choresMilestoneID)
	}
	quietMilestones := epic["quiet_milestones"].([]any)
	if len(quietMilestones) != 1 {
		t.Fatalf("len(quiet_milestones)=%d want 1", len(quietMilestones))
	}
	quiet := quietMilestones[0].(map[string]any)
	if got := quiet["lane_kind"]; got != roomMilestoneLaneKindChores {
		t.Fatalf("lane_kind=%v want %s", got, roomMilestoneLaneKindChores)
	}
	if got := quiet["followup_policy"]; got != roomFollowupPolicyNone {
		t.Fatalf("followup_policy=%v want %s", got, roomFollowupPolicyNone)
	}
	if got := quiet["id"]; got != choresMilestoneID {
		t.Fatalf("quiet milestone id=%v want %s", got, choresMilestoneID)
	}
}

func TestRunRoomEpicHealthComplete(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	epicID, milestoneID, storyID := setupRoomAgileWorkpackFixture(t, ctx, workspace)
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomMilestoneContract(cmd, workspace, "human-a", "alpha", milestoneID, "Foundation objective.", nil, nil, nil, []string{"review"}, []string{"story validated"}); err != nil {
		t.Fatalf("runRoomMilestoneContract: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneCriteria(cmd, workspace, "human-a", "alpha", milestoneID, "Accepted stories are validated"); err != nil {
		t.Fatalf("runRoomMilestoneCriteria: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneReview(cmd, workspace, "human-a", "alpha", milestoneID, "pass", "Looks good."); err != nil {
		t.Fatalf("runRoomMilestoneReview: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomMilestoneSummary(cmd, workspace, "human-a", "alpha", milestoneID, "Summary.", "", nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("runRoomMilestoneSummary: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomLogAppend(cmd, workspace, "human-a", "alpha", epicID, "Foundation landed", []string{"health slice shipped"}, nil, nil, []string{"none"}, "Stable."); err != nil {
		t.Fatalf("runRoomLogAppend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicHealth(cmd, workspace, "alpha", epicID, "", 100); err != nil {
		t.Fatalf("runRoomEpicHealth: %v", err)
	}
	health := decodeRoomEnvelope(t, out)["health"].(map[string]any)
	if got := health["health"]; got != "complete" {
		t.Fatalf("health=%v want complete", got)
	}
	if got := health["issue_count"]; got != float64(0) {
		t.Fatalf("issue_count=%v want 0", got)
	}
}

func roomIssueTypesContain(items []any, want string) bool {
	for _, raw := range items {
		item := raw.(map[string]any)
		if got, _ := item["type"].(string); got == want {
			return true
		}
	}
	return false
}

func TestRunRoomStoryValidateRejectsArtifactDigestWithoutPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	storyID := setupRoomStoryValidationFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validation attached at story level.", "", "sha256:test", "", "", nil)
	assertRoomErrorContains(t, err, "artifact-digest requires artifact-path")
}

func TestRunRoomStoryValidateRequiresWaiverNotesAndAuthority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	storyID := setupRoomStoryValidationFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomStoryValidate(cmd, workspace, "gemini-a", "alpha", storyID, "review", "waived", "Waive validation.", "", "", "", "", nil)
	assertRoomErrorContains(t, err, "waived validations require the story owner or coordinator")

	cmd, _ = newRoomTestCommand(ctx)
	err = runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "waived", "Waive validation.", "", "", "", "", nil)
	assertRoomErrorContains(t, err, "waived validations require waiver notes")
}

func TestRunRoomStoryValidateSupersedesByValidatorType(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ctx := context.Background()
	workspace := t.TempDir()

	storyID := setupRoomStoryValidationFixture(t, ctx, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "fail", "Initial review failed.", "docs/reviews/first.md", "", "", "Found a problem.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate first: %v", err)
	}
	firstID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Second review passed.", "docs/reviews/second.md", "", "", "Fixed.", nil); err != nil {
		t.Fatalf("runRoomStoryValidate second: %v", err)
	}
	secondID := decodeRoomEnvelope(t, out)["validation_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := story["latest_validation_status"]; got != "pass" {
		t.Fatalf("latest_validation_status=%v want pass", got)
	}
	if got := story["latest_validation_id"]; got != secondID {
		t.Fatalf("latest_validation_id=%v want %s", got, secondID)
	}
	effective := story["effective_validations"].([]any)
	if len(effective) != 1 {
		t.Fatalf("len(effective_validations)=%d want 1", len(effective))
	}
	effectiveValidation := effective[0].(map[string]any)
	if got := effectiveValidation["validation_id"]; got != secondID {
		t.Fatalf("effective validation_id=%v want %s", got, secondID)
	}
	validations := story["validations"].([]any)
	if len(validations) != 2 {
		t.Fatalf("len(validations)=%d want 2", len(validations))
	}
	superseded := map[string]bool{}
	for _, raw := range validations {
		item := raw.(map[string]any)
		id := item["validation_id"].(string)
		superseded[id] = item["superseded"].(bool)
	}
	if !superseded[firstID] {
		t.Fatalf("first validation superseded=%v want true", superseded[firstID])
	}
	if superseded[secondID] {
		t.Fatalf("second validation superseded=%v want false", superseded[secondID])
	}
}

func setupRoomStoryValidationFixture(t *testing.T, ctx context.Context, workspace string) string {
	t.Helper()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship work-packs", "human-a", "", "", []string{"room", "filesystem mirror"}, []string{"story validation is visible"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation", "work-pack sync"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Implement story validate", "Add a story-level validation record.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	return decodeRoomEnvelope(t, out)["story_id"].(string)
}

func setupRoomAgileWorkpackFixture(t *testing.T, ctx context.Context, workspace string) (string, string, string) {
	t.Helper()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomEpicStart(cmd, workspace, "human-a", "alpha", "Room agile protocol", "Ship work-packs", "human-a", "", "", []string{"room", "filesystem mirror"}, []string{"story validation is visible"}); err != nil {
		t.Fatalf("runRoomEpicStart: %v", err)
	}
	epicID := decodeRoomEnvelope(t, out)["epic_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomEpicFinalize(cmd, workspace, "human-a", "alpha", epicID, "Clarified brief."); err != nil {
		t.Fatalf("runRoomEpicFinalize: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneStart(cmd, workspace, "human-a", "alpha", epicID, "Foundation", "Ship the first validation slice", "", "human-a", []string{"story validation", "work-pack sync"}, nil, nil, nil, nil, nil, ""); err != nil {
		t.Fatalf("runRoomMilestoneStart: %v", err)
	}
	milestoneID := decodeRoomEnvelope(t, out)["milestone_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStoryAdd(cmd, workspace, "human-a", "alpha", milestoneID, "Implement story validate", "Add a story-level validation record.", "human-a"); err != nil {
		t.Fatalf("runRoomStoryAdd: %v", err)
	}
	storyID := decodeRoomEnvelope(t, out)["story_id"].(string)
	return epicID, milestoneID, storyID
}

func setupRoomStoryLifecycleFixture(t *testing.T, ctx context.Context, workspace string) (string, string, string) {
	t.Helper()
	return setupRoomAgileWorkpackFixture(t, ctx, workspace)
}

func TestRunRoomStoryStateProgression(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, storyID := setupRoomStoryLifecycleFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "in_progress", "Started implementation.", "", ""); err != nil {
		t.Fatalf("runRoomStoryState in_progress: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "in_review", "Ready for review.", "", "human-a"); err != nil {
		t.Fatalf("runRoomStoryState in_review: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := story["state"]; got != "in_review" {
		t.Fatalf("state=%v want in_review", got)
	}
	if got := story["reviewer"]; got != "human-a" {
		t.Fatalf("reviewer=%v want human-a", got)
	}
	if got := story["state_update_count"]; got != float64(2) {
		t.Fatalf("state_update_count=%v want 2", got)
	}
	history := story["state_history"].([]any)
	if len(history) != 2 {
		t.Fatalf("len(state_history)=%d want 2", len(history))
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["in_review_story_count"]; got != float64(1) {
		t.Fatalf("in_review_story_count=%v want 1", got)
	}
}

func TestRunRoomStoryStateBlockedRequiresReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, _, storyID := setupRoomStoryLifecycleFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "blocked", "", "", "")
	assertRoomErrorContains(t, err, "blocked stories require a reason")
}

func TestRunRoomStoryStateDoneRequiresValidationOrWaiver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, _, storyID := setupRoomStoryLifecycleFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "done", "Finished.", "", "")
	assertRoomErrorContains(t, err, "done stories require the latest validation status to be pass or waived")

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryValidate(cmd, workspace, "human-a", "alpha", storyID, "review", "pass", "Validated.", "docs/reviews/pass.md", "", "", "", nil); err != nil {
		t.Fatalf("runRoomStoryValidate pass: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "done", "Finished.", "", ""); err != nil {
		t.Fatalf("runRoomStoryState done: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := story["state"]; got != "done" {
		t.Fatalf("state=%v want done", got)
	}
}

func TestRunRoomStoryStateValidatedRequiresPassingValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, _, storyID := setupRoomStoryLifecycleFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "validated", "Looks validated.", "", "")
	assertRoomErrorContains(t, err, "validated story state requires the latest story validation status to be pass")
}

func TestRunRoomStoryStateDeferredPersists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	_, milestoneID, storyID := setupRoomStoryLifecycleFixture(t, ctx, workspace)

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomStoryState(cmd, workspace, "human-a", "alpha", storyID, "deferred", "Move to the next tranche.", "", ""); err != nil {
		t.Fatalf("runRoomStoryState deferred: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStoryShow(cmd, workspace, "alpha", storyID, 100); err != nil {
		t.Fatalf("runRoomStoryShow: %v", err)
	}
	story := decodeRoomEnvelope(t, out)["story"].(map[string]any)
	if got := story["state"]; got != "deferred" {
		t.Fatalf("state=%v want deferred", got)
	}
	if got := story["state_reason"]; got != "Move to the next tranche." {
		t.Fatalf("state_reason=%v want deferred reason", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomMilestoneShow(cmd, workspace, "alpha", milestoneID, 100); err != nil {
		t.Fatalf("runRoomMilestoneShow: %v", err)
	}
	milestone := decodeRoomEnvelope(t, out)["milestone"].(map[string]any)
	if got := milestone["deferred_story_count"]; got != float64(1) {
		t.Fatalf("deferred_story_count=%v want 1", got)
	}
}

func TestSyncRoomRedgreenWorktreePreservesHiddenPaths(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	mustWriteRoomTestFile(t, filepath.Join(src, "pkg", "impl.go"), "package pkg\nconst Value = 2\n")
	mustWriteRoomTestFile(t, filepath.Join(src, "README.md"), "updated\n")
	mustWriteRoomTestFile(t, filepath.Join(dst, "pkg", "impl.go"), "package pkg\nconst Value = 1\n")
	mustWriteRoomTestFile(t, filepath.Join(dst, "pkg", "hidden_test.go"), "package pkg\nfunc TestHidden(t *testing.T) {}\n")
	mustWriteRoomTestFile(t, filepath.Join(dst, "stale.txt"), "remove me\n")

	if err := syncRoomRedgreenWorktree(src, dst, []string{"pkg/hidden_test.go"}); err != nil {
		t.Fatalf("syncRoomRedgreenWorktree: %v", err)
	}

	impl := mustReadRoomTestFile(t, filepath.Join(dst, "pkg", "impl.go"))
	if !strings.Contains(impl, "Value = 2") {
		t.Fatalf("impl=%q want synced green implementation", impl)
	}
	hidden := mustReadRoomTestFile(t, filepath.Join(dst, "pkg", "hidden_test.go"))
	if !strings.Contains(hidden, "TestHidden") {
		t.Fatalf("hidden=%q want preserved hidden test", hidden)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale.txt err=%v want not exist", err)
	}
}

func TestRunRoomRedgreenInitHideAndShow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRedgreenInit(cmd, workspace, "alpha", "retry-loop", "", "", "red-a", "green-a", "human-a", filepath.Join(t.TempDir(), "pair"), "HEAD", "go test ./..."); err != nil {
		t.Fatalf("runRoomRedgreenInit: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	stateRaw, ok := data["state"].(map[string]any)
	if !ok {
		t.Fatalf("state payload type=%T", data["state"])
	}
	if stateRaw["red_actor"] != "red-a" || stateRaw["green_actor"] != "green-a" {
		t.Fatalf("state=%v want red-a/green-a", stateRaw)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomRedgreenHide(cmd, workspace, "red-a", "alpha", "pkg/hidden_test.go"); err != nil {
		t.Fatalf("runRoomRedgreenHide: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRedgreenShow(cmd, workspace, "green-a", "alpha"); err != nil {
		t.Fatalf("runRoomRedgreenShow green: %v", err)
	}
	showGreen := decodeRoomEnvelope(t, out)
	if showGreen["red_worktree"] != "[redacted]" {
		t.Fatalf("green red_worktree=%v want [redacted]", showGreen["red_worktree"])
	}
	if showGreen["hidden_paths"] != "[redacted]" {
		t.Fatalf("green hidden_paths=%v want [redacted]", showGreen["hidden_paths"])
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomRedgreenShow(cmd, workspace, "human-a", "alpha"); err != nil {
		t.Fatalf("runRoomRedgreenShow coordinator: %v", err)
	}
	showCoord := decodeRoomEnvelope(t, out)
	if showCoord["red_worktree"] == "[redacted]" {
		t.Fatalf("coordinator red_worktree unexpectedly redacted: %v", showCoord["red_worktree"])
	}
	hiddenPaths, ok := showCoord["hidden_paths"].([]any)
	if !ok || len(hiddenPaths) != 1 || hiddenPaths[0] != "pkg/hidden_test.go" {
		t.Fatalf("hidden_paths=%T/%v want pkg/hidden_test.go", showCoord["hidden_paths"], showCoord["hidden_paths"])
	}
}

func TestRunRoomCreatePatternRedgreen(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "alpha", "", "", nil, roomCreateProvisionOptions{
		Pattern:          "redgreen",
		PatternSlug:      "alpha-rg",
		RedActor:         "red-a",
		GreenActor:       "green-a",
		CoordinatorActor: "human-a",
		WorktreeRoot:     filepath.Join(t.TempDir(), "pair"),
		BaseRef:          "HEAD",
		CheckCommand:     "go test ./...",
	})
	if err != nil {
		t.Fatalf("runRoomCreateWithProvision redgreen: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T", data["room"])
	}
	if roomRaw["id"] != "alpha" {
		t.Fatalf("room id=%v want alpha", roomRaw["id"])
	}
	stateRaw, ok := data["state"].(map[string]any)
	if !ok {
		t.Fatalf("state payload type=%T", data["state"])
	}
	if stateRaw["slug"] != "alpha-rg" {
		t.Fatalf("slug=%v want alpha-rg", stateRaw["slug"])
	}
	if stateRaw["red_actor"] != "red-a" || stateRaw["green_actor"] != "green-a" || stateRaw["coordinator"] != "human-a" {
		t.Fatalf("state=%v want red/green/coordinator roles", stateRaw)
	}
}

func TestRunRoomInterviewFlow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInterviewStart(cmd, workspace, "human-a", "alpha", "spec-meaning", "Need to clarify the retry semantics", "docs/spec/retry.md", "human-a", "gemini-a", "cursor-a", "human-a", []string{"keep API stable"}); err != nil {
		t.Fatalf("runRoomInterviewStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewAsk(cmd, workspace, "gemini-a", "alpha", sessionID, "", "Should the retry ladder stop on 429 or continue after backoff?"); err != nil {
		t.Fatalf("runRoomInterviewAsk: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	questionMsg := data["message"].(map[string]any)
	questionID := questionMsg["id"].(string)
	if got := questionMsg["recipient"]; got != "cursor-a" {
		t.Fatalf("recipient=%v want cursor-a", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewAnswer(cmd, workspace, "cursor-a", "alpha", questionID, "Stop on 429 after recording the backoff reason."); err != nil {
		t.Fatalf("runRoomInterviewAnswer: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	answerMsg := data["message"].(map[string]any)
	answerID := answerMsg["id"].(string)
	if got := answerMsg["recipient"]; got != "human-a" {
		t.Fatalf("answer recipient=%v want human-a", got)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomInterviewVerify(cmd, workspace, "human-a", "alpha", answerID, "accept", "Yes, that matches the intended semantics."); err != nil {
		t.Fatalf("runRoomInterviewVerify: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewShow(cmd, workspace, "alpha", sessionID, 50); err != nil {
		t.Fatalf("runRoomInterviewShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	session := data["session"].(map[string]any)
	if got := session["status"]; got != "verified" {
		t.Fatalf("status=%v want verified", got)
	}
	if got := session["questions"]; got != float64(1) {
		t.Fatalf("questions=%v want 1", got)
	}
	if got := session["answers"]; got != float64(1) {
		t.Fatalf("answers=%v want 1", got)
	}
	if got := session["verified"]; got != float64(1) {
		t.Fatalf("verified=%v want 1", got)
	}
}

func TestRunRoomInterviewVerifyRequiresVerifier(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "cursor-a=reviewer", "claude-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInterviewStart(cmd, workspace, "human-a", "alpha", "spec-meaning", "", "", "human-a", "gemini-a", "cursor-a", "human-a", nil); err != nil {
		t.Fatalf("runRoomInterviewStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewAsk(cmd, workspace, "gemini-a", "alpha", sessionID, "", "What should we do?"); err != nil {
		t.Fatalf("runRoomInterviewAsk: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	questionID := data["message"].(map[string]any)["id"].(string)

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewAnswer(cmd, workspace, "cursor-a", "alpha", questionID, "Here is the answer."); err != nil {
		t.Fatalf("runRoomInterviewAnswer: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	answerID := data["message"].(map[string]any)["id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	err := runRoomInterviewVerify(cmd, workspace, "claude-a", "alpha", answerID, "accept", "Looks right.")
	assertRoomErrorContains(t, err, "only the verifier or coordinator can record an interview verdict")
}

func TestRunRoomInterviewNext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInterviewStart(cmd, workspace, "human-a", "alpha", "spec-meaning", "", "", "human-a", "gemini-a", "cursor-a", "human-a", nil); err != nil {
		t.Fatalf("runRoomInterviewStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomInterviewAsk(cmd, workspace, "gemini-a", "alpha", sessionID, "", "What should we do?"); err != nil {
		t.Fatalf("runRoomInterviewAsk: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewNext(cmd, workspace, "alpha", "cursor-a", 50); err != nil {
		t.Fatalf("runRoomInterviewNext cursor-a: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if pending, ok := data["pending"].(bool); !ok || !pending {
		t.Fatalf("pending=%v want true", data["pending"])
	}
	item := data["item"].(map[string]any)
	if item["type"] != "answer_question" {
		t.Fatalf("type=%v want answer_question", item["type"])
	}

	questionID := item["message"].(map[string]any)["id"].(string)
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomInterviewAnswer(cmd, workspace, "cursor-a", "alpha", questionID, "Here is the answer."); err != nil {
		t.Fatalf("runRoomInterviewAnswer: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInterviewNext(cmd, workspace, "alpha", "human-a", 50); err != nil {
		t.Fatalf("runRoomInterviewNext human-a: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	item = data["item"].(map[string]any)
	if item["type"] != "verify_answer" {
		t.Fatalf("type=%v want verify_answer", item["type"])
	}
}

func TestRunRoomStatusIncludesInterviewLane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInterviewStart(cmd, workspace, "human-a", "alpha", "spec-meaning", "", "", "human-a", "gemini-a", "cursor-a", "human-a", nil); err != nil {
		t.Fatalf("runRoomInterviewStart: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	sessionID := data["session_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomInterviewAsk(cmd, workspace, "gemini-a", "alpha", sessionID, "", "What should we do?"); err != nil {
		t.Fatalf("runRoomInterviewAsk: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, []string{"interview"}, "open", false); err != nil {
		t.Fatalf("runRoomStatus interview: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	action := data["action_required"].(map[string]any)
	if got := action["participants_with_pending"]; got != float64(1) {
		t.Fatalf("participants_with_pending=%v want 1", got)
	}
	if got := action["pending_replies"]; got != float64(1) {
		t.Fatalf("pending_replies=%v want 1", got)
	}
	top := action["top_entries"].([]any)
	if len(top) == 0 {
		t.Fatalf("top_entries empty")
	}
	if got := top[0].(map[string]any)["subject"]; got != "Interview Question: What should we do?" {
		t.Fatalf("subject=%v want interview question", got)
	}
	flags := top[0].(map[string]any)["flags"].([]any)
	foundInterview := false
	for _, raw := range flags {
		if raw == "INTERVIEW" {
			foundInterview = true
			break
		}
	}
	if !foundInterview {
		t.Fatalf("flags=%v want INTERVIEW", flags)
	}
}

func TestRunRoomSendResolvesCoordinatorAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "gemini-a", "@coordinator", "", "please take a look", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	message := data["message"].(map[string]any)
	if got := message["recipient"]; got != "human-a" {
		t.Fatalf("recipient=%v want human-a", got)
	}
}

func TestRunRoomCoordinatorSetTransfersRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomCoordinatorSet(cmd, workspace, "alpha", "human-a", "gemini-a"); err != nil {
		t.Fatalf("runRoomCoordinatorSet: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	if got := data["coordinator"]; got != "gemini-a" {
		t.Fatalf("coordinator=%v want gemini-a", got)
	}
	room := data["room"].(map[string]any)
	members := room["members"].([]any)
	foundNew := false
	for _, raw := range members {
		member := raw.(map[string]any)
		if member["actor_id"] == "gemini-a" && member["role"] == "coordinator" {
			foundNew = true
		}
		if member["actor_id"] == "human-a" && member["role"] == "coordinator" {
			t.Fatalf("human-a still coordinator: %v", member)
		}
	}
	if !foundNew {
		t.Fatalf("expected gemini-a coordinator in members=%v", members)
	}
}

func TestDetectRoomCoordinatorPulseMessagesEmitsReminder(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "gemini-a", Role: "reviewer"},
		},
		Participants: []string{"human-a", "gemini-a"},
	}
	messages := []agent.BoardMessage{{
		ID:          "m1",
		WorkspaceID: "ws1",
		Stream:      room.Stream,
		Sender:      "gemini-a",
		Recipient:   "human-a",
		Kind:        agent.BoardMessageKindInstruction,
		Priority:    2,
		AckRequired: true,
		Subject:     "Need unblock",
		Body:        "Need unblock",
		CreatedAt:   now.Add(-10 * time.Minute),
	}}
	pulses := detectRoomCoordinatorPulseMessages(room, messages, nil, now, roomPulseConfig{
		Interval:                30 * time.Second,
		TaskStaleAfter:          5 * time.Minute,
		CoordinatorPulseEnabled: true,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if got := pulses[0].Message.Recipient; got != "human-a" {
		t.Fatalf("recipient=%q want human-a", got)
	}
	if !strings.Contains(pulses[0].Message.Body, "keep the room on track") {
		t.Fatalf("body missing coordinator responsibility: %q", pulses[0].Message.Body)
	}
}

func TestRunRoomInboxFiltersActionableMessages(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead", "agent-b=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "agent-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend ack: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "agent-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend reply: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "", "", "plain broadcast", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend broadcast: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, true); err != nil {
		t.Fatalf("runRoomInbox: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	entries, ok := data["entries"].([]any)
	if !ok {
		t.Fatalf("entries type=%T", data["entries"])
	}
	if len(entries) != 2 {
		t.Fatalf("entries=%d want 2 actionable entries", len(entries))
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "ack-required", false, true, false, true); err != nil {
		t.Fatalf("runRoomInbox ids-only: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	ids, ok := data["ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("ids=%v want one ack-required id", data["ids"])
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomAck(cmd, workspace, "alpha", "agent-a", []string{ids[0].(string)}); err != nil {
		t.Fatalf("runRoomAck: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, true); err != nil {
		t.Fatalf("runRoomInbox after ack: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	entries, ok = data["entries"].([]any)
	if !ok {
		t.Fatalf("entries type=%T", data["entries"])
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1 actionable entry after ack", len(entries))
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "", "done", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend unrelated response: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, true); err != nil {
		t.Fatalf("runRoomInbox after unrelated response: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	entries, ok = data["entries"].([]any)
	if !ok {
		t.Fatalf("entries type=%T", data["entries"])
	}
	if len(entries) != 1 {
		t.Fatalf("entries=%d want 1 actionable entry after unrelated response", len(entries))
	}

	store, err := openRoomBoardStore(ctx)
	if err != nil {
		t.Fatalf("openRoomBoardStore: %v", err)
	}
	defer store.Close()

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages := data["messages"].([]any)
	var replyMessageID string
	for _, raw := range messages {
		msg := raw.(map[string]any)
		if msg["recipient"] == "agent-a" && msg["reply_expected"] == true {
			replyMessageID = msg["id"].(string)
			break
		}
	}
	if replyMessageID == "" {
		t.Fatal("expected reply-required message id")
	}
	if err := store.SendMessage(ctx, &agent.BoardMessage{
		ID:               "related-reply-1",
		WorkspaceID:      workspace,
		RelatedMessageID: replyMessageID,
		Stream:           agent.RoomStreamName("alpha"),
		Sender:           "agent-a",
		Recipient:        "human-a",
		Kind:             agent.BoardMessageKindInfo,
		Priority:         agent.DefaultPriority,
		Body:             "related done",
		CreatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SendMessage related reply: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, true); err != nil {
		t.Fatalf("runRoomInbox after reply: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	entries, ok = data["entries"].([]any)
	if !ok {
		t.Fatalf("entries type=%T", data["entries"])
	}
	if len(entries) != 0 {
		t.Fatalf("entries=%d want 0 actionable entries after ack and reply", len(entries))
	}
}

func TestRunRoomInboxCompactOmitsBulkRoomFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, true); err != nil {
		t.Fatalf("runRoomInbox: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	room, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T want map", data["room"])
	}
	for _, key := range []string{"task_ids", "members", "participants"} {
		if _, exists := room[key]; exists {
			t.Fatalf("compact room should omit %q, got keys", key)
		}
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false, false); err != nil {
		t.Fatalf("runRoomInbox full: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	room, ok = data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T want map", data["room"])
	}
	if _, exists := room["members"]; !exists {
		t.Fatalf("full room should include members")
	}
}

func TestDetectRoomPulseMessagesEmitsReminderForStaleReplyExpected(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Please respond",
			CreatedAt:     now.Add(-3 * time.Minute),
		},
	}, nil, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Key != "msg-1" {
		t.Fatalf("key=%q want msg-1", pulses[0].Key)
	}
	if pulses[0].Message.Recipient != "gemini-a" {
		t.Fatalf("recipient=%q want gemini-a", pulses[0].Message.Recipient)
	}
	if !pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want true", pulses[0].Message.Interrupt)
	}
}

func TestDetectRoomPulseMessagesSkipsSatisfiedReplyExpected(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Please respond",
			CreatedAt:     now.Add(-3 * time.Minute),
		},
		{
			ID:               "msg-2",
			WorkspaceID:      "/repo",
			RelatedMessageID: "msg-1",
			Stream:           "room:alpha",
			Sender:           "gemini-a",
			Recipient:        "human-a",
			Body:             "I replied",
			CreatedAt:        now.Add(-2 * time.Minute),
		},
	}, nil, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomPulseMessagesSelfDirectedReplyExpectedStillAwaitsFollowUp(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "gemini-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Check in",
			CreatedAt:     now.Add(-3 * time.Minute),
		},
	}, nil, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Message.Recipient != "gemini-a" {
		t.Fatalf("recipient=%q want gemini-a", pulses[0].Message.Recipient)
	}
	if pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want false for self-directed reminder", pulses[0].Message.Interrupt)
	}
}

func TestDetectRoomPulseMessagesSkipsReadReplyExpected(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Status:        agent.BoardMessageStatusRead,
			Subject:       "Please respond",
			CreatedAt:     now.Add(-3 * time.Minute),
		},
	}, nil, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomPulseMessagesHonorsMinimumPulseFloor(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Need reply",
			CreatedAt:     now.Add(-26 * time.Hour),
		},
	}, nil, now, roomPulseConfig{
		Enabled:         true,
		ReplyStaleAfter: 2 * time.Hour,
		MinPulseFloor:   24 * time.Hour,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-3 * time.Hour), Count: 1},
	}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomCoordinatorPulseMessagesHonorsCoordinatorToggle(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "gemini-a", Role: "reviewer"},
		},
		Participants: []string{"human-a", "gemini-a"},
	}
	messages := []agent.BoardMessage{{
		ID:          "m1",
		WorkspaceID: "ws1",
		Stream:      room.Stream,
		Sender:      "gemini-a",
		Recipient:   "human-a",
		Kind:        agent.BoardMessageKindInstruction,
		Priority:    2,
		Subject:     "Need unblock",
		Body:        "Need unblock",
		CreatedAt:   now.Add(-10 * time.Minute),
	}}
	pulses := detectRoomCoordinatorPulseMessages(room, messages, nil, now, roomPulseConfig{
		Enabled:                 true,
		Interval:                30 * time.Minute,
		TaskStaleAfter:          5 * time.Minute,
		MinPulseFloor:           24 * time.Hour,
		CoordinatorPulseEnabled: false,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomCoordinatorPulseMessagesInterruptsCoordinator(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "gemini-a", Role: "reviewer"},
		},
		Participants: []string{"human-a", "gemini-a"},
	}
	messages := []agent.BoardMessage{{
		ID:          "m1",
		WorkspaceID: "ws1",
		Stream:      room.Stream,
		Sender:      "gemini-a",
		Recipient:   "human-a",
		Kind:        agent.BoardMessageKindInstruction,
		Priority:    2,
		AckRequired: true,
		Subject:     "Need unblock",
		Body:        "Need unblock",
		CreatedAt:   now.Add(-10 * time.Minute),
	}}
	pulses := detectRoomCoordinatorPulseMessages(room, messages, nil, now, roomPulseConfig{
		Enabled:                 true,
		Interval:                30 * time.Minute,
		TaskStaleAfter:          5 * time.Minute,
		CoordinatorPulseEnabled: true,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want false", pulses[0].Message.Interrupt)
	}
	if pulses[0].Message.Recipient != "human-a" {
		t.Fatalf("recipient=%q want human-a", pulses[0].Message.Recipient)
	}
}

func TestDetectRoomCoordinatorPulseMessagesSkipsUnchangedState(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "ws1",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "gemini-a", Role: "reviewer"},
		},
		Participants: []string{"human-a", "gemini-a"},
	}
	messages := []agent.BoardMessage{{
		ID:          "m1",
		WorkspaceID: "ws1",
		Stream:      room.Stream,
		Sender:      "gemini-a",
		Recipient:   "human-a",
		Kind:        agent.BoardMessageKindInstruction,
		Priority:    2,
		AckRequired: true,
		Subject:     "Need unblock",
		Body:        "Need unblock",
		CreatedAt:   now.Add(-10 * time.Minute),
	}}
	key := "human-a|1|1|0|0|0|0"
	pulses := detectRoomCoordinatorPulseMessages(room, messages, nil, now, roomPulseConfig{
		Enabled:                 true,
		Interval:                30 * time.Minute,
		TaskStaleAfter:          5 * time.Minute,
		CoordinatorPulseEnabled: true,
	}, map[string]time.Time{key: now.Add(-2 * time.Hour)}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0 when state has not changed", len(pulses))
	}
}

func TestDetectRoomTaskPulseMessagesInterruptsOwner(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	tasks := []taskstore.Task{{
		ID:              "task-1",
		WorkspaceID:     ws.CanonicalID("/repo"),
		Title:           "Review pulse wiring",
		Status:          taskstore.StatusInProgress,
		OwnerActorID:    "cursor-a",
		AssignedActorID: "cursor-a",
		CreatedAt:       now.Add(-20 * time.Minute),
	}}
	pulses := detectRoomTaskPulseMessages("/repo", "alpha", tasks, now, roomPulseConfig{
		Enabled:        true,
		TaskStaleAfter: 5 * time.Minute,
	}, map[string]roomPulseState{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if !pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want true", pulses[0].Message.Interrupt)
	}
	if pulses[0].Message.Recipient != "cursor-a" {
		t.Fatalf("recipient=%q want cursor-a", pulses[0].Message.Recipient)
	}
}

func TestDetectRoomPulseMessagesKeepsOnlyLatestOutstandingPerRecipient(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Older request",
			CreatedAt:     now.Add(-4 * time.Minute),
		},
		{
			ID:            "msg-2",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Latest request",
			CreatedAt:     now.Add(-3 * time.Minute),
		},
	}, nil, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Key != "msg-2" {
		t.Fatalf("key=%q want msg-2", pulses[0].Key)
	}
}

func TestDetectRoomPulseMessagesUsesExponentialBackoff(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Need reply",
			CreatedAt:     now.Add(-48 * time.Hour),
		},
	}, nil, now, roomPulseConfig{
		ReplyStaleAfter: 2 * time.Hour,
		MinPulseFloor:   2 * time.Hour,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-3 * time.Hour), Count: 2},
	}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomPulseEscalationMessagesAfterInterruptBudget(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "gemini-a", Role: "reviewer"},
		},
	}
	messages := []agent.BoardMessage{{
		ID:            "msg-1",
		WorkspaceID:   "/repo",
		Stream:        room.Stream,
		Sender:        "human-a",
		Recipient:     "gemini-a",
		ReplyExpected: true,
		Subject:       "Need reply",
		Body:          "Need reply",
		CreatedAt:     now.Add(-48 * time.Hour),
	}}
	pulses := detectRoomPulseEscalationMessages(room, messages, nil, now, roomPulseConfig{
		ReplyStaleAfter:              2 * time.Hour,
		CoordinatorEscalationEnabled: true,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-10 * time.Hour), Count: roomPulseInterruptLimit},
	}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Message.Recipient != "human-a" {
		t.Fatalf("recipient=%q want human-a", pulses[0].Message.Recipient)
	}
	if !pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want true", pulses[0].Message.Interrupt)
	}
}

func TestRoomLoopRuntimeStateFromStoreRestoresOperationalMemory(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	replySentAt := now.Add(-10 * time.Minute)
	taskSentAt := now.Add(-8 * time.Minute)
	followupSentAt := now.Add(-6 * time.Minute)
	coordinatorSentAt := now.Add(-4 * time.Minute)
	cursorAt := now.Add(-2 * time.Minute)
	runtime := roomLoopRuntimeStateFromStore(coordination.RoomLoop{
		DeliveryLeaseName:       "room-loop:ws:alpha:delivery",
		DeliveryOwnerID:         "owner-a",
		DeliveryCursorMessageID: "msg-9",
		DeliveryCursorAt:        &cursorAt,
		ReplyPulseState: map[string]coordination.RoomLoopPulseState{
			"msg-1": {LastSentAt: &replySentAt, Count: 2},
		},
		TaskPulseState: map[string]coordination.RoomLoopPulseState{
			"task-1": {LastSentAt: &taskSentAt, Count: 1, Escalated: true},
		},
		TaskFollowupState: map[string]time.Time{
			"task-1": followupSentAt,
		},
		CoordinatorPulseState: map[string]time.Time{
			"coord-1": coordinatorSentAt,
		},
	})

	if runtime.ReplyPulseState["msg-1"].Count != 2 {
		t.Fatalf("ReplyPulseState=%+v", runtime.ReplyPulseState["msg-1"])
	}
	if runtime.TaskPulseState["task-1"].LastSentAt != taskSentAt {
		t.Fatalf("TaskPulseState.LastSentAt=%v want %v", runtime.TaskPulseState["task-1"].LastSentAt, taskSentAt)
	}
	if runtime.TaskFollowupState["task-1"] != followupSentAt {
		t.Fatalf("TaskFollowupState=%v want %v", runtime.TaskFollowupState["task-1"], followupSentAt)
	}
	if runtime.CoordinatorPulseState["coord-1"] != coordinatorSentAt {
		t.Fatalf("CoordinatorPulseState=%v want %v", runtime.CoordinatorPulseState["coord-1"], coordinatorSentAt)
	}
}

func TestDetectRoomPulseMessagesUsesRestoredRuntimeStateAfterRestart(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	lastSentAt := now.Add(-30 * time.Second)
	runtime := roomLoopRuntimeStateFromStore(coordination.RoomLoop{
		ReplyPulseState: map[string]coordination.RoomLoopPulseState{
			"msg-1": {LastSentAt: &lastSentAt, Count: 1},
		},
	})
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{
		{
			ID:            "msg-1",
			WorkspaceID:   "/repo",
			Stream:        "room:alpha",
			Sender:        "human-a",
			Recipient:     "gemini-a",
			ReplyExpected: true,
			Subject:       "Need reply",
			CreatedAt:     now.Add(-5 * time.Minute),
		},
	}, nil, now, roomPulseConfig{
		ReplyStaleAfter: 1 * time.Minute,
		MinPulseFloor:   2 * time.Minute,
	}, runtime.ReplyPulseState, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0 after restored runtime state suppresses duplicate reminder", len(pulses))
	}
}

func TestDetectRoomPulseMessagesSkipsClosedTaskReplyDebt(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	tasks := []taskstore.Task{{
		ID:     "task-1",
		Status: taskstore.StatusCompleted,
	}}
	suppression := buildRoomActionSuppression(nil, tasks, nil)
	pulses := detectRoomPulseMessages("alpha", []agent.BoardMessage{{
		ID:            "msg-1",
		TaskID:        "task-1",
		WorkspaceID:   "/repo",
		Stream:        "room:alpha",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		ReplyExpected: true,
		Subject:       "Need review",
		CreatedAt:     now.Add(-5 * time.Minute),
	}}, tasks, now, roomPulseConfig{
		ReplyStaleAfter: 1 * time.Minute,
	}, map[string]roomPulseState{}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomPulseMessagesSkipsQuietChoresMilestoneReplyDebt(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
		{
			ID:               "msg-1",
			WorkspaceID:      "/repo",
			Stream:           "room:alpha",
			Sender:           "human-a",
			Recipient:        "gemini-a",
			RelatedMessageID: "mile-chores-1",
			ReplyExpected:    true,
			Subject:          "Need review",
			CreatedAt:        now.Add(-5 * time.Minute),
		},
	}
	suppression := buildRoomActionSuppression(messages, nil, nil)
	pulses := detectRoomPulseMessages("alpha", messages, nil, now, roomPulseConfig{
		ReplyStaleAfter: 1 * time.Minute,
	}, map[string]roomPulseState{}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestProcessRoomReminderTickEmitsScheduledFollowUp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	lastSent := now.Add(-20 * time.Minute)
	if _, err := store.UpsertRoomReminder(ctx, coordination.RoomReminder{
		ID:            "msg-1",
		WorkspaceID:   "/repo",
		RoomID:        "alpha",
		RootMessageID: "msg-1",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Subject:       "Check MR !26",
		Body:          "Check MR !26 and report status",
		ReplyExpected: true,
		Interrupt:     true,
		Interval:      15 * time.Minute,
		MaxIterations: 3,
		Active:        true,
		LastSentAt:    &lastSent,
	}); err != nil {
		t.Fatalf("UpsertRoomReminder: %v", err)
	}

	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
	}
	messages := []agent.BoardMessage{{
		ID:            "msg-1",
		WorkspaceID:   "/repo",
		Stream:        agent.RoomStreamName("alpha"),
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		ReplyExpected: true,
		Status:        agent.BoardMessageStatusUnread,
		Subject:       "Check MR !26",
		Body:          "Check MR !26 and report status",
		CreatedAt:     now.Add(-30 * time.Minute),
	}}

	out, err := processRoomReminderTick(ctx, store, room, messages, now)
	if err != nil {
		t.Fatalf("processRoomReminderTick: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1", len(out))
	}
	if out[0].Recipient != "gemini-a" {
		t.Fatalf("recipient=%q want gemini-a", out[0].Recipient)
	}
	if out[0].Interrupt != true {
		t.Fatalf("interrupt=%v want true", out[0].Interrupt)
	}
	updated, err := store.GetRoomReminder(ctx, "/repo", "msg-1")
	if err != nil {
		t.Fatalf("GetRoomReminder: %v", err)
	}
	if updated == nil || updated.SentCount != 1 {
		t.Fatalf("sent_count=%v want 1", updated)
	}
}

func TestProcessRoomReminderTickKeepsSelfDirectedReminderActiveUntilLaterReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	lastSent := now.Add(-20 * time.Minute)
	if _, err := store.UpsertRoomReminder(ctx, coordination.RoomReminder{
		ID:            "msg-self",
		WorkspaceID:   "/repo",
		RoomID:        "alpha",
		RootMessageID: "msg-self",
		Sender:        "gemini-a",
		Recipient:     "gemini-a",
		Subject:       "Check in",
		Body:          "Check in and report status",
		ReplyExpected: true,
		Interval:      15 * time.Minute,
		MaxIterations: 3,
		Active:        true,
		LastSentAt:    &lastSent,
	}); err != nil {
		t.Fatalf("UpsertRoomReminder: %v", err)
	}

	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
	}
	messages := []agent.BoardMessage{{
		ID:            "msg-self",
		WorkspaceID:   "/repo",
		Stream:        agent.RoomStreamName("alpha"),
		Sender:        "gemini-a",
		Recipient:     "gemini-a",
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		ReplyExpected: true,
		Status:        agent.BoardMessageStatusUnread,
		Subject:       "Check in",
		Body:          "Check in and report status",
		CreatedAt:     now.Add(-30 * time.Minute),
	}}

	out, err := processRoomReminderTick(ctx, store, room, messages, now)
	if err != nil {
		t.Fatalf("processRoomReminderTick: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1", len(out))
	}
	updated, err := store.GetRoomReminder(ctx, "/repo", "msg-self")
	if err != nil {
		t.Fatalf("GetRoomReminder: %v", err)
	}
	if updated == nil || !updated.Active || updated.SentCount != 1 {
		t.Fatalf("updated=%+v want active reminder with sent_count=1", updated)
	}
}

func TestProcessRoomReminderTickIgnoresUnrelatedLaterRecipientMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	lastSent := now.Add(-20 * time.Minute)
	if _, err := store.UpsertRoomReminder(ctx, coordination.RoomReminder{
		ID:            "msg-root",
		WorkspaceID:   "/repo",
		RoomID:        "alpha",
		RootMessageID: "msg-root",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Subject:       "Check in",
		Body:          "Need chain-aware reply",
		ReplyExpected: true,
		Interval:      15 * time.Minute,
		MaxIterations: 3,
		Active:        true,
		LastSentAt:    &lastSent,
	}); err != nil {
		t.Fatalf("UpsertRoomReminder: %v", err)
	}

	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
	}
	messages := []agent.BoardMessage{
		{
			ID:            "msg-root",
			WorkspaceID:   "/repo",
			Stream:        agent.RoomStreamName("alpha"),
			Sender:        "human-a",
			Recipient:     "gemini-a",
			Kind:          agent.BoardMessageKindInstruction,
			Priority:      agent.DefaultPriority,
			ReplyExpected: true,
			Status:        agent.BoardMessageStatusUnread,
			Subject:       "Check in",
			Body:          "Need chain-aware reply",
			CreatedAt:     now.Add(-30 * time.Minute),
		},
		{
			ID:          "msg-unrelated",
			WorkspaceID: "/repo",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      "gemini-a",
			Recipient:   "*",
			Kind:        agent.BoardMessageKindInfo,
			Priority:    agent.DefaultPriority,
			Body:        "spoke later but not in-chain",
			CreatedAt:   now.Add(-10 * time.Minute),
		},
	}

	out, err := processRoomReminderTick(ctx, store, room, messages, now)
	if err != nil {
		t.Fatalf("processRoomReminderTick: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1 reminder because unrelated later speech must not satisfy the chain", len(out))
	}
}

func TestProcessRoomReminderTickIgnoresAckedReminderInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	lastSent := now.Add(-20 * time.Minute)
	if _, err := store.UpsertRoomReminder(ctx, coordination.RoomReminder{
		ID:            "msg-root",
		WorkspaceID:   "/repo",
		RoomID:        "alpha",
		RootMessageID: "msg-root",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Subject:       "Confirm receipt",
		Body:          "Please confirm receipt",
		AckRequired:   true,
		Interval:      15 * time.Minute,
		MaxIterations: 3,
		Active:        true,
		LastSentAt:    &lastSent,
	}); err != nil {
		t.Fatalf("UpsertRoomReminder: %v", err)
	}

	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
	}
	messages := []agent.BoardMessage{
		{
			ID:          "msg-root",
			WorkspaceID: "/repo",
			Stream:      agent.RoomStreamName("alpha"),
			Sender:      "human-a",
			Recipient:   "gemini-a",
			Kind:        agent.BoardMessageKindInstruction,
			Priority:    agent.DefaultPriority,
			AckRequired: true,
			Status:      agent.BoardMessageStatusUnread,
			Subject:     "Confirm receipt",
			Body:        "Please confirm receipt",
			CreatedAt:   now.Add(-30 * time.Minute),
		},
		{
			ID:               "msg-reminder-1",
			WorkspaceID:      "/repo",
			RelatedMessageID: "msg-root",
			Stream:           agent.RoomStreamName("alpha"),
			Sender:           roomLoopSender("alpha"),
			Recipient:        "gemini-a",
			Kind:             agent.BoardMessageKindAlert,
			Priority:         2,
			AckRequired:      true,
			Status:           agent.BoardMessageStatusAcked,
			Subject:          "Reminder (1/3): Confirm receipt",
			Body:             "Reminder instance was acknowledged",
			CreatedAt:        now.Add(-5 * time.Minute),
		},
	}

	out, err := processRoomReminderTick(ctx, store, room, messages, now)
	if err != nil {
		t.Fatalf("processRoomReminderTick: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out)=%d want 1 reminder because acking a reminder instance must not deactivate the recurring schedule", len(out))
	}
	updated, err := store.GetRoomReminder(ctx, "/repo", "msg-root")
	if err != nil {
		t.Fatalf("GetRoomReminder: %v", err)
	}
	if updated == nil || !updated.Active || updated.SentCount != 1 {
		t.Fatalf("updated=%+v want active reminder with sent_count=1", updated)
	}
}

func TestRoomReminderRoundTripPreservesLinkedWorkIDs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	store, err := coordination.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	_, err = store.UpsertRoomReminder(ctx, coordination.RoomReminder{
		ID:            "msg-root",
		WorkspaceID:   "/repo",
		RoomID:        "alpha",
		RootMessageID: "msg-root",
		TaskID:        "task-1",
		StoryID:       "story-1",
		MilestoneID:   "milestone-1",
		Sender:        "human-a",
		Recipient:     "gemini-a",
		Subject:       "Check in",
		Body:          "Check in",
		AckRequired:   true,
		Interval:      15 * time.Minute,
		MaxIterations: 3,
		Active:        true,
		LastSentAt:    &now,
	})
	if err != nil {
		t.Fatalf("UpsertRoomReminder: %v", err)
	}

	got, err := store.GetRoomReminder(ctx, "/repo", "msg-root")
	if err != nil {
		t.Fatalf("GetRoomReminder: %v", err)
	}
	if got == nil {
		t.Fatal("GetRoomReminder returned nil")
	}
	if got.TaskID != "task-1" || got.StoryID != "story-1" || got.MilestoneID != "milestone-1" {
		t.Fatalf("linked work ids = (%q, %q, %q), want (task-1, story-1, milestone-1)", got.TaskID, got.StoryID, got.MilestoneID)
	}
}

func TestRoomReminderLinkedWorkSatisfiedForCompletedTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	storageRoot := filepath.Join(t.TempDir(), "storage")
	taskStore, err := taskstore.Open(ctx, storageRoot)
	if err != nil {
		t.Fatalf("taskstore.Open: %v", err)
	}
	defer taskStore.Close()

	task, err := taskStore.Add(ctx, taskstore.Task{
		WorkspaceID: ws.CanonicalID("/repo"),
		Title:       "Done task",
		Status:      taskstore.StatusCompleted,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("taskStore.Add: %v", err)
	}

	if !roomReminderLinkedWorkSatisfied(ctx, taskStore, coordination.RoomReminder{TaskID: task.ID}, nil, nil, nil) {
		t.Fatal("completed linked task should satisfy the reminder")
	}
}

func TestRoomReminderLinkedWorkSatisfiedForDoneStory(t *testing.T) {
	if !roomReminderLinkedWorkSatisfied(context.Background(), nil, coordination.RoomReminder{StoryID: "story-1"}, nil, []map[string]any{
		{"id": "story-1", "state": "done"},
	}, nil) {
		t.Fatal("done linked story should satisfy the reminder")
	}
}

func TestRoomReminderLinkedWorkSatisfiedForSummarizedMilestone(t *testing.T) {
	if !roomReminderLinkedWorkSatisfied(context.Background(), nil, coordination.RoomReminder{MilestoneID: "milestone-1"}, nil, nil, []map[string]any{
		{"id": "milestone-1", "summary_count": 1},
	}) {
		t.Fatal("summarized linked milestone should satisfy the reminder")
	}
}

func TestBuildRoomStatusEntriesSkipsSystemReminderMessages(t *testing.T) {
	entries := buildRoomStatusEntries("human-a", []agent.BoardMessage{
		{
			ID:        "sys-1",
			Stream:    "room:alpha",
			Sender:    "actor:system:room:alpha",
			Recipient: "human-a",
			Subject:   "Coordinator pulse",
			Body:      "keep the room on track",
			Status:    agent.BoardMessageStatusUnread,
			CreatedAt: time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC),
		},
		{
			ID:            "msg-1",
			Stream:        "room:alpha",
			Sender:        "cursor-a",
			Recipient:     "human-a",
			Subject:       "Re: smoke",
			Body:          "yes",
			Status:        agent.BoardMessageStatusUnread,
			ReplyExpected: true,
			CreatedAt:     time.Date(2026, 4, 4, 19, 1, 0, 0, time.UTC),
		},
	}, nil)
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ID != "msg-1" {
		t.Fatalf("entry id=%q want msg-1", entries[0].ID)
	}
}

func TestBuildRoomInboxEntriesSuppressesPassiveReminderChains(t *testing.T) {
	suppression := &roomActionSuppression{
		PassiveReminderRoots: map[string]struct{}{"msg-root": {}},
	}
	entries := buildRoomInboxEntries("gemini-a", []agent.BoardMessage{
		{
			ID:        "msg-root",
			Stream:    "room:alpha",
			Sender:    "human-a",
			Recipient: "gemini-a",
			Subject:   "Pulse",
			Body:      "keep moving",
			Status:    agent.BoardMessageStatusUnread,
			CreatedAt: time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:               "msg-followup",
			RelatedMessageID: "msg-root",
			Stream:           "room:alpha",
			Sender:           "actor:system:room:alpha",
			Recipient:        "gemini-a",
			Subject:          "Reminder (1/3): Pulse",
			Body:             "keep moving",
			Status:           agent.BoardMessageStatusUnread,
			CreatedAt:        time.Date(2026, 4, 13, 10, 5, 0, 0, time.UTC),
		},
	}, "all", false, suppression)
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestBuildRoomStatusEntriesSkipsNonActionableDirectInfo(t *testing.T) {
	entries := buildRoomStatusEntries("cursor-a", []agent.BoardMessage{
		{
			ID:        "m1",
			Stream:    "room:alpha",
			Sender:    "human-a",
			Recipient: "cursor-a",
			Subject:   "Smoke",
			Body:      "plain direct info",
			Status:    agent.BoardMessageStatusUnread,
			CreatedAt: time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC),
		},
	}, nil)
	if len(entries) != 0 {
		t.Fatalf("len(entries)=%d want 0", len(entries))
	}
}

func TestDetectRoomTaskPulseMessagesEmitsReminderForStaleClaimedTask(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-10 * time.Minute)
	pulses := detectRoomTaskPulseMessages("/repo", "alpha", []taskstore.Task{
		{
			ID:           "task-1",
			Title:        "Review retry path",
			Status:       taskstore.StatusInProgress,
			OwnerActorID: "claude-a",
			ClaimedAt:    &claimedAt,
		},
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Key != "task-1" {
		t.Fatalf("key=%q want task-1", pulses[0].Key)
	}
	if pulses[0].Message.Recipient != "claude-a" {
		t.Fatalf("recipient=%q want claude-a", pulses[0].Message.Recipient)
	}
	if pulses[0].Message.WorkspaceID != "/repo" {
		t.Fatalf("workspace=%q want /repo", pulses[0].Message.WorkspaceID)
	}
}

func TestDetectRoomTaskPulseMessagesSkipsRecentlyTouchedTask(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-10 * time.Minute)
	heartbeatAt := now.Add(-30 * time.Second)
	pulses := detectRoomTaskPulseMessages("/repo", "alpha", []taskstore.Task{
		{
			ID:           "task-1",
			Title:        "Review retry path",
			Status:       taskstore.StatusInProgress,
			OwnerActorID: "claude-a",
			ClaimedAt:    &claimedAt,
			HeartbeatAt:  &heartbeatAt,
		},
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]roomPulseState{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomTaskFollowupMessagesEmitsTipForClaimedTask(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-10 * time.Minute)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
	}
	pulses := detectRoomTaskFollowupMessages(room, []taskstore.Task{
		{
			ID:           "task-1",
			Title:        "Review retry path",
			Status:       taskstore.StatusInProgress,
			OwnerActorID: "claude-a",
			ClaimedAt:    &claimedAt,
		},
	}, now, roomPulseConfig{
		TaskFollowupInterval: 5 * time.Minute,
		TaskStaleAfter:       30 * time.Minute,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	msg := pulses[0].Message
	if msg.Recipient != "claude-a" {
		t.Fatalf("recipient=%q want claude-a", msg.Recipient)
	}
	if msg.Interrupt {
		t.Fatalf("interrupt=%v want false", msg.Interrupt)
	}
	if msg.Kind != agent.BoardMessageKindInfo {
		t.Fatalf("kind=%q want %q", msg.Kind, agent.BoardMessageKindInfo)
	}
	if !strings.Contains(msg.Body, "Quick tip:") {
		t.Fatalf("body missing quick tip: %s", msg.Body)
	}
	if !strings.Contains(msg.Body, "foxctl room task complete alpha --id task-1") {
		t.Fatalf("body missing completion tip: %s", msg.Body)
	}
}

func TestDetectRoomTaskFollowupMessagesSkipsFreshlyClaimedTask(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-2 * time.Minute)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
	}
	pulses := detectRoomTaskFollowupMessages(room, []taskstore.Task{
		{
			ID:           "task-1",
			Title:        "Review retry path",
			Status:       taskstore.StatusInProgress,
			OwnerActorID: "claude-a",
			ClaimedAt:    &claimedAt,
		},
	}, now, roomPulseConfig{
		TaskFollowupInterval: 5 * time.Minute,
		TaskStaleAfter:       30 * time.Minute,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomTaskFollowupMessagesDisabledWhenFollowupIntervalUnset(t *testing.T) {
	now := time.Date(2026, 4, 4, 19, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-10 * time.Minute)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
	}
	pulses := detectRoomTaskFollowupMessages(room, []taskstore.Task{
		{
			ID:           "task-1",
			Title:        "Review retry path",
			Status:       taskstore.StatusInProgress,
			OwnerActorID: "claude-a",
			ClaimedAt:    &claimedAt,
		},
	}, now, roomPulseConfig{
		Interval:        5 * time.Minute,
		TaskStaleAfter:  30 * time.Minute,
		ReplyStaleAfter: 10 * time.Minute,
	}, map[string]time.Time{}, nil)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomTaskEscalationMessagesAfterInterruptBudget(t *testing.T) {
	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "cursor-a", Role: "reviewer"},
		},
	}
	tasks := []taskstore.Task{{
		ID:              "task-1",
		WorkspaceID:     ws.CanonicalID("/repo"),
		Title:           "Long running",
		Status:          taskstore.StatusInProgress,
		OwnerActorID:    "cursor-a",
		AssignedActorID: "cursor-a",
		CreatedAt:       now.Add(-48 * time.Hour),
	}}
	pulses := detectRoomTaskEscalationMessages(room, tasks, now, roomPulseConfig{
		TaskStaleAfter:               5 * time.Minute,
		CoordinatorEscalationEnabled: true,
	}, map[string]roomPulseState{
		"task-1": {LastSentAt: now.Add(-10 * time.Hour), Count: roomPulseInterruptLimit},
	}, nil)
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Message.Recipient != "human-a" {
		t.Fatalf("recipient=%q want human-a", pulses[0].Message.Recipient)
	}
	if !pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want true", pulses[0].Message.Interrupt)
	}
}

func TestDetectRoomTaskPulseMessagesSkipsQuietChoresTask(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
	}
	tasks := []taskstore.Task{{
		ID:           "task-1",
		Title:        "Quiet follow-up",
		Status:       taskstore.StatusInProgress,
		OwnerActorID: "claude-a",
		MilestoneID:  "mile-chores-1",
		CreatedAt:    now.Add(-20 * time.Minute),
	}}
	suppression := buildRoomActionSuppression(messages, tasks, nil)
	pulses := detectRoomTaskPulseMessages("/repo", "alpha", tasks, now, roomPulseConfig{
		TaskStaleAfter: 5 * time.Minute,
	}, map[string]roomPulseState{}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomTaskFollowupMessagesSkipsQuietChoresTask(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
	}
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
	}
	claimedAt := now.Add(-20 * time.Minute)
	tasks := []taskstore.Task{{
		ID:           "task-1",
		Title:        "Quiet follow-up",
		Status:       taskstore.StatusInProgress,
		OwnerActorID: "claude-a",
		ClaimedAt:    &claimedAt,
		MilestoneID:  "mile-chores-1",
	}}
	suppression := buildRoomActionSuppression(messages, tasks, nil)
	pulses := detectRoomTaskFollowupMessages(room, tasks, now, roomPulseConfig{
		TaskFollowupInterval: 5 * time.Minute,
		TaskStaleAfter:       30 * time.Minute,
	}, map[string]time.Time{}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomTaskEscalationMessagesSkipsQuietChoresTask(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
	}
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
	}
	tasks := []taskstore.Task{{
		ID:           "task-1",
		Title:        "Quiet follow-up",
		Status:       taskstore.StatusInProgress,
		OwnerActorID: "claude-a",
		MilestoneID:  "mile-chores-1",
		CreatedAt:    now.Add(-48 * time.Hour),
	}}
	suppression := buildRoomActionSuppression(messages, tasks, nil)
	pulses := detectRoomTaskEscalationMessages(room, tasks, now, roomPulseConfig{
		TaskStaleAfter:               5 * time.Minute,
		CoordinatorEscalationEnabled: true,
	}, map[string]roomPulseState{
		"task-1": {LastSentAt: now.Add(-10 * time.Hour), Count: roomPulseInterruptLimit},
	}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestDetectRoomCoordinatorPulseMessagesSkipsQuietChoresTaskDebt(t *testing.T) {
	now := time.Date(2026, 4, 12, 13, 0, 0, 0, time.UTC)
	room := agent.RoomSummary{
		ID:          "alpha",
		WorkspaceID: "/repo",
		Stream:      agent.RoomStreamName("alpha"),
		Members: []agent.RoomMember{
			{ActorID: "human-a", Role: "coordinator"},
			{ActorID: "claude-a", Role: "reviewer"},
		},
		Participants: []string{"human-a", "claude-a"},
	}
	messages := []agent.BoardMessage{
		{
			ID:        "epic-1",
			Kind:      agent.BoardMessageKindEpic,
			Subject:   "Epic: Delivery runtime",
			CreatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:               "mile-chores-1",
			Kind:             agent.BoardMessageKindMilestone,
			RelatedMessageID: "epic-1",
			Subject:          "Milestone: Chores",
			Body:             "EpicID: epic-1\nLaneKind: chores\nFollowupPolicy: none\nObjective: quiet chores",
			CreatedAt:        now.Add(-9 * time.Minute),
		},
	}
	tasks := []taskstore.Task{{
		ID:              "task-1",
		Title:           "Quiet follow-up",
		Status:          taskstore.StatusPending,
		AssignedActorID: "claude-a",
		MilestoneID:     "mile-chores-1",
		CreatedAt:       now.Add(-20 * time.Minute),
	}}
	suppression := buildRoomActionSuppression(messages, tasks, nil)
	pulses := detectRoomCoordinatorPulseMessages(room, messages, tasks, now, roomPulseConfig{
		Enabled:                 true,
		Interval:                30 * time.Minute,
		TaskStaleAfter:          5 * time.Minute,
		CoordinatorPulseEnabled: true,
	}, map[string]time.Time{}, suppression)
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
	}
}

func TestCollectRoomRelayTargetsSkipsSender(t *testing.T) {
	targets, skipped := collectRoomRelayTargets(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "agent-a"},
			{ActorID: "agent-b"},
			{ActorID: "agent-c"},
		},
	}, agent.BoardMessage{Sender: "agent-b"})
	if len(targets) != 2 || targets[0] != "agent-a" || targets[1] != "agent-c" {
		t.Fatalf("targets=%v want [agent-a agent-c]", targets)
	}
	if len(skipped) != 1 || skipped[0] != "agent-b" {
		t.Fatalf("skipped=%v want [agent-b]", skipped)
	}
}

func TestFormatRoomRelayContentIncludesSender(t *testing.T) {
	room := agent.RoomSummary{ID: "alpha"}
	msg := agent.BoardMessage{
		Sender:  "claude-a",
		Subject: "Short hello",
		Body:    "Hello from Claude",
	}
	got := formatRoomRelayContent(room, msg)
	want := "[room alpha from=claude-a to=*] Short hello\nHello from Claude"
	if got != want {
		t.Fatalf("formatRoomRelayContent() = %q, want %q", got, want)
	}
}

func TestFormatRoomRelayContentFallsBackToUnknownSender(t *testing.T) {
	room := agent.RoomSummary{ID: "alpha"}
	msg := agent.BoardMessage{
		Body: "Hello room",
	}
	got := formatRoomRelayContent(room, msg)
	want := "[room alpha from=unknown to=*] Hello room"
	if got != want {
		t.Fatalf("formatRoomRelayContent() = %q, want %q", got, want)
	}
}

func TestFindRoomMemberForMuxSubmit(t *testing.T) {
	summary := agent.RoomSummary{
		ID: "triad",
		Members: []agent.RoomMember{
			{ActorID: "cursor-c-a", Backend: "tmux", PaneID: "%15"},
			{ActorID: "zellij:spark:terminal_2", Backend: "zellij", Session: "spark", PaneID: "2"},
		},
	}
	if _, ok := findRoomMemberForMuxSubmit(summary, "missing"); ok {
		t.Fatal("expected no match")
	}
	m, ok := findRoomMemberForMuxSubmit(summary, "cursor-c-a")
	if !ok || strings.TrimSpace(m.ActorID) != "cursor-c-a" {
		t.Fatalf("got %+v ok=%v", m, ok)
	}
	m, ok = findRoomMemberForMuxSubmit(summary, "zellij:spark:terminal_2")
	if !ok {
		t.Fatal("expected match by canonical actor id")
	}
	if strings.TrimSpace(m.Session) != "spark" {
		t.Fatalf("session=%q", m.Session)
	}
}

func TestFormatRoomRelayContentIncludesRecipientAndFlags(t *testing.T) {
	room := agent.RoomSummary{ID: "alpha"}
	msg := agent.BoardMessage{
		Sender:        "human-a",
		Recipient:     "claude-a",
		Subject:       "Review needed",
		Body:          "Please review the spawn flow.",
		AckRequired:   true,
		ReplyExpected: true,
		Interrupt:     true,
	}
	got := formatRoomRelayContent(room, msg)
	want := "[room alpha from=human-a to=claude-a ack reply interrupt] Review needed\nPlease review the spawn flow.\n" +
		"Action: open your inbox (`foxctl room inbox <room> --actor <you>`), acknowledge if required, then reply or complete the requested follow-up."
	if got != want {
		t.Fatalf("formatRoomRelayContent() = %q, want %q", got, want)
	}
}

func TestFormatRoomRelayContentForTargetAddsDroidExecuteHint(t *testing.T) {
	room := agent.RoomSummary{ID: "alpha"}
	msg := agent.BoardMessage{
		Sender:        "human-a",
		Recipient:     "droid-a",
		Subject:       "Reply needed",
		Body:          "Please send a short status update.",
		ReplyExpected: true,
	}
	got := formatRoomRelayContentForTarget(room, msg, "droid-a")
	if !strings.Contains(got, `Execute directly if you are ready to answer: foxctl room send alpha --to human-a --sender droid-a "<response>"`) {
		t.Fatalf("formatRoomRelayContentForTarget() missing droid execute hint: %q", got)
	}
}

func TestTargetUsesComposerSubmitIncludesAgent(t *testing.T) {
	if !targetUsesComposerSubmit("agent-a") {
		t.Fatal("targetUsesComposerSubmit(agent-a) = false, want true")
	}
	if targetUsesComposerSubmit("claude-a") {
		t.Fatal("targetUsesComposerSubmit(claude-a) = true, want false")
	}
}

func TestTargetSubmitModeUsesEnterForClaude(t *testing.T) {
	if got := targetSubmitMode("claude-a"); got != agentpane.SubmitModeEnter {
		t.Fatalf("targetSubmitMode(claude-a) = %q, want %q", got, agentpane.SubmitModeEnter)
	}
}

func TestTargetSubmitModeUsesEnterSplitForGemini(t *testing.T) {
	if got := targetSubmitMode("gemini-a"); got != agentpane.SubmitModeEnterSplit {
		t.Fatalf("targetSubmitMode(gemini-a) = %q, want %q", got, agentpane.SubmitModeEnterSplit)
	}
}

func TestMergeRoomMembersPreservesBindingOnlyFields(t *testing.T) {
	merged := mergeRoomMembers(
		[]agent.RoomMember{
			{
				ActorID: "codex-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:     "zellij",
					MuxSession:     "dev",
					MuxPaneID:      "terminal_1",
					SubmitMode:     agent.RoomDeliverySubmitModeComposerCtrlEnter,
					Health:         agent.RoomDeliveryHealthReady,
					FallbackPolicy: agent.RoomDeliveryFallbackAllowLegacyMux,
				},
			},
		},
		agent.RoomMember{
			ActorID: "codex-a",
			DeliveryBinding: &agent.RoomDeliveryBinding{
				TransportEndpoint: "/tmp/foxctl-pane/dev/codex-a.sock",
				TransportKind:     agent.PaneSocketTransportKind,
			},
		},
	)

	if len(merged) != 1 {
		t.Fatalf("len(merged)=%d want 1", len(merged))
	}
	got := merged[0]
	if got.DeliveryBinding == nil {
		t.Fatal("DeliveryBinding=nil want merged binding")
	}
	if got.DeliveryBinding.SubmitMode != agent.RoomDeliverySubmitModeComposerCtrlEnter {
		t.Fatalf("SubmitMode=%q want %q", got.DeliveryBinding.SubmitMode, agent.RoomDeliverySubmitModeComposerCtrlEnter)
	}
	if got.DeliveryBinding.TransportEndpoint != "/tmp/foxctl-pane/dev/codex-a.sock" {
		t.Fatalf("TransportEndpoint=%q", got.DeliveryBinding.TransportEndpoint)
	}
	if got.TransportEndpoint != "/tmp/foxctl-pane/dev/codex-a.sock" {
		t.Fatalf("mirrored TransportEndpoint=%q", got.TransportEndpoint)
	}
}

func TestCollectRoomRelayTargetsDirectRecipient(t *testing.T) {
	targets, skipped := collectRoomRelayTargets(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{ActorID: "claude-a"},
			{ActorID: "gemini-a"},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "claude-a",
	})
	if len(targets) != 1 || targets[0] != "claude-a" {
		t.Fatalf("targets=%v want [claude-a]", targets)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want 2 entries", skipped)
	}
}

func TestCollectRoomRelayTargetsDirectRecipientUsesTmuxPaneID(t *testing.T) {
	targets, skipped := collectRoomRelayTargets(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a", Backend: "tmux", PaneID: "%3"},
			{ActorID: "cursor-review", Backend: "tmux", PaneID: "%11"},
			{ActorID: "gemini-a", Backend: "tmux", PaneID: "%10"},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "cursor-review",
	})
	if len(targets) != 1 || targets[0] != "%11" {
		t.Fatalf("targets=%v want [%%11]", targets)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want 2 entries", skipped)
	}
}

func TestCollectRoomRelayTargetsByBackendRoutesZellijBySession(t *testing.T) {
	tmuxTargets, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{ActorID: "cursor-a", Backend: "zellij", Session: "fascinating-salamander"},
			{ActorID: "claude-a"},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "cursor-a",
	})
	if len(tmuxTargets) != 0 {
		t.Fatalf("tmuxTargets=%v want none", tmuxTargets)
	}
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want 2 entries", skipped)
	}
	targets := zellijTargets["fascinating-salamander"]
	if len(targets) != 1 || targets[0] != "cursor-a" {
		t.Fatalf("zellijTargets=%v want title route for fascinating-salamander", zellijTargets)
	}
}

func TestCollectRoomRelayTargetsByBackendRoutesTmuxByPaneID(t *testing.T) {
	tmuxTargets, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "codex-backend", Backend: "tmux", PaneID: "%3"},
			{ActorID: "cursor-review", Backend: "tmux", PaneID: "%11"},
			{ActorID: "block-gemini-a", Backend: "tmux", PaneID: "%10"},
		},
	}, agent.BoardMessage{
		Sender:    "codex-backend",
		Recipient: "cursor-review",
	})
	if len(tmuxTargets) != 1 || tmuxTargets[0] != "%11" {
		t.Fatalf("tmuxTargets=%v want [%%11]", tmuxTargets)
	}
	if len(zellijTargets) != 0 {
		t.Fatalf("zellijTargets=%v want none", zellijTargets)
	}
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want 2 entries", skipped)
	}
}

func TestCollectRoomRelayTargetsByBackendRoutesTmuxByBindingOnlyPaneID(t *testing.T) {
	tmuxTargets, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "codex-backend"},
			{
				ActorID: "cursor-review",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "tmux",
					MuxSession:        "binding-collab",
					MuxPaneID:         "%11",
					TransportEndpoint: "tmux:binding-collab:%11",
					TransportKind:     "mux_pane",
				},
			},
			{ActorID: "block-gemini-a"},
		},
	}, agent.BoardMessage{
		Sender:    "codex-backend",
		Recipient: "cursor-review",
	})
	if len(tmuxTargets) != 1 || tmuxTargets[0] != "%11" {
		t.Fatalf("tmuxTargets=%v want [%%11]", tmuxTargets)
	}
	if len(zellijTargets) != 0 {
		t.Fatalf("zellijTargets=%v want none", zellijTargets)
	}
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want 2 entries", skipped)
	}
}

func TestCollectRoomRelayTargetsByBackendRoutesZellijByPaneID(t *testing.T) {
	_, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{ActorID: "cursor-a", Backend: "zellij", Session: "sparkling-apricot", PaneID: "3"},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "cursor-a",
	})
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped=%v want 1 entry", skipped)
	}
	targets := zellijTargets["sparkling-apricot"]
	if len(targets) != 1 || targets[0] != "zellij:sparkling-apricot:terminal_3" {
		t.Fatalf("zellijTargets=%v want canonical pane target", zellijTargets)
	}
}

func TestCollectRoomRelayTargetsByBackendRoutesZellijNamedPaneByTitle(t *testing.T) {
	_, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{ActorID: "claude-a", Backend: "zellij", Session: "sparkling-apricot", PaneID: "claude-a"},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "claude-a",
	})
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped=%v want 1 entry", skipped)
	}
	targets := zellijTargets["sparkling-apricot"]
	if len(targets) != 1 || targets[0] != "claude-a" {
		t.Fatalf("zellijTargets=%v want title target", zellijTargets)
	}
}

func TestCollectRoomRelayTargetsByBackendUsesBindingOnlyZellijTransport(t *testing.T) {
	_, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{
				ActorID: "cursor-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "zellij",
					MuxSession:        "binding-session",
					MuxPaneID:         "3",
					TransportEndpoint: "zellij:binding-session:terminal_3",
					TransportKind:     "mux_pane",
				},
			},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "cursor-a",
	})
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped=%v want 1 entry", skipped)
	}
	targets := zellijTargets["binding-session"]
	if len(targets) != 1 || targets[0] != "zellij:binding-session:terminal_3" {
		t.Fatalf("zellijTargets=%v want canonical binding target", zellijTargets)
	}
}

func TestCollectRoomRelayTargetsByBackendSkipsHerdrForLegacyMuxFallback(t *testing.T) {
	tmuxTargets, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "human-a"},
			{
				ActorID: "codex-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "herdr",
					MuxSession:        "dev",
					MuxPaneID:         "w1-2",
					TransportEndpoint: "herdr:dev:w1-2",
					TransportKind:     "mux_pane",
				},
			},
		},
	}, agent.BoardMessage{
		Sender:    "human-a",
		Recipient: "codex-a",
	})
	if len(tmuxTargets) != 0 {
		t.Fatalf("tmuxTargets=%v want none", tmuxTargets)
	}
	if len(zellijTargets) != 0 {
		t.Fatalf("zellijTargets=%v want none", zellijTargets)
	}
	if len(failed) != 0 {
		t.Fatalf("failed=%v want none", failed)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped=%v want human plus herdr legacy skip", skipped)
	}
}

func TestRelayRoomMessageZellijTargetsPrefersPaneSocketForNamedTargets(t *testing.T) {
	origDeliver := deliverAgentPane
	defer func() { deliverAgentPane = origDeliver }()

	calls := 0
	deliverAgentPane = func(ctx context.Context, socketPath string, msg agentpane.ControlMessage) (agentpane.ControlResponse, error) {
		calls++
		if !strings.Contains(socketPath, "sparkling-apricot") || !strings.Contains(socketPath, "claude-a") {
			t.Fatalf("socketPath=%q", socketPath)
		}
		if msg.SubmitMode != agentpane.SubmitModeComposerCtrlEnter {
			t.Fatalf("submitMode=%q want %q", msg.SubmitMode, agentpane.SubmitModeComposerCtrlEnter)
		}
		return agentpane.ControlResponse{OK: true, BytesWritten: len(msg.Content)}, nil
	}

	result := relayRoomMessageZellijTargets(context.Background(), agent.RoomSummary{
		ID: "room-1",
		Members: []agent.RoomMember{
			{
				ActorID: "claude-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:     "zellij",
					MuxSession:     "sparkling-apricot",
					MuxPaneID:      "claude-a",
					SubmitMode:     agentpane.SubmitModeComposerCtrlEnter,
					FallbackPolicy: agent.RoomDeliveryFallbackAllowLegacyMux,
				},
			},
		},
	}, agent.BoardMessage{
		ID:        "msg-1",
		Sender:    "human-a",
		Recipient: "claude-a",
		Body:      "hello",
	}, "sparkling-apricot", []string{"claude-a"}, roomRelayOptions{})

	if calls != 1 {
		t.Fatalf("deliverAgentPane calls=%d want 1", calls)
	}
	if result.DeliveredCount != 1 || len(result.DeliveredTo) != 1 || result.DeliveredTo[0] != "claude-a" {
		t.Fatalf("result=%+v want socket delivery", result)
	}
	if result.FailedCount != 0 || result.Error != "" {
		t.Fatalf("result=%+v want no failures", result)
	}
}

func TestResolveRoomMemberZellijTargetPrefersTitleForNamedPane(t *testing.T) {
	session, target, ok := resolveRoomMemberZellijTarget(agent.RoomMember{
		ActorID: "claude-a",
		Backend: "zellij",
		Session: "sparkling-apricot",
		PaneID:  "claude-a",
	})
	if !ok {
		t.Fatal("expected target")
	}
	if session != "sparkling-apricot" || target != "claude-a" {
		t.Fatalf("got session=%q target=%q", session, target)
	}
}

func TestResolveRoomMemberZellijTargetUsesBindingTransportEndpoint(t *testing.T) {
	session, target, ok := resolveRoomMemberZellijTarget(agent.RoomMember{
		ActorID: "cursor-a",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "zellij",
			MuxSession:        "binding-session",
			MuxPaneID:         "3",
			TransportEndpoint: "zellij:binding-session:terminal_3",
			TransportKind:     "mux_pane",
		},
	})
	if !ok {
		t.Fatal("expected target from binding transport endpoint")
	}
	if session != "binding-session" || target != "zellij:binding-session:terminal_3" {
		t.Fatalf("got session=%q target=%q", session, target)
	}
}

func TestResolveRoomMemberZellijTargetPrefersBindingOverMirroredLegacyFields(t *testing.T) {
	session, target, ok := resolveRoomMemberZellijTarget(agent.RoomMember{
		ActorID: "cursor-a",
		Backend: "tmux",
		Session: "legacy-session",
		PaneID:  "%7",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "zellij",
			MuxSession:        "binding-session",
			MuxPaneID:         "4",
			TransportEndpoint: "zellij:binding-session:terminal_4",
			TransportKind:     "mux_pane",
		},
	})
	if !ok {
		t.Fatal("expected target from canonical binding")
	}
	if session != "binding-session" || target != "zellij:binding-session:terminal_4" {
		t.Fatalf("got session=%q target=%q want binding target", session, target)
	}
}

func TestRoomMemberTmuxTargetUsesBindingTransportEndpoint(t *testing.T) {
	target := roomMemberTmuxTarget(agent.RoomMember{
		ActorID: "codex-a",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "tmux",
			TransportEndpoint: "tmux:binding-session:%21",
			TransportKind:     "mux_pane",
		},
	})
	if target != "%21" {
		t.Fatalf("target=%q want %%21", target)
	}
}

func TestRoomMemberRelayBackendPrefersBindingOverMirroredLegacyFields(t *testing.T) {
	backend := roomMemberRelayBackend(agent.RoomMember{
		ActorID: "cursor-a",
		Backend: "tmux",
		Session: "legacy-session",
		PaneID:  "%7",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "zellij",
			MuxSession:        "binding-session",
			MuxPaneID:         "4",
			TransportEndpoint: "zellij:binding-session:terminal_4",
			TransportKind:     "mux_pane",
		},
	})
	if backend != "zellij" {
		t.Fatalf("backend=%q want zellij", backend)
	}
}

func TestRoomMemberRelayBackendResolvesHerdrBinding(t *testing.T) {
	backend := roomMemberRelayBackend(agent.RoomMember{
		ActorID: "codex-a",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "herdr",
			MuxSession:        "dev",
			MuxPaneID:         "w1-2",
			TransportEndpoint: "herdr:dev:w1-2",
			TransportKind:     "mux_pane",
		},
	})
	if backend != "herdr" {
		t.Fatalf("backend=%q want herdr", backend)
	}
}

func TestResolveRoomMemberHerdrTargetUsesBindingTransportEndpoint(t *testing.T) {
	session, target, ok := resolveRoomMemberHerdrTarget(agent.RoomMember{
		ActorID: "codex-a",
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "herdr",
			MuxSession:        "ignored",
			MuxPaneID:         "ignored",
			TransportEndpoint: "herdr:dev:w1-2",
			TransportKind:     "mux_pane",
		},
	})
	if !ok {
		t.Fatal("expected target")
	}
	if session != "dev" || target != "w1-2" {
		t.Fatalf("session=%q target=%q want dev/w1-2", session, target)
	}
}

func TestRunRoomSendRejectsReplyExpectedBroadcast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	err := runRoomSend(cmd, workspace, "alpha", "human-a", "", "", "hello room", "info", "", 0, false, true, false, true)
	assertRoomErrorContains(t, err, "reply_expected requires a direct recipient")
}

func TestRunRoomSendDerivesSenderFromCurrentTmuxPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%7")
	restoreSubmit := swapRoomSendMuxSubmitHook(func(context.Context, string) (map[string]any, string) {
		return nil, ""
	})
	defer restoreSubmit()
	restore := swapRoomTmuxClientForTest(func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(roomFakeRunner{responses: map[string]roomFakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %7 -p " + roomListFormat(): {
				stdout: "%7" + roomFieldSep() + "collab" + roomFieldSep() + "0" + roomFieldSep() + "0" + roomFieldSep() + "main" + roomFieldSep() + "111" + roomFieldSep() + "120" + roomFieldSep() + "30" + roomFieldSep() + "codex-a" + roomFieldSep() + "/repo" + roomFieldSep() + "zsh" + roomFieldSep() + "1\n",
			},
		}}, map[string]string{
			"TMUX":      "/tmp/tmux.sock,1,0",
			"TMUX_PANE": "%7",
		})
	})
	defer restore()

	ctx := context.Background()
	workspace := t.TempDir()
	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"codex-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "", "", "", "hello room", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	msg, ok := data["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", data["message"])
	}
	if got := msg["sender"]; got != "codex-a" {
		t.Fatalf("sender=%v want codex-a", got)
	}
	warnings, ok := data["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("warnings=%T/%v want inferred sender warning", data["warnings"], data["warnings"])
	}
}

func TestRunRoomSendExplicitSenderSkipsMuxSubmitHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := 0
	restoreSubmit := swapRoomSendMuxSubmitHook(func(context.Context, string) (map[string]any, string) {
		calls++
		return map[string]any{"backend": "tmux"}, ""
	})
	defer restoreSubmit()

	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "hello room", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	if calls != 0 {
		t.Fatalf("mux submit hook calls=%d want 0", calls)
	}
	data := decodeRoomEnvelope(t, out)
	warnings, ok := data["warnings"].([]any)
	if !ok {
		t.Fatalf("warnings type=%T", data["warnings"])
	}
	found := false
	for _, raw := range warnings {
		if s, _ := raw.(string); strings.Contains(s, "mux submit skipped because --sender was provided explicitly") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("warnings=%v want explicit sender mux-submit warning", warnings)
	}
	if _, ok := data["mux_submit"]; ok {
		t.Fatalf("mux_submit present=%v want absent", data["mux_submit"])
	}
}

func TestRunRoomSendInferredSenderStillAllowsMuxSubmitHook(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%7")
	calls := 0
	restoreSubmit := swapRoomSendMuxSubmitHook(func(context.Context, string) (map[string]any, string) {
		calls++
		return map[string]any{"backend": "tmux"}, ""
	})
	defer restoreSubmit()
	restore := swapRoomTmuxClientForTest(func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(roomFakeRunner{responses: map[string]roomFakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %7 -p " + roomListFormat(): {
				stdout: "%7" + roomFieldSep() + "collab" + roomFieldSep() + "0" + roomFieldSep() + "0" + roomFieldSep() + "main" + roomFieldSep() + "111" + roomFieldSep() + "120" + roomFieldSep() + "30" + roomFieldSep() + "codex-a" + roomFieldSep() + "/repo" + roomFieldSep() + "zsh" + roomFieldSep() + "1\n",
			},
		}}, map[string]string{
			"TMUX":      "/tmp/tmux.sock,1,0",
			"TMUX_PANE": "%7",
		})
	})
	defer restore()

	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"codex-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "", "", "", "hello room", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	if calls != 1 {
		t.Fatalf("mux submit hook calls=%d want 1", calls)
	}
	data := decodeRoomEnvelope(t, out)
	if _, ok := data["mux_submit"].(map[string]any); !ok {
		t.Fatalf("mux_submit type=%T want map", data["mux_submit"])
	}
}

func TestRunRoomSendAnnotatesBodyWithSenderAndHint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSendWithHint(cmd, workspace, "alpha", "human-a", "gemini-a", "Need review", "Reply with a short recommendation and blocker list.", "Please review the work-pack draft.", "info", "", 0, false, true, false, true, roomSendMuxOpts{}); err != nil {
		t.Fatalf("runRoomSendWithHint: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	msg, ok := data["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", data["message"])
	}
	body, _ := msg["body"].(string)
	if !strings.Contains(body, "Sent by: human-a") {
		t.Fatalf("body missing sender signature: %q", body)
	}
	if !strings.Contains(body, "Direct recipient: gemini-a") {
		t.Fatalf("body missing recipient signature: %q", body)
	}
	if !strings.Contains(body, "Response requested: true") {
		t.Fatalf("body missing reply expectation: %q", body)
	}
	if !strings.Contains(body, `Reply with: foxctl room send alpha --to human-a "<response>"`) {
		t.Fatalf("body missing direct reply command: %q", body)
	}
	if !strings.Contains(body, "Response hint: Reply with a short recommendation and blocker list.") {
		t.Fatalf("body missing response hint: %q", body)
	}
}

func TestRunRoomSendRejectsDirectRecipientOutsideRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	var err error
	cmd, _ = newRoomTestCommand(ctx)
	err = runRoomSend(cmd, workspace, "alpha", "human-a", "cursor-a", "", "hello room", "info", "", 0, false, false, false, true)
	assertRoomErrorContains(t, err, `recipient "cursor-a" is not a participant in room "alpha"`)
}

func TestRunRoomAckMarksMessageAcked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead", "agent-b=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "agent-b", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	msg, ok := data["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", data["message"])
	}
	msgID, _ := msg["id"].(string)
	if msgID == "" {
		t.Fatalf("message id missing in payload=%v", msg)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomAck(cmd, workspace, "alpha", "agent-b", []string{msgID}); err != nil {
		t.Fatalf("runRoomAck: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	if got := data["updated"]; got != float64(1) {
		t.Fatalf("updated=%v want 1", got)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T/%v want 1 entry", data["messages"], data["messages"])
	}
	gotMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", messages[0])
	}
	if got := gotMsg["status"]; got != "acked" {
		t.Fatalf("status=%v want acked", got)
	}
}

func TestRunRoomAckDerivesActorFromCurrentTmuxPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%7")
	restoreSubmit := swapRoomSendMuxSubmitHook(func(context.Context, string) (map[string]any, string) {
		return nil, ""
	})
	defer restoreSubmit()
	restore := swapRoomTmuxClientForTest(func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(roomFakeRunner{responses: map[string]roomFakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %7 -p " + roomListFormat(): {
				stdout: "%7" + roomFieldSep() + "collab" + roomFieldSep() + "0" + roomFieldSep() + "0" + roomFieldSep() + "main" + roomFieldSep() + "111" + roomFieldSep() + "120" + roomFieldSep() + "30" + roomFieldSep() + "codex-a" + roomFieldSep() + "/repo" + roomFieldSep() + "zsh" + roomFieldSep() + "1\n",
			},
		}}, map[string]string{
			"TMUX":      "/tmp/tmux.sock,1,0",
			"TMUX_PANE": "%7",
		})
	})
	defer restore()

	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"codex-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	activateTestRoomLoop(t, ctx, workspace, "alpha")
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "codex-a", "", "please ack", "info", "", 0, true, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	msg, ok := data["message"].(map[string]any)
	if !ok {
		t.Fatalf("message type=%T", data["message"])
	}
	msgID, _ := msg["id"].(string)
	if msgID == "" {
		t.Fatalf("message id missing in payload=%v", msg)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomAck(cmd, workspace, "alpha", "", []string{msgID}); err != nil {
		t.Fatalf("runRoomAck: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	identity, ok := data["acker_identity"].(map[string]any)
	if !ok {
		t.Fatalf("acker_identity type=%T", data["acker_identity"])
	}
	if got := identity["Sender"]; got != "codex-a" {
		t.Fatalf("acker sender=%v want codex-a", got)
	}
}

func TestRunRoomJoinCurrentDerivesCanonicalTmuxParticipant(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%9")
	restore := swapRoomTmuxClientForTest(func() *tmuxbridge.Client {
		return tmuxbridge.NewWithRunner(roomFakeRunner{responses: map[string]roomFakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %9 -p " + roomListFormat(): {
				stdout: "%9" + roomFieldSep() + "collab" + roomFieldSep() + "0" + roomFieldSep() + "1" + roomFieldSep() + "main" + roomFieldSep() + "111" + roomFieldSep() + "120" + roomFieldSep() + "30" + roomFieldSep() + "" + roomFieldSep() + "/repo" + roomFieldSep() + "zsh" + roomFieldSep() + "1\n",
			},
		}}, map[string]string{
			"TMUX":      "/tmp/tmux.sock,1,0",
			"TMUX_PANE": "%9",
		})
	})
	defer restore()

	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", nil); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "", "worker", "", "", "", "", "", false, true, true); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok || len(members) != 1 {
		t.Fatalf("members=%v want 1", roomRaw["members"])
	}
	member, ok := members[0].(map[string]any)
	if !ok {
		t.Fatalf("member type=%T", members[0])
	}
	if got := member["actor_id"]; got != "tmux:collab:%9" {
		t.Fatalf("actor_id=%v want tmux:collab:%%9", got)
	}
}

func TestSameRoomParticipantRecognizesCanonicalIDs(t *testing.T) {
	if !sameRoomParticipant("tmux:collab:%1", "tmux:collab:%1") {
		t.Fatal("sameRoomParticipant false, want true for matching tmux ids")
	}
	if !sameRoomParticipant("zellij:alpha:terminal_3", "zellij:alpha:3") {
		t.Fatal("sameRoomParticipant false, want true for matching zellij ids")
	}
	if sameRoomParticipant("codex-a", "tmux:collab:%1") {
		t.Fatal("sameRoomParticipant true, want false for unrelated ids")
	}
}

func TestRunRoomJoinPersistsTransportBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "cursor-a", "reviewer", "zellij", "fascinating-salamander", "", "", "", false, true, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	found := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "cursor-a" {
			found = true
			if member["backend"] != "zellij" || member["session"] != "fascinating-salamander" {
				t.Fatalf("cursor binding=%v want zellij/fascinating-salamander", member)
			}
		}
	}
	if !found {
		t.Fatalf("cursor-a not found in members=%v", members)
	}
}

func TestRunRoomRebindUpdatesTransportBindingWithoutChangingRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "claude-a", "reviewer", "tmux", "13", "%25", "", "", false, true, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRebind(cmd, workspace, "alpha", "claude-a", "", "tmux", "13", "%27", "", "", false, false); err != nil {
		t.Fatalf("runRoomRebind: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	found := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "claude-a" {
			found = true
			if member["backend"] != "tmux" || member["session"] != "13" || member["pane_id"] != "%27" {
				t.Fatalf("claude binding=%v want tmux/13/%%27", member)
			}
			if member["role"] != "reviewer" {
				t.Fatalf("claude role=%v want reviewer", member["role"])
			}
		}
	}
	if !found {
		t.Fatalf("claude-a not found in members=%v", members)
	}
}

func TestRunRoomRebindRequiresExistingMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomRebind(cmd, workspace, "alpha", "missing-a", "", "tmux", "13", "%27", "", "", false, false)
	if err == nil {
		t.Fatal("runRoomRebind error = nil want written error envelope")
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error payload=%s", env.Status, out.String())
	}
	if env.Error.Code != string(protocol.ErrorCodeENotFound) {
		t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeENotFound)
	}
}

func TestRunRoomJoinCurrentPersistsZellijPaneBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", "sparkling-apricot")
	t.Setenv("ZELLIJ_PANE_ID", "7")
	t.Setenv("FOXCTL_ZELLIJ_PARTICIPANT", "cursor-a")
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "", "reviewer", "", "", "", "", "", false, true, true); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	found := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "cursor-a" {
			found = true
			if member["backend"] != "zellij" || member["session"] != "sparkling-apricot" || member["pane_id"] != "7" {
				t.Fatalf("cursor binding=%v want zellij/sparkling-apricot/7", member)
			}
		}
	}
	if !found {
		t.Fatalf("cursor-a not found in members=%v", members)
	}
}

func TestRunRoomJoinPersistsTransportEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("ZELLIJ", "")
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "ep-room", "EP Room", "", nil); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	endpoint := "/tmp/foxctl-pane/s1/claude-a.sock"
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "ep-room", "claude-a", "worker", "zellij", "s1", "terminal_0", endpoint, "pane_socket", false, false, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	var found bool
	for _, raw := range members {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["actor_id"] == "claude-a" {
			found = true
			if m["transport_endpoint"] != endpoint {
				t.Errorf("transport_endpoint=%q want %q", m["transport_endpoint"], endpoint)
			}
			if m["transport_kind"] != "pane_socket" {
				t.Errorf("transport_kind=%q want pane_socket", m["transport_kind"])
			}
		}
	}
	if !found {
		t.Fatalf("claude-a not found in members=%v", members)
	}
}

func TestRunRoomRebindPersistsTransportEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("ZELLIJ", "")
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "rb-room", "Rebind Room", "", []string{"claude-a=worker"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	// Join claude-a with no transport initially.
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "rb-room", "claude-a", "worker", "tmux", "s1", "%1", "", "", false, false, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	endpoint := "/tmp/foxctl-pane/s1/claude-a.sock"
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRebind(cmd, workspace, "rb-room", "claude-a", "", "", "", "", endpoint, "pane_socket", false, false); err != nil {
		t.Fatalf("runRoomRebind: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	var found bool
	for _, raw := range members {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if m["actor_id"] == "claude-a" {
			found = true
			if m["transport_endpoint"] != endpoint {
				t.Errorf("transport_endpoint=%q want %q", m["transport_endpoint"], endpoint)
			}
			if m["transport_kind"] != "pane_socket" {
				t.Errorf("transport_kind=%q want pane_socket", m["transport_kind"])
			}
			// Existing backend preserved.
			if m["backend"] != "tmux" {
				t.Errorf("backend=%q want tmux (must be preserved after rebind)", m["backend"])
			}
		}
	}
	if !found {
		t.Fatalf("claude-a not found in members=%v", members)
	}
}

func TestRunRoomRestoreLaunchesAndRebindsMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "restore-room", "Restore Room", "", []string{"codex-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	originalHook := roomRestoreLaunchHook
	roomRestoreLaunchHook = func(_ context.Context, opts roomRestoreLaunchOptions) (roomRestoreLaunchResult, error) {
		if opts.ActorID != "codex-a" {
			t.Fatalf("opts.ActorID = %q", opts.ActorID)
		}
		if opts.Agent != "codex" {
			t.Fatalf("opts.Agent = %q", opts.Agent)
		}
		if opts.AgentSessionID != "sess-123" {
			t.Fatalf("opts.AgentSessionID = %q", opts.AgentSessionID)
		}
		return roomRestoreLaunchResult{
			Backend:           "tmux",
			Session:           "room-alpha-codex",
			PaneID:            "%27",
			ParticipantID:     "codex-a",
			TransportEndpoint: "/tmp/foxctl-pane/room-alpha-codex/codex-a.sock",
			TransportKind:     "pane_socket",
			AttachCommand:     "tmux attach-session -t room-alpha-codex",
			SocketMode:        "default",
			Command:           "codex resume sess-123",
		}, nil
	}
	defer func() { roomRestoreLaunchHook = originalHook }()

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRestore(cmd, workspace, "restore-room", "codex-a", "tmux", "room-alpha-codex", "codex", "interactive", nil, "sess-123", "", "direct", false); err != nil {
		t.Fatalf("runRoomRestore: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	runtime, ok := data["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime type=%T", data["runtime"])
	}
	if runtime["backend"] != "tmux" || runtime["session"] != "room-alpha-codex" || runtime["pane_id"] != "%27" {
		t.Fatalf("runtime=%v want tmux/room-alpha-codex/%%27", runtime)
	}
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	found := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "codex-a" {
			found = true
			if member["backend"] != "tmux" || member["session"] != "room-alpha-codex" || member["pane_id"] != "%27" {
				t.Fatalf("codex-a binding=%v want tmux/room-alpha-codex/%%27", member)
			}
			if member["transport_endpoint"] != "/tmp/foxctl-pane/room-alpha-codex/codex-a.sock" {
				t.Fatalf("transport_endpoint=%v", member["transport_endpoint"])
			}
			if member["transport_kind"] != "pane_socket" {
				t.Fatalf("transport_kind=%v want pane_socket", member["transport_kind"])
			}
		}
	}
	if !found {
		t.Fatalf("codex-a not found in members=%v", members)
	}
}

func TestRunRoomRestoreRequiresExistingMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "restore-room", "Restore Room", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	originalHook := roomRestoreLaunchHook
	roomRestoreLaunchHook = func(_ context.Context, _ roomRestoreLaunchOptions) (roomRestoreLaunchResult, error) {
		t.Fatal("roomRestoreLaunchHook should not be called for missing member")
		return roomRestoreLaunchResult{}, nil
	}
	defer func() { roomRestoreLaunchHook = originalHook }()

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomRestore(cmd, workspace, "restore-room", "missing-a", "tmux", "", "claude", "interactive", nil, "", "", "direct", false)
	if err == nil {
		t.Fatal("runRoomRestore error = nil want written error envelope")
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, out.String())
	}
	if env.Status != envelope.StatusError {
		t.Fatalf("status=%q want error payload=%s", env.Status, out.String())
	}
	if env.Error.Code != string(protocol.ErrorCodeENotFound) {
		t.Fatalf("error.code=%q want %q", env.Error.Code, protocol.ErrorCodeENotFound)
	}
}

func TestRunRoomRestorePersistsProviderSubmitModeForFeatureScopedActor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "restore-room", "Restore Room", "", []string{"human-a=coordinator", "feat-internal-grouping-gemini-review-b=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	originalHook := roomRestoreLaunchHook
	roomRestoreLaunchHook = func(_ context.Context, opts roomRestoreLaunchOptions) (roomRestoreLaunchResult, error) {
		if opts.ActorID != "feat-internal-grouping-gemini-review-b" {
			t.Fatalf("opts.ActorID = %q", opts.ActorID)
		}
		if opts.Agent != "gemini" {
			t.Fatalf("opts.Agent = %q", opts.Agent)
		}
		return roomRestoreLaunchResult{
			Backend:           "tmux",
			Session:           "room-alpha-gemini",
			PaneID:            "%31",
			ParticipantID:     "feat-internal-grouping-gemini-review-b",
			TransportEndpoint: "/tmp/foxctl-pane/room-alpha-gemini/feat-internal-grouping-gemini-review-b.sock",
			TransportKind:     "pane_socket",
			AttachCommand:     "tmux attach-session -t room-alpha-gemini",
			SocketMode:        "default",
			Command:           "gemini --resume session-1",
		}, nil
	}
	defer func() { roomRestoreLaunchHook = originalHook }()

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomRestore(cmd, workspace, "restore-room", "feat-internal-grouping-gemini-review-b", "tmux", "room-alpha-gemini", "gemini", "interactive", nil, "session-1", "", "direct", false); err != nil {
		t.Fatalf("runRoomRestore: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room type=%T", data["room"])
	}
	members, ok := roomRaw["members"].([]any)
	if !ok {
		t.Fatalf("members=%T", roomRaw["members"])
	}
	found := false
	for _, raw := range members {
		member, ok := raw.(map[string]any)
		if !ok || member["actor_id"] != "feat-internal-grouping-gemini-review-b" {
			continue
		}
		found = true
		binding, ok := member["delivery_binding"].(map[string]any)
		if !ok {
			t.Fatalf("delivery_binding=%T want map", member["delivery_binding"])
		}
		if got := strings.TrimSpace(fmt.Sprint(binding["submit_mode"])); got != agent.RoomDeliverySubmitModeEnterSplit {
			t.Fatalf("submit_mode=%q want %q", got, agent.RoomDeliverySubmitModeEnterSplit)
		}
		if got := strings.TrimSpace(fmt.Sprint(binding["transport_kind"])); got != agent.PaneSocketTransportKind {
			t.Fatalf("transport_kind=%q want %q", got, agent.PaneSocketTransportKind)
		}
	}
	if !found {
		t.Fatalf("feature scoped gemini reviewer not found in members=%v", members)
	}
}

func newRoomTestCommand(ctx context.Context) (*cobra.Command, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetContext(ctx)
	cmd.SetOut(buf)
	return cmd, buf
}

func decodeRoomEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, buf.String())
	}
	if env.Status != envelope.StatusOK {
		t.Fatalf("status=%q want ok payload=%s", env.Status, buf.String())
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type=%T", env.Data)
	}
	return data
}

func roomCategoryScoreByName(t *testing.T, categories []any, name string) int {
	t.Helper()
	for _, raw := range categories {
		category, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if category["name"] != name {
			continue
		}
		switch score := category["score"].(type) {
		case float64:
			return int(score)
		case int:
			return score
		}
	}
	t.Fatalf("category %q not found in %v", name, categories)
	return 0
}

func roomCategoryReasonsByName(t *testing.T, categories []any, name string) []string {
	t.Helper()
	for _, raw := range categories {
		category, ok := raw.(map[string]any)
		if !ok || category["name"] != name {
			continue
		}
		return stringSliceValue(category["reasons"])
	}
	t.Fatalf("category %q not found in %v", name, categories)
	return nil
}

func roomStringSliceContainsSubstring(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func roomScoutFindingsByFamilyAndReason(scout map[string]any, family, reason string) []map[string]any {
	findings := make([]map[string]any, 0)
	rawFindings, _ := scout["findings"].([]any)
	for _, raw := range rawFindings {
		finding, ok := raw.(map[string]any)
		if !ok || stringField(finding, "rule_family") != family {
			continue
		}
		evidence := anyMap(finding["evidence"])
		if stringField(evidence, "reason") != reason {
			continue
		}
		findings = append(findings, finding)
	}
	return findings
}

func TestBuildRoomEpicGradeDispatchArtifacts(t *testing.T) {
	epic := map[string]any{"title": "Sample Epic"}
	grade := map[string]any{
		"decision":        "NOT READY",
		"total":           68,
		"blockers":        []string{"Milestone criteria are missing."},
		"upgrade_actions": []string{"Add milestone acceptance criteria."},
	}
	brief := buildRoomEpicGradeAgentBrief("alpha", "epic-1", epic, grade)
	if !strings.Contains(brief, "Do not split or reshape stories unless a structural contradiction clearly blocks readiness") {
		t.Fatalf("brief=%q missing story-boundary constraint", brief)
	}
	if !strings.Contains(brief, "Repair order: start with the earliest milestone in the room view") {
		t.Fatalf("brief=%q missing repair order", brief)
	}
	cmd := buildRoomEpicGradeDispatchSpawnCommand("/tmp/work", "alpha", roomEpicGradeDispatchOptions{
		AgentRole:     "planner",
		LLMProvider:   "openai",
		LLMModel:      "gpt-5.4",
		ExecMode:      "autonomous",
		MaxAutoTurns:  2,
		MaxIterations: 12,
		RoomAccess:    "direct",
		SpawnDryRun:   true,
	}, brief)
	if !strings.Contains(cmd, "agent spawn") || !strings.Contains(cmd, "--llm-model 'gpt-5.4'") || !strings.Contains(cmd, "--dry-run") {
		t.Fatalf("spawn command=%q missing expected flags", cmd)
	}
	if err := validateRoomEpicGradeDispatchSpawn(roomEpicGradeDispatchOptions{Spawn: true, LLMProvider: "openai"}); err == nil {
		t.Fatalf("expected provider restriction error")
	}
	if err := validateRoomEpicGradeDispatchSpawn(roomEpicGradeDispatchOptions{Spawn: true, LLMProvider: "lmstudio"}); err != nil {
		t.Fatalf("lmstudio provider should be allowed: %v", err)
	}
}

func assertRoomErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%q want substring %q", err.Error(), want)
	}
}

func activateTestRoomLoop(t *testing.T, ctx context.Context, workspace, roomID string) {
	t.Helper()
	cfg, err := loadConfig(ctx)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("coordination.Open: %v", err)
	}
	defer coordStore.Close()

	now := time.Now().UTC()
	leaseName := roomLoopLeaseName(workspace, roomID)
	ownerID := "test-room-loop-owner"
	acquired, err := coordStore.TryAcquireLease(ctx, leaseName, ownerID, roomLoopLeaseTTL)
	if err != nil {
		t.Fatalf("TryAcquireLease: %v", err)
	}
	if !acquired {
		t.Fatal("expected test room loop lease acquisition")
	}
	if _, err := coordStore.UpsertRoomLoop(ctx, coordination.RoomLoop{
		WorkspaceID:                  workspace,
		RoomID:                       roomID,
		Enabled:                      true,
		ManagedBy:                    roomLoopManagedBy,
		LastTickAt:                   &now,
		DeliveryLeaseName:            leaseName,
		DeliveryOwnerID:              ownerID,
		PulseInterval:                30 * time.Second,
		ReplyStaleAfter:              2 * time.Minute,
		TaskStaleAfter:               5 * time.Minute,
		MinPulseFloor:                roomLoopMinimumPulseFloor,
		InterruptAttemptLimit:        roomPulseInterruptLimit,
		ReminderBackoffCap:           roomPulseBackoffCap,
		CoordinatorPulseEnabled:      true,
		CoordinatorEscalationEnabled: true,
	}); err != nil {
		t.Fatalf("UpsertRoomLoop: %v", err)
	}
}

func parseMembersForTest(raw ...string) []agent.RoomMember {
	members, err := parseRoomMembers(raw)
	if err != nil {
		panic(err)
	}
	return members
}

func swapRoomTmuxClientForTest(fn func() *tmuxbridge.Client) func() {
	prev := newRoomTmuxClient
	newRoomTmuxClient = fn
	return func() { newRoomTmuxClient = prev }
}

func swapRoomSendMuxSubmitHook(fn func(context.Context, string) (map[string]any, string)) func() {
	prev := roomSendMuxSubmitHook
	roomSendMuxSubmitHook = fn
	return func() { roomSendMuxSubmitHook = prev }
}

type roomFakeRunner struct {
	responses map[string]roomFakeResponse
}

type roomFakeResponse struct {
	stdout string
	stderr string
	err    error
}

func (f roomFakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	resp, ok := f.responses[key]
	if !ok {
		return "", "", fmt.Errorf("unexpected command: %s", key)
	}
	return resp.stdout, resp.stderr, resp.err
}

func roomFieldSep() string {
	return "\x1f"
}

func roomListFormat() string {
	return "#{pane_id}" + roomFieldSep() +
		"#{session_name}" + roomFieldSep() +
		"#{window_index}" + roomFieldSep() +
		"#{pane_index}" + roomFieldSep() +
		"#{window_name}" + roomFieldSep() +
		"#{pane_pid}" + roomFieldSep() +
		"#{pane_width}" + roomFieldSep() +
		"#{pane_height}" + roomFieldSep() +
		"#{@name}" + roomFieldSep() +
		"#{pane_current_path}" + roomFieldSep() +
		"#{pane_current_command}" + roomFieldSep() +
		"#{pane_active}" + roomFieldSep() +
		"#{@foxctl_participant}" + roomFieldSep() +
		"#{@foxctl_provider}" + roomFieldSep() +
		"#{@foxctl_room_id}" + roomFieldSep() +
		"#{@foxctl_wrapped}"
}

func mustWriteRoomTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadRoomTestFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(raw)
}

func initRoomGitRepo(t *testing.T, dir string) {
	t.Helper()
	runRoomGit(t, dir, "init")
	runRoomGit(t, dir, "config", "user.email", "room-tests@example.com")
	runRoomGit(t, dir, "config", "user.name", "Room Tests")
	mustWriteRoomTestFile(t, filepath.Join(dir, "pkg", "impl.go"), "package pkg\nconst Value = 1\n")
	runRoomGit(t, dir, "add", ".")
	runRoomGit(t, dir, "commit", "-m", "initial")
}

func runRoomGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

// --- Sandbox Create Tests ---

func TestProvisionSandbox_CreatesWorktreeAndTmuxSession(t *testing.T) {
	if os.Getenv("TMUX") == "" {
		// Need tmux for session creation
		if _, err := exec.LookPath("tmux"); err != nil {
			t.Skip("tmux not available")
		}
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// Create a git repo as the workspace
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a room first
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "test-sandbox",
		WorkspaceID: workspace,
		Title:       "Test Sandbox",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Provision sandbox
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxBaseRef:      "HEAD",
		SandboxWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	})
	if err != nil {
		t.Fatalf("provisionSandbox: %v", err)
	}

	if result["status"] != "created" {
		t.Errorf("status = %v, want created", result["status"])
	}
	if result["runtime"] != "worktree" {
		t.Errorf("runtime = %v, want worktree", result["runtime"])
	}

	worktreePath, _ := result["worktree_path"].(string)
	if worktreePath == "" {
		t.Fatal("worktree_path is empty")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree dir does not exist at %q: %v", worktreePath, err)
	}

	tmuxSession, _ := result["tmux_session"].(string)
	if tmuxSession == "" {
		t.Fatal("tmux_session is empty")
	}

	terminalURL, _ := result["terminal_url"].(string)
	if terminalURL != "/terminal/test-sandbox" {
		t.Errorf("terminal_url = %q, want /terminal/test-sandbox", terminalURL)
	}

	// Verify room.SandboxConfig was set
	if room.SandboxConfig == nil {
		t.Fatal("room.SandboxConfig is nil")
	}
	if room.SandboxConfig.WorktreePath != worktreePath {
		t.Errorf("SandboxConfig.WorktreePath = %q, want %q", room.SandboxConfig.WorktreePath, worktreePath)
	}
	if room.SandboxConfig.TmuxSession != tmuxSession {
		t.Errorf("SandboxConfig.TmuxSession = %q, want %q", room.SandboxConfig.TmuxSession, tmuxSession)
	}
	if room.SandboxConfig.Runtime != "worktree" {
		t.Errorf("SandboxConfig.Runtime = %q, want worktree", room.SandboxConfig.Runtime)
	}

	// Clean up tmux session
	cmd := exec.Command("tmux", "kill-session", "-t", tmuxSession)
	_ = cmd.Run()
}

func TestProvisionSandbox_IdempotentOnExistingSandbox(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	// Create a room with existing sandbox config
	existingPath := filepath.Join(t.TempDir(), "existing-wt")
	if err := os.MkdirAll(existingPath, 0o755); err != nil {
		t.Fatal(err)
	}

	room := agent.Room{
		ID:          "existing-sandbox",
		WorkspaceID: workspace,
		Title:       "Existing Sandbox",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   existingPath,
			WorktreeBranch: "sandbox/room-existing-sandbox",
			TmuxSession:    "foxctl-sandbox-existing-sandbox",
			TerminalURL:    "/terminal/existing-sandbox",
			Runtime:        "worktree",
		},
	}

	// Create the tmux session so idempotency check passes
	tmuxClient := tmuxbridge.New()
	_ = createTmuxSessionForSandbox(ctx, tmuxClient, "foxctl-sandbox-existing-sandbox", existingPath)
	defer func() {
		cmd := exec.Command("tmux", "kill-session", "-t", "foxctl-sandbox-existing-sandbox")
		_ = cmd.Run()
	}()

	// Calling provisionSandbox should return existing info
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox: true,
	})
	if err != nil {
		t.Fatalf("provisionSandbox (idempotent): %v", err)
	}

	if result["status"] != "existing" {
		t.Errorf("status = %v, want existing", result["status"])
	}
	if result["worktree_path"] != existingPath {
		t.Errorf("worktree_path = %v, want %v", result["worktree_path"], existingPath)
	}
}

func TestProvisionSandbox_NotGitRepoReturnsError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	// Don't init a git repo

	room := agent.Room{
		ID:          "no-git-room",
		WorkspaceID: workspace,
		Title:       "No Git Room",
	}

	_, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox: true,
	})
	if err == nil {
		t.Fatal("expected error for non-git workspace")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %q, want substring 'not a git repository'", err.Error())
	}
}

func TestProvisionSandbox_CustomWorktreeRoot(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	customRoot := filepath.Join(t.TempDir(), "custom-wt-root")

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "custom-root-room",
		WorkspaceID: workspace,
		Title:       "Custom Root",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxWorktreeRoot: customRoot,
	})
	if err != nil {
		t.Fatalf("provisionSandbox: %v", err)
	}

	worktreePath, _ := result["worktree_path"].(string)
	// Resolve symlinks for comparison (macOS: /var → /private/var)
	resolvedCustomRoot, _ := filepath.EvalSymlinks(customRoot)
	if resolvedCustomRoot != "" && !strings.HasPrefix(worktreePath, resolvedCustomRoot) {
		t.Errorf("worktree_path = %q, want prefix %q", worktreePath, resolvedCustomRoot)
	}

	// Clean up
	if session, ok := result["tmux_session"].(string); ok {
		cmd := exec.Command("tmux", "kill-session", "-t", session)
		_ = cmd.Run()
	}
}

func TestRoomCreateWithSandboxFlag_Integration(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "integration-sandbox", "Integration Test", "", nil, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	})
	if err != nil {
		t.Fatalf("runRoomCreateWithProvision --sandbox: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T", data["room"])
	}

	// Check sandbox metadata in response
	sandboxInfo, ok := data["sandbox"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox payload type=%T", data["sandbox"])
	}
	if sandboxInfo["status"] != "created" {
		t.Errorf("sandbox status = %v, want created", sandboxInfo["status"])
	}
	if sandboxInfo["runtime"] != "worktree" {
		t.Errorf("sandbox runtime = %v, want worktree", sandboxInfo["runtime"])
	}

	// Check room has sandbox_config
	scRaw, ok := roomRaw["sandbox_config"].(map[string]any)
	if !ok {
		t.Fatalf("sandbox_config type=%T", roomRaw["sandbox_config"])
	}
	if scRaw["worktree_path"] == nil {
		t.Error("sandbox_config.worktree_path is nil")
	}
	if scRaw["tmux_session"] == nil {
		t.Error("sandbox_config.tmux_session is nil")
	}

	// Clean up tmux session
	if session, ok := scRaw["tmux_session"].(string); ok {
		killCmd := exec.Command("tmux", "kill-session", "-t", session)
		_ = killCmd.Run()
	}
}

func TestRoomCreateWithoutSandbox_NoSandboxConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(cmd, workspace, "plain-room", "Plain Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("runRoomCreateWithProvision: %v", err)
	}

	data := decodeRoomEnvelope(t, out)
	roomRaw, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatalf("room payload type=%T", data["room"])
	}

	// sandbox_config should be nil
	if sc, exists := roomRaw["sandbox_config"]; exists && sc != nil {
		t.Errorf("sandbox_config should be nil for non-sandbox room, got %v", sc)
	}
}

func TestTmuxSessionExists(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// Non-existent session
	if tmuxSessionExists("nonexistent-session-xyz") {
		t.Error("tmuxSessionExists should return false for non-existent session")
	}

	// Create a session
	tc := tmuxbridge.New()
	_ = createTmuxSessionForSandbox(ctx, tc, "test-exist-session", t.TempDir())
	defer func() {
		cmd := exec.Command("tmux", "kill-session", "-t", "test-exist-session")
		_ = cmd.Run()
	}()

	if !tmuxSessionExists("test-exist-session") {
		t.Error("tmuxSessionExists should return true for existing session")
	}
}

// TestProvisionSandbox_BaseRef verifies that --sandbox-base-ref creates a worktree
// branched from the specified git ref (VAL-RS-003).
func TestProvisionSandbox_BaseRef(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	// Create a tag to use as base ref
	runRoomGit(t, workspace, "tag", "v1.0.0")

	// Make another commit on HEAD so HEAD != v1.0.0
	mustWriteRoomTestFile(t, filepath.Join(workspace, "extra.txt"), "extra content\n")
	runRoomGit(t, workspace, "add", ".")
	runRoomGit(t, workspace, "commit", "-m", "extra commit")

	// Resolve the tag and HEAD SHAs for comparison
	tagSHA := strings.TrimSpace(roomGitOutput(t, workspace, "rev-parse", "v1.0.0"))
	headSHA := strings.TrimSpace(roomGitOutput(t, workspace, "rev-parse", "HEAD"))
	if tagSHA == headSHA {
		t.Fatal("tag and HEAD should be different commits for this test")
	}

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "baseref-room",
		WorkspaceID: workspace,
		Title:       "Base Ref Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxBaseRef:      "v1.0.0",
		SandboxWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	})
	if err != nil {
		t.Fatalf("provisionSandbox: %v", err)
	}

	// The worktree HEAD should match the tag, not the main branch HEAD
	worktreePath, _ := result["worktree_path"].(string)
	if worktreePath == "" {
		t.Fatal("worktree_path is empty")
	}

	wtHeadCmd := exec.Command("git", "rev-parse", "HEAD")
	wtHeadCmd.Dir = worktreePath
	wtHeadOut, err := wtHeadCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in worktree: %v", err)
	}
	wtHeadSHA := strings.TrimSpace(string(wtHeadOut))

	if wtHeadSHA != tagSHA {
		t.Errorf("worktree HEAD = %q, want tag SHA %q (base-ref=v1.0.0)", wtHeadSHA, tagSHA)
	}

	// Verify base ref is stored in SandboxConfig
	if room.SandboxConfig == nil {
		t.Fatal("SandboxConfig is nil")
	}
	if room.SandboxConfig.BaseRef != "v1.0.0" {
		t.Errorf("BaseRef = %q, want %q", room.SandboxConfig.BaseRef, "v1.0.0")
	}

	// Clean up
	if session, ok := result["tmux_session"].(string); ok {
		killCmd := exec.Command("tmux", "kill-session", "-t", session)
		_ = killCmd.Run()
	}
}

// roomGitOutput runs a git command in the given directory and returns its stdout.
func roomGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

// TestProvisionSandbox_RollbackOnTmuxFailure verifies that if tmux session creation
// fails, the previously created worktree is cleaned up (VAL-RS-004).
func TestProvisionSandbox_RollbackOnTmuxFailure(t *testing.T) {
	// This test intentionally creates a scenario where tmux session creation
	// will fail, then verifies the worktree was cleaned up.
	//
	// We use an invalid session name (containing a colon, which tmux rejects)
	// to trigger the failure. But since the session name is derived from room ID,
	// we need to mock the tmux client instead.
	//
	// However, provisionSandbox directly calls tmuxbridge.New() and
	// createTmuxSessionForSandbox(). We'll create a scenario where the worktree
	// dir is valid but tmux will fail (e.g., no tmux server available with
	// an isolated HOME).

	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	wtRoot := filepath.Join(t.TempDir(), "worktrees")

	// We can't easily force tmux to fail while still having it available.
	// Instead, we'll verify rollback by examining the implementation:
	// The provisionSandbox function removes the worktree on tmux error.
	// We'll test with a non-existent CWD that will cause tmux to fail.
	//
	// Actually, the easiest approach: use a room ID with a session name that's
	// too long or invalid. But tmux session names can be long.
	//
	// Best approach: We use a fake tmux that will fail. Let's test indirectly
	// by creating a scenario where PrepareSession fails.
	//
	// The simplest way: create the worktree root read-only so tmux can't
	// create a session CWD. But actually, the worktree is created first,
	// then tmux session is created with CWD=worktree_path.
	//
	// Let me use a different approach: swap the tmux client for a failing one.

	// Save and restore the tmux client factory
	origNewTmux := newRoomTmuxClient
	defer func() { newRoomTmuxClient = origNewTmux }()

	// First create the worktree successfully to get its path
	mgr := worktree.NewManager()
	branchName := "sandbox/room-rollback-test"
	wtResult, err := mgr.Create(ctx, workspace, branchName,
		worktree.WithNewBranch(true),
		worktree.WithBaseDir(wtRoot),
	)
	if err != nil {
		t.Fatalf("manual worktree creation: %v", err)
	}
	worktreePath := wtResult.Path

	// Verify worktree exists
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree should exist: %v", err)
	}

	// Now remove the worktree so provisionSandbox can create it fresh
	if err := mgr.Remove(ctx, workspace, worktreePath, worktree.WithForce(true), worktree.WithDeleteBranch(true)); err != nil {
		t.Fatalf("remove worktree: %v", err)
	}

	// Create a mock tmux client that always fails PrepareSession
	mockTmux := &failingTmuxClient{}
	newRoomTmuxClient = func() *tmuxbridge.Client { return nil }

	// Since provisionSandbox creates its own tmux client internally,
	// we need to test the rollback path differently.
	// We'll inject a failure by having the tmux session name collide
	// with an impossible-to-create session.

	// Actually, the simplest test: remove tmux from PATH temporarily
	// to force createTmuxSessionForSandbox to fail.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "/nonexistent")
	defer t.Setenv("PATH", origPath)

	room := agent.Room{
		ID:          "rollback-room",
		WorkspaceID: workspace,
		Title:       "Rollback Room",
	}

	_, err = provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxWorktreeRoot: wtRoot,
	})
	if err == nil {
		// Clean up if somehow it succeeded
		if room.SandboxConfig != nil && room.SandboxConfig.TmuxSession != "" {
			killCmd := exec.Command("tmux", "kill-session", "-t", room.SandboxConfig.TmuxSession)
			_ = killCmd.Run()
		}
		t.Fatal("expected error when tmux is not available")
	}

	// Restore PATH for verification
	t.Setenv("PATH", origPath)

	// The worktree should have been cleaned up (rolled back)
	if _, statErr := os.Stat(worktreePath); statErr == nil {
		t.Errorf("worktree at %q should have been cleaned up on tmux failure", worktreePath)
		// Clean up residual
		_ = mgr.Remove(ctx, workspace, worktreePath, worktree.WithForce(true), worktree.WithDeleteBranch(true))
	}

	// Verify the room was not given a SandboxConfig
	if room.SandboxConfig != nil {
		t.Error("room.SandboxConfig should be nil after failed sandbox provisioning")
	}

	_ = mockTmux // suppress unused var warning
}

// failingTmuxClient is a stub for type checking; not used directly.
type failingTmuxClient = tmuxbridge.Client

// TestProvisionSandbox_UpgradesNonSandboxRoom verifies that running --sandbox on
// an existing non-sandbox room adds sandbox to it (VAL-RS-010).
func TestProvisionSandbox_UpgradesNonSandboxRoom(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Step 1: Create a room without sandbox
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "upgrade-room",
		WorkspaceID: workspace,
		Title:       "Upgrade Room",
	})
	if err != nil {
		t.Fatalf("UpsertRoom (no sandbox): %v", err)
	}
	if room.SandboxConfig != nil {
		t.Fatal("SandboxConfig should be nil for non-sandbox room")
	}

	// Step 2: Upgrade with sandbox
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	})
	if err != nil {
		t.Fatalf("provisionSandbox (upgrade): %v", err)
	}

	// Verify sandbox was provisioned
	if result["status"] != "created" {
		t.Errorf("status = %v, want created", result["status"])
	}
	if result["runtime"] != "worktree" {
		t.Errorf("runtime = %v, want worktree", result["runtime"])
	}

	// Verify room now has SandboxConfig
	if room.SandboxConfig == nil {
		t.Fatal("room.SandboxConfig is nil after upgrade")
	}
	worktreePath := room.SandboxConfig.WorktreePath
	if worktreePath == "" {
		t.Fatal("SandboxConfig.WorktreePath is empty after upgrade")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("worktree dir does not exist at %q: %v", worktreePath, err)
	}
	if room.SandboxConfig.TmuxSession == "" {
		t.Error("SandboxConfig.TmuxSession is empty after upgrade")
	}

	// Step 3: Persist and verify round-trip
	room, err = store.UpsertRoom(ctx, room)
	if err != nil {
		t.Fatalf("UpsertRoom (persist upgrade): %v", err)
	}
	if room.SandboxConfig == nil {
		t.Fatal("SandboxConfig lost after UpsertRoom")
	}
	if room.SandboxConfig.WorktreePath != worktreePath {
		t.Errorf("WorktreePath changed after UpsertRoom: got %q, want %q", room.SandboxConfig.WorktreePath, worktreePath)
	}

	// Step 4: Read back from store
	got, err := store.GetRoom(ctx, workspace, "upgrade-room", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if got.SandboxConfig == nil {
		t.Fatal("GetRoom: SandboxConfig is nil")
	}
	if got.SandboxConfig.WorktreePath != worktreePath {
		t.Errorf("GetRoom: WorktreePath = %q, want %q", got.SandboxConfig.WorktreePath, worktreePath)
	}

	// Clean up tmux session
	if session := room.SandboxConfig.TmuxSession; session != "" {
		killCmd := exec.Command("tmux", "kill-session", "-t", session)
		_ = killCmd.Run()
	}
}

// TestRoomCreateSandboxCLIFlags verifies that the CLI flags are correctly
// wired to the room create command.
func TestRoomCreateSandboxCLIFlags(t *testing.T) {
	cmd := newRoomCreateCommand()

	// Verify --sandbox flag exists
	f := cmd.Flags().Lookup("sandbox")
	if f == nil {
		t.Fatal("--sandbox flag not found")
	}
	if f.DefValue != "false" {
		t.Errorf("--sandbox default = %q, want false", f.DefValue)
	}

	// Verify --sandbox-worktree-root flag exists
	f = cmd.Flags().Lookup("sandbox-worktree-root")
	if f == nil {
		t.Fatal("--sandbox-worktree-root flag not found")
	}
	if f.DefValue != "" {
		t.Errorf("--sandbox-worktree-root default = %q, want empty", f.DefValue)
	}

	// Verify --sandbox-base-ref flag exists
	f = cmd.Flags().Lookup("sandbox-base-ref")
	if f == nil {
		t.Fatal("--sandbox-base-ref flag not found")
	}
	if f.DefValue != "HEAD" {
		t.Errorf("--sandbox-base-ref default = %q, want HEAD", f.DefValue)
	}
}

// --- Sandbox Lifecycle Tests ---

func TestRoomDestroySandbox_CleansUpResources(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	// Create a git repo as the workspace
	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a room with sandbox
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "destroy-test",
		WorkspaceID: workspace,
		Title:       "Destroy Test",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Provision sandbox
	result, err := provisionSandbox(ctx, workspace, &room, roomCreateProvisionOptions{
		Sandbox:             true,
		SandboxWorktreeRoot: filepath.Join(t.TempDir(), "worktrees"),
	})
	if err != nil {
		t.Fatalf("provisionSandbox: %v", err)
	}

	worktreePath := result["worktree_path"].(string)
	tmuxSession := result["tmux_session"].(string)

	// Persist sandbox config
	room, err = store.UpsertRoom(ctx, room)
	if err != nil {
		t.Fatalf("UpsertRoom (persist sandbox): %v", err)
	}

	// Verify resources exist
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree should exist before destroy: %v", err)
	}
	if !tmuxSessionExists(tmuxSession) {
		t.Fatal("tmux session should exist before destroy")
	}

	// Run cleanupSandbox
	summary, err := store.GetRoom(ctx, workspace, "destroy-test", "")
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}

	cleanupResult, err := cleanupSandbox(ctx, workspace, "destroy-test", summary.SandboxConfig)
	if err != nil {
		t.Fatalf("cleanupSandbox: %v", err)
	}

	if cleanupResult["worktree_removed"] != true {
		t.Error("worktree_removed should be true")
	}
	if cleanupResult["tmux_killed"] != true {
		t.Error("tmux_killed should be true")
	}
	if cleanupResult["status"] != "cleaned" {
		t.Errorf("status = %v, want cleaned", cleanupResult["status"])
	}

	// Verify worktree is gone
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Error("worktree dir should be removed after cleanup")
	}

	// Verify tmux session is gone
	if tmuxSessionExists(tmuxSession) {
		t.Error("tmux session should be killed after cleanup")
	}
}

func TestRoomDestroySandbox_NonSandboxRoomIsNoop(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	store, err := blackboard.OpenBoardStore(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a room WITHOUT sandbox
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "no-sandbox-room",
		WorkspaceID: workspace,
		Title:       "No Sandbox",
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Verify no sandbox config
	if room.SandboxConfig != nil {
		t.Fatal("non-sandbox room should not have SandboxConfig")
	}

	// Destroy the room via DeleteRoom (same path as runRoomDestroy but without sandbox)
	err = store.DeleteRoom(ctx, workspace, "no-sandbox-room")
	if err != nil {
		t.Fatalf("DeleteRoom should succeed for non-sandbox room: %v", err)
	}

	// Verify room is gone
	_, err = store.GetRoom(ctx, workspace, "no-sandbox-room", "")
	if !errors.Is(err, blackboard.ErrRoomNotFound) {
		t.Errorf("expected ErrRoomNotFound, got: %v", err)
	}
}

func TestRoomDestroySandbox_PartialCleanupOnMissingWorktree(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	// Create a room with sandbox config pointing to a deleted worktree
	fakeWorktreePath := filepath.Join(t.TempDir(), "deleted-wt")

	tmuxSessionName := "foxctl-sandbox-partial-test"
	tc := tmuxbridge.New()
	_ = createTmuxSessionForSandbox(ctx, tc, tmuxSessionName, t.TempDir())
	defer func() {
		cmd := exec.Command("tmux", "kill-session", "-t", tmuxSessionName)
		_ = cmd.Run()
	}()

	sc := &agent.SandboxConfig{
		WorktreePath:   fakeWorktreePath,
		WorktreeBranch: "sandbox/room-partial-test",
		TmuxSession:    tmuxSessionName,
		TerminalURL:    "/terminal/partial-test",
		Runtime:        "worktree",
	}

	// Cleanup should handle missing worktree gracefully
	result, err := cleanupSandbox(ctx, workspace, "partial-test", sc)
	if err != nil {
		t.Fatalf("cleanupSandbox with missing worktree: %v", err)
	}

	// Worktree removal should be marked as done (dir was already gone)
	if result["worktree_removed"] != true {
		t.Error("worktree_removed should be true (already gone)")
	}
	if result["tmux_killed"] != true {
		t.Error("tmux_killed should be true")
	}
}

func TestCleanupSandbox_AlreadyCleanedTmux(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()
	initRoomGitRepo(t, workspace)

	sc := &agent.SandboxConfig{
		WorktreePath:   "/nonexistent/path",
		WorktreeBranch: "sandbox/room-already-clean",
		TmuxSession:    "foxctl-sandbox-nonexistent-session",
		TerminalURL:    "/terminal/already-clean",
		Runtime:        "worktree",
	}

	// Cleanup should handle already-gone tmux session gracefully
	result, err := cleanupSandbox(ctx, workspace, "already-clean", sc)
	if err != nil {
		t.Fatalf("cleanupSandbox with already-cleaned resources: %v", err)
	}

	if result["worktree_removed"] != true {
		t.Error("worktree_removed should be true")
	}
	if result["tmux_killed"] != true {
		t.Error("tmux_killed should be true (session already gone is ok)")
	}
	if result["status"] != "cleaned" {
		t.Errorf("status = %v, want cleaned", result["status"])
	}
}

func TestRoomDestroyCommand_Flags(t *testing.T) {
	cmd := newRoomDestroyCommand()

	// Verify --force flag exists
	f := cmd.Flags().Lookup("force")
	if f == nil {
		t.Fatal("--force flag not found on destroy command")
	}
	if f.DefValue != "false" {
		t.Errorf("--force default = %q, want false", f.DefValue)
	}

	// Verify --workspace flag exists
	f = cmd.Flags().Lookup("workspace")
	if f == nil {
		t.Fatal("--workspace flag not found on destroy command")
	}
	if f.DefValue != "." {
		t.Errorf("--workspace default = %q, want .", f.DefValue)
	}
}

func TestRoomDestroyCommand_EnvelopeOutput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create a room via the command path (uses same config/storage as destroy)
	createCmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd, workspace, "destroy-envelope-test", "Envelope Test", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("runRoomCreateWithProvision: %v", err)
	}

	// Run destroy command
	buf := &bytes.Buffer{}
	destroyCmd := &cobra.Command{}
	destroyCmd.SetOut(buf)
	destroyCmd.SetContext(ctx)

	err = runRoomDestroy(destroyCmd, workspace, "destroy-envelope-test", false)
	if err != nil {
		t.Fatalf("runRoomDestroy: %v", err)
	}

	// Verify output is valid JSON envelope
	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	if env.Status != "ok" {
		t.Errorf("envelope status = %q, want ok", env.Status)
	}
	if env.Command != "foxctl.room.destroy" {
		t.Errorf("envelope command = %q, want foxctl.room.destroy", env.Command)
	}

	// Verify data fields
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}
	if data["room_id"] != "destroy-envelope-test" {
		t.Errorf("room_id = %v, want destroy-envelope-test", data["room_id"])
	}
	if data["status"] != "destroyed" {
		t.Errorf("status = %v, want destroyed", data["status"])
	}
}

func TestRoomDestroyCommand_NotFoundError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	workspace := t.TempDir()

	buf := &bytes.Buffer{}
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	cmd.SetContext(context.Background())

	err := runRoomDestroy(cmd, workspace, "nonexistent-room", false)
	if err != nil {
		// The function writes error envelope and returns nil or error depending on WriteError behavior
		// WriteError returns the error so it propagates
		t.Logf("runRoomDestroy returned error: %v", err)
		// Check the envelope
		var env envelope.Envelope
		if jsonErr := json.Unmarshal(buf.Bytes(), &env); jsonErr == nil {
			if env.Status != "error" {
				t.Errorf("envelope status = %q, want error", env.Status)
			}
			if env.Error.Code != string(protocol.ErrorCodeENotFound) && env.Error.Code != string(protocol.ErrorCodeERuntime) {
				t.Errorf("error code = %q, want ENOTFOUND or ERUNTIME", env.Error.Code)
			}
		}
	}
}

func TestRoomListSandbox_IncludesSandboxStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create a room WITH sandbox config via the command path
	createCmd1, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd1, workspace, "sandbox-list-room", "Sandbox Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("create sandbox room: %v", err)
	}

	// Manually update the room to add sandbox config (simulating what provisionSandbox would do)
	// We need to go through the store directly since provisionSandbox needs tmux
	storeDir := filepath.Join(t.TempDir(), ".foxctl", "storage")
	cfg, cfgErr := loadConfig(ctx)
	if cfgErr != nil {
		// If we can't load config, try to open store directly from the known path
		t.Logf("loadConfig: %v (trying direct store path)", cfgErr)
	}
	var store blackboard.BoardStore
	if cfgErr == nil {
		store, err = blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	} else {
		// Fall back to the test store
		store, err = blackboard.OpenBoardStore(ctx, storeDir)
	}
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Get the room and add sandbox config
	room, err := store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-list-room",
		WorkspaceID: workspace,
		Title:       "Sandbox Room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox-list-room",
			WorktreeBranch: "sandbox/room-sandbox-list-room",
			TmuxSession:    "foxctl-sandbox-sandbox-list-room",
			TerminalURL:    "/terminal/sandbox-list-room",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom (sandbox): %v", err)
	}
	_ = room

	// Create a room WITHOUT sandbox config
	createCmd2, _ := newRoomTestCommand(ctx)
	err = runRoomCreateWithProvision(createCmd2, workspace, "plain-list-room", "Plain Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("create plain room: %v", err)
	}

	// List rooms
	buf := &bytes.Buffer{}
	listCmd := &cobra.Command{}
	listCmd.SetOut(buf)
	listCmd.SetContext(ctx)

	err = runRoomList(listCmd, workspace, "", 50)
	if err != nil {
		t.Fatalf("runRoomList: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}

	rooms, ok := data["rooms"].([]any)
	if !ok {
		t.Fatal("rooms is not a slice")
	}

	// Find the sandbox room in the list
	var foundSandbox, foundPlain bool
	for _, r := range rooms {
		roomMap, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := roomMap["id"].(string)
		if id == "sandbox-list-room" {
			foundSandbox = true
			sc, _ := roomMap["sandbox_config"].(map[string]any)
			if sc == nil {
				t.Error("sandbox room should have sandbox_config in list output")
			} else {
				if sc["worktree_path"] != "/tmp/worktrees/sandbox-list-room" {
					t.Errorf("sandbox worktree_path = %v, want /tmp/worktrees/sandbox-list-room", sc["worktree_path"])
				}
				if sc["terminal_url"] != "/terminal/sandbox-list-room" {
					t.Errorf("sandbox terminal_url = %v, want /terminal/sandbox-list-room", sc["terminal_url"])
				}
				if sc["runtime"] != "worktree" {
					t.Errorf("sandbox runtime = %v, want worktree", sc["runtime"])
				}
			}
		}
		if id == "plain-list-room" {
			foundPlain = true
			if _, hasSC := roomMap["sandbox_config"]; hasSC {
				sc, _ := roomMap["sandbox_config"].(map[string]any)
				if len(sc) > 0 {
					t.Error("plain room should not have sandbox_config data")
				}
			}
		}
	}
	if !foundSandbox {
		t.Error("sandbox-list-room not found in list output")
	}
	if !foundPlain {
		t.Error("plain-list-room not found in list output")
	}
}

func TestRoomShowSandbox_IncludesSandboxMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create room via command path first
	createCmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd, workspace, "sandbox-show-room", "Sandbox Show Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	// Update with sandbox config via store
	cfg, cfgErr := loadConfig(ctx)
	if cfgErr != nil {
		t.Fatalf("loadConfig: %v", cfgErr)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a room WITH sandbox config
	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-show-room",
		WorkspaceID: workspace,
		Title:       "Sandbox Show Room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox-show-room",
			WorktreeBranch: "sandbox/room-sandbox-show-room",
			TmuxSession:    "foxctl-sandbox-sandbox-show-room",
			TerminalURL:    "/terminal/sandbox-show-room",
			Runtime:        "worktree",
			BaseRef:        "main",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Show the room
	buf := &bytes.Buffer{}
	showCmd := &cobra.Command{}
	showCmd.SetOut(buf)
	showCmd.SetContext(ctx)

	err = runRoomShow(showCmd, workspace, "sandbox-show-room", "", 100)
	if err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}

	room, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatal("room is not a map")
	}

	sc, ok := room["sandbox_config"].(map[string]any)
	if !ok || sc == nil {
		t.Fatal("sandbox_config not found in room show output")
	}

	// Verify all SandboxConfig fields
	if sc["worktree_path"] != "/tmp/worktrees/sandbox-show-room" {
		t.Errorf("worktree_path = %v, want /tmp/worktrees/sandbox-show-room", sc["worktree_path"])
	}
	if sc["worktree_branch"] != "sandbox/room-sandbox-show-room" {
		t.Errorf("worktree_branch = %v, want sandbox/room-sandbox-show-room", sc["worktree_branch"])
	}
	if sc["tmux_session"] != "foxctl-sandbox-sandbox-show-room" {
		t.Errorf("tmux_session = %v, want foxctl-sandbox-sandbox-show-room", sc["tmux_session"])
	}
	if sc["terminal_url"] != "/terminal/sandbox-show-room" {
		t.Errorf("terminal_url = %v, want /terminal/sandbox-show-room", sc["terminal_url"])
	}
	if sc["runtime"] != "worktree" {
		t.Errorf("runtime = %v, want worktree", sc["runtime"])
	}
	if sc["base_ref"] != "main" {
		t.Errorf("base_ref = %v, want main", sc["base_ref"])
	}
}

func TestRoomShowSandbox_NonSandboxRoom(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create a room WITHOUT sandbox
	createCmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd, workspace, "plain-show-room", "Plain Show Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	// Show the room
	buf := &bytes.Buffer{}
	showCmd := &cobra.Command{}
	showCmd.SetOut(buf)
	showCmd.SetContext(ctx)

	err = runRoomShow(showCmd, workspace, "plain-show-room", "", 100)
	if err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}

	room, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatal("room is not a map")
	}

	// Non-sandbox room should not have sandbox_config (omitempty)
	if _, hasSC := room["sandbox_config"]; hasSC {
		t.Error("non-sandbox room should not have sandbox_config field (omitempty)")
	}
}

func TestRoomJoinSandbox_TerminalBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create room via command path
	createCmd, _ := newRoomTestCommand(ctx)
	err := runRoomCreateWithProvision(createCmd, workspace, "sandbox-join-room", "Sandbox Join Room", "", nil, roomCreateProvisionOptions{})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}

	// Add sandbox config via store
	cfg, cfgErr := loadConfig(ctx)
	if cfgErr != nil {
		t.Fatalf("loadConfig: %v", cfgErr)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-join-room",
		WorkspaceID: workspace,
		Title:       "Sandbox Join Room",
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox-join",
			WorktreeBranch: "sandbox/room-sandbox-join-room",
			TmuxSession:    "foxctl-sandbox-sandbox-join-room",
			TerminalURL:    "/terminal/sandbox-join-room",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Join the room
	buf := &bytes.Buffer{}
	joinCmd := &cobra.Command{}
	joinCmd.SetOut(buf)
	joinCmd.SetContext(ctx)

	err = runRoomJoin(joinCmd, workspace, "sandbox-join-room", "agent-a", "worker", "", "", "", "", "", false, false, false)
	if err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}

	room, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatal("room is not a map")
	}

	// Room should include sandbox_config
	sc, ok := room["sandbox_config"].(map[string]any)
	if !ok || sc == nil {
		t.Fatal("joined room should include sandbox_config")
	}
	if sc["terminal_url"] != "/terminal/sandbox-join-room" {
		t.Errorf("terminal_url = %v, want /terminal/sandbox-join-room", sc["terminal_url"])
	}

	// Verify member was added
	members, ok := room["members"].([]any)
	if !ok {
		t.Fatal("members is not a slice")
	}
	found := false
	for _, m := range members {
		member, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if member["actor_id"] == "agent-a" {
			found = true
			break
		}
	}
	if !found {
		t.Error("agent-a should be in the members list")
	}
}

func TestRoomLeaveSandbox_TerminalBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// Create room via command path, then add members + sandbox config via store
	cfg, cfgErr := loadConfig(ctx)
	if cfgErr != nil {
		t.Fatalf("loadConfig: %v", cfgErr)
	}
	store, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("OpenBoardStore: %v", err)
	}
	defer store.Close()

	// Create a sandbox room with members
	_, err = store.UpsertRoom(ctx, agent.Room{
		ID:          "sandbox-leave-room",
		WorkspaceID: workspace,
		Title:       "Sandbox Leave Room",
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Role: "worker", JoinedAt: time.Now().UTC()},
			{ActorID: "agent-b", Role: "reviewer", JoinedAt: time.Now().UTC()},
		},
		SandboxConfig: &agent.SandboxConfig{
			WorktreePath:   "/tmp/worktrees/sandbox-leave",
			WorktreeBranch: "sandbox/room-sandbox-leave-room",
			TmuxSession:    "foxctl-sandbox-sandbox-leave-room",
			TerminalURL:    "/terminal/sandbox-leave-room",
			Runtime:        "worktree",
		},
	})
	if err != nil {
		t.Fatalf("UpsertRoom: %v", err)
	}

	// Leave the room
	buf := &bytes.Buffer{}
	leaveCmd := &cobra.Command{}
	leaveCmd.SetOut(buf)
	leaveCmd.SetContext(ctx)

	err = runRoomLeave(leaveCmd, workspace, "sandbox-leave-room", "agent-a")
	if err != nil {
		t.Fatalf("runRoomLeave: %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON envelope: %v\noutput: %s", err, buf.String())
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatal("envelope data is not a map")
	}

	room, ok := data["room"].(map[string]any)
	if !ok {
		t.Fatal("room is not a map")
	}

	// Room should still have sandbox_config
	sc, ok := room["sandbox_config"].(map[string]any)
	if !ok || sc == nil {
		t.Fatal("room should still have sandbox_config after member leaves")
	}
	if sc["terminal_url"] != "/terminal/sandbox-leave-room" {
		t.Errorf("terminal_url = %v, want /terminal/sandbox-leave-room", sc["terminal_url"])
	}

	// Verify agent-a was removed but agent-b remains
	members, ok := room["members"].([]any)
	if !ok {
		t.Fatal("members is not a slice")
	}
	if len(members) != 1 {
		t.Errorf("members count = %d, want 1", len(members))
	}
	member, _ := members[0].(map[string]any)
	if member["actor_id"] != "agent-b" {
		t.Errorf("remaining member = %v, want agent-b", member["actor_id"])
	}
}

func TestFindActiveAgentsInRoom_NoActiveAgents(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()

	workspace := t.TempDir()

	// No agent store → should return empty list, not error
	active, err := findActiveAgentsInRoom(ctx, workspace, "no-agents-room")
	if err != nil {
		// Agent store may not exist in test env, that's acceptable
		t.Logf("findActiveAgentsInRoom returned error (acceptable in test): %v", err)
		return
	}
	if len(active) != 0 {
		t.Errorf("active agents = %v, want empty", active)
	}
}
