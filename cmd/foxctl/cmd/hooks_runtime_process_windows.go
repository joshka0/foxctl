//go:build windows

package cmd

import "os/exec"

func setDetachedProcessGroup(cmd *exec.Cmd) {
	_ = cmd
}
