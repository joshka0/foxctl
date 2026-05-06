// Package main provides the expo skill for unified Expo development operations.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/mobileutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const skillName = "mobile/expo"

var allowedOps = []string{
	"debug_status",
	"debug_snapshot",
	"shake",
	"reload",
	"deep_link",
	"dev_menu",
	"toggle_inspector",
	"toggle_performance",
	"toggle_remote_debug",
	"build",
	"update",
	"build_status",
	"logs",
}

// Input represents the skill input parameters for mobile/expo operations.
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

// main is the skill entry point for mobile/expo.
func main() {
	skillmain.Main(skillName, run)
}

// run orchestrates Expo development operations with device management and EAS integration.
//
// Index:
//   Purpose: Unified Expo development with device control, deep linking, EAS builds, and log retrieval
//   Flow: validate operation → detect platform → route to handler → emit operation-specific results
//   SideEffects: device interactions; ADB/IDB commands; EAS API calls; file system access
//   FailureModes: invalid operations, device not found, ADB/IDB failures, EAS errors, missing tools
//   Observability: emits operation status, platform info, build IDs, and detailed error messages
//   Related: detectPlatform, shake, reload, deepLink, build, update, logs
//   Keywords: mobile/expo, expo, react_native, device_control, eas_build, development
//
// [[domain:expo-mobile-development]]
// [[protocol:expo-device-control]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate input
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	// Detect platform if not specified
	platform := detectPlatform(in.DeviceID, in.Platform)

	switch op {
	case "debug_status":
		return debugStatus(ctx, rc, platform, in.DeviceID)
	case "debug_snapshot":
		return debugSnapshot(ctx, rc, platform, in.DeviceID, in.Filter, in.Count)
	// Device operations
	case "shake":
		return shake(ctx, rc, platform, in.DeviceID)
	case "reload":
		return reload(ctx, rc, platform, in.DeviceID)
	case "deep_link":
		return deepLink(ctx, rc, platform, in.DeviceID, in.URL)
	case "dev_menu":
		return devMenu(ctx, rc, platform, in.DeviceID)
	case "toggle_inspector":
		return toggleInspector(ctx, rc, platform, in.DeviceID)
	case "toggle_performance":
		return togglePerformance(ctx, rc, platform, in.DeviceID)
	case "toggle_remote_debug":
		return toggleRemoteDebug(ctx, rc, platform, in.DeviceID)
	// EAS operations
	case "build":
		return build(ctx, rc, in.BuildPlatform, in.Profile)
	case "update":
		return update(ctx, rc, in.Channel, in.Message)
	case "build_status":
		return buildStatus(ctx, rc)
	// Logs
	case "logs":
		return logs(ctx, rc, in.Filter, in.Count)
	default:
		return skillerr.Arg(fmt.Sprintf("unknown operation: %s", in.Operation), skillerr.WithHint(opHint))
	}
}

