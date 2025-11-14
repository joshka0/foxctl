// Package runner provides a unified interface for executing skills across different distribution types.
package runner

import (
	"context"
	"fmt"
	"os"

	execrunner "github.com/jkatigb/agentctl/internal/runner/exec"
	wasirunner "github.com/jkatigb/agentctl/internal/runner/wasi"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/jkatigb/agentctl/internal/workspace"
)

// Run executes the appropriate runtime for a manifest.
func Run(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	ws, _ := workspace.FromContext(ctx)
	switch manifest.Distribution.Type {
	case "exec":
		r := execrunner.Runner{Manifest: manifest, Binary: artifactPath}
		if ws != "" {
			env := append(os.Environ(), fmt.Sprintf("AGENTCTL_WORKSPACE=%s", ws))
			r.Options.Env = env
		}
		return r.Run(ctx, input)
	case "wasi":
		r := wasirunner.Runner{Manifest: manifest, ModulePath: artifactPath}
		if ws != "" {
			r.Options.Env = append(r.Options.Env, fmt.Sprintf("AGENTCTL_WORKSPACE=%s", ws))
		}
		return r.Run(ctx, input)
	default:
		return nil, nil, fmt.Errorf("unsupported distribution type %q", manifest.Distribution.Type)
	}
}
