package repl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
)

const (
	defaultSmolVMMachineName = "foxctl-rlm-longcot-glibc-offline"
	defaultSmolVMImage       = "python:3.12-slim"
	defaultSmolVMGuestWork   = "/workspace/foxctl-rlm-python"
)

// SmolVMPythonOptions configure a Python sandbox that executes inside a smolvm
// machine. Package installation happens inside the VM, never on the host.
type SmolVMPythonOptions struct {
	MachineName           string
	Image                 string
	GuestWorkDir          string
	SitePackagesDir       string
	Network               bool
	CreateOnInit          bool
	StartOnInit           bool
	StopOnClose           bool
	MaxOutputBytes        int
	AllowPackageInstall   bool
	AllowedPackages       []string
	PackageInstallTimeout time.Duration
	PackageAliases        map[string]string
	ForwardEnv            []string
}

// SmolVMPythonSession implements rlm.Sandbox by executing Python snippets in a
// persistent smolvm machine. It persists JSON-serializable globals between
// calls; non-serializable objects should be recreated by code snippets.
type SmolVMPythonSession struct {
	mu                    sync.Mutex
	machineName           string
	image                 string
	guestWorkDir          string
	sitePackagesDir       string
	network               bool
	createOnInit          bool
	startOnInit           bool
	stopOnClose           bool
	maxOutputBytes        int
	allowPackageInstall   bool
	allowedPackages       map[string]struct{}
	packageInstallTimeout time.Duration
	packageAliases        map[string]string
	forwardEnv            []string

	initialized       bool
	closed            bool
	turn              int
	installedPackages map[string]struct{}
}

var _ rlm.Sandbox = (*SmolVMPythonSession)(nil)

// NewSmolVMPythonSession creates an uninitialized smolvm-backed Python sandbox.
func NewSmolVMPythonSession(opts SmolVMPythonOptions) *SmolVMPythonSession {
	machineName := strings.TrimSpace(opts.MachineName)
	if machineName == "" {
		machineName = defaultSmolVMMachineName
	}
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = defaultSmolVMImage
	}
	guestWorkDir := strings.TrimSpace(opts.GuestWorkDir)
	if guestWorkDir == "" {
		guestWorkDir = defaultSmolVMGuestWork
	}
	sitePackagesDir := strings.TrimSpace(opts.SitePackagesDir)
	if sitePackagesDir == "" {
		sitePackagesDir = guestWorkDir + "/site-packages"
	}
	maxOutputBytes := opts.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	packageInstallTimeout := opts.PackageInstallTimeout
	if packageInstallTimeout <= 0 {
		packageInstallTimeout = 180 * time.Second
	}
	return &SmolVMPythonSession{
		machineName:           machineName,
		image:                 image,
		guestWorkDir:          guestWorkDir,
		sitePackagesDir:       sitePackagesDir,
		network:               opts.Network,
		createOnInit:          opts.CreateOnInit,
		startOnInit:           opts.StartOnInit,
		stopOnClose:           opts.StopOnClose,
		maxOutputBytes:        maxOutputBytes,
		allowPackageInstall:   opts.AllowPackageInstall,
		allowedPackages:       normalizeAllowedPythonPackages(opts.AllowedPackages),
		packageInstallTimeout: packageInstallTimeout,
		packageAliases:        normalizePythonPackageAliases(opts.PackageAliases),
		forwardEnv:            normalizeForwardEnv(opts.ForwardEnv),
		installedPackages:     map[string]struct{}{},
	}
}

// WorkDir returns the smolvm machine/workdir handle.
func (s *SmolVMPythonSession) WorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("smolvm:%s:%s", s.machineName, s.guestWorkDir)
}

