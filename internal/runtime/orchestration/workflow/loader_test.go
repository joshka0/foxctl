package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_ValidWorkflow(t *testing.T) {
	yaml := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: test-workflow
  description: A test workflow
inputs:
  - name: path
    type: string
    required: true
steps:
  - id: step1
    skill: test/skill
    input:
      path: "{{.inputs.path}}"
outputs:
  - name: result
    value: "{{.step1.data}}"
`
	wf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if wf.Metadata.Name != "test-workflow" {
		t.Errorf("expected name 'test-workflow', got %q", wf.Metadata.Name)
	}

	if len(wf.Steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(wf.Steps))
	}

	if len(wf.Inputs) != 1 {
		t.Errorf("expected 1 input, got %d", len(wf.Inputs))
	}

	if len(wf.Outputs) != 1 {
		t.Errorf("expected 1 output, got %d", len(wf.Outputs))
	}
}

func TestParse_InvalidAPIVersion(t *testing.T) {
	yaml := `
apiVersion: invalid/v1
kind: Workflow
metadata:
  name: test
steps:
  - id: step1
    skill: test
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid API version")
	}
}

func TestParse_MissingName(t *testing.T) {
	yaml := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  description: no name
steps:
  - id: step1
    skill: test
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParse_NoSteps(t *testing.T) {
	yaml := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: test
steps: []
`
	_, err := Parse([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for no steps")
	}
}

func TestParse_StepWithLoop(t *testing.T) {
	yaml := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: loop-workflow
steps:
  - id: find
    skill: fs/find
    input:
      pattern: "*.go"
  - id: process
    skill: code/symbols
    loop:
      over: "{{.find.data.files}}"
      as: file
    input:
      path: "{{.file}}"
`
	wf, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if wf.Steps[1].Loop == nil {
		t.Fatal("expected loop config on step 2")
	}

	if wf.Steps[1].Loop.Over != "{{.find.data.files}}" {
		t.Errorf("unexpected loop.over: %s", wf.Steps[1].Loop.Over)
	}

	if wf.Steps[1].Loop.As != "file" {
		t.Errorf("unexpected loop.as: %s", wf.Steps[1].Loop.As)
	}
}

func TestLoader_LoadFromPath(t *testing.T) {
	// Create temp directory and workflow file
	tmpDir, err := os.MkdirTemp("", "workflow-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wfPath := filepath.Join(tmpDir, "test.yaml")
	content := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: test-from-file
steps:
  - id: step1
    skill: test/skill
`
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	loader := NewLoader()
	handle, err := loader.Load(wfPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if handle.Workflow.Metadata.Name != "test-from-file" {
		t.Errorf("expected name 'test-from-file', got %q", handle.Workflow.Metadata.Name)
	}
}

func TestLoader_LoadFromSearchPaths(t *testing.T) {
	// Create temp directory structure
	tmpDir, err := os.MkdirTemp("", "workflow-search-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	wfPath := filepath.Join(tmpDir, "my-workflow.yaml")
	content := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: my-workflow
steps:
  - id: step1
    skill: test/skill
`
	if err := os.WriteFile(wfPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write workflow file: %v", err)
	}

	loader := NewLoader(WithWorkflowPaths(tmpDir))
	handle, err := loader.Load("my-workflow")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if handle.Workflow.Metadata.Name != "my-workflow" {
		t.Errorf("expected name 'my-workflow', got %q", handle.Workflow.Metadata.Name)
	}
}

func TestLoader_List(t *testing.T) {
	// Create temp directory with workflows
	tmpDir, err := os.MkdirTemp("", "workflow-list-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create two workflow files
	wf1 := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: workflow-1
  description: First workflow
steps:
  - id: step1
    skill: test/skill
`
	wf2 := `
apiVersion: foxctl/v1
kind: Workflow
metadata:
  name: workflow-2
  description: Second workflow
steps:
  - id: step1
    skill: test/skill
`
	if err := os.WriteFile(filepath.Join(tmpDir, "workflow-1.yaml"), []byte(wf1), 0o644); err != nil {
		t.Fatalf("failed to write workflow-1.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "workflow-2.yml"), []byte(wf2), 0o644); err != nil {
		t.Fatalf("failed to write workflow-2.yml: %v", err)
	}

	loader := NewLoader(WithWorkflowPaths(tmpDir))
	handles, err := loader.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(handles) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(handles))
	}
}

func TestValidate_LoopConfig(t *testing.T) {
	// Missing loop.over
	wf := &Workflow{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata:   Metadata{Name: "test"},
		Steps: []Step{
			{
				ID:    "step1",
				Skill: "test",
				Loop:  &LoopConfig{As: "item"},
			},
		},
	}

	err := Validate(wf)
	if err == nil {
		t.Fatal("expected error for missing loop.over")
	}

	// Missing loop.as
	wf.Steps[0].Loop = &LoopConfig{Over: "{{.data}}"}
	err = Validate(wf)
	if err == nil {
		t.Fatal("expected error for missing loop.as")
	}

	// Valid loop
	wf.Steps[0].Loop = &LoopConfig{Over: "{{.data}}", As: "item"}
	err = Validate(wf)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
