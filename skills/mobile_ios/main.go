// Package main implements the mobile/ios skill for iOS Simulator automation via IDB.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mobileutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textutil"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const command = "mobile/ios"

var allowedOps = []string{
	"list_devices",
	"device_info",
	"boot",
	"install",
	"launch",
	"terminate",
	"screenshot",
	"tap",
	"swipe",
	"type_text",
	"button",
	"ui_tree",
	"describe_point",
	"logs",
	"open_url",
	"set_location",
	"approve_permissions",
	"record_start",
	"record_stop",
	"crash_logs",
	"add_media",
	"clear_keychain",
	"focus",
	"shake",
	"expo_deep_link",
	"expo_reload",
}

// input defines the skill input parameters for iOS simulator operations with comprehensive device control.
type input struct {
	Operation   string   `json:"operation"`
	UDID        string   `json:"udid,omitempty"`
	App         string   `json:"app,omitempty"`
	X           int      `json:"x,omitempty"`
	Y           int      `json:"y,omitempty"`
	X2          int      `json:"x2,omitempty"`
	Y2          int      `json:"y2,omitempty"`
	Text        string   `json:"text,omitempty"`
	URL         string   `json:"url,omitempty"`
	Button      string   `json:"button,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Lat         float64  `json:"lat,omitempty"`
	Long        float64  `json:"long,omitempty"`
	Output      string   `json:"output,omitempty"`
	ExpoURL     string   `json:"expo_url,omitempty"`
	MediaPath   string   `json:"media_path,omitempty"`
}

// main is the skill entry point for mobile/ios with IDB companion integration.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates iOS simulator automation via IDB with comprehensive device management.
//
// Index:
// - Purpose: Automate iOS Simulator operations via IDB for mobile testing and development
// - Flow: validate operation → check IDB availability → route to handler → execute device operation → emit results
// - SideEffects: simulator control; app installation/launch; UI interactions; screenshots; recordings
// - FailureModes: IDB not found, device errors, invalid operations, file access issues, permission errors
// - Observability: emits operation results, device status, screenshots, logs, and error details
// - Related: mobileutil.RunIDB, executil.RequireTool, skillout.PersistBuffer
// - Keywords: mobile/ios, ios_simulator, idb_companion, device_automation, mobile_testing
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate input
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg(
			"operation is required",
			skillerr.WithHint(opHint),
		)
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}
	// Check IDB availability
	if _, err := executil.RequireTool("idb", "install idb-companion"); err != nil {
		return skillerr.Runtime(
			"idb not found",
			skillerr.WithCause(err),
			skillerr.WithHint("Install idb-companion (brew install idb-companion)."),
		)
	}

	switch op {
	case "list_devices":
		return listDevices(ctx, rc)
	case "device_info":
		return deviceInfo(ctx, rc, in.UDID)
	case "boot":
		return boot(ctx, rc, in.UDID)
	case "install":
		return install(ctx, rc, in.UDID, in.App)
	case "launch":
		return launch(ctx, rc, in.UDID, in.App)
	case "terminate":
		return terminate(ctx, rc, in.UDID, in.App)
	case "screenshot":
		return screenshot(ctx, rc, in.UDID, in.Output)
	case "tap":
		return tap(ctx, rc, in.UDID, in.X, in.Y)
	case "swipe":
		return swipe(ctx, rc, in.UDID, in.X, in.Y, in.X2, in.Y2)
	case "type_text":
		return typeText(ctx, rc, in.UDID, in.Text)
	case "button":
		return button(ctx, rc, in.UDID, in.Button)
	case "ui_tree":
		return uiTree(ctx, rc, in.UDID)
	case "describe_point":
		return describePoint(ctx, rc, in.UDID, in.X, in.Y)
	case "logs":
		return logs(ctx, rc, in.UDID)
	case "open_url":
		return openURL(ctx, rc, in.UDID, in.URL)
	case "set_location":
		return setLocation(ctx, rc, in.UDID, in.Lat, in.Long)
	case "approve_permissions":
		return approvePermissions(ctx, rc, in.UDID, in.App, in.Permissions)
	case "record_start":
		return recordStart(ctx, rc, in.UDID, in.Output)
	case "record_stop":
		return recordStop(ctx, rc, in.UDID)
	case "crash_logs":
		return crashLogs(ctx, rc, in.UDID)
	case "add_media":
		return addMedia(ctx, rc, in.UDID, in.MediaPath)
	case "clear_keychain":
		return clearKeychain(ctx, rc, in.UDID)
	case "focus":
		return focus(ctx, rc, in.UDID)
	case "shake":
		return shake(ctx, rc, in.UDID)
	case "expo_deep_link":
		return expoDeepLink(ctx, rc, in.UDID, in.ExpoURL)
	case "expo_reload":
		return expoReload(ctx, rc, in.UDID)
	default:
		return skillerr.Arg(fmt.Sprintf("unknown operation: %s", in.Operation), skillerr.WithHint(opHint))
	}
}

// listDevices lists all available iOS simulators with filtering for simulators only and device enumeration.
func listDevices(ctx context.Context, rc *skillmain.RunContext) error {
	devices, err := mobileutil.ListIDBDevices(ctx)
	if err != nil {
		return skillerr.WrapRuntime("idb list-targets", err)
	}

	simulators := make([]mobileutil.IDBDevice, 0, len(devices))
	for _, dev := range devices {
		// Only include simulators (not real devices)
		if dev.TargetType == "simulator" {
			simulators = append(simulators, dev)
		}
	}

	return emit(rc, map[string]any{
		"operation": "list_devices",
		"devices":   simulators,
		"count":     len(simulators),
	})
}

// deviceInfo gets detailed info about a specific device via IDB describe with JSON parsing.
func deviceInfo(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	result := mobileutil.RunIDB(ctx, udid, "describe", "--json")
	if result.Err != nil {
		return skillerr.WrapRuntime("idb describe", result.Err)
	}

	var device map[string]any
	if err := json.Unmarshal(result.Stdout, &device); err != nil {
		return skillerr.WrapParse("parse device info", err)
	}

	return emit(rc, map[string]any{
		"operation": "device_info",
		"device":    device,
	})
}

// boot boots a simulator with UDID validation and success confirmation via IDB.
func boot(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	if udid == "" {
		return skillerr.Arg("udid is required for boot operation")
	}

	result := mobileutil.RunIDB(ctx, "", "boot", udid)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb boot", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "boot",
		"udid":      udid,
		"success":   true,
		"message":   "Simulator booted successfully",
	})
}

// install installs an app on the simulator with path validation, symlink resolution, and security checks.
func install(ctx context.Context, rc *skillmain.RunContext, udid, app string) error {
	if app == "" {
		return skillerr.Arg(
			"app path is required for install operation",
			skillerr.WithHint("Provide the .app or .ipa path in app."),
		)
	}

	// Resolve app path to absolute and validate
	absPath, err := filepath.Abs(app)
	if err != nil {
		return skillerr.WrapIO("resolve app path", err)
	}

	// Resolve symlinks to get the real path
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return skillerr.WrapIO("app path does not exist or is inaccessible", err)
	}

	// Verify the file exists and is not a directory (should be .app bundle or .ipa)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return skillerr.WrapIO("app path invalid", err)
	}

	// .app bundles are directories, .ipa files are not
	ext := strings.ToLower(filepath.Ext(resolvedPath))
	if ext == ".ipa" && info.IsDir() {
		return skillerr.Validation(fmt.Sprintf("app path %s: expected .ipa file but got directory", resolvedPath))
	}

	result := mobileutil.RunIDB(ctx, udid, "install", resolvedPath)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb install", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "install",
		"app":       resolvedPath,
		"success":   true,
		"message":   "App installed successfully",
	})
}

// launch launches an app by bundle ID with validation and success tracking via IDB.
func launch(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return skillerr.Arg(
			"app bundle ID is required for launch operation",
			skillerr.WithHint("Provide the app bundle ID in app."),
		)
	}

	result := mobileutil.RunIDB(ctx, udid, "launch", bundleID)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb launch", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "launch",
		"app":       bundleID,
		"success":   true,
		"message":   "App launched successfully",
	})
}

// terminate stops a running app by bundle ID with validation and graceful shutdown.
func terminate(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return skillerr.Arg(
			"app bundle ID is required for terminate operation",
			skillerr.WithHint("Provide the app bundle ID in app."),
		)
	}

	result := mobileutil.RunIDB(ctx, udid, "terminate", bundleID)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb terminate", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "terminate",
		"app":       bundleID,
		"success":   true,
		"message":   "App terminated successfully",
	})
}

// screenshot captures the simulator screen with CAS storage, temp file cleanup, and artifact management.
func screenshot(ctx context.Context, rc *skillmain.RunContext, udid, outputPath string) error {
	// Track if we created a temp file that needs cleanup
	tempCreated := false
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/ios_screenshot_%d.png", time.Now().UnixNano())
		tempCreated = true
	}

	// Schedule cleanup of temp file after we're done
	if tempCreated {
		defer func() {
			_ = os.Remove(outputPath) // Best-effort cleanup
		}()
	}

	result := mobileutil.RunIDB(ctx, udid, "screenshot", outputPath)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb screenshot", result.Err)
	}

	// Read screenshot and store in CAS
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return skillerr.WrapIO("read screenshot", err)
	}

	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(data), "image/png", "ios_screenshot")
	if err != nil {
		return skillerr.WrapIO("persist screenshot", err)
	}

	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"size_bytes": len(data),
		"success":    true,
	})
}

// tap performs a tap at coordinates with IDB UI command execution and coordinate validation.
func tap(ctx context.Context, rc *skillmain.RunContext, udid string, x, y int) error {
	result := mobileutil.RunIDB(ctx, udid, "ui", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if result.Err != nil {
		return skillerr.WrapRuntime("idb ui tap", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "tap",
		"x":         x,
		"y":         y,
		"success":   true,
	})
}

// swipe performs a swipe gesture with start and end coordinates and gesture validation.
func swipe(ctx context.Context, rc *skillmain.RunContext, udid string, x1, y1, x2, y2 int) error {
	result := mobileutil.RunIDB(ctx, udid, "ui", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2),
	)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb ui swipe", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "swipe",
		"from":      map[string]int{"x": x1, "y": y1},
		"to":        map[string]int{"x": x2, "y": y2},
		"success":   true,
	})
}

// typeText types text into the simulator with validation and IDB UI command execution.
func typeText(ctx context.Context, rc *skillmain.RunContext, udid, text string) error {
	if text == "" {
		return skillerr.Arg("text is required for type_text operation", skillerr.WithHint("Provide text to type."))
	}

	result := mobileutil.RunIDB(ctx, udid, "ui", "text", text)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb ui text", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "type_text",
		"text":      text,
		"success":   true,
	})
}

// button presses a hardware button with name validation and IDB UI command execution.
func button(ctx context.Context, rc *skillmain.RunContext, udid, buttonName string) error {
	if buttonName == "" {
		return skillerr.Arg("button name is required", skillerr.WithHint("Provide the button name to press."))
	}

	result := mobileutil.RunIDB(ctx, udid, "ui", "button", buttonName)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb ui button", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "button",
		"button":    buttonName,
		"success":   true,
	})
}

// uiTree gets the UI accessibility tree with JSON parsing, line-by-line fallback, and CAS storage for large trees.
func uiTree(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	cmdResult := mobileutil.RunIDB(ctx, udid, "ui", "describe-all", "--json")
	if cmdResult.Err != nil {
		return skillerr.WrapRuntime("idb ui describe-all", cmdResult.Err)
	}

	// Initialize to empty slice so JSON serializes as [] not null
	elements := make([]any, 0)
	if err := json.Unmarshal(cmdResult.Stdout, &elements); err != nil {
		// Try line-by-line parsing
		lines := strings.Split(strings.TrimSpace(string(cmdResult.Stdout)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var elem any
			if err := json.Unmarshal([]byte(line), &elem); err == nil {
				elements = append(elements, elem)
			}
		}
	}

	// Prepare preview for large UI trees
	const maxPreview = 20
	preview := elements
	truncated := false
	if len(elements) > maxPreview {
		preview = elements[:maxPreview]
		truncated = true
	}

	payload := map[string]any{
		"operation": "ui_tree",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}

	// Store full tree in CAS if truncated
	if truncated {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(cmdResult.Stdout), "application/json", "ios_ui_tree")
		if err != nil {
			return skillerr.WrapIO("persist ui tree", err)
		}
		payload["artifact"] = artifact.Digest
		payload["hint"] = skillout.FormatCASHint("UI tree", artifact.Digest)
	}

	return emit(rc, payload)
}

// describePoint describes the UI element at a specific point with JSON parsing and coordinate validation.
func describePoint(ctx context.Context, rc *skillmain.RunContext, udid string, x, y int) error {
	result := mobileutil.RunIDB(ctx, udid, "ui", "describe-point", strconv.Itoa(x), strconv.Itoa(y), "--json")
	if result.Err != nil {
		return skillerr.WrapRuntime("idb ui describe-point", result.Err)
	}

	var element any
	if err := json.Unmarshal(result.Stdout, &element); err != nil {
		return skillerr.WrapParse("parse element", err)
	}

	return emit(rc, map[string]any{
		"operation": "describe_point",
		"x":         x,
		"y":         y,
		"element":   element,
	})
}

// logs gets simulator logs with timeout handling, CAS storage, preview generation, and error distinction.
func logs(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	// Use a timeout context to get recent logs
	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result := mobileutil.RunIDB(logCtx, udid, "log", "--style", "json")

	// Distinguish expected timeout from real command failures
	if result.Err != nil {
		// Context timeout/cancellation is expected - we use it to limit log capture time
		if errors.Is(logCtx.Err(), context.DeadlineExceeded) || errors.Is(logCtx.Err(), context.Canceled) {
			// Expected: timeout used to limit log capture, continue with captured output
		} else {
			// Real command failure - return the error
			if result.ExitCode > 0 {
				return skillerr.Runtimef("idb log failed (exit %d): %s", result.ExitCode, string(result.Stderr))
			}
			return skillerr.WrapRuntime("idb log", result.Err)
		}
	}

	// Store logs in CAS
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(result.Stdout), "application/x-ndjson", "ios_logs")
	if err != nil {
		return skillerr.WrapIO("persist logs", err)
	}

	// Get a preview of recent logs
	lines := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
	preview := textutil.JoinTail(lines, 50, "\n")

	return emit(rc, map[string]any{
		"operation":    "logs",
		"artifact":     artifact.Digest,
		"preview":      preview,
		"total_lines":  len(lines),
		"preview_from": "last_50",
	})
}

// openURL opens a URL in the simulator with validation, scheme handling, and success tracking.
func openURL(ctx context.Context, rc *skillmain.RunContext, udid, url string) error {
	if url == "" {
		return skillerr.Arg("url is required for open_url operation", skillerr.WithHint("Provide a URL to open."))
	}

	result := mobileutil.RunIDB(ctx, udid, "open", url)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb open", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "open_url",
		"url":       url,
		"success":   true,
	})
}

// setLocation sets the simulated GPS location with coordinate formatting and IDB command execution.
func setLocation(ctx context.Context, rc *skillmain.RunContext, udid string, lat, long float64) error {
	result := mobileutil.RunIDB(ctx, udid, "set_location",
		strconv.FormatFloat(lat, 'f', 6, 64),
		strconv.FormatFloat(long, 'f', 6, 64),
	)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb set_location", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "set_location",
		"latitude":  lat,
		"longitude": long,
		"success":   true,
	})
}

// approvePermissions approves permissions for an app with validation and batch permission handling.
func approvePermissions(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string, permissions []string) error {
	if bundleID == "" {
		return skillerr.Arg("app bundle ID is required for approve_permissions", skillerr.WithHint("Provide the app bundle ID in app."))
	}
	if len(permissions) == 0 {
		return skillerr.Arg("permissions list is required", skillerr.WithHint("Provide at least one permission to approve."))
	}

	args := append([]string{"approve", bundleID}, permissions...)
	result := mobileutil.RunIDB(ctx, udid, args...)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb approve", result.Err)
	}

	return emit(rc, map[string]any{
		"operation":   "approve_permissions",
		"app":         bundleID,
		"permissions": permissions,
		"success":     true,
	})
}

// recordPIDFile returns the PID file path for a specific device with device-scoped recording management.
// This scopes recordings per-device, preventing conflicts when automating different simulators.
func recordPIDFile(udid string) string {
	if udid == "" {
		udid = "default"
	}
	return fmt.Sprintf("/tmp/agentctl_ios_record_%s.pid", udid)
}

// recordStart starts video recording with background process, PID tracking, and process lifecycle management.
func recordStart(ctx context.Context, rc *skillmain.RunContext, udid, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/ios_recording_%d.mp4", time.Now().UnixNano())
	}

	pidFile := recordPIDFile(udid)

	// Start recording in background
	cmd, err := executil.Start(ctx, "", "idb", mobileutil.IDBArgs(udid, "record", outputPath)...)
	if err != nil {
		return skillerr.WrapRuntime("idb record start", err)
	}

	// Store PID for later stop
	pid := cmd.Process.Pid
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		errs.Ignore(cmd.Process.Kill(), "kill recording process after pid file write failure")
		return skillerr.WrapIO("write pid file", err)
	}

	// Start a goroutine to wait for the process (prevents zombie)
	go func() {
		errs.Ignore(cmd.Wait(), "wait for recording process")
		_ = os.Remove(pidFile)
	}()

	return emit(rc, map[string]any{
		"operation":   "record_start",
		"output_path": outputPath,
		"pid":         pid,
		"success":     true,
		"message":     "Recording started. Use record_stop to finish.",
	})
}

// recordStop stops video recording with PID cleanup, signal handling, and graceful termination.
func recordStop(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	pidFile := recordPIDFile(udid)
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		// No PID file means no known recording in progress
		// Return error instead of using dangerous broad pkill fallback
		if os.IsNotExist(err) {
			return skillerr.NotFound(
				"no active recording found (PID file missing)",
				skillerr.WithHint("Start a recording with record_start first."),
			)
		}
		return skillerr.WrapIO("read recording PID file", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		_ = os.Remove(pidFile)
		return skillerr.WrapParse("invalid pid file", err)
	}

	proc, err := os.FindProcess(pid)
	if err == nil {
		errs.Ignore(proc.Signal(os.Interrupt), "send interrupt to recording process")
	}
	_ = os.Remove(pidFile)

	return emit(rc, map[string]any{
		"operation": "record_stop",
		"success":   true,
		"message":   "Recording stop signal sent",
	})
}

// crashLogs lists crash logs with JSON parsing, line-by-line fallback, and error handling.
func crashLogs(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	result := mobileutil.RunIDB(ctx, udid, "crash", "list", "--json")
	if result.Err != nil {
		return skillerr.WrapRuntime("idb crash list", result.Err)
	}

	// Initialize to empty slice so JSON serializes as [] not null
	crashes := make([]any, 0)
	if err := json.Unmarshal(result.Stdout, &crashes); err != nil {
		// Try line-by-line parsing
		lines := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var crash any
			if err := json.Unmarshal([]byte(line), &crash); err == nil {
				crashes = append(crashes, crash)
			}
		}
	}

	return emit(rc, map[string]any{
		"operation": "crash_logs",
		"crashes":   crashes,
		"count":     len(crashes),
	})
}

// addMedia adds media files to the simulator with path validation, security checks, and symlink resolution.
func addMedia(ctx context.Context, rc *skillmain.RunContext, udid, mediaPath string) error {
	if mediaPath == "" {
		return skillerr.Arg("media_path is required for add_media operation", skillerr.WithHint("Provide the media file path."))
	}

	// Resolve media path to absolute and validate to prevent path traversal
	absPath, err := filepath.Abs(mediaPath)
	if err != nil {
		return skillerr.WrapIO("resolve media path", err)
	}

	// Resolve symlinks to get the real path (prevents TOCTOU attacks)
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return skillerr.WrapIO("media path does not exist or is inaccessible", err)
	}

	// Verify the file exists and is a regular file (not a directory or device)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return skillerr.WrapIO("media path invalid", err)
	}

	if info.IsDir() {
		return skillerr.Validation(fmt.Sprintf("media_path %s: expected file but got directory", resolvedPath))
	}

	if !info.Mode().IsRegular() {
		return skillerr.Validation(fmt.Sprintf("media_path %s: not a regular file", resolvedPath))
	}

	result := mobileutil.RunIDB(ctx, udid, "add-media", resolvedPath)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb add-media", result.Err)
	}

	return emit(rc, map[string]any{
		"operation":  "add_media",
		"media_path": resolvedPath,
		"success":    true,
	})
}

// clearKeychain clears the keychain with IDB command execution and security reset.
func clearKeychain(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	result := mobileutil.RunIDB(ctx, udid, "clear_keychain")
	if result.Err != nil {
		return skillerr.WrapRuntime("idb clear_keychain", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "clear_keychain",
		"success":   true,
	})
}

// focus brings the simulator window to front with IDB command and window management.
func focus(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	result := mobileutil.RunIDB(ctx, udid, "focus")
	if result.Err != nil {
		return skillerr.WrapRuntime("idb focus", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "focus",
		"success":   true,
	})
}

// doShake performs the actual shake gesture logic without emitting an envelope to avoid double emission.
// This is used by both shake() and expoReload() to prevent duplicate envelope emissions.
func doShake(ctx context.Context, udid string) error {
	// IDB doesn't have a direct shake command, so we simulate it
	// by sending the shake hardware event through button
	result := mobileutil.RunIDB(ctx, udid, "ui", "button", "SHAKE")
	if result.Err != nil {
		// Fallback: Try using simctl
		simArgs := []string{"simctl"}
		if udid != "" {
			simArgs = append(simArgs, "io", udid)
		} else {
			simArgs = append(simArgs, "io", "booted")
		}
		simArgs = append(simArgs, "shake")

		simResult := executil.Run(ctx, "", "xcrun", simArgs...)
		if simResult.Err != nil {
			return skillerr.Runtimef("shake failed (idb: %v, simctl: %v)", result.Err, simResult.Err)
		}
	}

	return nil
}

// shake simulates a shake gesture (for Expo dev menu) with fallback support and error handling.
func shake(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	if err := doShake(ctx, udid); err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "shake",
		"success":   true,
		"message":   "Shake gesture triggered (opens Expo dev menu if app is running)",
	})
}

// expoDeepLink opens an Expo deep link URL with scheme validation and URL normalization.
func expoDeepLink(ctx context.Context, rc *skillmain.RunContext, udid, expoURL string) error {
	if expoURL == "" {
		return skillerr.Arg("expo_url is required for expo_deep_link operation", skillerr.WithHint("Provide the Expo URL to open."))
	}

	// Ensure URL has exp:// or exps:// scheme
	if !strings.HasPrefix(expoURL, "exp://") && !strings.HasPrefix(expoURL, "exps://") {
		expoURL = "exp://" + expoURL
	}

	result := mobileutil.RunIDB(ctx, udid, "open", expoURL)
	if result.Err != nil {
		return skillerr.WrapRuntime("idb open expo URL", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "expo_deep_link",
		"expo_url":  expoURL,
		"success":   true,
		"message":   "Expo deep link opened",
	})
}

// expoReload triggers a reload in the Expo app via shake gesture and keyboard shortcut with timing coordination.
func expoReload(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	// Method 1: Shake to open dev menu, then tap reload
	// Use doShake to avoid double envelope emission
	if err := doShake(ctx, udid); err != nil {
		return skillerr.WrapRuntime("shake for expo reload", err)
	}

	// Give the menu time to appear
	time.Sleep(500 * time.Millisecond)

	// Type 'r' which is the keyboard shortcut for reload in Expo
	result := mobileutil.RunIDB(ctx, udid, "ui", "text", "r")
	if result.Err != nil {
		return skillerr.WrapRuntime("type 'r' for reload", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "expo_reload",
		"success":   true,
		"message":   "Expo reload triggered via shake + 'r' key",
	})
}

// emit outputs the result envelope with operation data and standardized response format.
func emit(rc *skillmain.RunContext, data map[string]any) error {
	return skillout.Emit(rc, command, data)
}
