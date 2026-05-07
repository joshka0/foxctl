package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSkillRunnerRunUsesExplicitWorkspaceRoot(t *testing.T) {
	skillsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/cwd", nil, "reject")

	runner := NewSkillRunner(config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	})

	result, err := runner.Run(context.Background(), "test/cwd", map[string]any{
		"workspace_root": workspaceRoot,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("success=false error=%s", result.Error)
	}

	var output struct {
		CWD       string `json:"cwd"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v raw=%s", err, string(result.Output))
	}
	if got, want := canonicalTestPath(output.CWD), canonicalTestPath(workspaceRoot); got != want {
		t.Fatalf("cwd=%q want %q", output.CWD, workspaceRoot)
	}
	if got, want := canonicalTestPath(output.Workspace), canonicalTestPath(workspaceRoot); got != want {
		t.Fatalf("FOXCTL_WORKSPACE=%q want %q", output.Workspace, workspaceRoot)
	}
}

func TestSkillRunnerRunKeepsDeclaredWorkspaceRoot(t *testing.T) {
	skillsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/cwd-declared", []string{"workspace_root"}, "require")

	runner := NewSkillRunner(config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	})

	result, err := runner.Run(context.Background(), "test/cwd-declared", map[string]any{
		"workspace_root": workspaceRoot,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Success {
		t.Fatalf("success=false error=%s", result.Error)
	}
}

func TestSkillRunnerRunRequiresDeclaredWorkspaceRoot(t *testing.T) {
	skillsRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/cwd-requires-workspace", []string{"workspace_root"}, "require")

	runner := NewSkillRunner(config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	})

	_, err := runner.Run(context.Background(), "test/cwd-requires-workspace", map[string]any{
		"query": "anything",
	})
	if err == nil {
		t.Fatal("Run succeeded without a workspace root")
	}
	if !strings.Contains(err.Error(), "workspace root required") {
		t.Fatalf("error=%q, want clear workspace requirement", err.Error())
	}
}

func TestSkillRunnerRunRejectsNonStringWorkspaceRoot(t *testing.T) {
	skillsRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/cwd-bad-workspace", []string{"workspace_root"}, "require")

	runner := NewSkillRunner(config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	})

	_, err := runner.Run(context.Background(), "test/cwd-bad-workspace", map[string]any{
		"workspace_root": 42,
	})
	if err == nil {
		t.Fatal("Run succeeded with a non-string workspace root")
	}
	if !strings.Contains(err.Error(), "workspace_root must be a string workspace root") {
		t.Fatalf("error=%q, want clear type error", err.Error())
	}
}

func writeCwdSkill(t *testing.T, root, name string, params []string, workspaceInputMode string) {
	t.Helper()
	skillDir := filepath.Join(root, strings.ReplaceAll(name, "/", string(filepath.Separator)))
	binDir := filepath.Join(skillDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	var paramsYAML strings.Builder
	for _, param := range params {
		paramsYAML.WriteString(fmt.Sprintf("    - name: %s\n      type: string\n      required: false\n", param))
	}
	manifest := fmt.Sprintf(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: %s
  version: "1.0.0"
  description: cwd test skill
distribution:
  type: exec
  exec:
    entry: ./bin/test-skill
io:
  format: JSON
signature:
  command: %s
  parameters:
%s
capabilities:
  network: none
  filesystem: []
  pure: true
openapi:
  enabled: true
  methods:
    POST: "true"
`, name, name, paramsYAML.String())
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
	script := `#!/bin/sh
input="$(cat)"
case "` + workspaceInputMode + `" in
  reject)
    case "$input" in
      *workspace_root*|*workspace_path*|*\"workspace\"*) echo "workspace control leaked: $input" >&2; exit 12 ;;
    esac
    ;;
  require)
    case "$input" in
      *workspace_root*) ;;
      *) echo "workspace_root missing from input: $input" >&2; exit 13 ;;
    esac
    ;;
esac
printf '{"cwd":"%s","workspace":"%s"}\n' "$PWD" "$FOXCTL_WORKSPACE"
`
	if err := os.WriteFile(filepath.Join(binDir, "test-skill"), []byte(script), 0o755); err != nil {
		t.Fatalf("write test skill: %v", err)
	}
}

func canonicalTestPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path
}
