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
)

type zellijRelayRequest struct {
	RoomID  string   `json:"room_id"`
	Sender  string   `json:"sender"`
	Content string   `json:"content"`
	Targets []string `json:"targets"`
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
	pluginPath, err := ensureZellijRelayPlugin(ctx, relay.ZellijPluginPath)
	if err != nil {
		result.Error = err.Error()
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}

	payload, err := json.Marshal(zellijRelayRequest{
		RoomID:  room.ID,
		Sender:  strings.TrimSpace(msg.Sender),
		Content: formatRoomRelayContent(room, msg),
		Targets: targets,
	})
	if err != nil {
		result.Error = fmt.Sprintf("marshal zellij relay payload: %v", err)
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}

	pipeName := fmt.Sprintf("%s-%d", zellijRelayPipePrefix, time.Now().UnixNano())
	args := []string{"--session", session, "pipe", "--plugin", "file:" + pluginPath, "--name", pipeName, "--", string(payload)}
	cmd := exec.CommandContext(ctx, "zellij", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		result.Error = strings.TrimSpace(stderr.String())
		if result.Error == "" {
			result.Error = err.Error()
		}
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}

	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		result.Error = "zellij relay plugin returned no response"
		result.FailedCount = len(targets)
		result.FailedMembers = append(result.FailedMembers, targets...)
		return result
	}
	if err := json.Unmarshal(out, &result); err != nil {
		result = roomRelayResult{
			Backend:       "zellij",
			Error:         fmt.Sprintf("parse zellij relay response: %v", err),
			FailedCount:   len(targets),
			FailedMembers: append([]string(nil), targets...),
		}
	}
	if result.Backend == "" {
		result.Backend = "zellij"
	}
	return result
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
