package cmd

import (
	"testing"
	"time"

	domainagent "github.com/jkatigb/agentctl/internal/domain/agent"
)

func TestListZellijBoundPanesFiltersAndDedupesByLatestPane(t *testing.T) {
	base := time.Date(2026, 4, 4, 15, 0, 0, 0, time.UTC)
	all := []domainagent.Agent{
		{
			ID:        "old-parent",
			Role:      "overseer",
			State:     domainagent.StateError,
			CreatedAt: base,
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "alpha",
				PaneID:        "overseer",
				ParticipantID: "overseer",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "new-parent",
			Role:      "overseer",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "alpha",
				PaneID:        "overseer",
				ParticipantID: "overseer",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "child",
			ParentID:  "new-parent",
			Role:      "coder",
			State:     domainagent.StateStarting,
			CreatedAt: base.Add(2 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:             "zellij",
				Session:             "alpha",
				PaneID:              "coder-abc123",
				ParticipantID:       "coder-abc123",
				ParentParticipantID: "overseer",
				ParentAgentID:       "new-parent",
				RoomAccess:          "none",
			},
		},
		{
			ID:        "other-session",
			Role:      "reviewer",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(3 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "zellij",
				Session:       "beta",
				PaneID:        "reviewer-1",
				ParticipantID: "reviewer-1",
				RoomAccess:    "direct",
			},
		},
		{
			ID:        "tmux-agent",
			Role:      "coder",
			State:     domainagent.StateRunning,
			CreatedAt: base.Add(4 * time.Minute),
			TerminalBinding: domainagent.TerminalBinding{
				Backend:       "tmux",
				Session:       "14",
				PaneID:        "%1",
				ParticipantID: "tmux:14:%1",
			},
		},
	}

	panes := listZellijBoundPanes(all, "alpha")
	if len(panes) != 2 {
		t.Fatalf("len(panes) = %d, want 2", len(panes))
	}
	if panes[0].PaneName != "coder-abc123" || panes[0].AgentID != "child" {
		t.Fatalf("first pane = %+v, want child coder pane", panes[0])
	}
	if panes[1].PaneName != "overseer" || panes[1].AgentID != "new-parent" {
		t.Fatalf("second pane = %+v, want newest overseer pane", panes[1])
	}
}
