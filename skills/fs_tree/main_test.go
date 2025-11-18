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

func TestFsTreeBasic(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(filepath.Join(work, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "file.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "subdir", "nested.txt"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Path:   work,
		Format: "tree",
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
