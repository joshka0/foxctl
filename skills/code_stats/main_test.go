package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestCodeStatsBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create a simple Go file
	goCode := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(goCode), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Path:        work,
		BreakdownBy: "language",
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "ok" {
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	data := env["data"].(map[string]any)
	statistics := data["statistics"].(map[string]any)
	if statistics["total_files"].(float64) != 1 {
		t.Fatalf("expected 1 file, got %v", statistics["total_files"])
	}
	if statistics["total_lines"].(float64) < 5 {
		t.Fatalf("expected at least 5 lines, got %v", statistics["total_lines"])
	}

	// Check languages breakdown
	languages := statistics["languages"].(map[string]any)
	if languages["Go"] == nil {
		t.Fatalf("expected Go language to be detected")
	}
}

func TestCodeStatsMultipleLanguages(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create Go and Python files
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write go file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "script.py"), []byte("print('hello')\n"), 0o644); err != nil {
		t.Fatalf("write py file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Path:        work,
		BreakdownBy: "language",
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	data := env["data"].(map[string]any)
	statistics := data["statistics"].(map[string]any)
	languages := statistics["languages"].(map[string]any)

	if languages["Go"] == nil {
		t.Fatalf("expected Go to be detected")
	}
	if languages["Python"] == nil {
		t.Fatalf("expected Python to be detected")
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer) *runner.RunnerContext {
	t.Helper()
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   filepath.Join(state, "cas"),
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}
