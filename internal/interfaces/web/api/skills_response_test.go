package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestSkillsRunHandler_EmitsTypedResponse(t *testing.T) {
	skillsRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	writeCwdSkill(t, skillsRoot, "test/run-response", nil, "reject")

	cfg := config.Config{
		Home: t.TempDir(),
		Paths: config.Paths{
			Skills: skillsRoot,
			Cache:  filepath.Join(t.TempDir(), "cache"),
		},
		Storage: config.StorageSettings{Root: t.TempDir()},
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/skills/run",
		strings.NewReader(`{"skill":"test/run-response","input":{"workspace_root":`+strconv.Quote(workspaceRoot)+`}}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	SkillsRunHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body SkillRunResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if !body.OK {
		t.Fatalf("ok=%v error=%q", body.OK, body.Error)
	}
	if body.Skill != "test/run-response" {
		t.Fatalf("skill=%q want test/run-response", body.Skill)
	}
	if body.Error != "" {
		t.Fatalf("error=%q want empty", body.Error)
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

func TestMCPStatusHandler_EmitsTypedResponse(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/status", nil)
	rr := httptest.NewRecorder()
	MCPStatusHandler(config.Config{}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body MCPStatusResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if body.Daemon.Running {
		t.Fatalf("daemon running=%v want false without pid file", body.Daemon.Running)
	}
	if strings.TrimSpace(body.Daemon.Addr) != mcpDefaultAddr {
		t.Fatalf("addr=%q want %q", body.Daemon.Addr, mcpDefaultAddr)
	}
	if len(body.Backends) != 3 {
		t.Fatalf("backends=%d want 3", len(body.Backends))
	}
}

func TestMCPToolsHandler_EmitsTypedResponse(t *testing.T) {
	skillsRoot := t.TempDir()
	writeTestSkillManifest(t, skillsRoot, "code/echo", "code/echo")

	req := httptest.NewRequest(http.MethodGet, "/api/mcp/tools", nil)
	rr := httptest.NewRecorder()
	MCPToolsHandler(config.Config{Paths: config.Paths{Skills: skillsRoot}}, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var body MCPToolsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rr.Body.String())
	}
	if body.Source != "skills" {
		t.Fatalf("source=%q want skills", body.Source)
	}
	if body.Count != 1 || len(body.Tools) != 1 {
		t.Fatalf("count=%d tools=%d want 1", body.Count, len(body.Tools))
	}
	if body.Tools[0].Name != "code/echo" {
		t.Fatalf("tool name=%q want code/echo", body.Tools[0].Name)
	}
	if body.Tools[0].Schema["type"] != "object" {
		t.Fatalf("schema.type=%v want object", body.Tools[0].Schema["type"])
	}
}
