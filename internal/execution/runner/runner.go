// Package runner provides a unified interface for executing skills across different distribution types.
package runner

import (
	"context"
	"fmt"
	"os"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	execrunner "github.com/jkatigb/agentctl/internal/execution/exec"
	wasirunner "github.com/jkatigb/agentctl/internal/execution/wasi"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

// RunOptions contains parameters for executing a skill.
type RunOptions struct {
	Manifest     skill.Manifest
	ArtifactPath string
	Input        []byte
}

// RunWithOptions executes the appropriate runtime for a manifest using structured options.
func RunWithOptions(ctx context.Context, opts RunOptions) ([]byte, []byte, error) {
	ws, _ := workspace.FromContext(ctx)
	switch opts.Manifest.Distribution.Type {
	case "exec":
		r := execrunner.Runner{Manifest: opts.Manifest, Binary: opts.ArtifactPath}
		if ws != "" {
			env := append(os.Environ(), fmt.Sprintf("AGENTCTL_WORKSPACE=%s", ws))
			r.Options.Env = env
		}
		return r.Run(ctx, opts.Input)
	case "wasi":
		r := wasirunner.Runner{Manifest: opts.Manifest, ModulePath: opts.ArtifactPath}
		if ws != "" {
			r.Options.Env = append(r.Options.Env, fmt.Sprintf("AGENTCTL_WORKSPACE=%s", ws))
		}
		return r.Run(ctx, opts.Input)
	default:
		return nil, nil, fmt.Errorf("unsupported distribution type %q", opts.Manifest.Distribution.Type)
	}
}

// Run executes the appropriate runtime for a manifest.
// Deprecated: Use RunWithOptions for better clarity and extensibility.
func Run(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	return RunWithOptions(ctx, RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}