// Init ensures the machine exists/runs and writes the initial JSON state.
func (s *SmolVMPythonSession) Init(ctx context.Context, state map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}
	if s.initialized {
		return errors.New("smolvm python session already initialized")
	}
	if err := validateInitState(state); err != nil {
		return err
	}
	if err := s.ensureMachineLocked(ctx); err != nil {
		return err
	}
	if _, err := s.machineExecLocked(ctx, 30*time.Second, "sh", "-lc", "mkdir -p "+shellQuote(s.guestWorkDir)+" "+shellQuote(s.sitePackagesDirLocked())); err != nil {
		return fmt.Errorf("prepare smolvm python workdir: %w", err)
	}
	initState := state
	if initState == nil {
		initState = map[string]any{}
	}
	if err := s.copyJSONToGuestLocked(ctx, initState, s.statePathLocked()); err != nil {
		return fmt.Errorf("write smolvm python state: %w", err)
	}
	s.initialized = true
	return nil
}

// InstallPackages installs allowlisted pip packages inside the smolvm machine.
func (s *SmolVMPythonSession) InstallPackages(ctx context.Context, packages []string) (rlm.ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return rlm.ExecResult{}, errSessionClosed
	}
	if !s.initialized {
		return rlm.ExecResult{}, errSessionNotInitialized
	}
	if !s.allowPackageInstall {
		return rlm.ExecResult{}, errors.New("smolvm python package installation is not enabled for this session")
	}
	return s.installPackagesLocked(ctx, packages)
}

// Execute runs Python code inside the smolvm machine.
func (s *SmolVMPythonSession) Execute(ctx context.Context, code string) (rlm.ExecResult, error) {
	start := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return rlm.ExecResult{}, errSessionClosed
	}
	if !s.initialized {
		return rlm.ExecResult{}, errSessionNotInitialized
	}
	s.turn++
	turnPath := fmt.Sprintf("%s/turn-%04d.py", s.guestWorkDir, s.turn)
	if err := s.copyTextToGuestLocked(ctx, code, turnPath); err != nil {
		return rlm.ExecResult{}, fmt.Errorf("write smolvm python turn: %w", err)
	}
	runnerPath := s.runnerPathLocked()
	if err := s.copyTextToGuestLocked(ctx, smolVMPythonRunnerScript, runnerPath); err != nil {
		return rlm.ExecResult{}, fmt.Errorf("write smolvm python runner: %w", err)
	}
	resp, err := s.executeTurnLocked(ctx, turnPath)
	if err != nil {
		return rlm.ExecResult{}, err
	}
	var autoInstallMetadata map[string]any
	if !resp.OK && s.allowPackageInstall {
		if module := missingPythonModuleFromException(resp.Exception); module != "" {
			if pkg := s.packageForMissingModuleLocked(module); pkg != "" {
				installResult, installErr := s.installPackagesLocked(ctx, []string{pkg})
				if installErr == nil {
					autoInstallMetadata = cloneExecMetadata(installResult.Metadata)
					autoInstallMetadata["output"] = installResult.Output
					autoInstallMetadata["duration_ms"] = installResult.DurationMS
					autoInstallMetadata["missing_module"] = module
					resp, err = s.executeTurnLocked(ctx, turnPath)
					if err != nil {
						return rlm.ExecResult{}, err
					}
				}
			}
		}
	}

	out := rlm.ExecResult{
		Output:     formatOutput(resp.Stdout, resp.Stderr, resp.Result, resp.Exception),
		DurationMS: time.Since(start).Milliseconds(),
		ExecutedAt: start,
		Metadata: map[string]any{
			"ok":           resp.OK,
			"stdout":       resp.Stdout,
			"stderr":       resp.Stderr,
			"result":       resp.Result,
			"machine_name": s.machineName,
			"guest_turn":   turnPath,
		},
	}
	if len(resp.Truncated) > 0 {
		out.Metadata["truncated"] = resp.Truncated
	}
	if resp.Exception != "" {
		out.Metadata["exception"] = resp.Exception
	}
	if resp.Error != "" {
		out.Metadata["error"] = resp.Error
	}
	if len(autoInstallMetadata) > 0 {
		out.Metadata["package_auto_install"] = autoInstallMetadata
	}
	return out, nil
}

