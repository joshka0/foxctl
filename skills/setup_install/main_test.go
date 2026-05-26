package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_ProviderValues(t *testing.T) {
	providers := []string{"claude-code", "claude", "opencode", "codex", "all"}

	for _, p := range providers {
		in := input{Provider: p}
		assert.Equal(t, p, in.Provider)
	}
}

func TestEnvStatus_RequiredEnvVars(t *testing.T) {
	requiredVars := []envStatus{}

	optionalVars := []envStatus{
		{Name: "FOXCTL_EMBEDDING_PROVIDER", Required: false},
		{Name: "FOXCTL_EMBEDDING_MODEL", Required: false},
		{Name: "FOXCTL_EMBEDDING_BASE_URL", Required: false},
		{Name: "ANTHROPIC_API_KEY", Required: false},
		{Name: "FOXCTL_HOME", Required: false},
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
		{Name: "FOXCTL_EMBEDDING_PROVIDER", Set: true, Required: false},
		{Name: "ANTHROPIC_API_KEY", Set: false, Required: false},
		{Name: "FOXCTL_HOME", Set: true, Required: false},
		{Name: "FOXCTL_EMBEDDING_BASE_URL", Set: false, Required: false},
	}

	out := output{
		Status:      "ok",
		Environment: envs,
	}

	assert.Len(t, out.Environment, 4)
}
