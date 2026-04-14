package agent

import (
	"fmt"
	"testing"
	"time"
)

func TestParticipantStateFromRoomMember(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name   string
		member RoomMember
		want   ParticipantState
	}{
		{
			name: "tmux member with explicit backend",
			member: RoomMember{
				ActorID:  "claude-a",
				Backend:  "tmux",
				Session:  "agentctl-collab",
				PaneID:   "%42",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:           "claude-a",
				Membership:        MembershipActive,
				TransportEndpoint: "tmux:agentctl-collab:%42",
				Transport:         TransportUnknown,
				Runtime:           RuntimeUnknown,
				Presentation:      PresentationDetached,
				MuxBackend:        "tmux",
				CanTriggerTurn:    true,
			},
		},
		{
			name: "zellij member with explicit backend",
			member: RoomMember{
				ActorID:  "codex-a",
				Backend:  "zellij",
				Session:  "dev-session",
				PaneID:   "terminal_1",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:           "codex-a",
				Membership:        MembershipActive,
				TransportEndpoint: "zellij:dev-session:terminal_1",
				Transport:         TransportUnknown,
				Runtime:           RuntimeUnknown,
				Presentation:      PresentationDetached,
				MuxBackend:        "zellij",
				CanTriggerTurn:    true,
			},
		},
		{
			name: "unbound member",
			member: RoomMember{
				ActorID:  "human-a",
				Unbound:  true,
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:        "human-a",
				Membership:     MembershipUnbound,
				Transport:      TransportNone,
				Runtime:        RuntimeUnknown,
				Presentation:   PresentationNone,
				CanTriggerTurn: false,
				Reason:         "member is unbound (no live transport binding)",
			},
		},
		{
			name: "member with no backend no pane no session",
			member: RoomMember{
				ActorID:  "plain-member",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:        "plain-member",
				Membership:     MembershipUnbound,
				Transport:      TransportNone,
				Runtime:        RuntimeUnknown,
				Presentation:   PresentationNone,
				CanTriggerTurn: false,
				Reason:         "member is unbound (no live transport binding)",
			},
		},
		{
			name: "legacy tmux participant id in actor id",
			member: RoomMember{
				ActorID:  "tmux:collab:%99",
				Session:  "collab",
				PaneID:   "%99",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:           "tmux:collab:%99",
				Membership:        MembershipActive,
				TransportEndpoint: "tmux:collab:%99",
				Transport:         TransportUnknown,
				Runtime:           RuntimeUnknown,
				Presentation:      PresentationDetached,
				MuxBackend:        "tmux",
				CanTriggerTurn:    true,
			},
		},
		{
			name: "legacy zellij participant id in actor id",
			member: RoomMember{
				ActorID:  "zellij:dev:pane_2",
				Session:  "dev",
				PaneID:   "pane_2",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:           "zellij:dev:pane_2",
				Membership:        MembershipActive,
				TransportEndpoint: "zellij:dev:pane_2",
				Transport:         TransportUnknown,
				Runtime:           RuntimeUnknown,
				Presentation:      PresentationDetached,
				MuxBackend:        "zellij",
				CanTriggerTurn:    true,
			},
		},
		{
			name: "whitespace is trimmed",
			member: RoomMember{
				ActorID:  "  claude-a  ",
				Backend:  "  tmux  ",
				Session:  "  session  ",
				PaneID:   "  %1  ",
				JoinedAt: now,
			},
			want: ParticipantState{
				ActorID:           "claude-a",
				Membership:        MembershipActive,
				TransportEndpoint: "tmux:session:%1",
				Transport:         TransportUnknown,
				Runtime:           RuntimeUnknown,
				Presentation:      PresentationDetached,
				MuxBackend:        "tmux",
				CanTriggerTurn:    true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParticipantStateFromRoomMember(tc.member)
			if got.ActorID != tc.want.ActorID {
				t.Errorf("ActorID = %q, want %q", got.ActorID, tc.want.ActorID)
			}
			if got.Membership != tc.want.Membership {
				t.Errorf("Membership = %q, want %q", got.Membership, tc.want.Membership)
			}
			if got.TransportEndpoint != tc.want.TransportEndpoint {
				t.Errorf("TransportEndpoint = %q, want %q", got.TransportEndpoint, tc.want.TransportEndpoint)
			}
			if got.Transport != tc.want.Transport {
				t.Errorf("Transport = %q, want %q", got.Transport, tc.want.Transport)
			}
			if got.Runtime != tc.want.Runtime {
				t.Errorf("Runtime = %q, want %q", got.Runtime, tc.want.Runtime)
			}
			if got.Presentation != tc.want.Presentation {
				t.Errorf("Presentation = %q, want %q", got.Presentation, tc.want.Presentation)
			}
			if got.MuxBackend != tc.want.MuxBackend {
				t.Errorf("MuxBackend = %q, want %q", got.MuxBackend, tc.want.MuxBackend)
			}
			if got.CanTriggerTurn != tc.want.CanTriggerTurn {
				t.Errorf("CanTriggerTurn = %v, want %v", got.CanTriggerTurn, tc.want.CanTriggerTurn)
			}
			if got.Reason != tc.want.Reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.want.Reason)
			}
		})
	}
}

