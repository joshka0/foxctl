package agentpane

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	terminalruntime "github.com/jkatigb/agentctl/internal/runtime/terminal"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/zellijbridge"
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

func TestRoomTerminalUser(t *testing.T) {
	if got := terminalruntime.RoomTerminalUser("alpha-beta"); got != "room-alpha-beta" {
		t.Fatalf("RoomTerminalUser() = %q, want room-alpha-beta", got)
	}
}

func TestParseRoomTerminalUser(t *testing.T) {
	tests := []struct {
		name string
		user string
		want string
	}{
		{name: "standard room", user: "room-my-room", want: "my-room"},
		{name: "room with underscore", user: "room-my_room", want: "my_room"},
		{name: "wrong prefix", user: "space-my-room", want: ""},
		{name: "empty suffix", user: "room-", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalruntime.ParseRoomTerminalUser(tt.user); got != tt.want {
				t.Fatalf("ParseRoomTerminalUser(%q) = %q, want %q", tt.user, got, tt.want)
			}
		})
	}
}

func TestDefaultRoomTmuxSession(t *testing.T) {
	if got := terminalruntime.DefaultRoomTmuxSession("room-1"); got != "room-room-1" {
		t.Fatalf("DefaultRoomTmuxSession() = %q, want room-room-1", got)
	}
}

func TestResolveRoomTmuxSession(t *testing.T) {
	if got := terminalruntime.ResolveRoomTmuxSession("room-1", ""); got != "room-room-1" {
		t.Fatalf("ResolveRoomTmuxSession() fallback = %q, want room-room-1", got)
	}
	if got := terminalruntime.ResolveRoomTmuxSession("room-1", "custom-session"); got != "custom-session" {
		t.Fatalf("ResolveRoomTmuxSession() override = %q, want custom-session", got)
	}
}

func TestResolveTerminalRoomConfig(t *testing.T) {
	cfg := ResolveTerminalRoomConfig(" alpha-room ", "", 3)
	if cfg.RoomID != "alpha-room" {
		t.Fatalf("RoomID = %q, want alpha-room", cfg.RoomID)
	}
	if cfg.TmuxSession != "room-alpha-room" {
		t.Fatalf("TmuxSession = %q, want room-alpha-room", cfg.TmuxSession)
	}
	if cfg.MaxConnections != 3 {
		t.Fatalf("MaxConnections = %d, want 3", cfg.MaxConnections)
	}
}

func TestTerminalRoomRegistry(t *testing.T) {
	reg := NewTerminalRoomRegistry()
	cfg := ResolveTerminalRoomConfig("alpha-room", "", 2)

	reg.Register(cfg)
	if !reg.HasRoom("alpha-room") {
		t.Fatal("HasRoom(alpha-room) = false, want true")
	}
	got, ok := reg.RoomConfig("alpha-room")
	if !ok {
		t.Fatal("RoomConfig(alpha-room) missing")
	}
	if got.TmuxSession != cfg.TmuxSession {
		t.Fatalf("TmuxSession = %q, want %q", got.TmuxSession, cfg.TmuxSession)
	}
	if len(reg.RoomIDs()) != 1 {
		t.Fatalf("RoomIDs len = %d, want 1", len(reg.RoomIDs()))
	}
	reg.Unregister("alpha-room")
	if reg.HasRoom("alpha-room") {
		t.Fatal("HasRoom(alpha-room) = true after unregister")
	}
}

func TestEffectiveRoomLimit(t *testing.T) {
	if got := EffectiveRoomLimit(3, 10); got != 3 {
		t.Fatalf("EffectiveRoomLimit(3, 10) = %d, want 3", got)
	}
	if got := EffectiveRoomLimit(0, 10); got != 10 {
		t.Fatalf("EffectiveRoomLimit(0, 10) = %d, want 10", got)
	}
}

func TestRoomLimitReached(t *testing.T) {
	if !RoomLimitReached(2, 2) {
		t.Fatal("RoomLimitReached(2, 2) = false, want true")
	}
	if RoomLimitReached(1, 0) {
		t.Fatal("RoomLimitReached(1, 0) = true, want false")
	}
}

func TestRoomNotFoundError(t *testing.T) {
	err := &RoomNotFoundError{RoomID: "alpha"}
	if err.Error() != "room not found: alpha" {
		t.Fatalf("RoomNotFoundError = %q, want %q", err.Error(), "room not found: alpha")
	}
}

func TestFormatRoomLimitError(t *testing.T) {
	got := FormatRoomLimitError("alpha", "connection limit reached", 2, 2)
	if got != "room alpha: connection limit reached (2/2)" {
		t.Fatalf("FormatRoomLimitError() = %q", got)
	}
}

type stubRoomRegistrar struct {
	registered   []TerminalRoomConfig
	unregistered []string
}

func (s *stubRoomRegistrar) RegisterTerminalRoom(config TerminalRoomConfig) {
	s.registered = append(s.registered, config)
}

func (s *stubRoomRegistrar) UnregisterRoom(roomID string) {
	s.unregistered = append(s.unregistered, roomID)
}

func TestTerminalRoomService(t *testing.T) {
	a := &stubRoomRegistrar{}
	b := &stubRoomRegistrar{}
	svc := NewTerminalRoomService(a, b)
	cfg := ResolveTerminalRoomConfig("alpha", "session-a", 2)

	svc.Register(cfg)
	svc.Unregister("alpha")

	if len(a.registered) != 1 || len(b.registered) != 1 {
		t.Fatal("expected both registrars to receive registration")
	}
	if len(a.unregistered) != 1 || a.unregistered[0] != "alpha" {
		t.Fatal("expected unregister to propagate")
	}
}

func TestNormalizeRegisterRequest(t *testing.T) {
	cfg, err := NormalizeRegisterRequest(TerminalRoomRegisterRequest{
		RoomID:         " alpha ",
		TmuxSession:    " session-a ",
		MaxConnections: 2,
	})
	if err != nil {
		t.Fatalf("NormalizeRegisterRequest() error = %v", err)
	}
	if cfg.RoomID != "alpha" || cfg.TmuxSession != "session-a" || cfg.MaxConnections != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestStartTmuxAttachRequiresSession(t *testing.T) {
	_, err := StartTmuxAttach(context.Background(), TmuxAttachOptions{})
	if err == nil {
		t.Fatal("StartTmuxAttach() error = nil, want error")
	}
}

func TestTmuxAttachProcessMethodsRequireRunningPTY(t *testing.T) {
	p := &TmuxAttachProcess{}

	if err := p.WriteInput([]byte("x")); err == nil {
		t.Fatal("WriteInput() error = nil, want error")
	}
	if err := p.Resize(80, 24); err == nil {
		t.Fatal("Resize() error = nil, want error")
	}
	if _, err := p.CopyOutput(io.Discard); err == nil {
		t.Fatal("CopyOutput() error = nil, want error")
	}
	if _, err := p.CopyInput(strings.NewReader("x")); err == nil {
		t.Fatal("CopyInput() error = nil, want error")
	}
	if err := p.Signal(syscall.SIGTERM); err == nil {
		t.Fatal("Signal() error = nil, want error")
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
