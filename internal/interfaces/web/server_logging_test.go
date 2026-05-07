package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedactedRequestTargetRedactsTicket(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws/workbench-terminal/att-1?ticket=secret-token&mode=read", nil)

	got := redactedRequestTarget(req)

	if got != "/ws/workbench-terminal/att-1?mode=read&ticket=REDACTED" {
		t.Fatalf("redactedRequestTarget() = %q", got)
	}
}

func TestRedactedRequestTargetLeavesUnticketedRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/workbench/sessions?limit=10", nil)

	got := redactedRequestTarget(req)

	if got != "/api/workbench/sessions?limit=10" {
		t.Fatalf("redactedRequestTarget() = %q", got)
	}
}
