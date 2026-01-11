// Package main implements the mobile/android skill for Android Emulator automation via ADB.
package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const command = "mobile/android"

type input struct {
	Operation  string `json:"operation"`
	Serial     string `json:"serial,omitempty"`
	App        string `json:"app,omitempty"`
	Activity   string `json:"activity,omitempty"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	X2         int    `json:"x2,omitempty"`
	Y2         int    `json:"y2,omitempty"`
	Text       string `json:"text,omitempty"`
	URL        string `json:"url,omitempty"`
	Keycode    string `json:"keycode,omitempty"`
	Permission string `json:"permission,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Level      string `json:"level,omitempty"`
	Service    string `json:"service,omitempty"`
	RemotePath string `json:"remote_path,omitempty"`
	LocalPath  string `json:"local_path,omitempty"`
	Output     string `json:"output,omitempty"`
	Duration   int    `json:"duration,omitempty"`
	// Enhanced logcat parameters
	Count   int    `json:"count,omitempty"`   // Number of log lines to fetch
	Pattern string `json:"pattern,omitempty"` // Grep pattern to filter logs
	Since   string `json:"since,omitempty"`   // Time filter (e.g., "1h", "30m", "2024-01-01 12:00:00")
}

// ADBDevice represents an Android device from adb devices.
type ADBDevice struct {
	Serial      string            `json:"serial"`
	State       string            `json:"state"`
	Product     string            `json:"product,omitempty"`
	Model       string            `json:"model,omitempty"`
	Device      string            `json:"device,omitempty"`
	TransportID string            `json:"transport_id,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// UINode represents a node in the UI hierarchy.
type UINode struct {
	XMLName       xml.Name `xml:"node"`
	Index         string   `xml:"index,attr"`
	Text          string   `xml:"text,attr"`
	ResourceID    string   `xml:"resource-id,attr"`
	Class         string   `xml:"class,attr"`
	Package       string   `xml:"package,attr"`
	ContentDesc   string   `xml:"content-desc,attr"`
	Checkable     string   `xml:"checkable,attr"`
	Checked       string   `xml:"checked,attr"`
	Clickable     string   `xml:"clickable,attr"`
	Enabled       string   `xml:"enabled,attr"`
	Focusable     string   `xml:"focusable,attr"`
	Focused       string   `xml:"focused,attr"`
	Scrollable    string   `xml:"scrollable,attr"`
	LongClickable string   `xml:"long-clickable,attr"`
	Password      string   `xml:"password,attr"`
	Selected      string   `xml:"selected,attr"`
	Bounds        string   `xml:"bounds,attr"`
	Children      []UINode `xml:"node"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate input
	if in.Operation == "" {
		return errors.New("operation is required")
	}
	// Check ADB availability
	if _, err := exec.LookPath("adb"); err != nil {
		return errors.New("adb not found: install Android SDK or 'brew install android-platform-tools'")
	}

	switch in.Operation {
	case "list_devices":
		return listDevices(ctx, rc)
	case "device_info":
		return deviceInfo(ctx, rc, in.Serial)
	case "install":
		return install(ctx, rc, in.Serial, in.App)
	case "launch":
		return launch(ctx, rc, in.Serial, in.App, in.Activity)
	case "terminate":
		return terminate(ctx, rc, in.Serial, in.App)
	case "screenshot":
		return screenshot(ctx, rc, in.Serial, in.Output)
	case "tap":
		return tap(ctx, rc, in.Serial, in.X, in.Y)
	case "swipe":
		return swipe(ctx, rc, in.Serial, in.X, in.Y, in.X2, in.Y2, in.Duration)
	case "type_text":
		return typeText(ctx, rc, in.Serial, in.Text)
	case "press_key":
		return pressKey(ctx, rc, in.Serial, in.Keycode)
	case "ui_tree":
		return uiTree(ctx, rc, in.Serial)
	case "logs":
		return logs(ctx, rc, in.Serial, in.Count, in.Pattern)
	case "logcat_filter":
		return logcatFilter(ctx, rc, in.Serial, in.Tag, in.Level, in.Count, in.Pattern)
	case "logcat_app":
		return logcatApp(ctx, rc, in.Serial, in.App, in.Level, in.Count, in.Pattern)
	case "logcat_crash":
		return logcatCrash(ctx, rc, in.Serial, in.App, in.Count)
	case "logcat_clear":
		return logcatClear(ctx, rc, in.Serial)
	case "open_url":
		return openURL(ctx, rc, in.Serial, in.URL)
	case "grant_permission":
		return grantPermission(ctx, rc, in.Serial, in.App, in.Permission)
	case "record_screen":
		return recordScreen(ctx, rc, in.Serial, in.Output)
	case "record_stop":
		return recordStop(ctx, rc)
	case "dumpsys":
		return dumpsys(ctx, rc, in.Serial, in.Service)
	case "pull_file":
		return pullFile(ctx, rc, in.Serial, in.RemotePath, in.LocalPath)
	case "push_file":
		return pushFile(ctx, rc, in.Serial, in.LocalPath, in.RemotePath)
	default:
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}
}