// Snapshot returns JSON-serializable globals stored by the runner.
func (s *SmolVMPythonSession) Snapshot(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errSessionClosed
	}
	if !s.initialized {
		return nil, errSessionNotInitialized
	}
	output, err := s.machineExecLocked(ctx, 30*time.Second, "cat", s.statePathLocked())
	if err != nil {
		return nil, err
	}
	var state map[string]any
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		return nil, fmt.Errorf("decode smolvm python state: %w", err)
	}
	if state == nil {
		return map[string]any{}, nil
	}
	return state, nil
}

// Close optionally stops the smolvm machine. It never deletes or prunes it.
func (s *SmolVMPythonSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.stopOnClose && s.machineName != "" {
		_, err := s.runSmolVMLocked(ctx, 15*time.Second, "machine", "stop", "--name", s.machineName)
		return err
	}
	return nil
}

func (s *SmolVMPythonSession) ensureMachineLocked(ctx context.Context) error {
	if _, err := s.runSmolVMLocked(ctx, 20*time.Second, "machine", "status", "--name", s.machineName); err != nil {
		if !s.createOnInit {
			return fmt.Errorf("smolvm machine %q is unavailable: %w", s.machineName, err)
		}
		args := []string{"machine", "create", "--image", s.image, "--workdir", "/workspace"}
		if s.network {
			args = append(args, "--net")
		}
		args = append(args, s.machineName)
		if _, createErr := s.runSmolVMLocked(ctx, 10*time.Minute, args...); createErr != nil && !strings.Contains(createErr.Error(), "already exists") {
			return fmt.Errorf("create smolvm machine %q: %w", s.machineName, createErr)
		}
	}
	if s.startOnInit {
		if _, err := s.runSmolVMLocked(ctx, 5*time.Minute, "machine", "start", "--name", s.machineName); err != nil && !strings.Contains(err.Error(), "already running") {
			if path, ok := missingSmolMachineSidecarPath(err.Error()); ok {
				return fmt.Errorf("start smolvm machine %q: stale machine references missing .smolmachine sidecar %q; recreate the machine from a durable sidecar or select a valid machine: %w", s.machineName, path, err)
			}
			return fmt.Errorf("start smolvm machine %q: %w", s.machineName, err)
		}
	}
	return nil
}

func missingSmolMachineSidecarPath(text string) (string, bool) {
	if !strings.Contains(text, "source .smolmachine not found") {
		return "", false
	}
	const marker = "source .smolmachine not found:"
	idx := strings.Index(text, marker)
	if idx < 0 {
		return "", true
	}
	rest := strings.TrimSpace(text[idx+len(marker):])
	if rest == "" {
		return "", true
	}
	path := strings.Fields(rest)[0]
	return strings.TrimSpace(path), true
}

