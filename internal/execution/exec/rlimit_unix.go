//go:build unix && !linux

package execrunner

import (
	"os/exec"
	"syscall"
)

func setResourceLimits(cmd *exec.Cmd, maxMemory, maxCPUSeconds uint64) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}

	// Set process group isolation for better cleanup
	cmd.SysProcAttr.Setpgid = true

	// Note: Memory and CPU rlimits cannot be enforced via os/exec in current Go versions
	// Current enforcement relies on:
	// 1. Timeout via context (primary mechanism)
	// 2. Process group isolation (Setpgid)
	//
	// Platform-specific features like Pdeathsig are Linux-only

	_ = maxMemory     // Not currently enforced
	_ = maxCPUSeconds // Not currently enforced

	return nil
}
