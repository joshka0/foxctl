package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	runtimejobs "github.com/joshka0/foxctl/internal/runtime/jobs"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/jobs"
)

func TestWorkerCSkillManifestHandler(t *testing.T) {
	skillsRoot := t.TempDir()
	writeTestSkillManifest(t, skillsRoot, "code/echo", "code/echo")

	cfg := config.Config{
		Paths: config.Paths{Skills: skillsRoot},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/skills/manifest/code/echo", nil)
	rr := httptest.NewRecorder()

	SkillDetailHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(asString(t, body["name"])); got != "code/echo" {
		t.Fatalf("name=%q want code/echo", got)
	}
	if got := strings.TrimSpace(asString(t, body["command"])); got != "code/echo" {
		t.Fatalf("command=%q want code/echo", got)
	}
}

func TestWorkerCJobsProgressHandler(t *testing.T) {
	jobsRoot := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{Jobs: jobsRoot},
	}

	ctx := context.Background()
	store, err := jobs.Open(ctx, jobsRoot)
	if err != nil {
		t.Fatalf("open jobs store: %v", err)
	}
	defer store.Close()

	job, err := store.SubmitEcho(ctx, "hello")
	if err != nil {
		t.Fatalf("submit echo: %v", err)
	}

	progressPath := filepath.Join(jobsRoot, job.ID, "progress.ndjson")
	progress := strings.Join([]string{
		`{"ts":"2026-01-01T00:00:00Z","message":"queued","percent":10}`,
		`{"ts":"2026-01-01T00:00:01Z","message":"running","percent":60}`,
		`{"ts":"2026-01-01T00:00:02Z","message":"done","percent":100}`,
	}, "\n") + "\n"
	if err := os.WriteFile(progressPath, []byte(progress), 0o644); err != nil {
		t.Fatalf("write progress file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+job.ID+"/progress?limit=2", nil)
	rr := httptest.NewRecorder()

	JobDetailHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeResponseBody(t, rr)
	if got := int(body["count"].(float64)); got != 2 {
		t.Fatalf("count=%d want 2", got)
	}
	events, ok := body["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("events=%v want 2 events", body["events"])
	}
	last, ok := events[1].(map[string]any)
	if !ok {
		t.Fatalf("last event type=%T", events[1])
	}
	if got := strings.TrimSpace(asString(t, last["message"])); got != "done" {
		t.Fatalf("last message=%q want done", got)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/jobs/"+job.ID+"/events?limit=1", nil)
	eventsRR := httptest.NewRecorder()
	JobDetailHandler(cfg, zerolog.Nop()).ServeHTTP(eventsRR, eventsReq)
	if eventsRR.Code != http.StatusOK {
		t.Fatalf("events status=%d want %d body=%s", eventsRR.Code, http.StatusOK, eventsRR.Body.String())
	}
	if got := eventsRR.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("events content-type=%q want text/event-stream", got)
	}
	if body := eventsRR.Body.String(); !strings.Contains(body, "event: job.progress") || !strings.Contains(body, "event: job.state") {
		t.Fatalf("events body=%q want progress and state events", body)
	}

	waitReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/wait?timeout_ms=1000&poll_ms=10", nil)
	waitRR := httptest.NewRecorder()
	JobDetailHandler(cfg, zerolog.Nop()).ServeHTTP(waitRR, waitReq)
	if waitRR.Code != http.StatusOK {
		t.Fatalf("wait status=%d want %d body=%s", waitRR.Code, http.StatusOK, waitRR.Body.String())
	}
	waitBody := decodeResponseBody(t, waitRR)
	if got := strings.TrimSpace(asString(t, waitBody["state"])); got != "ok" {
		t.Fatalf("wait state=%q want ok", got)
	}
}

func TestWorkerCJobsCancelHandler(t *testing.T) {
	jobsRoot := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{Jobs: jobsRoot},
	}

	ctx := context.Background()
	store, err := runtimejobs.OpenSkillStore(ctx, jobsRoot)
	if err != nil {
		t.Fatalf("open jobs store: %v", err)
	}
	job, err := store.PrepareSkillJob(ctx, "code/echo", []byte(`{"q":"hello"}`))
	if err != nil {
		_ = store.Close()
		t.Fatalf("prepare skill job: %v", err)
	}
	_ = store.Close()

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/jobs/"+job.ID+"/cancel", nil)
	cancelRR := httptest.NewRecorder()
	JobDetailHandler(cfg, zerolog.Nop()).ServeHTTP(cancelRR, cancelReq)
	if cancelRR.Code != http.StatusOK {
		t.Fatalf("cancel status=%d want %d body=%s", cancelRR.Code, http.StatusOK, cancelRR.Body.String())
	}
	body := decodeResponseBody(t, cancelRR)
	if got := strings.TrimSpace(asString(t, body["status"])); got != "canceled" {
		t.Fatalf("status=%q want canceled", got)
	}
}

func TestWorkerCCASHandler_ListReadPinUnpin(t *testing.T) {
	casRoot := t.TempDir()
	cfg := config.Config{
		Paths: config.Paths{CAS: casRoot},
	}

	store, err := cas.NewStore(casRoot)
	if err != nil {
		t.Fatalf("new cas store: %v", err)
	}
	obj, err := store.Put(context.Background(), strings.NewReader("hello world"), "text/plain", []string{"test"})
	if err != nil {
		t.Fatalf("put cas object: %v", err)
	}
	_ = store.Close()

	handler := CASHandler(cfg, zerolog.Nop())

	listReq := httptest.NewRequest(http.MethodGet, "/api/cas", nil)
	listRR := httptest.NewRecorder()
	handler.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	listBody := decodeResponseBody(t, listRR)
	if got := int(listBody["count"].(float64)); got != 1 {
		t.Fatalf("list count=%d want 1", got)
	}

	readReq := httptest.NewRequest(http.MethodGet, "/api/cas/"+obj.Digest+"/read?page=1&page_size=5", nil)
	readRR := httptest.NewRecorder()
	handler.ServeHTTP(readRR, readReq)
	if readRR.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", readRR.Code, readRR.Body.String())
	}
	readBody := decodeResponseBody(t, readRR)
	if got := asString(t, readBody["content"]); got != "hello" {
		t.Fatalf("read content=%q want hello", got)
	}

	pinReq := httptest.NewRequest(http.MethodPost, "/api/cas/"+obj.Digest+"/pin", nil)
	pinRR := httptest.NewRecorder()
	handler.ServeHTTP(pinRR, pinReq)
	if pinRR.Code != http.StatusOK {
		t.Fatalf("pin status=%d body=%s", pinRR.Code, pinRR.Body.String())
	}

	pinnedReq := httptest.NewRequest(http.MethodGet, "/api/cas?pinned=true", nil)
	pinnedRR := httptest.NewRecorder()
	handler.ServeHTTP(pinnedRR, pinnedReq)
	if pinnedRR.Code != http.StatusOK {
		t.Fatalf("pinned list status=%d body=%s", pinnedRR.Code, pinnedRR.Body.String())
	}
	pinnedBody := decodeResponseBody(t, pinnedRR)
	if got := int(pinnedBody["count"].(float64)); got != 1 {
		t.Fatalf("pinned count=%d want 1", got)
	}

	unpinReq := httptest.NewRequest(http.MethodPost, "/api/cas/"+obj.Digest+"/unpin", nil)
	unpinRR := httptest.NewRecorder()
	handler.ServeHTTP(unpinRR, unpinReq)
	if unpinRR.Code != http.StatusOK {
		t.Fatalf("unpin status=%d body=%s", unpinRR.Code, unpinRR.Body.String())
	}
}

