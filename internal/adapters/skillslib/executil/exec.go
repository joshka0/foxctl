package executil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/protocol"
)

// CmdResult holds the result of a command execution.
type CmdResult struct {
	// ExitCode is the command exit code (0 = success).
	ExitCode int

	// Stdout is the captured standard output.
	Stdout []byte

	// Stderr is the captured standard error.
	Stderr []byte

	// Duration is how long the command took to execute.
	Duration time.Duration

	// Err is the underlying error if execution failed.
	Err error
}

// Success returns true if the command exited with code 0.
func (r CmdResult) Success() bool {
	return r.ExitCode == 0 && r.Err == nil
}

// String returns stdout as a string with trailing whitespace trimmed.
func (r CmdResult) String() string {
	return strings.TrimSpace(string(r.Stdout))
}

// Lines returns stdout split into lines (empty lines removed).
func (r CmdResult) Lines() []string {
	s := strings.TrimSpace(string(r.Stdout))
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// IsNoMatch returns true if this looks like a "no matches found" result.
// This is common for grep/ripgrep where exit code 1 means no matches.
func (r CmdResult) IsNoMatch() bool {
	return r.ExitCode == 1 && len(r.Stderr) == 0
}

// RequireTool checks if a tool is available in PATH and returns its path.
// If not found, returns an error with the install hint.
//
// Example:
//
//	rgPath, err := executil.RequireTool("rg", "install ripgrep: brew install ripgrep")
func RequireTool(name, installHint string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if installHint != "" {
			return "", fmt.Errorf("%s not found in PATH (%s)", name, installHint)
		}
		return "", fmt.Errorf("%s not found in PATH", name)
	}
	return path, nil
}

// RequireAny returns the path of the first available tool from the list.
// If none are found, returns an error with the install hint.
//
// Example:
//
//	pylsp, err := executil.RequireAny([]string{"pylsp", "pyls"}, "pip install python-lsp-server")
func RequireAny(names []string, installHint string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if installHint != "" {
		return "", fmt.Errorf("none of %v found in PATH (%s)", names, installHint)
	}
	return "", fmt.Errorf("none of %v found in PATH", names)
}

// HasTool returns true if the tool is available in PATH.
func HasTool(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// AgentctlBin returns the path to the agentctl binary for subprocess calls.
func AgentctlBin() string {
	if bin := os.Getenv("AGENTCTL_BIN"); bin != "" {
		return bin
	}
	if projDir := os.Getenv("CLAUDE_PROJECT_DIR"); projDir != "" {
		candidate := filepath.Join(projDir, "bin", "agentctl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "bin", "agentctl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("agentctl"); err == nil {
		return path
	}
	return "agentctl"
}

// AgentctlResult captures a decoded envelope with raw output.
type AgentctlResult struct {
	Envelope envelope.Envelope
	Stdout   []byte
	Stderr   []byte
}

// DecodeError wraps errors from decoding agentctl envelope data.
type DecodeError struct {
	Err error
}

func (e DecodeError) Error() string {
	return e.Err.Error()
}

func (e DecodeError) Unwrap() error {
	return e.Err
}

// RunAgentctlSkill executes "agentctl run <skill>" with optional JSON input and decodes the envelope.
func RunAgentctlSkill(ctx context.Context, workspace, skill string, input []byte) (AgentctlResult, error) {
	return RunAgentctlSkillWithArgs(ctx, workspace, skill, input, nil)
}

// RunAgentctlSkillDecode executes "agentctl run <skill>" and decodes its data payload into dst.
func RunAgentctlSkillDecode(ctx context.Context, workspace, skill string, input []byte, dst any) (AgentctlResult, error) {
	return RunAgentctlSkillDecodeWithArgs(ctx, workspace, skill, input, nil, dst)
}

// RunAgentctlSkillWithArgs executes "agentctl run <skill>" with extra CLI args (e.g., --workspace).
func RunAgentctlSkillWithArgs(ctx context.Context, workspace, skill string, input []byte, extraArgs []string) (AgentctlResult, error) {
	if strings.TrimSpace(skill) == "" {
		return AgentctlResult{}, fmt.Errorf("skill is required")
	}

	args := []string{"run", skill}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}
	var result CmdResult
	if len(input) > 0 {
		args = append(args, "--input-file", "-")
		result = RunWithInput(ctx, workspace, AgentctlBin(), input, args...)
	} else {
		result = Run(ctx, workspace, AgentctlBin(), args...)
	}

	out := AgentctlResult{
		Stdout: result.Stdout,
		Stderr: result.Stderr,
	}

	if result.Err != nil {
		return out, fmt.Errorf("run agentctl %s: %w", skill, result.Err)
	}

	env, err := protocol.DecodeEnvelope(result.Stdout)
	if err != nil {
		return out, err
	}
	out.Envelope = env
	if env.Status == envelope.StatusError {
		return out, protocol.EnvelopeStatusErrorFromEnvelope(env)
	}

	return out, nil
}

// RunAgentctlSkillDecodeWithArgs executes "agentctl run <skill>" with extra CLI args and decodes its data payload into dst.
func RunAgentctlSkillDecodeWithArgs(ctx context.Context, workspace, skill string, input []byte, extraArgs []string, dst any) (AgentctlResult, error) {
	if dst == nil {
		return AgentctlResult{}, fmt.Errorf("destination is required")
	}
	result, err := RunAgentctlSkillWithArgs(ctx, workspace, skill, input, extraArgs)
	if err != nil {
		return result, err
	}
	if err := protocol.DecodeEnvelopeDataInto(result.Envelope, dst); err != nil {
		return result, DecodeError{Err: err}
	}
	return result, nil
}

// Start launches a command without waiting for it to finish.
func Start(ctx context.Context, dir, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

// Run executes a command and captures its output.
// It handles context cancellation and captures both stdout and stderr.
//
// Example:
//
//	result := executil.Run(ctx, "/path/to/project", "git", "status", "--porcelain")
//	if result.Success() {
//	    for _, line := range result.Lines() { ... }
//	}
func Run(ctx context.Context, dir, name string, args ...string) CmdResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := CmdResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: duration,
		Err:      err,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// RunWithEnv executes a command with additional environment variables.
func RunWithEnv(ctx context.Context, dir, name string, env []string, args ...string) CmdResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := CmdResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: duration,
		Err:      err,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// RunWithInput executes a command with stdin input.
func RunWithInput(ctx context.Context, dir, name string, input []byte, args ...string) CmdResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := CmdResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		Duration: duration,
		Err:      err,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// Git runs a git command in the given directory.
func Git(ctx context.Context, dir string, args ...string) CmdResult {
	return Run(ctx, dir, "git", args...)
}

// GitOutput runs a git command and returns stdout as string.
// Returns empty string and error if command fails.
func GitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	result := Git(ctx, dir, args...)
	if !result.Success() {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), string(result.Stderr))
	}
	return result.String(), nil
}
