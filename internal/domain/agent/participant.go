package agent

import (
	"strings"
)

// PaneSocketTransportKind is the TransportKind value used when a member has an
// agentctl pane serve wrapper socket registered.
const PaneSocketTransportKind = "pane_socket"

const (
	RoomDeliverySubmitModeNewline           = "newline"
	RoomDeliverySubmitModeEnter             = "enter"
	RoomDeliverySubmitModeEnterSplit        = "enter_split"
	RoomDeliverySubmitModeComposerCtrlEnter = "composer_ctrl_enter"

	RoomDeliveryHealthUnknown = "unknown"
	RoomDeliveryHealthReady   = "ready"

	RoomDeliveryFallbackAllowLegacyMux = "allow_legacy_mux"
)

// ParticipantMembership describes whether a participant is a known room member.
type ParticipantMembership string

const (
	// MembershipActive means the participant is a joined room member.
	MembershipActive ParticipantMembership = "active"
	// MembershipUnbound means the participant was joined but has no live transport binding.
	MembershipUnbound ParticipantMembership = "unbound"
	// MembershipNone means the participant is not a room member.
	MembershipNone ParticipantMembership = "none"
)

// TransportAvailability describes whether the participant's transport endpoint is reachable.
type TransportAvailability string

const (
	// TransportAvailable means the transport endpoint is reachable.
	TransportAvailable TransportAvailability = "available"
	// TransportUnknown means transport reachability has not been checked.
	TransportUnknown TransportAvailability = "unknown"
	// TransportUnavailable means the transport endpoint is not reachable.
	TransportUnavailable TransportAvailability = "unavailable"
	// TransportNone means the participant has no transport endpoint configured.
	TransportNone TransportAvailability = "none"
)

// RuntimeAvailability describes whether the participant's provider runtime is live.
type RuntimeAvailability string

const (
	// RuntimeLive means the provider runtime process is confirmed running.
	RuntimeLive RuntimeAvailability = "live"
	// RuntimeUnknown means the provider runtime state has not been checked.
	RuntimeUnknown RuntimeAvailability = "unknown"
	// RuntimeStopped means the provider runtime process is not running.
	RuntimeStopped RuntimeAvailability = "stopped"
	// RuntimeNone means the participant has no associated provider runtime.
	RuntimeNone RuntimeAvailability = "none"
)

// PresentationAttachment describes whether the participant has a live mux presentation layer.
type PresentationAttachment string

const (
	// PresentationAttached means a mux presentation (tmux/zellij pane) is connected.
	PresentationAttached PresentationAttachment = "attached"
	// PresentationDetached means the mux presentation exists but is not currently attached.
	PresentationDetached PresentationAttachment = "detached"
	// PresentationNone means there is no mux presentation layer.
	PresentationNone PresentationAttachment = "none"
)

// ParticipantState is the explicit, computed state of one room participant across four
// independent dimensions: membership, transport, runtime, and presentation.
//
// This type exists so that room status, inbox, and relay surfaces can explain *why*
// a participant is not acting without relying on pane names, scrollback, or transport
// heuristics. It is computed from RoomMember and live probe results; it is not persisted.
type ParticipantState struct {
	// ActorID is the stable participant identifier.
	ActorID string `json:"actor_id"`

	// Membership is the room membership status.
	Membership ParticipantMembership `json:"membership"`

	// TransportEndpoint is the transport address (e.g. "tmux:session:%123", "zellij:session:pane").
	// Empty when the participant has no transport binding.
	TransportEndpoint string `json:"transport_endpoint,omitempty"`

	// Transport is the transport endpoint reachability status.
	Transport TransportAvailability `json:"transport"`

	// Runtime is the provider runtime availability status.
	Runtime RuntimeAvailability `json:"runtime"`

	// Presentation is the mux presentation attachment status.
	Presentation PresentationAttachment `json:"presentation"`

	// MuxBackend is "tmux", "zellij", or empty when no mux is involved.
	MuxBackend string `json:"mux_backend,omitempty"`

	// Reason is a human-readable explanation when the participant is not fully available.
	// For example: "no transport endpoint", "tmux pane not found", "runtime not detected".
	Reason string `json:"reason,omitempty"`

	// CanTriggerTurn is true when this participant should be considered eligible for
	// turn triggering through the agentctl-owned transport path. This is independent
	// of presentation attachment — a detached mux pane does not prevent trigger delivery
	// if the transport endpoint is still reachable.
	CanTriggerTurn bool `json:"can_trigger_turn"`
}

