// Package main implements the mobile/android skill for Android Emulator automation via ADB.
package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/mobileutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/oputil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textutil"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "mobile/android"

var allowedOps = []string{
	"list_devices",
	"device_info",
	"install",
	"launch",
	"terminate",
	"screenshot",
	"tap",
	"swipe",
	"type_text",
	"press_key",
	"ui_tree",
	"logs",
	"logcat_filter",
	"logcat_app",
	"logcat_crash",
	"logcat_clear",
	"open_url",
	"grant_permission",
	"record_screen",
	"record_stop",
	"dumpsys",
	"pull_file",
	"push_file",
}

// input defines the parameters for Android device automation with ADB.
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

// UINode represents a node in the UI hierarchy with XML attributes.
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

// main is the skill entry point for mobile/android.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates Android device automation via ADB with comprehensive device management capabilities.
//
// Index:
// - Purpose: Automate Android devices/emulators via ADB including app management, UI interaction, and system operations
// - Flow: validate operation → check ADB availability → route to specific handler → execute ADB commands → emit results
// - SideEffects: device interaction; app installation/launch; UI automation; file transfers; screen recording; log collection
// - FailureModes: ADB unavailable, device not connected, invalid operations, command execution failures
// - Observability: emits operation results, device status, file artifacts, and structured UI hierarchy data
// - Related: mobileutil.RunADB, mobileutil.ListADBDevices, parseUIHierarchy, emitLogcatResult
// - Keywords: mobile/android, adb_automation, device_management, ui_automation, android_testing
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
	// Check ADB availability
	if _, err := executil.RequireTool("adb", "install Android SDK or android-platform-tools"); err != nil {
		return skillerr.Runtime(
			"adb not found",
			skillerr.WithCause(err),
			skillerr.WithHint("Install Android platform-tools (adb) and ensure it is available in PATH."),
		)
	}

	switch op {
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
		return skillerr.Arg(fmt.Sprintf("unknown operation: %s", in.Operation), skillerr.WithHint(opHint))
	}
}

// listDevices lists all connected Android devices/emulators.
func listDevices(ctx context.Context, rc *skillmain.RunContext) error {
	devices, err := mobileutil.ListADBDevices(ctx)
	if err != nil {
		return skillerr.WrapRuntime("adb devices", err)
	}

	emulators := make([]mobileutil.ADBDevice, 0, len(devices))
	for _, dev := range devices {
		if strings.HasPrefix(dev.Serial, "emulator-") {
			emulators = append(emulators, dev)
		}
	}

	return emit(rc, map[string]any{
		"operation": "list_devices",
		"devices":   emulators,
		"count":     len(emulators),
	})
}

// deviceInfo gets detailed info about a device.
func deviceInfo(ctx context.Context, rc *skillmain.RunContext, serial string) error {
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
	properties := mobileutil.CollectADBProperties(ctx, serial, props, true, true)

	return emit(rc, map[string]any{
		"operation":  "device_info",
		"serial":     serial,
		"properties": properties,
	})
}

// install installs an APK.
func install(ctx context.Context, rc *skillmain.RunContext, serial, apkPath string) error {
	if apkPath == "" {
		return skillerr.Arg(
			"app (APK path) is required for install operation",
			skillerr.WithHint("Provide the APK path in app."),
		)
	}

	result := mobileutil.RunADB(ctx, serial, "install", "-r", apkPath)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return skillerr.Runtimef("adb install: %v (%s)", result.Err, string(combined))
	}

	return emit(rc, map[string]any{
		"operation": "install",
		"app":       apkPath,
		"success":   true,
		"message":   strings.TrimSpace(string(combined)),
	})
}

// launch starts an app.
func launch(ctx context.Context, rc *skillmain.RunContext, serial, pkg, activity string) error {
	if pkg == "" {
		return skillerr.Arg(
			"app (package name) is required for launch operation",
			skillerr.WithHint("Provide the app package name in app."),
		)
	}

	component := mobileutil.ResolveAndroidLaunchComponent(ctx, serial, pkg, activity)

	result := mobileutil.RunADB(ctx, serial, "shell", "am", "start", "-n", component)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return skillerr.Runtimef("adb am start: %v (%s)", result.Err, string(combined))
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
		return skillerr.Arg(
			"app (package name) is required for terminate operation",
			skillerr.WithHint("Provide the app package name in app."),
		)
	}

	result := mobileutil.RunADB(ctx, serial, "shell", "am", "force-stop", pkg)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb am force-stop", result.Err)
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
	result := mobileutil.RunADB(ctx, serial, "exec-out", "screencap", "-p")
	if result.Err != nil {
		return skillerr.WrapRuntime("adb screencap", result.Err)
	}

	// Write to local file
	if err := os.WriteFile(outputPath, result.Stdout, 0o644); err != nil {
		return skillerr.WrapIO("write screenshot", err)
	}

	// Store in CAS
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(result.Stdout), "image/png", "android_screenshot")
	if err != nil {
		return skillerr.WrapIO("persist screenshot", err)
	}

	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"size_bytes": len(result.Stdout),
		"success":    true,
	})
}

