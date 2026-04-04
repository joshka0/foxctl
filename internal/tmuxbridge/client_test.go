package tmuxbridge

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParsePaneList(t *testing.T) {
	raw := strings.Join([]string{
		"%1" + fieldSep + "work" + fieldSep + "2" + fieldSep + "1" + fieldSep + "chat" + fieldSep + "1234" + fieldSep + "180" + fieldSep + "42" + fieldSep + "codex-a" + fieldSep + "/repo" + fieldSep + "codex" + fieldSep + "1",
		"%2" + fieldSep + "work" + fieldSep + "2" + fieldSep + "2" + fieldSep + "chat" + fieldSep + "1235" + fieldSep + "180" + fieldSep + "42" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
	}, "\n")

	panes, err := parsePaneList(raw)
	if err != nil {
		t.Fatalf("parsePaneList() error = %v", err)
	}
	if len(panes) != 2 {
		t.Fatalf("len(panes) = %d, want 2", len(panes))
	}
	if panes[0].SessionPane != "work:2.1" {
		t.Fatalf("SessionPane = %q, want %q", panes[0].SessionPane, "work:2.1")
	}
	if panes[0].Label != "codex-a" {
		t.Fatalf("Label = %q, want codex-a", panes[0].Label)
	}
	if !panes[0].Active {
		t.Fatal("expected first pane active")
	}
	if panes[1].Active {
		t.Fatal("expected second pane inactive")
	}
}

func TestResolveTargetByLabel(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux list-panes -a -F " + labelFormat: {
				stdout: "%1" + fieldSep + "codex-a\n%2" + fieldSep + "codex-b\n",
			},
			"tmux display-message -t %2 -p #{pane_id}": {stdout: "%2\n"},
		},
	}, map[string]string{})

	got, err := client.ResolveTarget(context.Background(), "codex-b")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if got != "%2" {
		t.Fatalf("ResolveTarget() = %q, want %q", got, "%2")
	}
}

func TestResolveTargetDirectTarget(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions":                       {stdout: "ok\n"},
			"tmux display-message -t %7 -p #{pane_id}": {stdout: "%7\n"},
		},
	}, map[string]string{})

	got, err := client.ResolveTarget(context.Background(), " %7 ")
	if err != nil {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
	if got != "%7" {
		t.Fatalf("ResolveTarget() = %q, want %q", got, "%7")
	}
}

func TestResolveTargetMissingLabel(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux list-panes -a -F " + labelFormat: {
				stdout: "%1" + fieldSep + "codex-a\n",
			},
		},
	}, map[string]string{})

	_, err := client.ResolveTarget(context.Background(), "unknown-pane")
	if err == nil {
		t.Fatal("ResolveTarget() error = nil, want error")
	}
	if !strings.Contains(err.Error(), `no pane found with label "unknown-pane"`) {
		t.Fatalf("ResolveTarget() error = %v", err)
	}
}

func TestReadReturnsCapturedLines(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux display-message -t %3 -p #{pane_id}": {
				stdout: "%3\n",
			},
			"tmux display-message -t %3 -p " + listFormat: {
				stdout: "%3" + fieldSep + "work" + fieldSep + "4" + fieldSep + "0" + fieldSep + "chat" + fieldSep + "444" + fieldSep + "120" + fieldSep + "30" + fieldSep + "codex-c" + fieldSep + "/repo" + fieldSep + "codex" + fieldSep + "1\n",
			},
			"tmux capture-pane -t %3 -p -J -S -20": {
				stdout: "line one\nline two\n",
			},
		},
	}, map[string]string{})

	got, err := client.Read(context.Background(), "%3", 20)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.ResolvedTarget != "%3" {
		t.Fatalf("ResolvedTarget = %q, want %%3", got.ResolvedTarget)
	}
	if got.Pane.Label != "codex-c" {
		t.Fatalf("Pane.Label = %q, want codex-c", got.Pane.Label)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(got.Lines))
	}
}

