package cmd

import "testing"

func TestNewTmuxCommandHasSendParentSubcommand(t *testing.T) {
	cmd := newTmuxCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "send-parent" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tmux send-parent subcommand")
	}
}

func TestResolveParentParticipantIDRequiresEnv(t *testing.T) {
	t.Setenv("AGENTCTL_PARENT_PARTICIPANT_ID", "")
	if _, err := resolveParentParticipantID(); err == nil {
		t.Fatal("expected error when AGENTCTL_PARENT_PARTICIPANT_ID is missing")
	}
}

func TestResolveParentParticipantIDReturnsEnv(t *testing.T) {
	t.Setenv("AGENTCTL_PARENT_PARTICIPANT_ID", "parent-a")
	got, err := resolveParentParticipantID()
	if err != nil {
		t.Fatalf("resolveParentParticipantID() error = %v", err)
	}
	if got != "parent-a" {
		t.Fatalf("resolveParentParticipantID() = %q, want parent-a", got)
	}
}

func TestResolveMuxCreateBackendAutoPrefersZellijWhenInsideSession(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "sparkling-apricot")
	if got := resolveMuxCreateBackend("auto"); got != "zellij" {
		t.Fatalf("resolveMuxCreateBackend(auto) = %q, want zellij", got)
	}
}

func TestResolveMuxCreateBackendAutoFallsBackToTmux(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "")
	if got := resolveMuxCreateBackend("auto"); got != "tmux" {
		t.Fatalf("resolveMuxCreateBackend(auto) = %q, want tmux", got)
	}
}

func TestResolveMuxCreateSessionUsesCurrentZellijSession(t *testing.T) {
	t.Setenv("ZELLIJ_SESSION_NAME", "sparkling-apricot")
	if got := resolveMuxCreateSession(nil, "zellij", ""); got != "sparkling-apricot" {
		t.Fatalf("resolveMuxCreateSession() = %q, want sparkling-apricot", got)
	}
}

func TestResolveMuxCreateCommandAutoModeMappings(t *testing.T) {
	got, err := resolveMuxCreateCommand("", "gemini", "auto", nil, "")
	if err != nil {
		t.Fatalf("resolveMuxCreateCommand() error = %v", err)
	}
	if got != "gemini --yolo" {
		t.Fatalf("resolveMuxCreateCommand() = %q, want %q", got, "gemini --yolo")
	}
}

func TestDeriveMuxCreateLabelPrefixSanitizesAgentName(t *testing.T) {
	if got := deriveMuxCreateLabelPrefix("Cursor Agent"); got != "cursor-agent" {
		t.Fatalf("deriveMuxCreateLabelPrefix() = %q, want cursor-agent", got)
	}
}
