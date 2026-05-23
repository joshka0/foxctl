package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
)

const relayTestFieldSep = "\x1f"

// relayTestTmuxListFormat matches internal/runtime/terminal/tmuxbridge listFormat (describePane metadata line).
var relayTestTmuxListFormat = "#{pane_id}" + relayTestFieldSep + "#{session_name}" + relayTestFieldSep + "#{window_index}" + relayTestFieldSep + "#{pane_index}" + relayTestFieldSep + "#{window_name}" + relayTestFieldSep + "#{pane_pid}" + relayTestFieldSep + "#{pane_width}" + relayTestFieldSep + "#{pane_height}" + relayTestFieldSep + "#{@name}" + relayTestFieldSep + "#{pane_current_path}" + relayTestFieldSep + "#{pane_current_command}" + relayTestFieldSep + "#{pane_active}" + relayTestFieldSep + "#{@foxctl_participant}" + relayTestFieldSep + "#{@foxctl_provider}" + relayTestFieldSep + "#{@foxctl_room_id}" + relayTestFieldSep + "#{@foxctl_wrapped}"

func relayTmuxMember(actorID, session, paneID string) agent.RoomMember {
	return agent.RoomMember{
		ActorID: actorID,
		DeliveryBinding: &agent.RoomDeliveryBinding{
			MuxBackend:        "tmux",
			MuxSession:        session,
			MuxPaneID:         paneID,
			TransportEndpoint: "tmux:" + session + ":" + paneID,
			TransportKind:     "mux_pane",
		},
	}
}

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
			relayTmuxMember("agent-a", "collab", "%1"),
			relayTmuxMember("agent-b", "collab", "%2"),
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
			"tmux display-message -t %2 -p #{pane_id}":                 {stdout: "%2\n"},
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

func TestRelayRoomMessageTmuxUsesBindingOnlyPaneTarget(t *testing.T) {
	t.Parallel()

	room := agent.RoomSummary{
		ID: "alpha",
		Members: []agent.RoomMember{
			{ActorID: "agent-a"},
			{
				ActorID: "agent-b",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "tmux",
					MuxSession:        "collab",
					MuxPaneID:         "%2",
					TransportEndpoint: "tmux:collab:%2",
					TransportKind:     "mux_pane",
				},
			},
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
			"tmux display-message -t %2 -p #{pane_id}":                 {stdout: "%2\n"},
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
	if len(got.DeliveredTo) != 1 || got.DeliveredTo[0] != "%2" {
		t.Fatalf("DeliveredTo=%v want [%%2]", got.DeliveredTo)
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
			func() agent.RoomMember {
				m := relayTmuxMember("tmux:triad-cur0:%13", "triad-cur0", "%13")
				m.Role = "coordinator"
				return m
			}(),
			relayTmuxMember("cursor-c-a", "triad-cur0", "%15"),
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
			"tmux display-message -t %13 -p #{pane_id}":                 {stdout: "%13\n"},
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

// TestRelayRoomMessageDirectToHumanAPrefersCoordinatorWhenHumanALabelRowExists asserts we do not
// fan out to both a "human-a" label row and the coordinator row.
func TestRelayRoomMessageDirectToHumanAPrefersCoordinatorWhenHumanALabelRowExists(t *testing.T) {
	t.Parallel()

	room := agent.RoomSummary{
		ID: "triad",
		Members: []agent.RoomMember{
			relayTmuxMember("human-a", "triad-cur0", "%99"),
			func() agent.RoomMember {
				m := relayTmuxMember("tmux:triad-cur0:%13", "triad-cur0", "%13")
				m.Role = "coordinator"
				return m
			}(),
			relayTmuxMember("cursor-c-a", "triad-cur0", "%15"),
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
			"tmux display-message -t %13 -p #{pane_id}":                 {stdout: "%13\n"},
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

func TestCollectRelayParticipantsSkipsSender(t *testing.T) {
	participants, skipped := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			relayTmuxMember("agent-a", "s", "%1"),
			relayTmuxMember("agent-b", "s", "%2"),
			relayTmuxMember("agent-c", "s", "%3"),
		},
	}, agent.BoardMessage{Sender: "agent-b"})
	if len(participants) != 2 {
		t.Fatalf("participants=%d want 2", len(participants))
	}
	if len(skipped) != 1 || skipped[0] != "agent-b" {
		t.Fatalf("skipped=%v want [agent-b]", skipped)
	}
	for _, p := range participants {
		if p.ActorID == "agent-b" {
			t.Fatal("sender should not be in participants")
		}
	}
}

func TestCollectRelayParticipantsSkipsUnbound(t *testing.T) {
	participants, _ := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			relayTmuxMember("bound-a", "s", "%1"),
			{ActorID: "unbound-a", Unbound: true},
		},
	}, agent.BoardMessage{Sender: "sender"})
	if len(participants) != 1 {
		t.Fatalf("participants=%d want 1 (unbound skipped)", len(participants))
	}
	if participants[0].ActorID != "bound-a" {
		t.Fatalf("participants[0].ActorID=%q want bound-a", participants[0].ActorID)
	}
}

