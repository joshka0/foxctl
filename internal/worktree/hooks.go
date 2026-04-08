package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RunHook executes a hook command with the given environment variables and timeout.
// If the command is empty, it returns nil immediately (no-op).
// The hook runs in its own process group and is killed if the timeout is exceeded.
// The hook's stdout and stderr are discarded.
//
// This is part of the imperative shell — it performs IO.
func RunHook(ctx context.Context, command string, env map[string]string, timeout time.Duration) error {
	if command == "" {
		return nil
	}

	if err := ctx.Err(); err != nil {
		return fmt.Errorf("hook: %w", err)
	}

	// Create a derived context with the specified timeout
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(hookCtx, "sh", "-c", command)

	// Build environment: inherit current + add hook-specific vars
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Discard output to avoid polluting stdout (envelopes-only rule)
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		if hookCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hook: timed out after %s: %s", timeout, command)
		}
		if hookCtx.Err() == context.Canceled {
			return fmt.Errorf("hook: cancelled: %w", ctx.Err())
		}
		return fmt.Errorf("hook: %q failed: %w", command, err)
	}

	return nil
}
