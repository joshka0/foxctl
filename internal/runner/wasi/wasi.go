// Package wasirunner executes WASI-distributed skills via wazero (pure Go).
package wasirunner

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

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
	if r.Manifest.Distribution.Type != "wasi" {
		return nil, nil, fmt.Errorf("wasi runner requires distribution.type=wasi")
	}
	if netCap := strings.TrimSpace(r.Manifest.Capabilities.Network); netCap != "" && netCap != "none" {
		return nil, nil, fmt.Errorf("wasi runner only supports network:\"none\" (got %q)", netCap)
	}

	// Apply timeout (default 5 minutes if not specified)
	timeout := r.Options.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	modulePath := r.ModulePath
	if modulePath == "" && r.Manifest.Distribution.WASI != nil {
		modulePath = r.Manifest.Distribution.WASI.Module
	}
	if modulePath == "" {
		return nil, nil, fmt.Errorf("missing WASI module path")
	}

	moduleBytes, err := os.ReadFile(modulePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read wasm module: %w", err)
	}

	workDir, cleanup, err := r.prepareWorkDir()
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	rt := wazero.NewRuntime(ctx)
	defer func() { _ = rt.Close(ctx) }()

	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
		return nil, nil, fmt.Errorf("instantiate wasi: %w", err)
	}

	compiled, err := rt.CompileModule(ctx, moduleBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("compile wasm: %w", err)
	}
	defer func() { _ = compiled.Close(ctx) }()

	moduleName := strings.ReplaceAll(r.Manifest.Metadata.Name, "/", "_")
	modConfig := wazero.NewModuleConfig().
		WithStdout(stdout).
		WithStderr(stderr).
		WithStdin(bytes.NewReader(input)).
		WithName(moduleName)

	if workDir != "" {
		fsCfg := wazero.NewFSConfig().WithDirMount(workDir, "/work")
		modConfig = modConfig.WithFSConfig(fsCfg)
	}

	env := r.envVars()
	for k, v := range env {
		modConfig = modConfig.WithEnv(k, v)
	}

	_, runErr := rt.InstantiateModule(ctx, compiled, modConfig)
	return stdout.Bytes(), stderr.Bytes(), runErr
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
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}
