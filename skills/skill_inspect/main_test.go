package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer) *lite.RunContext {
	t.Helper()

	state := t.TempDir()
	rc, err := lite.BuildRunContext(lite.LiteConfig{
		Home:           state,
		InlineOutputKB: 64,
		Paths: lite.LitePaths{
			CAS:   filepath.Join(state, "cas"),
			Cache: filepath.Join(state, "cache"),
		},
		CAS: lite.LiteCASPolicy{Store: true, Expose: "off"},
	}, stdout)
	if err != nil {
		t.Fatalf("build run context: %v", err)
	}
	return rc
}

func withFixtureSkill(t *testing.T) {
	t.Helper()

	root := t.TempDir()
	skillDir := filepath.Join(root, "skills", "test_skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	manifest := `name: test_skill
description: "A test skill for inspection"
command: test/skill
parameters:
  - name: query
    type: string
    required: true
    description: "The search query"
  - name: limit
    type: integer
    required: false
    default: "10"
returns:
  description: "Results"
`
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}

	source := `package main

type Input struct {
	Query string ` + "`json:\"query\"`" + `
	Limit int    ` + "`json:\"limit\"`" + `
}

type Output struct {
	Results []string ` + "`json:\"results\"`" + `
}

func main() {}

func helper(name string) string {
	return name
}
`
	if err := os.WriteFile(filepath.Join(skillDir, "main.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir fixture root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})
}

func runSkillInspect(t *testing.T, in input) map[string]any {
	t.Helper()

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	if err := run(context.Background(), rc, in); err != nil {
		t.Fatalf("run: %v", err)
	}

	var envelope struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v\nraw: %s", err, stdout.String())
	}
	if envelope.Status != "ok" {
		t.Fatalf("status = %q, want ok", envelope.Status)
	}
	return envelope.Data
}

func TestRunRejectsMissingSkillName(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	err := run(context.Background(), rc, input{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "skill_name is required") {
		t.Fatalf("error = %q, want skill_name is required", err.Error())
	}
}

func TestRunRejectsMissingSkill(t *testing.T) {
	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})

	err = run(context.Background(), rc, input{SkillName: "missing"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "skill not found") {
		t.Fatalf("error = %q, want skill not found", err.Error())
	}
}

func TestRunRejectsInvalidView(t *testing.T) {
	withFixtureSkill(t)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout)
	defer func() { _ = rc.Close() }()

	err := run(context.Background(), rc, input{SkillName: "test_skill", View: "bogus"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid view") {
		t.Fatalf("error = %q, want invalid view", err.Error())
	}
}

func TestRunDefaultsToAPIView(t *testing.T) {
	withFixtureSkill(t)

	data := runSkillInspect(t, input{SkillName: "test_skill"})
	if data["view"] != "api" {
		t.Fatalf("view = %v, want api", data["view"])
	}
	if data["skill_name"] != "test_skill" {
		t.Fatalf("skill_name = %v, want test_skill", data["skill_name"])
	}
	if data["description"] != "A test skill for inspection" {
		t.Fatalf("description = %v, want fixture description", data["description"])
	}
	if data["parameters"] == nil || data["types"] == nil {
		t.Fatalf("api view missing parameters or types: %#v", data)
	}
}

func TestRunSupportsCoreViews(t *testing.T) {
	tests := []struct {
		view string
		key  string
	}{
		{view: "manifest", key: "manifest"},
		{view: "types", key: "types"},
		{view: "implementation", key: "functions"},
		{view: "full", key: "source"},
		{view: "examples", key: "examples"},
		{view: "all", key: "source_lines"},
	}

	for _, tt := range tests {
		t.Run(tt.view, func(t *testing.T) {
			withFixtureSkill(t)

			data := runSkillInspect(t, input{SkillName: "test_skill", View: tt.view})
			if data["view"] != tt.view {
				t.Fatalf("view = %v, want %s", data["view"], tt.view)
			}
			if data[tt.key] == nil {
				t.Fatalf("%s view missing %q: %#v", tt.view, tt.key, data)
			}
		})
	}
}

func TestRunImplementationViewFiltersFunction(t *testing.T) {
	withFixtureSkill(t)

	data := runSkillInspect(t, input{
		SkillName: "test_skill",
		View:      "implementation",
		Function:  "helper",
	})
	if data["filter"] != "helper" {
		t.Fatalf("filter = %v, want helper", data["filter"])
	}
	if data["function_count"] != float64(1) {
		t.Fatalf("function_count = %v, want 1", data["function_count"])
	}
	functions, ok := data["functions"].([]any)
	if !ok || len(functions) != 1 {
		t.Fatalf("functions = %#v, want one function", data["functions"])
	}
	function, ok := functions[0].(map[string]any)
	if !ok || function["name"] != "helper" {
		t.Fatalf("function = %#v, want helper", functions[0])
	}
}