// adbCommand builds an adb command with optional device serial.
func adbCommand(ctx context.Context, serial string, args ...string) *exec.Cmd {
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	return exec.CommandContext(ctx, "adb", args...)
}

// listDevices lists all connected Android devices/emulators.
func listDevices(ctx context.Context, rc *skillmain.RunContext) error {
	cmd := exec.CommandContext(ctx, "adb", "devices", "-l")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb devices: %w", err)
	}

	var devices []ADBDevice
	lines := strings.Split(string(output), "\n")

	// Skip header line
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		dev := ADBDevice{
			Serial: parts[0],
			State:  parts[1],
		}

		// Parse extended info (product:xxx model:xxx device:xxx transport_id:xxx)
		for _, part := range parts[2:] {
			if strings.HasPrefix(part, "product:") {
				dev.Product = strings.TrimPrefix(part, "product:")
			} else if strings.HasPrefix(part, "model:") {
				dev.Model = strings.TrimPrefix(part, "model:")
			} else if strings.HasPrefix(part, "device:") {
				dev.Device = strings.TrimPrefix(part, "device:")
			} else if strings.HasPrefix(part, "transport_id:") {
				dev.TransportID = strings.TrimPrefix(part, "transport_id:")
			}
		}

		// Only include emulators (serial starts with "emulator-")
		if strings.HasPrefix(dev.Serial, "emulator-") {
			devices = append(devices, dev)
		}
	}

	return emit(rc, map[string]any{
		"operation": "list_devices",
		"devices":   devices,
		"count":     len(devices),
	})
}

// deviceInfo gets detailed info about a device.
func deviceInfo(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	properties := make(map[string]string)

	// Key properties to fetch
	props := []string{
		"ro.product.model",
		"ro.product.brand",
		"ro.product.name",
		"ro.build.version.release",
		"ro.build.version.sdk",
		"ro.build.display.id",
		"ro.product.cpu.abi",
		"ro.hardware",
	}

	for _, prop := range props {
		cmd := adbCommand(ctx, serial, "shell", "getprop", prop)
		output, err := cmd.Output()
		if err == nil {
			properties[prop] = strings.TrimSpace(string(output))
		}
	}

	// Get screen size
	cmd := adbCommand(ctx, serial, "shell", "wm", "size")
	if output, err := cmd.Output(); err == nil {
		if matches := regexp.MustCompile(`(\d+x\d+)`).FindString(string(output)); matches != "" {
			properties["screen_size"] = matches
		}
	}

	// Get density
	cmd = adbCommand(ctx, serial, "shell", "wm", "density")
	if output, err := cmd.Output(); err == nil {
		if matches := regexp.MustCompile(`(\d+)`).FindString(string(output)); matches != "" {
			properties["density"] = matches
		}
	}

	return emit(rc, map[string]any{
		"operation":  "device_info",
		"serial":     serial,
		"properties": properties,
	})
}

