package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
)

func TestDispatchAgentSpawn_RoutesByFlag(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"role":"researcher","prompt":"hello"}`)

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
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

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
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
}

func TestDispatchAgentKill_RoutesByFlag(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"session_id":"sess-1"}`)

	t.Setenv("AGENTCTL_V2_COMMANDS", "")
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
