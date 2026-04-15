//go:build integration

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
)

// TestIntegrationRelayRoomMessageTmuxRealTmux runs against a live tmux server: creates a detached
// session, relays a broadcast message to the non-sender pane, then capture-pane and checks the
// relay line appears (proving text injection + Enter reached the shell).
//
// Run manually:
//
//	FOXCTL_INTEGRATION_TMUX=1 go test -tags=integration ./cmd/foxctl/cmd/ -run IntegrationRelayRoomMessageTmuxRealTmux -v
//
// Requires `tmux` on PATH and permission to create sessions.
func TestIntegrationRelayRoomMessageTmuxRealTmux(t *testing.T) {
	if os.Getenv("FOXCTL_INTEGRATION_TMUX") != "1" {
		t.Skip("set FOXCTL_INTEGRATION_TMUX=1 to run real tmux relay integration")
	}
	if testing.Short() {
		t.Skip("skipping integration in -short mode")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}

	session := fmt.Sprintf("foxctl-relay-int-%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newSession := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", session, "sleep", "600")
	if out, err := newSession.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	split := exec.CommandContext(ctx, "tmux", "split-window", "-t", session, "-h", "sleep", "600")
	if out, err := split.CombinedOutput(); err != nil {
		t.Fatalf("tmux split-window: %v\n%s", err, out)
	}

	list := exec.CommandContext(ctx, "tmux", "list-panes", "-t", session, "-F", "#{pane_id}")
	out, err := list.Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) < 2 {
		t.Fatalf("want 2 panes, got %q", ids)
	}
	paneA, paneB := ids[0], ids[1]

	room := agent.RoomSummary{
		ID: "alpha",
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Backend: "tmux", PaneID: paneA},
			{ActorID: "agent-b", Backend: "tmux", PaneID: paneB},
		},
	}
	msg := agent.BoardMessage{
		Sender:    "agent-a",
		Recipient: agent.BroadcastRecipient,
		Body:      "integration-relay-ping",
	}

	client := tmuxbridge.New()
	res := relayRoomMessageTmux(ctx, client, room, msg)
	if res.DeliveredCount != 1 || res.FailedCount != 0 {
		t.Fatalf("relay: %+v", res)
	}

	cap := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", paneB, "-p", "-S", "-20")
	captured, err := cap.Output()
	if err != nil {
		t.Fatalf("capture-pane: %v", err)
	}
	flat := strings.ReplaceAll(string(captured), "\n", "")
	if !strings.Contains(flat, "integration-relay-ping") || !strings.Contains(flat, "[room alpha from=agent-a to=*]") {
		t.Fatalf("capture did not contain relay text; got:\n%s", string(captured))
	}
}

// TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux proves the relay path does more than
// paste text into a pane: it must actually submit the line so the target terminal process consumes
// it. We run a real tmux pane with `cat >> file` and assert the delivered relay line appears in the
// file for both Enter-only and composer-style C-Enter submit paths.
//
// Run manually:
//
//	FOXCTL_INTEGRATION_TMUX=1 go test -tags=integration ./cmd/foxctl/cmd/ -run IntegrationRelayRoomMessageTmuxConsumesInputRealTmux -v
func TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux(t *testing.T) {
	for _, tc := range []struct {
		name      string
		targetID  string
		targetLbl string
	}{
		{name: "enter_submit", targetID: "gemini-a", targetLbl: "gemini-a"},
		{name: "composer_ctrl_enter_submit", targetID: "droid-a", targetLbl: "droid-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if os.Getenv("FOXCTL_INTEGRATION_TMUX") != "1" {
				t.Skip("set FOXCTL_INTEGRATION_TMUX=1 to run real tmux relay integration")
			}
			if testing.Short() {
				t.Skip("skipping integration in -short mode")
			}
			if _, err := exec.LookPath("tmux"); err != nil {
				t.Skip("tmux not on PATH")
			}

			session := fmt.Sprintf("foxctl-relay-submit-%s-%d", strings.ReplaceAll(tc.name, "_", "-"), time.Now().UnixNano())
			tmpDir := t.TempDir()
			receivedPath := filepath.Join(tmpDir, "received.txt")

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			newSession := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", session, "sleep", "600")
			if out, err := newSession.CombinedOutput(); err != nil {
				t.Fatalf("tmux new-session: %v\n%s", err, out)
			}
			t.Cleanup(func() {
				_ = exec.Command("tmux", "kill-session", "-t", session).Run()
			})

			receiverCmd := fmt.Sprintf(`bash -lc 'exec cat >> %q'`, receivedPath)
			split := exec.CommandContext(ctx, "tmux", "split-window", "-t", session, "-h", receiverCmd)
			if out, err := split.CombinedOutput(); err != nil {
				t.Fatalf("tmux split-window: %v\n%s", err, out)
			}

			list := exec.CommandContext(ctx, "tmux", "list-panes", "-t", session, "-F", "#{pane_id}")
			out, err := list.Output()
			if err != nil {
				t.Fatalf("list-panes: %v", err)
			}
			ids := strings.Fields(strings.TrimSpace(string(out)))
			if len(ids) < 2 {
				t.Fatalf("want 2 panes, got %q", ids)
			}
			paneSender, paneReceiver := ids[0], ids[1]

			if out, err := exec.CommandContext(ctx, "tmux", "set-option", "-pt", paneSender, "@name", "agent-a").CombinedOutput(); err != nil {
				t.Fatalf("tmux set-option sender: %v\n%s", err, out)
			}
			if out, err := exec.CommandContext(ctx, "tmux", "set-option", "-pt", paneReceiver, "@name", tc.targetLbl).CombinedOutput(); err != nil {
				t.Fatalf("tmux set-option receiver: %v\n%s", err, out)
			}
			waitForPaneCurrentCommand(t, ctx, paneReceiver, "cat")

			room := agent.RoomSummary{
				ID: "alpha",
				Members: []agent.RoomMember{
					{ActorID: "agent-a", Backend: "tmux", PaneID: paneSender},
					{ActorID: tc.targetID, Backend: "tmux", PaneID: paneReceiver},
				},
			}
			msg := agent.BoardMessage{
				Sender:    "agent-a",
				Recipient: agent.BroadcastRecipient,
				Body:      "integration-submit-proof",
			}
			expected := formatRoomRelayContent(room, msg)

			client := tmuxbridge.New()
			res := relayRoomMessageTmux(ctx, client, room, msg)
			if res.DeliveredCount != 1 || res.FailedCount != 0 {
				t.Fatalf("relay: %+v", res)
			}

			deadline := time.Now().Add(5 * time.Second)
			for {
				data, err := os.ReadFile(receivedPath)
				if err == nil && strings.Contains(string(data), expected) {
					return
				}
				if time.Now().After(deadline) {
					capOut, _ := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", paneReceiver, "-p", "-S", "-20").CombinedOutput()
					if err != nil && !os.IsNotExist(err) {
						t.Fatalf("read received file: %v\npane:\n%s", err, string(capOut))
					}
					t.Fatalf("relay text was not consumed by target process; want file %q to contain %q\npane:\n%s", receivedPath, expected, string(capOut))
				}
				time.Sleep(100 * time.Millisecond)
			}
		})
	}
}