// tap performs a tap.
func tap(ctx context.Context, rc *skillmain.RunContext, serial string, x, y int) error {
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "tap",
		strconv.Itoa(x), strconv.Itoa(y))
	if result.Err != nil {
		return skillerr.WrapRuntime("adb input tap", result.Err)
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

	result := mobileutil.RunADB(ctx, serial, "shell", "input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2),
		strconv.Itoa(duration))
	if result.Err != nil {
		return skillerr.WrapRuntime("adb input swipe", result.Err)
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
		return skillerr.Arg(
			"text is required for type_text operation",
			skillerr.WithHint("Provide text to type."),
		)
	}

	// ADB input text requires spaces as %s and special chars escaped.
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "text", mobileutil.EscapeADBInputText(text))
	if result.Err != nil {
		return skillerr.WrapRuntime("adb input text", result.Err)
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
		return skillerr.Arg(
			"keycode is required for press_key operation",
			skillerr.WithHint("Provide the Android keycode (e.g., KEYCODE_HOME)."),
		)
	}

	// Ensure KEYCODE_ prefix
	if !strings.HasPrefix(keycode, "KEYCODE_") {
		keycode = "KEYCODE_" + keycode
	}

	result := mobileutil.RunADB(ctx, serial, "shell", "input", "keyevent", keycode)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb input keyevent", result.Err)
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
	cmdResult := mobileutil.RunADB(ctx, serial, "shell", "uiautomator", "dump", remotePath)
	combined := append(append([]byte{}, cmdResult.Stdout...), cmdResult.Stderr...)
	if cmdResult.Err != nil {
		return skillerr.Runtimef("uiautomator dump: %v (%s)", cmdResult.Err, string(combined))
	}

	// Read the dump
	cmdResult = mobileutil.RunADB(ctx, serial, "shell", "cat", remotePath)
	if cmdResult.Err != nil {
		return skillerr.WrapRuntime("read ui dump", cmdResult.Err)
	}

	// Clean up
	_ = mobileutil.RunADB(ctx, serial, "shell", "rm", remotePath).Err

	// Parse XML to extract elements
	elements := parseUIHierarchy(cmdResult.Stdout)

	// Prepare preview
	const maxPreview = 30
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
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(cmdResult.Stdout), "application/xml", "android_ui_tree")
		if err != nil {
			return skillerr.WrapIO("persist ui tree", err)
		}
		payload["artifact"] = artifact.Digest
		payload["hint"] = skillout.FormatCASHint("UI tree", artifact.Digest)
	}

	return emit(rc, payload)
}

// parseUIHierarchy parses the UI hierarchy XML into a flat list of elements with boolean conversions.
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
	cmdResult := mobileutil.RunADB(ctx, serial, "logcat", "-d", "-t", strconv.Itoa(count))
	if cmdResult.Err != nil {
		return skillerr.WrapRuntime("adb logcat", cmdResult.Err)
	}

	// Apply pattern filter if specified
	filteredOutput := cmdResult.Stdout
	if pattern != "" {
		filteredOutput = filterLogsByPattern(cmdResult.Stdout, pattern)
	}

	// Store in CAS
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_logs")
	if err != nil {
		return skillerr.WrapIO("persist logs", err)
	}

	// Get preview
	lines := strings.Split(string(filteredOutput), "\n")
	preview := textutil.JoinTail(lines, 50, "\n")

	payload := map[string]any{
		"operation":    "logs",
		"artifact":     artifact.Digest,
		"preview":      preview,
		"total_lines":  len(lines),
		"preview_from": "last_50",
	}
	if pattern != "" {
		payload["pattern"] = pattern
	}

	return emit(rc, payload)
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

	cmdResult := mobileutil.RunADB(ctx, serial, args...)
	if cmdResult.Err != nil {
		return skillerr.WrapRuntime("adb logcat filter", cmdResult.Err)
	}

	// Apply pattern filter if specified
	filteredOutput := cmdResult.Stdout
	if pattern != "" {
		filteredOutput = filterLogsByPattern(cmdResult.Stdout, pattern)
	}

	lines := strings.Split(string(filteredOutput), "\n")

	// Store in CAS for large outputs
	payload := map[string]any{
		"operation": "logcat_filter",
		"tag":       tag,
		"level":     level,
		"lines":     len(lines),
	}

	if pattern != "" {
		payload["pattern"] = pattern
	}

	// Store in CAS if large, otherwise return inline
	if len(filteredOutput) > 10000 {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_logs_filtered")
		if err != nil {
			return skillerr.WrapIO("persist filtered logs", err)
		}
		payload["artifact"] = artifact.Digest
		// Show preview
		payload["preview"] = textutil.JoinTail(lines, 50, "\n")
		payload["hint"] = skillout.FormatCASHint("logs", artifact.Digest)
	} else {
		payload["logs"] = string(filteredOutput)
	}

	return emit(rc, payload)
}

