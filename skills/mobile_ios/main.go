// Package main implements the mobile/ios skill for iOS Simulator automation via IDB.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const skillName = "mobile/ios"

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

// IDBDevice represents a device from idb list-targets.
type IDBDevice struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	State      string `json:"state"`
	TargetType string `json:"type"`
	OSVersion  string `json:"os_version"`
	Arch       string `json:"architecture"`
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

	if err := run(ctx, rc, in); err != nil {
		fail("ERUNTIME", err)
	}
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}

	if in.Operation == "" {
		return input{}, fmt.Errorf("operation is required")
	}

	return in, nil
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Check IDB availability
	if _, err := exec.LookPath("idb"); err != nil {
		return fmt.Errorf("idb not found: install with 'brew install idb-companion'")
	}

	switch in.Operation {
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
		return recordStop(ctx, rc)
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
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}
}

// listDevices lists all available iOS simulators.
func listDevices(ctx context.Context, rc *runner.RunnerContext) error {
	cmd := exec.CommandContext(ctx, "idb", "list-targets", "--json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb list-targets: %w", err)
	}

	// IDB outputs one JSON object per line
	// Initialize to empty slice so JSON serializes as [] not null
	devices := make([]IDBDevice, 0)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var dev IDBDevice
		if err := json.Unmarshal([]byte(line), &dev); err != nil {
			continue // Skip malformed lines
		}
		// Only include simulators (not real devices)
		if dev.TargetType == "simulator" {
			devices = append(devices, dev)
		}
	}

	return emit(rc, map[string]any{
		"operation": "list_devices",
		"devices":   devices,
		"count":     len(devices),
	})
}

// deviceInfo gets detailed info about a specific device.
func deviceInfo(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"describe", "--json"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb describe: %w", err)
	}

	var device map[string]any
	if err := json.Unmarshal(output, &device); err != nil {
		return fmt.Errorf("parse device info: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "device_info",
		"device":    device,
	})
}

// boot boots a simulator.
func boot(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	if udid == "" {
		return fmt.Errorf("udid is required for boot operation")
	}

	cmd := exec.CommandContext(ctx, "idb", "boot", udid)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb boot: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "boot",
		"udid":      udid,
		"success":   true,
		"message":   "Simulator booted successfully",
	})
}

// install installs an app on the simulator.
func install(ctx context.Context, rc *runner.RunnerContext, udid, app string) error {
	if app == "" {
		return fmt.Errorf("app path is required for install operation")
	}

	// Resolve app path to absolute and validate
	absPath, err := filepath.Abs(app)
	if err != nil {
		return fmt.Errorf("resolve app path: %w", err)
	}

	// Resolve symlinks to get the real path
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("app path does not exist or is inaccessible: %w", err)
	}

	// Verify the file exists and is not a directory (should be .app bundle or .ipa)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Errorf("app path invalid: %w", err)
	}

	// .app bundles are directories, .ipa files are not
	ext := strings.ToLower(filepath.Ext(resolvedPath))
	if ext == ".ipa" && info.IsDir() {
		return fmt.Errorf("app path %s: expected .ipa file but got directory", resolvedPath)
	}

	args := []string{"install", resolvedPath}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb install: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "install",
		"app":       resolvedPath,
		"success":   true,
		"message":   "App installed successfully",
	})
}

// launch launches an app by bundle ID.
func launch(ctx context.Context, rc *runner.RunnerContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("app bundle ID is required for launch operation")
	}

	args := []string{"launch", bundleID}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb launch: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "launch",
		"app":       bundleID,
		"success":   true,
		"message":   "App launched successfully",
	})
}

// terminate stops a running app.
func terminate(ctx context.Context, rc *runner.RunnerContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("app bundle ID is required for terminate operation")
	}

	args := []string{"terminate", bundleID}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb terminate: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "terminate",
		"app":       bundleID,
		"success":   true,
		"message":   "App terminated successfully",
	})
}