// TestIntegrationRelayRoomMessageTmuxDispatchesQueuedDraftRealTmux simulates a receiver UI that
// accepts the relayed line, shows a queued marker, and only dispatches after one extra Enter. This
// proves the tmux relay does more than reach the process: it can detect the queued-draft state and
// perform one bounded retry so the message is actually dispatched.
//
// Run manually:
//
//	FOXCTL_INTEGRATION_TMUX=1 go test -tags='integration libsqlite3' ./cmd/foxctl/cmd/ -run IntegrationRelayRoomMessageTmuxDispatchesQueuedDraftRealTmux -v
func TestIntegrationRelayRoomMessageTmuxDispatchesQueuedDraftRealTmux(t *testing.T) {
	if os.Getenv("FOXCTL_INTEGRATION_TMUX") != "1" {
		t.Skip("set FOXCTL_INTEGRATION_TMUX=1 to run real tmux relay integration")
	}
	if testing.Short() {
		t.Skip("skipping integration in -short mode")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}

	session := fmt.Sprintf("foxctl-relay-queued-%d", time.Now().UnixNano())
	tmpDir := t.TempDir()
	dispatchedPath := filepath.Join(tmpDir, "dispatched.txt")
	receiverScriptPath := filepath.Join(tmpDir, "queued_receiver.py")
	receiverScript := fmt.Sprintf(`import pathlib
import sys

dispatched = pathlib.Path(%q)
pending = None

while True:
    line = sys.stdin.readline()
    if line == "":
        break
    line = line.rstrip("\n")
    if pending is not None and line == "":
        existing = dispatched.read_text() if dispatched.exists() else ""
        dispatched.write_text(existing + pending + "\n")
        pending = None
        continue
    if "[room alpha from=agent-a to=*]" in line:
        print(f"Queued (press up to edit): {line}", flush=True)
        pending = line
`, dispatchedPath)
	if err := os.WriteFile(receiverScriptPath, []byte(receiverScript), 0o600); err != nil {
		t.Fatalf("write receiver script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	newSession := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", session, "sleep", "600")
	if out, err := newSession.CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", session).Run()
	})

	receiverCmd := fmt.Sprintf("python3 -u %q", receiverScriptPath)
	split := exec.CommandContext(ctx, "tmux", "split-window", "-t", session, "-h", receiverCmd)
	if out, err := split.CombinedOutput(); err != nil {
		t.Fatalf("tmux split-window: %v\n%s", err, out)
	}

	list := exec.CommandContext(ctx, "tmux", "list-panes", "-t", session, "-F", "#{pane_id}")
	out, err := list.Output()
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	ids := strings.Fields(strings.TrimSpace(string(out)))
	if len(ids) < 2 {
		t.Fatalf("want 2 panes, got %q", ids)
	}
	paneSender, paneReceiver := ids[0], ids[1]

	if out, err := exec.CommandContext(ctx, "tmux", "set-option", "-pt", paneSender, "@name", "agent-a").CombinedOutput(); err != nil {
		t.Fatalf("tmux set-option sender: %v\n%s", err, out)
	}
	if out, err := exec.CommandContext(ctx, "tmux", "set-option", "-pt", paneReceiver, "@name", "gemini-a").CombinedOutput(); err != nil {
		t.Fatalf("tmux set-option receiver: %v\n%s", err, out)
	}
	waitForPaneCurrentCommand(t, ctx, paneReceiver, "python3")

	room := agent.RoomSummary{
		ID: "alpha",
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Backend: "tmux", PaneID: paneSender},
			{ActorID: "gemini-a", Backend: "tmux", PaneID: paneReceiver},
		},
	}
	msg := agent.BoardMessage{
		Sender:    "agent-a",
		Recipient: agent.BroadcastRecipient,
		Body:      "integration-queued-dispatch",
	}
	expected := formatRoomRelayContent(room, msg)

	client := tmuxbridge.New()
	res := relayRoomMessageTmux(ctx, client, room, msg)
	if res.DeliveredCount != 1 || res.FailedCount != 0 {
		t.Fatalf("relay: %+v", res)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(dispatchedPath)
		if err == nil && strings.Contains(string(data), expected) {
			return
		}
		if time.Now().After(deadline) {
			capOut, _ := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", paneReceiver, "-p", "-S", "-40").CombinedOutput()
			if err != nil && !os.IsNotExist(err) {
				t.Fatalf("read dispatched file: %v\npane:\n%s", err, string(capOut))
			}
			t.Fatalf("relay text was consumed but not dispatched after queued-draft retry; want file %q to contain %q\npane:\n%s", dispatchedPath, expected, string(capOut))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitForPaneCurrentCommand(t *testing.T, ctx context.Context, paneID string, wantPrefix string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := exec.CommandContext(ctx, "tmux", "display-message", "-t", paneID, "-p", "#{pane_current_command}").CombinedOutput()
		got := strings.TrimSpace(string(out))
		if err == nil && strings.HasPrefix(got, wantPrefix) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane %s did not reach current_command prefix %q; last=%q err=%v", paneID, wantPrefix, got, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
