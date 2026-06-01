package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_SourceValues(t *testing.T) {
	sources := []string{"claude", "codex"}

	for _, source := range sources {
		in := Input{Source: source}
		assert.Equal(t, source, in.Source)
	}
}

// Tests for Output structure

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
