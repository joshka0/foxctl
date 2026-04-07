package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellTool_UnsupportedCommand(t *testing.T) {
	cfg := Config{
		WorkspaceRoot: t.TempDir(),
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	result, err := registry.structuredShell(context.Background(), map[string]any{
		"command": "helm list",
	})
	if err != nil {
		t.Fatalf("structuredShell: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestShellTool_CatGoMod(t *testing.T) {
	repoRoot := makeShellToolWorkspace(t)
	cfg := Config{
		WorkspaceRoot: repoRoot,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	result, err := registry.structuredShell(context.Background(), map[string]any{
		"command": "cat go.mod",
	})
	if err != nil {
		t.Fatalf("structuredShell: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	content := extractResultContent(t, result)
	summary, _ := content["summary"].(string)
	if !strings.Contains(summary, "module github.com/jkatigb/agentctl") {
		t.Fatalf("summary=%q missing module line", summary)
	}
	route, _ := content["route"].(map[string]any)
	if route["skill"] != "fs/read" {
		t.Fatalf("route.skill=%v want fs/read", route["skill"])
	}
}

func TestShellTool_CatGoModWithMeasure(t *testing.T) {
	repoRoot := makeShellToolWorkspace(t)
	cfg := Config{
		WorkspaceRoot: repoRoot,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	result, err := registry.structuredShell(context.Background(), map[string]any{
		"command":     "cat go.mod",
		"measure_raw": true,
		"token_model": "cl100k_base",
	})
	if err != nil {
		t.Fatalf("structuredShell: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %v", result.Content)
	}

	content := extractResultContent(t, result)
	measure, ok := content["measure"].(map[string]any)
	if !ok {
		t.Fatalf("measure missing from result: %v", content)
	}
	if _, ok := measure["raw"].(map[string]any); !ok {
		t.Fatalf("raw measure missing: %v", measure)
	}
}

func TestRegistryToolExecutor_Shell(t *testing.T) {
	repoRoot := makeShellToolWorkspace(t)
	registry, err := NewRegistry(Config{WorkspaceRoot: repoRoot}, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	executor := NewRegistryToolExecutor(registry)
	args, err := json.Marshal(map[string]any{"command": "cat go.mod"})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	got, err := executor.Execute(context.Background(), "shell", args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(got, `"summary":"`) {
		t.Fatalf("expected JSON payload with summary, got %s", got)
	}
	if !strings.Contains(got, `module github.com/jkatigb/agentctl`) {
		t.Fatalf("expected go.mod preview, got %s", got)
	}
}

func makeShellToolWorkspace(t *testing.T) string {
	t.Helper()

	sourceRoot := findRepoRoot(t)
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaceRoot, "go.mod"), []byte("module github.com/jkatigb/agentctl\n\ngo 1.25.0\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	skillDir := filepath.Join(workspaceRoot, "skills", "fs_read")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(sourceRoot, "skills", "fs_read", "skill.yaml"))
	if err != nil {
		t.Fatalf("read skill manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), manifest, 0o644); err != nil {
		t.Fatalf("write skill manifest: %v", err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(skillDir, "fs_read"), "./skills/fs_read")
	build.Dir = sourceRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fs_read artifact: %v\n%s", err, out)
	}
	return workspaceRoot
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
