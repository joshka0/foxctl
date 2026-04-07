//go:build integration

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
)

// TestIntegrationRelayRoomMessageTmuxRealTmux runs against a live tmux server: creates a detached
// session, relays a broadcast message to the non-sender pane, then capture-pane and checks the
// relay line appears (proving text injection + Enter reached the shell).
//
// Run manually:
//
//	AGENTCTL_INTEGRATION_TMUX=1 go test -tags=integration ./cmd/agentctl/cmd/ -run IntegrationRelayRoomMessageTmuxRealTmux -v
//
// Requires `tmux` on PATH and permission to create sessions.
func TestIntegrationRelayRoomMessageTmuxRealTmux(t *testing.T) {
	if os.Getenv("AGENTCTL_INTEGRATION_TMUX") != "1" {
		t.Skip("set AGENTCTL_INTEGRATION_TMUX=1 to run real tmux relay integration")
	}
	if testing.Short() {
		t.Skip("skipping integration in -short mode")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}

	session := fmt.Sprintf("agentctl-relay-int-%d", time.Now().UnixNano())
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
