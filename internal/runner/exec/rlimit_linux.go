//go:build linux

package execrunner

import (
	"os/exec"
	"syscall"
)

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
