package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/runtime/orchestration/workflow/frontmatter"
)

func TestLoadOrchestrationConfig_DefaultWorkflowPath(t *testing.T) {
	dir := t.TempDir()
	content := `---
tracker:
  kind: linear
  api_key: lin-key
  project_slug: AG-12
workspace:
  root: ./workspace
---
You are the worker.`
	if err := os.WriteFile(filepath.Join(dir, "WORKFLOW.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	snap, err := LoadOrchestrationConfig("", dir, frontmatter.DecodeOptions{})
	if err != nil {
		t.Fatalf("LoadOrchestrationConfig() error = %v", err)
	}
	if snap.Config.Tracker.ProjectSlug != "AG-12" {
		t.Fatalf("project_slug=%q", snap.Config.Tracker.ProjectSlug)
	}
	if snap.Version != 1 {
		t.Fatalf("version=%d want 1", snap.Version)
	}
}

func TestYAMLWorkflowLoaderUnchanged(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "wf.yaml")
	content := `
apiVersion: agentctl/v1
kind: Workflow
metadata:
  name: test-yaml
steps:
  - id: step1
    skill: test/skill
`
	if err := os.WriteFile(yamlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	l := NewLoader(WithWorkflowPaths(dir))
	handle, err := l.Load(yamlPath)
	if err != nil {
		t.Fatalf("Load() yaml error = %v", err)
	}
	if handle.Workflow.Metadata.Name != "test-yaml" {
		t.Fatalf("name=%q want test-yaml", handle.Workflow.Metadata.Name)
	}
}
