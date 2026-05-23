package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/protocol"
)

const (
	defaultWallTimeout         = 500 * time.Millisecond
	defaultCPUTimeout          = 200 * time.Millisecond
	defaultMaxOutputBytes      = 32 * 1024
	defaultMaxInputBytes       = 128 * 1024
	defaultMaxStderrBytes      = 16 * 1024
	defaultHandshakeTimeout    = 5 * time.Second
	maxAllowedWallTimeout      = 5 * time.Second
	maxAllowedCPUTimeout       = 2 * time.Second
	maxAllowedHandshakeTimeout = 15 * time.Second
	maxAllowedOutputBytes      = 128 * 1024
	maxAllowedInputBytes       = 1024 * 1024
	envPluginPath              = "FOXCTL_PLUGIN_PATH"
	envOpenAPIPluginPath       = "FOXCTL_OPENAPI_PLUGIN_PATH"
	pluginBinaryPrefix         = "foxctl-plugin-"
)

// Manager locates and executes auth/pagination plugins according to the Plugin Protocol v1 specification.
type Manager struct {
	searchPaths      []string
	env              []string
	workspace        string
	jobID            string
	limits           runtimeLimits
	handshakeTimeout time.Duration
	handshakes       map[string]*Handshake
	handshakesMu     sync.Mutex
	home             string
}

// RuntimeLimits controls the execution limits enforced for plugin processes.
type RuntimeLimits struct {
	WallTimeout    time.Duration
	CPUTimeout     time.Duration
	MaxOutputBytes int
	MaxInputBytes  int
	MaxStderrBytes int
}

type runtimeLimits struct {
	wall      time.Duration
	cpu       time.Duration
	maxOutput int
	maxInput  int
	maxStderr int
}

// Option customizes the manager.
type Option func(*Manager)

// WithSearchPaths overrides the plugin search paths used by the manager.
func WithSearchPaths(paths []string) Option {
	return func(m *Manager) {
		m.searchPaths = dedupeStrings(paths)
	}
}

// WithEnvironment overrides the base environment passed to plugins.
func WithEnvironment(env []string) Option {
	return func(m *Manager) {
		m.env = append([]string(nil), env...)
	}
}

// WithWorkspace records the active workspace path for env propagation.
func WithWorkspace(path string) Option {
	return func(m *Manager) {
		m.workspace = path
	}
}

// WithJobID records the active job identifier for env propagation.
func WithJobID(jobID string) Option {
	return func(m *Manager) {
		m.jobID = jobID
	}
}

// WithRuntimeLimits overrides the runtime limits enforced for plugin execution.
func WithRuntimeLimits(limits RuntimeLimits) Option {
	return func(m *Manager) {
		if limits.WallTimeout > 0 {
			if limits.WallTimeout > maxAllowedWallTimeout {
				limits.WallTimeout = maxAllowedWallTimeout
			}
			m.limits.wall = limits.WallTimeout
		}
		if limits.CPUTimeout > 0 {
			if limits.CPUTimeout > maxAllowedCPUTimeout {
				limits.CPUTimeout = maxAllowedCPUTimeout
			}
			m.limits.cpu = limits.CPUTimeout
		}
		if limits.MaxOutputBytes > 0 {
			if limits.MaxOutputBytes > maxAllowedOutputBytes {
				limits.MaxOutputBytes = maxAllowedOutputBytes
			}
			m.limits.maxOutput = limits.MaxOutputBytes
		}
		if limits.MaxInputBytes > 0 {
			if limits.MaxInputBytes > maxAllowedInputBytes {
				limits.MaxInputBytes = maxAllowedInputBytes
			}
			m.limits.maxInput = limits.MaxInputBytes
		}
		if limits.MaxStderrBytes > 0 {
			m.limits.maxStderr = limits.MaxStderrBytes
		}
	}
}

// WithHandshakeTimeout overrides the timeout applied to plugin handshakes.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(m *Manager) {
		if d <= 0 {
			return
		}
		if d > maxAllowedHandshakeTimeout {
			d = maxAllowedHandshakeTimeout
		}
		m.handshakeTimeout = d
	}
}