func DefaultRoomDeliverySubmitMode(actorID string) string {
	t := strings.ToLower(strings.TrimSpace(actorID))
	switch {
	case strings.HasPrefix(t, "droid"):
		return RoomDeliverySubmitModeEnter
	case strings.HasPrefix(t, "codex"),
		strings.HasPrefix(t, "cursor"),
		strings.HasPrefix(t, "agent"):
		return RoomDeliverySubmitModeComposerCtrlEnter
	case strings.HasPrefix(t, "gemini"):
		return RoomDeliverySubmitModeEnterSplit
	case strings.HasPrefix(t, "claude"):
		return RoomDeliverySubmitModeEnter
	default:
		return RoomDeliverySubmitModeNewline
	}
}

func DefaultRoomDeliverySubmitModeForProvider(provider, actorID string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "droid":
		return RoomDeliverySubmitModeEnter
	case "codex", "cursor", "agent":
		return RoomDeliverySubmitModeComposerCtrlEnter
	case "gemini":
		return RoomDeliverySubmitModeEnterSplit
	case "claude":
		return RoomDeliverySubmitModeEnter
	}
	return DefaultRoomDeliverySubmitMode(actorID)
}

func NormalizeRoomDeliveryBinding(actorID string, binding *RoomDeliveryBinding) *RoomDeliveryBinding {
	if binding == nil {
		return nil
	}
	normalized := &RoomDeliveryBinding{
		MuxBackend:        strings.ToLower(strings.TrimSpace(binding.MuxBackend)),
		MuxSession:        strings.TrimSpace(binding.MuxSession),
		MuxPaneID:         strings.TrimSpace(binding.MuxPaneID),
		TransportEndpoint: strings.TrimSpace(binding.TransportEndpoint),
		TransportKind:     strings.ToLower(strings.TrimSpace(binding.TransportKind)),
		SubmitMode:        strings.TrimSpace(binding.SubmitMode),
		Health:            strings.TrimSpace(binding.Health),
		FallbackPolicy:    strings.TrimSpace(binding.FallbackPolicy),
	}
	if normalized.SubmitMode == "" {
		normalized.SubmitMode = DefaultRoomDeliverySubmitMode(actorID)
	}
	if normalized.Health == "" {
		normalized.Health = RoomDeliveryHealthUnknown
	}
	if normalized.FallbackPolicy == "" {
		normalized.FallbackPolicy = RoomDeliveryFallbackAllowLegacyMux
	}
	return normalized
}

