package agentpane

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
)

func TestInheritChildBinding(t *testing.T) {
	got := InheritChildBinding(agent.TerminalBinding{
		Backend:       "tmux",
		Session:       "collab",
		ParticipantID: "lead-a",
		RoomID:        "room-alpha",
		RoomAccess:    "direct",
	}, "", "agent:parent-1")

	if got.Backend != "tmux" || got.Session != "collab" {
		t.Fatalf("unexpected inherited backend/session: %+v", got)
	}
	if got.ParentParticipantID != "lead-a" {
		t.Fatalf("ParentParticipantID = %q, want lead-a", got.ParentParticipantID)
	}
	if got.ParentAgentID != "agent:parent-1" {
		t.Fatalf("ParentAgentID = %q, want agent:parent-1", got.ParentAgentID)
	}
	if got.RoomAccess != "none" || got.RoomID != "" {
		t.Fatalf("expected child-private binding, got %+v", got)
	}
}

func TestCreateWatchPaneSkipsWhenBindingEmpty(t *testing.T) {
	got, meta, err := CreateWatchPane(context.Background(), agent.TerminalBinding{}, "/repo", "worker-a", "watch")
	if err != nil {
		t.Fatalf("CreateWatchPane() error = %v", err)
	}
	if got != (agent.TerminalBinding{}) || meta != nil {
		t.Fatalf("unexpected result: binding=%+v meta=%v", got, meta)
	}
}

func TestCreateWatchPaneTmux(t *testing.T) {
	prev := newTmuxClient
	defer func() { newTmuxClient = prev }()
	runner := &tmuxSequenceRunner{steps: []tmuxSequenceStep{
		{key: "tmux new-session -d -P -F #{pane_id} -s collab -c /repo " + tmuxDefaultShell(), stdout: "%71\n"},
		{key: "tmux set-option -p -t %71 @name child-a"},
		{key: "tmux select-layout -t collab tiled"},
		{key: "tmux respawn-pane -k -t %71 -c /repo env AGENTCTL_PARTICIPANT_ID=tmux:collab:%71 AGENTCTL_MUX_BACKEND=tmux AGENTCTL_MUX_SESSION=collab AGENTCTL_MUX_PANE_ID=%71 AGENTCTL_PARENT_PARTICIPANT_ID=parent-a AGENTCTL_PARENT_AGENT_ID=agent:parent-1 agentctl agent watch agent-123"},
		{key: "tmux display-message -t %71 -p " + tmuxListFormat, stdout: "%71" + tmuxFieldSep + "collab" + tmuxFieldSep + "0" + tmuxFieldSep + "0" + tmuxFieldSep + "main" + tmuxFieldSep + "111" + tmuxFieldSep + "80" + tmuxFieldSep + "24" + tmuxFieldSep + "child-a" + tmuxFieldSep + "/repo" + tmuxFieldSep + "agentctl" + tmuxFieldSep + "1" + tmuxFieldSep + "" + tmuxFieldSep + "" + tmuxFieldSep + "" + tmuxFieldSep + "" + "\n"},
		{key: "tmux display-message -t %71 -p " + tmuxListFormat, stdout: "%71" + tmuxFieldSep + "collab" + tmuxFieldSep + "0" + tmuxFieldSep + "0" + tmuxFieldSep + "main" + tmuxFieldSep + "111" + tmuxFieldSep + "80" + tmuxFieldSep + "24" + tmuxFieldSep + "child-a" + tmuxFieldSep + "/repo" + tmuxFieldSep + "agentctl" + tmuxFieldSep + "1" + tmuxFieldSep + "" + tmuxFieldSep + "" + tmuxFieldSep + "" + tmuxFieldSep + "" + "\n"},
	}}
	newTmuxClient = func() *tmuxbridge.Client { return tmuxbridge.NewWithRunner(runner, map[string]string{}) }

	got, meta, err := CreateWatchPane(context.Background(), agent.TerminalBinding{
		Backend:             "tmux",
		Session:             "collab",
		ParentParticipantID: "parent-a",
		ParentAgentID:       "agent:parent-1",
		RoomAccess:          "none",
	}, "/repo", "child-a", "agentctl agent watch agent-123")
	if err != nil {
		t.Fatalf("CreateWatchPane() error = %v", err)
	}
	if got.ParticipantID != "tmux:collab:%71" {
		t.Fatalf("ParticipantID = %q, want tmux:collab:%%71", got.ParticipantID)
	}
	if got.PaneID != "%71" {
		t.Fatalf("PaneID = %q, want %%71", got.PaneID)
	}
	if meta["backend"] != "tmux" {
		t.Fatalf("backend meta = %v, want tmux", meta["backend"])
	}
}

