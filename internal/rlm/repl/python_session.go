package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/rlm"
)

const (
	defaultMaxOutputBytes = 64 * 1024
	stderrTailBytes       = 8 * 1024
)

var (
	errSessionNotInitialized = errors.New("python session is not initialized")
	errSessionClosed         = errors.New("python session is closed")
	errSessionBroken         = errors.New("python session is broken")
)

// Options configure PythonSession behavior.
type Options struct {
	// PythonPath optionally overrides python binary discovery.
	PythonPath string
	// MaxOutputBytes caps captured stdout/stderr/result text per execution call.
	MaxOutputBytes int
	// WorkDir optionally pins the session working directory instead of creating
	// a temp directory.
	WorkDir string
	// PreserveWorkDir keeps the session working directory after Close or process
	// termination so failed runs can be inspected.
	PreserveWorkDir bool
}

// PythonSession is a persistent Python subprocess that implements rlm.Sandbox.
type PythonSession struct {
	mu                sync.Mutex
	pythonPath        string
	maxOutputBytes    int
	configuredWorkDir string
	preserveWorkDir   bool

	cmd       *exec.Cmd
	stdin     io.WriteCloser
	encoder   *json.Encoder
	responses chan pythonResponse
	waitErrCh chan error
	waitErr   error
	stderrLog *tailBuffer

	workDir string
	execSeq int
	closed  bool
	broken  bool
}

var _ rlm.Sandbox = (*PythonSession)(nil)

// NewPythonSession creates a new uninitialized persistent Python session.
func NewPythonSession(opts Options) *PythonSession {
	maxOutputBytes := opts.MaxOutputBytes
	if maxOutputBytes <= 0 {
		maxOutputBytes = defaultMaxOutputBytes
	}
	return &PythonSession{
		pythonPath:        strings.TrimSpace(opts.PythonPath),
		maxOutputBytes:    maxOutputBytes,
		configuredWorkDir: strings.TrimSpace(opts.WorkDir),
		preserveWorkDir:   opts.PreserveWorkDir,
		stderrLog:         newTailBuffer(stderrTailBytes),
	}
}

// WorkDir returns the active session working directory, if initialized.
func (s *PythonSession) WorkDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.workDir
}

// FindPython resolves an available python binary (`python3` first, then `python`).
func FindPython() (string, error) {
	for _, candidate := range []string{"python3", "python"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}
	}
	return "", errors.New("python binary not found (checked python3, python)")
}

