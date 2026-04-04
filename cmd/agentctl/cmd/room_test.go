package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
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

func TestRoomCommandFlow_CreateJoinSendShow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "coordination room", []string{"agent-a=lead"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomJoin(cmd, workspace, "alpha", "agent-b", "reviewer", true); err != nil {
		t.Fatalf("runRoomJoin: %v", err)
	}

	cmd, out = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "agent-a", "", "hello room", "info", "", 0, false, true); err != nil {
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
	if !ok || len(messages) != 2 {
		t.Fatalf("messages=%T/%v want 2 entries", data["messages"], data["messages"])
	}
}

func TestCollectRoomRelayTargetsSkipsSender(t *testing.T) {
	targets, skipped := collectRoomRelayTargets(agent.RoomSummary{
		Members: []agent.RoomMember{
			{ActorID: "agent-a"},
			{ActorID: "agent-b"},
			{ActorID: "agent-c"},
		},
	}, "agent-b")
	if len(targets) != 2 || targets[0] != "agent-a" || targets[1] != "agent-c" {
		t.Fatalf("targets=%v want [agent-a agent-c]", targets)
	}
	if len(skipped) != 1 || skipped[0] != "agent-b" {
		t.Fatalf("skipped=%v want [agent-b]", skipped)
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
