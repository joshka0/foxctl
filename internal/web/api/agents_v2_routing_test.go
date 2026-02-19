package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestDispatchAgentAPICommand_ShadowRunsForNonMutatingCommand(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "list")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	var v1Calls atomic.Int32
	shadowDone := make(chan struct{}, 1)
	err := dispatchAgentAPICommand(
		context.Background(),
		"list",
		"corr-api-shadow-list",
		func(context.Context) error {
			v1Calls.Add(1)
			return nil
		},
		func(context.Context) error {
			select {
			case shadowDone <- struct{}{}:
			default:
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentAPICommand() error = %v", err)
	}
	if v1Calls.Load() != 1 {
		t.Fatalf("v1 calls=%d want 1", v1Calls.Load())
	}

	select {
	case <-shadowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for api shadow call")
	}
}

func TestDispatchAgentAPICommand_ShadowMutatingRequiresOptIn(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "kill")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	var blocked atomic.Int32
	err := dispatchAgentAPICommand(
		context.Background(),
		"kill",
		"corr-api-shadow-kill-blocked",
		func(context.Context) error { return nil },
		func(context.Context) error {
			blocked.Add(1)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentAPICommand() blocked case error = %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if blocked.Load() != 0 {
		t.Fatalf("blocked shadow calls=%d want 0", blocked.Load())
	}

	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "1")
	allowedDone := make(chan struct{}, 1)
	err = dispatchAgentAPICommand(
		context.Background(),
		"kill",
		"corr-api-shadow-kill-allowed",
		func(context.Context) error { return nil },
		func(context.Context) error {
			select {
			case allowedDone <- struct{}{}:
			default:
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("dispatchAgentAPICommand() allowed case error = %v", err)
	}
	select {
	case <-allowedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for allowed api mutating shadow call")
	}
}

func TestDispatchAgentAPICommand_FreezeBlocksV1Path(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	t.Setenv("AGENTCTL_V2_FREEZE_V1_COMMANDS", "list")

	var v1Calls, v2Calls atomic.Int32
	err := dispatchAgentAPICommand(
		context.Background(),
		"list",
		"corr-api-freeze-list",
		func(context.Context) error { v1Calls.Add(1); return nil },
		func(context.Context) error { v2Calls.Add(1); return nil },
	)
	if err == nil {
		t.Fatal("dispatchAgentAPICommand() error = nil, want freeze error")
	}
	if !strings.Contains(err.Error(), "v1 path frozen for command list") {
		t.Fatalf("unexpected freeze error: %v", err)
	}
	if v1Calls.Load() != 0 || v2Calls.Load() != 0 {
		t.Fatalf("calls v1/v2 = %d/%d want 0/0", v1Calls.Load(), v2Calls.Load())
	}
}