// install installs an APK.
func install(ctx context.Context, rc *skillmain.RunContext, serial, apkPath string) error {
	if apkPath == "" {
		return errors.New("app (APK path) is required for install operation")
	}

	cmd := adbCommand(ctx, serial, "install", "-r", apkPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb install: %w (%s)", err, string(output))
	}

	return emit(rc, map[string]any{
		"operation": "install",
		"app":       apkPath,
		"success":   true,
		"message":   strings.TrimSpace(string(output)),
	})
}

// launch starts an app.
func launch(ctx context.Context, rc *skillmain.RunContext, serial, pkg, activity string) error {
	if pkg == "" {
		return errors.New("app (package name) is required for launch operation")
	}

	var component string
	if activity != "" {
		component = pkg + "/" + activity
	} else {
		// Try to find the main activity
		cmd := adbCommand(ctx, serial, "shell", "cmd", "package", "resolve-activity",
			"--brief", pkg)
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			if len(lines) > 0 {
				component = lines[len(lines)-1]
			}
		}
		if component == "" {
			component = pkg + "/.MainActivity"
		}
	}

	cmd := adbCommand(ctx, serial, "shell", "am", "start", "-n", component)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb am start: %w (%s)", err, string(output))
	}

	return emit(rc, map[string]any{
		"operation": "launch",
		"app":       pkg,
		"component": component,
		"success":   true,
	})
}

// terminate stops an app.
func terminate(ctx context.Context, rc *skillmain.RunContext, serial, pkg string) error {
	if pkg == "" {
		return errors.New("app (package name) is required for terminate operation")
	}

	cmd := adbCommand(ctx, serial, "shell", "am", "force-stop", pkg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb am force-stop: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "terminate",
		"app":       pkg,
		"success":   true,
	})
}

// screenshot captures the screen.
func screenshot(ctx context.Context, rc *skillmain.RunContext, serial, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/android_screenshot_%d.png", time.Now().UnixNano())
	}

	// Capture directly using exec-out (faster than screencap + pull)
	cmd := adbCommand(ctx, serial, "exec-out", "screencap", "-p")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb screencap: %w", err)
	}

	// Write to local file
	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("write screenshot: %w", err)
	}

	// Store in CAS
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "image/png", "android_screenshot")
	if err != nil {
		return fmt.Errorf("persist screenshot: %w", err)
	}

	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"size_bytes": len(output),
		"success":    true,
	})
}

// tap performs a tap.
func tap(ctx context.Context, rc *skillmain.RunContext, serial string, x, y int) error {
	cmd := adbCommand(ctx, serial, "shell", "input", "tap",
		strconv.Itoa(x), strconv.Itoa(y))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input tap: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "tap",
		"x":         x,
		"y":         y,
		"success":   true,
	})
}

// swipe performs a swipe gesture.
func swipe(ctx context.Context, rc *skillmain.RunContext, serial string, x1, y1, x2, y2, duration int) error {
	if duration <= 0 {
		duration = 300 // Default 300ms
	}

	cmd := adbCommand(ctx, serial, "shell", "input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2),
		strconv.Itoa(duration))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input swipe: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "swipe",
		"from":      map[string]int{"x": x1, "y": y1},
		"to":        map[string]int{"x": x2, "y": y2},
		"duration":  duration,
		"success":   true,
	})
}

// typeText types text.
func typeText(ctx context.Context, rc *skillmain.RunContext, serial, text string) error {
	if text == "" {
		return errors.New("text is required for type_text operation")
	}

	// ADB input text requires spaces as %s and special chars escaped
	// See: https://developer.android.com/studio/command-line/adb#shellcommands
	var escaped strings.Builder
	for _, r := range text {
		switch r {
		case ' ':
			escaped.WriteString("%s")
		case '\'', '"', '`', '$', '\\', '&', '|', ';', '(', ')', '<', '>', '*', '?', '[', ']', '{', '}', '~', '!', '#':
			escaped.WriteRune('\\')
			escaped.WriteRune(r)
		default:
			escaped.WriteRune(r)
		}
	}

	cmd := adbCommand(ctx, serial, "shell", "input", "text", escaped.String())
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input text: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "type_text",
		"text":      text,
		"success":   true,
	})
}