// NewManager constructs a Manager using configuration defaults and optional overrides.
func NewManager(cfg config.Config, opts ...Option) *Manager {
	m := &Manager{
		env:              os.Environ(),
		limits:           runtimeLimits{wall: defaultWallTimeout, cpu: defaultCPUTimeout, maxOutput: defaultMaxOutputBytes, maxInput: defaultMaxInputBytes, maxStderr: defaultMaxStderrBytes},
		handshakeTimeout: defaultHandshakeTimeout,
		handshakes:       make(map[string]*Handshake),
		home:             cfg.Home,
	}
	m.searchPaths = m.collectSearchPaths(cfg)
	for _, opt := range opts {
		opt(m)
	}
	if len(m.searchPaths) == 0 {
		m.searchPaths = []string{filepath.Join(cfg.Home, "plugins")}
	}
	return m
}

// InvokeAuth executes the specified auth plugin and returns the resulting headers/body modifications.
func (m *Manager) InvokeAuth(ctx context.Context, ref string, payload AuthRequestPayload) (AuthResult, error) {
	info, err := m.prepare(ctx, ref, CommandAuth)
	if err != nil {
		return AuthResult{}, err
	}
	req := AuthRequestPayload{
		Request: payload.Request,
		Context: payload.Context,
		Limits:  m.limitsPayload(),
	}
	resp, err := m.invoke(ctx, info, CommandAuth, req)
	if err != nil {
		return AuthResult{}, err
	}
	var result AuthResult
	if err := DecodeData(resp.Data, &result); err != nil {
		return AuthResult{}, newInvocationError(protocol.ErrorCodeEEnvelope, "decode auth plugin response", err, m.errorDetails(info, nil, nil))
	}
	return result, nil
}

// InvokePagination executes the specified pagination plugin and returns the pagination directives.
func (m *Manager) InvokePagination(ctx context.Context, ref string, payload PaginationRequestPayload) (PaginationResult, error) {
	info, err := m.prepare(ctx, ref, CommandPagination)
	if err != nil {
		return PaginationResult{}, err
	}
	req := PaginationRequestPayload{
		LastResponse:      payload.LastResponse,
		RequestedMaxItems: payload.RequestedMaxItems,
		ItemsFetchedSoFar: payload.ItemsFetchedSoFar,
		Context:           payload.Context,
		Limits:            m.limitsPayload(),
	}
	resp, err := m.invoke(ctx, info, CommandPagination, req)
	if err != nil {
		return PaginationResult{}, err
	}
	var result PaginationResult
	if err := DecodeData(resp.Data, &result); err != nil {
		return PaginationResult{}, newInvocationError(protocol.ErrorCodeEEnvelope, "decode pagination plugin response", err, m.errorDetails(info, nil, nil))
	}
	return result, nil
}

func (m *Manager) prepare(ctx context.Context, ref, command string) (*pluginInfo, error) {
	info, err := m.resolve(ref)
	if err != nil {
		return nil, err
	}
	hs, err := m.ensureHandshake(ctx, info)
	if err != nil {
		return nil, err
	}
	if hs != nil {
		info.handshake = hs
		if hs.Name != "" {
			info.Name = hs.Name
		}
		if len(hs.Commands) > 0 && !containsString(hs.Commands, command) {
			return nil, newInvocationError(protocol.ErrorCodeEPolicy, fmt.Sprintf("plugin %s does not support %s", info.Name, command), nil, m.errorDetails(info, nil, nil))
		}
		if len(hs.Protocols) > 0 && !containsString(hs.Protocols, "core/v1") {
			return nil, newInvocationError(protocol.ErrorCodeEPolicy, fmt.Sprintf("plugin %s is not compatible with core/v1", info.Name), nil, m.errorDetails(info, nil, nil))
		}
	}
	return info, nil
}

func (m *Manager) ensureHandshake(ctx context.Context, info *pluginInfo) (*Handshake, error) {
	// First check: optimistic read to avoid lock contention in common case
	m.handshakesMu.Lock()
	cached, ok := m.handshakes[info.Path]
	if ok {
		m.handshakesMu.Unlock()
		return cached, nil
	}
	m.handshakesMu.Unlock()

	// Execute handshake without holding lock (expensive operation)
	hs, err := m.runHandshake(ctx, info)
	if err != nil {
		return nil, err
	}

	// Second check: prevent race condition when multiple goroutines
	// run handshake concurrently for the same plugin
	m.handshakesMu.Lock()
	defer m.handshakesMu.Unlock()

	// Check again - another goroutine may have completed while we were running handshake
	if cached, ok := m.handshakes[info.Path]; ok {
		// Use the cached version to ensure consistency
		return cached, nil
	}

	// Store our result
	m.handshakes[info.Path] = hs
	return hs, nil
}

