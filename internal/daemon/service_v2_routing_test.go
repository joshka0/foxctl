package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	v2daemonports "github.com/jkatigb/agentctl/internal/v2/ports/daemon"
)

func TestDispatchAgentSpawn_RoutesByFlag(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"role":"researcher","prompt":"hello"}`)

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	_, decision, err := svc.dispatchAgentSpawn(context.Background(), "req-spawn-v1", params)
	if err == nil {
		t.Fatalf("dispatchAgentSpawn() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentSpawn() error = %q, want orchestration initialization failure", err.Error())
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV1)
	}

	t.Setenv("AGENTCTL_V2_COMMANDS", "spawn")
	_, decision, err = svc.dispatchAgentSpawn(context.Background(), "req-spawn-v2", params)
	if err == nil {
		t.Fatalf("dispatchAgentSpawn() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentSpawn() error = %q, want orchestration initialization failure", err.Error())
	}
	if decision != v2ports.DecisionV2 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV2)
	}
}

func TestDispatchAgentList_RoutesByFlag(t *testing.T) {
	svc := &Service{}

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	result, decision, err := svc.dispatchAgentList(context.Background(), "req-list-v1")
	if err != nil {
		t.Fatalf("dispatchAgentList() error = %v", err)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV1)
	}
	if result == nil || result.Count != 0 {
		t.Fatalf("result = %#v, want empty list result", result)
	}

	t.Setenv("AGENTCTL_V2_COMMANDS", "list")
	result, decision, err = svc.dispatchAgentList(context.Background(), "req-list-v2")
	if err != nil {
		t.Fatalf("dispatchAgentList() error = %v", err)
	}
	if decision != v2ports.DecisionV2 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV2)
	}
	if result == nil || result.Count != 0 {
		t.Fatalf("result = %#v, want empty list result", result)
	}

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
	result, decision, err = svc.dispatchAgentList(context.Background(), "req-list-default-env")
	if err != nil {
		t.Fatalf("dispatchAgentList() default-env error = %v", err)
	}
	if decision != v2ports.DecisionV2 {
		t.Fatalf("default-env decision = %q, want %q", decision, v2ports.DecisionV2)
	}
	if result == nil || result.Count != 0 {
		t.Fatalf("default-env result = %#v, want empty list result", result)
	}
}

func TestDispatchAgentKill_RoutesByFlag(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"session_id":"sess-1"}`)

	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	_, decision, err := svc.dispatchAgentKill(context.Background(), "req-kill-v1", params)
	if err == nil {
		t.Fatalf("dispatchAgentKill() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentKill() error = %q, want orchestration initialization failure", err.Error())
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV1)
	}

	t.Setenv("AGENTCTL_V2_COMMANDS", "kill")
	_, decision, err = svc.dispatchAgentKill(context.Background(), "req-kill-v2", params)
	if err == nil {
		t.Fatalf("dispatchAgentKill() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentKill() error = %q, want orchestration initialization failure", err.Error())
	}
	if decision != v2ports.DecisionV2 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV2)
	}
}

func TestDispatchAgentSpawn_InvalidEnvFallsBackToV1(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"role":"researcher","prompt":"hello"}`)

	t.Setenv("AGENTCTL_V2_COMMANDS", "unknown-command")
	_, decision, err := svc.dispatchAgentSpawn(context.Background(), "req-spawn-fallback", params)
	if err == nil {
		t.Fatalf("dispatchAgentSpawn() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentSpawn() error = %q, want orchestration initialization failure", err.Error())
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision = %q, want %q", decision, v2ports.DecisionV1)
	}
}

func TestDaemonMethodRouter_ShadowRunsForNonMutatingCommand(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "list")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	router := daemonMethodRouter()
	var v1Calls atomic.Int32
	shadowDone := make(chan struct{}, 1)

	out, decision, err := v2daemonports.DispatchMethod(context.Background(), router, "agent.list", "corr-shadow-list",
		func(context.Context) (string, error) {
			v1Calls.Add(1)
			return "ok", nil
		},
		func(context.Context) (string, error) {
			select {
			case shadowDone <- struct{}{}:
			default:
			}
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("DispatchMethod() error = %v", err)
	}
	if out != "ok" || decision != v2ports.DecisionV1 {
		t.Fatalf("out/decision = %q/%q want ok/v1", out, decision)
	}
	if v1Calls.Load() != 1 {
		t.Fatalf("v1 calls=%d want 1", v1Calls.Load())
	}
	select {
	case <-shadowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for daemon shadow call")
	}
}

func TestDaemonMethodRouter_ShadowMutatingRequiresOptIn(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_SHADOW_COMMANDS", "kill")
	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "")

	router := daemonMethodRouter()
	var blockedCalls atomic.Int32
	_, decision, err := v2daemonports.DispatchMethod(context.Background(), router, "agent.kill", "corr-shadow-kill-blocked",
		func(context.Context) (string, error) { return "ok", nil },
		func(context.Context) (string, error) {
			blockedCalls.Add(1)
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("DispatchMethod() blocked case error = %v", err)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}
	time.Sleep(200 * time.Millisecond)
	if blockedCalls.Load() != 0 {
		t.Fatalf("blocked shadow calls=%d want 0", blockedCalls.Load())
	}

	t.Setenv("AGENTCTL_V2_SHADOW_MUTATING", "true")
	router = daemonMethodRouter()
	allowedDone := make(chan struct{}, 1)
	_, _, err = v2daemonports.DispatchMethod(context.Background(), router, "agent.kill", "corr-shadow-kill-allowed",
		func(context.Context) (string, error) { return "ok", nil },
		func(context.Context) (string, error) {
			select {
			case allowedDone <- struct{}{}:
			default:
			}
			return "ok", nil
		},
	)
	if err != nil {
		t.Fatalf("DispatchMethod() allowed case error = %v", err)
	}
	select {
	case <-allowedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for allowed daemon mutating shadow call")
	}
}

func TestDaemonMethodRouter_FreezeBlocksV1Path(t *testing.T) {
	t.Setenv("AGENTCTL_V2_COMMANDS", "none")
	t.Setenv("AGENTCTL_V2_FREEZE_V1_COMMANDS", "list")

	router := daemonMethodRouter()
	var v1Calls atomic.Int32
	var v2Calls atomic.Int32
	_, decision, err := v2daemonports.DispatchMethod(context.Background(), router, "agent.list", "corr-freeze-list",
		func(context.Context) (string, error) {
			v1Calls.Add(1)
			return "ok", nil
		},
		func(context.Context) (string, error) {
			v2Calls.Add(1)
			return "ok", nil
		},
	)
	if err == nil {
		t.Fatal("DispatchMethod() error = nil, want freeze error")
	}
	if !strings.Contains(err.Error(), "v1 path frozen for command list") {
		t.Fatalf("unexpected freeze error: %v", err)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}
	if v1Calls.Load() != 0 || v2Calls.Load() != 0 {
		t.Fatalf("calls v1/v2 = %d/%d want 0/0", v1Calls.Load(), v2Calls.Load())
	}
}
