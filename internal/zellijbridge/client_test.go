package zellijbridge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type fakeRunner struct {
	lastName string
	lastArgs []string
	stderr   string
	err      error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.lastName = name
	f.lastArgs = append([]string(nil), args...)
	return "", f.stderr, f.err
}

func TestCreatePaneBuildsNamedZellijRunCommand(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session:           "collab",
		CWD:               "/repo",
		Name:              "researcher-a1b2",
		Command:           "agentctl agent watch agent-123",
		ParticipantID:     "researcher-a1b2",
		ParentParticipant: "lead-a",
		ParentAgentID:     "agent:parent-1",
		RoomAccess:        "none",
	})
	if err != nil {
		t.Fatalf("CreatePane() error = %v", err)
	}
	if got.PaneName != "researcher-a1b2" {
		t.Fatalf("PaneName = %q, want researcher-a1b2", got.PaneName)
	}
	if runner.lastName != "zellij" {
		t.Fatalf("runner name = %q, want zellij", runner.lastName)
	}
	cmd := strings.Join(runner.lastArgs, " ")
	for _, want := range []string{
		"--session collab run",
		"--cwd /repo",
		"--name researcher-a1b2",
		"AGENTCTL_PARTICIPANT_ID=researcher-a1b2",
		"AGENTCTL_ZELLIJ_PARTICIPANT=researcher-a1b2",
		"AGENTCTL_MUX_BACKEND=zellij",
		"AGENTCTL_PARENT_PARTICIPANT_ID=lead-a",
		"AGENTCTL_PARENT_AGENT_ID=agent:parent-1",
		"sh -lc agentctl agent watch agent-123",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command %q missing %q", cmd, want)
		}
	}
	if strings.Contains(cmd, "AGENTCTL_ROOM_ID=") {
		t.Fatalf("command %q unexpectedly contains AGENTCTL_ROOM_ID", cmd)
	}
}

func TestCreatePaneDirectRoomAddsRoomID(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	_, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session:       "collab",
		Name:          "lead-a1b2",
		Command:       "agentctl agent watch agent-123",
		ParticipantID: "lead-a1b2",
		RoomID:        "room-alpha",
		RoomAccess:    "direct",
	})
	if err != nil {
		t.Fatalf("CreatePane() error = %v", err)
	}
	if !strings.Contains(strings.Join(runner.lastArgs, " "), "AGENTCTL_ROOM_ID=room-alpha") {
		t.Fatalf("expected room id in command, got %q", strings.Join(runner.lastArgs, " "))
	}
}

func TestCreatePaneRequiresSessionAndName(t *testing.T) {
	client := NewWithRunner(&fakeRunner{})
	if _, err := client.CreatePane(context.Background(), CreatePaneOptions{Name: "x", Command: "cmd"}); err == nil {
		t.Fatal("expected session error")
	}
	if _, err := client.CreatePane(context.Background(), CreatePaneOptions{Session: "s", Command: "cmd"}); err == nil {
		t.Fatal("expected name error")
	}
}

func TestCreatePaneSurfacesRunnerError(t *testing.T) {
	runner := &fakeRunner{stderr: "permission denied", err: fmt.Errorf("exit status 1")}
	client := NewWithRunner(runner)
	if _, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session: "collab",
		Name:    "x",
		Command: "cmd",
	}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("CreatePane() error = %v, want permission denied", err)
	}
}