func TestLatestBridgeMessage(t *testing.T) {
	lines := []string{
		"plain line",
		"[tmux-bridge from=codex-a pane=%3 reply_to=codex-a] review mailbox",
		"[tmux-bridge from=codex-b pane=%4 reply_to=codex-b] use attach mode",
	}

	got, ok := LatestBridgeMessage(lines)
	if !ok {
		t.Fatal("LatestBridgeMessage() ok = false, want true")
	}
	if got.From != "codex-b" || got.ReplyTo != "codex-b" {
		t.Fatalf("LatestBridgeMessage() = %#v", got)
	}
	if got.Content != "use attach mode" {
		t.Fatalf("Content = %q, want %q", got.Content, "use attach mode")
	}
}

func TestAgentLabelPrefix(t *testing.T) {
	if got := agentLabelPrefix("claude"); got != "claude" {
		t.Fatalf("agentLabelPrefix() = %q, want claude", got)
	}
	if got := agentLabelPrefix("/usr/local/bin/codex"); got != "codex" {
		t.Fatalf("agentLabelPrefix() = %q, want codex", got)
	}
}

func TestShellQuoteArgs(t *testing.T) {
	got := shellQuoteArgs([]string{"claude", "--model", "claude-sonnet-4-6", "--append-system-prompt", "review auth path"})
	want := "claude --model claude-sonnet-4-6 --append-system-prompt 'review auth path'"
	if got != want {
		t.Fatalf("shellQuoteArgs() = %q, want %q", got, want)
	}
}

func TestBuildAgentPaneCommandResume(t *testing.T) {
	codexCmd, err := buildAgentPaneCommand("codex", "interactive", []string{"--model", "gpt-5"}, "session-123")
	if err != nil {
		t.Fatalf("buildAgentPaneCommand(codex) error = %v", err)
	}
	if !strings.Contains(codexCmd, "resume --model gpt-5 session-123") {
		t.Fatalf("codex resume command = %q", codexCmd)
	}

	claudeCmd, err := buildAgentPaneCommand("claude", "interactive", []string{"--model", "sonnet"}, "session-abc")
	if err != nil {
		t.Fatalf("buildAgentPaneCommand(claude) error = %v", err)
	}
	if !strings.Contains(claudeCmd, "--resume session-abc --model sonnet") {
		t.Fatalf("claude resume command = %q", claudeCmd)
	}
}

func TestPrepareSessionCreatesAndLabelsPanes(t *testing.T) {
	runner := &sequenceRunner{
		steps: []sequenceStep{
			{key: "tmux new-session -d -s agentctl-collab -c /repo /bin/zsh"},
			{key: "tmux list-panes -t agentctl-collab -F " + listFormat, stdout: "%0" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "0" + fieldSep + "agentctl-collab" + fieldSep + "100" + fieldSep + "80" + fieldSep + "24" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1\n"},
			{key: "tmux split-window -d -t agentctl-collab -c /repo /bin/zsh"},
			{key: "tmux list-panes -t agentctl-collab -F " + listFormat, stdout: strings.Join([]string{
				"%0" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "0" + fieldSep + "agentctl-collab" + fieldSep + "100" + fieldSep + "80" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1",
				"%1" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "1" + fieldSep + "agentctl-collab" + fieldSep + "101" + fieldSep + "80" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
			}, "\n") + "\n"},
			{key: "tmux split-window -d -t agentctl-collab -c /repo /bin/zsh"},
			{key: "tmux list-panes -t agentctl-collab -F " + listFormat, stdout: strings.Join([]string{
				"%0" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "0" + fieldSep + "agentctl-collab" + fieldSep + "100" + fieldSep + "40" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1",
				"%2" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "1" + fieldSep + "agentctl-collab" + fieldSep + "102" + fieldSep + "40" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
				"%1" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "2" + fieldSep + "agentctl-collab" + fieldSep + "101" + fieldSep + "80" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
			}, "\n") + "\n"},
			{key: "tmux select-layout -t agentctl-collab tiled"},
			{key: "tmux list-panes -t agentctl-collab -F " + listFormat, stdout: strings.Join([]string{
				"%0" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "0" + fieldSep + "agentctl-collab" + fieldSep + "100" + fieldSep + "39" + fieldSep + "11" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1",
				"%2" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "1" + fieldSep + "agentctl-collab" + fieldSep + "102" + fieldSep + "40" + fieldSep + "11" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
				"%1" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "2" + fieldSep + "agentctl-collab" + fieldSep + "101" + fieldSep + "80" + fieldSep + "12" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
			}, "\n") + "\n"},
			{key: "tmux set-option -p -t %0 @name codex-a"},
			{key: "tmux set-option -p -t %2 @name codex-b"},
			{key: "tmux set-option -p -t %1 @name codex-c"},
		},
	}
	client := NewWithRunner(runner, map[string]string{})

	got, err := client.PrepareSession(context.Background(), PrepareOptions{
		Session:     "agentctl-collab",
		Panes:       3,
		PaneCommand: "/bin/zsh",
		CWD:         "/repo",
		LabelPrefix: "codex",
	})
	if err != nil {
		t.Fatalf("PrepareSession() error = %v", err)
	}
	if !got.Created {
		t.Fatal("expected Created = true")
	}
	if got.AttachCommand != "tmux attach-session -t agentctl-collab" {
		t.Fatalf("AttachCommand = %q", got.AttachCommand)
	}
	labels := []string{got.Panes[0].Label, got.Panes[1].Label, got.Panes[2].Label}
	wantLabels := []string{"codex-a", "codex-b", "codex-c"}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("labels = %#v, want %#v", labels, wantLabels)
	}
}

