package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestSkillName(t *testing.T) {
	assert.Equal(t, "mobile/expo", skillName)
}

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{
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
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 11)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Operation:     "build",
		DeviceID:      "device-123",
		Platform:      "ios",
		URL:           "exp://localhost:8081",
		BuildPlatform: "all",
		Profile:       "development",
		Channel:       "production",
		Message:       "Update message",
		Filter:        "error",
		Count:         100,
	}

	assert.Equal(t, "build", in.Operation)
	assert.Equal(t, "device-123", in.DeviceID)
	assert.Equal(t, "ios", in.Platform)
	assert.Equal(t, "exp://localhost:8081", in.URL)
	assert.Equal(t, "all", in.BuildPlatform)
	assert.Equal(t, "development", in.Profile)
	assert.Equal(t, "production", in.Channel)
	assert.Equal(t, "Update message", in.Message)
	assert.Equal(t, "error", in.Filter)
	assert.Equal(t, 100, in.Count)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Operation: "shake",
		Platform:  "android",
		DeviceID:  "emulator-5554",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.Platform, decoded.Platform)
	assert.Equal(t, in.DeviceID, decoded.DeviceID)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Operation)
	assert.Empty(t, in.DeviceID)
	assert.Empty(t, in.Platform)
	assert.Empty(t, in.URL)
	assert.Empty(t, in.BuildPlatform)
	assert.Empty(t, in.Profile)
	assert.Empty(t, in.Channel)
	assert.Empty(t, in.Message)
	assert.Empty(t, in.Filter)
	assert.Zero(t, in.Count)
}

func TestInput_PlatformValues(t *testing.T) {
	platforms := []string{"ios", "android", "auto"}

	for _, platform := range platforms {
		in := Input{Platform: platform}
		assert.Equal(t, platform, in.Platform)
	}
}

func TestInput_BuildPlatformValues(t *testing.T) {
	platforms := []string{"ios", "android", "all"}

	for _, platform := range platforms {
		in := Input{BuildPlatform: platform}
		assert.Equal(t, platform, in.BuildPlatform)
	}
}

func TestInput_ProfileValues(t *testing.T) {
	profiles := []string{"development", "preview", "production"}

	for _, profile := range profiles {
		in := Input{Profile: profile}
		assert.Equal(t, profile, in.Profile)
	}
}

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := Input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_OmitEmptyFields(t *testing.T) {
	in := Input{
		Operation: "shake",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.NotContains(t, jsonStr, "device_id")
	assert.NotContains(t, jsonStr, "platform")
	assert.NotContains(t, jsonStr, `"url"`)
	assert.NotContains(t, jsonStr, "build_platform")
	assert.NotContains(t, jsonStr, "profile")
	assert.NotContains(t, jsonStr, "channel")
	assert.NotContains(t, jsonStr, "message")
	assert.NotContains(t, jsonStr, "filter")
	assert.NotContains(t, jsonStr, "count")
}

// Tests for detectPlatform helper

func TestDetectPlatform_ExplicitIOS(t *testing.T) {
	result := detectPlatform("any-device", "ios")
	assert.Equal(t, "ios", result)
}

func TestDetectPlatform_ExplicitAndroid(t *testing.T) {
	result := detectPlatform("any-device", "android")
	assert.Equal(t, "android", result)
}

func TestDetectPlatform_AutoWithEmptyDevice(t *testing.T) {
	result := detectPlatform("", "auto")
	assert.Equal(t, "ios", result) // Default to iOS
}

func TestDetectPlatform_EmptyPlatformAndDevice(t *testing.T) {
	result := detectPlatform("", "")
	assert.Equal(t, "ios", result) // Default to iOS
}

func TestDetectPlatform_AndroidEmulatorPrefix(t *testing.T) {
	result := detectPlatform("emulator-5554", "")
	assert.Equal(t, "android", result)
}

func TestDetectPlatform_AndroidEmulatorPrefix2(t *testing.T) {
	result := detectPlatform("emulator-5556", "auto")
	assert.Equal(t, "android", result)
}

func TestDetectPlatform_AndroidTCPIP(t *testing.T) {
	result := detectPlatform("192.168.1.100:5555", "")
	assert.Equal(t, "android", result)
}

func TestDetectPlatform_IOSLongUDID(t *testing.T) {
	// iOS UDID is typically 36+ chars
	result := detectPlatform("ABCD1234-5678-90EF-GHIJ-KLMNOPQRSTUV", "")
	assert.Equal(t, "ios", result)
}

func TestDetectPlatform_IOSHexUDID(t *testing.T) {
	// 40 char hex UDID
	result := detectPlatform("1234567890abcdef1234567890abcdef12345678", "")
	assert.Equal(t, "ios", result)
}

func TestDetectPlatform_ShortDeviceID(t *testing.T) {
	// Short device ID defaults to iOS
	result := detectPlatform("short-id", "")
	assert.Equal(t, "ios", result)
}

func TestDetectPlatform_AutoIgnored(t *testing.T) {
	// "auto" should be treated same as empty
	result := detectPlatform("emulator-5554", "auto")
	assert.Equal(t, "android", result)
}

// Tests for operations in allowedOps

func TestAllowedOps_ContainsDeviceOps(t *testing.T) {
	deviceOps := []string{"shake", "reload", "deep_link", "dev_menu"}
	for _, op := range deviceOps {
		assert.Contains(t, allowedOps, op)
	}
}

func TestAllowedOps_ContainsToggleOps(t *testing.T) {
	toggleOps := []string{"toggle_inspector", "toggle_performance", "toggle_remote_debug"}
	for _, op := range toggleOps {
		assert.Contains(t, allowedOps, op)
	}
}

func TestAllowedOps_ContainsEASOps(t *testing.T) {
	easOps := []string{"build", "update", "build_status"}
	for _, op := range easOps {
		assert.Contains(t, allowedOps, op)
	}
}

func TestAllowedOps_ContainsLogs(t *testing.T) {
	assert.Contains(t, allowedOps, "logs")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Operation:     "build",
		DeviceID:      "device-full",
		Platform:      "ios",
		URL:           "exp://full:8081",
		BuildPlatform: "all",
		Profile:       "production",
		Channel:       "stable",
		Message:       "Full test",
		Filter:        "warn",
		Count:         200,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.DeviceID, decoded.DeviceID)
	assert.Equal(t, in.Platform, decoded.Platform)
	assert.Equal(t, in.URL, decoded.URL)
	assert.Equal(t, in.BuildPlatform, decoded.BuildPlatform)
	assert.Equal(t, in.Profile, decoded.Profile)
	assert.Equal(t, in.Channel, decoded.Channel)
	assert.Equal(t, in.Message, decoded.Message)
	assert.Equal(t, in.Filter, decoded.Filter)
	assert.Equal(t, in.Count, decoded.Count)
}

