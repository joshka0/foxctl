package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/tmuxbridge"
)

type roomIdentity struct {
	Sender  string
	Backend string
	Session string
	PaneID  string
}

var newRoomTmuxClient = func() *tmuxbridge.Client {
	return tmuxbridge.New()
}

func resolveRoomSender(ctx context.Context, explicit string) (roomIdentity, error) {
	if sender := strings.TrimSpace(explicit); sender != "" {
		return classifyExplicitRoomSender(sender), nil
	}
	if sender := strings.TrimSpace(os.Getenv("AGENTCTL_PARTICIPANT_ID")); sender != "" {
		return classifyExplicitRoomSender(sender), nil
	}
	if strings.TrimSpace(os.Getenv("TMUX")) != "" || strings.TrimSpace(os.Getenv("TMUX_PANE")) != "" {
		return resolveCurrentTmuxRoomSender(ctx)
	}
	if strings.TrimSpace(os.Getenv("ZELLIJ")) != "" || strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) != "" {
		return resolveCurrentZellijRoomSender()
	}
	return roomIdentity{}, fmt.Errorf("sender is required outside tmux/zellij; pass --sender or run inside a labeled pane")
}

func resolveCurrentTmuxRoomSender(ctx context.Context) (roomIdentity, error) {
	client := newRoomTmuxClient()
	sender, pane, err := client.CurrentParticipantID(ctx)
	if err != nil {
		return roomIdentity{}, err
	}
	return roomIdentity{
		Sender:  sender,
		Backend: "tmux",
		Session: strings.TrimSpace(pane.Session),
		PaneID:  strings.TrimSpace(pane.ID),
	}, nil
}

func resolveCurrentZellijRoomSender() (roomIdentity, error) {
	session := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME"))
	if session == "" {
		return roomIdentity{}, fmt.Errorf("cannot derive zellij sender without ZELLIJ_SESSION_NAME; pass --sender")
	}
	if participant := strings.TrimSpace(os.Getenv("AGENTCTL_ZELLIJ_PARTICIPANT")); participant != "" {
		return roomIdentity{
			Sender:  participant,
			Backend: "zellij",
			Session: session,
		}, nil
	}
	paneID := strings.TrimSpace(os.Getenv("ZELLIJ_PANE_ID"))
	if paneID == "" {
		return roomIdentity{}, fmt.Errorf("cannot derive zellij sender without ZELLIJ_PANE_ID; pass --sender or set AGENTCTL_ZELLIJ_PARTICIPANT")
	}
	return roomIdentity{
		Sender:  formatZellijParticipantID(session, paneID),
		Backend: "zellij",
		Session: session,
		PaneID:  paneID,
	}, nil
}

func classifyExplicitRoomSender(sender string) roomIdentity {
	sender = strings.TrimSpace(sender)
	switch {
	case strings.HasPrefix(sender, "tmux:"):
		if ref, ok := tmuxbridge.ParseParticipantID(sender); ok {
			return roomIdentity{Sender: sender, Backend: "tmux", Session: ref.Session, PaneID: ref.Target}
		}
	case strings.HasPrefix(sender, "zellij:"):
		if session, paneID, ok := parseZellijParticipantID(sender); ok {
			return roomIdentity{Sender: sender, Backend: "zellij", Session: session, PaneID: paneID}
		}
	}
	return roomIdentity{Sender: sender}
}

func formatZellijParticipantID(session, paneID string) string {
	return "zellij:" + strings.TrimSpace(session) + ":" + normalizeZellijPaneID(paneID)
}

func parseZellijParticipantID(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "zellij:") {
		return "", "", false
	}
	parts := strings.SplitN(value, ":", 3)
	if len(parts) != 3 {
		return "", "", false
	}
	session := strings.TrimSpace(parts[1])
	paneID := normalizeZellijPaneID(parts[2])
	if session == "" || paneID == "" {
		return "", "", false
	}
	return session, paneID, true
}

func normalizeZellijPaneID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "terminal_") || strings.HasPrefix(value, "plugin_") {
		return value
	}
	if digitsOnly(value) {
		return "terminal_" + value
	}
	return value
}

func digitsOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
