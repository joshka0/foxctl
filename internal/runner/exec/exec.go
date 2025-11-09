// Package execrunner executes exec-distributed skills with basic policy enforcement.
package execrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/jkatigb/agentctl/internal/skill"
)

// Runner executes exec-based skills.
type Runner struct {
	Manifest skill.Manifest
	Binary   string
	Options  Options
}

// Options control execution behavior.
type Options struct {
	WorkDir string
	Env     []string
	Timeout time.Duration // Execution timeout (0 = no timeout, default 5 minutes)
}

// Run executes the skill and returns stdout/stderr bytes.
func (r Runner) Run(ctx context.Context, input []byte) ([]byte, []byte, error) {
	if r.Manifest.Distribution.Type != "exec" {
		return nil, nil, fmt.Errorf("runner: manifest distribution %s not exec", r.Manifest.Distribution.Type)
	}
	if r.Manifest.Capabilities.Network != "" && r.Manifest.Capabilities.Network != "none" {
		return nil, nil, fmt.Errorf("runner: network policy %s not supported", r.Manifest.Capabilities.Network)
	}

	// Apply timeout (default 5 minutes if not specified)
	timeout := r.Options.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := r.Options.WorkDir
	var cleanup func()
	if workDir == "" {
		tmp, err := os.MkdirTemp("", "agentctl-skill-")
		if err != nil {
			return nil, nil, err
		}
		workDir = tmp
		cleanup = func() { _ = os.RemoveAll(tmp) }
	} else {
		cleanup = func() {}
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, r.Binary)
	cmd.Dir = workDir
	cmd.Stdin = bytes.NewReader(input)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	env := r.Options.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	env = append(env, fmt.Sprintf("SKILL_NAME=%s", r.Manifest.Metadata.Name))
	env = append(env, fmt.Sprintf("SKILL_VERSION=%s", r.Manifest.Metadata.Version))
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}
