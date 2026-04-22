package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestATCPHandler_SpawnCLIRequiresCommand(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/atcp/foxctl-rooms/alpha/spawn-cli", strings.NewReader(`{
		"workspace_id": "ws1",
		"agent_id": "codex-a",
		"adapter": "codex"
	}`))
	rec := httptest.NewRecorder()

	ATCPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cmd is required") {
		t.Fatalf("body=%q want cmd validation", rec.Body.String())
	}
}

func TestATCPHandler_SendMessageRequiresText(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/atcp/foxctl-rooms/alpha/messages", strings.NewReader(`{
		"workspace_id": "ws1",
		"source": "actor:web"
	}`))
	rec := httptest.NewRecorder()

	ATCPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "text is required") {
		t.Fatalf("body=%q want text validation", rec.Body.String())
	}
}
