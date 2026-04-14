package mobileutil

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
)

// ADBArgs builds adb arguments with an optional device serial.
func ADBArgs(serial string, args ...string) []string {
	if serial != "" {
		args = append([]string{"-s", serial}, args...)
	}
	return args
}

// IDBArgs builds idb arguments with an optional device UDID.
func IDBArgs(udid string, args ...string) []string {
	if udid != "" {
		args = append(args, "--udid", udid)
	}
	return args
}

// RunADB executes adb with the optional device serial.
func RunADB(ctx context.Context, serial string, args ...string) executil.CmdResult {
	return executil.Run(ctx, "", "adb", ADBArgs(serial, args...)...)
}

// RunIDB executes idb with the optional device UDID.
func RunIDB(ctx context.Context, udid string, args ...string) executil.CmdResult {
	path, err := executil.ResolveRunnableTool(ctx, "idb", "--help")
	if err != nil {
		return executil.CmdResult{ExitCode: -1, Err: err}
	}
	return executil.Run(ctx, "", path, IDBArgs(udid, args...)...)
}

// RunSimctl executes xcrun simctl with the provided arguments.
func RunSimctl(ctx context.Context, args ...string) executil.CmdResult {
	return executil.Run(ctx, "", "xcrun", append([]string{"simctl"}, args...)...)
}

// HasRunnableIDB returns true if idb resolves in PATH and can be launched.
func HasRunnableIDB(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return executil.HasRunnableTool(probeCtx, "idb", "--help")
}

