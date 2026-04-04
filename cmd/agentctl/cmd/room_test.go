package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
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
	if err := runRoomJoin(cmd, workspace, "alpha", "agent-b", "reviewer", true, false); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "", "hello room", "info", "", 0, false, false, true); err != nil {
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
	if !ok || len(members) != 2 {
		t.Fatalf("members=%T/%v want 2 entries", roomRaw["members"], roomRaw["members"])
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
	if err := runRoomStatus(cmd, workspace, "alpha", 50, 5*time.Minute); err != nil {
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
	if got := backlog["pending_direct_requests"]; got == nil {
		t.Fatalf("pending_direct_requests missing in backlog=%v", backlog)
	}
	participants, ok := data["participants"].([]any)
	if !ok || len(participants) == 0 {
		t.Fatalf("participants=%T/%v want entries", data["participants"], data["participants"])
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
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "agent-a", "", "please ack", "info", "", 0, true, false, true); err != nil {
		t.Fatalf("runRoomSend ack: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "agent-a", "", "please reply", "info", "", 0, false, true, true); err != nil {
		t.Fatalf("runRoomSend reply: %v", err)
	}
	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "", "", "plain broadcast", "info", "", 0, false, false, true); err != nil {
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
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "", "done", "info", "", 0, false, false, true); err != nil {
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]time.Time{})
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Key != "msg-1" {
		t.Fatalf("key=%q want msg-1", pulses[0].Key)
	}
	if pulses[0].Message.Recipient != "gemini-a" {
		t.Fatalf("recipient=%q want gemini-a", pulses[0].Message.Recipient)
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]time.Time{})
	if len(pulses) != 0 {
		t.Fatalf("len(pulses)=%d want 0", len(pulses))
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
	}, now, roomPulseConfig{ReplyStaleAfter: 2 * time.Minute}, map[string]time.Time{})
	if len(pulses) != 1 {
		t.Fatalf("len(pulses)=%d want 1", len(pulses))
	}
	if pulses[0].Key != "msg-2" {
		t.Fatalf("key=%q want msg-2", pulses[0].Key)
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
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]time.Time{})
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
	}, now, roomPulseConfig{TaskStaleAfter: 5 * time.Minute}, map[string]time.Time{})
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

func TestFormatRoomRelayContentIncludesRecipientAndFlags(t *testing.T) {
	room := agent.RoomSummary{ID: "alpha"}
	msg := agent.BoardMessage{
		Sender:        "human-a",
		Recipient:     "claude-a",
		Subject:       "Review needed",
		Body:          "Please review the spawn flow.",
		AckRequired:   true,
		ReplyExpected: true,
	}
	got := formatRoomRelayContent(room, msg)
	want := "[room alpha from=human-a to=claude-a ack reply] Review needed\nPlease review the spawn flow."
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

func TestRunRoomSendRejectsReplyExpectedBroadcast(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "", "", "hello room", "info", "", 0, false, true, true); err != nil {
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
	if err := runRoomSend(cmd, workspace, "alpha", "", "", "", "hello room", "info", "", 0, false, false, true); err != nil {
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
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "agent-b", "", "please ack", "info", "", 0, true, false, true); err != nil {
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
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "codex-a", "", "please ack", "info", "", 0, true, false, true); err != nil {
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
	if err := runRoomJoin(cmd, workspace, "alpha", "", "worker", true, true); err != nil {
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