// pressKey presses a key.
func pressKey(ctx context.Context, rc *skillmain.RunContext, serial, keycode string) error {
	if keycode == "" {
		return errors.New("keycode is required for press_key operation")
	}

	// Ensure KEYCODE_ prefix
	if !strings.HasPrefix(keycode, "KEYCODE_") {
		keycode = "KEYCODE_" + keycode
	}

	cmd := adbCommand(ctx, serial, "shell", "input", "keyevent", keycode)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input keyevent: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "press_key",
		"keycode":   keycode,
		"success":   true,
	})
}

// uiTree gets the UI hierarchy.
func uiTree(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	// Dump UI hierarchy to device
	remotePath := "/sdcard/window_dump.xml"
	cmd := adbCommand(ctx, serial, "shell", "uiautomator", "dump", remotePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uiautomator dump: %w (%s)", err, string(output))
	}

	// Read the dump
	cmd = adbCommand(ctx, serial, "shell", "cat", remotePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read ui dump: %w", err)
	}

	// Clean up
	cleanupCmd := adbCommand(ctx, serial, "shell", "rm", remotePath)
	_ = cleanupCmd.Run()

	// Parse XML to extract elements
	elements := parseUIHierarchy(output)

	// Prepare preview
	const maxPreview = 30
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
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/xml", "android_ui_tree")
		if err != nil {
			return fmt.Errorf("persist ui tree: %w", err)
		}
		result["artifact"] = artifact.Digest
		result["hint"] = "Full UI tree stored in CAS; fetch via: agentctl cas get " + artifact.Digest
	}

	return emit(rc, result)
}

// parseUIHierarchy parses the UI hierarchy XML into a flat list of elements.
func parseUIHierarchy(data []byte) []map[string]any {
	var elements []map[string]any

	var root struct {
		XMLName xml.Name `xml:"hierarchy"`
		Nodes   []UINode `xml:"node"`
	}

	if err := xml.Unmarshal(data, &root); err != nil {
		return elements
	}

	var flatten func(nodes []UINode)
	flatten = func(nodes []UINode) {
		for _, n := range nodes {
			elem := map[string]any{
				"index":          n.Index,
				"text":           n.Text,
				"resource_id":    n.ResourceID,
				"class":          n.Class,
				"package":        n.Package,
				"content_desc":   n.ContentDesc,
				"checkable":      n.Checkable == "true",
				"checked":        n.Checked == "true",
				"clickable":      n.Clickable == "true",
				"enabled":        n.Enabled == "true",
				"focusable":      n.Focusable == "true",
				"focused":        n.Focused == "true",
				"scrollable":     n.Scrollable == "true",
				"long_clickable": n.LongClickable == "true",
				"password":       n.Password == "true",
				"selected":       n.Selected == "true",
				"bounds":         n.Bounds,
			}
			elements = append(elements, elem)
			flatten(n.Children)
		}
	}

	flatten(root.Nodes)
	return elements
}

// logs gets logcat output with optional filtering.
func logs(ctx context.Context, rc *skillmain.RunContext, serial string, count int, pattern string) error {
	if count <= 0 {
		count = 500
	}

	// Get recent logs (dump and exit)
	cmd := adbCommand(ctx, serial, "logcat", "-d", "-t", strconv.Itoa(count))
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat: %w", err)
	}

	// Apply pattern filter if specified
	filteredOutput := output
	if pattern != "" {
		filteredOutput = filterLogsByPattern(output, pattern)
	}

	// Store in CAS
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_logs")
	if err != nil {
		return fmt.Errorf("persist logs: %w", err)
	}

	// Get preview
	lines := strings.Split(string(filteredOutput), "\n")
	previewLines := lines
	if len(lines) > 50 {
		previewLines = lines[len(lines)-50:]
	}

	result := map[string]any{
		"operation":    "logs",
		"artifact":     artifact.Digest,
		"preview":      strings.Join(previewLines, "\n"),
		"total_lines":  len(lines),
		"preview_from": "last_50",
	}
	if pattern != "" {
		result["pattern"] = pattern
	}

	return emit(rc, result)
}

