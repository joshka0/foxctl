package runner

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/env"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	execrunner "github.com/joshka0/foxctl/internal/runtime/execution/exec"
	k8ssandbox "github.com/joshka0/foxctl/internal/runtime/execution/k8ssandbox"
	wasirunner "github.com/joshka0/foxctl/internal/runtime/execution/wasi"
)

// sessionEnvVars lists environment variables that should be propagated to skills
// for session lineage tracking. These enable skills to identify the current session
// context regardless of which AI coding tool invoked them.
var sessionEnvVars = []string{
	"FOXCTL_SESSION_ID",   // Canonical session ID (highest priority)
	"FOXCTL_AGENT_ID",     // Agent identifier (foxctl, subagent:X, etc.)
	"CLAUDE_SESSION_ID",   // Claude Code session ID
	"OPENCODE_SESSION_ID", // OpenCode session ID
	"CURSOR_SESSION_ID",   // Cursor session ID
	"TERM_SESSION_ID",     // Terminal session ID
}

// buildSkillEnv constructs the environment for skill execution.
// For exec runners, baseEnv is os.Environ() and session vars pass through automatically.
// For WASI runners, baseEnv is nil and propagateSessionVars must be true to explicitly
// propagate session-related vars since WASI starts with an empty environment.
func buildSkillEnv(baseEnv []string, workspace string, propagateSessionVars bool) []string {
	envVars := baseEnv

	// Add workspace if provided
	if workspace != "" {
		envVars = append(envVars, fmt.Sprintf("FOXCTL_WORKSPACE=%s", workspace))
	}

	// For WASI or other isolated runtimes, explicitly propagate session vars from parent
	if propagateSessionVars {
		for _, key := range sessionEnvVars {
			if val := env.GetString(key); val != "" {
				envVars = append(envVars, fmt.Sprintf("%s=%s", key, val))
			}
		}
	}

	return envVars
}

// RunOptions contains parameters for executing a skill.
type RunOptions struct {
	Manifest     skill.Manifest
	ArtifactPath string
	Input        []byte
	// WorkDir overrides the default working directory when running the skill.
	// If empty, the detected workspace is used.
	WorkDir string
	// ExtraEnv contains additional environment variables to pass to the skill.
	// Format: []string{"KEY=value", "KEY2=value2"}
	ExtraEnv []string
}

// RunWithOptions executes the appropriate runtime for a manifest using structured options.
//
// Index:
//
//	Purpose: Execute a skill using exec or WASI runtime with explicit options
//	Keywords: run_options, exec, wasi, work_dir, extra_env, manifest, artifact_path, runner
//	Related: Run, buildSkillEnv, execrunner.Runner, wasirunner.Runner
//	Flow: resolve workspace/workdir → select runner → build env → run → return output
//	Resources: execrunner.Runner, wasirunner.Runner
//	Events: none
//	OutputFields: stdout, stderr, error
//
// [[protocol:skill-runtime-dispatch]]
// [[invariant:session-var-propagation]]
func RunWithOptions(ctx context.Context, opts RunOptions) ([]byte, []byte, error) {
	ws, _ := workspace.FromContext(ctx)
	if strings.TrimSpace(ws) == "" {
		if envWS := env.GetString("FOXCTL_WORKSPACE"); envWS != "" {
			ws = workspace.Normalize(envWS)
		}
		if ws == "" {
			ws = workspace.Detect("")
		}
	}
	workDir := strings.TrimSpace(opts.WorkDir)
	if workDir == "" {
		workDir = ws
	} else {
		workDir = workspace.Normalize(workDir)
		ws = workDir
	}

	switch opts.Manifest.Distribution.Type {
	case "exec":
		r := execrunner.Runner{Manifest: opts.Manifest, Binary: opts.ArtifactPath}
		// exec inherits os.Environ(), so session vars pass through automatically (propagate=false)
		r.Options.Env = buildSkillEnv(os.Environ(), ws, false)
		// Append extra env vars (these override any existing values)
		r.Options.Env = append(r.Options.Env, opts.ExtraEnv...)
		// Set working directory to workspace so skills can detect git repo, etc.
		r.Options.WorkDir = workDir
		return r.Run(ctx, opts.Input)
	case "wasi":
		r := wasirunner.Runner{Manifest: opts.Manifest, ModulePath: opts.ArtifactPath}
		// WASI starts with empty env, so we must explicitly propagate session vars (propagate=true)
		r.Options.Env = buildSkillEnv(nil, ws, true)
		// Append extra env vars
		r.Options.Env = append(r.Options.Env, opts.ExtraEnv...)
		// Set working directory to workspace so skills can detect git repo, etc.
		r.Options.WorkDir = workDir
		return r.Run(ctx, opts.Input)
	case "k8s-sandbox":
		// k8s-sandbox runs commands inside a Kubernetes agent-sandbox pod.
		// Config is read from FOXCTL_K8S_SANDBOX_* environment variables.
		sbRunner, err := k8ssandbox.NewRunner(ctx, k8ssandbox.Config{
			WarmPool:         os.Getenv("FOXCTL_K8S_SANDBOX_WARMPOOL"),
			Namespace:        os.Getenv("FOXCTL_K8S_SANDBOX_NAMESPACE"),
			Mode:             os.Getenv("FOXCTL_K8S_SANDBOX_MODE"),
			APIURL:           os.Getenv("FOXCTL_K8S_SANDBOX_API_URL"),
			GatewayName:      os.Getenv("FOXCTL_K8S_SANDBOX_GATEWAY"),
			GatewayNamespace: os.Getenv("FOXCTL_K8S_SANDBOX_GATEWAY_NS"),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("k8s-sandbox runner init: %w", err)
		}
		defer sbRunner.Close(ctx)
		// ArtifactPath is the command to execute inside the sandbox.
		command := opts.ArtifactPath
		if command == "" {
			return nil, nil, fmt.Errorf("k8s-sandbox requires artifact_path (command to execute)")
		}
		// The sandbox runner implements the SkillExecutor interface.
		sbResult, err := sbRunner.ExecuteRaw(ctx, command, opts.Input, append(buildSkillEnv(nil, ws, true), opts.ExtraEnv...))
		if err != nil {
			return nil, nil, fmt.Errorf("k8s-sandbox execute: %w", err)
		}
		return sbResult.Stdout, sbResult.Stderr, nil
	default:
		return nil, nil, fmt.Errorf("unsupported distribution type %q", opts.Manifest.Distribution.Type)
	}
}

// Run executes the appropriate runtime for a manifest.
// Deprecated: Use RunWithOptions for better clarity and extensibility.
//
// Index:
//
//	Purpose: Execute a skill using exec or WASI runtime
//	Keywords: runner, exec, wasi, manifest
//	Related: RunWithOptions
//	Flow: select runner → prepare env/workdir → run → return output
//	Resources: execrunner.Runner, wasirunner.Runner
//	Events: none
//	OutputFields: stdout, stderr, error
func Run(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	return RunWithOptions(ctx, RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}