func (s *SmolVMPythonSession) installPackagesLocked(ctx context.Context, packages []string) (rlm.ExecResult, error) {
	start := time.Now().UTC()
	normalized, err := s.normalizeInstallPackagesLocked(packages)
	if err != nil {
		return rlm.ExecResult{}, err
	}
	if len(normalized) == 0 {
		return rlm.ExecResult{
			Output:     "no new packages requested",
			DurationMS: time.Since(start).Milliseconds(),
			ExecutedAt: start,
			Metadata: map[string]any{
				"ok":       true,
				"packages": []string{},
				"cached":   true,
			},
		}, nil
	}
	if !s.network {
		missing, checkOutput, checkErr := s.missingPreinstalledPackagesLocked(ctx, normalized)
		if checkErr != nil {
			return rlm.ExecResult{}, checkErr
		}
		if len(missing) > 0 {
			return rlm.ExecResult{
				Output:     strings.TrimSpace(checkOutput),
				DurationMS: time.Since(start).Milliseconds(),
				ExecutedAt: start,
				Metadata: map[string]any{
					"ok":               false,
					"packages":         normalized,
					"missing_packages": missing,
					"machine_name":     s.machineName,
					"network_enabled":  false,
				},
			}, fmt.Errorf("smolvm python packages %v are not preinstalled and network is disabled", missing)
		}
		for _, pkg := range normalized {
			s.installedPackages[strings.ToLower(pkg)] = struct{}{}
		}
		return rlm.ExecResult{
			Output:     strings.TrimSpace(checkOutput),
			DurationMS: time.Since(start).Milliseconds(),
			ExecutedAt: start,
			Metadata: map[string]any{
				"ok":              true,
				"packages":        normalized,
				"site_packages":   s.sitePackagesDirLocked(),
				"machine_name":    s.machineName,
				"preinstalled":    true,
				"network_enabled": false,
			},
		}, nil
	}
	timeout := s.packageInstallTimeout
	args := append([]string{"python3", "-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--target", s.sitePackagesDirLocked()}, normalized...)
	output, runErr := s.machineExecLocked(ctx, timeout, args...)
	if runErr != nil {
		return rlm.ExecResult{
			Output:     output,
			DurationMS: time.Since(start).Milliseconds(),
			ExecutedAt: start,
			Metadata: map[string]any{
				"ok":           false,
				"packages":     normalized,
				"error":        runErr.Error(),
				"machine_name": s.machineName,
			},
		}, fmt.Errorf("install smolvm python packages %v: %w: %s", normalized, runErr, strings.TrimSpace(output))
	}
	for _, pkg := range normalized {
		s.installedPackages[strings.ToLower(pkg)] = struct{}{}
	}
	return rlm.ExecResult{
		Output:     strings.TrimSpace(output),
		DurationMS: time.Since(start).Milliseconds(),
		ExecutedAt: start,
		Metadata: map[string]any{
			"ok":            true,
			"packages":      normalized,
			"site_packages": s.sitePackagesDirLocked(),
			"machine_name":  s.machineName,
		},
	}, nil
}

func (s *SmolVMPythonSession) executeTurnLocked(ctx context.Context, turnPath string) (pythonResponse, error) {
	output, err := s.machineExecLocked(
		ctx,
		0,
		"python3",
		s.runnerPathLocked(),
		s.guestWorkDir,
		turnPath,
		fmt.Sprintf("%d", s.maxOutputBytes),
	)
	if err != nil {
		return pythonResponse{}, fmt.Errorf("execute smolvm python turn: %w: %s", err, strings.TrimSpace(output))
	}
	var resp pythonResponse
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		return pythonResponse{}, fmt.Errorf("decode smolvm python response: %w: %s", err, strings.TrimSpace(output))
	}
	return resp, nil
}

func (s *SmolVMPythonSession) normalizeInstallPackagesLocked(packages []string) ([]string, error) {
	out := make([]string, 0, len(packages))
	seen := map[string]struct{}{}
	for _, pkg := range packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		if !validPythonPackageName(pkg) {
			return nil, fmt.Errorf("python package %q is not a simple package name", pkg)
		}
		if len(s.allowedPackages) > 0 {
			if _, ok := s.allowedPackages[strings.ToLower(pkg)]; !ok {
				return nil, fmt.Errorf("python package %q is not allowed by smolvm sandbox policy", pkg)
			}
		}
		key := strings.ToLower(pkg)
		if _, ok := s.installedPackages[key]; ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, pkg)
	}
	return out, nil
}

func (s *SmolVMPythonSession) packageForMissingModuleLocked(module string) string {
	module = strings.ToLower(strings.TrimSpace(module))
	if module == "" {
		return ""
	}
	if pkg := strings.TrimSpace(s.packageAliases[module]); pkg != "" {
		return pkg
	}
	if _, ok := s.allowedPackages[module]; ok {
		return module
	}
	return ""
}

