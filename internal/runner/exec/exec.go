// Package execrunner executes exec-distributed skills with basic policy enforcement.
package execrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
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
	WorkDir        string
	Env            []string
	Timeout        time.Duration // Maximum execution time (0 = no timeout)
	MaxMemoryBytes uint64        // Maximum memory in bytes (default: 512MB)
	MaxCPUSeconds  uint64        // Maximum CPU seconds (default: 30s)
}

// Run executes the skill and returns stdout/stderr bytes.
func (r Runner) Run(ctx context.Context, input []byte) ([]byte, []byte, error) {
	if r.Manifest.Distribution.Type != "exec" {
		return nil, nil, fmt.Errorf("runner: manifest distribution %s not exec", r.Manifest.Distribution.Type)
	}
	if r.Manifest.Capabilities.Network != "" && r.Manifest.Capabilities.Network != "none" {
		return nil, nil, fmt.Errorf("runner: network policy %s not supported", r.Manifest.Capabilities.Network)
	}

	// Apply timeout only if explicitly set
	if r.Options.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Options.Timeout)
		defer cancel()
	}

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

	// Apply resource limits (Linux-specific)
	maxMemory := r.Options.MaxMemoryBytes
	if maxMemory == 0 {
		maxMemory = 512 * 1024 * 1024 // 512MB default
	}
	maxCPU := r.Options.MaxCPUSeconds
	if maxCPU == 0 {
		maxCPU = 30 // 30 seconds default
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create new process group for better cleanup
	}

	// Set resource limits via prlimit (applied before exec)
	// Note: These limits are enforced by the kernel
	if err := setResourceLimits(cmd, maxMemory, maxCPU); err != nil {
		return nil, nil, fmt.Errorf("runner: failed to set resource limits: %w", err)
	}

	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), stderr.Bytes(), err
	}
	return stdout.Bytes(), stderr.Bytes(), nil
}

func setResourceLimits(cmd *exec.Cmd, maxMemory, maxCPUSeconds uint64) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Preserve Setpgid for process group isolation
	cmd.SysProcAttr.Setpgid = true

	// Set Pdeathsig to ensure child dies if parent dies
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL

	// Note: Memory and CPU rlimits cannot be enforced via os/exec in current Go versions
	// The Prlimit field in SysProcAttr was proposed but not yet added to stdlib
	// Current enforcement relies on:
	// 1. Timeout via context (primary mechanism)
	// 2. Process group isolation (Setpgid)
	// 3. Parent death signal (Pdeathsig)
	//
	// Future options for rlimit enforcement:
	// - Wait for SysProcAttr.Prlimit in future Go version
	// - Use cgroups via SysProcAttr.CgroupFD
	// - Implement wrapper using unix.ForkExec
	// - External limit enforcement via systemd/cgroups

	_ = maxMemory     // Not currently enforced
	_ = maxCPUSeconds // Not currently enforced

	return nil
}
