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
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
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

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "coordination room", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "agent-b", "reviewer", "", "", "", false, true, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "", "hello room", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
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
	if msg["body"] != "hello room" {
		t.Fatalf("body=%v want hello room", msg["body"])
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
	if err := runRoomTaskList(cmd, workspace, "alpha", ""); err != nil {
		t.Fatalf("runRoomTaskList: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	tasksRaw, ok := data["tasks"].([]any)
	if !ok || len(tasksRaw) != 1 {
		t.Fatalf("tasks=%T/%v want 1 entry", data["tasks"], data["tasks"])
	}

	cmd, out = newRoomTestCommand(ctx)
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
	if err := runRoomShow(cmd, workspace, "alpha", "", 20); err != nil {
		t.Fatalf("runRoomShow: %v", err)
	}
	data = decodeRoomEnvelope(t, out)
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages=%T/%v want 3 entries", data["messages"], data["messages"])
	}
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
		t.Fatalf("runRoomTaskComplete returned error instead of envelope: %v", err)
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "gemini-a", taskID, "gemini-a", "take this"); err != nil {
		t.Fatalf("runRoomTaskAssign returned error instead of envelope: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "please pick this up"); err != nil {
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
	if !ok || len(messages) < 3 {
		t.Fatalf("messages=%T/%v want at least 3 entries", data["messages"], data["messages"])
	}
	foundDirect := false
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msg["recipient"] == "gemini-a" && msg["task_id"] == taskID {
			foundDirect = true
			break
		}
	}
	if !foundDirect {
		t.Fatalf("expected direct assignment message for gemini-a in messages=%v", messages)
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this"); err != nil {
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this"); err != nil {
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this"); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomTaskClaim(cmd, workspace, "alpha", "claude-a", taskID); err != nil {
		t.Fatalf("runRoomTaskClaim returned error instead of envelope: %v", err)
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this"); err != nil {
		t.Fatalf("runRoomTaskAssign: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute, nil, false); err != nil {
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
	})
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ID != "m2" {
		t.Fatalf("entries[0].ID=%q want m2", entries[0].ID)
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
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "gemini-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute, nil, true); err != nil {
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
	if err := runRoomTaskAssign(cmd, workspace, "alpha", "human-a", taskID, "gemini-a", "take this"); err != nil {
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
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute, []string{"blocked"}, false); err != nil {
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
	err := runRoomResolve(cmd, workspace, "alpha", "gemini-a", "acked", false, nil, []string{msgID})
	if err != nil {
		t.Fatalf("runRoomResolve returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("expected error envelope, got %s", out.String())
	}
	if !strings.Contains(out.String(), "room resolve requires coordinator role") {
		t.Fatalf("expected coordinator error, got %s", out.String())
	}
}

func TestRunRoomResolveMarksMessageResolved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "gemini-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
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
	if err := runRoomStatus(cmd, workspace, "alpha", 20, 5*time.Minute, nil, false); err != nil {
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
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute, nil, false); err != nil {
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

	cmd, out := newRoomTestCommand(ctx)
	err := runRoomClear(cmd, workspace, "alpha", "gemini-a", "read", "coordinator-pulses")
	if err != nil {
		t.Fatalf("runRoomClear returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("expected error envelope, got %s", out.String())
	}
	if !strings.Contains(out.String(), "room clear requires coordinator role") {
		t.Fatalf("expected coordinator error, got %s", out.String())
	}
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

	cmd, out = newRoomTestCommand(ctx)
	err := runRoomPlanEntry(cmd, workspace, "alpha", "gemini-a", sessionID, agent.BoardMessageKindPlanDecision, "Decision: do it", "Coordinator decision", true)
	if err != nil {
		t.Fatalf("runRoomPlanEntry decision returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("expected error envelope, got %s", out.String())
	}
	if !strings.Contains(out.String(), "room plan phase changes require coordinator role") {
		t.Fatalf("expected coordinator plan error, got %s", out.String())
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

	cmd, out = newRoomTestCommand(ctx)
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

	cmd, out = newRoomTestCommand(ctx)
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

	cmd, out = newRoomTestCommand(ctx)
	err := runRoomInterviewVerify(cmd, workspace, "claude-a", "alpha", answerID, "accept", "Looks right.")
	if err != nil {
		t.Fatalf("runRoomInterviewVerify returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"status":"error"`) {
		t.Fatalf("expected error envelope, got %s", out.String())
	}
	if !strings.Contains(out.String(), "only the verifier or coordinator can record an interview verdict") {
		t.Fatalf("expected verifier error, got %s", out.String())
	}
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
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute, []string{"interview"}, false); err != nil {
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
	}, map[string]time.Time{})
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
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false); err != nil {
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
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "ack-required", false, true, false); err != nil {
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
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false); err != nil {
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
		t.Fatalf("runRoomSend response: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomInbox(cmd, workspace, "alpha", "agent-a", 20, "all", false, false, false); err != nil {
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{})
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
			ID:          "msg-2",
			WorkspaceID: "/repo",
			Stream:      "room:alpha",
			Sender:      "gemini-a",
			Recipient:   "*",
			Body:        "I replied",
			CreatedAt:   now.Add(-2 * time.Minute),
		},
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{})
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{})
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
	}, now, roomPulseConfig{
		Enabled:         true,
		ReplyStaleAfter: 2 * time.Hour,
		MinPulseFloor:   24 * time.Hour,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-3 * time.Hour), Count: 1},
	})
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
	}, map[string]time.Time{})
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
	}, map[string]time.Time{})
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if !pulses[0].Message.Interrupt {
		t.Fatalf("interrupt=%v want true", pulses[0].Message.Interrupt)
	}
	if pulses[0].Message.Recipient != "human-a" {
		t.Fatalf("recipient=%q want human-a", pulses[0].Message.Recipient)
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
	}, map[string]roomPulseState{})
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]roomPulseState{})
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
	}, now, roomPulseConfig{
		ReplyStaleAfter: 2 * time.Hour,
		MinPulseFloor:   2 * time.Hour,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-3 * time.Hour), Count: 2},
	})
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
	pulses := detectRoomPulseEscalationMessages(room, messages, now, roomPulseConfig{
		ReplyStaleAfter:              2 * time.Hour,
		CoordinatorEscalationEnabled: true,
	}, map[string]roomPulseState{
		"msg-1": {LastSentAt: now.Add(-10 * time.Hour), Count: roomPulseInterruptLimit},
	})
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
	})
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ID != "msg-1" {
		t.Fatalf("entry id=%q want msg-1", entries[0].ID)
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
	})
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
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]roomPulseState{})
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
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]roomPulseState{})
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
	})
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
	want := "[room alpha from=human-a to=claude-a ack reply interrupt] Review needed\nPlease review the spawn flow."
	if got != want {
		t.Fatalf("formatRoomRelayContent() = %q, want %q", got, want)
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

func TestRunRoomSendRejectsReplyExpectedBroadcast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "", "", "hello room", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend returned error instead of envelope: %v", err)
	}
	var env envelope.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if env.Status != "error" {
		t.Fatalf("status=%q want error body=%s", env.Status, out.String())
	}
}

func TestRunRoomSendDerivesSenderFromCurrentTmuxPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux.sock,1,0")
	t.Setenv("TMUX_PANE", "%7")
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
}

func TestRunRoomAckMarksMessageAcked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"agent-a=lead", "agent-b=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

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
	if err := runRoomJoin(cmd, workspace, "alpha", "", "worker", "", "", "", false, true, true); err != nil {
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
	if err := runRoomJoin(cmd, workspace, "alpha", "cursor-a", "reviewer", "zellij", "fascinating-salamander", "", false, true, false); err != nil {
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

func TestRunRoomJoinCurrentPersistsZellijPaneBinding(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("ZELLIJ", "1")
	t.Setenv("ZELLIJ_SESSION_NAME", "sparkling-apricot")
	t.Setenv("ZELLIJ_PANE_ID", "7")
	t.Setenv("AGENTCTL_ZELLIJ_PARTICIPANT", "cursor-a")
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "", "reviewer", "", "", "", false, true, true); err != nil {
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
		"#{pane_active}"
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