func TestBuildParticipantStates(t *testing.T) {
	now := time.Now().UTC()
	members := []RoomMember{
		{ActorID: "a", Backend: "tmux", Session: "s", PaneID: "%1", JoinedAt: now},
		{ActorID: "b", Backend: "zellij", Session: "s", PaneID: "t1", JoinedAt: now},
		{ActorID: "c", Unbound: true, JoinedAt: now},
	}

	states := BuildParticipantStates(members)

	if len(states) != 3 {
		t.Fatalf("BuildParticipantStates returned %d states, want 3", len(states))
	}
	if states["a"].MuxBackend != "tmux" {
		t.Errorf("states[a].MuxBackend = %q, want tmux", states["a"].MuxBackend)
	}
	if states["a"].CanTriggerTurn != true {
		t.Errorf("states[a].CanTriggerTurn = false, want true")
	}
	if states["b"].MuxBackend != "zellij" {
		t.Errorf("states[b].MuxBackend = %q, want zellij", states["b"].MuxBackend)
	}
	if states["c"].Membership != MembershipUnbound {
		t.Errorf("states[c].Membership = %q, want unbound", states["c"].Membership)
	}
	if states["c"].CanTriggerTurn != false {
		t.Errorf("states[c].CanTriggerTurn = true, want false")
	}
}

func TestDefaultRoomDeliverySubmitModeForProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		actorID  string
		want     string
	}{
		{name: "droid provider uses plain enter", provider: "droid", actorID: "feat-internal-grouping-droid-review-b", want: RoomDeliverySubmitModeEnter},
		{name: "gemini provider overrides feature scoped actor id", provider: "gemini", actorID: "feat-internal-grouping-gemini-review-b", want: RoomDeliverySubmitModeEnterSplit},
		{name: "codex provider overrides feature scoped actor id", provider: "codex", actorID: "feat-internal-grouping-codex", want: RoomDeliverySubmitModeComposerCtrlEnter},
		{name: "claude provider overrides feature scoped actor id", provider: "claude", actorID: "feat-docs-claude-review", want: RoomDeliverySubmitModeEnter},
		{name: "falls back to actor id heuristic for droid", provider: "", actorID: "droid-a", want: RoomDeliverySubmitModeEnter},
		{name: "falls back to actor id heuristic when provider missing", provider: "", actorID: "gemini-a", want: RoomDeliverySubmitModeEnterSplit},
		{name: "falls back to newline when neither provider nor actor indicate one", provider: "", actorID: "feat-misc-reviewer", want: RoomDeliverySubmitModeNewline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DefaultRoomDeliverySubmitModeForProvider(tt.provider, tt.actorID); got != tt.want {
				t.Fatalf("DefaultRoomDeliverySubmitModeForProvider(%q, %q) = %q, want %q", tt.provider, tt.actorID, got, tt.want)
			}
		})
	}
}

func TestBuildParticipantStatesSkipsEmptyActorID(t *testing.T) {
	now := time.Now().UTC()
	members := []RoomMember{
		{ActorID: "", JoinedAt: now},
		{ActorID: "valid", Backend: "tmux", Session: "s", PaneID: "%1", JoinedAt: now},
	}
	states := BuildParticipantStates(members)
	if len(states) != 1 {
		t.Fatalf("BuildParticipantStates returned %d states, want 1 (empty actor_id skipped)", len(states))
	}
	if _, ok := states["valid"]; !ok {
		t.Error("states missing entry for 'valid'")
	}
}

func TestParticipantStateForActorID(t *testing.T) {
	states := map[string]ParticipantState{
		"a": {ActorID: "a", Membership: MembershipActive},
	}

	got := ParticipantStateForActorID(states, "a")
	if got.Membership != MembershipActive {
		t.Errorf("for known actor: Membership = %q, want active", got.Membership)
	}

	got = ParticipantStateForActorID(states, "unknown")
	if got.Membership != MembershipNone {
		t.Errorf("for unknown actor: Membership = %q, want none", got.Membership)
	}
	if got.Reason != "not a room member" {
		t.Errorf("for unknown actor: Reason = %q, want 'not a room member'", got.Reason)
	}
}

