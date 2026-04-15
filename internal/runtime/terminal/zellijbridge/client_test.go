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
	calls    []string
	stderr   string
	err      error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.lastName = name
	f.lastArgs = append([]string(nil), args...)
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return "", f.stderr, f.err
}

func TestCreatePaneBuildsNamedZellijNewPaneCommand(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session:           "collab",
		CWD:               "/repo",
		Name:              "researcher-a1b2",
		Command:           "foxctl agent watch agent-123",
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
		"--session collab action new-pane",
		"--cwd /repo",
		"--name researcher-a1b2",
		"FOXCTL_PARTICIPANT_ID=researcher-a1b2",
		"FOXCTL_ZELLIJ_PARTICIPANT=researcher-a1b2",
		"FOXCTL_MUX_BACKEND=zellij",
		"FOXCTL_PARENT_PARTICIPANT_ID=lead-a",
		"FOXCTL_PARENT_AGENT_ID=agent:parent-1",
		"sh -lc if [ -n",
		"exec foxctl agent watch agent-123",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command %q missing %q", cmd, want)
		}
	}
	if strings.Contains(cmd, "FOXCTL_ROOM_ID=") {
		t.Fatalf("command %q unexpectedly contains FOXCTL_ROOM_ID", cmd)
	}
}

func TestCreatePaneEnsuresSessionBeforeRun(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	_, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session: "collab",
		Name:    "pane-a",
		Command: "echo ok",
	})
	if err != nil {
		t.Fatalf("CreatePane() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%v want 2 invocations", runner.calls)
	}
	if !strings.Contains(runner.calls[0], "zellij attach --create-background collab") {
		t.Fatalf("first call=%q want attach --create-background", runner.calls[0])
	}
	if !strings.Contains(runner.calls[1], "zellij --session collab action new-pane") {
		t.Fatalf("second call=%q want session action new-pane", runner.calls[1])
	}
}

func TestCreatePaneSkipsEnsureForCurrentSession(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "awesome-orange")
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	_, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session: "awesome-orange",
		Name:    "pane-a",
		Command: "echo ok",
	})
	if err != nil {
		t.Fatalf("CreatePane() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%v want 1 invocation", runner.calls)
	}
	if strings.Contains(runner.calls[0], "attach --create-background") {
		t.Fatalf("call=%q unexpectedly ensured current session", runner.calls[0])
	}
	if !strings.Contains(runner.calls[0], "zellij --session awesome-orange action new-pane") {
		t.Fatalf("call=%q want session action new-pane", runner.calls[0])
	}
}

func TestCreatePaneDirectRoomAddsRoomID(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	_, err := client.CreatePane(context.Background(), CreatePaneOptions{
		Session:       "collab",
		Name:          "lead-a1b2",
		Command:       "foxctl agent watch agent-123",
		ParticipantID: "lead-a1b2",
		RoomID:        "room-alpha",
		RoomAccess:    "direct",
	})
	if err != nil {
		t.Fatalf("CreatePane() error = %v", err)
	}
	if !strings.Contains(strings.Join(runner.lastArgs, " "), "FOXCTL_ROOM_ID=room-alpha") {
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

func TestSubmitUsesEscapeThenEnter(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.Submit(context.Background(), "collab", SubmitOptions{})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if got.Session != "collab" {
		t.Fatalf("Session = %q, want collab", got.Session)
	}
	if got.Mode != SubmitModeEscapeEnter {
		t.Fatalf("Mode = %q, want %s", got.Mode, SubmitModeEscapeEnter)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%v want 2 invocations", runner.calls)
	}
	if runner.calls[0] != "zellij --session collab action write 27" {
		t.Fatalf("first call=%q want escape write", runner.calls[0])
	}
	if runner.calls[1] != "zellij --session collab action write 13" {
		t.Fatalf("second call=%q want enter write", runner.calls[1])
	}
}

func TestSubmitEnterOnly(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.Submit(context.Background(), "collab", SubmitOptions{Mode: SubmitModeEnterOnly})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if got.Mode != SubmitModeEnterOnly {
		t.Fatalf("Mode = %q, want %s", got.Mode, SubmitModeEnterOnly)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%v want 1 invocation", runner.calls)
	}
	if runner.calls[0] != "zellij --session collab action write 13" {
		t.Fatalf("call=%q want enter write only", runner.calls[0])
	}
}

func TestSubmitWithPaneID(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.Submit(context.Background(), "collab", SubmitOptions{PaneID: "terminal_2"})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if got.PaneID != "terminal_2" {
		t.Fatalf("PaneID = %q, want terminal_2", got.PaneID)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls=%v want 2 invocations", runner.calls)
	}
	want0 := "zellij --session collab action write --pane-id terminal_2 27"
	want1 := "zellij --session collab action write --pane-id terminal_2 13"
	if runner.calls[0] != want0 {
		t.Fatalf("first call=%q want %q", runner.calls[0], want0)
	}
	if runner.calls[1] != want1 {
		t.Fatalf("second call=%q want %q", runner.calls[1], want1)
	}
}

func TestInterruptWritesEscape(t *testing.T) {
	runner := &fakeRunner{}
	client := NewWithRunner(runner)

	got, err := client.Interrupt(context.Background(), "collab", "terminal_2")
	if err != nil {
		t.Fatalf("Interrupt() error = %v", err)
	}
	if got.PaneID != "terminal_2" {
		t.Fatalf("PaneID = %q, want terminal_2", got.PaneID)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls=%v want 1 invocation", runner.calls)
	}
	want := "zellij --session collab action write --pane-id terminal_2 27"
	if runner.calls[0] != want {
		t.Fatalf("call=%q want %q", runner.calls[0], want)
	}
}
