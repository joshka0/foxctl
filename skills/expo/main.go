// Package main provides the expo skill for unified Expo development operations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const skillName = "mobile/expo"

// Input represents the skill input parameters.
type Input struct {
	Operation     string `json:"operation"`
	DeviceID      string `json:"device_id,omitempty"`
	Platform      string `json:"platform,omitempty"` // ios, android, auto
	URL           string `json:"url,omitempty"`
	BuildPlatform string `json:"build_platform,omitempty"` // ios, android, all
	Profile       string `json:"profile,omitempty"`        // development, preview, production
	Channel       string `json:"channel,omitempty"`
	Message       string `json:"message,omitempty"`
	Filter        string `json:"filter,omitempty"`
	Count         int    `json:"count,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("EARG", err)
	}

	// Detect platform if not specified
	platform := detectPlatform(in.DeviceID, in.Platform)

	switch in.Operation {
	// Device operations
	case "shake":
		err = shake(ctx, rc, platform, in.DeviceID)
	case "reload":
		err = reload(ctx, rc, platform, in.DeviceID)
	case "deep_link":
		err = deepLink(ctx, rc, platform, in.DeviceID, in.URL)
	case "dev_menu":
		err = devMenu(ctx, rc, platform, in.DeviceID)
	case "toggle_inspector":
		err = toggleInspector(ctx, rc, platform, in.DeviceID)
	case "toggle_performance":
		err = togglePerformance(ctx, rc, platform, in.DeviceID)
	case "toggle_remote_debug":
		err = toggleRemoteDebug(ctx, rc, platform, in.DeviceID)
	// EAS operations
	case "build":
		err = build(ctx, rc, in.BuildPlatform, in.Profile)
	case "update":
		err = update(ctx, rc, in.Channel, in.Message)
	case "build_status":
		err = buildStatus(ctx, rc)
	// Logs
	case "logs":
		err = logs(ctx, rc, in.Filter, in.Count)
	default:
		fail("EARG", fmt.Errorf("unknown operation: %s", in.Operation))
	}

	if err != nil {
		fail("ERUNTIME", err)
	}
}

func fail(code string, err error) {
	// Emit canonical error envelope to stdout
	data := map[string]any{"hint": "Check device connectivity and platform availability"}
	env := envelope.Error(skillName, code, err.Error(), data)
	_ = envelope.Write(os.Stdout, env)
	os.Exit(1)
}

func emit(rc *runner.RunnerContext, data map[string]any) error {
	return rc.Emit(skillName, data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return Input{}, fmt.Errorf("decode input: %w", err)
	}

	if in.Operation == "" {
		return Input{}, fmt.Errorf("operation is required")
	}

	return in, nil
}

// detectPlatform determines the platform from device ID or explicit setting.
func detectPlatform(deviceID, explicit string) string {
	if explicit != "" && explicit != "auto" {
		return explicit
	}

	if deviceID == "" {
		// Default to iOS if no device specified
		return "ios"
	}

	// Android emulator patterns
	if strings.HasPrefix(deviceID, "emulator-") ||
		strings.Contains(deviceID, ":5") {
		return "android"
	}

	// iOS UDID is typically 36 chars (with dashes) or 40 chars hex
	if len(deviceID) >= 36 {
		return "ios"
	}

	return "ios"
}

// ============================================================================
// Device Operations
// ============================================================================

// shake triggers a shake gesture to open the Expo dev menu.
func shake(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	var err error
	if platform == "android" {
		err = androidShake(ctx, deviceID)
	} else {
		err = iosShake(ctx, deviceID)
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "shake",
		"platform":  platform,
		"success":   true,
		"message":   "Shake gesture triggered (opens Expo dev menu)",
	})
}

func iosShake(ctx context.Context, udid string) error {
	// Method 1: Try sending keyboard shortcut via IDB (Cmd+D opens React Native dev menu)
	// First, focus the simulator
	focusArgs := []string{"focus"}
	if udid != "" {
		focusArgs = append(focusArgs, "--udid", udid)
	}
	focusCmd := exec.CommandContext(ctx, "idb", focusArgs...)
	_ = focusCmd.Run() // Ignore errors, just best effort

	// Method 2: Use IDB to send key sequence that opens dev menu
	// Sending 'd' key typically opens dev menu in Expo apps
	keyArgs := []string{"ui", "text", "d"}
	if udid != "" {
		keyArgs = append(keyArgs, "--udid", udid)
	}
	keyCmd := exec.CommandContext(ctx, "idb", keyArgs...)
	if err := keyCmd.Run(); err != nil {
		// Method 3: Try AppleScript to trigger shake via menu (requires accessibility)
		script := `tell application "Simulator" to activate
delay 0.3
tell application "System Events"
	tell process "Simulator"
		click menu item "Shake" of menu "Device" of menu bar 1
	end tell
end tell`
		appleCmd := exec.CommandContext(ctx, "osascript", "-e", script)
		if appleErr := appleCmd.Run(); appleErr != nil {
			return fmt.Errorf("shake failed (idb key: %v, applescript: %v)", err, appleErr)
		}
	}
	return nil
}

func androidShake(ctx context.Context, serial string) error {
	// Android: Send accelerometer event to simulate shake
	// Using input command to send key events that trigger shake detection
	args := adbArgs(serial, "shell", "input", "keyevent", "82") // KEYCODE_MENU opens dev menu
	cmd := exec.CommandContext(ctx, "adb", args...)
	return cmd.Run()
}

// reload triggers a JS bundle reload.
func reload(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	var err error
	if platform == "android" {
		err = androidReload(ctx, deviceID)
	} else {
		err = iosReload(ctx, deviceID)
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "reload",
		"platform":  platform,
		"success":   true,
		"message":   "Expo reload triggered",
	})
}

func iosReload(ctx context.Context, udid string) error {
	// Shake to open dev menu
	if err := iosShake(ctx, udid); err != nil {
		return fmt.Errorf("shake for reload: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Type 'r' for reload
	args := []string{"ui", "text", "r"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	return cmd.Run()
}

func androidReload(ctx context.Context, serial string) error {
	// Open dev menu first
	if err := androidShake(ctx, serial); err != nil {
		return fmt.Errorf("open dev menu for reload: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Send 'r' key for reload (double-tap R in Expo)
	args := adbArgs(serial, "shell", "input", "text", "rr")
	cmd := exec.CommandContext(ctx, "adb", args...)
	return cmd.Run()
}

// deepLink opens an Expo deep link URL.
func deepLink(ctx context.Context, rc *runner.RunnerContext, platform, deviceID, url string) error {
	if url == "" {
		return fmt.Errorf("url is required for deep_link operation")
	}

	// Ensure URL has exp:// or exps:// scheme
	if !strings.HasPrefix(url, "exp://") && !strings.HasPrefix(url, "exps://") {
		url = "exp://" + url
	}

	var err error
	if platform == "android" {
		err = androidDeepLink(ctx, deviceID, url)
	} else {
		err = iosDeepLink(ctx, deviceID, url)
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "deep_link",
		"platform":  platform,
		"url":       url,
		"success":   true,
		"message":   "Expo deep link opened",
	})
}

func iosDeepLink(ctx context.Context, udid, url string) error {
	args := []string{"open", url}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	return cmd.Run()
}

func androidDeepLink(ctx context.Context, serial, url string) error {
	args := adbArgs(serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	cmd := exec.CommandContext(ctx, "adb", args...)
	return cmd.Run()
}

// devMenu opens the Expo developer menu.
func devMenu(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	// Same as shake - opens the dev menu
	return shake(ctx, rc, platform, deviceID)
}

// toggleInspector toggles the React element inspector.
func toggleInspector(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	var err error
	if platform == "android" {
		err = androidToggleDevOption(ctx, deviceID, "i")
	} else {
		err = iosToggleDevOption(ctx, deviceID, "i")
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "toggle_inspector",
		"platform":  platform,
		"success":   true,
		"message":   "Element inspector toggled (opens dev menu + 'i')",
	})
}

// togglePerformance toggles the performance monitor.
func togglePerformance(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	var err error
	if platform == "android" {
		err = androidToggleDevOption(ctx, deviceID, "p")
	} else {
		err = iosToggleDevOption(ctx, deviceID, "p")
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "toggle_performance",
		"platform":  platform,
		"success":   true,
		"message":   "Performance monitor toggled (opens dev menu + 'p')",
	})
}

// toggleRemoteDebug toggles remote JS debugging.
func toggleRemoteDebug(ctx context.Context, rc *runner.RunnerContext, platform, deviceID string) error {
	var err error
	if platform == "android" {
		err = androidToggleDevOption(ctx, deviceID, "d")
	} else {
		err = iosToggleDevOption(ctx, deviceID, "d")
	}

	if err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "toggle_remote_debug",
		"platform":  platform,
		"success":   true,
		"message":   "Remote debugging toggled (opens dev menu + 'd')",
	})
}

func iosToggleDevOption(ctx context.Context, udid, key string) error {
	// Open dev menu
	if err := iosShake(ctx, udid); err != nil {
		return fmt.Errorf("open dev menu: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Type the key
	args := []string{"ui", "text", key}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	return cmd.Run()
}

func androidToggleDevOption(ctx context.Context, serial, key string) error {
	// Open dev menu
	args := adbArgs(serial, "shell", "input", "keyevent", "82")
	cmd := exec.CommandContext(ctx, "adb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open dev menu: %w", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Type the key
	args = adbArgs(serial, "shell", "input", "text", key)
	cmd = exec.CommandContext(ctx, "adb", args...)
	return cmd.Run()
}

// ============================================================================
// EAS Operations
// ============================================================================

// build triggers an EAS cloud build.
func build(ctx context.Context, rc *runner.RunnerContext, buildPlatform, profile string) error {
	if buildPlatform == "" {
		buildPlatform = "all"
	}
	if profile == "" {
		profile = "development"
	}

	args := []string{"build", "--platform", buildPlatform, "--profile", profile, "--json", "--non-interactive"}

	cmd := exec.CommandContext(ctx, "eas", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("eas build: %w", err)
	}

	// Parse JSON output
	var builds []map[string]any
	if err := json.Unmarshal(output, &builds); err != nil {
		// Try single object
		var build map[string]any
		if err := json.Unmarshal(output, &build); err != nil {
			return emit(rc, map[string]any{
				"operation":      "build",
				"build_platform": buildPlatform,
				"profile":        profile,
				"success":        true,
				"raw_output":     string(output),
				"message":        "Build triggered (could not parse JSON output)",
			})
		}
		builds = []map[string]any{build}
	}

	// Extract build IDs
	buildIDs := []string{}
	for _, b := range builds {
		if id, ok := b["id"].(string); ok {
			buildIDs = append(buildIDs, id)
		}
	}

	return emit(rc, map[string]any{
		"operation":      "build",
		"build_platform": buildPlatform,
		"profile":        profile,
		"success":        true,
		"build_ids":      buildIDs,
		"builds":         builds,
		"message":        fmt.Sprintf("Build triggered for %s (%s profile)", buildPlatform, profile),
	})
}

// update publishes an OTA update.
func update(ctx context.Context, rc *runner.RunnerContext, channel, message string) error {
	if channel == "" {
		return fmt.Errorf("channel is required for update operation")
	}

	args := []string{"update", "--channel", channel, "--json", "--non-interactive"}
	if message != "" {
		args = append(args, "--message", message)
	}

	cmd := exec.CommandContext(ctx, "eas", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("eas update: %w", err)
	}

	// Parse JSON output
	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return emit(rc, map[string]any{
			"operation":  "update",
			"channel":    channel,
			"message":    message,
			"success":    true,
			"raw_output": string(output),
		})
	}

	updateID := ""
	if id, ok := result["id"].(string); ok {
		updateID = id
	}

	return emit(rc, map[string]any{
		"operation": "update",
		"channel":   channel,
		"message":   message,
		"success":   true,
		"update_id": updateID,
		"result":    result,
	})
}

// buildStatus checks EAS build status.
func buildStatus(ctx context.Context, rc *runner.RunnerContext) error {
	args := []string{"build:list", "--json", "--non-interactive", "--limit", "5"}

	cmd := exec.CommandContext(ctx, "eas", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("eas build:list: %w", err)
	}

	// Parse JSON output
	var builds []map[string]any
	if err := json.Unmarshal(output, &builds); err != nil {
		return emit(rc, map[string]any{
			"operation":  "build_status",
			"success":    true,
			"raw_output": string(output),
		})
	}

	// Summarize builds
	summary := []map[string]any{}
	for _, b := range builds {
		s := map[string]any{
			"id":       b["id"],
			"platform": b["platform"],
			"status":   b["status"],
		}
		if createdAt, ok := b["createdAt"].(string); ok {
			s["created_at"] = createdAt
		}
		if profile, ok := b["profile"].(string); ok {
			s["profile"] = profile
		}
		summary = append(summary, s)
	}

	return emit(rc, map[string]any{
		"operation": "build_status",
		"success":   true,
		"builds":    summary,
		"count":     len(builds),
		"message":   fmt.Sprintf("Found %d recent builds", len(builds)),
	})
}

// ============================================================================
// Logs
// ============================================================================

// logs retrieves Metro bundler logs.
func logs(ctx context.Context, rc *runner.RunnerContext, filter string, count int) error {
	if count <= 0 {
		count = 100
	}

	// Try to get logs from the running Metro process
	// First, check if Metro is running by looking for its log file or process

	// Method 1: Check for Expo CLI output in the terminal (best effort)
	// This is tricky since Metro runs interactively

	// Method 2: Use expo start --no-dev --minify to get recent logs
	// This isn't ideal either

	// Method 3: Check the Expo log directory
	homeDir, _ := os.UserHomeDir()
	logPaths := []string{
		homeDir + "/.expo/debug.log",
		".expo/debug.log",
	}

	var logContent []byte
	var logPath string
	for _, p := range logPaths {
		if content, err := os.ReadFile(p); err == nil {
			logContent = content
			logPath = p
			break
		}
	}

	if logContent == nil {
		// No log file found, provide instructions
		return emit(rc, map[string]any{
			"operation": "logs",
			"success":   false,
			"message":   "No Expo logs found. Start Metro with: npx expo start --clear",
			"hint":      "Logs are available at: ~/.expo/debug.log or .expo/debug.log",
		})
	}

	// Get last N lines
	lines := strings.Split(string(logContent), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

	// Apply filter if specified
	if filter != "" {
		var filtered []string
		filterLower := strings.ToLower(filter)
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), filterLower) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
	}

	output := strings.Join(lines, "\n")

	// Store in CAS if large
	if len(output) > 4096 {
		artifact, err := runner.PersistBuffer(ctx, rc, bytes.NewBufferString(output), "text/plain", "expo_logs")
		if err != nil {
			return fmt.Errorf("persist logs: %w", err)
		}

		return emit(rc, map[string]any{
			"operation":   "logs",
			"success":     true,
			"artifact":    artifact.Digest,
			"total_lines": len(lines),
			"source":      logPath,
			"hint":        "Full logs stored in CAS; fetch via: agentctl cas get " + artifact.Digest,
		})
	}

	return emit(rc, map[string]any{
		"operation":   "logs",
		"success":     true,
		"logs":        output,
		"total_lines": len(lines),
		"source":      logPath,
	})
}

// ============================================================================
// Helpers
// ============================================================================

// adbArgs builds ADB command arguments with optional serial.
func adbArgs(serial string, args ...string) []string {
	if serial != "" {
		return append([]string{"-s", serial}, args...)
	}
	return args
}