// Init starts a Python process and binds initial state into the Python globals.
func (s *PythonSession) Init(ctx context.Context, state map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errSessionClosed
	}
	if s.cmd != nil {
		return errors.New("python session already initialized")
	}
	if err := validateInitState(state); err != nil {
		return err
	}

	pythonPath := s.pythonPath
	if pythonPath == "" {
		var err error
		pythonPath, err = FindPython()
		if err != nil {
			return err
		}
		s.pythonPath = pythonPath
	}

	workDir := s.configuredWorkDir
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "foxctl-rlm-python-*")
		if err != nil {
			return fmt.Errorf("create temp work dir: %w", err)
		}
	} else if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("create configured work dir: %w", err)
	}

	cmd := exec.CommandContext(context.Background(), pythonPath, "-u", "-c", pythonBridgeScript)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "FOXCTL_RLM_PARENT_PID="+strconv.Itoa(os.Getpid()))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("create stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("create stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = os.RemoveAll(workDir)
		return fmt.Errorf("start python process: %w", err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.encoder = json.NewEncoder(stdin)
	respCh := make(chan pythonResponse, 1)
	waitErrCh := make(chan error, 1)
	s.responses = respCh
	s.waitErrCh = waitErrCh
	s.waitErr = nil
	s.workDir = workDir
	s.closed = false
	s.broken = false
	s.stderrLog.Reset()

	go s.readResponses(stdout, respCh)
	go func() {
		_, _ = io.Copy(s.stderrLog, stderr)
	}()
	go s.waitProcess(cmd, waitErrCh)

	initState := state
	if initState == nil {
		initState = map[string]any{}
	}

	resp, err := s.sendAndReceiveLocked(ctx, pythonRequest{
		Op:    "init",
		State: initState,
	})
	if err != nil {
		s.markBrokenLocked()
		s.terminateLocked()
		return err
	}
	if !resp.OK {
		s.markBrokenLocked()
		s.terminateLocked()
		return fmt.Errorf("python init failed: %s", firstNonEmpty(resp.Error, resp.Exception, strings.TrimSpace(s.stderrLog.String())))
	}
	return nil
}

// Execute runs a code snippet in the persistent Python session.
func (s *PythonSession) Execute(ctx context.Context, code string) (rlm.ExecResult, error) {
	start := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return rlm.ExecResult{}, errSessionClosed
	}
	if s.cmd == nil {
		return rlm.ExecResult{}, errSessionNotInitialized
	}
	if s.broken {
		return rlm.ExecResult{}, errSessionBroken
	}

	turnSeq := s.nextExecSeqLocked()
	if turnSeq > 0 {
		s.writeTurnFileLocked(turnSeq, ".py", code)
	}
	resp, err := s.sendAndReceiveLocked(ctx, pythonRequest{
		Op:             "exec",
		Code:           code,
		MaxOutputBytes: s.maxOutputBytes,
	})
	if err != nil {
		s.markBrokenLocked()
		return rlm.ExecResult{}, err
	}

	execResult := rlm.ExecResult{
		Output:     formatOutput(resp.Stdout, resp.Stderr, resp.Result, resp.Exception),
		DurationMS: time.Since(start).Milliseconds(),
		ExecutedAt: start,
		Metadata: map[string]any{
			"ok":     resp.OK,
			"stdout": resp.Stdout,
			"stderr": resp.Stderr,
			"result": resp.Result,
		},
	}
	if len(resp.Truncated) > 0 {
		execResult.Metadata["truncated"] = resp.Truncated
	}
	if resp.Exception != "" {
		execResult.Metadata["exception"] = resp.Exception
	}
	if resp.Error != "" {
		execResult.Metadata["error"] = resp.Error
	}
	if turnSeq > 0 {
		s.writeTurnFileLocked(turnSeq, ".output.txt", execResult.Output)
		execResult.Metadata["turn_path"] = s.turnFilePathLocked(turnSeq, ".py")
		execResult.Metadata["turn_output_path"] = s.turnFilePathLocked(turnSeq, ".output.txt")
	}

	return execResult, nil
}

// Snapshot returns a JSON-serializable subset of Python globals.
func (s *PythonSession) Snapshot(ctx context.Context) (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errSessionClosed
	}
	if s.cmd == nil {
		return nil, errSessionNotInitialized
	}
	if s.broken {
		return nil, errSessionBroken
	}

	resp, err := s.sendAndReceiveLocked(ctx, pythonRequest{Op: "snapshot"})
	if err != nil {
		s.markBrokenLocked()
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("python snapshot failed: %s", firstNonEmpty(resp.Error, resp.Exception))
	}
	if resp.State == nil {
		return map[string]any{}, nil
	}
	return resp.State, nil
}

// Close shuts down the Python process and removes the temp working directory.
func (s *PythonSession) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.cmd == nil {
		if s.workDir != "" {
			s.removeWorkDirLocked()
			s.workDir = ""
		}
		return nil
	}

	_, _ = s.sendAndReceiveLocked(ctx, pythonRequest{Op: "close"})
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	waitErr := s.waitForExitLocked(ctx)
	if waitErr != nil {
		s.terminateLocked()
		if s.workDir != "" {
			s.removeWorkDirLocked()
			s.workDir = ""
		}
		return waitErr
	}

	if s.waitErr != nil && !errors.Is(s.waitErr, os.ErrProcessDone) {
		// Normal close should produce nil wait error.
		if s.workDir != "" {
			s.removeWorkDirLocked()
			s.workDir = ""
		}
		s.resetProcessLocked()
		return fmt.Errorf("python process exited with error: %w", s.waitErr)
	}

	if s.workDir != "" {
		s.removeWorkDirLocked()
		s.workDir = ""
	}
	s.resetProcessLocked()
	return nil
}

func (s *PythonSession) sendAndReceiveLocked(ctx context.Context, req pythonRequest) (pythonResponse, error) {
	if s.encoder == nil {
		return pythonResponse{}, errSessionNotInitialized
	}
	if err := s.encoder.Encode(req); err != nil {
		return pythonResponse{}, fmt.Errorf("encode python request: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			s.terminateLocked()
			return pythonResponse{}, fmt.Errorf("python request timed out: %w", ctx.Err())
		case resp, ok := <-s.responses:
			if !ok {
				return pythonResponse{}, fmt.Errorf("python process terminated: %s", firstNonEmpty(strings.TrimSpace(s.stderrLog.String()), "no response received"))
			}
			return resp, nil
		}
	}
}