func TestCreateWatchPaneZellij(t *testing.T) {
	prev := newZellijClient
	defer func() { newZellijClient = prev }()
	runner := &fakeZellijRunner{}
	newZellijClient = func() *zellijbridge.Client { return zellijbridge.NewWithRunner(runner) }

	got, meta, err := CreateWatchPane(context.Background(), agent.TerminalBinding{
		Backend:             "zellij",
		Session:             "alpha",
		ParentParticipantID: "lead-a",
		ParentAgentID:       "agent:parent-1",
		RoomAccess:          "none",
	}, "/repo", "reviewer-a1b2", "agentctl agent watch agent-123")
	if err != nil {
		t.Fatalf("CreateWatchPane() error = %v", err)
	}
	if got.ParticipantID != "reviewer-a1b2" {
		t.Fatalf("ParticipantID = %q, want reviewer-a1b2", got.ParticipantID)
	}
	if got.PaneID != "reviewer-a1b2" {
		t.Fatalf("PaneID = %q, want reviewer-a1b2", got.PaneID)
	}
	if meta["backend"] != "zellij" {
		t.Fatalf("backend meta = %v, want zellij", meta["backend"])
	}
}

const (
	tmuxFieldSep   = "\x1f"
	tmuxListFormat = "#{pane_id}" + tmuxFieldSep + "#{session_name}" + tmuxFieldSep + "#{window_index}" + tmuxFieldSep + "#{pane_index}" + tmuxFieldSep + "#{window_name}" + tmuxFieldSep + "#{pane_pid}" + tmuxFieldSep + "#{pane_width}" + tmuxFieldSep + "#{pane_height}" + tmuxFieldSep + "#{@name}" + tmuxFieldSep + "#{pane_current_path}" + tmuxFieldSep + "#{pane_current_command}" + tmuxFieldSep + "#{pane_active}" + tmuxFieldSep + "#{@agentctl_participant}" + tmuxFieldSep + "#{@agentctl_provider}" + tmuxFieldSep + "#{@agentctl_room_id}" + tmuxFieldSep + "#{@agentctl_wrapped}"
)

type tmuxSequenceStep struct {
	key    string
	stdout string
	stderr string
	err    error
}

type tmuxSequenceRunner struct {
	steps []tmuxSequenceStep
	index int
}

func (s *tmuxSequenceRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	if s.index >= len(s.steps) {
		return "", "", fmt.Errorf("unexpected command after sequence end: %s %s", name, strings.Join(args, " "))
	}
	step := s.steps[s.index]
	s.index++
	key := name
	if len(args) > 0 {
		key += " " + strings.Join(args, " ")
	}
	if key != step.key {
		return "", "", fmt.Errorf("command %d = %q, want %q", s.index, key, step.key)
	}
	return step.stdout, step.stderr, step.err
}

func tmuxDefaultShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

type fakeZellijRunner struct {
	lastName string
	lastArgs []string
}

func (f *fakeZellijRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	f.lastName = name
	f.lastArgs = append([]string(nil), args...)
	return "", "", nil
}
