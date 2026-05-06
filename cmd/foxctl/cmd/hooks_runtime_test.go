package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHooksCommand(t *testing.T) {
	cmd := newHooksCommand()
	if cmd.Use != "hooks" {
		t.Fatalf("expected use hooks, got %s", cmd.Use)
	}
	got := map[string]bool{}
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	if !got["session-start"] {
		t.Fatalf("expected session-start subcommand")
	}
	if !got["session-end"] {
		t.Fatalf("expected session-end subcommand")
	}
	if !got["subagent-stop"] {
		t.Fatalf("expected subagent-stop subcommand")
	}
	if !got["todo-sync"] {
		t.Fatalf("expected todo-sync subcommand")
	}
	if !got["todo-continuation"] {
		t.Fatalf("expected todo-continuation subcommand")
	}
	if !got["task-file-link"] {
		t.Fatalf("expected task-file-link subcommand")
	}
	if !got["context-updater-drain"] {
		t.Fatalf("expected context-updater-drain subcommand")
	}
	if !got["session-restore-postcompact"] {
		t.Fatalf("expected session-restore-postcompact subcommand")
	}
	if !got["overseer-inbox"] {
		t.Fatalf("expected overseer-inbox subcommand")
	}
	if !got["overseer-inbox-post"] {
		t.Fatalf("expected overseer-inbox-post subcommand")
	}
	if !got["anchor-detect"] {
		t.Fatalf("expected anchor-detect subcommand")
	}
	if !got["memory-detector"] {
		t.Fatalf("expected memory-detector subcommand")
	}
	if !got["memory-recall"] {
		t.Fatalf("expected memory-recall subcommand")
	}
	if !got["memory-lifecycle"] {
		t.Fatalf("expected memory-lifecycle subcommand")
	}
	if !got["code-analysis"] {
		t.Fatalf("expected code-analysis subcommand")
	}
	if !got["semantic-anchors"] {
		t.Fatalf("expected semantic-anchors subcommand")
	}
	if !got["live-index"] {
		t.Fatalf("expected live-index subcommand")
	}
	if !got["lsp-diagnostics"] {
		t.Fatalf("expected lsp-diagnostics subcommand")
	}
	if !got["embedding-flush"] {
		t.Fatalf("expected embedding-flush subcommand")
	}
	if !got["plan-sync"] {
		t.Fatalf("expected plan-sync subcommand")
	}
	if !got["graph-maintenance"] {
		t.Fatalf("expected graph-maintenance subcommand")
	}
}

func TestHooksSemanticAnchorsOutputsEnvelopeCompatibleAdvisoryResponse(t *testing.T) {
	t.Setenv("FOXCTL_SEMANTIC_ANCHORS_HOOK", "1")
	workspace := t.TempDir()
	writeHookRuntimeTestFile(t, workspace, "internal/demo_test.go", "package demo\n")
	writeHookRuntimeTestFile(t, workspace, "internal/demo.go", `package demo

// [[test:internal/demo_test.go]]
func Build() {}
`)

	cmd := newHooksSemanticAnchorsCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader(`{"tool_input":{"file_path":"internal/demo.go"}}`))
	cmd.SetArgs([]string{"--workspace", workspace})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("semantic-anchors command: %v", err)
	}

	var env struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Command string `json:"command"`
		Data    struct {
			Response struct {
				Decision string `json:"decision"`
				Context  string `json:"context"`
				FilePath string `json:"file_path"`
			} `json:"response"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope %q: %v", out.String(), err)
	}
	if env.Version != 1 || env.Status != "ok" || env.Command != "hooks/semantic-anchors" {
		t.Fatalf("unexpected envelope: %#v", env)
	}
	if env.Data.Response.Decision != "approve" {
		t.Fatalf("decision = %q", env.Data.Response.Decision)
	}
	if !strings.Contains(env.Data.Response.Context, "Semantic anchors") {
		t.Fatalf("expected semantic anchor context, got %q", env.Data.Response.Context)
	}
}

func writeHookRuntimeTestFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