// logcatFilter gets filtered logcat output by tag and level.
func logcatFilter(ctx context.Context, rc *skillmain.RunContext, serial, tag, level string, count int, pattern string) error {
	if count <= 0 {
		count = 500
	}

	args := []string{"logcat", "-d", "-t", strconv.Itoa(count)}

	// Build filter spec
	if tag != "" {
		filter := tag
		if level != "" {
			filter += ":" + level
		} else {
			filter += ":V"
		}
		args = append(args, "-s", filter)
	} else if level != "" {
		// Filter by level only using *:LEVEL format
		args = append(args, "*:"+level)
	}

	cmd := adbCommand(ctx, serial, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat filter: %w", err)
	}

	// Apply pattern filter if specified
	filteredOutput := output
	if pattern != "" {
		filteredOutput = filterLogsByPattern(output, pattern)
	}

	lines := strings.Split(string(filteredOutput), "\n")

	// Store in CAS for large outputs
	result := map[string]any{
		"operation": "logcat_filter",
		"tag":       tag,
		"level":     level,
		"lines":     len(lines),
	}

	if pattern != "" {
		result["pattern"] = pattern
	}

	// Store in CAS if large, otherwise return inline
	if len(filteredOutput) > 10000 {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_logs_filtered")
		if err != nil {
			return fmt.Errorf("persist filtered logs: %w", err)
		}
		result["artifact"] = artifact.Digest
		// Show preview
		previewLines := lines
		if len(lines) > 50 {
			previewLines = lines[len(lines)-50:]
		}
		result["preview"] = strings.Join(previewLines, "\n")
		result["hint"] = "Full logs stored in CAS; fetch via: agentctl cas get " + artifact.Digest
	} else {
		result["logs"] = string(filteredOutput)
	}

	return emit(rc, result)
}

// logcatApp gets logs filtered by app package name.
func logcatApp(ctx context.Context, rc *skillmain.RunContext, serial, pkg, level string, count int, pattern string) error {
	if pkg == "" {
		return errors.New("app (package name) is required for logcat_app operation")
	}
	if count <= 0 {
		count = 500
	}

	// Get the PID of the app
	pidCmd := adbCommand(ctx, serial, "shell", "pidof", "-s", pkg)
	pidOutput, err := pidCmd.Output()
	if err != nil {
		// App might not be running, try to get logs anyway with grep
		return logcatAppByGrep(ctx, rc, serial, pkg, level, count, pattern)
	}

	pid := strings.TrimSpace(string(pidOutput))
	if pid == "" {
		return logcatAppByGrep(ctx, rc, serial, pkg, level, count, pattern)
	}

	// Get logs filtered by PID
	args := []string{"logcat", "-d", "-t", strconv.Itoa(count), "--pid=" + pid}
	if level != "" {
		args = append(args, "*:"+level)
	}

	cmd := adbCommand(ctx, serial, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat --pid: %w", err)
	}

	// Apply pattern filter if specified
	filteredOutput := output
	if pattern != "" {
		filteredOutput = filterLogsByPattern(output, pattern)
	}

	return emitLogcatResult(ctx, rc, "logcat_app", filteredOutput, map[string]any{
		"app":   pkg,
		"pid":   pid,
		"level": level,
	}, pattern)
}

// logcatAppByGrep filters logs by grepping for the package name (fallback when PID not available).
func logcatAppByGrep(ctx context.Context, rc *skillmain.RunContext, serial, pkg, level string, count int, pattern string) error {
	args := []string{"logcat", "-d", "-t", strconv.Itoa(count)}
	if level != "" {
		args = append(args, "*:"+level)
	}

	cmd := adbCommand(ctx, serial, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat: %w", err)
	}

	// Filter lines containing the package name
	lines := strings.Split(string(output), "\n")
	var filtered []string
	for _, line := range lines {
		if strings.Contains(line, pkg) {
			filtered = append(filtered, line)
		}
	}

	filteredOutput := []byte(strings.Join(filtered, "\n"))

	// Apply additional pattern filter if specified
	if pattern != "" {
		filteredOutput = filterLogsByPattern(filteredOutput, pattern)
	}

	return emitLogcatResult(ctx, rc, "logcat_app", filteredOutput, map[string]any{
		"app":          pkg,
		"pid":          "(grep mode)",
		"level":        level,
		"filter_mode":  "grep",
		"grep_pattern": pkg,
	}, pattern)
}

