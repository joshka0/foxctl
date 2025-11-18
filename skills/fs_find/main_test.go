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

func TestFsFindBasicSearch(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	// Create test files
	files := []string{"main.go", "test.go", "README.md", "config.yaml"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(work, f), []byte("test"), 0o644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	buf := &bytes.Buffer{}
	rc := newTestRunnerContext(t, buf)
	t.Cleanup(func() {
		if err := rc.Close(); err != nil {
			t.Fatalf("close runner context: %v", err)
		}
	})

	in := input{
		Path:       work,
		Query:      "main",
		MaxResults: 10,
	}
	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env["status"] != "ok" {
		// Print the error for debugging
		if errData, ok := env["error"].(map[string]any); ok {
			t.Fatalf("expected ok status, got error: %v - %v", errData["code"], errData["message"])
		}
		t.Fatalf("expected ok status, got %v", env["status"])
	}

	data, ok := env["data"].(map[string]any)
	if !ok || data == nil {
		t.Fatalf("expected data field in response")
	}

	// Results are in "preview" field
	results, ok := data["preview"].([]any)
	if !ok {
		t.Fatalf("expected preview field in data")
	}

	if len(results) == 0 {
		t.Fatalf("expected at least one result")
	}

	// Verify main.go was found
	found := false
	for _, r := range results {
		result := r.(map[string]any)
		if result["name"] == "main.go" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find main.go")
	}
}

func TestFsFindWithExtensionFilter(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	work := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	// Create test files
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "main.py"), []byte("test"), 0o644); err != nil {
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
		Path:       work,
		Query:      "",
		Pattern:    "*.go",
		MaxResults: 10,
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

	data, ok := env["data"].(map[string]any)
	if !ok || data == nil {
		t.Fatalf("expected data field in response")
	}

	results, ok := data["preview"].([]any)
	if !ok {
		t.Fatalf("expected preview field in data")
	}

	// Should only find .go files
	for _, r := range results {
		result := r.(map[string]any)
		name := result["name"].(string)
		if filepath.Ext(name) != ".go" {
			t.Fatalf("expected only .go files, got %s", name)
		}
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
