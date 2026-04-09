package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/tmuxbridge"
)

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

func TestNewTmuxCommandHasRemindSubcommand(t *testing.T) {
	cmd := newTmuxCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "remind" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tmux remind subcommand")
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

func TestResolveMuxRemindArgsUsesEnvRoomID(t *testing.T) {
	t.Setenv("AGENTCTL_ROOM_ID", "room-alpha")
	roomID, body, err := resolveMuxRemindArgs([]string{"check in"})
	if err != nil {
		t.Fatalf("resolveMuxRemindArgs() error = %v", err)
	}
	if roomID != "room-alpha" {
		t.Fatalf("resolveMuxRemindArgs() roomID = %q, want room-alpha", roomID)
	}
	if body != "check in" {
		t.Fatalf("resolveMuxRemindArgs() body = %q, want %q", body, "check in")
	}
}

func TestResolveMuxRemindArgsRequiresRoomIDOutsideRoomBoundPane(t *testing.T) {
	t.Setenv("AGENTCTL_ROOM_ID", "")
	if _, _, err := resolveMuxRemindArgs([]string{"check in"}); err == nil {
		t.Fatal("expected error when room id is missing outside room-bound pane")
	}
}

func TestResolveMuxSendConfirmationRejectsNonMember(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "cursor-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	replyTo := data["message_id"].(string)

	if _, err := resolveMuxSendConfirmation(ctx, workspace, "alpha", "ghost-a", replyTo, 5*time.Second); err == nil {
		t.Fatal("expected error for non-member confirm actor")
	}
}

func TestWaitForMuxRoomConfirmationDetectsRoomReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "cursor-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	replyTo := data["message_id"].(string)

	cmd, _ = newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "cursor-a", "human-a", "", "replying now", "info", "", 0, false, false, false, true); err != nil {
		t.Fatalf("runRoomSend reply: %v", err)
	}

	confirmation, err := waitForMuxRoomConfirmation(ctx, muxSendConfirmationSpec{
		Workspace: workspace,
		RoomID:    "alpha",
		ActorID:   "cursor-a",
		ReplyTo:   replyTo,
		Timeout:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("waitForMuxRoomConfirmation: %v", err)
	}
	if confirmation.Status != "confirmed" {
		t.Fatalf("status=%q want confirmed", confirmation.Status)
	}
	if confirmation.Signal != "room_reply" {
		t.Fatalf("signal=%q want room_reply", confirmation.Signal)
	}
	if !confirmation.ReplyInboxCleared {
		t.Fatalf("ReplyInboxCleared=%v want true", confirmation.ReplyInboxCleared)
	}
	if confirmation.ReplyMessageID == "" {
		t.Fatalf("ReplyMessageID empty")
	}
}

func TestWaitForMuxRoomConfirmationTimesOutWithoutReply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ctx := context.Background()
	workspace := t.TempDir()

	cmd, _ := newRoomTestCommand(ctx)
	if err := runRoomCreate(cmd, workspace, "alpha", "Alpha", "", []string{"human-a=coordinator", "cursor-a=reviewer"}); err != nil {
		t.Fatalf("runRoomCreate: %v", err)
	}
	cmd, out := newRoomTestCommand(ctx)
	if err := runRoomSend(cmd, workspace, "alpha", "human-a", "cursor-a", "", "please reply", "info", "", 0, false, true, false, true); err != nil {
		t.Fatalf("runRoomSend: %v", err)
	}
	data := decodeRoomEnvelope(t, out)
	replyTo := data["message_id"].(string)

	confirmation, err := waitForMuxRoomConfirmation(ctx, muxSendConfirmationSpec{
		Workspace: workspace,
		RoomID:    "alpha",
		ActorID:   "cursor-a",
		ReplyTo:   replyTo,
		Timeout:   10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if confirmation.Status != "timed_out_waiting_for_confirmation" {
		t.Fatalf("status=%q want timed_out_waiting_for_confirmation", confirmation.Status)
	}
}

func TestParseMuxSubmitMode(t *testing.T) {
	got, err := parseMuxSubmitModeString("")
	if err != nil {
		t.Fatalf("parseMuxSubmitModeString(\"\") error = %v", err)
	}
	if got != tmuxbridge.SubmitModeEscapeEnter {
		t.Fatalf("got %q, want %s", got, tmuxbridge.SubmitModeEscapeEnter)
	}
	got, err = parseMuxSubmitModeString("escape_enter")
	if err != nil || got != tmuxbridge.SubmitModeEscapeEnter {
		t.Fatalf("escape_enter: got %q err=%v", got, err)
	}
	got, err = parseMuxSubmitModeString("enter-only")
	if err != nil || got != tmuxbridge.SubmitModeEnterOnly {
		t.Fatalf("enter-only: got %q err=%v", got, err)
	}
	if _, err := parseMuxSubmitModeString("bogus"); err == nil {
		t.Fatal("expected error for bogus mode")
	}
}
