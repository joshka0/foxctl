package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

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
