package repl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPythonSession_PromptBindingAndPersistence(t *testing.T) {
	s := newTestSession(t, map[string]any{"prompt": "hello"})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := s.Execute(ctx, "prompt")
	if err != nil {
		t.Fatalf("execute prompt failed: %v", err)
	}
	if got := metadataString(t, res.Metadata, "result"); got != "'hello'" {
		t.Fatalf("unexpected prompt result: %q", got)
	}

	if _, err := s.Execute(ctx, "x = 41"); err != nil {
		t.Fatalf("execute assignment failed: %v", err)
	}
	res, err = s.Execute(ctx, "x + 1")
	if err != nil {
		t.Fatalf("execute expression failed: %v", err)
	}
	if got := metadataString(t, res.Metadata, "result"); got != "42" {
		t.Fatalf("unexpected expression result: %q", got)
	}
}

func TestPythonSession_StdoutAndExceptionCapture(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := s.Execute(ctx, `print("hi")`)
	if err != nil {
		t.Fatalf("execute print failed: %v", err)
	}
	if got := metadataString(t, res.Metadata, "stdout"); got != "hi\n" {
		t.Fatalf("unexpected stdout: %q", got)
	}

	res, err = s.Execute(ctx, `raise ValueError("boom")`)
	if err != nil {
		t.Fatalf("execute raise failed: %v", err)
	}
	okValue, ok := res.Metadata["ok"].(bool)
	if !ok {
		t.Fatalf("metadata ok missing or not bool: %#v", res.Metadata["ok"])
	}
	if okValue {
		t.Fatalf("expected ok=false for exception result")
	}

	exception := metadataString(t, res.Metadata, "exception")
	if !strings.Contains(exception, "ValueError: boom") {
		t.Fatalf("exception text missing ValueError details: %q", exception)
	}
}

func TestPythonSession_ExecuteTimeout(t *testing.T) {
	s := newTestSession(t, nil)
	pid := pythonSessionPID(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := s.Execute(ctx, "import time\ntime.sleep(5)")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got: %v", err)
	}

	_, err = s.Execute(context.Background(), "1 + 1")
	if err == nil {
		t.Fatal("expected session to be unusable after timeout")
	}
	eventuallyProcessGone(t, pid, 3*time.Second)
}

func TestPythonSession_Close(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.Close(ctx); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if _, err := s.Execute(context.Background(), "1 + 1"); !errors.Is(err, errSessionClosed) {
		t.Fatalf("expected errSessionClosed after close, got: %v", err)
	}
}

func TestPythonSessionPreservesConfiguredWorkDir(t *testing.T) {
	pythonPath, err := FindPython()
	if err != nil {
		t.Skipf("python not available: %v", err)
	}

	workDir := t.TempDir()
	s := NewPythonSession(Options{
		PythonPath:      pythonPath,
		WorkDir:         workDir,
		PreserveWorkDir: true,
		MaxOutputBytes:  16 * 1024,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if got := s.WorkDir(); got != workDir {
		t.Fatalf("WorkDir()=%q want %q", got, workDir)
	}
	result, err := s.Execute(ctx, `open("turn.txt", "w").write("kept")`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if metadataString(t, result.Metadata, "turn_path") == "" {
		t.Fatalf("turn_path metadata missing: %#v", result.Metadata)
	}
	if _, err := os.Stat(filepath.Join(workDir, "turn-0001.py")); err != nil {
		t.Fatalf("turn source file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "turn-0001.output.txt")); err != nil {
		t.Fatalf("turn output file missing: %v", err)
	}
	if err := s.Close(ctx); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "turn.txt")); err != nil {
		t.Fatalf("preserved work dir file missing: %v", err)
	}
}

func newTestSession(t *testing.T, initState map[string]any) *PythonSession {
	t.Helper()

	pythonPath, err := FindPython()
	if err != nil {
		t.Skipf("python not available: %v", err)
	}

	s := NewPythonSession(Options{
		PythonPath:     pythonPath,
		MaxOutputBytes: 16 * 1024,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Init(ctx, initState); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		_ = s.Close(closeCtx)
	})
	return s
}

func metadataString(t *testing.T, metadata map[string]any, key string) string {
	t.Helper()
	value, ok := metadata[key]
	if !ok {
		t.Fatalf("metadata key %q missing", key)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("metadata key %q is not a string: %#v", key, value)
	}
	return text
}

func pythonSessionPID(t *testing.T, s *PythonSession) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil || s.cmd.Process == nil {
		t.Fatal("python session process is not initialized")
	}
	return s.cmd.Process.Pid
}

func eventuallyProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after %s", pid, timeout)
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