func (m *Manager) runHandshake(ctx context.Context, info *pluginInfo) (*Handshake, error) {
	ctx, cancel := context.WithTimeout(ctx, m.handshakeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, info.Path, "--handshake")
	stdoutBuf := newLimitedBuffer(m.limits.maxOutput)
	stderrBuf := newLimitedBuffer(m.limits.maxStderr)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	cmd.Env = m.buildEnv(info, "handshake")

	if err := cmd.Run(); err != nil {
		details := m.errorDetails(info, stderrBuf, stdoutBuf)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, newInvocationError(protocol.ErrorCodeETimeout, fmt.Sprintf("handshake timed out for plugin %s", info.Path), err, details)
		}
		return nil, newInvocationError(protocol.ErrorCodeERuntime, fmt.Sprintf("handshake failed for plugin %s", info.Path), err, details)
	}
	if stdoutBuf.Truncated() {
		return nil, newInvocationError(protocol.ErrorCodeEOutputTooLarge, "handshake output exceeded limit", stdoutBuf.Err(), m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	data := bytes.TrimSpace(stdoutBuf.Bytes())
	if len(data) == 0 {
		return nil, newInvocationError(protocol.ErrorCodeEEnvelope, "handshake returned empty output", nil, m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	var hs Handshake
	if err := json.Unmarshal(data, &hs); err != nil {
		return nil, newInvocationError(protocol.ErrorCodeEEnvelope, "handshake decode", err, m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	if hs.Name == "" {
		hs.Name = info.Name
	}
	return &hs, nil
}

func (m *Manager) invoke(ctx context.Context, info *pluginInfo, command string, payload any) (envelope.Envelope, error) {
	req := envelope.OK(command, payload)
	body, err := json.Marshal(req)
	if err != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEEnvelope, "encode plugin request", err, m.errorDetails(info, nil, nil))
	}
	if len(body)+1 > m.limits.maxInput { // +1 for trailing newline
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEOutputTooLarge, fmt.Sprintf("plugin input exceeds limit (%d bytes)", m.limits.maxInput), nil, m.errorDetails(info, nil, nil))
	}
	body = append(body, '\n')

	ctx, cancel := context.WithTimeout(ctx, m.limits.wall)
	defer cancel()

	cmd := exec.CommandContext(ctx, info.Path)
	stdoutBuf := newLimitedBuffer(m.limits.maxOutput)
	stderrBuf := newLimitedBuffer(m.limits.maxStderr)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	cmd.Env = m.buildEnv(info, command)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeERuntime, "plugin stdin pipe", err, m.errorDetails(info, stderrBuf, stdoutBuf))
	}

	if err := cmd.Start(); err != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeERuntime, "start plugin", err, m.errorDetails(info, stderrBuf, stdoutBuf))
	}

	writeErr := writeAndClose(stdin, body)
	waitErr := cmd.Wait()

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeETimeout, fmt.Sprintf("plugin %s timed out", info.Name), ctx.Err(), m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	if stdoutBuf.Truncated() {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEOutputTooLarge, "plugin output exceeded limit", stdoutBuf.Err(), m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	if writeErr != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeERuntime, "write plugin input", writeErr, m.errorDetails(info, stderrBuf, stdoutBuf))
	}

	outBytes := bytes.TrimSpace(stdoutBuf.Bytes())
	if len(outBytes) == 0 {
		details := m.errorDetails(info, stderrBuf, stdoutBuf)
		if waitErr != nil {
			if code, ok := exitCode(waitErr); ok {
				details["exit_code"] = code
			}
		}
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeERuntime, "plugin produced no output", waitErr, details)
	}

	var resp envelope.Envelope
	if err := json.Unmarshal(outBytes, &resp); err != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEEnvelope, "decode plugin response", err, m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	if err := envelope.Validate(resp); err != nil {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEEnvelope, "invalid plugin envelope", err, m.errorDetails(info, stderrBuf, stdoutBuf))
	}
	if resp.Command != command {
		return envelope.Envelope{}, newInvocationError(protocol.ErrorCodeEEnvelope, fmt.Sprintf("plugin returned unexpected command %s", resp.Command), nil, m.errorDetails(info, stderrBuf, stdoutBuf))
	}

	if resp.Status == envelope.StatusOK {
		return resp, nil
	}

	details := m.errorDetails(info, stderrBuf, stdoutBuf)
	if resp.Data != nil {
		details["plugin_data"] = resp.Data
	}
	code := protocol.ErrorCode(resp.Error.Code)
	if code == "" {
		code = protocol.ErrorCodeERuntime
	}
	return envelope.Envelope{}, &InvocationError{
		Code:    code,
		Message: resp.Error.Message,
		Details: details,
	}
}

