// Package executil provides command execution helpers for skills.
package executil

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
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