func (s *PythonSession) waitForExitLocked(ctx context.Context) error {
	if s.waitErrCh == nil {
		return nil
	}
	select {
	case err, ok := <-s.waitErrCh:
		if ok {
			s.waitErr = err
		}
		return err
	case <-ctx.Done():
		return fmt.Errorf("wait for python process exit: %w", ctx.Err())
	}
}

func (s *PythonSession) terminateLocked() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	if s.waitErrCh != nil {
		select {
		case err, ok := <-s.waitErrCh:
			if ok {
				s.waitErr = err
			}
		case <-time.After(2 * time.Second):
		}
	}
	s.broken = true
	s.resetProcessLocked()
	if s.workDir != "" {
		s.removeWorkDirLocked()
		s.workDir = ""
	}
}

func (s *PythonSession) removeWorkDirLocked() {
	if s.preserveWorkDir || strings.TrimSpace(s.configuredWorkDir) != "" {
		return
	}
	_ = os.RemoveAll(s.workDir)
}

func (s *PythonSession) nextExecSeqLocked() int {
	if s.workDir == "" || !s.preserveWorkDir {
		return 0
	}
	s.execSeq++
	return s.execSeq
}

func (s *PythonSession) writeTurnFileLocked(seq int, suffix string, content string) {
	path := s.turnFilePathLocked(seq, suffix)
	if path == "" {
		return
	}
	_ = os.WriteFile(path, []byte(content), 0o600)
}

func (s *PythonSession) turnFilePathLocked(seq int, suffix string) string {
	if seq <= 0 || s.workDir == "" {
		return ""
	}
	return fmt.Sprintf("%s/turn-%04d%s", s.workDir, seq, suffix)
}

func (s *PythonSession) resetProcessLocked() {
	s.cmd = nil
	s.stdin = nil
	s.encoder = nil
	s.responses = nil
	s.waitErrCh = nil
}

func (s *PythonSession) markBrokenLocked() {
	s.broken = true
}

func (s *PythonSession) readResponses(stdout io.Reader, responses chan pythonResponse) {
	decoder := json.NewDecoder(bufio.NewReader(stdout))
	for {
		var resp pythonResponse
		if err := decoder.Decode(&resp); err != nil {
			close(responses)
			return
		}
		responses <- resp
	}
}

func (s *PythonSession) waitProcess(cmd *exec.Cmd, waitErrCh chan error) {
	err := cmd.Wait()
	waitErrCh <- err
	close(waitErrCh)
}

func validateInitState(state map[string]any) error {
	for key := range state {
		if strings.TrimSpace(key) == "" {
			return errors.New("init state contains empty key")
		}
		if !isPythonIdentifier(key) {
			return fmt.Errorf("init state key %q is not a valid Python identifier", key)
		}
	}
	return nil
}

func isPythonIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for idx, r := range name {
		if idx == 0 {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && r != '_' {
				return false
			}
			continue
		}
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func formatOutput(stdout, stderr, result, exception string) string {
	parts := make([]string, 0, 4)
	if stdout != "" {
		parts = append(parts, "stdout:\n"+stdout)
	}
	if stderr != "" {
		parts = append(parts, "stderr:\n"+stderr)
	}
	if result != "" {
		parts = append(parts, "result:\n"+result)
	}
	if exception != "" {
		parts = append(parts, "exception:\n"+exception)
	}
	return strings.Join(parts, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type pythonRequest struct {
	Op             string         `json:"op"`
	State          map[string]any `json:"state,omitempty"`
	Code           string         `json:"code,omitempty"`
	MaxOutputBytes int            `json:"max_output_bytes,omitempty"`
}

type pythonResponse struct {
	OK        bool            `json:"ok"`
	Error     string          `json:"error,omitempty"`
	Stdout    string          `json:"stdout,omitempty"`
	Stderr    string          `json:"stderr,omitempty"`
	Result    string          `json:"result,omitempty"`
	Exception string          `json:"exception,omitempty"`
	Truncated map[string]bool `json:"truncated,omitempty"`
	State     map[string]any  `json:"state,omitempty"`
}

type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func newTailBuffer(max int) *tailBuffer {
	return &tailBuffer{max: max}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.max <= 0 {
		return len(p), nil
	}
	combined := append(b.buf, p...)
	if len(combined) > b.max {
		combined = combined[len(combined)-b.max:]
	}
	b.buf = combined
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

func (b *tailBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = nil
}

const pythonBridgeScript = `
import contextlib
import io
import json
import os
import sys
import threading
import time
import traceback

globals_ns = {}

parent_pid_raw = os.environ.get("FOXCTL_RLM_PARENT_PID", "").strip()
try:
    parent_pid = int(parent_pid_raw) if parent_pid_raw else 0
except Exception:
    parent_pid = 0

def monitor_parent():
    if parent_pid <= 0:
        return
    while True:
        time.sleep(1.0)
        try:
            if os.getppid() != parent_pid:
                os._exit(70)
        except Exception:
            os._exit(70)

threading.Thread(target=monitor_parent, name="foxctl-parent-watchdog", daemon=True).start()

class LimitedTextBuffer(io.TextIOBase):
    def __init__(self, max_bytes):
        self.max_bytes = max_bytes
        self.parts = []
        self.size = 0
        self.truncated = False

    def write(self, text):
        if text is None:
            return 0
        text = str(text)
        encoded = text.encode("utf-8")

        remaining = self.max_bytes - self.size
        if remaining <= 0:
            self.truncated = True
            return len(text)

        if len(encoded) <= remaining:
            self.parts.append(text)
            self.size += len(encoded)
            return len(text)

        piece = encoded[:remaining].decode("utf-8", errors="ignore")
        if piece:
            self.parts.append(piece)
            self.size += len(piece.encode("utf-8"))
        self.truncated = True
        return len(text)

    def getvalue(self):
        return "".join(self.parts)

def truncate_text(text, max_bytes):
    if text is None:
        return "", False
    encoded = text.encode("utf-8")
    if len(encoded) <= max_bytes:
        return text, False
    return encoded[:max_bytes].decode("utf-8", errors="ignore"), True

def send(response):
    print(json.dumps(response, ensure_ascii=False), flush=True)

for raw in sys.stdin:
    raw = raw.strip()
    if not raw:
        continue

    try:
        request = json.loads(raw)
    except Exception as ex:
        send({"ok": False, "error": f"invalid request JSON: {ex}"})
        continue

    op = request.get("op", "")
    if op == "init":
        state = request.get("state", {})
        if not isinstance(state, dict):
            send({"ok": False, "error": "state must be an object"})
            continue
        globals_ns.update(state)
        send({"ok": True})
        continue

    if op == "exec":
        code = request.get("code", "")
        max_output = int(request.get("max_output_bytes", 65536))
        if max_output <= 0:
            max_output = 65536

        out = LimitedTextBuffer(max_output)
        err = LimitedTextBuffer(max_output)
        response = {"ok": True, "stdout": "", "stderr": "", "result": "", "truncated": {}}

        try:
            with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
                result_obj = None
                try:
                    compiled = compile(code, "<foxctl-python-session>", "eval")
                except SyntaxError:
                    compiled = compile(code, "<foxctl-python-session>", "exec")
                    exec(compiled, globals_ns, globals_ns)
                else:
                    result_obj = eval(compiled, globals_ns, globals_ns)

                if result_obj is not None:
                    try:
                        response["result"] = repr(result_obj)
                    except Exception:
                        response["result"] = "<repr failed>"
        except Exception:
            response["ok"] = False
            response["exception"] = traceback.format_exc()

        response["stdout"] = out.getvalue()
        response["stderr"] = err.getvalue()
        if out.truncated:
            response["truncated"]["stdout"] = True
        if err.truncated:
            response["truncated"]["stderr"] = True
        result_text, result_truncated = truncate_text(response["result"], max_output)
        response["result"] = result_text
        if result_truncated:
            response["truncated"]["result"] = True
        if not response["truncated"]:
            response.pop("truncated", None)
        send(response)
        continue

    if op == "snapshot":
        snapshot = {}
        for key, value in globals_ns.items():
            if key.startswith("__"):
                continue
            try:
                json.dumps(value)
            except Exception:
                continue
            snapshot[key] = value
        send({"ok": True, "state": snapshot})
        continue

    if op == "close":
        send({"ok": True})
        break

    send({"ok": False, "error": f"unknown operation: {op}"})
`
