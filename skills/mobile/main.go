// Package main implements the unified mobile skill that dispatches to iOS or Android.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/mobileutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "mobile"

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
	"ui_tree",
	"logs",
	"open_url",
}

type input struct {
	Platform  string `json:"platform,omitempty"`
	Operation string `json:"operation"`
	Device    string `json:"device,omitempty"`
	App       string `json:"app,omitempty"`
	X         int    `json:"x,omitempty"`
	Y         int    `json:"y,omitempty"`
	X2        int    `json:"x2,omitempty"`
	Y2        int    `json:"y2,omitempty"`
	Text      string `json:"text,omitempty"`
	URL       string `json:"url,omitempty"`
	Output    string `json:"output,omitempty"`
}

// UnifiedDevice represents a device from either platform.
type UnifiedDevice struct {
	Platform string `json:"platform"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	OS       string `json:"os,omitempty"`
	Model    string `json:"model,omitempty"`
}

func simctlTarget(device string) string {
	if strings.TrimSpace(device) != "" {
		return strings.TrimSpace(device)
	}
	return "booted"
}

func iosOpRequiresIDB(op string) bool {
	switch op {
	case "device_info", "tap", "swipe", "type_text", "ui_tree", "logs":
		return true
	default:
		return false
	}
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults (moved from parseInput)
	op := oputil.Op(in.Operation)
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if op == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint(opHint))
	}
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}
	if in.Platform == "" {
		in.Platform = "auto"
	}
	// Check tool availability
	hasIDB := mobileutil.HasRunnableIDB(ctx)
	hasSimctl := checkTool("xcrun")
	hasADB := checkTool("adb")

	if !hasIDB && !hasSimctl && !hasADB {
		return skillerr.Runtime(
			"none of idb, xcrun, or adb found",
			skillerr.WithHint("Install Xcode command line tools for simctl, optionally idb-companion, or adb (Android SDK)."),
		)
	}

	// Handle auto mode for list_devices
	if in.Platform == "auto" {
		if op == "list_devices" {
			return listAllDevices(ctx, rc, hasIDB, hasSimctl, hasADB)
		}
		return skillerr.Arg(
			fmt.Sprintf("platform is required for %s operation", op),
			skillerr.WithHint("Use platform \"ios\" or \"android\" for non-list_devices operations."),
		)
	}

	// Validate platform availability
	if in.Platform == "ios" && !hasIDB && !hasSimctl {
		return skillerr.Runtime("neither idb nor xcrun found", skillerr.WithHint("Install Xcode command line tools and optionally idb-companion."))
	}
	if in.Platform == "ios" && iosOpRequiresIDB(op) && !hasIDB {
		return skillerr.Runtime("idb not found", skillerr.WithHint("Install with: brew install idb-companion. This iOS operation still requires idb."))
	}
	if in.Platform == "android" && !hasADB {
		return skillerr.Runtime("adb not found", skillerr.WithHint("Install Android SDK or brew install android-platform-tools."))
	}

	// Dispatch to platform-specific implementation
	switch in.Platform {
	case "ios":
		in.Operation = op
		return runIOS(ctx, rc, in)
	case "android":
		in.Operation = op
		return runAndroid(ctx, rc, in)
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown platform: %s", in.Platform),
			skillerr.WithHint("Use platform \"ios\", \"android\", or \"auto\"."),
		)
	}
}

func checkTool(name string) bool {
	return executil.HasTool(name)
}

// listAllDevices lists devices from both iOS and Android.
func listAllDevices(ctx context.Context, rc *skillmain.RunContext, hasIDB, hasSimctl, hasADB bool) error {
	var allDevices []UnifiedDevice

	// Get iOS simulators
	if hasIDB || hasSimctl {
		iosDevices, err := listIOSDevices(ctx)
		if err == nil {
			allDevices = append(allDevices, iosDevices...)
		}
	}

	// Get Android emulators
	if hasADB {
		androidDevices, err := listAndroidDevices(ctx)
		if err == nil {
			allDevices = append(allDevices, androidDevices...)
		}
	}

	// Count by platform
	iosCount := 0
	androidCount := 0
	for _, d := range allDevices {
		if d.Platform == "ios" {
			iosCount++
		} else {
			androidCount++
		}
	}

	return emit(rc, map[string]any{
		"operation":     "list_devices",
		"platform":      "both",
		"devices":       allDevices,
		"count":         len(allDevices),
		"ios_count":     iosCount,
		"android_count": androidCount,
	})
}

func listIOSDevices(ctx context.Context) ([]UnifiedDevice, error) {
	var rawDevices []mobileutil.IDBDevice
	rawDevices, err := mobileutil.ListSimctlDevices(ctx)
	if err != nil {
		rawDevices, err = mobileutil.ListIDBDevices(ctx)
		if err != nil {
			return nil, err
		}
	}

	devices := make([]UnifiedDevice, 0, len(rawDevices))
	for _, dev := range rawDevices {
		if dev.TargetType != "simulator" {
			continue
		}
		devices = append(devices, UnifiedDevice{
			Platform: "ios",
			ID:       dev.UDID,
			Name:     dev.Name,
			State:    dev.State,
			OS:       dev.OSVersion,
		})
	}
	return devices, nil
}

func listAndroidDevices(ctx context.Context) ([]UnifiedDevice, error) {
	adbDevices, err := mobileutil.ListADBDevices(ctx)
	if err != nil {
		return nil, err
	}

	devices := make([]UnifiedDevice, 0, len(adbDevices))
	for _, dev := range adbDevices {
		// Only include emulators
		if !strings.HasPrefix(dev.Serial, "emulator-") {
			continue
		}
		unified := UnifiedDevice{
			Platform: "android",
			ID:       dev.Serial,
			State:    dev.State,
			Model:    dev.Model,
		}
		if dev.Model != "" {
			unified.Name = dev.Model
		} else {
			unified.Name = unified.ID
		}
		devices = append(devices, unified)
	}
	return devices, nil
}

// runIOS dispatches to iOS-specific operations.
func runIOS(ctx context.Context, rc *skillmain.RunContext, in input) error {
	switch in.Operation {
	case "list_devices":
		return iosListDevices(ctx, rc)
	case "device_info":
		return iosDeviceInfo(ctx, rc, in.Device)
	case "install":
		return iosInstall(ctx, rc, in.Device, in.App)
	case "launch":
		return iosLaunch(ctx, rc, in.Device, in.App)
	case "terminate":
		return iosTerminate(ctx, rc, in.Device, in.App)
	case "screenshot":
		return iosScreenshot(ctx, rc, in.Device, in.Output)
	case "tap":
		return iosTap(ctx, rc, in.Device, in.X, in.Y)
	case "swipe":
		return iosSwipe(ctx, rc, in.Device, in.X, in.Y, in.X2, in.Y2)
	case "type_text":
		return iosTypeText(ctx, rc, in.Device, in.Text)
	case "ui_tree":
		return iosUITree(ctx, rc, in.Device)
	case "logs":
		return iosLogs(ctx, rc, in.Device)
	case "open_url":
		return iosOpenURL(ctx, rc, in.Device, in.URL)
	default:
		return fmt.Errorf("unknown operation for iOS: %s", in.Operation)
	}
}

// runAndroid dispatches to Android-specific operations.
func runAndroid(ctx context.Context, rc *skillmain.RunContext, in input) error {
	switch in.Operation {
	case "list_devices":
		return androidListDevices(ctx, rc)
	case "device_info":
		return androidDeviceInfo(ctx, rc, in.Device)
	case "install":
		return androidInstall(ctx, rc, in.Device, in.App)
	case "launch":
		return androidLaunch(ctx, rc, in.Device, in.App)
	case "terminate":
		return androidTerminate(ctx, rc, in.Device, in.App)
	case "screenshot":
		return androidScreenshot(ctx, rc, in.Device, in.Output)
	case "tap":
		return androidTap(ctx, rc, in.Device, in.X, in.Y)
	case "swipe":
		return androidSwipe(ctx, rc, in.Device, in.X, in.Y, in.X2, in.Y2)
	case "type_text":
		return androidTypeText(ctx, rc, in.Device, in.Text)
	case "ui_tree":
		return androidUITree(ctx, rc, in.Device)
	case "logs":
		return androidLogs(ctx, rc, in.Device)
	case "open_url":
		return androidOpenURL(ctx, rc, in.Device, in.URL)
	default:
		return fmt.Errorf("unknown operation for Android: %s", in.Operation)
	}
}

// ==================== iOS Operations ====================

func iosListDevices(ctx context.Context, rc *skillmain.RunContext) error {
	devices, err := listIOSDevices(ctx)
	if err != nil {
		return err
	}
	return emit(rc, map[string]any{
		"operation": "list_devices",
		"platform":  "ios",
		"devices":   devices,
		"count":     len(devices),
	})
}

func iosDeviceInfo(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	result := mobileutil.RunIDB(ctx, udid, "describe", "--json")
	if result.Err != nil {
		return fmt.Errorf("idb describe: %w", result.Err)
	}
	var device map[string]any
	_ = json.Unmarshal(result.Stdout, &device) // Best-effort parse
	return emit(rc, map[string]any{
		"operation": "device_info",
		"platform":  "ios",
		"device":    device,
	})
}

func iosInstall(ctx context.Context, rc *skillmain.RunContext, udid, app string) error {
	if app == "" {
		return fmt.Errorf("app path is required")
	}
	backend := "simctl"
	result := mobileutil.RunSimctl(ctx, "install", simctlTarget(udid), app)
	if result.Err != nil {
		simctlErr := result.Err
		if !mobileutil.HasRunnableIDB(ctx) {
			return fmt.Errorf("ios install failed (simctl: %w)", simctlErr)
		}
		backend = "idb"
		result = mobileutil.RunIDB(ctx, udid, "install", app)
		if result.Err != nil {
			return fmt.Errorf("ios install failed (simctl: %w, idb: %v)", simctlErr, result.Err)
		}
	}
	return emit(rc, map[string]any{
		"operation": "install",
		"platform":  "ios",
		"backend":   backend,
		"app":       app,
		"success":   true,
	})
}

func iosLaunch(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("bundle ID is required")
	}
	backend := "simctl"
	result := mobileutil.RunSimctl(ctx, "launch", simctlTarget(udid), bundleID)
	if result.Err != nil {
		simctlErr := result.Err
		if !mobileutil.HasRunnableIDB(ctx) {
			return fmt.Errorf("ios launch failed (simctl: %w)", simctlErr)
		}
		backend = "idb"
		result = mobileutil.RunIDB(ctx, udid, "launch", bundleID)
		if result.Err != nil {
			return fmt.Errorf("ios launch failed (simctl: %w, idb: %v)", simctlErr, result.Err)
		}
	}
	return emit(rc, map[string]any{
		"operation": "launch",
		"platform":  "ios",
		"backend":   backend,
		"app":       bundleID,
		"success":   true,
	})
}

func iosTerminate(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("bundle ID is required")
	}
	backend := "simctl"
	result := mobileutil.RunSimctl(ctx, "terminate", simctlTarget(udid), bundleID)
	if result.Err != nil {
		simctlErr := result.Err
		if !mobileutil.HasRunnableIDB(ctx) {
			return fmt.Errorf("ios terminate failed (simctl: %w)", simctlErr)
		}
		backend = "idb"
		result = mobileutil.RunIDB(ctx, udid, "terminate", bundleID)
		if result.Err != nil {
			return fmt.Errorf("ios terminate failed (simctl: %w, idb: %v)", simctlErr, result.Err)
		}
	}
	return emit(rc, map[string]any{
		"operation": "terminate",
		"platform":  "ios",
		"backend":   backend,
		"app":       bundleID,
		"success":   true,
	})
}

func iosScreenshot(ctx context.Context, rc *skillmain.RunContext, udid, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/ios_screenshot_%d.png", time.Now().UnixNano())
	}
	backend := "simctl"
	result := mobileutil.RunSimctl(ctx, "io", simctlTarget(udid), "screenshot", outputPath)
	if result.Err != nil {
		simctlErr := result.Err
		if !mobileutil.HasRunnableIDB(ctx) {
			return fmt.Errorf("ios screenshot failed (simctl: %w)", simctlErr)
		}
		backend = "idb"
		result = mobileutil.RunIDB(ctx, udid, "screenshot", outputPath)
		if result.Err != nil {
			return fmt.Errorf("ios screenshot failed (simctl: %w, idb: %v)", simctlErr, result.Err)
		}
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read screenshot: %w", err)
	}
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(data), "image/png", "mobile_screenshot")
	if err != nil {
		return fmt.Errorf("persist screenshot: %w", err)
	}
	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"platform":   "ios",
		"backend":    backend,
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"success":    true,
	})
}

func iosTap(ctx context.Context, rc *skillmain.RunContext, udid string, x, y int) error {
	result := mobileutil.RunIDB(ctx, udid, "ui", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if result.Err != nil {
		return fmt.Errorf("idb ui tap: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "tap",
		"platform":  "ios",
		"x":         x,
		"y":         y,
		"success":   true,
	})
}

func iosSwipe(ctx context.Context, rc *skillmain.RunContext, udid string, x1, y1, x2, y2 int) error {
	result := mobileutil.RunIDB(ctx, udid, "ui", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2))
	if result.Err != nil {
		return fmt.Errorf("idb ui swipe: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "swipe",
		"platform":  "ios",
		"from":      map[string]int{"x": x1, "y": y1},
		"to":        map[string]int{"x": x2, "y": y2},
		"success":   true,
	})
}

func iosTypeText(ctx context.Context, rc *skillmain.RunContext, udid, text string) error {
	if text == "" {
		return fmt.Errorf("text is required")
	}
	result := mobileutil.RunIDB(ctx, udid, "ui", "text", text)
	if result.Err != nil {
		return fmt.Errorf("idb ui text: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "type_text",
		"platform":  "ios",
		"text":      text,
		"success":   true,
	})
}

func iosUITree(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	cmdResult := mobileutil.RunIDB(ctx, udid, "ui", "describe-all", "--json")
	if cmdResult.Err != nil {
		return fmt.Errorf("idb ui describe-all: %w", cmdResult.Err)
	}
	var elements []any
	_ = json.Unmarshal(cmdResult.Stdout, &elements) // Try as JSON array first
	if elements == nil {
		lines := strings.Split(strings.TrimSpace(string(cmdResult.Stdout)), "\n")
		for _, line := range lines {
			var elem any
			if json.Unmarshal([]byte(line), &elem) == nil {
				elements = append(elements, elem)
			}
		}
	}
	const maxPreview = 20
	preview := elements
	truncated := len(elements) > maxPreview
	if truncated {
		preview = elements[:maxPreview]
	}
	payload := map[string]any{
		"operation": "ui_tree",
		"platform":  "ios",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}
	if truncated {
		if artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(cmdResult.Stdout), "application/json", "mobile_ui_tree"); err == nil {
			payload["artifact"] = artifact.Digest
		}
	}
	return emit(rc, payload)
}

func iosLogs(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := mobileutil.RunIDB(logCtx, udid, "log", "--style", "json")
	output := result.Stdout                                                                                        // Timeout expected, ignore error
	artifact, _ := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/x-ndjson", "mobile_logs") // Best-effort persist
	lines := strings.Split(string(output), "\n")
	preview := lines
	if len(lines) > 50 {
		preview = lines[len(lines)-50:]
	}
	return emit(rc, map[string]any{
		"operation": "logs",
		"platform":  "ios",
		"artifact":  artifact.Digest,
		"preview":   strings.Join(preview, "\n"),
	})
}

func iosOpenURL(ctx context.Context, rc *skillmain.RunContext, udid, url string) error {
	if url == "" {
		return fmt.Errorf("url is required")
	}
	backend := "simctl"
	result := mobileutil.RunSimctl(ctx, "openurl", simctlTarget(udid), url)
	if result.Err != nil {
		simctlErr := result.Err
		if !mobileutil.HasRunnableIDB(ctx) {
			return fmt.Errorf("ios open_url failed (simctl: %w)", simctlErr)
		}
		backend = "idb"
		result = mobileutil.RunIDB(ctx, udid, "open", url)
		if result.Err != nil {
			return fmt.Errorf("ios open_url failed (simctl: %w, idb: %v)", simctlErr, result.Err)
		}
	}
	return emit(rc, map[string]any{
		"operation": "open_url",
		"platform":  "ios",
		"backend":   backend,
		"url":       url,
		"success":   true,
	})
}

// ==================== Android Operations ====================

func androidListDevices(ctx context.Context, rc *skillmain.RunContext) error {
	devices, err := listAndroidDevices(ctx)
	if err != nil {
		return err
	}
	return emit(rc, map[string]any{
		"operation": "list_devices",
		"platform":  "android",
		"devices":   devices,
		"count":     len(devices),
	})
}

func androidDeviceInfo(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	props := []string{
		"ro.product.model", "ro.product.brand", "ro.build.version.release",
		"ro.build.version.sdk", "ro.product.cpu.abi",
	}
	properties := mobileutil.CollectADBProperties(ctx, serial, props, true, false)
	return emit(rc, map[string]any{
		"operation":  "device_info",
		"platform":   "android",
		"serial":     serial,
		"properties": properties,
	})
}

func androidInstall(ctx context.Context, rc *skillmain.RunContext, serial, apk string) error {
	if apk == "" {
		return fmt.Errorf("APK path is required")
	}
	result := mobileutil.RunADB(ctx, serial, "install", "-r", apk)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return fmt.Errorf("adb install: %w (%s)", result.Err, string(combined))
	}
	return emit(rc, map[string]any{
		"operation": "install",
		"platform":  "android",
		"app":       apk,
		"success":   true,
	})
}

func androidLaunch(ctx context.Context, rc *skillmain.RunContext, serial, pkg string) error {
	if pkg == "" {
		return fmt.Errorf("package name is required")
	}
	// Try to resolve main activity
	component := mobileutil.ResolveAndroidLaunchComponent(ctx, serial, pkg, "")
	result := mobileutil.RunADB(ctx, serial, "shell", "am", "start", "-n", component)
	combined := append(append([]byte{}, result.Stdout...), result.Stderr...)
	if result.Err != nil {
		return fmt.Errorf("adb am start: %w (%s)", result.Err, string(combined))
	}
	return emit(rc, map[string]any{
		"operation": "launch",
		"platform":  "android",
		"app":       pkg,
		"component": component,
		"success":   true,
	})
}

func androidTerminate(ctx context.Context, rc *skillmain.RunContext, serial, pkg string) error {
	if pkg == "" {
		return fmt.Errorf("package name is required")
	}
	result := mobileutil.RunADB(ctx, serial, "shell", "am", "force-stop", pkg)
	if result.Err != nil {
		return fmt.Errorf("adb am force-stop: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "terminate",
		"platform":  "android",
		"app":       pkg,
		"success":   true,
	})
}

func androidScreenshot(ctx context.Context, rc *skillmain.RunContext, serial, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/android_screenshot_%d.png", time.Now().UnixNano())
	}
	result := mobileutil.RunADB(ctx, serial, "exec-out", "screencap", "-p")
	if result.Err != nil {
		return fmt.Errorf("adb screencap: %w", result.Err)
	}
	_ = os.WriteFile(outputPath, result.Stdout, 0o644) // Best-effort local copy
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(result.Stdout), "image/png", "mobile_screenshot")
	if err != nil {
		return fmt.Errorf("persist screenshot: %w", err)
	}
	return emit(rc, map[string]any{
		"operation":  "screenshot",
		"platform":   "android",
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"success":    true,
	})
}

func androidTap(ctx context.Context, rc *skillmain.RunContext, serial string, x, y int) error {
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if result.Err != nil {
		return fmt.Errorf("adb input tap: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "tap",
		"platform":  "android",
		"x":         x,
		"y":         y,
		"success":   true,
	})
}

func androidSwipe(ctx context.Context, rc *skillmain.RunContext, serial string, x1, y1, x2, y2 int) error {
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2), "300")
	if result.Err != nil {
		return fmt.Errorf("adb input swipe: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "swipe",
		"platform":  "android",
		"from":      map[string]int{"x": x1, "y": y1},
		"to":        map[string]int{"x": x2, "y": y2},
		"success":   true,
	})
}

func androidTypeText(ctx context.Context, rc *skillmain.RunContext, serial, text string) error {
	if text == "" {
		return fmt.Errorf("text is required")
	}
	// ADB input text requires spaces as %s and special chars escaped.
	result := mobileutil.RunADB(ctx, serial, "shell", "input", "text", mobileutil.EscapeADBInputText(text))
	if result.Err != nil {
		return fmt.Errorf("adb input text: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "type_text",
		"platform":  "android",
		"text":      text,
		"success":   true,
	})
}

// UINode for Android XML parsing.
type UINode struct {
	XMLName     xml.Name `xml:"node"`
	Text        string   `xml:"text,attr"`
	ResourceID  string   `xml:"resource-id,attr"`
	Class       string   `xml:"class,attr"`
	ContentDesc string   `xml:"content-desc,attr"`
	Clickable   string   `xml:"clickable,attr"`
	Enabled     string   `xml:"enabled,attr"`
	Bounds      string   `xml:"bounds,attr"`
	Children    []UINode `xml:"node"`
}

func androidUITree(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	remotePath := "/sdcard/window_dump.xml"
	cmdResult := mobileutil.RunADB(ctx, serial, "shell", "uiautomator", "dump", remotePath)
	combined := append(append([]byte{}, cmdResult.Stdout...), cmdResult.Stderr...)
	if cmdResult.Err != nil {
		return fmt.Errorf("uiautomator dump: %w (%s)", cmdResult.Err, string(combined))
	}
	cmdResult = mobileutil.RunADB(ctx, serial, "shell", "cat", remotePath)
	if cmdResult.Err != nil {
		return fmt.Errorf("read ui dump: %w", cmdResult.Err)
	}
	_ = mobileutil.RunADB(ctx, serial, "shell", "rm", remotePath).Err // Best-effort cleanup

	// Parse XML
	var elements []map[string]any
	var root struct {
		Nodes []UINode `xml:"node"`
	}
	if xml.Unmarshal(cmdResult.Stdout, &root) == nil {
		var flatten func([]UINode)
		flatten = func(nodes []UINode) {
			for _, n := range nodes {
				elements = append(elements, map[string]any{
					"class":        n.Class,
					"text":         n.Text,
					"resource_id":  n.ResourceID,
					"content_desc": n.ContentDesc,
					"bounds":       n.Bounds,
					"clickable":    n.Clickable == "true",
					"enabled":      n.Enabled == "true",
				})
				flatten(n.Children)
			}
		}
		flatten(root.Nodes)
	}

	const maxPreview = 30
	preview := elements
	truncated := len(elements) > maxPreview
	if truncated {
		preview = elements[:maxPreview]
	}
	payload := map[string]any{
		"operation": "ui_tree",
		"platform":  "android",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}
	if truncated {
		if artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(cmdResult.Stdout), "application/xml", "mobile_ui_tree"); err == nil {
			payload["artifact"] = artifact.Digest
		}
	}
	return emit(rc, payload)
}

func androidLogs(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	result := mobileutil.RunADB(ctx, serial, "logcat", "-d", "-t", "500")
	if result.Err != nil {
		return fmt.Errorf("adb logcat: %w", result.Err)
	}
	artifact, _ := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(result.Stdout), "text/plain", "mobile_logs")
	lines := strings.Split(string(result.Stdout), "\n")
	preview := lines
	if len(lines) > 50 {
		preview = lines[len(lines)-50:]
	}
	return emit(rc, map[string]any{
		"operation": "logs",
		"platform":  "android",
		"artifact":  artifact.Digest,
		"preview":   strings.Join(preview, "\n"),
	})
}

func androidOpenURL(ctx context.Context, rc *skillmain.RunContext, serial, url string) error {
	if url == "" {
		return fmt.Errorf("url is required")
	}
	result := mobileutil.RunADB(ctx, serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	if result.Err != nil {
		return fmt.Errorf("adb am start VIEW: %w", result.Err)
	}
	return emit(rc, map[string]any{
		"operation": "open_url",
		"platform":  "android",
		"url":       url,
		"success":   true,
	})
}

// ==================== Helpers ====================

func emit(rc *skillmain.RunContext, data map[string]any) error {
	return skillout.Emit(rc, command, data)
}