func TestNormalizePrepareOptionsDefaults(t *testing.T) {
	got, err := normalizePrepareOptions(PrepareOptions{Panes: 2})
	if err != nil {
		t.Fatalf("normalizePrepareOptions() error = %v", err)
	}
	if got.session != defaultSessionName {
		t.Fatalf("session = %q, want %q", got.session, defaultSessionName)
	}
	if got.labelPrefix != defaultLabelPrefix {
		t.Fatalf("labelPrefix = %q, want %q", got.labelPrefix, defaultLabelPrefix)
	}
	if got.paneCommand == "" {
		t.Fatal("paneCommand should default to a shell")
	}
}

func TestNormalizePrepareOptionsAgentDefaultsLabelPrefix(t *testing.T) {
	got, err := normalizePrepareOptions(PrepareOptions{
		Panes: 1,
		Agent: "droid",
	})
	if err != nil {
		t.Fatalf("normalizePrepareOptions() error = %v", err)
	}
	if got.agent != "droid" {
		t.Fatalf("agent = %q, want %q", got.agent, "droid")
	}
	if got.labelPrefix != "droid" {
		t.Fatalf("labelPrefix = %q, want %q", got.labelPrefix, "droid")
	}
	if !strings.Contains(got.paneCommand, "droid") {
		t.Fatalf("paneCommand = %q, want command containing droid", got.paneCommand)
	}
}

func TestNormalizePrepareOptionsAgentModeAuto(t *testing.T) {
	got, err := normalizePrepareOptions(PrepareOptions{
		Panes:     1,
		Agent:     "claude",
		AgentMode: "auto",
	})
	if err != nil {
		t.Fatalf("normalizePrepareOptions() error = %v", err)
	}
	if got.agentMode != "auto" {
		t.Fatalf("agentMode = %q, want auto", got.agentMode)
	}
	if !strings.Contains(got.paneCommand, "--dangerously-skip-permissions") {
		t.Fatalf("paneCommand = %q, want auto-mode flag", got.paneCommand)
	}
}

func TestBuildAgentPaneCommandAutoMappings(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		mode     string
		wantFrag string
	}{
		{name: "codex", agent: "codex", mode: "auto", wantFrag: "--full-auto"},
		{name: "claude", agent: "claude", mode: "auto", wantFrag: "--dangerously-skip-permissions"},
		{name: "gemini", agent: "gemini", mode: "auto", wantFrag: "--yolo"},
		{name: "cursor-agent", agent: "agent", mode: "auto", wantFrag: "--yolo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildAgentPaneCommand(tt.agent, tt.mode, nil, "")
			if err != nil {
				t.Fatalf("buildAgentPaneCommand() error = %v", err)
			}
			if !strings.Contains(got, tt.wantFrag) {
				t.Fatalf("buildAgentPaneCommand() = %q, want fragment %q", got, tt.wantFrag)
			}
		})
	}
}