func (s *SmolVMPythonSession) missingPreinstalledPackagesLocked(ctx context.Context, packages []string) ([]string, string, error) {
	checks := make(map[string]string, len(packages))
	for _, pkg := range packages {
		checks[pkg] = importModuleForPythonPackage(pkg)
	}
	raw, err := json.Marshal(checks)
	if err != nil {
		return nil, "", err
	}
	code := fmt.Sprintf(`import importlib.util, json
checks = json.loads(%q)
missing = []
present = []
for pkg, module in checks.items():
    if importlib.util.find_spec(module) is None:
        missing.append(pkg)
    else:
        present.append(pkg)
print(json.dumps({"missing": missing, "present": present}, sort_keys=True))
`, string(raw))
	output, err := s.machineExecLocked(ctx, 30*time.Second, "python3", "-c", code)
	if err != nil {
		return nil, output, err
	}
	var payload struct {
		Missing []string `json:"missing"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload); err != nil {
		return nil, output, fmt.Errorf("decode preinstalled package check: %w", err)
	}
	return payload.Missing, output, nil
}

func importModuleForPythonPackage(pkg string) string {
	switch strings.ToLower(strings.TrimSpace(pkg)) {
	case "python-chess":
		return "chess"
	case "rdkit-pypi":
		return "rdkit"
	default:
		return strings.ReplaceAll(strings.TrimSpace(pkg), "-", "_")
	}
}

func (s *SmolVMPythonSession) copyJSONToGuestLocked(ctx context.Context, value any, guestPath string) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.copyBytesToGuestLocked(ctx, b, guestPath)
}

func (s *SmolVMPythonSession) copyTextToGuestLocked(ctx context.Context, text string, guestPath string) error {
	return s.copyBytesToGuestLocked(ctx, []byte(text), guestPath)
}

func (s *SmolVMPythonSession) copyBytesToGuestLocked(ctx context.Context, data []byte, guestPath string) error {
	tmpDir, err := os.MkdirTemp("", "foxctl-smolvm-copy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	hostPath := filepath.Join(tmpDir, "payload")
	if err := os.WriteFile(hostPath, data, 0o600); err != nil {
		return err
	}
	if _, err := s.runSmolVMLocked(ctx, 30*time.Second, "machine", "cp", hostPath, s.machineName+":"+guestPath); err != nil {
		return err
	}
	return nil
}

func (s *SmolVMPythonSession) machineExecLocked(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cmdArgs := []string{"machine", "exec", "--name", s.machineName}
	for _, key := range s.forwardEnv {
		if val := os.Getenv(key); val != "" {
			cmdArgs = append(cmdArgs, "-e", key+"="+val)
		}
	}
	cmdArgs = append(cmdArgs, "-e", "PYTHONPATH="+s.sitePackagesDirLocked())
	if timeout > 0 {
		cmdArgs = append(cmdArgs, "--timeout", timeout.String())
	}
	cmdArgs = append(cmdArgs, "--")
	cmdArgs = append(cmdArgs, args...)
	runTimeout := time.Duration(0)
	if timeout > 0 {
		runTimeout = timeout + 10*time.Second
	}
	return s.runSmolVMLocked(ctx, runTimeout, cmdArgs...)
}

func (s *SmolVMPythonSession) runSmolVMLocked(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	lockFile, err := acquireSmolVMCLILock(runCtx)
	if err != nil {
		return "", err
	}
	defer releaseSmolVMCLILock(lockFile)
	cmd := exec.CommandContext(runCtx, "smolvm", args...)
	output, err := cmd.CombinedOutput()
	if runCtx.Err() != nil {
		return string(output), runCtx.Err()
	}
	if err != nil {
		return string(output), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func acquireSmolVMCLILock(ctx context.Context) (*os.File, error) {
	path := strings.TrimSpace(os.Getenv("FOXCTL_SMOLVM_CLI_LOCK"))
	if path == "" {
		path = filepath.Join(os.TempDir(), "foxctl-smolvm-cli.lock")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open smolvm cli lock: %w", err)
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, fmt.Errorf("acquire smolvm cli lock: %w", err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func releaseSmolVMCLILock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

func (s *SmolVMPythonSession) statePathLocked() string {
	return s.guestWorkDir + "/state.json"
}

func (s *SmolVMPythonSession) runnerPathLocked() string {
	return s.guestWorkDir + "/runner.py"
}

func (s *SmolVMPythonSession) sitePackagesDirLocked() string {
	return s.sitePackagesDir
}

func normalizeForwardEnv(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || !isShellEnvName(key) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func isShellEnvName(name string) bool {
	if name == "" {
		return false
	}
	for idx, r := range name {
		if idx == 0 {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

const smolVMPythonRunnerScript = `
import ast
import contextlib
import io
import json
import os
import sys
import traceback
import types

workdir = sys.argv[1]
code_path = sys.argv[2]
max_output = int(sys.argv[3])
state_path = os.path.join(workdir, "state.json")

def cap_text(value):
    text = "" if value is None else str(value)
    if max_output > 0 and len(text.encode("utf-8")) > max_output:
        return text.encode("utf-8")[:max_output].decode("utf-8", "ignore"), True
    return text, False

def jsonable(value):
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    if isinstance(value, list):
        return [jsonable(v) for v in value]
    if isinstance(value, tuple):
        return [jsonable(v) for v in value]
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if isinstance(k, str):
                out[k] = jsonable(v)
        return out
    raise TypeError()

def save_state(ns):
    out = {}
    for key, value in ns.items():
        if key.startswith("__"):
            continue
        if isinstance(value, types.ModuleType) or callable(value):
            continue
        try:
            out[key] = jsonable(value)
            json.dumps(out[key])
        except Exception:
            pass
    with open(state_path, "w", encoding="utf-8") as f:
        json.dump(out, f, ensure_ascii=False)

def emit_rlm_sentinel_globals(ns):
    for name in ("RLM_CHECK_JSON", "RLM_ANSWER_JSON"):
        if name not in ns:
            continue
        value = ns.get(name)
        try:
            if isinstance(value, str):
                text = value.strip()
            else:
                text = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
            if not text:
                continue
            if text.startswith(name + "="):
                print(text)
            else:
                print(name + "=" + text)
        except Exception:
            pass

try:
    with open(state_path, "r", encoding="utf-8") as f:
        state = json.load(f)
except Exception:
    state = {}

ns = {"__builtins__": __builtins__}
if isinstance(state, dict):
    ns.update(state)

with open(code_path, "r", encoding="utf-8") as f:
    code = f.read()

stdout_buf = io.StringIO()
stderr_buf = io.StringIO()
resp = {"ok": False, "stdout": "", "stderr": "", "result": "", "exception": "", "error": "", "state": None, "truncated": {}}
try:
    parsed = ast.parse(code, filename=code_path, mode="exec")
    result = None
    with contextlib.redirect_stdout(stdout_buf), contextlib.redirect_stderr(stderr_buf):
        if len(parsed.body) == 1 and isinstance(parsed.body[0], ast.Expr):
            expr = ast.Expression(parsed.body[0].value)
            ast.fix_missing_locations(expr)
            result = eval(compile(expr, code_path, "eval"), ns)
        else:
            exec(compile(parsed, code_path, "exec"), ns)
        emit_rlm_sentinel_globals(ns)
    save_state(ns)
    resp["ok"] = True
    resp["result"] = "" if result is None else repr(result)
except Exception:
    resp["exception"] = traceback.format_exc()

stdout, stdout_trunc = cap_text(stdout_buf.getvalue())
stderr, stderr_trunc = cap_text(stderr_buf.getvalue())
result, result_trunc = cap_text(resp.get("result", ""))
exception, exception_trunc = cap_text(resp.get("exception", ""))
resp["stdout"] = stdout
resp["stderr"] = stderr
resp["result"] = result
resp["exception"] = exception
if stdout_trunc:
    resp["truncated"]["stdout"] = True
if stderr_trunc:
    resp["truncated"]["stderr"] = True
if result_trunc:
    resp["truncated"]["result"] = True
if exception_trunc:
    resp["truncated"]["exception"] = True
print(json.dumps(resp, ensure_ascii=False))
`
