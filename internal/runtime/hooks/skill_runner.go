package hooks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/maputil"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/execution"
)

// SkillRunner executes Go-based hook skills using the execution framework.
type SkillRunner struct {
	// Executor is the skill executor (typically RunnerExecutor).
	Executor execution.SkillExecutor

	// Resolver finds skill manifests and artifacts.
	Resolver SkillResolver
}

// Run executes a skill-based hook.
// Run executes hook skills in order and merges their outputs.
//
// Index:
//
//	Purpose: Execute hook skills and merge outputs into a decision
//	Keywords: hook_skill, skill_runner, hook_output, execute, merge
//	Related: SkillResolver.Resolve, Merge, parseSkillOutput
//	Flow: resolve skills → marshal input → execute → parse outputs → merge
//	Resources: execution.SkillExecutor, SkillResolver
//	Events: none
//	OutputFields: Output
//
// [[protocol:hook-skill-execution]]
// [[invariant:skill-output-validation]]
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

	if declaresEnvelopeVersion(data) {
		env, err := protocol.DecodeEnvelope(data)
		if err != nil {
			return Output{}, fmt.Errorf("invalid envelope: %w", err)
		}
		if err := protocol.Validate(env); err != nil {
			return Output{}, fmt.Errorf("invalid envelope: %w", err)
		}
		if env.Status == envelope.StatusError {
			return Output{}, protocol.EnvelopeStatusErrorFromEnvelope(env)
		}
		if payload, ok := maputil.AsStringMap(env.Data); ok {
			if hookRaw, ok := payload["hook_output"]; ok {
				hookOutput, err := decodeHookOutput(hookRaw)
				if err != nil {
					return Output{}, fmt.Errorf("invalid hook_output: %w", err)
				}
				if hookOutput.Decision != "" {
					if !hookOutput.Decision.IsValid() {
						return Output{}, fmt.Errorf("invalid hook decision %q", hookOutput.Decision)
					}
					return hookOutput, nil
				}
			}
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
		if !out.Decision.IsValid() {
			return Output{}, fmt.Errorf("invalid hook decision %q", out.Decision)
		}
		return out, nil
	}

	// No decision field - treat as approve
	return NewApprove("skill completed", nil), nil
}

func declaresEnvelopeVersion(data []byte) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(data, &payload); err != nil {
		return false
	}
	_, ok := payload["version"]
	return ok
}

func decodeHookOutput(value any) (Output, error) {
	switch v := value.(type) {
	case Output:
		return v, nil
	case map[string]any:
		payload, err := json.Marshal(v)
		if err != nil {
			return Output{}, err
		}
		var out Output
		if err := json.Unmarshal(payload, &out); err != nil {
			return Output{}, err
		}
		return out, nil
	default:
		return Output{}, fmt.Errorf("expected object, got %T", value)
	}
}

// buildSkillExtraEnv builds extra environment variables for skill execution.
func buildSkillExtraEnv(input Input) []string {
	var env []string

	if input.SessionID != "" {
		env = append(env, "FOXCTL_SESSION_ID="+input.SessionID)
	}
	if input.ActorID != "" {
		env = append(env, "FOXCTL_AGENT_ID="+input.ActorID)
	}
	if input.TraceID != "" {
		env = append(env, "FOXCTL_TRACE_ID="+input.TraceID)
	}
	if input.CorrelationID != "" {
		env = append(env, "FOXCTL_CORRELATION_ID="+input.CorrelationID)
	}
	if input.Event != "" {
		env = append(env, "FOXCTL_HOOK_EVENT="+string(input.Event))
	}

	return env
}