func TestCollectRelayParticipantsDirectRecipient(t *testing.T) {
	participants, skipped := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			relayTmuxMember("agent-a", "s", "%1"),
			relayTmuxMember("agent-b", "s", "%2"),
		},
	}, agent.BoardMessage{Sender: "sender", Recipient: "agent-a"})
	if len(participants) != 1 || participants[0].ActorID != "agent-a" {
		t.Fatalf("participants=%v want only agent-a", participants)
	}
	if len(skipped) != 1 || skipped[0] != "agent-b" {
		t.Fatalf("skipped=%v want [agent-b]", skipped)
	}
}

func TestCollectRelayParticipantsAllowsDirectSelfRecipient(t *testing.T) {
	participants, skipped := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			relayTmuxMember("codex-a", "s", "%1"),
			relayTmuxMember("agent-b", "s", "%2"),
		},
	}, agent.BoardMessage{Sender: "codex-a", Recipient: "codex-a"})
	if len(participants) != 1 || participants[0].ActorID != "codex-a" {
		t.Fatalf("participants=%v want only codex-a", participants)
	}
	if len(skipped) != 1 || skipped[0] != "agent-b" {
		t.Fatalf("skipped=%v want [agent-b]", skipped)
	}
}

func TestCollectRelayParticipantsUsesParticipantStateNotPresentation(t *testing.T) {
	// Key test: a member with canonical mux binding (presentation=detached)
	// should still be included because CanTriggerTurn=true via transport endpoint.
	participants, _ := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			relayTmuxMember("claude-a", "collab", "%42"),
		},
	}, agent.BoardMessage{Sender: "sender"})
	if len(participants) != 1 {
		t.Fatalf("participants=%d want 1", len(participants))
	}
	if !participants[0].State.CanTriggerTurn {
		t.Error("CanTriggerTurn=false, want true (transport-first, presentation-independent)")
	}
	if participants[0].State.Presentation != agent.PresentationDetached {
		t.Errorf("Presentation=%q want detached", participants[0].State.Presentation)
	}
}

func TestCollectRelayParticipantsSkipsViewerInboxTransports(t *testing.T) {
	participants, skipped := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			{
				ActorID: "actor:pi:local",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					TransportEndpoint: "p_21",
					TransportKind:     agent.PiExtensionTransportKind,
				},
			},
		},
	}, agent.BoardMessage{Sender: "sender"})
	if len(participants) != 0 {
		t.Fatalf("participants=%v want none for viewer/inbox transport", participants)
	}
	if len(skipped) != 1 || skipped[0] != "actor:pi:local" {
		t.Fatalf("skipped=%v want [actor:pi:local]", skipped)
	}
}

func TestRelayViaParticipantsUsesPaneSocket(t *testing.T) {
	// When a participant has TransportKind=pane_socket, relayViaParticipants
	// should use deliverAgentPane, not tmux DeliverTextWithOptions.
	origDeliver := deliverAgentPane
	defer func() { deliverAgentPane = origDeliver }()

	socketPath := "/tmp/test-foxctl-socket"
	calls := 0
	deliverAgentPane = func(ctx context.Context, socket string, msg agentpane.ControlMessage) (agentpane.ControlResponse, error) {
		calls++
		if socket != socketPath {
			t.Fatalf("socket=%q want %q", socket, socketPath)
		}
		if msg.Kind != "room_message" {
			t.Fatalf("kind=%q want room_message", msg.Kind)
		}
		if msg.Recipient != "droid-a" {
			t.Fatalf("recipient=%q want droid-a", msg.Recipient)
		}
		if msg.SubmitMode != agentpane.SubmitModeEnter {
			t.Fatalf("submit mode=%q want %q", msg.SubmitMode, agentpane.SubmitModeEnter)
		}
		return agentpane.ControlResponse{OK: true, BytesWritten: len(msg.Content)}, nil
	}

	room := agent.RoomSummary{
		ID: "test-room",
		Members: []agent.RoomMember{
			{
				ActorID: "droid-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "tmux",
					MuxSession:        "collab",
					MuxPaneID:         "%42",
					TransportEndpoint: socketPath,
					TransportKind:     agent.PaneSocketTransportKind,
				},
			},
		},
	}
	// Pane socket delivery doesn't use tmux client, but we still need a valid one.
	client := tmuxbridge.NewWithRunner(&relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{},
	}, map[string]string{})
	result := relayViaParticipants(context.Background(), client, room, agent.BoardMessage{
		Sender: "sender", Body: "hello", Subject: "test",
	})
	if calls != 1 {
		t.Fatalf("deliverAgentPane calls=%d want 1", calls)
	}
	if result.DeliveredCount != 1 {
		t.Fatalf("DeliveredCount=%d want 1", result.DeliveredCount)
	}
	if len(result.DeliveredTo) != 1 || result.DeliveredTo[0] != "droid-a" {
		t.Fatalf("DeliveredTo=%v want [droid-a]", result.DeliveredTo)
	}
}