// screenshot captures the simulator screen.
func screenshot(ctx context.Context, rc *runner.RunnerContext, udid, outputPath string) error {
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

	args := []string{"screenshot", outputPath}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb screenshot: %w", err)
	}

	// Read screenshot and store in CAS
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read screenshot: %w", err)
	}

	artifact, err := runner.PersistBuffer(ctx, rc, bytes.NewBuffer(data), "image/png", "ios_screenshot")
	if err != nil {
		return fmt.Errorf("persist screenshot: %w", err)
	}

	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"size_bytes": len(data),
		"success":    true,
	})
}

// tap performs a tap at coordinates.
func tap(ctx context.Context, rc *runner.RunnerContext, udid string, x, y int) error {
	args := []string{"ui", "tap", strconv.Itoa(x), strconv.Itoa(y)}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui tap: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "tap",
		"x":         x,
		"y":         y,
		"success":   true,
	})
}

// swipe performs a swipe gesture.
func swipe(ctx context.Context, rc *runner.RunnerContext, udid string, x1, y1, x2, y2 int) error {
	args := []string{
		"ui", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2),
	}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui swipe: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "swipe",
		"from":      map[string]int{"x": x1, "y": y1},
		"to":        map[string]int{"x": x2, "y": y2},
		"success":   true,
	})
}

// typeText types text into the simulator.
func typeText(ctx context.Context, rc *runner.RunnerContext, udid, text string) error {
	if text == "" {
		return fmt.Errorf("text is required for type_text operation")
	}

	args := []string{"ui", "text", text}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui text: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "type_text",
		"text":      text,
		"success":   true,
	})
}

// button presses a hardware button.
func button(ctx context.Context, rc *runner.RunnerContext, udid, buttonName string) error {
	if buttonName == "" {
		return fmt.Errorf("button name is required")
	}

	args := []string{"ui", "button", buttonName}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui button: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "button",
		"button":    buttonName,
		"success":   true,
	})
}

// uiTree gets the UI accessibility tree.
func uiTree(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"ui", "describe-all", "--json"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb ui describe-all: %w", err)
	}

	// Initialize to empty slice so JSON serializes as [] not null
	elements := make([]any, 0)
	if err := json.Unmarshal(output, &elements); err != nil {
		// Try line-by-line parsing
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
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

	result := map[string]any{
		"operation": "ui_tree",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}

	// Store full tree in CAS if truncated
	if truncated {
		artifact, err := runner.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/json", "ios_ui_tree")
		if err != nil {
			return fmt.Errorf("persist ui tree: %w", err)
		}
		result["artifact"] = artifact.Digest
		result["hint"] = "Full UI tree stored in CAS; fetch via: agentctl cas get " + artifact.Digest
	}

	return emit(rc, result)
}

// describePoint describes the UI element at a specific point.
func describePoint(ctx context.Context, rc *runner.RunnerContext, udid string, x, y int) error {
	args := []string{"ui", "describe-point", strconv.Itoa(x), strconv.Itoa(y), "--json"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb ui describe-point: %w", err)
	}

	var element any
	if err := json.Unmarshal(output, &element); err != nil {
		return fmt.Errorf("parse element: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "describe_point",
		"x":         x,
		"y":         y,
		"element":   element,
	})
}

// logs gets simulator logs.
func logs(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"log", "--style", "json"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	// Use a timeout context to get recent logs
	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(logCtx, "idb", args...)
	output, cmdErr := cmd.Output()

	// Distinguish expected timeout from real command failures
	if cmdErr != nil {
		// Context timeout/cancellation is expected - we use it to limit log capture time
		if errors.Is(logCtx.Err(), context.DeadlineExceeded) || errors.Is(logCtx.Err(), context.Canceled) {
			// Expected: timeout used to limit log capture, continue with captured output
		} else {
			// Real command failure - return the error
			var exitErr *exec.ExitError
			if errors.As(cmdErr, &exitErr) {
				return fmt.Errorf("idb log failed (exit %d): %s", exitErr.ExitCode(), string(exitErr.Stderr))
			}
			return fmt.Errorf("idb log: %w", cmdErr)
		}
	}

	// Store logs in CAS
	artifact, err := runner.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/x-ndjson", "ios_logs")
	if err != nil {
		return fmt.Errorf("persist logs: %w", err)
	}

	// Get a preview of recent logs
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	previewLines := lines
	if len(lines) > 50 {
		previewLines = lines[len(lines)-50:]
	}

	return emit(rc, map[string]any{
		"operation":    "logs",
		"artifact":     artifact.Digest,
		"preview":      strings.Join(previewLines, "\n"),
		"total_lines":  len(lines),
		"preview_from": "last_50",
	})
}

