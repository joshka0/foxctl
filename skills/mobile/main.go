// Package main implements the unified mobile skill that dispatches to iOS or Android.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "mobile"

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

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults (moved from parseInput)
	if in.Operation == "" {
		return fmt.Errorf("operation is required")
	}
	if in.Platform == "" {
		in.Platform = "auto"
	}
	// Check tool availability
	hasIDB := checkTool("idb")
	hasADB := checkTool("adb")

	if !hasIDB && !hasADB {
		return fmt.Errorf("neither idb nor adb found: install idb (brew install idb-companion) or adb (Android SDK)")
	}

	// Handle auto mode for list_devices
	if in.Platform == "auto" {
		if in.Operation == "list_devices" {
			return listAllDevices(ctx, rc, hasIDB, hasADB)
		}
		return fmt.Errorf("platform is required for %s operation (use 'ios' or 'android')", in.Operation)
	}

	// Validate platform availability
	if in.Platform == "ios" && !hasIDB {
		return fmt.Errorf("idb not found: install with 'brew install idb-companion'")
	}
	if in.Platform == "android" && !hasADB {
		return fmt.Errorf("adb not found: install Android SDK or 'brew install android-platform-tools'")
	}

	// Dispatch to platform-specific implementation
	switch in.Platform {
	case "ios":
		return runIOS(ctx, rc, in)
	case "android":
		return runAndroid(ctx, rc, in)
	default:
		return fmt.Errorf("unknown platform: %s (use 'ios', 'android', or 'auto')", in.Platform)
	}
}

func checkTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// listAllDevices lists devices from both iOS and Android.
func listAllDevices(ctx context.Context, rc *skillmain.RunContext, hasIDB, hasADB bool) error {
	var allDevices []UnifiedDevice

	// Get iOS simulators
	if hasIDB {
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
	cmd := exec.CommandContext(ctx, "idb", "list-targets", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var devices []UnifiedDevice
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw struct {
			UDID       string `json:"udid"`
			Name       string `json:"name"`
			State      string `json:"state"`
			TargetType string `json:"target_type"`
			OSVersion  string `json:"os_version"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.TargetType != "simulator" {
			continue
		}
		devices = append(devices, UnifiedDevice{
			Platform: "ios",
			ID:       raw.UDID,
			Name:     raw.Name,
			State:    raw.State,
			OS:       raw.OSVersion,
		})
	}
	return devices, nil
}

func listAndroidDevices(ctx context.Context) ([]UnifiedDevice, error) {
	cmd := exec.CommandContext(ctx, "adb", "devices", "-l")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var devices []UnifiedDevice
	lines := strings.Split(string(output), "\n")
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// Only include emulators
		if !strings.HasPrefix(parts[0], "emulator-") {
			continue
		}

		dev := UnifiedDevice{
			Platform: "android",
			ID:       parts[0],
			State:    parts[1],
		}
		for _, part := range parts[2:] {
			if strings.HasPrefix(part, "model:") {
				dev.Model = strings.TrimPrefix(part, "model:")
				dev.Name = dev.Model
			}
		}
		if dev.Name == "" {
			dev.Name = dev.ID
		}
		devices = append(devices, dev)
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

func idbCommand(ctx context.Context, udid string, args ...string) *exec.Cmd {
	if udid != "" {
		args = append(args, "--udid", udid)
	}
	return exec.CommandContext(ctx, "idb", args...)
}

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
	cmd := idbCommand(ctx, udid, "describe", "--json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb describe: %w", err)
	}
	var device map[string]any
	_ = json.Unmarshal(output, &device) // Best-effort parse
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
	cmd := idbCommand(ctx, udid, "install", app)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb install: %w", err)
	}
	return emit(rc, map[string]any{
		"operation": "install",
		"platform":  "ios",
		"app":       app,
		"success":   true,
	})
}

func iosLaunch(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("bundle ID is required")
	}
	cmd := idbCommand(ctx, udid, "launch", bundleID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb launch: %w", err)
	}
	return emit(rc, map[string]any{
		"operation": "launch",
		"platform":  "ios",
		"app":       bundleID,
		"success":   true,
	})
}

func iosTerminate(ctx context.Context, rc *skillmain.RunContext, udid, bundleID string) error {
	if bundleID == "" {
		return fmt.Errorf("bundle ID is required")
	}
	cmd := idbCommand(ctx, udid, "terminate", bundleID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb terminate: %w", err)
	}
	return emit(rc, map[string]any{
		"operation": "terminate",
		"platform":  "ios",
		"app":       bundleID,
		"success":   true,
	})
}

func iosScreenshot(ctx context.Context, rc *skillmain.RunContext, udid, outputPath string) error {
	if outputPath == "" {
		outputPath = fmt.Sprintf("/tmp/ios_screenshot_%d.png", time.Now().UnixNano())
	}
	cmd := idbCommand(ctx, udid, "screenshot", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb screenshot: %w", err)
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
		"screenshot": artifact.Digest,
		"path":       outputPath,
		"success":    true,
	})
}

func iosTap(ctx context.Context, rc *skillmain.RunContext, udid string, x, y int) error {
	cmd := idbCommand(ctx, udid, "ui", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui tap: %w", err)
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
	cmd := idbCommand(ctx, udid, "ui", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui swipe: %w", err)
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
	cmd := idbCommand(ctx, udid, "ui", "text", text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb ui text: %w", err)
	}
	return emit(rc, map[string]any{
		"operation": "type_text",
		"platform":  "ios",
		"text":      text,
		"success":   true,
	})
}

func iosUITree(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	cmd := idbCommand(ctx, udid, "ui", "describe-all", "--json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("idb ui describe-all: %w", err)
	}
	var elements []any
	_ = json.Unmarshal(output, &elements) // Try as JSON array first
	if elements == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
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
	result := map[string]any{
		"operation": "ui_tree",
		"platform":  "ios",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}
	if truncated {
		if artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/json", "mobile_ui_tree"); err == nil {
			result["artifact"] = artifact.Digest
		}
	}
	return emit(rc, result)
}

func iosLogs(ctx context.Context, rc *skillmain.RunContext, udid string) error {
	logCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := idbCommand(logCtx, udid, "log", "--style", "json")
	output, _ := cmd.Output()                                                                                    // Timeout expected, ignore error
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
	cmd := idbCommand(ctx, udid, "open", url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("idb open: %w", err)
	}
	return emit(rc, map[string]any{
		"operation": "open_url",
		"platform":  "ios",
		"url":       url,
		"success":   true,
	})
}

// ==================== Android Operations ====================

func adbCommand(ctx context.Context, serial string, args ...string) *exec.Cmd {
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	return exec.CommandContext(ctx, "adb", args...)
}

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
	properties := make(map[string]string)
	props := []string{
		"ro.product.model", "ro.product.brand", "ro.build.version.release",
		"ro.build.version.sdk", "ro.product.cpu.abi",
	}
	for _, prop := range props {
		cmd := adbCommand(ctx, serial, "shell", "getprop", prop)
		if output, err := cmd.Output(); err == nil {
			properties[prop] = strings.TrimSpace(string(output))
		}
	}
	cmd := adbCommand(ctx, serial, "shell", "wm", "size")
	if output, err := cmd.Output(); err == nil {
		if m := regexp.MustCompile(`(\d+x\d+)`).FindString(string(output)); m != "" {
			properties["screen_size"] = m
		}
	}
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
	cmd := adbCommand(ctx, serial, "install", "-r", apk)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb install: %w (%s)", err, string(output))
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
	cmd := adbCommand(ctx, serial, "shell", "cmd", "package", "resolve-activity", "--brief", pkg)
	component := pkg + "/.MainActivity"
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) > 0 {
			component = lines[len(lines)-1]
		}
	}
	cmd = adbCommand(ctx, serial, "shell", "am", "start", "-n", component)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adb am start: %w (%s)", err, string(output))
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
	cmd := adbCommand(ctx, serial, "shell", "am", "force-stop", pkg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb am force-stop: %w", err)
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
	cmd := adbCommand(ctx, serial, "exec-out", "screencap", "-p")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb screencap: %w", err)
	}
	_ = os.WriteFile(outputPath, output, 0o644) // Best-effort local copy
	artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "image/png", "mobile_screenshot")
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
	cmd := adbCommand(ctx, serial, "shell", "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input tap: %w", err)
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
	cmd := adbCommand(ctx, serial, "shell", "input", "swipe",
		strconv.Itoa(x1), strconv.Itoa(y1),
		strconv.Itoa(x2), strconv.Itoa(y2), "300")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb input swipe: %w", err)
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
	// ADB input text requires spaces as %s and special chars escaped
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
	cmd := adbCommand(ctx, serial, "shell", "uiautomator", "dump", remotePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("uiautomator dump: %w (%s)", err, string(output))
	}
	cmd = adbCommand(ctx, serial, "shell", "cat", remotePath)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read ui dump: %w", err)
	}
	_ = adbCommand(ctx, serial, "shell", "rm", remotePath).Run() // Best-effort cleanup

	// Parse XML
	var elements []map[string]any
	var root struct {
		Nodes []UINode `xml:"node"`
	}
	if xml.Unmarshal(output, &root) == nil {
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
	result := map[string]any{
		"operation": "ui_tree",
		"platform":  "android",
		"elements":  preview,
		"count":     len(elements),
		"truncated": truncated,
	}
	if truncated {
		if artifact, err := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "application/xml", "mobile_ui_tree"); err == nil {
			result["artifact"] = artifact.Digest
		}
	}
	return emit(rc, result)
}

func androidLogs(ctx context.Context, rc *skillmain.RunContext, serial string) error {
	cmd := adbCommand(ctx, serial, "logcat", "-d", "-t", "500")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("adb logcat: %w", err)
	}
	artifact, _ := skillout.PersistBuffer(ctx, rc, bytes.NewBuffer(output), "text/plain", "mobile_logs")
	lines := strings.Split(string(output), "\n")
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
	cmd := adbCommand(ctx, serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("adb am start VIEW: %w", err)
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
