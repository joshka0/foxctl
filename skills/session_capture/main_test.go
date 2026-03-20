package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "session/capture", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	in := Input{
		Workspace:  "/workspace/path",
		SessionID:  "sess-123",
		Source:     "claude",
		ClaudeHome: "/home/user/.claude",
		CodexHome:  "/home/user/.codex",
		Scan:       true,
		ScanLimit:  100,
		Summarize:  true,
		Force:      true,
	}

	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "sess-123", in.SessionID)
	assert.Equal(t, "claude", in.Source)
	assert.Equal(t, "/home/user/.claude", in.ClaudeHome)
	assert.Equal(t, "/home/user/.codex", in.CodexHome)
	assert.True(t, in.Scan)
	assert.Equal(t, 100, in.ScanLimit)
	assert.True(t, in.Summarize)
	assert.True(t, in.Force)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := Input{
		Workspace: "/test/workspace",
		SessionID: "sess-abc",
		Source:    "codex",
		Scan:      true,
		ScanLimit: 50,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Source, decoded.Source)
	assert.Equal(t, in.Scan, decoded.Scan)
	assert.Equal(t, in.ScanLimit, decoded.ScanLimit)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.SessionID)
	assert.Empty(t, in.Source)
	assert.Empty(t, in.ClaudeHome)
	assert.Empty(t, in.CodexHome)
	assert.False(t, in.Scan)
	assert.Zero(t, in.ScanLimit)
	assert.False(t, in.Summarize)
	assert.False(t, in.Force)
}

func TestInput_SourceValues(t *testing.T) {
	sources := []string{"claude", "codex"}

	for _, source := range sources {
		in := Input{Source: source}
		assert.Equal(t, source, in.Source)
	}
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	output := Output{
		SessionID:        "sess-123",
		WorkspacePath:    "/workspace",
		ProjectName:      "myproject",
		GitBranch:        "main",
		MessageCount:     100,
		UserTurns:        25,
		ToolInvocations:  50,
		TotalTokens:      10000,
		Status:           "captured",
		RawJSONLPath:     "/path/to/session.jsonl",
		HighSignal:       []string{"signal1", "signal2"},
		SessionsScanned:  10,
		SessionsMatched:  5,
		SessionsCaptured: 3,
		SessionsSkipped:  2,
		Message:          "Success",
	}

	assert.Equal(t, "sess-123", output.SessionID)
	assert.Equal(t, "/workspace", output.WorkspacePath)
	assert.Equal(t, "myproject", output.ProjectName)
	assert.Equal(t, "main", output.GitBranch)
	assert.Equal(t, 100, output.MessageCount)
	assert.Equal(t, 25, output.UserTurns)
	assert.Equal(t, 50, output.ToolInvocations)
	assert.Equal(t, 10000, output.TotalTokens)
	assert.Equal(t, "captured", output.Status)
	assert.Equal(t, "/path/to/session.jsonl", output.RawJSONLPath)
	assert.Len(t, output.HighSignal, 2)
	assert.Equal(t, 10, output.SessionsScanned)
	assert.Equal(t, 5, output.SessionsMatched)
	assert.Equal(t, 3, output.SessionsCaptured)
	assert.Equal(t, 2, output.SessionsSkipped)
}

func TestOutput_JSONSerialization(t *testing.T) {
	output := Output{
		SessionID:       "sess-test",
		WorkspacePath:   "/test",
		MessageCount:    50,
		ToolInvocations: 20,
		Status:          "captured",
		Message:         "Test success",
	}

	data, err := json.Marshal(output)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, output.SessionID, decoded.SessionID)
	assert.Equal(t, output.WorkspacePath, decoded.WorkspacePath)
	assert.Equal(t, output.MessageCount, decoded.MessageCount)
	assert.Equal(t, output.Status, decoded.Status)
}

func TestOutput_StatusValues(t *testing.T) {
	statuses := []string{"captured", "exists", "scanned"}

	for _, status := range statuses {
		output := Output{Status: status}
		assert.Equal(t, status, output.Status)
	}
}

// Tests for normalizeGitURL helper

func TestNormalizeGitURL_Empty(t *testing.T) {
	result := normalizeGitURL("")
	assert.Empty(t, result)
}

func TestNormalizeGitURL_Whitespace(t *testing.T) {
	result := normalizeGitURL("   ")
	assert.Empty(t, result)
}

