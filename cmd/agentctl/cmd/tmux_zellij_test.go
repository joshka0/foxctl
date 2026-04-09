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

func TestZellijSingletonSubmitKind(t *testing.T) {
	t.Parallel()
	room := domainagent.RoomSummary{
		Members: []domainagent.RoomMember{
			{ActorID: "droid-a"},
			{ActorID: "gemini-a"},
			{ActorID: "claude-a"},
		},
	}
	if got := zellijSingletonSubmitKind(room, "droid-a"); got != "composer" {
		t.Fatalf("droid-a: %q want composer", got)
	}
	if got := zellijSingletonSubmitKind(room, "gemini-a"); got != "gemini" {
		t.Fatalf("gemini-a: %q want gemini", got)
	}
	if got := zellijSingletonSubmitKind(room, "claude-a"); got != "enter" {
		t.Fatalf("claude-a: %q want enter", got)
	}
	if got := zellijSingletonSubmitKind(room, domainagent.BroadcastRecipient); got != "enter" {
		t.Fatalf("broadcast: %q want enter", got)
	}
}
