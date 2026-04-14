package cmd

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/agentpane"
	"github.com/jkatigb/agentctl/internal/runtime/terminal/tmuxbridge"
)

// participantTransportTarget extracts the transport target from a ParticipantState
// for use in relay delivery. Returns ("", false) when no target is available.
func participantTransportTarget(state agent.ParticipantState) (string, bool) {
	if !state.CanTriggerTurn {
		return "", false
	}
	endpoint := strings.TrimSpace(state.TransportEndpoint)
	// Fresh pane_socket registrations can exist before any mux backend metadata is
	// populated. Treat socket-like endpoints as first-class transport targets so
	// relay and loop pulses do not silently skip those members.
	if endpoint != "" && (filepath.IsAbs(endpoint) || strings.HasSuffix(strings.ToLower(endpoint), ".sock")) {
		return endpoint, true
	}
	switch strings.ToLower(strings.TrimSpace(state.MuxBackend)) {
	case "tmux":
		return participantTmuxTarget(state), true
	case "zellij":
		return endpoint, true
	default:
		return "", false
	}
}

// participantTmuxTarget derives a tmux target from a ParticipantState's transport endpoint.
func participantTmuxTarget(state agent.ParticipantState) string {
	endpoint := strings.TrimSpace(state.TransportEndpoint)
	if endpoint == "" {
		return ""
	}
	if ref, ok := tmuxbridge.ParseParticipantID(endpoint); ok {
		return ref.Target
	}
	// Plain pane ID like %42
	if strings.HasPrefix(endpoint, "%") {
		return endpoint
	}
	return endpoint
}

// canTriggerViaParticipantState reports whether a participant should receive turn
// trigger delivery through the agentctl-owned transport path (independent of mux
// presentation attachment). This is the core of the transport-first model: if the
// participant has a transport endpoint and CanTriggerTurn is true, the trigger path
// should go through the transport even when no mux client is attached.
func canTriggerViaParticipantState(state agent.ParticipantState) bool {
	return state.CanTriggerTurn && state.TransportEndpoint != ""
}

// relayParticipant is one resolved delivery target derived from participant state.
type relayParticipant struct {
	ActorID       string
	State         agent.ParticipantState
	Target        string // resolved tmux/zellij target or pane socket path for delivery
	Backend       string // "tmux" or "zellij"
	TransportKind string // "pane_socket", "mux_pane", or ""
	Member        agent.RoomMember
}

// isPaneSocketDelivery is true when this participant should be delivered via
// the agentpane socket path rather than mux send-keys.
func (p relayParticipant) isPaneSocketDelivery() bool {
	return strings.EqualFold(strings.TrimSpace(p.TransportKind), agent.PaneSocketTransportKind)
}

func (p relayParticipant) submitMode() string {
	return roomMemberSubmitMode(p.Member)
}

// collectRelayParticipants builds the delivery list using explicit participant state
// rather than raw member field heuristics. This is the transport-first relay path:
// it decides who gets delivery based on CanTriggerTurn and transport endpoint availability,
// independent of whether a mux presentation layer is attached.
//
// The sender is excluded from delivery. For direct messages (non-broadcast), only
// the matching recipient is included.
func collectRelayParticipants(room agent.RoomSummary, msg agent.BoardMessage) ([]relayParticipant, []string) {
	states := agent.BuildParticipantStates(room.Members)
	participants := make([]relayParticipant, 0, len(room.Members))
	skipped := make([]string, 0, len(room.Members))
	recipient := normalizeRoomRecipient(msg.Recipient)

	for _, m := range room.Members {
		m = agent.NormalizeRoomMember(m)
		actorID := m.ActorID
		if actorID == "" {
			continue
		}
		// Skip sender for broadcasts and for direct messages addressed to someone else.
		// A direct self-targeted message/reminder should still be delivered to the sender.
		if sameRoomParticipant(actorID, strings.TrimSpace(msg.Sender)) &&
			(recipient == agent.BroadcastRecipient || !relayRecipientMatchesMember(room, m, recipient)) {
			skipped = append(skipped, actorID)
			continue
		}
		// For direct messages, skip non-recipients.
		if recipient != agent.BroadcastRecipient && !relayRecipientMatchesMember(room, m, recipient) {
			skipped = append(skipped, actorID)
			continue
		}
		state, ok := states[actorID]
		if !ok {
			state = agent.ParticipantStateFromRoomMember(m)
		}
		if !canTriggerViaParticipantState(state) {
			skipped = append(skipped, actorID)
			continue
		}
		target, _ := participantTransportTarget(state)
		if target == "" {
			skipped = append(skipped, actorID)
			continue
		}
		participants = append(participants, relayParticipant{
			ActorID:       actorID,
			State:         state,
			Target:        target,
			Backend:       state.MuxBackend,
			TransportKind: m.TransportKind,
			Member:        m,
		})
	}
	return participants, skipped
}

