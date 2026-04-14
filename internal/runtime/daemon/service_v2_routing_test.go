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
