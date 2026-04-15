package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "setup/install", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Provider:     "claude-code",
		SkipHooks:    true,
		SkipSkills:   true,
		RepoRoot:     "/path/to/foxctl",
		ValidateOnly: true,
	}

	assert.Equal(t, "claude-code", in.Provider)
	assert.True(t, in.SkipHooks)
	assert.True(t, in.SkipSkills)
	assert.Equal(t, "/path/to/foxctl", in.RepoRoot)
	assert.True(t, in.ValidateOnly)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Provider:     "opencode",
		SkipHooks:    false,
		ValidateOnly: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.SkipHooks, decoded.SkipHooks)
	assert.Equal(t, in.ValidateOnly, decoded.ValidateOnly)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Provider)
	assert.False(t, in.SkipHooks)
	assert.False(t, in.SkipSkills)
	assert.Empty(t, in.RepoRoot)
	assert.False(t, in.ValidateOnly)
}

func TestInput_ProviderValues(t *testing.T) {
	providers := []string{"claude-code", "claude", "opencode", "codex", "all"}

	for _, p := range providers {
		in := input{Provider: p}
		assert.Equal(t, p, in.Provider)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Provider:     "p",
		SkipHooks:    true,
		SkipSkills:   true,
		RepoRoot:     "r",
		ValidateOnly: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "provider")
	assert.Contains(t, jsonStr, "skip_hooks")
	assert.Contains(t, jsonStr, "skip_skills")
	assert.Contains(t, jsonStr, "repo_root")
	assert.Contains(t, jsonStr, "validate_only")
}

// Tests for output structure

func TestOutput_AllFields(t *testing.T) {
	out := output{
		Status:   "ok",
		Provider: "claude-code",
		Directories: []directoryStatus{
			{Path: "/home/.foxctl", Exists: true, Created: false},
		},
		Hooks: hooksStatus{
			Provider:  "claude-code",
			Installed: true,
			HookCount: 5,
			Hooks:     []string{"hook1.sh", "hook2.sh"},
		},
		Binary: binaryStatus{
			Path:    "/usr/local/bin/foxctl",
			Exists:  true,
			Version: "1.0.0",
		},
		Environment: []envStatus{
			{Name: "VOYAGE_API_KEY", Set: true, Required: true},
		},
		Errors:       []string{"error1"},
		Warnings:     []string{"warning1"},
		Instructions: []string{"instruction1"},
	}

	assert.Equal(t, "ok", out.Status)
	assert.Equal(t, "claude-code", out.Provider)
	assert.Len(t, out.Directories, 1)
	assert.True(t, out.Hooks.Installed)
	assert.True(t, out.Binary.Exists)
	assert.Len(t, out.Environment, 1)
	assert.Len(t, out.Errors, 1)
	assert.Len(t, out.Warnings, 1)
	assert.Len(t, out.Instructions, 1)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := output{
		Status:   "warning",
		Provider: "opencode",
		Warnings: []string{"missing directory"},
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Status, decoded.Status)
	assert.Equal(t, out.Provider, decoded.Provider)
	assert.Equal(t, out.Warnings, decoded.Warnings)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := output{}

	assert.Empty(t, out.Status)
	assert.Empty(t, out.Provider)
	assert.Nil(t, out.Directories)
	assert.Empty(t, out.Hooks.Provider)
	assert.Empty(t, out.Binary.Path)
	assert.Nil(t, out.Environment)
	assert.Nil(t, out.Errors)
	assert.Nil(t, out.Warnings)
	assert.Nil(t, out.Instructions)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"ok", "warning", "error"}

	for _, s := range statuses {
		out := output{Status: s}
		assert.Equal(t, s, out.Status)
	}
}

// Tests for directoryStatus structure

func TestDirectoryStatus_AllFields(t *testing.T) {
	ds := directoryStatus{
		Path:    "/home/user/.foxctl",
		Exists:  true,
		Created: true,
	}

	assert.Equal(t, "/home/user/.foxctl", ds.Path)
	assert.True(t, ds.Exists)
	assert.True(t, ds.Created)
}

func TestDirectoryStatus_JSONSerialization(t *testing.T) {
	ds := directoryStatus{
		Path:    "/var/data",
		Exists:  false,
		Created: false,
	}

	data, err := json.Marshal(ds)
	assert.NoError(t, err)

	var decoded directoryStatus
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, ds.Path, decoded.Path)
	assert.Equal(t, ds.Exists, decoded.Exists)
	assert.Equal(t, ds.Created, decoded.Created)
}