// logcatCrash gets crash logs from the crash buffer.
func logcatCrash(ctx context.Context, rc *skillmain.RunContext, serial, pkg string, count int) error {
	if count <= 0 {
		count = 100
	}

	// Get crash buffer logs
	args := []string{"logcat", "-d", "-b", "crash", "-t", strconv.Itoa(count)}
	cmd := adbCommand(ctx, serial, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat -b crash: %w", err)
	}

	// Filter by package if specified
	filteredOutput := output
	if pkg != "" {
		lines := strings.Split(string(output), "\n")
		var filtered []string
		for _, line := range lines {
			if strings.Contains(line, pkg) {
				filtered = append(filtered, line)
			}
		}
		filteredOutput = []byte(strings.Join(filtered, "\n"))
	}

	lines := strings.Split(string(filteredOutput), "\n")
	hasCrashes := len(lines) > 1 || (len(lines) == 1 && lines[0] != "")

	result := map[string]any{
		"operation":   "logcat_crash",
		"has_crashes": hasCrashes,
		"lines":       len(lines),
	}

	if pkg != "" {
		result["app"] = pkg
	}

	if len(filteredOutput) > 10000 {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_crash_logs")
		if err != nil {
			return fmt.Errorf("persist crash logs: %w", err)
		}
		result["artifact"] = artifact.Digest
		// Show preview
		previewLines := lines
		if len(lines) > 30 {
			previewLines = lines[len(lines)-30:]
		}
		result["preview"] = strings.Join(previewLines, "\n")
	} else {
		result["logs"] = string(filteredOutput)
	}

	return emit(rc, result)
}

// logcatClear clears the logcat buffer.
func logcatClear(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	// Clear all buffers
	cmd := adbCommand(ctx, serial, "logcat", "-c")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb logcat -c: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "logcat_clear",
		"success":   true,
		"message":   "Logcat buffer cleared",
	})
}

// filterLogsByPattern filters log lines by a regex pattern.
func filterLogsByPattern(data []byte, pattern string) []byte {
	re, err := regexp.Compile("(?i)" + pattern) // Case-insensitive
	if err != nil {
		// If pattern is invalid, treat it as a literal substring match
		lines := strings.Split(string(data), "\n")
		var filtered []string
		for _, line := range lines {
			if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
				filtered = append(filtered, line)
			}
		}
		return []byte(strings.Join(filtered, "\n"))
	}

	lines := strings.Split(string(data), "\n")
	var filtered []string
	for _, line := range lines {
		if re.MatchString(line) {
			filtered = append(filtered, line)
		}
	}
	return []byte(strings.Join(filtered, "\n"))
}

// emitLogcatResult handles common logcat result emission with CAS storage for large outputs.
func emitLogcatResult(ctx context.Context, rc *skillmain.RunContext, operation string, output []byte, extra map[string]any, pattern string) error {
	lines := strings.Split(string(output), "\n")

	result := map[string]any{
		"operation": operation,
		"lines":     len(lines),
	}

	// Merge extra fields
	for k, v := range extra {
		result[k] = v
	}

	if pattern != "" {
		result["pattern"] = pattern
	}

	// Store in CAS if large, otherwise return inline
	if len(output) > 10000 {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "text/plain", "android_logs")
		if err != nil {
			return fmt.Errorf("persist logs: %w", err)
		}
		result["artifact"] = artifact.Digest
		// Show preview
		previewLines := lines
		if len(lines) > 50 {
			previewLines = lines[len(lines)-50:]
		}
		result["preview"] = strings.Join(previewLines, "\n")
		result["hint"] = "Full logs stored in CAS; fetch via: agentctl cas get " + artifact.Digest
	} else {
		result["logs"] = string(output)
	}

	return emit(rc, result)
}