// openURL opens a URL in the simulator.
func openURL(ctx context.Context, rc *runner.RunnerContext, udid, url string) error {
	if url == "" {
		return fmt.Errorf("url is required for open_url operation")
	}

	args := []string{"open", url}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb open: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "open_url",
		"url":       url,
		"success":   true,
	})
}

// setLocation sets the simulated GPS location.
func setLocation(ctx context.Context, rc *runner.RunnerContext, udid string, lat, long float64) error {
	args := []string{
		"set_location",
		strconv.FormatFloat(lat, 'f', 6, 64),
		strconv.FormatFloat(long, 'f', 6, 64),
	}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb set_location: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "set_location",
		"latitude":  lat,
		"longitude": long,
		"success":   true,
	})
}

// approvePermissions approves permissions for an app.
func approvePermissions(ctx context.Context, rc *runner.RunnerContext, udid, bundleID string, permissions []string) error {
	if bundleID == "" {
		return fmt.Errorf("app bundle ID is required for approve_permissions")
	}
	if len(permissions) == 0 {
		return fmt.Errorf("permissions list is required")
	}

	args := []string{"approve", bundleID}
	args = append(args, permissions...)
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb approve: %w", err)
	}

	return emit(rc, map[string]any{
		"operation":   "approve_permissions",
		"app":         bundleID,
		"permissions": permissions,
		"success":     true,
	})
}

const iosRecordPIDFile = "/tmp/agentctl_ios_record.pid"

// recordStart starts video recording.
func recordStart(ctx context.Context, rc *runner.RunnerContext, udid, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/ios_recording_%d.mp4", time.Now().UnixNano())
	}

	args := []string{"record", outputPath}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	// Start recording in background
	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("idb record start: %w", err)
	}

	// Store PID for later stop
	pid := cmd.Process.Pid
	if err := os.WriteFile(iosRecordPIDFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		errs.Ignore(cmd.Process.Kill(), "kill recording process after pid file write failure")
		return fmt.Errorf("write pid file: %w", err)
	}

	// Start a goroutine to wait for the process (prevents zombie)
	go func() {
		errs.Ignore(cmd.Wait(), "wait for recording process")
		_ = os.Remove(iosRecordPIDFile)
	}()

	return emit(rc, map[string]any{
		"operation":   "record_start",
		"output_path": outputPath,
		"pid":         pid,
		"success":     true,
		"message":     "Recording started. Use record_stop to finish.",
	})
}

// recordStop stops video recording.
func recordStop(ctx context.Context, rc *runner.RunnerContext) error {
	pidData, err := os.ReadFile(iosRecordPIDFile)
	if err != nil {
		// No PID file means no known recording in progress
		// Return error instead of using dangerous broad pkill fallback
		if os.IsNotExist(err) {
			return fmt.Errorf("no active recording found (PID file missing): start a recording with record_start first")
		}
		return fmt.Errorf("read recording PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		_ = os.Remove(iosRecordPIDFile)
		return fmt.Errorf("invalid pid file: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err == nil {
		errs.Ignore(proc.Signal(os.Interrupt), "send interrupt to recording process")
	}
	_ = os.Remove(iosRecordPIDFile)

	return emit(rc, map[string]any{
		"operation": "record_stop",
		"success":   true,
		"message":   "Recording stop signal sent",
	})
}

// crashLogs lists crash logs.
func crashLogs(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"crash", "list", "--json"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb crash list: %w", err)
	}

	// Initialize to empty slice so JSON serializes as [] not null
	crashes := make([]any, 0)
	if err := json.Unmarshal(output, &crashes); err != nil {
		// Try line-by-line parsing
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
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

// addMedia adds media files to the simulator.
func addMedia(ctx context.Context, rc *runner.RunnerContext, udid, mediaPath string) error {
	if mediaPath == "" {
		return fmt.Errorf("media_path is required for add_media operation")
	}

	// Resolve media path to absolute and validate to prevent path traversal
	absPath, err := filepath.Abs(mediaPath)
	if err != nil {
		return fmt.Errorf("resolve media path: %w", err)
	}

	// Resolve symlinks to get the real path (prevents TOCTOU attacks)
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("media path does not exist or is inaccessible: %w", err)
	}

	// Verify the file exists and is a regular file (not a directory or device)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return fmt.Errorf("media path invalid: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("media_path %s: expected file but got directory", resolvedPath)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("media_path %s: not a regular file", resolvedPath)
	}

	args := []string{"add-media", resolvedPath}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb add-media: %w", err)
	}

	return emit(rc, map[string]any{
		"operation":  "add_media",
		"media_path": resolvedPath,
		"success":    true,
	})
}

