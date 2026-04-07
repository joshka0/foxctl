package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
)

const relayTestFieldSep = "\x1f"

// relayTestTmuxListFormat matches internal/tmuxbridge listFormat (describePane metadata line).
var relayTestTmuxListFormat = "#{pane_id}" + relayTestFieldSep + "#{session_name}" + relayTestFieldSep + "#{window_index}" + relayTestFieldSep + "#{pane_index}" + relayTestFieldSep + "#{window_name}" + relayTestFieldSep + "#{pane_pid}" + relayTestFieldSep + "#{pane_width}" + relayTestFieldSep + "#{pane_height}" + relayTestFieldSep + "#{@name}" + relayTestFieldSep + "#{pane_current_path}" + relayTestFieldSep + "#{pane_current_command}" + relayTestFieldSep + "#{pane_active}"

// relayTmuxRecordingRunner implements tmuxbridge.Runner for tests: repeats list-sessions for
// detectSocket, exact matches for pane probes and send-keys (asserting text + Enter delivery).
type relayTmuxRecordingRunner struct {
	calls     []string
	responses map[string]struct {
		stdout string
		stderr string
		err    error
	}
}

func (f *relayTmuxRecordingRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	key := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, key)
	if strings.Contains(key, "list-sessions") {
		return "ok\n", "", nil
	}
	r, ok := f.responses[key]
	if !ok {
		return "", "", fmt.Errorf("unexpected tmux command: %s", key)
	}
	return r.stdout, r.stderr, r.err
}

// TestRelayRoomMessageTmuxSendsPayloadThenEnter asserts the room→tmux relay path injects the
// formatted relay line with tmux send-keys -l and then sends the same submit chord as DeliverText
// (C-Enter for codex/node-class panes).
func TestRelayRoomMessageTmuxSendsPayloadThenEnter(t *testing.T) {
	t.Parallel()

	room := agent.RoomSummary{
		ID: "alpha",
		Members: []agent.RoomMember{
			{ActorID: "agent-a", Backend: "tmux", PaneID: "%1"},
			{ActorID: "agent-b", Backend: "tmux", PaneID: "%2"},
		},
	}
	msg := agent.BoardMessage{
		Sender:    "agent-a",
		Recipient: agent.BroadcastRecipient,
		Body:      "hello",
	}
	content := formatRoomRelayContent(room, msg)

	paneLine := "%2" + relayTestFieldSep + "collab" + relayTestFieldSep + "0" + relayTestFieldSep + "1" + relayTestFieldSep + "main" + relayTestFieldSep + "222" + relayTestFieldSep + "80" + relayTestFieldSep + "24" + relayTestFieldSep + "codex-b" + relayTestFieldSep + "/repo" + relayTestFieldSep + "codex" + relayTestFieldSep + "0\n"

	payloadKey := "tmux send-keys -t %2 -l -- " + content
	enterKey := "tmux send-keys -t %2 C-Enter"

	r := &relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{
			"tmux display-message -t %2 -p #{pane_id}": {stdout: "%2\n"},
			"tmux display-message -t %2 -p " + relayTestTmuxListFormat: {stdout: paneLine},
			payloadKey: {},
			enterKey:   {},
		},
	}
	client := tmuxbridge.NewWithRunner(r, map[string]string{})

	got := relayRoomMessageTmux(context.Background(), client, room, msg)
	if got.DeliveredCount != 1 {
		t.Fatalf("DeliveredCount=%d want 1; Failed=%v Skipped=%v Error=%q",
			got.DeliveredCount, got.FailedMembers, got.SkippedMembers, got.Error)
	}

	var payloadIdx, submitIdx int = -1, -1
	for i, c := range r.calls {
		if c == payloadKey {
			payloadIdx = i
		}
		if c == enterKey {
			submitIdx = i
		}
	}
	if payloadIdx < 0 || submitIdx < 0 {
		t.Fatalf("missing send-keys payload or submit keys; calls=%v", r.calls)
	}
	if payloadIdx >= submitIdx {
		t.Fatalf("want payload send-keys before submit; payloadIdx=%d submitIdx=%d calls=%v", payloadIdx, submitIdx, r.calls)
	}
}

