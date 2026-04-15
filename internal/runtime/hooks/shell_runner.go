package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// ShellRunner executes shell scripts that receive hook.Input on stdin
// and return hook.Output on stdout as JSON.
type ShellRunner struct {
	// ScriptPath is the absolute path to the shell script.
	// If empty, uses the skill name from HookDef.Run entries.
	ScriptPath string
}

// Run executes a shell script hook.
// Run executes the configured shell hook and parses its output.
//
// Index:
// - Purpose: Execute shell-based hook script and parse output
// - Flow: resolve script path → marshal input → run command → parse output
// - SideEffects: executes shell script; sets environment variables
// - FailureModes: missing script path, command errors, output parse errors
// - Related: parseShellOutput, buildHookEnv
// - Keywords: shell_hook, hook_output, FOXCTL_HOOK_EVENT, script_path, shell_runner
func (r *ShellRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	// Determine script path - either from runner config or from hook definition
	scriptPath := r.ScriptPath
	if scriptPath == "" && len(hookDef.Run) > 0 {
		scriptPath = hookDef.Run[0].Skill
	}
	if scriptPath == "" {
		return Output{}, fmt.Errorf("shell runner: no script path configured")
	}

	// Serialize input to JSON
	inputBytes, err := json.Marshal(input)
	if err != nil {
		return Output{}, fmt.Errorf("shell runner: marshal input: %w", err)
	}

	// Create command with timeout context
	cmd := exec.CommandContext(ctx, "/bin/sh", scriptPath)
	cmd.Stdin = bytes.NewReader(inputBytes)
	cmd.Dir = input.WorkspaceRoot
	cmd.Env = buildHookEnv(input)

	// Capture stdout/stderr
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run and handle errors
	if err := cmd.Run(); err != nil {
		// Check if context was canceled/timed out
		if ctx.Err() != nil {
			return Output{}, fmt.Errorf("shell runner: %w", ctx.Err())
		}
		return Output{}, fmt.Errorf("shell runner: %w (stderr: %s)", err, stderr.String())
	}

	// Parse output
	return parseShellOutput(stdout.Bytes())
}

// parseShellOutput handles various output formats from shell hooks.
func parseShellOutput(data []byte) (Output, error) {
	// Empty output = no-op (approve with no actions)
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return NewApprove("no output", nil), nil
	}

	// Try direct hook.Output format first (v1 format)
	var out Output
	if err := json.Unmarshal(trimmed, &out); err == nil {
		// Valid JSON - check if it has a decision field
		if out.Decision != "" {
			return out, nil
		}
	}

	// Try legacy format: { hookSpecificOutput: { additionalContext: "..." } }
	var legacy struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(trimmed, &legacy); err == nil && legacy.HookSpecificOutput.AdditionalContext != "" {
		return Output{
			Decision: DecisionNone,
			Context:  legacy.HookSpecificOutput.AdditionalContext,
		}, nil
	}

	// Try another legacy format: { context: "..." }
	var contextOnly struct {
		Context string `json:"context"`
	}
	if err := json.Unmarshal(trimmed, &contextOnly); err == nil && contextOnly.Context != "" {
		return Output{
			Decision: DecisionNone,
			Context:  contextOnly.Context,
		}, nil
	}

	// Valid JSON but no recognized format - treat as metadata
	var rawJSON map[string]any
	if err := json.Unmarshal(trimmed, &rawJSON); err == nil {
		return Output{
			Decision: DecisionNone,
			Meta:     rawJSON,
		}, nil
	}

	// Non-JSON output - treat as plain text context
	return Output{
		Decision: DecisionNone,
		Context:  string(trimmed),
	}, nil
}

// buildHookEnv constructs environment variables for shell hook execution.
func buildHookEnv(input Input) []string {
	env := os.Environ()

	// Add standard hook environment variables
	env = append(env,
		"FOXCTL_HOOK_EVENT="+string(input.Event),
	)

	if input.WorkspaceRoot != "" {
		env = append(env, "FOXCTL_WORKSPACE="+input.WorkspaceRoot)
	}
	if input.SessionID != "" {
		env = append(env, "FOXCTL_SESSION_ID="+input.SessionID)
	}
	if input.ActorID != "" {
		env = append(env, "FOXCTL_AGENT_ID="+input.ActorID)
	}
	if input.ToolName != "" {
		env = append(env, "FOXCTL_TOOL_NAME="+input.ToolName)
	}
	if input.TraceID != "" {
		env = append(env, "FOXCTL_TRACE_ID="+input.TraceID)
	}
	if input.CorrelationID != "" {
		env = append(env, "FOXCTL_CORRELATION_ID="+input.CorrelationID)
	}

	return env
}
