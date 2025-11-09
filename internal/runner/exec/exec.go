// Package execrunner executes skills in a subprocess with policy enforcement.
package execrunner

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/skill"
)

// Runner executes exec-distributed skills.
type Runner struct {
	Manifest skill.Manifest
	Binary   string
}

// Run executes the skill with the provided input JSON bytes.
func (r Runner) Run(ctx context.Context, input []byte) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, r.Binary)
	cmd.Stdin = bytes.NewReader(input)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = filepath.Dir(r.Binary)
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}
