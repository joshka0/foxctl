package executil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
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

// HasRunnableTool returns true if the tool resolves in PATH and can be executed.
// A non-zero exit code still counts as runnable; launch failures (for example stale shebang targets) do not.
func HasRunnableTool(ctx context.Context, name string, probeArgs ...string) bool {
	_, err := ResolveRunnableTool(ctx, name, probeArgs...)
	return err == nil
}

// ResolveRunnableTool resolves a tool in PATH and verifies it can be launched.
func ResolveRunnableTool(ctx context.Context, name string, probeArgs ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(probeCtx, path, probeArgs...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	err = cmd.Run()
	if err == nil {
		return path, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return path, nil
	}
	return "", err
}

// FoxctlBin returns the path to the foxctl binary for subprocess calls.
func FoxctlBin() string {
	if bin := os.Getenv("FOXCTL_BIN"); bin != "" {
		return bin
	}
	if projDir := os.Getenv("CLAUDE_PROJECT_DIR"); projDir != "" {
		candidate := filepath.Join(projDir, "bin", "foxctl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		candidate := filepath.Join(cwd, "bin", "foxctl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if path, err := exec.LookPath("foxctl"); err == nil {
		return path
	}
	return "foxctl"
}

// FoxctlResult captures a decoded envelope with raw output.
type FoxctlResult struct {
	Envelope envelope.Envelope
	Stdout   []byte
	Stderr   []byte
}

// DecodeError wraps errors from decoding foxctl envelope data.
type DecodeError struct {
	Err error
}

func (e DecodeError) Error() string {
	return e.Err.Error()
}

func (e DecodeError) Unwrap() error {
	return e.Err
}

// RunFoxctlSkill executes "foxctl run <skill>" with optional JSON input and decodes the envelope.
func RunFoxctlSkill(ctx context.Context, workspace, skill string, input []byte) (FoxctlResult, error) {
	return RunFoxctlSkillWithArgs(ctx, workspace, skill, input, nil)
}

// RunFoxctlSkillDecode executes "foxctl run <skill>" and decodes its data payload into dst.
func RunFoxctlSkillDecode(ctx context.Context, workspace, skill string, input []byte, dst any) (FoxctlResult, error) {
	return RunFoxctlSkillDecodeWithArgs(ctx, workspace, skill, input, nil, dst)
}

// RunFoxctlSkillWithArgs executes "foxctl run <skill>" with extra CLI args (e.g., --workspace).
func RunFoxctlSkillWithArgs(ctx context.Context, workspace, skill string, input []byte, extraArgs []string) (FoxctlResult, error) {
	if strings.TrimSpace(skill) == "" {
		return FoxctlResult{}, fmt.Errorf("skill is required")
	}

	args := []string{"run", skill}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}
	var result CmdResult
	if len(input) > 0 {
		args = append(args, "--input-file", "-")
		result = RunWithInput(ctx, workspace, FoxctlBin(), input, args...)
	} else {
		result = Run(ctx, workspace, FoxctlBin(), args...)
	}

	out := FoxctlResult{
		Stdout: result.Stdout,
		Stderr: result.Stderr,
	}

	if result.Err != nil {
		return out, fmt.Errorf("run foxctl %s: %w", skill, result.Err)
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

// RunFoxctlSkillDecodeWithArgs executes "foxctl run <skill>" with extra CLI args and decodes its data payload into dst.
func RunFoxctlSkillDecodeWithArgs(ctx context.Context, workspace, skill string, input []byte, extraArgs []string, dst any) (FoxctlResult, error) {
	if dst == nil {
		return FoxctlResult{}, fmt.Errorf("destination is required")
	}
	result, err := RunFoxctlSkillWithArgs(ctx, workspace, skill, input, extraArgs)
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

// RunWithInputEnv executes a command with stdin input and additional environment variables.
func RunWithInputEnv(ctx context.Context, dir, name string, env []string, input []byte, args ...string) CmdResult {
	start := time.Now()

	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
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
