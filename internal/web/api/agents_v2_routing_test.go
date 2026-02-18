package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/rs/zerolog"
)

func TestAgentDetailHandler_SpawnRoutesByFlag(t *testing.T) {
	origV1 := handleAgentSpawnV1Fn
	origV2 := handleAgentSpawnV2Fn
	defer func() {
		handleAgentSpawnV1Fn = origV1
		handleAgentSpawnV2Fn = origV2
	}()

	var v1Calls, v2Calls int
	handleAgentSpawnV1Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger) { v1Calls++ }
	handleAgentSpawnV2Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger) { v2Calls++ }

	handler := AgentDetailHandler(config.Config{}, zerolog.Nop())

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/spawn", strings.NewReader(`{}`))
	handler.ServeHTTP(rec, req)
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}

	v1Calls, v2Calls = 0, 0
	t.Setenv("AGENTCTL_V2_COMMANDS", "spawn")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agents/spawn", strings.NewReader(`{}`))
	handler.ServeHTTP(rec, req)
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("v1/v2 calls = %d/%d, want 0/1", v1Calls, v2Calls)
	}
}

func TestAgentDetailHandler_DaemonRoutesByFlag(t *testing.T) {
	origStartV1 := handleAgentDaemonStartV1Fn
	origStartV2 := handleAgentDaemonStartV2Fn
	origSessionsV1 := handleAgentDaemonSessionsV1Fn
	origSessionsV2 := handleAgentDaemonSessionsV2Fn
	origKillV1 := handleAgentDaemonKillV1Fn
	origKillV2 := handleAgentDaemonKillV2Fn
	defer func() {
		handleAgentDaemonStartV1Fn = origStartV1
		handleAgentDaemonStartV2Fn = origStartV2
		handleAgentDaemonSessionsV1Fn = origSessionsV1
		handleAgentDaemonSessionsV2Fn = origSessionsV2
		handleAgentDaemonKillV1Fn = origKillV1
		handleAgentDaemonKillV2Fn = origKillV2
	}()

	var startV1, startV2, sessionsV1, sessionsV2, killV1, killV2 int
	handleAgentDaemonStartV1Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger, string) { startV1++ }
	handleAgentDaemonStartV2Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger, string) { startV2++ }
	handleAgentDaemonSessionsV1Fn = func(http.ResponseWriter, *http.Request, zerolog.Logger, string) { sessionsV1++ }
	handleAgentDaemonSessionsV2Fn = func(http.ResponseWriter, *http.Request, zerolog.Logger, string) { sessionsV2++ }
	handleAgentDaemonKillV1Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger, string) { killV1++ }
	handleAgentDaemonKillV2Fn = func(http.ResponseWriter, *http.Request, config.Config, zerolog.Logger, string) { killV2++ }

	handler := AgentDetailHandler(config.Config{}, zerolog.Nop())

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/a1/daemon/start", nil)
	handler.ServeHTTP(rec, req)
	if startV1 != 1 || startV2 != 0 {
		t.Fatalf("daemon start v1/v2 calls = %d/%d, want 1/0", startV1, startV2)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/agents/a1/daemon/sessions", nil)
	handler.ServeHTTP(rec, req)
	if sessionsV1 != 1 || sessionsV2 != 0 {
		t.Fatalf("daemon sessions v1/v2 calls = %d/%d, want 1/0", sessionsV1, sessionsV2)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agents/a1/daemon/kill", nil)
	handler.ServeHTTP(rec, req)
	if killV1 != 1 || killV2 != 0 {
		t.Fatalf("daemon kill v1/v2 calls = %d/%d, want 1/0", killV1, killV2)
	}

	startV1, startV2 = 0, 0
	sessionsV1, sessionsV2 = 0, 0
	killV1, killV2 = 0, 0

	t.Setenv("AGENTCTL_V2_COMMANDS", "spawn,list,kill")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agents/a1/daemon/start", nil)
	handler.ServeHTTP(rec, req)
	if startV1 != 0 || startV2 != 1 {
		t.Fatalf("daemon start v1/v2 calls = %d/%d, want 0/1", startV1, startV2)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/agents/a1/daemon/sessions", nil)
	handler.ServeHTTP(rec, req)
	if sessionsV1 != 0 || sessionsV2 != 1 {
		t.Fatalf("daemon sessions v1/v2 calls = %d/%d, want 0/1", sessionsV1, sessionsV2)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/agents/a1/daemon/kill", nil)
	handler.ServeHTTP(rec, req)
	if killV1 != 0 || killV2 != 1 {
		t.Fatalf("daemon kill v1/v2 calls = %d/%d, want 0/1", killV1, killV2)
	}
}

func TestDispatchAgentAPICommand_InvalidEnvFallsBackToV1(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "unknown-command")

	var v1Calls, v2Calls int
	err := dispatchAgentAPICommand(
		httptest.NewRequest(http.MethodGet, "/api/agents", nil).Context(),
		"list",
		"corr-fallback",
		func(context.Context) error { v1Calls++; return nil },
		func(context.Context) error { v2Calls++; return nil },
	)
	if err != nil {
		t.Fatalf("dispatchAgentAPICommand() error = %v", err)
	}
	if v1Calls != 1 || v2Calls != 0 {
		t.Fatalf("v1/v2 calls = %d/%d, want 1/0", v1Calls, v2Calls)
	}
}
