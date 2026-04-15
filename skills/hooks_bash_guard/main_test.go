package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/joshka0/foxctl/internal/runtime/agentpolicy"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer) (*skillmain.RunContext, func()) {
	t.Helper()
	return skilltest.NewTestRunContext(t, buf, nil)
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func getHookOutput(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	hookOutput, ok := data["hook_output"].(map[string]any)
	if !ok {
		t.Fatalf("expected hook_output to be map, got %T", data["hook_output"])
	}
	return hookOutput
}

func bashInput(cmd string) hooks.Input {
	toolInput, _ := json.Marshal(map[string]string{"command": cmd})
	return hooks.Input{
		Event:     hooks.EventPreToolUse,
		ToolName:  "Bash",
		ToolInput: toolInput,
	}
}

// Tests for non-Bash tool calls

func TestBashGuard_NonBashToolApproved(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := hooks.Input{
		Event:    hooks.EventPreToolUse,
		ToolName: "Read",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
	assert.Equal(t, "not a Bash tool call", hookOutput["reason"])
}

// Tests for empty command

func TestBashGuard_EmptyCommandApproved(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("")

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
	assert.Equal(t, "empty command", hookOutput["reason"])
}

// Tests for unrestricted profile (default)

func TestBashGuard_UnrestrictedAllowsAllCommands(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("rm -rf /")

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
}

// Tests for sed range rewrite

func TestBashGuard_RewritesSedRangeCommand(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("sed -n '10,12p' /tmp/notes.txt")

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])

	updated, ok := hookOutput["updated_tool_input"].(map[string]any)
	require.True(t, ok)
	command, ok := updated["command"].(string)
	require.True(t, ok)
	assert.Contains(t, command, "code/context_grep")
	assert.Contains(t, command, `"file_path":"/tmp/notes.txt"`)
	assert.Contains(t, command, `"line_start":10`)
	assert.Contains(t, command, `"line_end":12`)
}

func TestBashGuard_RewritesSedRangeFromCat(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("cat -n /tmp/notes.txt | sed -n '10,12p'")

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])

	updated, ok := hookOutput["updated_tool_input"].(map[string]any)
	require.True(t, ok)
	command, ok := updated["command"].(string)
	require.True(t, ok)
	assert.Contains(t, command, "code/context_grep")
	assert.Contains(t, command, `"file_path":"/tmp/notes.txt"`)
	assert.Contains(t, command, `"line_start":10`)
	assert.Contains(t, command, `"line_end":12`)
}

// Tests for explorer profile blocking non-foxctl commands

func TestBashGuard_ExplorerBlocksArbitraryCommand(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("ls -la")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "block", hookOutput["decision"])
	assert.Contains(t, hookOutput["reason"], "only foxctl run commands are allowed")
}

// Tests for explorer profile allowing foxctl skills

func TestBashGuard_ExplorerAllowsAllowedSkill(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("foxctl run code/semantic_search --input '{}'")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])

	// Check meta includes parsed skill
	meta, ok := hookOutput["meta"].(map[string]any)
	if ok {
		assert.Equal(t, "code/semantic_search", meta["parsed_skill"])
	}
}

// Tests for explorer profile blocking disallowed skills

func TestBashGuard_ExplorerBlocksDisallowedSkill(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("foxctl run test/run --input '{}'")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "block", hookOutput["decision"])
	assert.Contains(t, hookOutput["reason"], "not allowed")
}

// Tests for profile from environment variable

func TestBashGuard_ProfileFromEnvVar(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Set environment variable
	os.Setenv("FOXCTL_AGENT_PROFILE", "explorer")
	defer os.Unsetenv("FOXCTL_AGENT_PROFILE")

	in := bashInput("ls -la")

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	// Should block because explorer doesn't allow arbitrary commands
	assert.Equal(t, "block", hookOutput["decision"])
}

// Tests for environment variable taking precedence over hook config

func TestBashGuard_EnvVarOverridesHookConfig(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	// Set environment variable to unrestricted
	os.Setenv("FOXCTL_AGENT_PROFILE", "unrestricted")
	defer os.Unsetenv("FOXCTL_AGENT_PROFILE")

	// Hook config says explorer
	in := bashInput("ls -la")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	// Should approve because env var says unrestricted
	assert.Equal(t, "approve", hookOutput["decision"])
}

// Tests for invalid profile falling back to unrestricted

func TestBashGuard_InvalidProfileFallsBackToUnrestricted(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("rm -rf /")
	in.HookConfig = map[string]any{"profile": "invalid_profile"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	// Invalid profile falls back to unrestricted, which allows everything
	assert.Equal(t, "approve", hookOutput["decision"])
}

// Tests for reviewer profile

func TestBashGuard_ReviewerAllowsAnalysisSkills(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("foxctl run code/complexity --input '{}'")
	in.HookConfig = map[string]any{"profile": "reviewer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
}

// Tests for implementer profile

func TestBashGuard_ImplementerAllowsTestRun(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("foxctl run test/run --input '{}'")
	in.HookConfig = map[string]any{"profile": "implementer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
}

// Tests for block output containing context with allowed skills

func TestBashGuard_BlockIncludesAllowedSkillsList(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("ls -la")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "block", hookOutput["decision"])

	// Check that context includes allowed skills
	contextStr, ok := hookOutput["context"].(string)
	require.True(t, ok, "context should be a string")
	assert.Contains(t, contextStr, "Allowed Skills")
	assert.Contains(t, contextStr, "foxctl run")
}

// Tests for malformed tool input

func TestBashGuard_MalformedToolInputApproved(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := hooks.Input{
		Event:     hooks.EventPreToolUse,
		ToolName:  "Bash",
		ToolInput: json.RawMessage(`{invalid json`),
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])
	assert.Contains(t, hookOutput["reason"], "failed to extract command")
}

// Test meta includes profile information

func TestBashGuard_MetaIncludesProfile(t *testing.T) {
	var buf bytes.Buffer
	rc, cleanup := newTestContext(t, &buf)
	defer cleanup()

	in := bashInput("foxctl run fs/read --input '{}'")
	in.HookConfig = map[string]any{"profile": "explorer"}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)
	hookOutput := getHookOutput(t, data)
	assert.Equal(t, "approve", hookOutput["decision"])

	meta, ok := hookOutput["meta"].(map[string]any)
	require.True(t, ok, "meta should be a map")
	assert.Equal(t, "explorer", meta["profile"])
}

// Test resolveProfile helper

func TestResolveProfile_EnvVarTakesPrecedence(t *testing.T) {
	os.Setenv("FOXCTL_AGENT_PROFILE", "reviewer")
	defer os.Unsetenv("FOXCTL_AGENT_PROFILE")

	in := hooks.Input{
		HookConfig: map[string]any{"profile": "explorer"},
	}

	profile := resolveProfile(in)
	assert.Equal(t, agentpolicy.ProfileReviewer, profile)
}

func TestResolveProfile_HookConfigFallback(t *testing.T) {
	// Ensure no env var
	os.Unsetenv("FOXCTL_AGENT_PROFILE")

	in := hooks.Input{
		HookConfig: map[string]any{"profile": "implementer"},
	}

	profile := resolveProfile(in)
	assert.Equal(t, agentpolicy.ProfileImplementer, profile)
}

func TestResolveProfile_DefaultUnrestricted(t *testing.T) {
	// Ensure no env var
	os.Unsetenv("FOXCTL_AGENT_PROFILE")

	in := hooks.Input{}

	profile := resolveProfile(in)
	assert.Equal(t, agentpolicy.ProfileUnrestricted, profile)
}
