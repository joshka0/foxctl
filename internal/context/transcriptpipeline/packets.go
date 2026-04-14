package transcriptpipeline

import (
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/context/companion"
)

// InteractionPacket is the deterministic mainline unit for transcript memory derivation.
type InteractionPacket struct {
	PacketID       string    `json:"packet_id"`
	ConversationID string    `json:"conversation_id"`
	SessionID      string    `json:"session_id,omitempty"`
	UserText       string    `json:"user_text,omitempty"`
	AssistantText  string    `json:"assistant_text,omitempty"`
	FollowUpText   string    `json:"followup_text,omitempty"`
	StartedAt      time.Time `json:"started_at,omitempty"`
}

// SidecarPacket is the deterministic sidecar/subagent unit for grouped transcript processing.
type SidecarPacket struct {
	PacketID      string    `json:"packet_id"`
	SessionID     string    `json:"session_id"`
	AgentNickname string    `json:"agent_nickname,omitempty"`
	AgentRole     string    `json:"agent_role,omitempty"`
	SourcePath    string    `json:"source_path,omitempty"`
	SummaryText   string    `json:"summary_text,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
}

// PacketSet contains the packetized representation of one grouped transcript family.
type PacketSet struct {
	Mainline []InteractionPacket `json:"mainline,omitempty"`
	Sidecars []SidecarPacket     `json:"sidecars,omitempty"`
}

// BuildPacketSet converts grouped transcript state into explicit packet types.
func BuildPacketSet(group SourceGroup, frames []companion.AnchoredInteractionFrame) PacketSet {
	set := PacketSet{
		Mainline: make([]InteractionPacket, 0, len(frames)),
		Sidecars: make([]SidecarPacket, 0, len(group.SidecarBundles())),
	}

	for idx, frame := range frames {
		packet := InteractionPacket{
			PacketID:       fmt.Sprintf("mainline:%02d", idx),
			ConversationID: frame.ConversationID,
			SessionID:      group.GroupID,
			UserText:       strings.TrimSpace(frame.UserEvent.Content),
			AssistantText:  strings.TrimSpace(frame.AssistantEvent.Content),
			StartedAt:      time.Time{},
		}
		if frame.FollowUpUser != nil {
			packet.FollowUpText = strings.TrimSpace(frame.FollowUpUser.Content)
		}
		set.Mainline = append(set.Mainline, packet)
	}

	for idx, bundle := range group.SidecarBundles() {
		text := SummarizeSidecarBundle(bundle)
		if strings.TrimSpace(text) == "" {
			continue
		}
		packet := SidecarPacket{
			PacketID:      fmt.Sprintf("sidecar:%02d", idx),
			SessionID:     bundle.Meta.SessionID,
			AgentNickname: strings.TrimSpace(bundle.Meta.AgentNickname),
			AgentRole:     strings.TrimSpace(bundle.Meta.AgentRole),
			SourcePath:    strings.TrimSpace(bundle.Meta.SourcePath),
			SummaryText:   text,
			StartedAt:     bundle.Meta.StartedAt,
		}
		set.Sidecars = append(set.Sidecars, packet)
	}

	return set
}

// SummarizeSidecarBundle collapses one sidecar transcript into a short deterministic summary.
func SummarizeSidecarBundle(bundle SourceBundle) string {
	snippets := make([]string, 0, len(bundle.Parsed.Turns))
	for _, turn := range bundle.Parsed.Turns {
		text := strings.TrimSpace(companion.NormalizeTranscriptTurnText(turn.FinalOutput.Text))
		if text == "" {
			continue
		}
		snippets = append(snippets, truncatePacketInline(text, 220))
	}
	if len(snippets) == 0 {
		return ""
	}
	if len(snippets) > 3 {
		snippets = snippets[len(snippets)-3:]
	}
	return strings.Join(snippets, " | ")
}

func truncatePacketInline(text string, max int) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(text) <= max {
		return text
	}
	if max <= 1 {
		return text[:max]
	}
	return text[:max-1] + "…"
}
