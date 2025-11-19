// Package wasirunner executes WASI-distributed skills via wazero (pure Go).
package wasirunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Buffer pool configuration
// maxBufferPoolSize limits the size of buffers returned to the pool to prevent
// memory bloat. Buffers larger than this limit are discarded rather than pooled.
// This prevents a single large WASM output from permanently consuming pool memory.
const maxBufferPoolSize = 1 << 20 // 1MB

// bufferPool reuses byte buffers for stdout/stderr capture to reduce allocations.
// Usage pattern:
//  1. Get buffer from pool with type assertion check
//  2. Reset the buffer before use
//  3. Use buffer for WASM module output
//  4. Check capacity before returning to pool (prevents memory bloat)
var bufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// Runner executes WASI modules that follow the agentctl contract.
type Runner struct {
	Manifest   skill.Manifest
	ModulePath string
	Options    Options
}

// Options control execution behavior.
type Options struct {
	WorkDir string
	Env     []string
	Timeout time.Duration // Execution timeout (0 = no timeout, default 5 minutes)
}

// Run executes the WASI module and captures stdout/stderr.
func (r Runner) Run(ctx context.Context, input []byte) ([]byte, []byte, error) {
	if err := r.validateManifest(); err != nil {
		return nil, nil, err
	}

	ctx, cancel := r.applyTimeout(ctx)
	defer cancel()

	modulePath, err := r.resolveModulePath()
	if err != nil {
		return nil, nil, err
	}
	moduleBytes, err := r.loadModule(modulePath)
	if err != nil {
		return nil, nil, err
	}

	workDir, cleanup, err := r.prepareWorkDir()
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	stdout, stderr, err := r.allocateBuffers()
	if err != nil {
		return nil, nil, err
	}
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

	runtime, closeRuntime, err := r.prepareRuntime(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer closeRuntime()

	compiled, closeCompiled, err := r.compileModule(ctx, runtime, moduleBytes)
	if err != nil {
		return nil, nil, err
	}
	defer closeCompiled()

	modConfig := r.buildModuleConfig(input, workDir, stdout, stderr)

	_, runErr := runtime.InstantiateModule(ctx, compiled, modConfig)

	// Clone output so returned slices don't alias pooled buffers.
	stdoutBytes := append([]byte(nil), stdout.Bytes()...)
	stderrBytes := append([]byte(nil), stderr.Bytes()...)
	return stdoutBytes, stderrBytes, runErr
}

func (r Runner) envVars() map[string]string {
	env := map[string]string{
		"SKILL_NAME":    r.Manifest.Metadata.Name,
		"SKILL_VERSION": r.Manifest.Metadata.Version,
	}
	for _, kv := range r.Options.Env {
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		env[parts[0]] = parts[1]
	}
	return env
}

func (r Runner) prepareWorkDir() (string, func(), error) {
	if r.Options.WorkDir != "" {
		return r.Options.WorkDir, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "agentctl-wasi-")
	if err != nil {
		return "", nil, err
	}
	return tmp, func() {
		errs.Ignore(os.RemoveAll(tmp), "cleanup wasi workdir")
	}, nil
}

func (r Runner) validateManifest() error {
	if r.Manifest.Distribution.Type != "wasi" {
		return fmt.Errorf("wasi runner requires distribution.type=wasi")
	}
	if netCap := strings.TrimSpace(r.Manifest.Capabilities.Network); netCap != "" && netCap != "none" {
		return fmt.Errorf("wasi runner only supports network capability \"none\" (got %q)", netCap)
	}
	return nil
}

func (r Runner) applyTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := r.Options.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return context.WithTimeout(ctx, timeout)
}

func (r Runner) resolveModulePath() (string, error) {
	if r.ModulePath != "" {
		return r.ModulePath, nil
	}
	if r.Manifest.Distribution.WASI != nil && r.Manifest.Distribution.WASI.Module != "" {
		return r.Manifest.Distribution.WASI.Module, nil
	}
	return "", fmt.Errorf("missing WASI module path")
}

func (r Runner) loadModule(path string) ([]byte, error) {
	moduleBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read wasm module: %w", err)
	}
	return moduleBytes, nil
}

func (r Runner) allocateBuffers() (*bytes.Buffer, *bytes.Buffer, error) {
	stdout, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		return nil, nil, fmt.Errorf("wasi runner: failed to get stdout buffer from pool")
	}
	stderr, ok := bufferPool.Get().(*bytes.Buffer)
	if !ok {
		bufferPool.Put(stdout)
		return nil, nil, fmt.Errorf("wasi runner: failed to get stderr buffer from pool")
	}
	stdout.Reset()
	stderr.Reset()
	return stdout, stderr, nil
}

func (r Runner) prepareRuntime(ctx context.Context) (wazero.Runtime, func(), error) {
	runtime := wazero.NewRuntime(ctx)
	cleanup := func() {
		errs.Ignore(runtime.Close(ctx), "close wazero runtime")
	}
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, runtime); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("instantiate wasi: %w", err)
	}
	return runtime, cleanup, nil
}

func (r Runner) compileModule(ctx context.Context, runtime wazero.Runtime, moduleBytes []byte) (wazero.CompiledModule, func(), error) {
	compiled, err := runtime.CompileModule(ctx, moduleBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("compile wasm: %w", err)
	}
	cleanup := func() {
		errs.Ignore(compiled.Close(ctx), "close compiled module")
	}
	return compiled, cleanup, nil
}

func (r Runner) buildModuleConfig(input []byte, workDir string, stdout, stderr *bytes.Buffer) wazero.ModuleConfig {
	moduleName := strings.ReplaceAll(r.Manifest.Metadata.Name, "/", "_")
	config := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr).
		WithStdin(bytes.NewReader(input)).
		WithName(moduleName)
	if workDir != "" {
		fsCfg := wazero.NewFSConfig().WithDirMount(workDir, "/work")
		config = config.WithFSConfig(fsCfg)
	}
	for k, v := range r.envVars() {
		config = config.WithEnv(k, v)
	}
	return config
}