func TestDirectoryStatus_EmptyFields(t *testing.T) {
	ds := directoryStatus{}

	assert.Empty(t, ds.Path)
	assert.False(t, ds.Exists)
	assert.False(t, ds.Created)
}

func TestDirectoryStatus_JSONFieldNames(t *testing.T) {
	ds := directoryStatus{
		Path:    "p",
		Exists:  true,
		Created: true,
	}

	data, err := json.Marshal(ds)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "path")
	assert.Contains(t, jsonStr, "exists")
	assert.Contains(t, jsonStr, "created")
}

// Tests for hooksStatus structure

func TestHooksStatus_AllFields(t *testing.T) {
	hs := hooksStatus{
		Provider:  "claude-code",
		Installed: true,
		HookCount: 3,
		Hooks:     []string{"hook1.sh", "hook2.sh", "hook3.sh"},
	}

	assert.Equal(t, "claude-code", hs.Provider)
	assert.True(t, hs.Installed)
	assert.Equal(t, 3, hs.HookCount)
	assert.Len(t, hs.Hooks, 3)
}

func TestHooksStatus_JSONSerialization(t *testing.T) {
	hs := hooksStatus{
		Provider:  "opencode",
		Installed: false,
		HookCount: 0,
	}

	data, err := json.Marshal(hs)
	assert.NoError(t, err)

	var decoded hooksStatus
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, hs.Provider, decoded.Provider)
	assert.Equal(t, hs.Installed, decoded.Installed)
	assert.Equal(t, hs.HookCount, decoded.HookCount)
}

func TestHooksStatus_EmptyFields(t *testing.T) {
	hs := hooksStatus{}

	assert.Empty(t, hs.Provider)
	assert.False(t, hs.Installed)
	assert.Zero(t, hs.HookCount)
	assert.Nil(t, hs.Hooks)
}

func TestHooksStatus_JSONFieldNames(t *testing.T) {
	hs := hooksStatus{
		Provider:  "p",
		Installed: true,
		HookCount: 1,
		Hooks:     []string{"h"},
	}

	data, err := json.Marshal(hs)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "provider")
	assert.Contains(t, jsonStr, "installed")
	assert.Contains(t, jsonStr, "hook_count")
	assert.Contains(t, jsonStr, "hooks")
}

// Tests for binaryStatus structure

func TestBinaryStatus_AllFields(t *testing.T) {
	bs := binaryStatus{
		Path:    "/usr/local/bin/foxctl",
		Exists:  true,
		Version: "1.2.3",
	}

	assert.Equal(t, "/usr/local/bin/foxctl", bs.Path)
	assert.True(t, bs.Exists)
	assert.Equal(t, "1.2.3", bs.Version)
}

func TestBinaryStatus_JSONSerialization(t *testing.T) {
	bs := binaryStatus{
		Path:    "/home/user/.local/bin/foxctl",
		Exists:  true,
		Version: "0.9.0",
	}

	data, err := json.Marshal(bs)
	assert.NoError(t, err)

	var decoded binaryStatus
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, bs.Path, decoded.Path)
	assert.Equal(t, bs.Exists, decoded.Exists)
	assert.Equal(t, bs.Version, decoded.Version)
}

func TestBinaryStatus_EmptyFields(t *testing.T) {
	bs := binaryStatus{}

	assert.Empty(t, bs.Path)
	assert.False(t, bs.Exists)
	assert.Empty(t, bs.Version)
}

func TestBinaryStatus_JSONFieldNames(t *testing.T) {
	bs := binaryStatus{
		Path:    "p",
		Exists:  true,
		Version: "v",
	}

	data, err := json.Marshal(bs)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "path")
	assert.Contains(t, jsonStr, "exists")
	assert.Contains(t, jsonStr, "version")
}

// Tests for envStatus structure

func TestEnvStatus_AllFields(t *testing.T) {
	es := envStatus{
		Name:     "VOYAGE_API_KEY",
		Set:      true,
		Required: true,
	}

	assert.Equal(t, "VOYAGE_API_KEY", es.Name)
	assert.True(t, es.Set)
	assert.True(t, es.Required)
}

func TestEnvStatus_JSONSerialization(t *testing.T) {
	es := envStatus{
		Name:     "ANTHROPIC_API_KEY",
		Set:      false,
		Required: false,
	}

	data, err := json.Marshal(es)
	assert.NoError(t, err)

	var decoded envStatus
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, es.Name, decoded.Name)
	assert.Equal(t, es.Set, decoded.Set)
	assert.Equal(t, es.Required, decoded.Required)
}