func TestBuildAgentPaneCommandAutoRejectsUnknownAgent(t *testing.T) {
	_, err := buildAgentPaneCommand("droid", "auto", nil, "")
	if err == nil {
		t.Fatal("buildAgentPaneCommand() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not mapped") {
		t.Fatalf("buildAgentPaneCommand() error = %v, want not mapped", err)
	}
}

func TestNormalizePrepareOptionsRejectsInvalidCombinations(t *testing.T) {
	tests := []struct {
		name string
		opts PrepareOptions
		want string
	}{
		{
			name: "mutually exclusive command and agent",
			opts: PrepareOptions{Panes: 1, PaneCommand: "/bin/zsh", Agent: "codex"},
			want: "pane_command and agent are mutually exclusive",
		},
		{
			name: "agent session requires agent",
			opts: PrepareOptions{Panes: 1, AgentSessionID: "abc"},
			want: "agent_session_id requires agent",
		},
		{
			name: "agent session requires single pane",
			opts: PrepareOptions{Panes: 2, Agent: "claude", AgentSessionID: "abc"},
			want: "agent_session_id currently requires panes=1",
		},
		{
			name: "unsupported agent mode",
			opts: PrepareOptions{Panes: 1, Agent: "codex", AgentMode: "turbo"},
			want: "unsupported agent mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizePrepareOptions(tt.opts)
			if err == nil {
				t.Fatal("normalizePrepareOptions() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("normalizePrepareOptions() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestDetectSocketFromBridgeEnv(t *testing.T) {
	socket := createTestUnixSocket(t)
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			fmt.Sprintf("tmux -S %s list-sessions", socket): {stdout: "ok\n"},
		},
	}, map[string]string{
		"TMUX_BRIDGE_SOCKET": socket,
	})

	got, err := client.detectSocket(context.Background())
	if err != nil {
		t.Fatalf("detectSocket() error = %v", err)
	}
	if got != socket {
		t.Fatalf("detectSocket() = %q, want %q", got, socket)
	}
}

func TestDetectSocketFromTmuxEnv(t *testing.T) {
	socket := createTestUnixSocket(t)
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			fmt.Sprintf("tmux -S %s list-sessions", socket): {stdout: "ok\n"},
		},
	}, map[string]string{
		"TMUX": socket + ",123,0",
	})

	got, err := client.detectSocket(context.Background())
	if err != nil {
		t.Fatalf("detectSocket() error = %v", err)
	}
	if got != socket {
		t.Fatalf("detectSocket() = %q, want %q", got, socket)
	}
}

func TestDoctorSummarizesReachableSocket(t *testing.T) {
	socket := createTestUnixSocket(t)
	paneList := strings.Join([]string{
		"%7" + fieldSep + "work" + fieldSep + "0" + fieldSep + "0" + fieldSep + "main" + fieldSep + "1234" + fieldSep + "120" + fieldSep + "30" + fieldSep + "praze-a" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1",
		"%8" + fieldSep + "work" + fieldSep + "0" + fieldSep + "1" + fieldSep + "main" + fieldSep + "1235" + fieldSep + "120" + fieldSep + "30" + fieldSep + "" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0",
	}, "\n") + "\n"
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux -V": {stdout: "tmux 3.4\n"},
			fmt.Sprintf("tmux -S %s list-sessions", socket):                        {stdout: "ok\n"},
			fmt.Sprintf("tmux -S %s display-message -t %%7 -p #{pane_id}", socket): {stdout: "%7\n"},
			fmt.Sprintf("tmux -S %s list-panes -a -F %s", socket, listFormat):      {stdout: paneList},
			"tmux list-sessions": {stderr: "failed to connect", err: fmt.Errorf("exit status 1")},
		},
	}, map[string]string{
		"TMUX_BRIDGE_SOCKET": socket,
		"TMUX_PANE":          "%7",
	})

	got, err := client.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if !got.CurrentPaneSeen {
		t.Fatal("CurrentPaneSeen = false, want true")
	}
	if got.TotalPanes != 2 || got.LabeledPanes != 1 {
		t.Fatalf("pane summary = (%d,%d), want (2,1)", got.TotalPanes, got.LabeledPanes)
	}
	if !got.Healthy {
		t.Fatalf("Healthy = false, issues = %#v", got.Issues)
	}
}