// logcatApp gets logs filtered by app package name.
func logcatApp(ctx context.Context, rc *skillmain.RunContext, serial, pkg, level string, count int, pattern string) error {
	if pkg == "" {
		return skillerr.Arg(
			"app (package name) is required for logcat_app operation",
			skillerr.WithHint("Provide the app package name in app."),
		)
	}
	if count <= 0 {
		count = 500
	}

	// Get the PID of the app
	pidResult := mobileutil.RunADB(ctx, serial, "shell", "pidof", "-s", pkg)
	if pidResult.Err != nil {
		// App might not be running, try to get logs anyway with grep
		return logcatAppByGrep(ctx, rc, serial, pkg, level, count, pattern)
	}

	pid := strings.TrimSpace(string(pidResult.Stdout))
	if pid == "" {
		return logcatAppByGrep(ctx, rc, serial, pkg, level, count, pattern)
	}

	// Get logs filtered by PID
	args := []string{"logcat", "-d", "-t", strconv.Itoa(count), "--pid=" + pid}
	if level != "" {
		args = append(args, "*:"+level)
	}

	result := mobileutil.RunADB(ctx, serial, args...)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb logcat --pid", result.Err)
	}

	// Apply pattern filter if specified
	filteredOutput := result.Stdout
	if pattern != "" {
		filteredOutput = filterLogsByPattern(result.Stdout, pattern)
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

	result := mobileutil.RunADB(ctx, serial, args...)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb logcat", result.Err)
	}

	// Filter lines containing the package name
	lines := strings.Split(string(result.Stdout), "\n")
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
	cmdResult := mobileutil.RunADB(ctx, serial, args...)
	if cmdResult.Err != nil {
		return skillerr.WrapRuntime("adb logcat -b crash", cmdResult.Err)
	}

	// Filter by package if specified
	filteredOutput := cmdResult.Stdout
	if pkg != "" {
		lines := strings.Split(string(cmdResult.Stdout), "\n")
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

	payload := map[string]any{
		"operation":   "logcat_crash",
		"has_crashes": hasCrashes,
		"lines":       len(lines),
	}

	if pkg != "" {
		payload["app"] = pkg
	}

	if len(filteredOutput) > 10000 {
		artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(filteredOutput), "text/plain", "android_crash_logs")
		if err != nil {
			return skillerr.WrapIO("persist crash logs", err)
		}
		payload["artifact"] = artifact.Digest
		// Show preview
		payload["preview"] = textutil.JoinTail(lines, 30, "\n")
	} else {
		payload["logs"] = string(filteredOutput)
	}

	return emit(rc, payload)
}

// logcatClear clears the logcat buffer.
func logcatClear(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	// Clear all buffers
	result := mobileutil.RunADB(ctx, serial, "logcat", "-c")
	if result.Err != nil {
		return skillerr.WrapRuntime("adb logcat -c", result.Err)
	}

	return emit(rc, map[string]any{
		"operation": "logcat_clear",
		"success":   true,
		"message":   "Logcat buffer cleared",
	})
}

// filterLogsByPattern filters log lines by a regex pattern with case-insensitive matching.
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
			return skillerr.WrapIO("persist logs", err)
		}
		result["artifact"] = artifact.Digest
		// Show preview
		result["preview"] = textutil.JoinTail(lines, 50, "\n")
		result["hint"] = skillout.FormatCASHint("logs", artifact.Digest)
	} else {
		result["logs"] = string(output)
	}

	return emit(rc, result)
}

