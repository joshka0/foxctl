// Package execrunner executes exec-distributed skills with basic policy enforcement.
package execrunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/errors"
)

// Buffer pool configuration
// maxBufferPoolSize limits the size of buffers returned to the pool to prevent
// memory bloat. Buffers larger than this limit are discarded rather than pooled.
// This prevents a single large output from permanently consuming pool memory.
const maxBufferPoolSize = 1 << 20 // 1MB

// bufferPool reuses byte buffers for stdout/stderr capture to reduce allocations.
// Usage pattern:
//  1. Get buffer from pool with type assertion check
//  2. Reset the buffer before use
//  3. Use buffer for command output
//  4. Check capacity before returning to pool (prevents memory bloat)
var bufferPool = sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

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
	MaxMemoryBytes uint64        // Maximum memory in bytes (0 = no limit)
	MaxCPUSeconds  uint64        // Maximum CPU seconds (0 = no limit)
}

// Run executes the skill and returns stdout/stderr bytes.
func (r Runner) Run(ctx context.Context, input []byte) ([]byte, []byte, error) {
	if r.Manifest.Distribution.Type != "exec" {
		return nil, nil, fmt.Errorf("runner: manifest distribution %s not exec", r.Manifest.Distribution.Type)
	}
	switch r.Manifest.Capabilities.Network {
	case "", "none", "egress":
		// allowed for exec runner; egress is enforced at the policy layer and by the skill itself
	default:
		return nil, nil, fmt.Errorf("exec runner only supports network capability \"none\" or \"egress\" (got %q)", r.Manifest.Capabilities.Network)
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
		cleanup = func() { errors.Ignore(os.RemoveAll(tmp), "exec runner temp cleanup") }
	} else {
		cleanup = func() {}
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, r.Binary)
	cmd.Dir = workDir
	cmd.Stdin = bytes.NewReader(input)
	stdout, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		return nil, nil, fmt.Errorf("runner: failed to get stdout buffer from pool")
	}
	stderr, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		bufferPool.Put(stdout)
		return nil, nil, fmt.Errorf("runner: failed to get stderr buffer from pool")
	}
	stdout.Reset()
	stderr.Reset()
	defer func() {
		// Only return to pool if buffer hasn't grown too large
		if stdout.Cap() < maxBufferPoolSize {
			bufferPool.Put(stdout)
		}
	}()
	defer func() {
		if stderr.Cap() < maxBufferPoolSize {
			bufferPool.Put(stderr)
		}
	}()
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	env := r.Options.Env
	if len(env) == 0 {
		env = os.Environ()
	}

	// Ensure HOME is available to the skill; some environments may not propagate it.
	// Read from env slice first for consistency, then fall back to os.UserHomeDir.
	home := getEnvVar(env, "HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			home = h
		}
	}
	if home == "" {
		// Last resort: avoid using temp directories as HOME, use root instead
		fallback := filepath.Dir(workDir)
		if !strings.HasPrefix(fallback, os.TempDir()) {
			home = fallback
		} else {
			// Temp dir parent is not safe, try harder or use empty
			if h, err := os.UserHomeDir(); err == nil && h != "" {
				home = h
			}
			// If still empty, leave HOME unset rather than pointing to temp
		}
	}
	if home != "" {
		env = ensureEnvVar(env, "HOME", home)
	}

	// Ensure AGENTCTL_HOME is available when not explicitly set.
	// Read from env slice for consistency.
	if getEnvVar(env, "AGENTCTL_HOME") == "" && home != "" {
		env = ensureEnvVar(env, "AGENTCTL_HOME", filepath.Join(home, ".agentctl"))
	}

	env = append(env, fmt.Sprintf("SKILL_NAME=%s", r.Manifest.Metadata.Name))
	env = append(env, fmt.Sprintf("SKILL_VERSION=%s", r.Manifest.Metadata.Version))
	cmd.Env = env

	// Apply resource limits if specified (0 = no limit per Options field docs)
	// setResourceLimits is best-effort and platform-specific
	if err := setResourceLimits(cmd, r.Options.MaxMemoryBytes, r.Options.MaxCPUSeconds); err != nil {
		return nil, nil, fmt.Errorf("runner: failed to set resource limits: %w", err)
	}

	err := cmd.Run()

	// Clone output so returned slices don't alias pooled buffers.
	stdoutBytes := append([]byte(nil), stdout.Bytes()...)
	stderrBytes := append([]byte(nil), stderr.Bytes()...)
	if err != nil {
		return stdoutBytes, stderrBytes, err
	}
	return stdoutBytes, stderrBytes, nil
}

// getEnvVar reads a value from the env slice, returning empty string if not found.
func getEnvVar(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

func ensureEnvVar(env []string, key, value string) []string {
	if key == "" || value == "" {
		return env
	}
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			// If it's empty, replace; otherwise leave as-is.
			if len(kv) == len(prefix) {
				env[i] = prefix + value
			}
			return env
		}
	}
	return append(env, prefix+value)
}
