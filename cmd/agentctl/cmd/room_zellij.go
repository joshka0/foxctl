package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
)

const (
	zellijRelayPipePrefix       = "agentctl-room-relay"
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

func relayRoomMessageZellij(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, relay roomRelayOptions) roomRelayResult {
	result := roomRelayResult{Backend: "zellij"}
	targets, skipped := collectRoomRelayTargets(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	if len(targets) == 0 {
		return result
	}
	session, err := resolveZellijSession(relay.ZellijSession)
	if err != nil {
		result.Error = err.Error()
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
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
	pluginPath, err := ensureZellijRelayPlugin(ctx, relay.ZellijPluginPath)
	if err != nil {
		result.Error = err.Error()
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}

	if err := ensureZellijRelaySessionReady(ctx, session, pluginPath); err != nil {
		result.Error = err.Error()
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}

	payload, err := json.Marshal(zellijRelayRequest{
		RoomID:    room.ID,
		Sender:    strings.TrimSpace(msg.Sender),
		Content:   formatRoomRelayContent(room, msg),
		Interrupt: msg.Interrupt,
		Targets:   targets,
	})
	if err != nil {
		result.Error = fmt.Sprintf("marshal zellij relay payload: %v", err)
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
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
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}
	result.DeliveredCount = len(targets)
	result.DeliveredTo = append(result.DeliveredTo, targets...)
	return result
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
	tmp, err := os.CreateTemp("", "agentctl-zellij-dump-*.txt")
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

func relayRoomMessageZellijSingleton(ctx context.Context, room agent.RoomSummary, msg agent.BoardMessage, session string) roomRelayResult {
	result := roomRelayResult{Backend: "zellij"}
	content := formatRoomRelayContent(room, msg)
	if msg.Interrupt {
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
	submit := exec.CommandContext(ctx, "zellij", "--session", session, "action", "write", "13")
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
		path := filepath.Join(home, ".agentctl", "plugins", zellijRelayArtifactName)
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