// openURL opens a URL.
func openURL(ctx context.Context, rc *skillmain.RunContext, serial, url string) error {
	if url == "" {
		return errors.New("url is required for open_url operation")
	}

	cmd := adbCommand(ctx, serial, "shell", "am", "start",
		"-a", "android.intent.action.VIEW", "-d", url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb am start VIEW: %w", err)
	}

	return emit(rc, map[string]any{
		"operation": "open_url",
		"url":       url,
		"success":   true,
	})
}

// grantPermission grants a runtime permission.
func grantPermission(ctx context.Context, rc *skillmain.RunContext, serial, pkg, permission string) error {
	if pkg == "" {
		return errors.New("app (package name) is required for grant_permission")
	}
	if permission == "" {
		return errors.New("permission is required")
	}

	// Ensure full permission name
	if !strings.Contains(permission, ".") {
		permission = "android.permission." + permission
	}

	cmd := adbCommand(ctx, serial, "shell", "pm", "grant", pkg, permission)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb pm grant: %w (%s)", err, string(output))
	}

	return emit(rc, map[string]any{
		"operation":  "grant_permission",
		"app":        pkg,
		"permission": permission,
		"success":    true,
	})
}

const (
	androidRecordPIDFile    = "/tmp/agentctl_android_record.pid"
	androidRecordPathFile   = "/tmp/agentctl_android_record_path.txt"
	androidRecordSerialFile = "/tmp/agentctl_android_record_serial.txt"
)

// recordScreen starts screen recording.
func recordScreen(ctx context.Context, rc *skillmain.RunContext, serial, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/android_recording_%d.mp4", time.Now().UnixNano())
	}

	// Use unique remote path to avoid conflicts
	remotePath := fmt.Sprintf("/sdcard/screen_recording_%d.mp4", time.Now().UnixNano())

	// Start recording in background on device
	cmd := adbCommand(ctx, serial, "shell", "screenrecord", "--time-limit", "180", remotePath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("adb screenrecord: %w", err)
	}

	// Store PID and remote path for later stop
	pid := cmd.Process.Pid
	if err := os.WriteFile(androidRecordPIDFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		errs.Ignore(cmd.Process.Kill(), "kill recording process after pid file write failure")
		return fmt.Errorf("write pid file: %w", err)
	}
	if err := os.WriteFile(androidRecordPathFile, []byte(remotePath), 0o600); err != nil {
		_ = os.Remove(androidRecordPIDFile)
		errs.Ignore(cmd.Process.Kill(), "kill recording process after path file write failure")
		return fmt.Errorf("write path file: %w", err)
	}
	if err := os.WriteFile(androidRecordSerialFile, []byte(serial), 0o600); err != nil {
		_ = os.Remove(androidRecordPIDFile)
		_ = os.Remove(androidRecordPathFile)
		errs.Ignore(cmd.Process.Kill(), "kill recording process after serial file write failure")
		return fmt.Errorf("write serial file: %w", err)
	}

	// Start a goroutine to wait for the process (prevents zombie)
	go func() {
		errs.Ignore(cmd.Wait(), "wait for recording process")
		_ = os.Remove(androidRecordPIDFile)
		_ = os.Remove(androidRecordPathFile)
		_ = os.Remove(androidRecordSerialFile)
	}()

	return emit(rc, map[string]any{
		"operation":    "record_screen",
		"remote_path":  remotePath,
		"local_output": outputPath,
		"pid":          pid,
		"success":      true,
		"message":      "Recording started. Use record_stop to finish and pull the file.",
	})
}