type expoPackageJSON struct {
	Name            string            `json:"name"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func metroProcessMatches(lines []string) []map[string]any {
	matches := make([]map[string]any, 0)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if !(strings.Contains(lower, "expo start") ||
			strings.Contains(lower, "@expo/cli") ||
			strings.Contains(lower, "react-native start") ||
			strings.Contains(lower, "metro")) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pid := fields[0]
		command := strings.TrimSpace(strings.TrimPrefix(line, pid))
		matches = append(matches, map[string]any{
			"pid":     pid,
			"command": command,
		})
	}
	return matches
}

func detectMetroProcesses(ctx context.Context) []map[string]any {
	result := executil.Run(ctx, "", "ps", "-axo", "pid=,command=")
	if result.Err != nil {
		return nil
	}
	return metroProcessMatches(strings.Split(string(result.Stdout), "\n"))
}

func detectExpoProjectInfo(workspace string) map[string]any {
	info := map[string]any{
		"workspace": workspace,
		"type":      "unknown",
	}
	pkgPath := filepath.Join(workspace, "package.json")
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return info
	}

	var pkg expoPackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		info["package_json"] = pkgPath
		info["package_json_parse_error"] = err.Error()
		return info
	}

	info["package_json"] = pkgPath
	if pkg.Name != "" {
		info["name"] = pkg.Name
	}

	hasExpo := pkg.Dependencies["expo"] != "" || pkg.DevDependencies["expo"] != ""
	hasRN := pkg.Dependencies["react-native"] != "" || pkg.DevDependencies["react-native"] != ""
	hasHermes := pkg.Dependencies["hermes-engine"] != "" || pkg.DevDependencies["hermes-engine"] != ""

	info["has_expo_dependency"] = hasExpo
	info["has_react_native_dependency"] = hasRN
	info["has_hermes_dependency"] = hasHermes

	switch {
	case hasExpo:
		info["type"] = "expo"
	case hasRN:
		info["type"] = "react-native"
	}

	return info
}

func expoLogPaths(workspace string) []string {
	homeDir, _ := os.UserHomeDir()
	paths := []string{}
	if homeDir != "" {
		paths = append(paths, filepath.Join(homeDir, ".expo", "debug.log"))
	}
	if workspace != "" {
		paths = append(paths, filepath.Join(workspace, ".expo", "debug.log"))
	}
	return paths
}

func inspectExpoLogSources(paths []string) []map[string]any {
	sources := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		src := map[string]any{"path": path, "exists": false}
		if path == "" {
			sources = append(sources, src)
			continue
		}
		info, err := os.Stat(path)
		if err == nil {
			src["exists"] = true
			src["size_bytes"] = info.Size()
			src["modified_at"] = info.ModTime().UTC().Format(time.RFC3339)
		}
		sources = append(sources, src)
	}
	return sources
}

func anyRecentLogSource(sources []map[string]any, now time.Time, window time.Duration) bool {
	for _, src := range sources {
		exists, _ := src["exists"].(bool)
		if !exists {
			continue
		}
		modifiedAt, _ := src["modified_at"].(string)
		if modifiedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, modifiedAt)
		if err != nil {
			continue
		}
		if now.Sub(ts) <= window {
			return true
		}
	}
	return false
}

func collectExpoLogs(workspace, filter string, count int) map[string]any {
	if count <= 0 {
		count = 100
	}
	paths := expoLogPaths(workspace)

	var logContent []byte
	var logPath string
	for _, p := range paths {
		if content, err := os.ReadFile(p); err == nil {
			logContent = content
			logPath = p
			break
		}
	}

	if logContent == nil {
		return map[string]any{
			"success": false,
			"message": "No Expo logs found. Start Metro with: npx expo start --clear",
			"hint":    "Logs are available at: ~/.expo/debug.log or .expo/debug.log",
			"sources": inspectExpoLogSources(paths),
		}
	}

	lines := strings.Split(string(logContent), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

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

	return map[string]any{
		"success":     true,
		"logs":        strings.Join(lines, "\n"),
		"total_lines": len(lines),
		"source":      logPath,
		"sources":     inspectExpoLogSources(paths),
	}
}

func metroStatus(ctx context.Context, workspace string) map[string]any {
	processes := detectMetroProcesses(ctx)
	logSources := inspectExpoLogSources(expoLogPaths(workspace))
	return map[string]any{
		"running_guess": anyRecentLogSource(logSources, time.Now().UTC(), 15*time.Minute) || len(processes) > 0,
		"processes":     processes,
		"log_sources":   logSources,
	}
}

// debugStatus reports whether the current workspace and machine are ready for modern Expo/React Native debugging.
func debugStatus(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
	workspace, _ := os.Getwd()
	logSources := inspectExpoLogSources(expoLogPaths(workspace))
	project := detectExpoProjectInfo(workspace)
	metro := metroStatus(ctx, workspace)

	tooling := map[string]any{
		"npx":    executil.HasTool("npx"),
		"expo":   executil.HasTool("expo"),
		"idb":    mobileutil.HasRunnableIDB(ctx),
		"simctl": executil.HasTool("xcrun"),
		"adb":    executil.HasTool("adb"),
	}

	return emit(rc, map[string]any{
		"operation":           "debug_status",
		"platform":            platform,
		"device_id":           deviceID,
		"workspace":           workspace,
		"project":             project,
		"tooling":             tooling,
		"metro":               metro,
		"log_sources":         logSources,
		"metro_running_guess": anyRecentLogSource(logSources, time.Now().UTC(), 15*time.Minute),
		"recommended_debugger": map[string]any{
			"name":      "React Native DevTools",
			"engine":    "Hermes",
			"open_hint": "For Expo apps, press 'j' in the terminal where Expo was started.",
		},
		"foxctl_paths": map[string]any{
			"expo_actions": []string{
				"debug_status",
				"logs",
				"deep_link",
				"dev_menu",
				"shake",
				"reload",
				"toggle_inspector",
				"toggle_performance",
				"toggle_remote_debug",
			},
			"ios_actions": []string{
				"list_devices",
				"launch",
				"terminate",
				"open_url",
				"screenshot",
				"ui_tree",
				"logs",
				"shake",
				"expo_deep_link",
				"expo_reload",
			},
		},
		"notes": []string{
			"Use React Native DevTools as the primary JS debugger path for Hermes-based apps.",
			"Use mobile/ios for simulator state, screenshots, UI tree inspection, and app lifecycle control.",
			"Use mobile/expo for Expo dev menu actions, reloads, and Metro-style log inspection.",
		},
	})
}

func debugSnapshot(ctx context.Context, rc *skillmain.RunContext, platform, deviceID, filter string, count int) error {
	workspace, _ := os.Getwd()
	logsData := collectExpoLogs(workspace, filter, count)
	project := detectExpoProjectInfo(workspace)
	metro := metroStatus(ctx, workspace)
	tooling := map[string]any{
		"npx":    executil.HasTool("npx"),
		"expo":   executil.HasTool("expo"),
		"idb":    mobileutil.HasRunnableIDB(ctx),
		"simctl": executil.HasTool("xcrun"),
		"adb":    executil.HasTool("adb"),
	}

	snapshot := map[string]any{
		"operation": "debug_snapshot",
		"platform":  platform,
		"device_id": deviceID,
		"workspace": workspace,
		"project":   project,
		"tooling":   tooling,
		"metro":     metro,
		"logs":      logsData,
		"recommended_debugger": map[string]any{
			"name":      "React Native DevTools",
			"engine":    "Hermes",
			"open_hint": "For Expo apps, press 'j' in the terminal where Expo was started.",
		},
	}

	if platform == "ios" {
		iosData := map[string]any{}
		if devices, err := mobileutil.ListSimctlDevices(ctx); err == nil {
			iosData["devices_count"] = len(devices)
		}
		if executil.HasTool("xcrun") {
			outputPath := fmt.Sprintf("/tmp/expo_debug_snapshot_%d.png", time.Now().UnixNano())
			defer func() { _ = os.Remove(outputPath) }()
			target := "booted"
			if strings.TrimSpace(deviceID) != "" {
				target = strings.TrimSpace(deviceID)
			}
			result := mobileutil.RunSimctl(ctx, "io", target, "screenshot", outputPath)
			if result.Err == nil {
				if data, err := os.ReadFile(outputPath); err == nil {
					if artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(data), "image/png", "expo_debug_snapshot"); err == nil {
						iosData["screenshot"] = artifact.Digest
						iosData["screenshot_backend"] = "simctl"
					}
				}
			} else {
				iosData["screenshot_error"] = result.Err.Error()
			}
		}
		if mobileutil.HasRunnableIDB(ctx) {
			cmdResult := mobileutil.RunIDB(ctx, deviceID, "ui", "describe-all", "--json")
			if cmdResult.Err == nil {
				var elements []any
				if err := json.Unmarshal(cmdResult.Stdout, &elements); err == nil {
					preview := elements
					truncated := false
					if len(preview) > 20 {
						preview = preview[:20]
						truncated = true
					}
					iosData["ui_tree_preview"] = preview
					iosData["ui_tree_count"] = len(elements)
					iosData["ui_tree_truncated"] = truncated
				}
			} else {
				iosData["ui_tree_error"] = cmdResult.Err.Error()
			}
		} else {
			iosData["ui_tree_unavailable"] = "idb unavailable"
		}
		snapshot["ios"] = iosData
	}

	return emit(rc, snapshot)
}

// emit outputs skill results with consistent formatting.
func emit(rc *skillmain.RunContext, data map[string]any) error {
	return skillout.Emit(rc, skillName, data)
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
func shake(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
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

// iosShake triggers shake gesture on iOS simulator using IDB or AppleScript.
func iosShake(ctx context.Context, udid string) error {
	// Method 1: Try sending keyboard shortcut via IDB (Cmd+D opens React Native dev menu)
	// First, focus the simulator
	if mobileutil.HasRunnableIDB(ctx) {
		_ = mobileutil.RunIDB(ctx, udid, "focus").Err // Ignore errors, just best effort
	}

	// Method 2: Use IDB to send key sequence that opens dev menu
	// Sending 'd' key typically opens dev menu in Expo apps
	keyResult := executil.CmdResult{}
	if mobileutil.HasRunnableIDB(ctx) {
		keyResult = mobileutil.RunIDB(ctx, udid, "ui", "text", "d")
	} else {
		keyResult.Err = errors.New("idb unavailable")
	}
	if keyResult.Err != nil {
		// Method 3: Fall back to native simulator shake support on current runtimes.
		simctlTarget := "booted"
		if strings.TrimSpace(udid) != "" {
			simctlTarget = strings.TrimSpace(udid)
		}
		simResult := mobileutil.RunSimctl(ctx, "io", simctlTarget, "shake")
		if simResult.Err == nil {
			return nil
		}

		// Method 4: Try AppleScript to trigger shake via menu (requires accessibility)
		script := `tell application "Simulator" to activate
	delay 0.3
	tell application "System Events"
		tell process "Simulator"
			click menu item "Shake" of menu "Device" of menu bar 1
		end tell
	end tell`
		appleResult := executil.Run(ctx, "", "osascript", "-e", script)
		if appleResult.Err != nil {
			return skillerr.Runtime("shake failed", skillerr.WithCause(errors.Join(keyResult.Err, simResult.Err, appleResult.Err)))
		}
	}
	return nil
}

// androidShake triggers shake gesture on Android device/emulator using ADB.
func androidShake(ctx context.Context, serial string) error {
	// Android: Send accelerometer event to simulate shake
	// Using input command to send key events that trigger shake detection
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "keyevent", "82") // KEYCODE_MENU opens dev menu
	return result.Err
}

// reload triggers a JS bundle reload.
func reload(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
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

// iosReload triggers reload on iOS simulator via dev menu.
func iosReload(ctx context.Context, udid string) error {
	// Shake to open dev menu
	if err := iosShake(ctx, udid); err != nil {
		return skillerr.Runtime("shake for reload", skillerr.WithCause(err))
	}

	time.Sleep(500 * time.Millisecond)

	// Type 'r' for reload
	result := mobileutil.RunIDB(ctx, udid, "ui", "text", "r")
	return result.Err
}

// androidReload triggers reload on Android device/emulator via dev menu.
func androidReload(ctx context.Context, serial string) error {
	// Open dev menu first
	if err := androidShake(ctx, serial); err != nil {
		return skillerr.Runtime("open dev menu for reload", skillerr.WithCause(err))
	}

	time.Sleep(500 * time.Millisecond)

	// Send 'r' key for reload (double-tap R in Expo)
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "text", "rr")
	return result.Err
}

// deepLink opens an Expo deep link URL.
func deepLink(ctx context.Context, rc *skillmain.RunContext, platform, deviceID, url string) error {
	if url == "" {
		return skillerr.Arg("url is required for deep_link operation")
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

// iosDeepLink opens deep link on iOS simulator using simctl first with IDB fallback.
func iosDeepLink(ctx context.Context, udid, url string) error {
	simctlTarget := "booted"
	if strings.TrimSpace(udid) != "" {
		simctlTarget = strings.TrimSpace(udid)
	}
	result := mobileutil.RunSimctl(ctx, "openurl", simctlTarget, url)
	if result.Err == nil {
		return nil
	}
	simctlErr := result.Err
	if !mobileutil.HasRunnableIDB(ctx) {
		return skillerr.Runtime("open deep link failed", skillerr.WithCause(simctlErr))
	}
	result = mobileutil.RunIDB(ctx, udid, "open", url)
	if result.Err != nil {
		return skillerr.Runtime("open deep link failed", skillerr.WithCause(errors.Join(simctlErr, result.Err)))
	}
	return nil
}

// androidDeepLink opens deep link on Android device/emulator using ADB.
func androidDeepLink(ctx context.Context, serial, url string) error {
	result := mobileutil.RunADB(ctx, serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	return result.Err
}

// devMenu opens the Expo developer menu.
func devMenu(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
	// Same as shake - opens the dev menu
	return shake(ctx, rc, platform, deviceID)
}

// toggleInspector toggles the React element inspector.
func toggleInspector(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
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
func togglePerformance(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
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
func toggleRemoteDebug(ctx context.Context, rc *skillmain.RunContext, platform, deviceID string) error {
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

// iosToggleDevOption toggles a dev menu option on iOS simulator.
func iosToggleDevOption(ctx context.Context, udid, key string) error {
	// Open dev menu
	if err := iosShake(ctx, udid); err != nil {
		return skillerr.Runtime("open dev menu", skillerr.WithCause(err))
	}

	time.Sleep(500 * time.Millisecond)

	// Type the key
	result := mobileutil.RunIDB(ctx, udid, "ui", "text", key)
	return result.Err
}

// androidToggleDevOption toggles a dev menu option on Android device/emulator.
func androidToggleDevOption(ctx context.Context, serial, key string) error {
	// Open dev menu
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "keyevent", "82")
	if result.Err != nil {
		return skillerr.Runtime("open dev menu", skillerr.WithCause(result.Err))
	}

	time.Sleep(500 * time.Millisecond)

	// Type the key
	result = mobileutil.RunADB(ctx, serial, "shell", "input", "text", key)
	return result.Err
}

// ============================================================================
// EAS Operations
// ============================================================================

// build triggers an EAS cloud build.
func build(ctx context.Context, rc *skillmain.RunContext, buildPlatform, profile string) error {
	if buildPlatform == "" {
		buildPlatform = "all"
	}
	if profile == "" {
		profile = "development"
	}

	args := []string{"build", "--platform", buildPlatform, "--profile", profile, "--json", "--non-interactive"}

	result := executil.Run(ctx, "", "eas", args...)
	if result.Err != nil {
		return skillerr.Integration("eas build", skillerr.WithCause(result.Err))
	}

	// Parse JSON output
	var builds []map[string]any
	if err := json.Unmarshal(result.Stdout, &builds); err != nil {
		// Try single object
		var build map[string]any
		if err := json.Unmarshal(result.Stdout, &build); err != nil {
			return emit(rc, map[string]any{
				"operation":      "build",
				"build_platform": buildPlatform,
				"profile":        profile,
				"success":        true,
				"raw_output":     string(result.Stdout),
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
func update(ctx context.Context, rc *skillmain.RunContext, channel, message string) error {
	if channel == "" {
		return skillerr.Arg("channel is required for update operation")
	}

	args := []string{"update", "--channel", channel, "--json", "--non-interactive"}
	if message != "" {
		args = append(args, "--message", message)
	}

	cmdResult := executil.Run(ctx, "", "eas", args...)
	if cmdResult.Err != nil {
		return skillerr.Integration("eas update", skillerr.WithCause(cmdResult.Err))
	}

	// Parse JSON output
	var result map[string]any
	if err := json.Unmarshal(cmdResult.Stdout, &result); err != nil {
		return emit(rc, map[string]any{
			"operation":  "update",
			"channel":    channel,
			"message":    message,
			"success":    true,
			"raw_output": string(cmdResult.Stdout),
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
func buildStatus(ctx context.Context, rc *skillmain.RunContext) error {
	args := []string{"build:list", "--json", "--non-interactive", "--limit", "5"}

	result := executil.Run(ctx, "", "eas", args...)
	if result.Err != nil {
		return skillerr.Integration("eas build:list", skillerr.WithCause(result.Err))
	}

	// Parse JSON output
	var builds []map[string]any
	if err := json.Unmarshal(result.Stdout, &builds); err != nil {
		return emit(rc, map[string]any{
			"operation":  "build_status",
			"success":    true,
			"raw_output": string(result.Stdout),
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
func logs(ctx context.Context, rc *skillmain.RunContext, filter string, count int) error {
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
		artifact, err := skillmain.PersistBuffer(ctx, rc, bytes.NewBufferString(output), "text/plain", "expo_logs")
		if err != nil {
			return skillerr.IO("persist logs", skillerr.WithCause(err))
		}

		return emit(rc, map[string]any{
			"operation":   "logs",
			"success":     true,
			"artifact":    artifact.Digest,
			"total_lines": len(lines),
			"source":      logPath,
			"hint":        skillout.FormatCASHint("logs", artifact.Digest),
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
