package repl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSmolVMPythonSessionSmoke(t *testing.T) {
	if strings.TrimSpace(os.Getenv("FOXCTL_RLM_SMOLVM_SMOKE")) == "" {
		t.Skip("set FOXCTL_RLM_SMOLVM_SMOKE=1 to run the smolvm integration smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sandbox := NewSmolVMPythonSession(SmolVMPythonOptions{
		MachineName:           "foxctl-rlm-longcot-glibc-offline",
		Image:                 "python:3.12-slim",
		GuestWorkDir:          "/workspace/foxctl-rlm-python",
		Network:               false,
		CreateOnInit:          false,
		StartOnInit:           true,
		StopOnClose:           true,
		AllowPackageInstall:   true,
		AllowedPackages:       []string{"python-chess"},
		PackageAliases:        map[string]string{"chess": "python-chess"},
		PackageInstallTimeout: 2 * time.Minute,
	})
	if err := sandbox.Init(ctx, map[string]any{"prompt": "smoke"}); err != nil {
		t.Fatalf("init smolvm python sandbox: %v", err)
	}
	defer func() { _ = sandbox.Close(context.Background()) }()

	result, err := sandbox.Execute(ctx, "import chess\nprint(chess.Board().fen())")
	if err != nil {
		t.Fatalf("execute smolvm python: %v", err)
	}
	if !strings.Contains(result.Output, "rnbqkbnr/pppppppp") {
		t.Fatalf("unexpected smolvm output: %s", result.Output)
	}

	result, err = sandbox.Execute(ctx, strings.Join([]string{
		`RLM_CHECK_JSON = {"pass": True, "reason": "computed"}`,
		`RLM_ANSWER_JSON = {"answer": "solution = 1", "pass": True, "checks": ["computed"]}`,
	}, "\n"))
	if err != nil {
		t.Fatalf("execute smolvm sentinel globals: %v", err)
	}
	if !strings.Contains(result.Output, `RLM_CHECK_JSON={"pass":true,"reason":"computed"}`) {
		t.Fatalf("missing smolvm auto-emitted check sentinel: %s", result.Output)
	}
	if !strings.Contains(result.Output, `RLM_ANSWER_JSON={"answer":"solution = 1","pass":true,"checks":["computed"]}`) {
		t.Fatalf("missing smolvm auto-emitted answer sentinel: %s", result.Output)
	}
}

func TestMissingSmolMachineSidecarPath(t *testing.T) {
	path, ok := missingSmolMachineSidecarPath("Error: agent operation failed: start machine: source .smolmachine not found: /private/tmp/foxctl-python312-clean-pack.smolmachine\nThe file may have been moved or deleted.")
	if !ok {
		t.Fatal("expected stale sidecar error to be detected")
	}
	if path != "/private/tmp/foxctl-python312-clean-pack.smolmachine" {
		t.Fatalf("path=%q", path)
	}

	if path, ok := missingSmolMachineSidecarPath("Error: network is unreachable"); ok || path != "" {
		t.Fatalf("unexpected stale sidecar detection path=%q ok=%v", path, ok)
	}
}
