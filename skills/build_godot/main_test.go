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

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Action:        "export",
		Preset:        "Windows Desktop",
		OutputPath:    "/path/to/output.exe",
		Debug:         true,
		GodotPath:     "/usr/bin/godot",
		PackOnly:      false,
		ExportDebug:   true,
		Configuration: "Release",
		Verbosity:     "detailed",
		Target:        "Build",
		DotnetPath:    "/usr/bin/dotnet",
	}

	assert.Equal(t, "export", in.Action)
	assert.Equal(t, "Windows Desktop", in.Preset)
	assert.Equal(t, "/path/to/output.exe", in.OutputPath)
	assert.True(t, in.Debug)
	assert.Equal(t, "/usr/bin/godot", in.GodotPath)
	assert.False(t, in.PackOnly)
	assert.True(t, in.ExportDebug)
	assert.Equal(t, "Release", in.Configuration)
	assert.Equal(t, "detailed", in.Verbosity)
	assert.Equal(t, "Build", in.Target)
	assert.Equal(t, "/usr/bin/dotnet", in.DotnetPath)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Action:     "list_presets",
		GodotPath:  "godot4",
		OutputPath: "/build/output",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Action, decoded.Action)
	assert.Equal(t, in.GodotPath, decoded.GodotPath)
	assert.Equal(t, in.OutputPath, decoded.OutputPath)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Action)
	assert.Empty(t, in.Preset)
	assert.Empty(t, in.OutputPath)
	assert.False(t, in.Debug)
	assert.Empty(t, in.GodotPath)
	assert.False(t, in.PackOnly)
	assert.False(t, in.ExportDebug)
	assert.Empty(t, in.Configuration)
	assert.Empty(t, in.Verbosity)
	assert.Empty(t, in.Target)
	assert.Empty(t, in.DotnetPath)
}

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

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		Action:        "a",
		Preset:        "p",
		OutputPath:    "o",
		Debug:         true,
		GodotPath:     "g",
		PackOnly:      true,
		ExportDebug:   true,
		Configuration: "c",
		Verbosity:     "v",
		Target:        "t",
		DotnetPath:    "d",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "action")
	assert.Contains(t, jsonStr, "preset")
	assert.Contains(t, jsonStr, "output_path")
	assert.Contains(t, jsonStr, "debug")
	assert.Contains(t, jsonStr, "godot_path")
	assert.Contains(t, jsonStr, "pack_only")
	assert.Contains(t, jsonStr, "export_debug")
	assert.Contains(t, jsonStr, "configuration")
	assert.Contains(t, jsonStr, "verbosity")
	assert.Contains(t, jsonStr, "target")
	assert.Contains(t, jsonStr, "dotnet_path")
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

func TestExportPreset_AllFields(t *testing.T) {
	preset := ExportPreset{
		Name:       "Windows Desktop",
		Platform:   "Windows",
		ExportPath: "builds/windows/game.exe",
		Runnable:   true,
	}

	assert.Equal(t, "Windows Desktop", preset.Name)
	assert.Equal(t, "Windows", preset.Platform)
	assert.Equal(t, "builds/windows/game.exe", preset.ExportPath)
	assert.True(t, preset.Runnable)
}

func TestExportPreset_JSONSerialization(t *testing.T) {
	preset := ExportPreset{
		Name:       "Linux",
		Platform:   "Linux/X11",
		ExportPath: "builds/linux/game.x86_64",
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

func TestExportPreset_EmptyFields(t *testing.T) {
	preset := ExportPreset{}

	assert.Empty(t, preset.Name)
	assert.Empty(t, preset.Platform)
	assert.Empty(t, preset.ExportPath)
	assert.False(t, preset.Runnable)
}

func TestExportPreset_JSONFieldNames(t *testing.T) {
	preset := ExportPreset{
		Name:       "n",
		Platform:   "p",
		ExportPath: "e",
		Runnable:   true,
	}

	data, err := json.Marshal(preset)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "name")
	assert.Contains(t, jsonStr, "platform")
	assert.Contains(t, jsonStr, "export_path")
	assert.Contains(t, jsonStr, "runnable")
}

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