// openURL opens a URL.
func openURL(ctx context.Context, rc *skillmain.RunContext, serial, url string) error {
	if url == "" {
		return skillerr.Arg("url is required for open_url operation", skillerr.WithHint("Provide a URL to open."))
	}

	result := mobileutil.RunADB(ctx, serial, "shell", "am", "start",
		"-a", "android.intent.action.VIEW", "-d", url)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb am start VIEW", result.Err)
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
		return skillerr.Arg(
			"app (package name) is required for grant_permission",
			skillerr.WithHint("Provide the app package name in app."),
		)
	}
	if permission == "" {
		return skillerr.Arg("permission is required", skillerr.WithHint("Provide a permission name to grant."))
	}

	// Ensure full permission name
	if !strings.Contains(permission, ".") {
		permission = "android.permission." + permission
	}

	result := mobileutil.RunADB(ctx, serial, "shell", "pm", "grant", pkg, permission)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return skillerr.Runtimef("adb pm grant: %v (%s)", result.Err, string(combined))
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
	cmd, err := executil.Start(ctx, "", "adb", mobileutil.ADBArgs(serial, "shell", "screenrecord", "--time-limit", "180", remotePath)...)
	if err != nil {
		return skillerr.WrapRuntime("adb screenrecord", err)
	}

	// Store PID and remote path for later stop
	pid := cmd.Process.Pid
	if err := os.WriteFile(androidRecordPIDFile, []byte(strconv.Itoa(pid)), 0o600); err != nil {
		errs.Ignore(cmd.Process.Kill(), "kill recording process after pid file write failure")
		return skillerr.WrapIO("write pid file", err)
	}
	if err := os.WriteFile(androidRecordPathFile, []byte(remotePath), 0o600); err != nil {
		_ = os.Remove(androidRecordPIDFile)
		errs.Ignore(cmd.Process.Kill(), "kill recording process after path file write failure")
		return skillerr.WrapIO("write path file", err)
	}
	if err := os.WriteFile(androidRecordSerialFile, []byte(serial), 0o600); err != nil {
		_ = os.Remove(androidRecordPIDFile)
		_ = os.Remove(androidRecordPathFile)
		errs.Ignore(cmd.Process.Kill(), "kill recording process after serial file write failure")
		return skillerr.WrapIO("write serial file", err)
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
		errs.Ignore(mobileutil.RunADB(ctx, serial, "shell", "pkill", "-INT", "screenrecord").Err, "pkill screenrecord fallback")
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
		return skillerr.WrapParse("invalid pid file", err)
	}

	// Send SIGINT to the specific process via adb
	errs.Ignore(mobileutil.RunADB(ctx, serial, "shell", "kill", "-INT", strconv.Itoa(pid)).Err, "send interrupt to recording process")
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

	result := mobileutil.RunADB(ctx, serial, "shell", "dumpsys", service)
	if result.Err != nil {
		return skillerr.WrapRuntime("adb dumpsys", result.Err)
	}

	// Store in CAS for large outputs
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(result.Stdout), "text/plain", "android_dumpsys")
	if err != nil {
		return skillerr.WrapIO("persist dumpsys", err)
	}

	// Preview
	lines := strings.Split(string(result.Stdout), "\n")
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
		return skillerr.Arg("remote_path is required for pull_file",
			skillerr.WithHint("Provide the device file path to pull in remote_path."))
	}
	if localPath == "" {
		localPath = "/tmp/" + strings.ReplaceAll(remotePath, "/", "_")
	}

	result := mobileutil.RunADB(ctx, serial, "pull", remotePath, localPath)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return skillerr.Runtimef("adb pull: %v (%s)", result.Err, string(combined))
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
		return skillerr.Arg("local_path is required for push_file",
			skillerr.WithHint("Provide the local file path to push in local_path."))
	}
	if remotePath == "" {
		return skillerr.Arg("remote_path is required for push_file",
			skillerr.WithHint("Provide the destination path on device in remote_path."))
	}

	result := mobileutil.RunADB(ctx, serial, "push", localPath, remotePath)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return skillerr.Runtimef("adb push: %v (%s)", result.Err, string(combined))
	}

	return emit(rc, map[string]any{
		"operation":   "push_file",
		"local_path":  localPath,
		"remote_path": remotePath,
		"success":     true,
		"message":     strings.TrimSpace(string(combined)),
	})
}

// emit outputs the result envelope.
func emit(rc *skillmain.RunContext, data map[string]any) error {
	return skillout.Emit(rc, command, data)
}
