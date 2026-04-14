package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/zellijbridge"
)

const (
	zellijRelayPipePrefix       = "foxctl-room-relay"
	zellijRelayArtifactName     = "zellij_room_relay.wasm"
	zellijRelayRelativeManifest = "plugins/zellij-room-relay/Cargo.toml"
	zellijRelaySingletonTarget  = "__singleton__"
)

type zellijRelayRequest struct {
	RoomID    string   `json:"room_id"`
	Sender    string   `json:"sender"`
	Content   string   `json:"content"`
	Interrupt bool     `json:"interrupt,omitempty"`
	Targets   []string `json:"targets"`
}

var deliverAgentPane = agentpane.Deliver

func relayRoomMessageZellij(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, relay roomRelayOptions) roomRelayResult {
	result := roomRelayResult{Backend: "zellij"}
	session, err := resolveZellijSession(relay.ZellijSession)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	// Use zellij-native targets (zellij:<session>:terminal_<id>), not tmux pane ids — the room-relay
	// plugin matches pane_target_matches / titles using those forms.
	_, zellijBySession, failed, skipped := collectRoomRelayTargetsByBackend(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	if len(failed) > 0 {
		result.FailedMembers = append(result.FailedMembers, failed...)
		result.FailedCount += len(failed)
	}
	targets := zellijBySession[session]
	if len(targets) == 0 {
		return result
	}
	return relayRoomMessageZellijTargets(ctx, room, msg, session, targets, relay)
}

func relayRoomMessageZellijTargets(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, session string, targets []string, relay roomRelayOptions) roomRelayResult {
	result := roomRelayResult{Backend: "zellij"}
	if len(targets) == 0 {
		return result
	}
	if onlySingletonRelayTarget(targets) {
		return relayRoomMessageZellijSingleton(ctx, room, msg, session)
	}
	remaining := make([]string, 0, len(targets))
	for _, target := range targets {
		submitMode := roomRelayTargetSubmitMode(room, msg, session, target)
		if deliverErr := relayRoomMessageZellijPaneSocket(ctx, room, msg, session, target, submitMode); deliverErr == nil {
			result.DeliveredCount++
			result.DeliveredTo = append(result.DeliveredTo, target)
			continue
		}
		if deliverErr := relayRoomMessageZellijTTY(session, target, formatRoomRelayContentForTarget(room, msg, target), msg.Interrupt, submitMode); deliverErr == nil {
			result.DeliveredCount++
			result.DeliveredTo = append(result.DeliveredTo, target)
			continue
		}
		remaining = append(remaining, target)
	}
	if len(remaining) == 0 {
		return result
	}
	pluginPath, err := ensureZellijRelayPlugin(ctx, relay.ZellijPluginPath)
	if err != nil {
		result.Error = err.Error()
		result.FailedCount = len(remaining)
		result.FailedMembers = append(result.FailedMembers, remaining...)
		return result
	}

	if err := ensureZellijRelaySessionReady(ctx, session, pluginPath); err != nil {
		result.Error = err.Error()
		result.FailedCount = len(remaining)
		result.FailedMembers = append(result.FailedMembers, remaining...)
		return result
	}

	payload, err := json.Marshal(zellijRelayRequest{
		RoomID:    room.ID,
		Sender:    strings.TrimSpace(msg.Sender),
		Content:   formatRoomRelayContentForTarget(room, msg, zellijRelaySingletonTarget),
		Interrupt: msg.Interrupt,
		Targets:   remaining,
	})
	if err != nil {
		result.Error = fmt.Sprintf("marshal zellij relay payload: %v", err)
		result.FailedCount = len(remaining)
		result.FailedMembers = append(result.FailedMembers, remaining...)
		return result
	}
	pipeName := fmt.Sprintf("%s-%d", zellijRelayPipePrefix, time.Now().UnixNano())
	pipeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	args := []string{"--session", session, "action", "pipe", "--plugin", "file:" + pluginPath, "--name", pipeName, "--", string(payload)}
	cmd := exec.CommandContext(pipeCtx, "zellij", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		result.Error = strings.TrimSpace(stderr.String())
		if result.Error == "" {
			if pipeCtx.Err() == context.DeadlineExceeded {
				result.Error = "zellij relay timed out while waiting for plugin action"
			} else {
				result.Error = err.Error()
			}
		}
		if hasPendingZellijRelayPermissionPrompt(ctx, session) {
			result.Error = "zellij relay plugin is waiting for permission approval in the attached zellij session; approve the prompt once and retry"
		}
		result.FailedCount = len(remaining)
		result.FailedMembers = append(result.FailedMembers, remaining...)
		return result
	}
	result.DeliveredCount += len(remaining)
	result.DeliveredTo = append(result.DeliveredTo, remaining...)
	return result
}

func roomRelayTargetSubmitMode(room agent.RoomSummary, msg agent.BoardMessage, session, target string) string {
	recipient := normalizeRoomRecipient(msg.Recipient)
	for _, member := range room.Members {
		member = normalizeRoomMember(member)
		if roomMemberRelayBackend(member) != "zellij" {
			continue
		}
		memberSession, memberTarget, ok := resolveRoomMemberZellijTarget(member)
		if !ok || strings.TrimSpace(memberSession) != strings.TrimSpace(session) || strings.TrimSpace(memberTarget) != strings.TrimSpace(target) {
			continue
		}
		if recipient != agent.BroadcastRecipient && !relayRecipientMatchesMember(room, member, recipient) {
			continue
		}
		if sameRoomParticipant(strings.TrimSpace(member.ActorID), strings.TrimSpace(msg.Sender)) &&
			(recipient == agent.BroadcastRecipient || !relayRecipientMatchesMember(room, member, recipient)) {
			continue
		}
		return roomMemberSubmitMode(member)
	}
	return targetSubmitMode(target)
}

func relayRoomMessageZellijPaneSocket(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, session, target, submitMode string) error {
	target = strings.TrimSpace(target)
	if strings.TrimSpace(session) == "" || target == "" || target == zellijRelaySingletonTarget {
		return fmt.Errorf("no pane socket route")
	}
	if strings.HasPrefix(target, "zellij:") || isResolvableZellijPaneID(target) {
		return fmt.Errorf("pane socket route requires named participant target")
	}
	candidates := []string{agentpane.DefaultSocketPath(session, target)}
	if roomID := strings.TrimSpace(room.ID); roomID != "" {
		candidates = append(candidates, agentpane.DefaultSocketPath(roomID, target))
	}
	var lastErr error
	for _, socketPath := range candidates {
		_, err := deliverAgentPane(ctx, socketPath, agentpane.ControlMessage{
			Kind:       "room_message",
			RoomID:     room.ID,
			MessageID:  strings.TrimSpace(msg.ID),
			Sender:     strings.TrimSpace(msg.Sender),
			Recipient:  target,
			Interrupt:  msg.Interrupt,
			Content:    formatRoomRelayContentForTarget(room, msg, target),
			SubmitMode: submitMode,
		})
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("deliver pane socket for %s in %s: %w", target, session, lastErr)
}

func relayRoomMessageZellijTTY(session, target, content string, interrupt bool, submitMode string) error {
	ttyPath, ok := zellijTTYPath(session, target)
	if !ok {
		return fmt.Errorf("no tty registry for %s", target)
	}
	payload := zellijTTYRelayPayload(target, content, interrupt, submitMode)
	f, err := os.OpenFile(ttyPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.WriteString(f, payload)
	return err
}

func zellijTTYPath(session, target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	// Named participants/panes created by foxctl register a tty file under this path.
	if !strings.HasPrefix(target, "zellij:") && !isResolvableZellijPaneID(target) {
		path := zellijbridge.TTYRegistryFile(session, target)
		if _, err := os.Stat(path); err == nil {
			data, readErr := os.ReadFile(path)
			if readErr == nil {
				if tty := strings.TrimSpace(string(data)); tty != "" {
					return tty, true
				}
			}
		}
	}
	return "", false
}

func zellijTTYRelayPayload(target, content string, interrupt bool, submitMode string) string {
	var b strings.Builder
	if interrupt && !usesComposerSubmitMode(submitMode) {
		b.WriteByte(0x1b)
	}
	b.WriteString(content)
	switch strings.TrimSpace(submitMode) {
	case agentpane.SubmitModeComposerCtrlEnter:
		b.WriteString("\x1b[13;5u")
	case agentpane.SubmitModeEnterSplit:
		b.WriteByte('\r')
	case agentpane.SubmitModeEnter:
		b.WriteByte('\r')
	default:
		b.WriteByte('\n')
	}
	return b.String()
}

func usesComposerSubmitMode(mode string) bool {
	return strings.TrimSpace(mode) == agentpane.SubmitModeComposerCtrlEnter
}

func targetSubmitMode(target string) string {
	return agent.DefaultRoomDeliverySubmitMode(target)
}

func targetUsesComposerSubmit(target string) bool {
	return targetSubmitMode(target) == agentpane.SubmitModeComposerCtrlEnter
}

func formatRoomRelayContentForTarget(room agent.RoomSummary, msg agent.BoardMessage, target string) string {
	content := formatRoomRelayContent(room, msg)
	target = strings.ToLower(strings.TrimSpace(target))
	if !strings.HasPrefix(target, "droid") || !msg.ReplyExpected {
		return content
	}
	sender := strings.TrimSpace(msg.Sender)
	if sender == "" || sender == "unknown" {
		return content
	}
	participantID := strings.TrimSpace(msg.Recipient)
	if participantID == "" || participantID == agent.BroadcastRecipient || participantID == zellijRelaySingletonTarget {
		participantID = strings.TrimSpace(target)
	}
	if participantID == "" {
		return content
	}
	return content + "\nExecute directly if you are ready to answer: " +
		fmt.Sprintf("foxctl room send %s --to %s --sender %s \"<response>\"", room.ID, sender, participantID)
}

func ensureZellijRelaySessionReady(ctx context.Context, session, pluginPath string) error {
	if !zellijSessionHasConnectedClients(ctx, session) {
		return fmt.Errorf("zellij relay requires at least one attached client in session %s", session)
	}
	cmd := exec.CommandContext(ctx, "zellij", "--session", session, "action", "start-or-reload-plugin", "file:"+pluginPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("zellij relay bootstrap: %s", msg)
	}
	if hasPendingZellijRelayPermissionPrompt(ctx, session) {
		return fmt.Errorf("zellij relay plugin is waiting for permission approval in session %s", session)
	}
	return nil
}

func zellijSessionHasConnectedClients(ctx context.Context, session string) bool {
	cmd := exec.CommandContext(ctx, "zellij", "--session", session, "action", "list-clients")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	return len(lines) > 1
}

func hasPendingZellijRelayPermissionPrompt(ctx context.Context, session string) bool {
	tmp, err := os.CreateTemp("", "foxctl-zellij-dump-*.txt")
	if err != nil {
		return false
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)
	cmd := exec.CommandContext(ctx, "zellij", "--session", session, "action", "dump-screen", path)
	if err := cmd.Run(); err != nil {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	screen := string(data)
	return strings.Contains(screen, "asks permission to:") && strings.Contains(screen, "Allow? (y/n)")
}

// zellijSingletonSubmitKind picks the trailing key sequence for relayRoomMessageZellijSingleton.
// "composer" = Kitty Ctrl+Enter CSI (aligns with tmux C-Enter for Droid/Codex/Cursor);
// "enter" = plain Enter (byte 13), which Gemini and Claude expect on the zellij path.
func zellijSingletonSubmitKind(room agent.RoomSummary, recipient string) string {
	recipient = normalizeRoomRecipient(recipient)
	if recipient == agent.BroadcastRecipient {
		return "enter"
	}
	id := strings.ToLower(strings.TrimSpace(recipient))
	if strings.HasPrefix(id, "droid") || strings.HasPrefix(id, "codex") || strings.HasPrefix(id, "cursor") {
		return "composer"
	}
	for _, member := range room.Members {
		if !sameRoomParticipant(member.ActorID, recipient) {
			continue
		}
		mid := strings.ToLower(strings.TrimSpace(member.ActorID))
		if strings.HasPrefix(mid, "droid") || strings.HasPrefix(mid, "codex") || strings.HasPrefix(mid, "cursor") {
			return "composer"
		}
		return "enter"
	}
	return "enter"
}

func relayRoomMessageZellijSingleton(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, session string) roomRelayResult {
	result := roomRelayResult{Backend: "zellij"}
	content := formatRoomRelayContentForTarget(room, msg, normalizeRoomRecipient(msg.Recipient))
	submitKind := zellijSingletonSubmitKind(room, msg.Recipient)
	// Composer targets match tmuxbridge: leading Escape clears their input; skip for Interrupt on those.
	if msg.Interrupt && submitKind != "composer" {
		interrupt := exec.CommandContext(ctx, "zellij", "--session", session, "action", "write", "27")
		var interruptErr bytes.Buffer
		interrupt.Stderr = &interruptErr
		if err := interrupt.Run(); err != nil {
			result.Error = strings.TrimSpace(interruptErr.String())
			if result.Error == "" {
				result.Error = err.Error()
			}
			result.FailedCount = 1
			result.FailedMembers = append(result.FailedMembers, zellijRelaySingletonTarget)
			return result
		}
	}
	writeChars := exec.CommandContext(ctx, "zellij", "--session", session, "action", "write-chars", content)
	var stderr bytes.Buffer
	writeChars.Stderr = &stderr
	if err := writeChars.Run(); err != nil {
		result.Error = strings.TrimSpace(stderr.String())
		if result.Error == "" {
			result.Error = err.Error()
		}
		result.FailedCount = 1
		result.FailedMembers = append(result.FailedMembers, zellijRelaySingletonTarget)
		return result
	}
	var submit *exec.Cmd
	if submitKind == "composer" {
		// Kitty keyboard protocol: Ctrl+Enter (same intent as tmux C-Enter).
		submit = exec.CommandContext(ctx, "zellij", "--session", session, "action", "write-chars", "\x1b[13;5u")
	} else {
		submit = exec.CommandContext(ctx, "zellij", "--session", session, "action", "write", "13")
	}
	stderr.Reset()
	submit.Stderr = &stderr
	if err := submit.Run(); err != nil {
		result.Error = strings.TrimSpace(stderr.String())
		if result.Error == "" {
			result.Error = err.Error()
		}
		result.FailedCount = 1
		result.FailedMembers = append(result.FailedMembers, zellijRelaySingletonTarget)
		return result
	}
	result.DeliveredCount = 1
	result.DeliveredTo = append(result.DeliveredTo, zellijRelaySingletonTarget)
	return result
}

func onlySingletonRelayTarget(targets []string) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if strings.TrimSpace(target) != zellijRelaySingletonTarget {
			return false
		}
	}
	return true
}

func resolveZellijSession(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("zellij relay requires --session or ZELLIJ_SESSION_NAME")
}

func ensureZellijRelayPlugin(ctx context.Context, explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		path, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("zellij relay plugin not found at %s", path)
		}
		return path, nil
	}
	if envPath := strings.TrimSpace(os.Getenv("AGENTCTL_ZELLIJ_ROOM_PLUGIN")); envPath != "" {
		path, err := filepath.Abs(envPath)
		if err == nil {
			if _, statErr := os.Stat(path); statErr == nil {
				return path, nil
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		path := filepath.Join(home, ".foxctl", "plugins", zellijRelayArtifactName)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	if manifest := findZellijRelayManifest(); manifest != "" {
		artifact := filepath.Join(filepath.Dir(manifest), "target", "wasm32-wasip1", "release", zellijRelayArtifactName)
		if _, err := os.Stat(artifact); err == nil {
			return artifact, nil
		}
		if err := buildZellijRelayPlugin(ctx, manifest); err != nil {
			return "", err
		}
		if _, err := os.Stat(artifact); err == nil {
			return artifact, nil
		}
	}
	return "", fmt.Errorf("zellij relay plugin not found; pass --plugin-path or build %s", zellijRelayRelativeManifest)
}

func findZellijRelayManifest() string {
	candidates := make([]string, 0, 2)
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, root := range candidates {
		if manifest := walkUpForRelativePath(root, zellijRelayRelativeManifest); manifest != "" {
			return manifest
		}
	}
	return ""
}

func walkUpForRelativePath(start, relative string) string {
	dir := filepath.Clean(start)
	for {
		candidate := filepath.Join(dir, relative)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func buildZellijRelayPlugin(ctx context.Context, manifest string) error {
	cmd := exec.CommandContext(ctx, "cargo", "build", "--manifest-path", manifest, "--target", "wasm32-wasip1", "--release")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("build zellij relay plugin: %s", message)
	}
	return nil
}