func TestNormalizeGitURL_HTTPS(t *testing.T) {
	result := normalizeGitURL("https://github.com/user/repo.git")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_HTTPSNoGit(t *testing.T) {
	result := normalizeGitURL("https://github.com/user/repo")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_HTTP(t *testing.T) {
	result := normalizeGitURL("http://github.com/user/repo.git")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_SSH(t *testing.T) {
	result := normalizeGitURL("git@github.com:user/repo.git")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_SSHNoGit(t *testing.T) {
	result := normalizeGitURL("git@github.com:user/repo")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_TrimsWhitespace(t *testing.T) {
	result := normalizeGitURL("  https://github.com/user/repo.git  ")
	assert.Equal(t, "github.com/user/repo", result)
}

func TestNormalizeGitURL_Plain(t *testing.T) {
	result := normalizeGitURL("github.com/user/repo")
	assert.Equal(t, "github.com/user/repo", result)
}

// Tests for resolveWorkspaceCandidates helper

func TestResolveWorkspaceCandidates_Empty(t *testing.T) {
	// filepath.Clean("") returns ".", so empty string produces ["."]
	result := resolveWorkspaceCandidates("")
	assert.Equal(t, []string{"."}, result)
}

func TestResolveWorkspaceCandidates_ValidPath(t *testing.T) {
	result := resolveWorkspaceCandidates("/some/path")
	assert.Contains(t, result, "/some/path")
}

func TestResolveWorkspaceCandidates_CleanedPath(t *testing.T) {
	result := resolveWorkspaceCandidates("/some//path/../other")
	assert.NotEmpty(t, result)
	// Should be cleaned
	assert.NotContains(t, result[0], "..")
}

func TestNormalizeWorkspacePath_Relative(t *testing.T) {
	cwd := t.TempDir()
	oldWD, err := os.Getwd()
	assert.NoError(t, err)
	err = os.Chdir(cwd)
	assert.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	got := normalizeWorkspacePath(".")
	assert.Equal(t, normalizeWorkspacePath(cwd), got)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := Input{
		Workspace:  "/full/workspace",
		SessionID:  "sess-full",
		Source:     "claude",
		ClaudeHome: "/home/.claude",
		Scan:       true,
		ScanLimit:  200,
		Summarize:  true,
		Force:      true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.SessionID, decoded.SessionID)
	assert.Equal(t, in.Source, decoded.Source)
	assert.Equal(t, in.ClaudeHome, decoded.ClaudeHome)
	assert.Equal(t, in.Scan, decoded.Scan)
	assert.Equal(t, in.ScanLimit, decoded.ScanLimit)
	assert.Equal(t, in.Summarize, decoded.Summarize)
	assert.Equal(t, in.Force, decoded.Force)
}

func TestOutput_ScanResult(t *testing.T) {
	output := Output{
		Status:           "scanned",
		SessionsScanned:  100,
		SessionsMatched:  20,
		SessionsCaptured: 15,
		SessionsSkipped:  5,
		Message:          "Scanned 100 sessions",
	}

	assert.Equal(t, "scanned", output.Status)
	assert.Equal(t, 100, output.SessionsScanned)
	assert.Equal(t, 20, output.SessionsMatched)
	assert.Equal(t, 15, output.SessionsCaptured)
	assert.Equal(t, 5, output.SessionsSkipped)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := Input{
		Workspace:  "/ws",
		SessionID:  "sess",
		Source:     "claude",
		ClaudeHome: "/home",
		Scan:       true,
		ScanLimit:  10,
		Summarize:  true,
		Force:      true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "workspace")
	assert.Contains(t, jsonStr, "session_id")
	assert.Contains(t, jsonStr, "source")
	assert.Contains(t, jsonStr, "claude_home")
	assert.Contains(t, jsonStr, "scan")
	assert.Contains(t, jsonStr, "scan_limit")
	assert.Contains(t, jsonStr, "summarize")
	assert.Contains(t, jsonStr, "force")
}

func TestNormalizeGitURL_GitLabHTTPS(t *testing.T) {
	result := normalizeGitURL("https://gitlab.com/user/project.git")
	assert.Equal(t, "gitlab.com/user/project", result)
}

func TestNormalizeGitURL_GitLabSSH(t *testing.T) {
	result := normalizeGitURL("git@gitlab.com:user/project.git")
	assert.Equal(t, "gitlab.com/user/project", result)
}

func TestNormalizeGitURL_BitbucketHTTPS(t *testing.T) {
	result := normalizeGitURL("https://bitbucket.org/user/repo.git")
	assert.Equal(t, "bitbucket.org/user/repo", result)
}