func TestWorkerCMCPHandlers(t *testing.T) {
	skillsRoot := t.TempDir()
	writeTestSkillManifest(t, skillsRoot, "code/echo", "code/echo")

	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.Config{
		Paths: config.Paths{Skills: skillsRoot},
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/mcp/status", nil)
	statusRR := httptest.NewRecorder()
	MCPStatusHandler(cfg, zerolog.Nop()).ServeHTTP(statusRR, statusReq)
	if statusRR.Code != http.StatusOK {
		t.Fatalf("status handler code=%d body=%s", statusRR.Code, statusRR.Body.String())
	}
	statusBody := decodeResponseBody(t, statusRR)
	daemon, ok := statusBody["daemon"].(map[string]any)
	if !ok {
		t.Fatalf("daemon payload type=%T", statusBody["daemon"])
	}
	if running, _ := daemon["running"].(bool); running {
		t.Fatalf("expected daemon running=false without pid file, got %v", daemon["running"])
	}
	if got := strings.TrimSpace(asString(t, daemon["addr"])); got != mcpDefaultAddr {
		t.Fatalf("daemon addr=%q want %q", got, mcpDefaultAddr)
	}

	toolsReq := httptest.NewRequest(http.MethodGet, "/api/mcp/tools", nil)
	toolsRR := httptest.NewRecorder()
	MCPToolsHandler(cfg, zerolog.Nop()).ServeHTTP(toolsRR, toolsReq)
	if toolsRR.Code != http.StatusOK {
		t.Fatalf("tools handler code=%d body=%s", toolsRR.Code, toolsRR.Body.String())
	}
	toolsBody := decodeResponseBody(t, toolsRR)
	if got := int(toolsBody["count"].(float64)); got != 1 {
		t.Fatalf("tool count=%d want 1", got)
	}
}

func writeTestSkillManifest(t *testing.T, root, name, command string) {
	t.Helper()
	skillDir := filepath.Join(root, strings.ReplaceAll(name, "/", string(filepath.Separator)))
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}

	content := fmt.Sprintf(`apiVersion: foxctl/v1
kind: Skill
metadata:
  name: %s
  version: "1.0.0"
  description: test skill
distribution:
  type: exec
  exec:
    entry: ./bin/test-skill
io:
  format: envelope
  inline_output_kb: 32
signature:
  command: %s
  parameters:
    - name: q
      type: string
      required: true
      description: query
capabilities:
  network: none
  filesystem: []
  pure: true
memory:
  recommend: false
  default_ttl: 24h
`, name, command)

	if err := os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill.yaml: %v", err)
	}
}

func asString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("value type=%T want string", v)
	}
	return s
}
