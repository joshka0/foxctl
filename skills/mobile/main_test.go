package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "mobile", command)
}

func TestAllowedOps(t *testing.T) {
	expectedOps := []string{
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
	assert.Equal(t, expectedOps, allowedOps)
}

func TestAllowedOpsCount(t *testing.T) {
	assert.Len(t, allowedOps, 12)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Platform:  "ios",
		Operation: "tap",
		Device:    "device-123",
		App:       "com.example.app",
		X:         100,
		Y:         200,
		X2:        300,
		Y2:        400,
		Text:      "hello world",
		URL:       "https://example.com",
		Output:    "/tmp/screenshot.png",
	}

	assert.Equal(t, "ios", in.Platform)
	assert.Equal(t, "tap", in.Operation)
	assert.Equal(t, "device-123", in.Device)
	assert.Equal(t, "com.example.app", in.App)
	assert.Equal(t, 100, in.X)
	assert.Equal(t, 200, in.Y)
	assert.Equal(t, 300, in.X2)
	assert.Equal(t, 400, in.Y2)
	assert.Equal(t, "hello world", in.Text)
	assert.Equal(t, "https://example.com", in.URL)
	assert.Equal(t, "/tmp/screenshot.png", in.Output)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Platform:  "android",
		Operation: "screenshot",
		Device:    "emulator-5554",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Platform, decoded.Platform)
	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.Device, decoded.Device)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Platform)
	assert.Empty(t, in.Operation)
	assert.Empty(t, in.Device)
	assert.Empty(t, in.App)
	assert.Zero(t, in.X)
	assert.Zero(t, in.Y)
	assert.Zero(t, in.X2)
	assert.Zero(t, in.Y2)
	assert.Empty(t, in.Text)
	assert.Empty(t, in.URL)
	assert.Empty(t, in.Output)
}

func TestInput_PlatformValues(t *testing.T) {
	platforms := []string{"ios", "android", "auto"}

	for _, platform := range platforms {
		in := input{Platform: platform}
		assert.Equal(t, platform, in.Platform)
	}
}

func TestInput_OperationValues(t *testing.T) {
	for _, op := range allowedOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Platform:  "ios",
		Operation: "tap",
		Device:    "dev",
		App:       "app",
		X:         1,
		Y:         2,
		X2:        3,
		Y2:        4,
		Text:      "t",
		URL:       "u",
		Output:    "o",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "platform")
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "device")
	assert.Contains(t, jsonStr, "app")
	assert.Contains(t, jsonStr, `"x":`)
	assert.Contains(t, jsonStr, `"y":`)
	assert.Contains(t, jsonStr, "x2")
	assert.Contains(t, jsonStr, "y2")
	assert.Contains(t, jsonStr, "text")
	assert.Contains(t, jsonStr, "url")
	assert.Contains(t, jsonStr, "output")
}

func TestInput_OmitEmptyFields(t *testing.T) {
	in := input{
		Operation: "tap",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	// Only operation should be present (others are omitempty)
	assert.Contains(t, jsonStr, "operation")
	assert.NotContains(t, jsonStr, "platform")
	assert.NotContains(t, jsonStr, `"device"`)
	assert.NotContains(t, jsonStr, `"app"`)
}

// Tests for UnifiedDevice structure

func TestUnifiedDevice_AllFields(t *testing.T) {
	device := UnifiedDevice{
		Platform: "ios",
		ID:       "UDID-12345",
		Name:     "iPhone 15 Pro",
		State:    "Booted",
		OS:       "17.0",
		Model:    "iPhone15,2",
	}

	assert.Equal(t, "ios", device.Platform)
	assert.Equal(t, "UDID-12345", device.ID)
	assert.Equal(t, "iPhone 15 Pro", device.Name)
	assert.Equal(t, "Booted", device.State)
	assert.Equal(t, "17.0", device.OS)
	assert.Equal(t, "iPhone15,2", device.Model)
}

func TestUnifiedDevice_JSONSerialization(t *testing.T) {
	device := UnifiedDevice{
		Platform: "android",
		ID:       "emulator-5554",
		Name:     "Pixel 7",
		State:    "device",
	}

	data, err := json.Marshal(device)
	assert.NoError(t, err)

	var decoded UnifiedDevice
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, device.Platform, decoded.Platform)
	assert.Equal(t, device.ID, decoded.ID)
	assert.Equal(t, device.Name, decoded.Name)
	assert.Equal(t, device.State, decoded.State)
}

func TestUnifiedDevice_EmptyFields(t *testing.T) {
	device := UnifiedDevice{}

	assert.Empty(t, device.Platform)
	assert.Empty(t, device.ID)
	assert.Empty(t, device.Name)
	assert.Empty(t, device.State)
	assert.Empty(t, device.OS)
	assert.Empty(t, device.Model)
}