// recordStop stops screen recording and pulls the file.
func recordStop(ctx context.Context, rc *skillmain.RunContext) error {
	remotePath := "/sdcard/screen_recording.mp4" // default fallback
	serial := ""                                 // default to empty (uses default device)

	// Try to read the stored remote path
	if pathData, err := os.ReadFile(androidRecordPathFile); err == nil {
		remotePath = strings.TrimSpace(string(pathData))
		_ = os.Remove(androidRecordPathFile)
	}

	// Try to read the stored serial
	if serialData, err := os.ReadFile(androidRecordSerialFile); err == nil {
		serial = strings.TrimSpace(string(serialData))
		_ = os.Remove(androidRecordSerialFile)
	}

	// Try to read the stored PID
	pidData, err := os.ReadFile(androidRecordPIDFile)
	if err != nil {
		// Fallback to pkill if no PID file - use stored serial if available
		cmd := adbCommand(ctx, serial, "shell", "pkill", "-INT", "screenrecord")
		errs.Ignore(cmd.Run(), "pkill screenrecord fallback")
		time.Sleep(time.Second)
		return emit(rc, map[string]any{
			"operation":   "record_stop",
			"remote_path": remotePath,
			"serial":      serial,
			"success":     true,
			"message":     fmt.Sprintf("Recording stop signal sent (fallback mode). Use pull_file to retrieve %s", remotePath),
		})
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		_ = os.Remove(androidRecordPIDFile)
		return fmt.Errorf("invalid pid file: %w", err)
	}

	// Send SIGINT to the specific process via adb
	var killCmd *exec.Cmd
	if serial != "" {
		killCmd = exec.CommandContext(ctx, "adb", "-s", serial, "shell", "kill", "-INT", strconv.Itoa(pid))
	} else {
		killCmd = exec.CommandContext(ctx, "adb", "shell", "kill", "-INT", strconv.Itoa(pid))
	}
	errs.Ignore(killCmd.Run(), "send interrupt to recording process")
	_ = os.Remove(androidRecordPIDFile)

	// Wait for it to finish
	time.Sleep(time.Second)

	return emit(rc, map[string]any{
		"operation":   "record_stop",
		"remote_path": remotePath,
		"success":     true,
		"message":     fmt.Sprintf("Recording stop signal sent. Use pull_file to retrieve %s", remotePath),
	})
}

// dumpsys gets system service dump.
func dumpsys(ctx context.Context, rc *skillmain.RunContext, serial, service string) error {
	if service == "" {
		service = "activity"
	}

	cmd := adbCommand(ctx, serial, "shell", "dumpsys", service)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb dumpsys: %w", err)
	}

	// Store in CAS for large outputs
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "text/plain", "android_dumpsys")
	if err != nil {
		return fmt.Errorf("persist dumpsys: %w", err)
	}

	// Preview
	lines := strings.Split(string(output), "\n")
	preview := lines
	if len(lines) > 100 {
		preview = lines[:100]
	}

	return emit(rc, map[string]any{
		"operation":   "dumpsys",
		"service":     service,
		"artifact":    artifact.Digest,
		"preview":     strings.Join(preview, "\n"),
		"total_lines": len(lines),
	})
}

// pullFile pulls a file from the device.
func pullFile(ctx context.Context, rc *skillmain.RunContext, serial, remotePath, localPath string) error {
	if remotePath == "" {
		return errors.New("remote_path is required for pull_file")
	}
	if localPath == "" {
		localPath = "/tmp/" + strings.ReplaceAll(remotePath, "/", "_")
	}

	cmd := adbCommand(ctx, serial, "pull", remotePath, localPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb pull: %w (%s)", err, string(output))
	}

	// Get file info
	info, _ := os.Stat(localPath)
	var size int64
	if info != nil {
		size = info.Size()
	}

	return emit(rc, map[string]any{
		"operation":   "pull_file",
		"remote_path": remotePath,
		"local_path":  localPath,
		"size_bytes":  size,
		"success":     true,
	})
}

// pushFile pushes a file to the device.
func pushFile(ctx context.Context, rc *skillmain.RunContext, serial, localPath, remotePath string) error {
	if localPath == "" {
		return errors.New("local_path is required for push_file")
	}
	if remotePath == "" {
		return errors.New("remote_path is required for push_file")
	}

	cmd := adbCommand(ctx, serial, "push", localPath, remotePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push: %w (%s)", err, string(output))
	}

	return emit(rc, map[string]any{
		"operation":   "push_file",
		"local_path":  localPath,
		"remote_path": remotePath,
		"success":     true,
		"message":     strings.TrimSpace(string(output)),
	})
}

// emit outputs the result envelope.
func emit(rc *skillmain.RunContext, data map[string]any) error {
	return skillout.Emit(rc, command, data)
}