// clearKeychain clears the keychain.
func clearKeychain(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"clear_keychain"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb clear_keychain: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "clear_keychain",
		"success":   true,
	})
}

// focus brings the simulator window to front.
func focus(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	args := []string{"focus"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb focus: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "focus",
		"success":   true,
	})
}

// doShake performs the actual shake gesture logic without emitting an envelope.
// This is used by both shake() and expoReload() to avoid double envelope emission.
func doShake(ctx context.Context, udid string) error {
	// IDB doesn't have a direct shake command, so we simulate it
	// by sending the shake hardware event through button
	args := []string{"ui", "button", "SHAKE"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	err := cmd.Run()
	if err != nil {
		// Fallback: Try using simctl
		simArgs := []string{"simctl"}
		if udid != "" {
			simArgs = append(simArgs, "io", udid)
		} else {
			simArgs = append(simArgs, "io", "booted")
		}
		simArgs = append(simArgs, "shake")

		simCmd := exec.CommandContext(ctx, "xcrun", simArgs...)
		if simErr := simCmd.Run(); simErr != nil {
			return fmt.Errorf("shake failed (idb: %v, simctl: %v)", err, simErr)
		}
	}

	return nil
}

// shake simulates a shake gesture (for Expo dev menu).
func shake(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	if err := doShake(ctx, udid); err != nil {
		return err
	}

	return emit(rc, map[string]any{
		"operation": "shake",
		"success":   true,
		"message":   "Shake gesture triggered (opens Expo dev menu if app is running)",
	})
}

// expoDeepLink opens an Expo deep link URL.
func expoDeepLink(ctx context.Context, rc *runner.RunnerContext, udid, expoURL string) error {
	if expoURL == "" {
		return fmt.Errorf("expo_url is required for expo_deep_link operation")
	}

	// Ensure URL has exp:// or exps:// scheme
	if !strings.HasPrefix(expoURL, "exp://") && !strings.HasPrefix(expoURL, "exps://") {
		expoURL = "exp://" + expoURL
	}

	args := []string{"open", expoURL}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb open expo URL: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "expo_deep_link",
		"expo_url":  expoURL,
		"success":   true,
		"message":   "Expo deep link opened",
	})
}

// expoReload triggers a reload in the Expo app.
func expoReload(ctx context.Context, rc *runner.RunnerContext, udid string) error {
	// Method 1: Shake to open dev menu, then tap reload
	// Use doShake to avoid double envelope emission
	if err := doShake(ctx, udid); err != nil {
		return fmt.Errorf("shake for expo reload: %w", err)
	}

	// Give the menu time to appear
	time.Sleep(500 * time.Millisecond)

	// Type 'r' which is the keyboard shortcut for reload in Expo
	args := []string{"ui", "text", "r"}
	if udid != "" {
		args = append(args, "--udid", udid)
	}

	cmd := exec.CommandContext(ctx, "idb", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("type 'r' for reload: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "expo_reload",
		"success":   true,
		"message":   "Expo reload triggered via shake + 'r' key",
	})
}

// emit outputs the result envelope.
func emit(rc *runner.RunnerContext, data map[string]any) error {
	return rc.Emit(skillName, data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(code string, err error) {
	env := envelope.Error(skillName, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure envelope")
	os.Exit(1)
}
