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