func TestEnvStatus_EmptyFields(t *testing.T) {
	es := envStatus{}

	assert.Empty(t, es.Name)
	assert.False(t, es.Set)
	assert.False(t, es.Required)
}

func TestEnvStatus_JSONFieldNames(t *testing.T) {
	es := envStatus{
		Name:     "n",
		Set:      true,
		Required: true,
	}

	data, err := json.Marshal(es)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "name")
	assert.Contains(t, jsonStr, "set")
	assert.Contains(t, jsonStr, "required")
}

func TestEnvStatus_RequiredEnvVars(t *testing.T) {
	requiredVars := []envStatus{
		{Name: "VOYAGE_API_KEY", Required: true},
	}

	optionalVars := []envStatus{
		{Name: "ANTHROPIC_API_KEY", Required: false},
		{Name: "FOXCTL_HOME", Required: false},
		{Name: "FOXCTL_SEMANTIC_RERANK", Required: false},
	}

	for _, v := range requiredVars {
		assert.True(t, v.Required, "expected %s to be required", v.Name)
	}

	for _, v := range optionalVars {
		assert.False(t, v.Required, "expected %s to be optional", v.Name)
	}
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Provider:     "all",
		SkipHooks:    true,
		SkipSkills:   true,
		RepoRoot:     "/full/repo/root",
		ValidateOnly: true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Provider, decoded.Provider)
	assert.Equal(t, in.SkipHooks, decoded.SkipHooks)
	assert.Equal(t, in.SkipSkills, decoded.SkipSkills)
	assert.Equal(t, in.RepoRoot, decoded.RepoRoot)
	assert.Equal(t, in.ValidateOnly, decoded.ValidateOnly)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := output{
		Status:   "ok",
		Provider: "claude-code",
		Directories: []directoryStatus{
			{Path: "/dir1", Exists: true, Created: false},
			{Path: "/dir2", Exists: true, Created: true},
		},
		Hooks: hooksStatus{
			Provider:  "claude-code",
			Installed: true,
			HookCount: 2,
			Hooks:     []string{"h1.sh", "h2.sh"},
		},
		Binary: binaryStatus{
			Path:    "/bin/foxctl",
			Exists:  true,
			Version: "1.0.0",
		},
		Environment: []envStatus{
			{Name: "VAR1", Set: true, Required: true},
			{Name: "VAR2", Set: false, Required: false},
		},
		Errors:       []string{},
		Warnings:     []string{"warn1"},
		Instructions: []string{"inst1", "inst2"},
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Status, decoded.Status)
	assert.Equal(t, out.Provider, decoded.Provider)
	assert.Len(t, decoded.Directories, 2)
	assert.Equal(t, out.Hooks.Installed, decoded.Hooks.Installed)
	assert.Equal(t, out.Binary.Version, decoded.Binary.Version)
	assert.Len(t, decoded.Environment, 2)
}

func TestInput_ValidateOnlyMode(t *testing.T) {
	in := input{
		Provider:     "claude-code",
		ValidateOnly: true,
	}

	assert.True(t, in.ValidateOnly)
}

func TestInput_InstallMode(t *testing.T) {
	in := input{
		Provider:     "claude-code",
		ValidateOnly: false,
	}

	assert.False(t, in.ValidateOnly)
}

func TestOutput_MultipleDirectories(t *testing.T) {
	dirs := []directoryStatus{
		{Path: "~/.foxctl", Exists: true},
		{Path: "~/.foxctl/storage", Exists: true},
		{Path: "~/.foxctl/skills", Exists: true},
		{Path: "~/.foxctl/cache", Exists: true},
		{Path: "~/.foxctl/cas", Exists: true},
	}

	out := output{
		Status:      "ok",
		Directories: dirs,
	}

	assert.Len(t, out.Directories, 5)
}

func TestOutput_MultipleEnvironmentVars(t *testing.T) {
	envs := []envStatus{
		{Name: "VOYAGE_API_KEY", Set: true, Required: true},
		{Name: "ANTHROPIC_API_KEY", Set: false, Required: false},
		{Name: "FOXCTL_HOME", Set: true, Required: false},
		{Name: "FOXCTL_SEMANTIC_RERANK", Set: false, Required: false},
	}

	out := output{
		Status:      "ok",
		Environment: envs,
	}

	assert.Len(t, out.Environment, 4)
}