// TestRelayRoomMessageDirectToHumanADeliversToCoordinatorPane asserts a direct send to "human-a"
// still relays to the coordinator member when membership stores a tmux participant id (common
// when join uses --current).
func TestRelayRoomMessageDirectToHumanADeliversToCoordinatorPane(t *testing.T) {
	t.Parallel()

	room := agent.RoomSummary{
		ID: "triad",
		Members: []agent.RoomMember{
			{ActorID: "tmux:triad-cur0:%13", Role: "coordinator", Backend: "tmux", Session: "triad-cur0", PaneID: "%13"},
			{ActorID: "cursor-c-a", Backend: "tmux", PaneID: "%15"},
		},
	}
	msg := agent.BoardMessage{
		Sender:    "cursor-c-a",
		Recipient: "human-a",
		Body:      "spec review done",
	}
	content := formatRoomRelayContent(room, msg)

	paneLine := "%13" + relayTestFieldSep + "triad-cur0" + relayTestFieldSep + "0" + relayTestFieldSep + "0" + relayTestFieldSep + "main" + relayTestFieldSep + "111" + relayTestFieldSep + "80" + relayTestFieldSep + "24" + relayTestFieldSep + "human-a" + relayTestFieldSep + "/repo" + relayTestFieldSep + "codex" + relayTestFieldSep + "1\n"

	payloadKey := "tmux send-keys -t %13 -l -- " + content
	submitKey := "tmux send-keys -t %13 C-Enter"

	r := &relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{
			"tmux display-message -t %13 -p #{pane_id}": {stdout: "%13\n"},
			"tmux display-message -t %13 -p " + relayTestTmuxListFormat: {stdout: paneLine},
			payloadKey: {},
			submitKey:  {},
		},
	}
	client := tmuxbridge.NewWithRunner(r, map[string]string{})

	got := relayRoomMessageTmux(context.Background(), client, room, msg)
	if got.DeliveredCount != 1 {
		t.Fatalf("DeliveredCount=%d want 1; Failed=%v Skipped=%v Error=%q",
			got.DeliveredCount, got.FailedMembers, got.SkippedMembers, got.Error)
	}
	if len(got.DeliveredTo) != 1 || got.DeliveredTo[0] != "%13" {
		t.Fatalf("DeliveredTo=%v want [%%13]", got.DeliveredTo)
	}
}

// TestRelayRoomMessageDirectToHumanAPrefersCoordinatorWhenLegacyHumanARowExists asserts we do not
// fan out to both a stale "human-a" member row and the coordinator row (which yields failed_count 1).
func TestRelayRoomMessageDirectToHumanAPrefersCoordinatorWhenLegacyHumanARowExists(t *testing.T) {
	t.Parallel()

	room := agent.RoomSummary{
		ID: "triad",
		Members: []agent.RoomMember{
			{ActorID: "human-a", Backend: "tmux", PaneID: "%99"},
			{ActorID: "tmux:triad-cur0:%13", Role: "coordinator", Backend: "tmux", Session: "triad-cur0", PaneID: "%13"},
			{ActorID: "cursor-c-a", Backend: "tmux", PaneID: "%15"},
		},
	}
	msg := agent.BoardMessage{
		Sender:    "cursor-c-a",
		Recipient: "human-a",
		Body:      "reply to coordinator only",
	}
	content := formatRoomRelayContent(room, msg)

	paneLine := "%13" + relayTestFieldSep + "triad-cur0" + relayTestFieldSep + "0" + relayTestFieldSep + "0" + relayTestFieldSep + "main" + relayTestFieldSep + "111" + relayTestFieldSep + "80" + relayTestFieldSep + "24" + relayTestFieldSep + "coord" + relayTestFieldSep + "/repo" + relayTestFieldSep + "codex" + relayTestFieldSep + "1\n"

	payloadKey := "tmux send-keys -t %13 -l -- " + content
	submitKey := "tmux send-keys -t %13 C-Enter"

	r := &relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{
			"tmux display-message -t %13 -p #{pane_id}": {stdout: "%13\n"},
			"tmux display-message -t %13 -p " + relayTestTmuxListFormat: {stdout: paneLine},
			payloadKey: {},
			submitKey:  {},
		},
	}
	client := tmuxbridge.NewWithRunner(r, map[string]string{})

	got := relayRoomMessageTmux(context.Background(), client, room, msg)
	if got.DeliveredCount != 1 || got.FailedCount != 0 {
		t.Fatalf("DeliveredCount=%d FailedCount=%d want 1 and 0; FailedMembers=%v DeliveryFailures=%v",
			got.DeliveredCount, got.FailedCount, got.FailedMembers, got.DeliveryFailures)
	}
	if len(got.DeliveredTo) != 1 || got.DeliveredTo[0] != "%13" {
		t.Fatalf("DeliveredTo=%v want [%%13]", got.DeliveredTo)
	}
}