func TestUnifiedDevice_JSONFieldNames(t *testing.T) {
	device := UnifiedDevice{
		Platform: "ios",
		ID:       "id",
		Name:     "name",
		State:    "state",
		OS:       "os",
		Model:    "model",
	}

	data, err := json.Marshal(device)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "platform")
	assert.Contains(t, jsonStr, `"id"`)
	assert.Contains(t, jsonStr, "name")
	assert.Contains(t, jsonStr, "state")
	assert.Contains(t, jsonStr, `"os"`)
	assert.Contains(t, jsonStr, "model")
}

func TestUnifiedDevice_iOSDevice(t *testing.T) {
	device := UnifiedDevice{
		Platform: "ios",
		ID:       "ABCD-1234-5678-EFGH",
		Name:     "iPhone 14 Pro Max",
		State:    "Booted",
		OS:       "16.4",
	}

	assert.Equal(t, "ios", device.Platform)
	assert.NotEmpty(t, device.OS)
	assert.Empty(t, device.Model) // iOS uses OS field
}

func TestUnifiedDevice_AndroidDevice(t *testing.T) {
	device := UnifiedDevice{
		Platform: "android",
		ID:       "emulator-5554",
		Name:     "Pixel 6",
		State:    "device",
		Model:    "Pixel 6",
	}

	assert.Equal(t, "android", device.Platform)
	assert.NotEmpty(t, device.Model)
	assert.Empty(t, device.OS) // Android uses Model field
}

// Tests for UINode structure (Android XML parsing)

func TestUINode_AllFields(t *testing.T) {
	node := UINode{
		Text:        "Click me",
		ResourceID:  "com.example:id/button",
		Class:       "android.widget.Button",
		ContentDesc: "Submit button",
		Clickable:   "true",
		Enabled:     "true",
		Bounds:      "[0,0][100,50]",
	}

	assert.Equal(t, "Click me", node.Text)
	assert.Equal(t, "com.example:id/button", node.ResourceID)
	assert.Equal(t, "android.widget.Button", node.Class)
	assert.Equal(t, "Submit button", node.ContentDesc)
	assert.Equal(t, "true", node.Clickable)
	assert.Equal(t, "true", node.Enabled)
	assert.Equal(t, "[0,0][100,50]", node.Bounds)
}

func TestUINode_EmptyFields(t *testing.T) {
	node := UINode{}

	assert.Empty(t, node.Text)
	assert.Empty(t, node.ResourceID)
	assert.Empty(t, node.Class)
	assert.Empty(t, node.ContentDesc)
	assert.Empty(t, node.Clickable)
	assert.Empty(t, node.Enabled)
	assert.Empty(t, node.Bounds)
	assert.Nil(t, node.Children)
}

func TestUINode_WithChildren(t *testing.T) {
	child1 := UINode{Text: "Child 1"}
	child2 := UINode{Text: "Child 2"}
	parent := UINode{
		Text:     "Parent",
		Children: []UINode{child1, child2},
	}

	assert.Len(t, parent.Children, 2)
	assert.Equal(t, "Child 1", parent.Children[0].Text)
	assert.Equal(t, "Child 2", parent.Children[1].Text)
}

func TestUINode_NestedChildren(t *testing.T) {
	grandchild := UINode{Text: "Grandchild"}
	child := UINode{
		Text:     "Child",
		Children: []UINode{grandchild},
	}
	parent := UINode{
		Text:     "Parent",
		Children: []UINode{child},
	}

	assert.Len(t, parent.Children, 1)
	assert.Len(t, parent.Children[0].Children, 1)
	assert.Equal(t, "Grandchild", parent.Children[0].Children[0].Text)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Platform:  "ios",
		Operation: "swipe",
		Device:    "full-device",
		App:       "com.full.app",
		X:         10,
		Y:         20,
		X2:        30,
		Y2:        40,
		Text:      "full text",
		URL:       "https://full.com",
		Output:    "/full/output.png",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Platform, decoded.Platform)
	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.Device, decoded.Device)
	assert.Equal(t, in.App, decoded.App)
	assert.Equal(t, in.X, decoded.X)
	assert.Equal(t, in.Y, decoded.Y)
	assert.Equal(t, in.X2, decoded.X2)
	assert.Equal(t, in.Y2, decoded.Y2)
	assert.Equal(t, in.Text, decoded.Text)
	assert.Equal(t, in.URL, decoded.URL)
	assert.Equal(t, in.Output, decoded.Output)
}

func TestUnifiedDevice_FullJSONRoundTrip(t *testing.T) {
	device := UnifiedDevice{
		Platform: "ios",
		ID:       "full-id",
		Name:     "Full Name",
		State:    "Booted",
		OS:       "17.0",
		Model:    "Full Model",
	}

	data, err := json.Marshal(device)
	assert.NoError(t, err)

	var decoded UnifiedDevice
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, device.Platform, decoded.Platform)
	assert.Equal(t, device.ID, decoded.ID)
	assert.Equal(t, device.Name, decoded.Name)
	assert.Equal(t, device.State, decoded.State)
	assert.Equal(t, device.OS, decoded.OS)
	assert.Equal(t, device.Model, decoded.Model)
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
		X:         540,
		Y:         960,
	}

	// Center tap on a 1080x1920 screen
	assert.Equal(t, 540, in.X)
	assert.Equal(t, 960, in.Y)
}
