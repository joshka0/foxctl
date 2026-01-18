package executil

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestCmdResult_Success(t *testing.T) {
	tests := []struct {
		name   string
		result CmdResult
		want   bool
	}{
		{
			name:   "success with exit 0",
			result: CmdResult{ExitCode: 0, Err: nil},
			want:   true,
		},
		{
			name:   "failure with exit 1",
			result: CmdResult{ExitCode: 1, Err: nil},
			want:   false,
		},
		{
			name:   "failure with error",
			result: CmdResult{ExitCode: 0, Err: exec.ErrNotFound},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.Success(); got != tt.want {
				t.Errorf("CmdResult.Success() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCmdResult_String(t *testing.T) {
	result := CmdResult{
		Stdout: []byte("  hello world  \n"),
	}
	if got := result.String(); got != "hello world" {
		t.Errorf("CmdResult.String() = %q, want %q", got, "hello world")
	}
}

func TestCmdResult_Lines(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   []string
	}{
		{
			name:   "empty",
			stdout: "",
			want:   nil,
		},
		{
			name:   "single line",
			stdout: "hello",
			want:   []string{"hello"},
		},
		{
			name:   "multiple lines",
			stdout: "line1\nline2\nline3",
			want:   []string{"line1", "line2", "line3"},
		},
		{
			name:   "with empty lines",
			stdout: "line1\n\nline2\n  \nline3\n",
			want:   []string{"line1", "line2", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CmdResult{Stdout: []byte(tt.stdout)}
			got := result.Lines()
			if len(got) != len(tt.want) {
				t.Errorf("CmdResult.Lines() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("CmdResult.Lines()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestCmdResult_IsNoMatch(t *testing.T) {
	tests := []struct {
		name   string
		result CmdResult
		want   bool
	}{
		{
			name:   "no match - exit 1 empty stderr",
			result: CmdResult{ExitCode: 1, Stderr: []byte{}},
			want:   true,
		},
		{
			name:   "has matches - exit 0",
			result: CmdResult{ExitCode: 0, Stderr: []byte{}},
			want:   false,
		},
		{
			name:   "error - exit 1 with stderr",
			result: CmdResult{ExitCode: 1, Stderr: []byte("error message")},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsNoMatch(); got != tt.want {
				t.Errorf("CmdResult.IsNoMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequireTool(t *testing.T) {
	// Test with a tool that should exist on any system
	path, err := RequireTool("sh", "")
	if err != nil {
		t.Errorf("RequireTool(sh) failed: %v", err)
	}
	if path == "" {
		t.Error("RequireTool(sh) returned empty path")
	}

	// Test with a tool that doesn't exist
	_, err = RequireTool("nonexistent-tool-xyz", "install it somehow")
	if err == nil {
		t.Error("RequireTool(nonexistent) should have failed")
	}
	if !strings.Contains(err.Error(), "install it somehow") {
		t.Errorf("error should contain hint: %v", err)
	}
}

func TestRequireAny(t *testing.T) {
	// Test with at least one existing tool
	path, err := RequireAny([]string{"nonexistent1", "sh", "nonexistent2"}, "")
	if err != nil {
		t.Errorf("RequireAny() failed: %v", err)
	}
	if path == "" {
		t.Error("RequireAny() returned empty path")
	}

	// Test with no existing tools
	_, err = RequireAny([]string{"nonexistent1", "nonexistent2"}, "install something")
	if err == nil {
		t.Error("RequireAny() should have failed")
	}
	if !strings.Contains(err.Error(), "install something") {
		t.Errorf("error should contain hint: %v", err)
	}
}

func TestHasTool(t *testing.T) {
	if !HasTool("sh") {
		t.Error("HasTool(sh) should return true")
	}
	if HasTool("nonexistent-tool-xyz") {
		t.Error("HasTool(nonexistent) should return false")
	}
}

func TestAgentctlBin_EnvOverride(t *testing.T) {
	customPath := "/custom/path/to/agentctl"
	t.Setenv("AGENTCTL_BIN", customPath)

	bin := AgentctlBin()
	if bin != customPath {
		t.Errorf("expected AGENTCTL_BIN override %q, got %q", customPath, bin)
	}
}

func TestAgentctlBin_DefaultFallback(t *testing.T) {
	t.Setenv("AGENTCTL_BIN", "")

	bin := AgentctlBin()
	if bin == "" {
		t.Error("AgentctlBin returned empty string when AGENTCTL_BIN is empty")
	}
}

func TestRunAgentctlSkill_EmptySkill(t *testing.T) {
	ctx := context.Background()

	if _, err := RunAgentctlSkill(ctx, "", "", nil); err == nil {
		t.Error("RunAgentctlSkill should fail with empty skill")
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()

	// Test successful command
	result := Run(ctx, "", "echo", "hello")
	if !result.Success() {
		t.Errorf("Run(echo) failed: %v", result.Err)
	}
	if result.String() != "hello" {
		t.Errorf("Run(echo) output = %q, want %q", result.String(), "hello")
	}
	if result.Duration == 0 {
		t.Error("Run() should set Duration")
	}

	// Test command with directory
	result = Run(ctx, "/tmp", "pwd")
	if !result.Success() {
		t.Errorf("Run(pwd) failed: %v", result.Err)
	}
	// On macOS /tmp is a symlink to /private/tmp
	if !strings.Contains(result.String(), "tmp") {
		t.Errorf("Run(pwd) in /tmp got %q", result.String())
	}

	// Test failing command
	result = Run(ctx, "", "sh", "-c", "exit 42")
	if result.Success() {
		t.Error("Run(exit 42) should fail")
	}
	if result.ExitCode != 42 {
		t.Errorf("Run(exit 42) ExitCode = %d, want 42", result.ExitCode)
	}

	// Test context cancellation
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately
	result = Run(cancelCtx, "", "sleep", "10")
	if result.Success() {
		t.Error("Run with cancelled context should fail")
	}
}

func TestRunWithEnv(t *testing.T) {
	ctx := context.Background()

	result := RunWithEnv(ctx, "", "sh", []string{"MY_VAR=test_value"}, "-c", "echo $MY_VAR")
	if !result.Success() {
		t.Errorf("RunWithEnv() failed: %v", result.Err)
	}
	if result.String() != "test_value" {
		t.Errorf("RunWithEnv() output = %q, want %q", result.String(), "test_value")
	}
}

func TestRunWithInput(t *testing.T) {
	ctx := context.Background()

	result := RunWithInput(ctx, "", "cat", []byte("stdin content"))
	if !result.Success() {
		t.Errorf("RunWithInput() failed: %v", result.Err)
	}
	if result.String() != "stdin content" {
		t.Errorf("RunWithInput() output = %q, want %q", result.String(), "stdin content")
	}
}

func TestGit(t *testing.T) {
	if !HasTool("git") {
		t.Skip("git not available")
	}

	ctx := context.Background()

	// Test git version (should work in any directory)
	result := Git(ctx, "", "version")
	if !result.Success() {
		t.Errorf("Git(version) failed: %v", result.Err)
	}
	if !strings.HasPrefix(result.String(), "git version") {
		t.Errorf("Git(version) output = %q", result.String())
	}
}

func TestGitOutput(t *testing.T) {
	if !HasTool("git") {
		t.Skip("git not available")
	}

	ctx := context.Background()

	// Test successful command
	output, err := GitOutput(ctx, "", "version")
	if err != nil {
		t.Errorf("GitOutput(version) failed: %v", err)
	}
	if !strings.HasPrefix(output, "git version") {
		t.Errorf("GitOutput(version) = %q", output)
	}

	// Test failing command
	_, err = GitOutput(ctx, "/nonexistent", "status")
	if err == nil {
		t.Error("GitOutput in nonexistent dir should fail")
	}
}

func TestRun_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := Run(ctx, "", "sleep", "10")
	if result.Success() {
		t.Error("Run with short timeout should fail")
	}
	// Should complete quickly due to timeout
	if result.Duration > 2*time.Second {
		t.Errorf("Run with timeout took too long: %v", result.Duration)
	}
}
