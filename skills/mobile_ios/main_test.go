package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "mobile/ios", command)
}

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{
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
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 26)
}

func TestRecordPIDFile(t *testing.T) {
	// Default device uses "default" suffix
	assert.Equal(t, "/tmp/agentctl_ios_record_default.pid", recordPIDFile(""))

	// Specific device UDID is used as suffix
	assert.Equal(t, "/tmp/agentctl_ios_record_12345-ABCDE.pid", recordPIDFile("12345-ABCDE"))
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Operation:   "tap",
		UDID:        "UDID-12345",
		App:         "com.example.app",
		X:           100,
		Y:           200,
		X2:          300,
		Y2:          400,
		Text:        "hello world",
		URL:         "https://example.com",
		Button:      "HOME",
		Permissions: []string{"photos", "camera"},
		Lat:         37.7749,
		Long:        -122.4194,
		Output:      "/tmp/screenshot.png",
		ExpoURL:     "exp://localhost:8081",
		MediaPath:   "/path/to/media.jpg",
	}

	assert.Equal(t, "tap", in.Operation)
	assert.Equal(t, "UDID-12345", in.UDID)
	assert.Equal(t, "com.example.app", in.App)
	assert.Equal(t, 100, in.X)
	assert.Equal(t, 200, in.Y)
	assert.Equal(t, 300, in.X2)
	assert.Equal(t, 400, in.Y2)
	assert.Equal(t, "hello world", in.Text)
	assert.Equal(t, "https://example.com", in.URL)
	assert.Equal(t, "HOME", in.Button)
	assert.Len(t, in.Permissions, 2)
	assert.Equal(t, 37.7749, in.Lat)
	assert.Equal(t, -122.4194, in.Long)
	assert.Equal(t, "/tmp/screenshot.png", in.Output)
	assert.Equal(t, "exp://localhost:8081", in.ExpoURL)
	assert.Equal(t, "/path/to/media.jpg", in.MediaPath)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Operation: "screenshot",
		UDID:      "ABCD-1234-5678-EFGH",
		Output:    "/tmp/test.png",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.UDID, decoded.UDID)
	assert.Equal(t, in.Output, decoded.Output)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Operation)
	assert.Empty(t, in.UDID)
	assert.Empty(t, in.App)
	assert.Zero(t, in.X)
	assert.Zero(t, in.Y)
	assert.Zero(t, in.X2)
	assert.Zero(t, in.Y2)
	assert.Empty(t, in.Text)
	assert.Empty(t, in.URL)
	assert.Empty(t, in.Button)
	assert.Nil(t, in.Permissions)
	assert.Zero(t, in.Lat)
	assert.Zero(t, in.Long)
	assert.Empty(t, in.Output)
	assert.Empty(t, in.ExpoURL)
	assert.Empty(t, in.MediaPath)
}

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Operation:   "tap",
		UDID:        "udid",
		App:         "app",
		X:           1,
		Y:           2,
		X2:          3,
		Y2:          4,
		Text:        "t",
		URL:         "u",
		Button:      "b",
		Permissions: []string{"p"},
		Lat:         1.0,
		Long:        2.0,
		Output:      "o",
		ExpoURL:     "e",
		MediaPath:   "m",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "udid")
	assert.Contains(t, jsonStr, "app")
	assert.Contains(t, jsonStr, `"x":`)
	assert.Contains(t, jsonStr, `"y":`)
	assert.Contains(t, jsonStr, "x2")
	assert.Contains(t, jsonStr, "y2")
	assert.Contains(t, jsonStr, "text")
	assert.Contains(t, jsonStr, "url")
	assert.Contains(t, jsonStr, "button")
	assert.Contains(t, jsonStr, "permissions")
	assert.Contains(t, jsonStr, "lat")
	assert.Contains(t, jsonStr, "long")
	assert.Contains(t, jsonStr, "output")
	assert.Contains(t, jsonStr, "expo_url")
	assert.Contains(t, jsonStr, "media_path")
}

