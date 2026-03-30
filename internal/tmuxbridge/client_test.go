package tmuxbridge

import (
	"context"
	"fmt"
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
	codexCmd, err := buildAgentPaneCommand("codex", []string{"--model", "gpt-5"}, "session-123")
	if err != nil {
		t.Fatalf("buildAgentPaneCommand(codex) error = %v", err)
	}
	if !strings.Contains(codexCmd, "resume --model gpt-5 session-123") {
		t.Fatalf("codex resume command = %q", codexCmd)
	}

	claudeCmd, err := buildAgentPaneCommand("claude", []string{"--model", "sonnet"}, "session-abc")
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
