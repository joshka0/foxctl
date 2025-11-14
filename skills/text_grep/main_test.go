package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/skillslib"
)

func TestGrepProducesPreview(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	content := "alpha beta\ngamma\nalpha delta\n"
	if err := os.WriteFile(filepath.Join(work, "sample.txt"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { _ = rc.Close() }()

	in := input{
		Path:       work,
		Pattern:    "alpha",
		MaxMatches: 10,
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
	if data["match_count"].(float64) != 2 {
		t.Fatalf("expected 2 matches, got %v", data["match_count"])
	}
	preview := data["preview"].([]any)
	if len(preview) != 2 {
		t.Fatalf("expected preview length 2, got %d", len(preview))
	}
	if _, ok := data["artifact"]; ok {
		t.Fatalf("did not expect artifact for small result")
	}
}

func TestGrepCreatesArtifactForLargeResults(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	var builder bytes.Buffer
	for i := 0; i < 20; i++ {
		builder.WriteString("match line\n")
	}
	if err := os.WriteFile(filepath.Join(work, "many.txt"), builder.Bytes(), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf, work)
	defer func() { _ = rc.Close() }()
	rc.MaxPreview = 5

	in := input{
		Path:       work,
		Pattern:    "match",
		MaxMatches: 100,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	data := env["data"].(map[string]any)
	if _, ok := data["artifact"]; !ok {
		t.Fatalf("expected artifact for large result")
	}
}

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillslib.RunnerContext {
	t.Helper()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(workspace); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		_ = os.Chdir(oldwd)
	}()
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
	rc, err := skillslib.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}