// ParticipantStateFromRoomMember computes the initial ParticipantState from a RoomMember
// record without performing live probes. Transport and Runtime are set to Unknown;
// Presentation is derived from the member's Backend/Session/PaneID fields.
//
// When a member has TransportKind=PaneSocketTransportKind, the TransportEndpoint is
// the unix socket path for an agentctl pane serve wrapper. Otherwise the endpoint is
// derived from Backend/Session/PaneID.
//
// This is a pure function (no IO) suitable for the functional core.
func ParticipantStateFromRoomMember(member RoomMember) ParticipantState {
	member = NormalizeRoomMember(member)
	actorID := strings.TrimSpace(member.ActorID)
	binding := NormalizeRoomDeliveryBinding(actorID, member.DeliveryBinding)
	state := ParticipantState{
		ActorID:    actorID,
		Membership: MembershipActive,
		Runtime:    RuntimeUnknown,
	}

	// Pane socket transport: the member registered an agentctl pane wrapper.
	// Presentation may still exist (via Backend/Session/PaneID) but transport
	// goes through the socket, not the mux plugin.
	if binding != nil && binding.TransportEndpoint != "" && strings.ToLower(binding.TransportKind) == PaneSocketTransportKind {
		state.TransportEndpoint = binding.TransportEndpoint
		state.Transport = TransportUnknown // caller should probe with ApplySocketProbe
		state.MuxBackend = strings.ToLower(binding.MuxBackend)
		if binding.MuxBackend != "" && binding.MuxPaneID != "" {
			state.Presentation = PresentationDetached
		} else {
			state.Presentation = PresentationNone
		}
		state.CanTriggerTurn = true
		return state
	}

	if member.Unbound || binding == nil || (strings.TrimSpace(binding.MuxPaneID) == "" && strings.TrimSpace(binding.MuxSession) == "") && strings.TrimSpace(binding.TransportEndpoint) == "" {
		state.Membership = MembershipUnbound
		state.Transport = TransportNone
		state.Presentation = PresentationNone
		state.CanTriggerTurn = false
		if member.Unbound {
			state.Reason = "member is unbound (no live transport binding)"
		} else {
			state.Reason = "no transport endpoint configured"
		}
		return state
	}

	state.MuxBackend = strings.ToLower(binding.MuxBackend)
	switch state.MuxBackend {
	case "tmux":
		session := binding.MuxSession
		paneID := binding.MuxPaneID
		if session != "" && paneID != "" {
			state.TransportEndpoint = "tmux:" + session + ":" + paneID
		} else if paneID != "" {
			state.TransportEndpoint = paneID
		}
		state.Transport = TransportUnknown
		state.Presentation = PresentationDetached
	case "zellij":
		session := binding.MuxSession
		paneID := binding.MuxPaneID
		if session != "" && paneID != "" {
			state.TransportEndpoint = "zellij:" + session + ":" + paneID
		} else if paneID != "" {
			state.TransportEndpoint = paneID
		}
		state.Transport = TransportUnknown
		state.Presentation = PresentationDetached
	default:
		// Legacy members may have tmux-style actor IDs without explicit Backend.
		if strings.HasPrefix(actorID, "tmux:") {
			state.MuxBackend = "tmux"
			state.TransportEndpoint = actorID
		} else if strings.HasPrefix(actorID, "zellij:") {
			state.MuxBackend = "zellij"
			state.TransportEndpoint = actorID
		}
		state.Transport = TransportUnknown
		state.Presentation = PresentationDetached
	}

	// A participant with a transport endpoint can be triggered even without
	// a live presentation attachment — the trigger path is transport-first.
	state.CanTriggerTurn = state.TransportEndpoint != ""
	if !state.CanTriggerTurn {
		state.Reason = "no transport endpoint"
	}

	return state
}

// ApplySocketProbe updates Transport and Runtime on a ParticipantState based on
// whether the registered unix socket file exists. socketExists should be the result
// of os.Stat(state.TransportEndpoint) == nil.
//
// This is separated from ParticipantStateFromRoomMember to keep the domain layer IO-free;
// callers in cmd or service layers perform the actual file stat and pass the result here.
func ApplySocketProbe(state ParticipantState, socketExists bool, sockErr error) ParticipantState {
	if state.TransportEndpoint == "" {
		return state
	}
	// Only probe pane_socket endpoints (unix socket paths, not mux address strings).
	if strings.HasPrefix(state.TransportEndpoint, "tmux:") || strings.HasPrefix(state.TransportEndpoint, "zellij:") {
		return state
	}
	if sockErr == nil && socketExists {
		state.Transport = TransportAvailable
		state.Runtime = RuntimeLive
		state.CanTriggerTurn = true
	} else if sockErr != nil && !socketExists {
		state.Transport = TransportUnavailable
		state.Runtime = RuntimeStopped
		state.CanTriggerTurn = false
		state.Reason = "pane socket not found"
	}
	return state
}