func TestRelayViaParticipantsUsesBindingSubmitModeOverride(t *testing.T) {
	origDeliver := deliverAgentPane
	defer func() { deliverAgentPane = origDeliver }()

	socketPath := "/tmp/test-foxctl-socket-submit-override"
	deliverAgentPane = func(ctx context.Context, socket string, msg agentpane.ControlMessage) (agentpane.ControlResponse, error) {
		if socket != socketPath {
			t.Fatalf("socket=%q want %q", socket, socketPath)
		}
		if msg.SubmitMode != agentpane.SubmitModeEnter {
			t.Fatalf("submit mode=%q want %q", msg.SubmitMode, agentpane.SubmitModeEnter)
		}
		return agentpane.ControlResponse{OK: true, BytesWritten: len(msg.Content)}, nil
	}

	room := agent.RoomSummary{
		ID: "test-room",
		Members: []agent.RoomMember{
			{
				ActorID: "codex-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					TransportEndpoint: socketPath,
					TransportKind:     agent.PaneSocketTransportKind,
					SubmitMode:        agentpane.SubmitModeEnter,
				},
			},
		},
	}
	client := tmuxbridge.NewWithRunner(&relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{},
	}, map[string]string{})
	result := relayViaParticipants(context.Background(), client, room, agent.BoardMessage{
		Sender: "sender", Body: "hello", Subject: "test",
	})
	if result.DeliveredCount != 1 {
		t.Fatalf("DeliveredCount=%d want 1", result.DeliveredCount)
	}
}

func TestParticipantTransportTargetUsesSocketEndpointWithoutBackend(t *testing.T) {
	target, ok := participantTransportTarget(agent.ParticipantState{
		ActorID:           "codex-a",
		CanTriggerTurn:    true,
		TransportEndpoint: "/tmp/foxctl-pane/room/codex-a.sock",
	})
	if !ok {
		t.Fatal("participantTransportTarget() ok=false, want true")
	}
	if target != "/tmp/foxctl-pane/room/codex-a.sock" {
		t.Fatalf("participantTransportTarget() = %q, want socket endpoint", target)
	}
}

func TestRelayViaParticipantsUsesPaneSocketWithoutBackend(t *testing.T) {
	origDeliver := deliverAgentPane
	defer func() { deliverAgentPane = origDeliver }()

	socketPath := "/tmp/test-foxctl-socket-no-backend"
	calls := 0
	deliverAgentPane = func(ctx context.Context, socket string, msg agentpane.ControlMessage) (agentpane.ControlResponse, error) {
		calls++
		if socket != socketPath {
			t.Fatalf("socket=%q want %q", socket, socketPath)
		}
		if msg.Recipient != "codex-a" {
			t.Fatalf("recipient=%q want codex-a", msg.Recipient)
		}
		if msg.SubmitMode != agentpane.SubmitModeComposerCtrlEnter {
			t.Fatalf("submit mode=%q want %q", msg.SubmitMode, agentpane.SubmitModeComposerCtrlEnter)
		}
		return agentpane.ControlResponse{OK: true, BytesWritten: len(msg.Content)}, nil
	}

	room := agent.RoomSummary{
		ID: "test-room",
		Members: []agent.RoomMember{
			{
				ActorID: "codex-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					TransportEndpoint: socketPath,
					TransportKind:     agent.PaneSocketTransportKind,
				},
			},
		},
	}
	client := tmuxbridge.NewWithRunner(&relayTmuxRecordingRunner{
		responses: map[string]struct {
			stdout string
			stderr string
			err    error
		}{},
	}, map[string]string{})
	result := relayViaParticipants(context.Background(), client, room, agent.BoardMessage{
		Sender: "sender", Body: "hello", Subject: "test",
	})
	if calls != 1 {
		t.Fatalf("deliverAgentPane calls=%d want 1", calls)
	}
	if result.DeliveredCount != 1 {
		t.Fatalf("DeliveredCount=%d want 1", result.DeliveredCount)
	}
	if len(result.DeliveredTo) != 1 || result.DeliveredTo[0] != "codex-a" {
		t.Fatalf("DeliveredTo=%v want [codex-a]", result.DeliveredTo)
	}
}

func TestCollectRelayParticipantsIncludesTransportKind(t *testing.T) {
	participants, _ := collectRelayParticipants(agent.RoomSummary{
		Members: []agent.RoomMember{
			{
				ActorID: "claude-a",
				DeliveryBinding: &agent.RoomDeliveryBinding{
					MuxBackend:        "zellij",
					MuxSession:        "dev",
					MuxPaneID:         "terminal_0",
					TransportEndpoint: "/tmp/test.sock",
					TransportKind:     agent.PaneSocketTransportKind,
				},
			},
		},
	}, agent.BoardMessage{Sender: "sender"})
	if len(participants) != 1 {
		t.Fatalf("participants=%d want 1", len(participants))
	}
	if !participants[0].isPaneSocketDelivery() {
		t.Error("isPaneSocketDelivery=false, want true for pane_socket transport")
	}
}
