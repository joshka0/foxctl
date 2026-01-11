package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/execution"
)

// SkillRunner executes Go-based hook skills using the execution framework.
type SkillRunner struct {
	// Executor is the skill executor (typically RunnerExecutor).
	Executor execution.SkillExecutor

	// Resolver finds skill manifests and artifacts.
	Resolver SkillResolver
}

// Run executes a skill-based hook.
func (r *SkillRunner) Run(ctx context.Context, hookDef HookDef, input Input) (Output, error) {
	if len(hookDef.Run) == 0 {
		return NewApprove("no skills to run", nil), nil
	}

	// Execute skills in order and merge outputs
	var outputs []Output
	for _, entry := range hookDef.Run {
		// Resolve skill manifest and artifact path
		manifest, artifactPath, err := r.Resolver.Resolve(entry.Skill)
		if err != nil {
			return Output{}, fmt.Errorf("resolve skill %s: %w", entry.Skill, err)
		}

		// Merge hook config into input
		hookInput := input
		hookInput.HookConfig = entry.Config

		// Serialize input
		inputBytes, err := json.Marshal(hookInput)
		if err != nil {
			return Output{}, fmt.Errorf("marshal input for %s: %w", entry.Skill, err)
		}

		// Build extra env vars for the skill
		extraEnv := buildSkillExtraEnv(input)

		// Execute skill
		result, err := r.Executor.Execute(ctx, execution.ExecuteOptions{
			Manifest:     manifest,
			ArtifactPath: artifactPath,
			Input:        inputBytes,
			ExtraEnv:     extraEnv,
		})
		if err != nil {
			return Output{}, fmt.Errorf("execute skill %s: %w", entry.Skill, err)
		}

		// Check for execution error
		if result.Error != nil {
			return Output{}, fmt.Errorf("skill %s failed: %w", entry.Skill, result.Error)
		}

		// Parse skill output
		output, err := parseSkillOutput(result.Stdout)
		if err != nil {
			return Output{}, fmt.Errorf("parse skill %s output: %w", entry.Skill, err)
		}

		outputs = append(outputs, output)
	}

	// Merge all skill outputs
	if len(outputs) == 1 {
		return outputs[0], nil
	}
	return Merge(outputs), nil
}

// parseSkillOutput extracts hook.Output from a skill's envelope output.
func parseSkillOutput(data []byte) (Output, error) {
	// Empty output = approve
	if len(data) == 0 {
		return NewApprove("skill completed", nil), nil
	}

	// Try to parse as envelope format first
	var env struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Command string `json:"command"`
		Data    struct {
			HookOutput Output `json:"hook_output"`
		} `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(data, &env); err == nil && env.Version != 0 {
		// Looks like a valid envelope (has version field)
		if env.Status == "error" {
			return Output{}, fmt.Errorf("skill error: %s: %s", env.Error.Code, env.Error.Message)
		}

		// Extract hook_output from data
		if env.Data.HookOutput.Decision != "" {
			return env.Data.HookOutput, nil
		}

		// Envelope OK but no hook_output - treat as approve
		return NewApprove("skill completed", nil), nil
	}

	// Not an envelope - try direct hook.Output
	var out Output
	if err := json.Unmarshal(data, &out); err != nil {
		return Output{}, fmt.Errorf("invalid output format: %w", err)
	}

	// If it has a decision, it's a valid hook output
	if out.Decision != "" {
		return out, nil
	}

	// No decision field - treat as approve
	return NewApprove("skill completed", nil), nil
}

// buildSkillExtraEnv builds extra environment variables for skill execution.
func buildSkillExtraEnv(input Input) []string {
	var env []string

	if input.SessionID != "" {
		env = append(env, "AGENTCTL_SESSION_ID="+input.SessionID)
	}
	if input.ActorID != "" {
		env = append(env, "AGENTCTL_AGENT_ID="+input.ActorID)
	}
	if input.TraceID != "" {
		env = append(env, "AGENTCTL_TRACE_ID="+input.TraceID)
	}
	if input.CorrelationID != "" {
		env = append(env, "AGENTCTL_CORRELATION_ID="+input.CorrelationID)
	}
	if input.Event != "" {
		env = append(env, "AGENTCTL_HOOK_EVENT="+string(input.Event))
	}

	return env
}
