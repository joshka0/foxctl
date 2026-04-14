package cmd

import (
	"context"
	"testing"
)

func TestResolveRoomSenderUsesParticipantEnvFallback(t *testing.T) {
	t.Setenv("AGENTCTL_PARTICIPANT_ID", "")
	t.Setenv("AGENTCTL_PARTICIPANT", "codex-a")
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("ZELLIJ", "")
	t.Setenv("ZELLIJ_SESSION_NAME", "")

	got, err := resolveRoomSender(context.Background(), "")
	if err != nil {
		t.Fatalf("resolveRoomSender() error = %v", err)
	}
	if got.Sender != "codex-a" {
		t.Fatalf("resolveRoomSender().Sender = %q, want codex-a", got.Sender)
	}
}