func (m *Manager) limitsPayload() Limits {
	return Limits{
		Wall:        int(m.limits.wall / time.Millisecond),
		CPU:         int(m.limits.cpu / time.Millisecond),
		MaxOutputKB: bytesToKB(m.limits.maxOutput),
		MaxInputKB:  bytesToKB(m.limits.maxInput),
	}
}

func (m *Manager) collectSearchPaths(cfg config.Config) []string {
	var paths []string
	paths = append(paths, resolvePathList(m.home, os.Getenv(envPluginPath))...)
	paths = append(paths, resolvePathList(m.home, os.Getenv(envOpenAPIPluginPath))...)
	paths = append(paths, cfg.OpenAPI.PluginPath...)
	defaultPath := filepath.Join(cfg.Home, "plugins")
	paths = append(paths, defaultPath)
	return dedupeStrings(paths)
}

func (m *Manager) resolve(ref string) (*pluginInfo, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return nil, newInvocationError(protocol.ErrorCodeEARG, "plugin reference is empty", nil, nil)
	}

	var (
		name string
		path string
		err  error
	)

	switch {
	case strings.HasPrefix(trimmed, "plugin:"):
		target := strings.TrimPrefix(trimmed, "plugin:")
		if target == "" {
			return nil, newInvocationError(protocol.ErrorCodeEARG, "plugin reference missing name", nil, nil)
		}
		if isPathReference(target) {
			path = target
			name = derivePluginName(target)
		} else {
			name = target
			path, err = m.findInSearchPaths(target)
		}
	case isPathReference(trimmed):
		path = trimmed
		name = derivePluginName(trimmed)
	default:
		name = trimmed
		path, err = m.findInSearchPaths(trimmed)
	}

	if err != nil {
		return nil, err
	}
	resolvedPath := resolvePath(m.home, path)
	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return nil, newInvocationError(protocol.ErrorCodeEIO, fmt.Sprintf("resolve plugin path %s", path), err, nil)
	}
	absPath = filepath.Clean(absPath)
	if !isExecutableFile(absPath) {
		details := map[string]any{"plugin_path": absPath}
		return nil, newInvocationError(protocol.ErrorCodeENotFound, fmt.Sprintf("plugin binary %s not executable", absPath), os.ErrPermission, details)
	}
	info := &pluginInfo{
		Name: name,
		Path: absPath,
	}
	if info.Name == "" {
		info.Name = derivePluginName(info.Path)
	}
	return info, nil
}

func (m *Manager) findInSearchPaths(name string) (string, error) {
	candidates := candidateFilenames(name)
	for _, dir := range m.searchPaths {
		if dir == "" {
			continue
		}
		for _, cand := range candidates {
			full := filepath.Join(dir, cand)
			if isExecutableFile(full) {
				return full, nil
			}
		}
	}
	details := map[string]any{
		"plugin":       name,
		"search_paths": append([]string(nil), m.searchPaths...),
	}
	return "", newInvocationError(protocol.ErrorCodeENotFound, fmt.Sprintf("plugin %s not found", name), os.ErrNotExist, details)
}

func (m *Manager) buildEnv(info *pluginInfo, command string) []string {
	env := append([]string(nil), m.env...)
	pluginName := info.Name
	if pluginName == "" && info.handshake != nil {
		pluginName = info.handshake.Name
	}
	if pluginName != "" {
		env = append(env, fmt.Sprintf("FOXCTL_PLUGIN_NAME=%s", pluginName))
	}
	env = append(env, fmt.Sprintf("FOXCTL_PLUGIN_COMMAND=%s", command))
	if info.handshake != nil && info.handshake.Version != "" {
		env = append(env, fmt.Sprintf("FOXCTL_PLUGIN_VERSION=%s", info.handshake.Version))
	}
	if m.workspace != "" {
		env = append(env, fmt.Sprintf("FOXCTL_WORKSPACE=%s", m.workspace))
	}
	if m.jobID != "" {
		env = append(env, fmt.Sprintf("FOXCTL_JOB_ID=%s", m.jobID))
	}
	return env
}

