package gopls

import (
	"os/exec"
	"testing"
)

// resetGlobalDaemon clears the global daemon state for testing.
func resetGlobalDaemon() {
	daemonMu.Lock()
	defer daemonMu.Unlock()
	if globalDaemon != nil {
		globalDaemon.Close()
		globalDaemon = nil
	}
}

func TestIsDaemonReady_NoDaemon(t *testing.T) {
	resetGlobalDaemon()

	if IsDaemonReady("/some/workspace") {
		t.Error("IsDaemonReady should return false when no daemon exists")
	}
}

func TestIsDaemonReady_WrongWorkspace(t *testing.T) {
	resetGlobalDaemon()

	// Set up a fake daemon with a different workspace
	daemonMu.Lock()
	globalDaemon = &Daemon{
		workspace: "/workspace/a",
		cmd:       &exec.Cmd{}, // Non-nil to pass isAlive check partially
	}
	daemonMu.Unlock()
	defer resetGlobalDaemon()

	if IsDaemonReady("/workspace/b") {
		t.Error("IsDaemonReady should return false for different workspace")
	}
}

func TestIsDaemonReady_MatchingWorkspace_NilProcess(t *testing.T) {
	resetGlobalDaemon()

	// Set up a fake daemon with matching workspace but nil process (dead)
	daemonMu.Lock()
	globalDaemon = &Daemon{
		workspace: "/workspace/a",
		cmd:       nil, // nil cmd means not alive
	}
	daemonMu.Unlock()
	defer resetGlobalDaemon()

	if IsDaemonReady("/workspace/a") {
		t.Error("IsDaemonReady should return false when process is nil")
	}
}

func TestIsDaemonReady_MatchingWorkspace_NilCmd(t *testing.T) {
	resetGlobalDaemon()

	// Set up a fake daemon with matching workspace but nil cmd.Process
	daemonMu.Lock()
	globalDaemon = &Daemon{
		workspace: "/workspace/a",
		cmd:       &exec.Cmd{}, // Non-nil cmd but Process is nil
	}
	daemonMu.Unlock()
	defer resetGlobalDaemon()

	if IsDaemonReady("/workspace/a") {
		t.Error("IsDaemonReady should return false when cmd.Process is nil")
	}
}

// Note: Testing a truly "alive" daemon would require starting a real process,
// which is slow and environment-dependent. The above tests verify the
// negative cases. Integration tests or manual testing covers the positive case.
