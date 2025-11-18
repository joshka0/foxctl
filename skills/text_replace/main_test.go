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

func TestReplaceSimpleLiteral(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "hello world\nhello universe\ngoodbye world\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "hello",
		Replacement: "hi",
		Paths:       []string{testFile},
		Literal:     true,
		DryRun:      false,
		MaxFiles:    100,
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
	if data["files_modified"].(float64) != 1 {
		t.Fatalf("expected 1 file modified, got %v", data["files_modified"])
	}

	// Verify file was actually modified
	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "hi world\nhi universe\ngoodbye world\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceDryRun(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "foo bar\nfoo baz\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "foo",
		Replacement: "qux",
		Paths:       []string{testFile},
		Literal:     true,
		DryRun:      true,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["dry_run"].(bool) != true {
		t.Fatalf("expected dry_run true")
	}

	// Verify file was NOT modified
	unchanged, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(unchanged) != content {
		t.Fatalf("expected file unchanged, got %q", string(unchanged))
	}
}

func TestReplaceRegex(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "test123\ntest456\nnodigits\n"
	testFile := filepath.Join(work, "test.txt")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     `test(\d+)`,
		Replacement: "result$1",
		Paths:       []string{testFile},
		Literal:     false,
		DryRun:      false,
		MaxFiles:    100,
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

	// Verify regex replacement worked
	modified, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("read modified file: %v", err)
	}
	expected := "result123\nresult456\nnodigits\n"
	if string(modified) != expected {
		t.Fatalf("expected %q, got %q", expected, string(modified))
	}
}

func TestReplaceMultipleFiles(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	// Create multiple files
	files := []string{"file1.txt", "file2.txt", "file3.txt"}
	for _, fname := range files {
		content := "old value\n"
		if err := os.WriteFile(filepath.Join(work, fname), []byte(content), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "old",
		Replacement: "new",
		Paths:       []string{work},
		Literal:     true,
		DryRun:      false,
		MaxFiles:    100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["files_modified"].(float64) != 3 {
		t.Fatalf("expected 3 files modified, got %v", data["files_modified"])
	}

	// Verify all files were modified
	for _, fname := range files {
		modified, err := os.ReadFile(filepath.Join(work, fname))
		if err != nil {
			t.Fatalf("read file %s: %v", fname, err)
		}
		expected := "new value\n"
		if string(modified) != expected {
			t.Fatalf("file %s: expected %q, got %q", fname, expected, string(modified))
		}
	}
}

func TestReplaceWithExtensionFilter(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	// Create files with different extensions
	if err := os.WriteFile(filepath.Join(work, "test.txt"), []byte("match\n"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "test.go"), []byte("match\n"), 0o644); err != nil {
		t.Fatalf("write go: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Pattern:     "match",
		Replacement: "replaced",
		Paths:       []string{work},
		Literal:     true,
		DryRun:      false,
		MaxFiles:    100,
		Extensions:  []string{".go"},
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if data["files_modified"].(float64) != 1 {
		t.Fatalf("expected 1 file modified (only .go), got %v", data["files_modified"])
	}

	// Verify only .go file was modified
	txtContent, _ := os.ReadFile(filepath.Join(work, "test.txt"))
	if string(txtContent) != "match\n" {
		t.Fatalf(".txt should be unchanged")
	}

	goContent, _ := os.ReadFile(filepath.Join(work, "test.go"))
	if string(goContent) != "replaced\n" {
		t.Fatalf(".go should be modified, got %q", string(goContent))
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.RunnerContext {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
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