// relayViaParticipants delivers a board message using the participant-state-aware
// transport path. This is the mux-independent trigger delivery: it uses explicit
// participant state to decide delivery targets.
//
// When a participant has TransportKind=pane_socket, delivery goes through
// deliverAgentPane (the agentctl-owned socket transport), not through mux
// send-keys. When the participant has a mux transport (no pane_socket), delivery
// falls through to tmux/zellij DeliverText as the transport mechanism.
//
// This function is additive -- when participants have no transport state, it
// returns zero deliveries and the caller should fall back to the legacy relay path.
func relayViaParticipants(ctx context.Context, client *tmuxbridge.Client, room agent.RoomSummary, msg agent.BoardMessage) roomRelayResult {
	result := roomRelayResult{Backend: "participant_transport"}
	participants, skipped := collectRelayParticipants(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)

	for _, p := range participants {
		if p.isPaneSocketDelivery() {
			// Pane socket delivery: use agentpane.Deliver through the registered
			// unix socket, independent of mux presentation.
			socketPath := strings.TrimSpace(p.State.TransportEndpoint)
			if socketPath == "" {
				result.FailedCount++
				result.FailedMembers = append(result.FailedMembers, p.ActorID)
				result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{
					Target: p.ActorID,
					Reason: "pane_socket endpoint is empty",
				})
				continue
			}
			participantTarget := strings.TrimSpace(p.ActorID)
			if participantTarget == "" {
				participantTarget = strings.TrimSpace(p.Target)
			}
			content := formatRoomRelayContentForTarget(room, msg, participantTarget)
			_, err := deliverAgentPane(ctx, socketPath, agentpane.ControlMessage{
				Kind:       "room_message",
				RoomID:     room.ID,
				MessageID:  strings.TrimSpace(msg.ID),
				Sender:     strings.TrimSpace(msg.Sender),
				Recipient:  participantTarget,
				Interrupt:  msg.Interrupt,
				Content:    content,
				SubmitMode: p.submitMode(),
			})
			if err != nil {
				result.FailedCount++
				result.FailedMembers = append(result.FailedMembers, p.ActorID)
				result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{
					Target: socketPath,
					Reason: err.Error(),
				})
				continue
			}
			result.DeliveredCount++
			result.DeliveredTo = append(result.DeliveredTo, p.ActorID)
			continue
		}

		// Mux transport delivery: tmux/zellij send-keys as the transport layer.
		content := formatRoomRelayContent(room, msg)
		switch p.Backend {
		case "tmux":
			_, err := client.DeliverTextWithOptions(ctx, p.Target, content, tmuxbridge.DeliverOptions{Interrupt: msg.Interrupt})
			if err != nil {
				result.FailedCount++
				result.FailedMembers = append(result.FailedMembers, p.ActorID)
				result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{
					Target: p.Target,
					Reason: err.Error(),
				})
				continue
			}
			result.DeliveredCount++
			result.DeliveredTo = append(result.DeliveredTo, p.ActorID)
		case "zellij":
			session := ""
			if s, _, ok := parseZellijParticipantID(p.ActorID); ok {
				session = s
			}
			zellijResult := relayRoomMessageZellijTargets(ctx, room, msg, session, []string{p.Target}, defaultRoomRelayOptions())
			result.DeliveredCount += zellijResult.DeliveredCount
			result.FailedCount += zellijResult.FailedCount
			result.DeliveredTo = append(result.DeliveredTo, zellijResult.DeliveredTo...)
			result.FailedMembers = append(result.FailedMembers, zellijResult.FailedMembers...)
		}
	}
	return result
}
