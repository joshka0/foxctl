package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
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

func TestNewTmuxCommandHasSubmitAllAndInterruptAllSubcommands(t *testing.T) {
	cmd := newTmuxCommand()
	var foundSubmitAll, foundInterruptAll bool
	for _, sub := range cmd.Commands() {
		switch sub.Name() {
		case "submit-all":
			foundSubmitAll = true
		case "interrupt-all":
			foundInterruptAll = true
		}
	}
	if !foundSubmitAll {
		t.Fatal("expected tmux submit-all subcommand")
	}
	if !foundInterruptAll {
		t.Fatal("expected tmux interrupt-all subcommand")
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
	if got != "gemini --approval-mode yolo" {
		t.Fatalf("resolveMuxCreateCommand() = %q, want %q", got, "gemini --approval-mode yolo")
	}
}

func TestResolveMuxCreateCommandAutoModeAllowsDroid(t *testing.T) {
	got, err := resolveMuxCreateCommand("", "droid", "auto", nil, "")
	if err != nil {
		t.Fatalf("resolveMuxCreateCommand() error = %v", err)
	}
	if got != "droid" {
		t.Fatalf("resolveMuxCreateCommand() = %q, want %q", got, "droid")
	}
}

func TestResolveMuxCreateCommandResumeMappings(t *testing.T) {
	tests := []struct {
		name    string
		agent   string
		mode    string
		session string
		want    string
	}{
		{name: "codex", agent: "codex", mode: "interactive", session: "session-1", want: "codex resume session-1"},
		{name: "claude", agent: "claude", mode: "interactive", session: "session-2", want: "claude --resume session-2"},
		{name: "gemini", agent: "gemini", mode: "interactive", session: "session-3", want: "gemini --resume session-3"},
		{name: "droid", agent: "droid", mode: "interactive", session: "session-4", want: "droid --resume session-4"},
		{name: "agent", agent: "agent", mode: "interactive", session: "session-5", want: "agent --resume session-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveMuxCreateCommand("", tt.agent, tt.mode, nil, tt.session)
			if err != nil {
				t.Fatalf("resolveMuxCreateCommand() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveMuxCreateCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveMuxCreateCommandAutoModeUsesClaudeBypassPermissions(t *testing.T) {
	got, err := resolveMuxCreateCommand("", "claude", "auto", nil, "")
	if err != nil {
		t.Fatalf("resolveMuxCreateCommand() error = %v", err)
	}
	if got != "claude --permission-mode bypassPermissions" {
		t.Fatalf("resolveMuxCreateCommand() = %q, want %q", got, "claude --permission-mode bypassPermissions")
	}
}

func TestDeriveMuxCreateLabelPrefixSanitizesAgentName(t *testing.T) {
	if got := deriveMuxCreateLabelPrefix("", "", "Cursor Agent"); got != "cursor-agent" {
		t.Fatalf("deriveMuxCreateLabelPrefix() = %q, want cursor-agent", got)
	}
}

func TestDeriveMuxCreateLabelPrefixUsesRoomScope(t *testing.T) {
	if got := deriveMuxCreateLabelPrefix("foxctl-collab", "transport-first", "claude"); got != "transport-first-claude" {
		t.Fatalf("deriveMuxCreateLabelPrefix() = %q, want transport-first-claude", got)
	}
}

func TestDeriveMuxCreateLabelPrefixUsesSessionScopeWhenNoRoom(t *testing.T) {
	if got := deriveMuxCreateLabelPrefix("feature-auth", "", "codex"); got != "feature-auth-codex" {
		t.Fatalf("deriveMuxCreateLabelPrefix() = %q, want feature-auth-codex", got)
	}
}

func TestWrapZellijPaneCommandIncludesPaneServe(t *testing.T) {
	got := wrapZellijPaneCommand("sparkling-apricot", "claude-a", "room-1", "claude --resume abc", "")
	if !strings.Contains(got, " pane serve ") {
		t.Fatalf("wrapZellijPaneCommand() = %q, want pane serve wrapper", got)
	}
	if !strings.Contains(got, "--participant claude-a") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing participant", got)
	}
	if !strings.Contains(got, "sparkling-apricot") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing session-scoped socket path", got)
	}
	if !strings.Contains(got, ".sock") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing socket path", got)
	}
	if !strings.Contains(got, "--room-id room-1") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing room id", got)
	}
	if !strings.Contains(got, "sh -lc") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing shell wrapper", got)
	}
}

func TestWrapZellijPaneCommandIncludesStartupProfileWhenSet(t *testing.T) {
	got := wrapZellijPaneCommand("sparkling-apricot", "droid-a", "room-1", "droid", "droid_auto_high")
	if !strings.Contains(got, "--startup-profile droid_auto_high") {
		t.Fatalf("wrapZellijPaneCommand() = %q, missing startup profile", got)
	}
}

