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

	// Set process group isolation for better cleanup
	cmd.SysProcAttr.Setpgid = true

	// Set Pdeathsig to ensure child dies if parent dies
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL

	// Build rlimit array if any limits are requested
	// Note: Zero means no limit per Options field documentation
	var rlimits []syscall.Rlimit

	if maxMemory > 0 {
		rlimits = append(rlimits, syscall.Rlimit{
			Cur: maxMemory,
			Max: maxMemory,
		})
	}

	if maxCPUSeconds > 0 {
		rlimits = append(rlimits, syscall.Rlimit{
			Cur: maxCPUSeconds,
			Max: maxCPUSeconds,
		})
	}

	// Note: Go's os/exec doesn't provide Prlimit field until Go 1.23+
	// For now, rlimit enforcement requires external tools or cgroups
	// The limits are documented in Options but enforcement is best-effort:
	// 1. Timeout via context (always enforced)
	// 2. Process group isolation (Setpgid)
	// 3. Parent death signal (Pdeathsig)
	// 4. Memory/CPU limits (requires Go 1.23+ or external enforcement)
	//
	// Future: When Go 1.23+ is adopted, use:
	//   cmd.SysProcAttr.Prlimit = []unix.Rlimit{...}

	_ = rlimits // Prepared but not yet enforceable via stdlib

	return nil
}