func TestSendWithExplicitSenderLabel(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions": {stdout: "ok\n"},
			"tmux list-panes -a -F " + labelFormat: {
				stdout: "%1" + fieldSep + "praze-a\n%2" + fieldSep + "agent-b\n",
			},
			"tmux display-message -t %2 -p #{pane_id}": {stdout: "%2\n"},
			"tmux display-message -t %2 -p " + listFormat: {
				stdout: "%2" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "1" + fieldSep + "zsh" + fieldSep + "222" + fieldSep + "80" + fieldSep + "24" + fieldSep + "agent-b" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0\n",
			},
			"tmux display-message -t %1 -p #{pane_id}": {stdout: "%1\n"},
			"tmux display-message -t %1 -p " + listFormat: {
				stdout: "%1" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "0" + fieldSep + "zsh" + fieldSep + "111" + fieldSep + "80" + fieldSep + "24" + fieldSep + "praze-a" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "1\n",
			},
			"tmux send-keys -t %2 -l -- [tmux-bridge from=praze-a pane=%1 reply_to=praze-a] review mailbox": {},
			"tmux send-keys -t %2 Enter": {},
		},
	}, map[string]string{})

	got, err := client.Send(context.Background(), "praze-a", "agent-b", "review mailbox")
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.ResolvedTarget != "%2" {
		t.Fatalf("ResolvedTarget = %q, want %q", got.ResolvedTarget, "%2")
	}
	if got.Sender.ID != "%1" {
		t.Fatalf("Sender.ID = %q, want %q", got.Sender.ID, "%1")
	}
	if got.BridgeMessage.From != "praze-a" {
		t.Fatalf("BridgeMessage.From = %q, want %q", got.BridgeMessage.From, "praze-a")
	}
	if got.BridgeMessage.Pane != "%1" {
		t.Fatalf("BridgeMessage.Pane = %q, want %q", got.BridgeMessage.Pane, "%1")
	}
}

func TestSendRequiresSenderOutsideTmux(t *testing.T) {
	client := NewWithRunner(fakeRunner{
		responses: map[string]fakeResponse{
			"tmux list-sessions":                       {stdout: "ok\n"},
			"tmux display-message -t %2 -p #{pane_id}": {stdout: "%2\n"},
			"tmux display-message -t %2 -p " + listFormat: {
				stdout: "%2" + fieldSep + "agentctl-collab" + fieldSep + "0" + fieldSep + "1" + fieldSep + "zsh" + fieldSep + "222" + fieldSep + "80" + fieldSep + "24" + fieldSep + "agent-b" + fieldSep + "/repo" + fieldSep + "zsh" + fieldSep + "0\n",
			},
		},
	}, map[string]string{})

	_, err := client.Send(context.Background(), "", "%2", "review mailbox")
	if err == nil {
		t.Fatal("Send() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "sender is required when not running inside tmux") {
		t.Fatalf("Send() error = %v", err)
	}
}

type fakeRunner struct {
	responses map[string]fakeResponse
}

type fakeResponse struct {
	stdout string
	stderr string
	err    error
}

func (f fakeRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	resp, ok := f.responses[key]
	if !ok {
		return "", "", fmt.Errorf("unexpected command: %s", key)
	}
	return resp.stdout, resp.stderr, resp.err
}

type sequenceRunner struct {
	steps []sequenceStep
	pos   int
}

type sequenceStep struct {
	key    string
	stdout string
	stderr string
	err    error
}

func (s *sequenceRunner) Run(_ context.Context, name string, args ...string) (string, string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	if s.pos >= len(s.steps) {
		return "", "", fmt.Errorf("unexpected command after sequence end: %s", key)
	}
	step := s.steps[s.pos]
	s.pos++
	if step.key != key {
		return "", "", fmt.Errorf("unexpected command at step %d: got %s want %s", s.pos, key, step.key)
	}
	return step.stdout, step.stderr, step.err
}

func createTestUnixSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tmuxbridge-")
	if err != nil {
		t.Fatalf("os.MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("net.Listen(unix) error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
	})
	return socket
}
