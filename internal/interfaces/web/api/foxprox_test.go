package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFoxproxHandler_SpawnCLIRequiresCommand(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/foxprox/foxctl-rooms/alpha/spawn-cli", strings.NewReader(`{
		"workspace_id": "ws1",
		"agent_id": "codex-a",
		"adapter": "codex"
	}`))
	rec := httptest.NewRecorder()

	FoxproxHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cmd is required") {
		t.Fatalf("body=%q want cmd validation", rec.Body.String())
	}
}

func TestFoxproxHandler_SendMessageRequiresText(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/foxprox/foxctl-rooms/alpha/messages", strings.NewReader(`{
		"workspace_id": "ws1",
		"source": "actor:web"
	}`))
	rec := httptest.NewRecorder()

	FoxproxHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "text is required") {
		t.Fatalf("body=%q want text validation", rec.Body.String())
	}
}

func TestSplitFoxproxFoxctlRoomSessionPath(t *testing.T) {
	roomID, sessionID := splitFoxproxFoxctlRoomSessionPath("foxctl-rooms/alpha/sessions/sess-1")
	if roomID != "alpha" || sessionID != "sess-1" {
		t.Fatalf("split room=%q session=%q", roomID, sessionID)
	}
	if isFoxproxFoxctlRoomSessionPath("foxctl-rooms/alpha/sessions") {
		t.Fatal("sessions collection path should not parse as session member path")
	}
	if isFoxproxFoxctlRoomSessionPath("foxctl-rooms/alpha/sessions/sess-1/extra") {
		t.Fatal("nested session path should not parse")
	}
}
