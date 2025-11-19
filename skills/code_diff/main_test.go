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
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func TestCodeDiffBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	oldContent := "line 1\nline 2\nline 3\n"
	newContent := "line 1\nline 2 modified\nline 3\nline 4\n"

	oldFile := filepath.Join(work, "old.txt")
	newFile := filepath.Join(work, "new.txt")

	if err := os.WriteFile(oldFile, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("write old file: %v", err)
	}
	if err := os.WriteFile(newFile, []byte(newContent), 0o644); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		OldPath:      oldFile,
		NewPath:      newFile,
		ContextLines: 3,
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

	// Just verify we have data
	if env["data"] == nil {
		t.Fatalf("expected data in response")
	}
}

func TestCodeDiffIdentical(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := "line 1\nline 2\n"
	file1 := filepath.Join(work, "file1.txt")
	file2 := filepath.Join(work, "file2.txt")

	if err := os.WriteFile(file1, []byte(content), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte(content), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		OldPath: file1,
		NewPath: file2,
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

	// Just verify we have data
	if env["data"] == nil {
		t.Fatalf("expected data in response")
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, _ string) *runner.RunnerContext {
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