func TestZellijPaneStartupProfileUsesDroidAutoHigh(t *testing.T) {
	if got := zellijPaneStartupProfile("droid", "auto"); got != "droid_auto_high" {
		t.Fatalf("zellijPaneStartupProfile() = %q, want droid_auto_high", got)
	}
	if got := zellijPaneStartupProfile("droid", "interactive"); got != "" {
		t.Fatalf("zellijPaneStartupProfile() interactive = %q, want empty", got)
	}
	if got := zellijPaneStartupProfile("claude", "auto"); got != "" {
		t.Fatalf("zellijPaneStartupProfile() claude = %q, want empty", got)
	}
}

func TestBuildMuxCreateRoomAgentPromptIncludesParticipantAndRoom(t *testing.T) {
	got := buildMuxCreateRoomAgentPrompt("/repo", "room-1", "direct", "claude-a")
	if !strings.Contains(got, `room "room-1"`) {
		t.Fatalf("buildMuxCreateRoomAgentPrompt() = %q, missing room", got)
	}
	if !strings.Contains(got, `participant id is "claude-a"`) {
		t.Fatalf("buildMuxCreateRoomAgentPrompt() = %q, missing participant id", got)
	}
	if !strings.Contains(got, `foxctl room send room-1 --to <recipient> "<response>"`) {
		t.Fatalf("buildMuxCreateRoomAgentPrompt() = %q, missing sender-auto reply example", got)
	}
	if !strings.Contains(got, "`--sender claude-a` only when replying from outside this pane") {
		t.Fatalf("buildMuxCreateRoomAgentPrompt() = %q, missing outside-pane fallback", got)
	}
}

func TestMuxCreateInteractivePromptArgsForKnownAgents(t *testing.T) {
	if got := muxCreateInteractivePromptArgs("claude", "hello"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("claude prompt args = %v", got)
	}
	if got := muxCreateInteractivePromptArgs("codex", "hello"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("codex prompt args = %v", got)
	}
	if got := muxCreateInteractivePromptArgs("gemini", "hello"); len(got) != 2 || got[0] != "--prompt-interactive" || got[1] != "hello" {
		t.Fatalf("gemini prompt args = %v", got)
	}
	if got := muxCreateInteractivePromptArgs("droid", "hello"); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("droid prompt args = %v", got)
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
	activateTestRoomLoop(t, ctx, workspace, "alpha")
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
	activateTestRoomLoop(t, ctx, workspace, "alpha")
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
	activateTestRoomLoop(t, ctx, workspace, "alpha")
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

func TestTmuxSubmitModeForParticipant(t *testing.T) {
	if got := tmuxSubmitModeForParticipant("claude-a"); got != tmuxbridge.SubmitModeEnterOnly {
		t.Fatalf("claude submit mode = %q want %q", got, tmuxbridge.SubmitModeEnterOnly)
	}
	if got := tmuxSubmitModeForParticipant("droid-a"); got != tmuxbridge.SubmitModeEnterOnly {
		t.Fatalf("droid submit mode = %q want %q", got, tmuxbridge.SubmitModeEnterOnly)
	}
}

func TestZellijSubmitModeForParticipant(t *testing.T) {
	if got := zellijSubmitModeForParticipant("gemini-a"); got != tmuxbridge.SubmitModeEnterOnly {
		t.Fatalf("gemini submit mode = %q want enter_only", got)
	}
	if got := zellijSubmitModeForParticipant("codex-a"); got != tmuxbridge.SubmitModeEscapeEnter {
		t.Fatalf("codex submit mode = %q want escape_enter", got)
	}
}

func TestMuxGroupControlViaSocketUsesSubmitKind(t *testing.T) {
	prev := muxGroupDeliverAgentPane
	t.Cleanup(func() { muxGroupDeliverAgentPane = prev })

	var got agentpane.ControlMessage
	muxGroupDeliverAgentPane = func(ctx context.Context, socketPath string, msg agentpane.ControlMessage) (agentpane.ControlResponse, error) {
		got = msg
		return agentpane.ControlResponse{OK: true}, nil
	}
	if err := muxGroupControlViaSocket(context.Background(), "/tmp/test.sock", "claude-a", "submit"); err != nil {
		t.Fatalf("muxGroupControlViaSocket() error = %v", err)
	}
	if got.Kind != "submit" {
		t.Fatalf("kind=%q want submit", got.Kind)
	}
	if got.SubmitMode != agentpane.SubmitModeEnter {
		t.Fatalf("submit mode=%q want %q", got.SubmitMode, agentpane.SubmitModeEnter)
	}
}

func TestMuxGroupControlForMemberSkipsSystemParticipant(t *testing.T) {
	item := muxGroupControlForMember(context.Background(), agent.RoomMember{ActorID: "actor:system:room:alpha"}, "interrupt")
	if item.Status != "skipped" {
		t.Fatalf("status=%q want skipped", item.Status)
	}
}

func TestMuxGroupControlForMemberSkipsRawMuxIdentity(t *testing.T) {
	item := muxGroupControlForMember(context.Background(), agent.RoomMember{ActorID: "tmux:146:%156"}, "interrupt")
	if item.Status != "skipped" {
		t.Fatalf("status=%q want skipped", item.Status)
	}
}
