// Package runner provides a unified interface for executing skills across different distribution types.
package runner

import (
	"context"
	"fmt"

	execrunner "github.com/jkatigb/agentctl/internal/runner/exec"
	wasirunner "github.com/jkatigb/agentctl/internal/runner/wasi"
	"github.com/jkatigb/agentctl/internal/skill"
)

// Run executes the appropriate runtime for a manifest.
func Run(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	switch manifest.Distribution.Type {
	case "exec":
		r := execrunner.Runner{Manifest: manifest, Binary: artifactPath}
		return r.Run(ctx, input)
	case "wasi":
		r := wasirunner.Runner{Manifest: manifest, ModulePath: artifactPath}
		return r.Run(ctx, input)
	default:
		return nil, nil, fmt.Errorf("unsupported distribution type %q", manifest.Distribution.Type)
	}
}
