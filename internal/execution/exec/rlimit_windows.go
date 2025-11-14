//go:build windows

package execrunner

import (
	"os/exec"
)

func setResourceLimits(cmd *exec.Cmd, maxMemory, maxCPUSeconds uint64) error {
	// Windows does not support Unix-style process groups or rlimits
	// Resource enforcement relies on:
	// 1. Timeout via context (primary mechanism)
	//
	// Future options for Windows:
	// - Job objects for resource limits
	// - Process priority classes
	// - External enforcement via Task Manager/Resource Monitor

	_ = maxMemory     // Not currently enforced on Windows
	_ = maxCPUSeconds // Not currently enforced on Windows

	return nil
}
