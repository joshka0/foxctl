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

func TestPythonSessionAutoEmitsRLMSentinelGlobals(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := s.Execute(ctx, strings.Join([]string{
		`RLM_CHECK_JSON = {"pass": True, "reason": "computed"}`,
		`RLM_ANSWER_JSON = {"answer": "solution = 1", "pass": True, "checks": ["computed"]}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("execute sentinel globals failed: %v", err)
	}
	stdout := metadataString(t, res.Metadata, "stdout")
	if !strings.Contains(stdout, `RLM_CHECK_JSON={"pass":true,"reason":"computed"}`) {
		t.Fatalf("missing auto-emitted check sentinel: %q", stdout)
	}
	if !strings.Contains(stdout, `RLM_ANSWER_JSON={"answer":"solution = 1","pass":true,"checks":["computed"]}`) {
		t.Fatalf("missing auto-emitted answer sentinel: %q", stdout)
	}
}

func TestPythonSessionExecutesMultilineVerifierCode(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	code := strings.Join([]string{
		"def verify_solution():",
		"    import json",
		"    moves = [[1, 0, 1]]",
		"    assert moves[0] == [1, 0, 1]",
		"    return {'verified': True, 'final_answer': 'solution = [[1, 0, 1]]'}",
		"",
		"artifact = verify_solution()",
		"print('VERIFIER_ARTIFACT_JSON=' + __import__('json').dumps(artifact))",
	}, "\n")

	res, err := s.Execute(ctx, code)
	if err != nil {
		t.Fatalf("execute verifier code failed: %v", err)
	}
	okValue, ok := res.Metadata["ok"].(bool)
	if !ok || !okValue {
		t.Fatalf("expected ok verifier execution, metadata=%#v", res.Metadata)
	}
	stdout := metadataString(t, res.Metadata, "stdout")
	if !strings.Contains(stdout, "VERIFIER_ARTIFACT_JSON=") {
		t.Fatalf("missing verifier artifact output: %q", stdout)
	}
	if exception, _ := res.Metadata["exception"].(string); strings.TrimSpace(exception) != "" {
		t.Fatalf("unexpected exception metadata: %q", exception)
	}
}

func TestPythonSessionMultilineAssertionTraceDoesNotIncludeEvalSyntaxFallback(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	code := strings.Join([]string{
		"def verify_solution():",
		"    import json",
		"    assert False, 'bad candidate'",
		"",
		"verify_solution()",
	}, "\n")

	res, err := s.Execute(ctx, code)
	if err != nil {
		t.Fatalf("execute verifier code failed: %v", err)
	}
	okValue, ok := res.Metadata["ok"].(bool)
	if !ok || okValue {
		t.Fatalf("expected ok=false verifier assertion, metadata=%#v", res.Metadata)
	}
	exception := metadataString(t, res.Metadata, "exception")
	if !strings.Contains(exception, "AssertionError: bad candidate") {
		t.Fatalf("exception missing assertion details: %q", exception)
	}
	if strings.Contains(exception, "SyntaxError") {
		t.Fatalf("exception should not include eval fallback SyntaxError: %q", exception)
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

func TestPythonSessionPackagePolicyValidation(t *testing.T) {
	t.Parallel()

	s := NewPythonSession(Options{
		AllowPackageInstall: true,
		AllowedPackages:     []string{"python-chess", "sympy"},
	})
	got, err := s.normalizeInstallPackagesLocked([]string{"python-chess", "sympy", "python-chess"})
	if err != nil {
		t.Fatalf("normalizeInstallPackagesLocked returned error: %v", err)
	}
	if strings.Join(got, ",") != "python-chess,sympy" {
		t.Fatalf("normalized packages=%v", got)
	}
	if _, err := s.normalizeInstallPackagesLocked([]string{"requests"}); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected allowlist error, got %v", err)
	}
	if _, err := s.normalizeInstallPackagesLocked([]string{"python-chess==1.0"}); err == nil || !strings.Contains(err.Error(), "simple package name") {
		t.Fatalf("expected simple-name error, got %v", err)
	}
}

func TestPythonSessionInstallPackagesDisabledByDefault(t *testing.T) {
	s := newTestSession(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.InstallPackages(ctx, []string{"python-chess"})
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("expected disabled package install error, got %v", err)
	}
}

func TestPythonSessionInstallPackagesSmoke(t *testing.T) {
	if os.Getenv("FOXCTL_RLM_PYTHON_INSTALL_SMOKE") != "1" {
		t.Skip("set FOXCTL_RLM_PYTHON_INSTALL_SMOKE=1 to run networked pip install smoke")
	}
	pythonPath, err := FindPython()
	if err != nil {
		t.Skipf("python not available: %v", err)
	}

	s := NewPythonSession(Options{
		PythonPath:            pythonPath,
		WorkDir:               t.TempDir(),
		PreserveWorkDir:       true,
		MaxOutputBytes:        16 * 1024,
		AllowPackageInstall:   true,
		AllowedPackages:       []string{"python-chess"},
		PackageAliases:        map[string]string{"chess": "python-chess"},
		PackageInstallTimeout: 90 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := s.Init(ctx, nil); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		_ = s.Close(closeCtx)
	})

	res, err := s.Execute(ctx, strings.Join([]string{
		"import chess",
		"board = chess.Board()",
		"board.push_uci('e2e4')",
		"print(board.fen())",
	}, "\n"))
	if err != nil {
		t.Fatalf("execute python-chess failed: %v", err)
	}
	t.Logf("python-chess metadata=%#v output=%q", res.Metadata, res.Output)
	if _, ok := res.Metadata["package_auto_install"].(map[string]any); !ok {
		t.Fatalf("missing package_auto_install metadata: %#v", res.Metadata)
	}
	if got := metadataString(t, res.Metadata, "stdout"); !strings.Contains(got, "4P3") {
		t.Fatalf("unexpected chess FEN result: %q", got)
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