func (m *Manager) errorDetails(info *pluginInfo, stderrBuf, stdoutBuf *limitedBuffer) map[string]any {
	details := map[string]any{
		"plugin_path": info.Path,
	}
	if info.handshake != nil && info.handshake.Version != "" {
		details["plugin_version"] = info.handshake.Version
	}
	name := info.Name
	if name == "" && info.handshake != nil {
		name = info.handshake.Name
	}
	if name != "" {
		details["plugin_name"] = name
	}
	if stderrBuf != nil && stderrBuf.Len() > 0 {
		details["plugin_stderr"] = stderrBuf.String()
	}
	if stdoutBuf != nil && stdoutBuf.Truncated() {
		details["stdout_truncated"] = true
	}
	if stderrBuf != nil && stderrBuf.Truncated() {
		details["stderr_truncated"] = true
	}
	return details
}

func resolvePathList(base, raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	resolved := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		resolved = append(resolved, resolvePath(base, trimmed))
	}
	return resolved
}

func resolvePath(base, raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			trimmed := strings.TrimPrefix(raw, "~")
			trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
			return filepath.Join(home, trimmed)
		}
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw)
	}
	return filepath.Join(base, raw)
}

func isPathReference(ref string) bool {
	return filepath.IsAbs(ref) || strings.ContainsRune(ref, os.PathSeparator)
}

func derivePluginName(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.TrimPrefix(base, pluginBinaryPrefix)
}

func candidateFilenames(name string) []string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	base := strings.TrimPrefix(trimmed, pluginBinaryPrefix)
	candidates := []string{}
	if trimmed != "" {
		candidates = append(candidates, trimmed)
	}
	canonical := pluginBinaryPrefix + base
	if canonical != trimmed {
		candidates = append(candidates, canonical)
	}
	if base != trimmed {
		candidates = append(candidates, base)
	}
	candidates = dedupeStrings(candidates)
	if runtime.GOOS == "windows" {
		extended := make([]string, 0, len(candidates)*2)
		seen := make(map[string]struct{}, len(candidates)*2)
		for _, cand := range candidates {
			if _, ok := seen[cand]; !ok {
				extended = append(extended, cand)
				seen[cand] = struct{}{}
			}
			if filepath.Ext(cand) == "" {
				exe := cand + ".exe"
				if _, ok := seen[exe]; !ok {
					extended = append(extended, exe)
					seen[exe] = struct{}{}
				}
			}
		}
		candidates = extended
	}
	return candidates
}

func dedupeStrings(in []string) []string {
	result := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if strings.EqualFold(item, target) {
			return true
		}
	}
	return false
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".exe" || ext == ".bat" || ext == ".cmd"
	}
	return info.Mode()&0o111 != 0
}

func writeAndClose(w io.WriteCloser, data []byte) error {
	defer w.Close()
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

func bytesToKB(v int) int {
	if v <= 0 {
		return 0
	}
	kb := v / 1024
	if v%1024 != 0 {
		kb++
	}
	return kb
}

func exitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ProcessState != nil {
			return exitErr.ExitCode(), true
		}
	}
	return 0, false
}

type pluginInfo struct {
	Name      string
	Path      string
	handshake *Handshake
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
	err       error
}

func newLimitedBuffer(limit int) *limitedBuffer {
	return &limitedBuffer{limit: limit}
}

var errLimitExceeded = errors.New("output limit exceeded")

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		b.err = errLimitExceeded
		return len(p), errLimitExceeded
	}
	if len(p) > remaining {
		if _, err := b.buf.Write(p[:remaining]); err != nil {
			return 0, err
		}
		b.truncated = true
		b.err = errLimitExceeded
		return len(p), errLimitExceeded
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitedBuffer) String() string {
	return b.buf.String()
}

func (b *limitedBuffer) Len() int {
	return b.buf.Len()
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}

func (b *limitedBuffer) Err() error {
	return b.err
}