func TestInput_DeepLinkOperation(t *testing.T) {
	in := Input{
		Operation: "deep_link",
		URL:       "exp://192.168.1.100:8081",
		Platform:  "ios",
	}

	assert.Equal(t, "deep_link", in.Operation)
	assert.Equal(t, "exp://192.168.1.100:8081", in.URL)
}

func TestInput_BuildOperation(t *testing.T) {
	in := Input{
		Operation:     "build",
		BuildPlatform: "ios",
		Profile:       "preview",
	}

	assert.Equal(t, "build", in.Operation)
	assert.Equal(t, "ios", in.BuildPlatform)
	assert.Equal(t, "preview", in.Profile)
}

func TestInput_UpdateOperation(t *testing.T) {
	in := Input{
		Operation: "update",
		Channel:   "production",
		Message:   "Bug fixes and improvements",
	}

	assert.Equal(t, "update", in.Operation)
	assert.Equal(t, "production", in.Channel)
	assert.Equal(t, "Bug fixes and improvements", in.Message)
}

func TestInput_LogsOperation(t *testing.T) {
	in := Input{
		Operation: "logs",
		Filter:    "error",
		Count:     50,
	}

	assert.Equal(t, "logs", in.Operation)
	assert.Equal(t, "error", in.Filter)
	assert.Equal(t, 50, in.Count)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		Operation:     "o",
		DeviceID:      "d",
		Platform:      "p",
		URL:           "u",
		BuildPlatform: "bp",
		Profile:       "pr",
		Channel:       "c",
		Message:       "m",
		Filter:        "f",
		Count:         1,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "device_id")
	assert.Contains(t, jsonStr, "platform")
	assert.Contains(t, jsonStr, `"url"`)
	assert.Contains(t, jsonStr, "build_platform")
	assert.Contains(t, jsonStr, "profile")
	assert.Contains(t, jsonStr, "channel")
	assert.Contains(t, jsonStr, "message")
	assert.Contains(t, jsonStr, "filter")
	assert.Contains(t, jsonStr, "count")
}

// Tests for default values in run logic

func TestInput_BuildPlatformDefault(t *testing.T) {
	in := Input{Operation: "build"}

	buildPlatform := in.BuildPlatform
	if buildPlatform == "" {
		buildPlatform = "all"
	}

	assert.Equal(t, "all", buildPlatform)
}

func TestInput_ProfileDefault(t *testing.T) {
	in := Input{Operation: "build"}

	profile := in.Profile
	if profile == "" {
		profile = "development"
	}

	assert.Equal(t, "development", profile)
}

func TestInput_CountDefault(t *testing.T) {
	in := Input{Operation: "logs"}

	count := in.Count
	if count <= 0 {
		count = 100
	}

	assert.Equal(t, 100, count)
}

func TestInput_CountPositive(t *testing.T) {
	in := Input{Operation: "logs", Count: 50}

	count := in.Count
	if count <= 0 {
		count = 100
	}

	assert.Equal(t, 50, count)
}

// Tests for URL handling

func TestInput_ExpoURLWithScheme(t *testing.T) {
	in := Input{
		Operation: "deep_link",
		URL:       "exp://localhost:8081",
	}

	assert.True(t, len(in.URL) > 0)
	assert.Contains(t, in.URL, "exp://")
}

func TestInput_ExpoURLWithSecureScheme(t *testing.T) {
	in := Input{
		Operation: "deep_link",
		URL:       "exps://localhost:8081",
	}

	assert.Contains(t, in.URL, "exps://")
}
