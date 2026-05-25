package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSkillsCRUDHandlerOperationComesFromManifestNotClient(t *testing.T) {
	skillsRoot := t.TempDir()
	writeRESTEchoSkill(t, skillsRoot, "test/crud", map[string]string{
		http.MethodPost: "create",
	}, "")

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/skills/test/crud?operation=query-override",
		strings.NewReader(`{"operation":"body-override","value":"kept"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	SkillsCRUDHandler(testSkillCRUDConfig(t, skillsRoot), zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	body, input := decodeRESTEchoResponse(t, rr.Body.Bytes())
	if body.Skill != "test/crud" {
		t.Fatalf("skill=%q want test/crud", body.Skill)
	}
	if body.SkillVersion != "1.0.0" {
		t.Fatalf("skill_version=%q want 1.0.0", body.SkillVersion)
	}
	if body.Error != "" {
		t.Fatalf("error=%q want empty", body.Error)
	}
	if got := input["operation"]; got != "create" {
		t.Fatalf("operation=%v want create", got)
	}
	if got := input["value"]; got != "kept" {
		t.Fatalf("value=%v want kept", got)
	}
}

func TestSkillsCRUDHandlerResourceIDUsesConfiguredIDParam(t *testing.T) {
	skillsRoot := t.TempDir()
	writeRESTEchoSkill(t, skillsRoot, "test/crud", map[string]string{
		"GET_ID": "show",
	}, "item_id")

	req := httptest.NewRequest(http.MethodGet, "/api/skills/test/crud/123?filter=open", nil)
	rr := httptest.NewRecorder()

	SkillsCRUDHandler(testSkillCRUDConfig(t, skillsRoot), zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	_, input := decodeRESTEchoResponse(t, rr.Body.Bytes())
	if got := input["operation"]; got != "show" {
		t.Fatalf("operation=%v want show", got)
	}
	if got := input["item_id"]; got != "123" {
		t.Fatalf("item_id=%v want 123", got)
	}
	if got := input["filter"]; got != "open" {
		t.Fatalf("filter=%v want open", got)
	}
}

func TestSkillsCRUDHandlerWorkspaceRootControlsCWDWithoutLeakingInput(t *testing.T) {
	skillsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/cwd-rest", nil, "reject")

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/skills/test/cwd-rest?workspace_root="+url.QueryEscape(workspaceRoot),
		strings.NewReader(`{"value":"kept"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	SkillsCRUDHandler(testSkillCRUDConfig(t, skillsRoot), zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body struct {
		Output json.RawMessage `json:"output"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	var output struct {
		CWD string `json:"cwd"`
	}
	if err := json.Unmarshal(body.Output, &output); err != nil {
		t.Fatalf("decode output: %v raw=%s", err, string(body.Output))
	}
	if got, want := canonicalTestPath(output.CWD), canonicalTestPath(workspaceRoot); got != want {
		t.Fatalf("cwd=%q want %q", output.CWD, workspaceRoot)
	}
}

func testSkillCRUDConfig(t *testing.T, skillsRoot string) config.Config {
	t.Helper()
	return config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	}
}

type restEchoResponse struct {
	OK           bool            `json:"ok"`
	Skill        string          `json:"skill"`
	SkillVersion string          `json:"skill_version"`
	Output       json.RawMessage `json:"output"`
	Error        string          `json:"error"`
	DurationMS   int64           `json:"duration_ms"`
}

func decodeRESTEchoResponse(t *testing.T, data []byte) (restEchoResponse, map[string]any) {
	t.Helper()
	var body restEchoResponse
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, string(data))
	}
	if !body.OK {
		t.Fatalf("ok=false body=%s", string(data))
	}
	var output struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body.Output, &output); err != nil {
		t.Fatalf("decode output: %v raw=%s", err, string(body.Output))
	}
	return body, output.Input
}

func writeRESTEchoSkill(t *testing.T, root, name string, methods map[string]string, idParam string) {
	t.Helper()
	skillDir := filepath.Join(root, strings.ReplaceAll(name, "/", string(filepath.Separator)))
	binDir := filepath.Join(skillDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	var methodsYAML strings.Builder
	for method, operation := range methods {
		methodsYAML.WriteString(fmt.Sprintf("    %s: %q\n", method, operation))
	}
	idParamYAML := ""
	if strings.TrimSpace(idParam) != "" {
		idParamYAML = fmt.Sprintf("  id_param: %q\n", idParam)
	}

	manifest := fmt.Sprintf(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: %s
  version: "1.0.0"
  description: rest echo test skill
distribution:
  type: exec
  exec:
    entry: ./bin/test-skill
io:
  format: JSON
signature:
  command: %s
  parameters:
    - name: operation
      type: string
      required: false
capabilities:
  network: none
  filesystem: []
  pure: true
openapi:
  enabled: true
  methods:
%s%s`, name, name, methodsYAML.String(), idParamYAML)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}

	script := `#!/bin/sh
input="$(cat)"
printf '{"input":%s}\n' "$input"
`
	if err := os.WriteFile(filepath.Join(binDir, "test-skill"), []byte(script), 0o755); err != nil {
		t.Fatalf("write test skill: %v", err)
	}
}
