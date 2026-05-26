package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestSkillName(t *testing.T) {
	assert.Equal(t, "build/godot", skillName)
}

func TestActionConstants(t *testing.T) {
	assert.Equal(t, "list_presets", ActionListPresets)
	assert.Equal(t, "export", ActionExport)
	assert.Equal(t, "validate", ActionValidate)
	assert.Equal(t, "build", ActionBuild)
	assert.Equal(t, "restore", ActionRestore)
	assert.Equal(t, "clean", ActionClean)
}

func TestAllActions(t *testing.T) {
	actions := []string{
		ActionListPresets,
		ActionExport,
		ActionValidate,
		ActionBuild,
		ActionRestore,
		ActionClean,
	}
	assert.Len(t, actions, 6)
}

// Tests for Input structure

func TestInput_ActionValues(t *testing.T) {
	actions := []string{
		ActionListPresets,
		ActionExport,
		ActionValidate,
		ActionBuild,
		ActionRestore,
		ActionClean,
	}

	for _, action := range actions {
		in := Input{Action: action}
		assert.Equal(t, action, in.Action)
	}
}

func TestInput_VerbosityValues(t *testing.T) {
	verbosities := []string{"quiet", "minimal", "normal", "detailed", "diagnostic"}

	for _, v := range verbosities {
		in := Input{Verbosity: v}
		assert.Equal(t, v, in.Verbosity)
	}
}

func TestInput_ConfigurationValues(t *testing.T) {
	configs := []string{"Debug", "Release"}

	for _, c := range configs {
		in := Input{Configuration: c}
		assert.Equal(t, c, in.Configuration)
	}
}

// Tests for ExportPreset structure

func TestExportPreset_OmitEmptyExportPath(t *testing.T) {
	preset := ExportPreset{
		Name:     "Android",
		Platform: "Android",
		Runnable: false,
	}

	data, err := json.Marshal(preset)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.NotContains(t, jsonStr, "export_path")
}

func TestExportPreset_PlatformValues(t *testing.T) {
	platforms := []string{
		"Windows",
		"Linux/X11",
		"macOS",
		"Android",
		"iOS",
		"Web",
	}

	for _, p := range platforms {
		preset := ExportPreset{Platform: p}
		assert.Equal(t, p, preset.Platform)
	}
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Action:        "export",
		Preset:        "Full Preset",
		OutputPath:    "/full/output/path.exe",
		Debug:         true,
		GodotPath:     "/full/godot/path",
		PackOnly:      true,
		ExportDebug:   true,
		Configuration: "Release",
		Verbosity:     "diagnostic",
		Target:        "Rebuild",
		DotnetPath:    "/full/dotnet/path",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.Preset, decoded.Preset)
	assert.Equal(t, in.OutputPath, decoded.OutputPath)
	assert.Equal(t, in.Debug, decoded.Debug)
	assert.Equal(t, in.GodotPath, decoded.GodotPath)
	assert.Equal(t, in.PackOnly, decoded.PackOnly)
	assert.Equal(t, in.ExportDebug, decoded.ExportDebug)
	assert.Equal(t, in.Configuration, decoded.Configuration)
	assert.Equal(t, in.Verbosity, decoded.Verbosity)
	assert.Equal(t, in.Target, decoded.Target)
	assert.Equal(t, in.DotnetPath, decoded.DotnetPath)
}

func TestExportPreset_FullJSONRoundTrip(t *testing.T) {
	preset := ExportPreset{
		Name:       "Full Preset",
		Platform:   "Full Platform",
		ExportPath: "/full/export/path",
		Runnable:   true,
	}

	data, err := json.Marshal(preset)
	assert.NoError(t, err)

	var decoded ExportPreset
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, preset.Name, decoded.Name)
	assert.Equal(t, preset.Platform, decoded.Platform)
	assert.Equal(t, preset.ExportPath, decoded.ExportPath)
	assert.Equal(t, preset.Runnable, decoded.Runnable)
}

func TestInput_ExportAction(t *testing.T) {
	in := Input{
		Action:      ActionExport,
		Preset:      "Windows Desktop",
		OutputPath:  "builds/game.exe",
		ExportDebug: false,
	}

	assert.Equal(t, "export", in.Action)
	assert.Equal(t, "Windows Desktop", in.Preset)
}

func TestInput_BuildAction(t *testing.T) {
	in := Input{
		Action:        ActionBuild,
		Configuration: "Release",
		Verbosity:     "minimal",
	}

	assert.Equal(t, "build", in.Action)
	assert.Equal(t, "Release", in.Configuration)
}

func TestInput_ValidateAction(t *testing.T) {
	in := Input{
		Action: ActionValidate,
		Preset: "Android Export",
	}

	assert.Equal(t, "validate", in.Action)
}

func TestInput_CleanAction(t *testing.T) {
	in := Input{
		Action:     ActionClean,
		DotnetPath: "dotnet",
	}

	assert.Equal(t, "clean", in.Action)
}

func TestInput_RestoreAction(t *testing.T) {
	in := Input{
		Action:    ActionRestore,
		Verbosity: "normal",
	}

	assert.Equal(t, "restore", in.Action)
}