func TestInput_OmitEmptyFields(t *testing.T) {
	in := input{
		Operation: "list_devices",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	// Only operation should be present (others are omitempty)
	assert.Contains(t, jsonStr, "operation")
	assert.NotContains(t, jsonStr, "udid")
	assert.NotContains(t, jsonStr, "app")
}

func TestInput_PermissionsArray(t *testing.T) {
	in := input{
		Operation:   "approve_permissions",
		App:         "com.example.app",
		Permissions: []string{"photos", "camera", "location"},
	}

	assert.Len(t, in.Permissions, 3)
	assert.Contains(t, in.Permissions, "photos")
	assert.Contains(t, in.Permissions, "camera")
	assert.Contains(t, in.Permissions, "location")
}

func TestInput_LocationCoordinates(t *testing.T) {
	// San Francisco coordinates
	in := input{
		Operation: "set_location",
		Lat:       37.7749,
		Long:      -122.4194,
	}

	assert.Equal(t, 37.7749, in.Lat)
	assert.Equal(t, -122.4194, in.Long)
}

func TestInput_SwipeCoordinates(t *testing.T) {
	in := input{
		Operation: "swipe",
		X:         0,
		Y:         500,
		X2:        0,
		Y2:        100,
	}

	// Swipe from (0, 500) to (0, 100) - a scroll up gesture
	assert.Equal(t, 0, in.X)
	assert.Equal(t, 500, in.Y)
	assert.Equal(t, 0, in.X2)
	assert.Equal(t, 100, in.Y2)
}

func TestInput_TapCoordinates(t *testing.T) {
	in := input{
		Operation: "tap",
		X:         195,
		Y:         422,
	}

	assert.Equal(t, 195, in.X)
	assert.Equal(t, 422, in.Y)
}

func TestInput_ButtonValues(t *testing.T) {
	buttons := []string{"HOME", "LOCK", "SIRI", "APPLE_PAY", "VOLUME_UP", "VOLUME_DOWN"}

	for _, btn := range buttons {
		in := input{Button: btn}
		assert.Equal(t, btn, in.Button)
	}
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Operation:   "approve_permissions",
		UDID:        "full-udid",
		App:         "com.full.app",
		X:           10,
		Y:           20,
		X2:          30,
		Y2:          40,
		Text:        "full text",
		URL:         "https://full.com",
		Button:      "HOME",
		Permissions: []string{"photos", "camera"},
		Lat:         37.7749,
		Long:        -122.4194,
		Output:      "/full/output.png",
		ExpoURL:     "exp://full",
		MediaPath:   "/full/media.jpg",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.UDID, decoded.UDID)
	assert.Equal(t, in.App, decoded.App)
	assert.Equal(t, in.X, decoded.X)
	assert.Equal(t, in.Y, decoded.Y)
	assert.Equal(t, in.X2, decoded.X2)
	assert.Equal(t, in.Y2, decoded.Y2)
	assert.Equal(t, in.Text, decoded.Text)
	assert.Equal(t, in.URL, decoded.URL)
	assert.Equal(t, in.Button, decoded.Button)
	assert.Equal(t, in.Permissions, decoded.Permissions)
	assert.Equal(t, in.Lat, decoded.Lat)
	assert.Equal(t, in.Long, decoded.Long)
	assert.Equal(t, in.Output, decoded.Output)
	assert.Equal(t, in.ExpoURL, decoded.ExpoURL)
	assert.Equal(t, in.MediaPath, decoded.MediaPath)
}

func TestInput_ExpoOperations(t *testing.T) {
	// expo_deep_link
	in1 := input{
		Operation: "expo_deep_link",
		ExpoURL:   "exp://192.168.1.100:8081",
	}
	assert.Equal(t, "expo_deep_link", in1.Operation)
	assert.Equal(t, "exp://192.168.1.100:8081", in1.ExpoURL)

	// expo_reload
	in2 := input{
		Operation: "expo_reload",
	}
	assert.Equal(t, "expo_reload", in2.Operation)
}

func TestInput_DescribePoint(t *testing.T) {
	in := input{
		Operation: "describe_point",
		X:         200,
		Y:         300,
	}

	assert.Equal(t, "describe_point", in.Operation)
	assert.Equal(t, 200, in.X)
	assert.Equal(t, 300, in.Y)
}

func TestInput_RecordOperations(t *testing.T) {
	// record_start
	in1 := input{
		Operation: "record_start",
		Output:    "/tmp/recording.mp4",
	}
	assert.Equal(t, "record_start", in1.Operation)
	assert.Equal(t, "/tmp/recording.mp4", in1.Output)

	// record_stop (no additional params needed)
	in2 := input{
		Operation: "record_stop",
	}
	assert.Equal(t, "record_stop", in2.Operation)
}

func TestAllowedOps_ContainsExpoOperations(t *testing.T) {
	assert.Contains(t, allowedOps, "expo_deep_link")
	assert.Contains(t, allowedOps, "expo_reload")
}

func TestAllowedOps_ContainsShake(t *testing.T) {
	assert.Contains(t, allowedOps, "shake")
}

func TestAllowedOps_ContainsFocus(t *testing.T) {
	assert.Contains(t, allowedOps, "focus")
}

func TestAllowedOps_ContainsClearKeychain(t *testing.T) {
	assert.Contains(t, allowedOps, "clear_keychain")
}

func TestAllowedOps_ContainsAddMedia(t *testing.T) {
	assert.Contains(t, allowedOps, "add_media")
}

func TestAllowedOps_ContainsCrashLogs(t *testing.T) {
	assert.Contains(t, allowedOps, "crash_logs")
}