// NormalizeRoomMember trims whitespace from a RoomMember.
func NormalizeRoomMember(m RoomMember) RoomMember {
	m.ActorID = strings.TrimSpace(m.ActorID)
	m.Role = strings.TrimSpace(m.Role)
	m.Backend = strings.TrimSpace(m.Backend)
	m.Session = strings.TrimSpace(m.Session)
	m.PaneID = strings.TrimSpace(m.PaneID)
	m.TransportEndpoint = strings.TrimSpace(m.TransportEndpoint)
	m.TransportKind = strings.TrimSpace(m.TransportKind)
	if m.DeliveryBinding == nil {
		m.DeliveryBinding = &RoomDeliveryBinding{
			MuxBackend:        m.Backend,
			MuxSession:        m.Session,
			MuxPaneID:         m.PaneID,
			TransportEndpoint: m.TransportEndpoint,
			TransportKind:     m.TransportKind,
		}
	}
	m.DeliveryBinding = NormalizeRoomDeliveryBinding(m.ActorID, m.DeliveryBinding)
	if m.DeliveryBinding != nil {
		m.Backend = m.DeliveryBinding.MuxBackend
		m.Session = m.DeliveryBinding.MuxSession
		m.PaneID = m.DeliveryBinding.MuxPaneID
		m.TransportEndpoint = m.DeliveryBinding.TransportEndpoint
		m.TransportKind = m.DeliveryBinding.TransportKind
	}
	if !m.Unbound && !roomMemberHasNormalizedTransportRoute(m) {
		m.Unbound = true
	}
	return m
}

func roomMemberHasNormalizedTransportRoute(m RoomMember) bool {
	if binding := m.DeliveryBinding; binding != nil {
		if strings.TrimSpace(binding.TransportEndpoint) != "" {
			return true
		}
		if strings.TrimSpace(binding.MuxPaneID) != "" {
			return true
		}
		if strings.TrimSpace(binding.MuxSession) != "" {
			return true
		}
	}
	actorID := strings.TrimSpace(m.ActorID)
	return strings.HasPrefix(actorID, "tmux:") || strings.HasPrefix(actorID, "zellij:")
}

// BuildParticipantStates computes ParticipantState for every member in a room summary.
// The result is keyed by ActorID for fast lookup.
func BuildParticipantStates(members []RoomMember) map[string]ParticipantState {
	out := make(map[string]ParticipantState, len(members))
	for _, m := range members {
		m = NormalizeRoomMember(m)
		if m.ActorID == "" {
			continue
		}
		out[m.ActorID] = ParticipantStateFromRoomMember(m)
	}
	return out
}

// ExplainParticipantState returns a human-readable summary of why a participant
// cannot act, or empty string if they appear fully available.
func ExplainParticipantState(state ParticipantState) string {
	if state.Reason != "" {
		return state.Reason
	}
	parts := make([]string, 0, 3)
	if state.Membership != MembershipActive {
		parts = append(parts, "membership: "+string(state.Membership))
	}
	if state.Transport != TransportAvailable && state.Transport != TransportUnknown {
		parts = append(parts, "transport: "+string(state.Transport))
	}
	if state.Presentation == PresentationNone {
		parts = append(parts, "no presentation layer")
	}
	if len(parts) == 0 {
		return ""
	}
	return "limited availability: " + strings.Join(parts, ", ")
}

// ParticipantStateForActorID looks up the participant state for a specific actor.
// Returns a MembershipNone state if the actor is not a room member.
func ParticipantStateForActorID(states map[string]ParticipantState, actorID string) ParticipantState {
	actorID = strings.TrimSpace(actorID)
	if s, ok := states[actorID]; ok {
		return s
	}
	return ParticipantState{
		ActorID:      actorID,
		Membership:   MembershipNone,
		Transport:    TransportNone,
		Runtime:      RuntimeNone,
		Presentation: PresentationNone,
		Reason:       "not a room member",
	}
}