func TestExplainParticipantState(t *testing.T) {
	tests := []struct {
		name  string
		state ParticipantState
		want  string
	}{
		{
			name:  "active with unknown transport",
			state: ParticipantState{Membership: MembershipActive, Transport: TransportUnknown, Presentation: PresentationDetached},
			want:  "",
		},
		{
			name:  "unbound with reason",
			state: ParticipantState{Membership: MembershipUnbound, Reason: "no transport endpoint"},
			want:  "no transport endpoint",
		},
		{
			name:  "not a member",
			state: ParticipantState{Membership: MembershipNone, Transport: TransportNone, Runtime: RuntimeNone, Presentation: PresentationNone},
			want:  "limited availability: membership: none, transport: none, no presentation layer",
		},
		{
			name:  "no presentation",
			state: ParticipantState{Membership: MembershipActive, Transport: TransportUnknown, Presentation: PresentationNone},
			want:  "limited availability: no presentation layer",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExplainParticipantState(tc.state)
			if got != tc.want {
				t.Errorf("ExplainParticipantState() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeRoomMember(t *testing.T) {
	m := RoomMember{
		ActorID: "  a  ",
		Role:    "  lead  ",
		Backend: "  tmux  ",
		Session: "  s  ",
		PaneID:  "  %1  ",
	}
	got := NormalizeRoomMember(m)
	if got.ActorID != "a" {
		t.Errorf("ActorID = %q, want %q", got.ActorID, "a")
	}
	if got.Role != "lead" {
		t.Errorf("Role = %q, want %q", got.Role, "lead")
	}
	if got.Backend != "tmux" {
		t.Errorf("Backend = %q, want %q", got.Backend, "tmux")
	}
	if got.Session != "s" {
		t.Errorf("Session = %q, want %q", got.Session, "s")
	}
	if got.PaneID != "%1" {
		t.Errorf("PaneID = %q, want %q", got.PaneID, "%1")
	}
	if got.DeliveryBinding == nil {
		t.Fatal("DeliveryBinding = nil, want synthesized binding")
	}
	if got.DeliveryBinding.MuxBackend != "tmux" {
		t.Errorf("DeliveryBinding.MuxBackend = %q, want %q", got.DeliveryBinding.MuxBackend, "tmux")
	}
	if got.DeliveryBinding.SubmitMode != RoomDeliverySubmitModeNewline {
		t.Errorf("DeliveryBinding.SubmitMode = %q, want %q", got.DeliveryBinding.SubmitMode, RoomDeliverySubmitModeNewline)
	}
	if got.DeliveryBinding.Health != RoomDeliveryHealthUnknown {
		t.Errorf("DeliveryBinding.Health = %q, want %q", got.DeliveryBinding.Health, RoomDeliveryHealthUnknown)
	}
}

func TestNormalizeRoomMemberUsesExplicitDeliveryBinding(t *testing.T) {
	m := RoomMember{
		ActorID: " codex-a ",
		DeliveryBinding: &RoomDeliveryBinding{
			MuxBackend:        " zellij ",
			MuxSession:        " dev-session ",
			MuxPaneID:         " terminal_2 ",
			TransportEndpoint: " /tmp/agentctl-pane/dev-session/codex-a.sock ",
			TransportKind:     " pane_socket ",
			SubmitMode:        RoomDeliverySubmitModeComposerCtrlEnter,
			Health:            RoomDeliveryHealthReady,
			FallbackPolicy:    RoomDeliveryFallbackAllowLegacyMux,
		},
	}

	got := NormalizeRoomMember(m)

	if got.Backend != "zellij" {
		t.Fatalf("Backend = %q, want zellij", got.Backend)
	}
	if got.Session != "dev-session" {
		t.Fatalf("Session = %q, want dev-session", got.Session)
	}
	if got.PaneID != "terminal_2" {
		t.Fatalf("PaneID = %q, want terminal_2", got.PaneID)
	}
	if got.TransportEndpoint != "/tmp/agentctl-pane/dev-session/codex-a.sock" {
		t.Fatalf("TransportEndpoint = %q", got.TransportEndpoint)
	}
	if got.TransportKind != PaneSocketTransportKind {
		t.Fatalf("TransportKind = %q, want %q", got.TransportKind, PaneSocketTransportKind)
	}
	if got.DeliveryBinding == nil {
		t.Fatal("DeliveryBinding = nil, want explicit binding")
	}
	if got.DeliveryBinding.SubmitMode != RoomDeliverySubmitModeComposerCtrlEnter {
		t.Fatalf("DeliveryBinding.SubmitMode = %q, want %q", got.DeliveryBinding.SubmitMode, RoomDeliverySubmitModeComposerCtrlEnter)
	}
	if got.DeliveryBinding.Health != RoomDeliveryHealthReady {
		t.Fatalf("DeliveryBinding.Health = %q, want %q", got.DeliveryBinding.Health, RoomDeliveryHealthReady)
	}
}

func TestNormalizeRoomMemberMarksMemberUnboundWhenNoRouteExists(t *testing.T) {
	got := NormalizeRoomMember(RoomMember{
		ActorID: "reviewer-a",
		DeliveryBinding: &RoomDeliveryBinding{
			MuxBackend: "tmux",
		},
	})

	if !got.Unbound {
		t.Fatal("Unbound=false want true when no transport route exists")
	}
}

func TestParticipantStateFromRoomMember_PaneSocket(t *testing.T) {
	now := time.Now().UTC()
	m := RoomMember{
		ActorID:           "claude-a",
		Backend:           "zellij",
		Session:           "dev-session",
		PaneID:            "terminal_0",
		TransportEndpoint: "/tmp/agentctl-pane/dev-session/claude-a.sock",
		TransportKind:     PaneSocketTransportKind,
		JoinedAt:          now,
	}

	state := ParticipantStateFromRoomMember(m)

	if state.Membership != MembershipActive {
		t.Errorf("Membership = %q, want active", state.Membership)
	}
	if state.TransportEndpoint != "/tmp/agentctl-pane/dev-session/claude-a.sock" {
		t.Errorf("TransportEndpoint = %q, want socket path", state.TransportEndpoint)
	}
	// Transport starts Unknown — caller must apply a socket probe.
	if state.Transport != TransportUnknown {
		t.Errorf("Transport = %q, want unknown (probe not applied yet)", state.Transport)
	}
	if state.MuxBackend != "zellij" {
		t.Errorf("MuxBackend = %q, want zellij", state.MuxBackend)
	}
	// Presentation reflects the attached pane even though transport is via socket.
	if state.Presentation != PresentationDetached {
		t.Errorf("Presentation = %q, want detached (pane exists, not confirmed live)", state.Presentation)
	}
	// CanTriggerTurn is true because we have a transport endpoint.
	if !state.CanTriggerTurn {
		t.Error("CanTriggerTurn = false, want true (socket endpoint registered)")
	}
}

func TestApplySocketProbe_SocketExists(t *testing.T) {
	state := ParticipantState{
		TransportEndpoint: "/tmp/agentctl-pane/s/claude-a.sock",
		Transport:         TransportUnknown,
		Runtime:           RuntimeUnknown,
		CanTriggerTurn:    true,
	}
	result := ApplySocketProbe(state, true, nil)

	if result.Transport != TransportAvailable {
		t.Errorf("Transport = %q, want available", result.Transport)
	}
	if result.Runtime != RuntimeLive {
		t.Errorf("Runtime = %q, want live", result.Runtime)
	}
	if !result.CanTriggerTurn {
		t.Error("CanTriggerTurn = false, want true")
	}
}

func TestApplySocketProbe_SocketMissing(t *testing.T) {
	state := ParticipantState{
		TransportEndpoint: "/tmp/agentctl-pane/s/claude-a.sock",
		Transport:         TransportUnknown,
		Runtime:           RuntimeUnknown,
		CanTriggerTurn:    true,
	}
	result := ApplySocketProbe(state, false, fmt.Errorf("file not found"))

	if result.Transport != TransportUnavailable {
		t.Errorf("Transport = %q, want unavailable", result.Transport)
	}
	if result.Runtime != RuntimeStopped {
		t.Errorf("Runtime = %q, want stopped", result.Runtime)
	}
	if result.CanTriggerTurn {
		t.Error("CanTriggerTurn = true, want false")
	}
}

func TestApplySocketProbe_MuxAddressNotProbed(t *testing.T) {
	// mux-style addresses should not be modified by socket probe.
	state := ParticipantState{
		TransportEndpoint: "tmux:session:%42",
		Transport:         TransportUnknown,
		Runtime:           RuntimeUnknown,
	}
	result := ApplySocketProbe(state, true, nil)

	// Should be unchanged — we don't probe mux addresses.
	if result.Transport != TransportUnknown {
		t.Errorf("Transport = %q, want unknown (mux address not probed)", result.Transport)
	}
}

func TestParticipantStateCanTriggerTurnDecoupledFromPresentation(t *testing.T) {
	// Key acceptance criteria: a participant with a transport endpoint
	// can trigger turns even when presentation is detached.
	member := RoomMember{
		ActorID:  "claude-a",
		Backend:  "tmux",
		Session:  "agentctl-collab",
		PaneID:   "%42",
		JoinedAt: time.Now().UTC(),
	}
	state := ParticipantStateFromRoomMember(member)

	if state.Presentation != PresentationDetached {
		t.Errorf("Presentation = %q, want detached (mux not confirmed live)", state.Presentation)
	}
	if !state.CanTriggerTurn {
		t.Error("CanTriggerTurn = false for member with transport endpoint, want true")
	}
}
