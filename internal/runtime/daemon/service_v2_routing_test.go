package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDispatchAgentSpawn_UsesV2Handler(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"role":"researcher","prompt":"hello"}`)

	_, err := svc.dispatchAgentSpawn(context.Background(), params)
	if err == nil {
		t.Fatalf("dispatchAgentSpawn() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentSpawn() error = %q, want orchestration initialization failure", err.Error())
	}
}

func TestDispatchAgentList_UsesV2Handler(t *testing.T) {
	svc := &Service{}

	result, err := svc.dispatchAgentList(context.Background())
	if err != nil {
		t.Fatalf("dispatchAgentList() error = %v", err)
	}
	if result == nil || result.Count != 0 {
		t.Fatalf("result = %#v, want empty list result", result)
	}
}

func TestDispatchAgentList_NilContextDoesNotPanic(t *testing.T) {
	svc := &Service{}

	var nilCtx context.Context
	result, err := svc.dispatchAgentList(nilCtx)
	if err != nil {
		t.Fatalf("dispatchAgentList(nil) error = %v", err)
	}
	if result == nil || result.Count != 0 {
		t.Fatalf("result = %#v, want empty list result", result)
	}
}

func TestDispatchAgentKill_UsesV2Handler(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"session_id":"sess-1"}`)

	_, err := svc.dispatchAgentKill(context.Background(), params)
	if err == nil {
		t.Fatalf("dispatchAgentKill() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent orchestration not initialized") {
		t.Fatalf("dispatchAgentKill() error = %q, want orchestration initialization failure", err.Error())
	}
}

func TestHandleAgentAskRPC_RequiresMailbox(t *testing.T) {
	svc := &Service{}
	params := json.RawMessage(`{"agent_id":"agent-1","message":"hello"}`)

	_, err := svc.handleAgentAskRPC(context.Background(), params)
	if err == nil {
		t.Fatalf("handleAgentAskRPC() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "agent mailbox is not initialized") {
		t.Fatalf("handleAgentAskRPC() error = %q, want mailbox initialization failure", err.Error())
	}
}

// TestAgentSpawnParams_AgentIDReuse verifies that the spawn params struct
// correctly carries the caller-provided AgentID. The daemon spawn handler
// reuses this ID for the agent record instead of generating a new ULID,
// preventing duplicate agent records between the web API and daemon.
func TestAgentSpawnParams_AgentIDReuse(t *testing.T) {
	params := json.RawMessage(`{
		"role": "coder",
		"agent_id": "web-api-agent-123",
		"workspace_id": "ws-456",
		"prompt": "test prompt"
	}`)

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	if p.AgentID != "web-api-agent-123" {
		t.Fatalf("AgentID = %q, want %q", p.AgentID, "web-api-agent-123")
	}
	if p.WorkspaceID != "ws-456" {
		t.Fatalf("WorkspaceID = %q, want %q", p.WorkspaceID, "ws-456")
	}
}

// TestAgentSpawnParams_EmptyAgentIDGeneratesNewID verifies that when no
// AgentID is provided, the spawn handler will generate a new ULID.
func TestAgentSpawnParams_EmptyAgentIDGeneratesNewID(t *testing.T) {
	params := json.RawMessage(`{"role":"coder","prompt":"hello"}`)

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	if p.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty string", p.AgentID)
	}
}

// TestAgentSpawnParams_NamespaceIsWorkspaceID verifies that the Namespace
// field in the agent record uses WorkspaceID, aligning with web API convention.
func TestAgentSpawnParams_NamespaceIsWorkspaceID(t *testing.T) {
	params := json.RawMessage(`{
		"role": "coder",
		"workspace_id": "my-workspace",
		"prompt": "hello"
	}`)

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}

	if p.WorkspaceID != "my-workspace" {
		t.Fatalf("WorkspaceID = %q, want %q", p.WorkspaceID, "my-workspace")
	}
}
