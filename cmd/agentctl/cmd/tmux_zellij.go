package cmd

import (
	"context"
	"sort"
	"strings"
	"time"

	domainagent "github.com/jkatigb/agentctl/internal/domain/agent"
	agentstore "github.com/jkatigb/agentctl/internal/storage/agents"
)

type zellijBoundPane struct {
	Session             string            `json:"session"`
	PaneName            string            `json:"pane_name"`
	ParticipantID       string            `json:"participant_id,omitempty"`
	AgentID             string            `json:"agent_id"`
	ParentID            string            `json:"parent_id,omitempty"`
	Role                string            `json:"role"`
	State               domainagent.State `json:"state"`
	CreatedAt           string            `json:"created_at"`
	ParentParticipantID string            `json:"parent_participant_id,omitempty"`
	ParentAgentID       string            `json:"parent_agent_id,omitempty"`
	RoomAccess          string            `json:"room_access,omitempty"`
}

func openAgentStore(ctx context.Context, storageRoot string) (agentstore.Store, error) {
	return agentstore.Open(ctx, storageRoot)
}

func listZellijBoundPanes(all []domainagent.Agent, session string) []zellijBoundPane {
	resolvedSession := strings.TrimSpace(session)
	type paneEntry struct {
		pane      zellijBoundPane
		createdAt time.Time
	}
	byPane := make(map[string]paneEntry)
	for _, item := range all {
		binding := domainagent.NormalizeTerminalBinding(item.TerminalBinding)
		if binding.Backend != "zellij" {
			continue
		}
		if resolvedSession != "" && binding.Session != resolvedSession {
			continue
		}
		paneName := strings.TrimSpace(binding.PaneID)
		if paneName == "" {
			continue
		}
		candidate := zellijBoundPane{
			Session:             binding.Session,
			PaneName:            paneName,
			ParticipantID:       binding.ParticipantID,
			AgentID:             item.ID,
			ParentID:            item.ParentID,
			Role:                item.Role,
			State:               item.State,
			CreatedAt:           item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ParentParticipantID: binding.ParentParticipantID,
			ParentAgentID:       binding.ParentAgentID,
			RoomAccess:          binding.RoomAccess,
		}
		existing, ok := byPane[paneName]
		if !ok || item.CreatedAt.After(existing.createdAt) {
			byPane[paneName] = paneEntry{
				pane:      candidate,
				createdAt: item.CreatedAt,
			}
		}
	}
	panes := make([]zellijBoundPane, 0, len(byPane))
	for _, item := range byPane {
		panes = append(panes, item.pane)
	}
	sort.Slice(panes, func(i, j int) bool {
		if panes[i].Session != panes[j].Session {
			return panes[i].Session < panes[j].Session
		}
		return panes[i].PaneName < panes[j].PaneName
	})
	return panes
}