// ADBDevice represents an Android device from adb devices -l.
type ADBDevice struct {
	Serial      string            `json:"serial"`
	State       string            `json:"state"`
	Product     string            `json:"product,omitempty"`
	Model       string            `json:"model,omitempty"`
	Device      string            `json:"device,omitempty"`
	TransportID string            `json:"transport_id,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

// ParseADBDevices parses adb devices output into a list of devices.
func ParseADBDevices(output []byte) []ADBDevice {
	lines := strings.Split(string(output), "\n")
	if len(lines) <= 1 {
		return nil
	}

	devices := make([]ADBDevice, 0)
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
		devices = append(devices, dev)
	}

	return devices
}

// ListADBDevices lists connected Android devices via adb.
func ListADBDevices(ctx context.Context) ([]ADBDevice, error) {
	result := RunADB(ctx, "", "devices", "-l")
	if result.Err != nil {
		return nil, result.Err
	}
	return ParseADBDevices(result.Stdout), nil
}

// IDBDevice represents an iOS simulator/device from idb list-targets.
type IDBDevice struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	State      string `json:"state"`
	TargetType string `json:"target_type"`
	OSVersion  string `json:"os_version"`
	Arch       string `json:"architecture"`
}

type idbTarget struct {
	UDID       string `json:"udid"`
	Name       string `json:"name"`
	State      string `json:"state"`
	TargetType string `json:"target_type"`
	Type       string `json:"type"`
	OSVersion  string `json:"os_version"`
	Arch       string `json:"architecture"`
}

// ParseIDBDevices parses idb list-targets output into a list of devices.
func ParseIDBDevices(output []byte) []IDBDevice {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")

	devices := make([]IDBDevice, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		var raw idbTarget
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		targetType := raw.TargetType
		if targetType == "" {
			targetType = raw.Type
		}
		devices = append(devices, IDBDevice{
			UDID:       raw.UDID,
			Name:       raw.Name,
			State:      raw.State,
			TargetType: targetType,
			OSVersion:  raw.OSVersion,
			Arch:       raw.Arch,
		})
	}

	return devices
}

// ListIDBDevices lists iOS targets via idb list-targets.
func ListIDBDevices(ctx context.Context) ([]IDBDevice, error) {
	result := RunIDB(ctx, "", "list-targets", "--json")
	if result.Err != nil {
		return nil, result.Err
	}
	return ParseIDBDevices(result.Stdout), nil
}

type simctlList struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

type simctlDevice struct {
	UDID              string `json:"udid"`
	Name              string `json:"name"`
	State             string `json:"state"`
	IsAvailable       bool   `json:"isAvailable"`
	AvailabilityError string `json:"availabilityError"`
}

// ParseSimctlDevices parses `xcrun simctl list devices --json` output into a normalized device list.
func ParseSimctlDevices(output []byte) []IDBDevice {
	var payload simctlList
	if err := json.Unmarshal(output, &payload); err != nil {
		return nil
	}

	devices := make([]IDBDevice, 0)
	for runtime, runtimeDevices := range payload.Devices {
		for _, dev := range runtimeDevices {
			if !dev.IsAvailable && strings.TrimSpace(dev.AvailabilityError) != "" {
				continue
			}
			devices = append(devices, IDBDevice{
				UDID:       dev.UDID,
				Name:       dev.Name,
				State:      strings.ToLower(strings.TrimSpace(dev.State)),
				TargetType: "simulator",
				OSVersion:  formatSimctlRuntime(runtime),
			})
		}
	}

	sort.Slice(devices, func(i, j int) bool {
		if devices[i].State != devices[j].State {
			return devices[i].State < devices[j].State
		}
		if devices[i].OSVersion != devices[j].OSVersion {
			return devices[i].OSVersion < devices[j].OSVersion
		}
		return devices[i].Name < devices[j].Name
	})
	return devices
}

// ListSimctlDevices lists available iOS simulators via simctl.
func ListSimctlDevices(ctx context.Context) ([]IDBDevice, error) {
	result := RunSimctl(ctx, "list", "devices", "available", "--json")
	if result.Err != nil {
		return nil, result.Err
	}
	return ParseSimctlDevices(result.Stdout), nil
}

func formatSimctlRuntime(runtime string) string {
	runtime = strings.TrimPrefix(runtime, "com.apple.CoreSimulator.SimRuntime.")
	if !strings.HasPrefix(runtime, "iOS-") && !strings.HasPrefix(runtime, "tvOS-") && !strings.HasPrefix(runtime, "watchOS-") && !strings.HasPrefix(runtime, "visionOS-") {
		return runtime
	}
	parts := strings.Split(runtime, "-")
	if len(parts) < 2 {
		return runtime
	}
	return parts[0] + " " + strings.Join(parts[1:], ".")
}

var (
	adbScreenSizeRegex = regexp.MustCompile(`(\d+x\d+)`)
	adbDensityRegex    = regexp.MustCompile(`(\d+)`)
)

// CollectADBProperties fetches the provided getprop keys and optional screen/density values.
func CollectADBProperties(ctx context.Context, serial string, props []string, includeScreenSize, includeDensity bool) map[string]string {
	properties := make(map[string]string, len(props)+2)
	for _, prop := range props {
		result := RunADB(ctx, serial, "shell", "getprop", prop)
		if result.Err == nil {
			properties[prop] = strings.TrimSpace(string(result.Stdout))
		}
	}

	if includeScreenSize {
		result := RunADB(ctx, serial, "shell", "wm", "size")
		if result.Err == nil {
			if matches := adbScreenSizeRegex.FindString(string(result.Stdout)); matches != "" {
				properties["screen_size"] = matches
			}
		}
	}

	if includeDensity {
		result := RunADB(ctx, serial, "shell", "wm", "density")
		if result.Err == nil {
			if matches := adbDensityRegex.FindString(string(result.Stdout)); matches != "" {
				properties["density"] = matches
			}
		}
	}

	return properties
}

// ResolveAndroidLaunchComponent returns the component string for launching an Android app.
func ResolveAndroidLaunchComponent(ctx context.Context, serial, pkg, activity string) string {
	if activity != "" {
		return pkg + "/" + activity
	}

	result := RunADB(ctx, serial, "shell", "cmd", "package", "resolve-activity", "--brief", pkg)
	if result.Err == nil {
		lines := strings.Split(strings.TrimSpace(string(result.Stdout)), "\n")
		if len(lines) > 0 {
			if component := strings.TrimSpace(lines[len(lines)-1]); component != "" {
				return component
			}
		}
	}

	return pkg + "/.MainActivity"
}

// EscapeADBInputText escapes text for "adb shell input text".
func EscapeADBInputText(text string) string {
	// Spaces must be encoded as %s, and special characters escaped.
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
	return escaped.String()
}
