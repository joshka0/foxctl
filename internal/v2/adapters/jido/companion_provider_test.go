package jido

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

func TestCompanionProvider_GetLayeredContext(t *testing.T) {
	t.Parallel()

	fc := &fakeCompanionClient{
		signalResp: SignalResponse{
			Status: "processed",
			Data: json.RawMessage(`{
				"state": {
					"agentctl": {
						"status": "completed",
						"last_result": {
							"companion_context": {
								"l2": "Durable preferences",
								"l1": "Mid-term summary",
								"l0": "Recent notes",
								"refs": ["memory/a","session/s1"],
								"meta": {"memory_count": 2}
							}
						}
					}
				}
			}`),
		},
	}

	provider, err := NewCompanionProvider(CompanionProviderConfig{
		Client:       fc,
		AgentID:      "agent:bridge",
		DefaultQuery: "orchestration",
	})
	if err != nil {
		t.Fatalf("NewCompanionProvider() error = %v", err)
	}

	got, err := provider.GetLayeredContext(context.Background(), "conv-1", companionReq(1200))
	if err != nil {
		t.Fatalf("GetLayeredContext() error = %v", err)
	}
	if got.L2 != "Durable preferences" {
		t.Fatalf("l2=%q want Durable preferences", got.L2)
	}
	if got.L1 != "Mid-term summary" {
		t.Fatalf("l1=%q want Mid-term summary", got.L1)
	}
	if got.L0 != "Recent notes" {
		t.Fatalf("l0=%q want Recent notes", got.L0)
	}
	if len(got.Refs) != 2 {
		t.Fatalf("refs=%v want 2 refs", got.Refs)
	}
	if fc.signalReq.AgentID != "agent:bridge" {
		t.Fatalf("agent_id=%q want agent:bridge", fc.signalReq.AgentID)
	}
	var payload map[string]any
	if err := json.Unmarshal(fc.signalReq.Signal.Data, &payload); err != nil {
		t.Fatalf("decode signal payload: %v", err)
	}
	if payload["session_id"] != "conv-1" {
		t.Fatalf("session_id=%v want conv-1", payload["session_id"])
	}
	if payload["query"] != "orchestration" {
		t.Fatalf("query=%v want orchestration", payload["query"])
	}
}

func TestCompanionProvider_FailOpenOnSignalError(t *testing.T) {
	t.Parallel()

	provider, err := NewCompanionProvider(CompanionProviderConfig{
		Client: &fakeCompanionClient{
			signalErr: errors.New("bridge down"),
		},
		AgentID: "agent:bridge",
		Strict:  false,
	})
	if err != nil {
		t.Fatalf("NewCompanionProvider() error = %v", err)
	}

	got, err := provider.GetLayeredContext(context.Background(), "conv-1", companionReq(0))
	if err != nil {
		t.Fatalf("GetLayeredContext() error = %v", err)
	}
	if got.L2 != "" || got.L1 != "" || got.L0 != "" || len(got.Refs) != 0 {
		t.Fatalf("expected empty fail-open context, got %+v", got)
	}
}

func TestCompanionProvider_StrictReturnsError(t *testing.T) {
	t.Parallel()

	provider, err := NewCompanionProvider(CompanionProviderConfig{
		Client: &fakeCompanionClient{
			signalResp: SignalResponse{
				Status: "processed",
				Data:   json.RawMessage(`{"state":{"agentctl":{"last_result":{}}}}`),
			},
		},
		AgentID: "agent:bridge",
		Strict:  true,
	})
	if err != nil {
		t.Fatalf("NewCompanionProvider() error = %v", err)
	}

	_, err = provider.GetLayeredContext(context.Background(), "conv-1", companionReq(0))
	if err == nil {
		t.Fatal("expected strict parse error")
	}
}

func companionReq(maxChars int) contextbuilder.CompanionRequest {
	return contextbuilder.CompanionRequest{MaxChars: maxChars}
}

type fakeCompanionClient struct {
	signalReq  SignalRequest
	signalResp SignalResponse
	signalErr  error
}

func (f *fakeCompanionClient) Health(context.Context) (HealthResponse, error) {
	return HealthResponse{Status: "ok"}, nil
}

func (f *fakeCompanionClient) StartAgent(context.Context, StartAgentRequest) (StartAgentResponse, error) {
	return StartAgentResponse{Status: "started"}, nil
}

func (f *fakeCompanionClient) StopAgent(context.Context, StopAgentRequest) (StopAgentResponse, error) {
	return StopAgentResponse{Status: "stopped"}, nil
}

func (f *fakeCompanionClient) Signal(_ context.Context, req SignalRequest) (SignalResponse, error) {
	f.signalReq = req
	if f.signalErr != nil {
		return SignalResponse{}, f.signalErr
	}
	return f.signalResp, nil
}

func (f *fakeCompanionClient) SpawnChild(context.Context, SignalRequest) (SignalResponse, error) {
	return SignalResponse{Status: "spawned"}, nil
}

func (f *fakeCompanionClient) Await(context.Context, AwaitRequest) (AwaitResponse, error) {
	return AwaitResponse{Status: "completed"}, nil
}

func (f *fakeCompanionClient) GetChildren(context.Context, GetChildrenRequest) (GetChildrenResponse, error) {
	return GetChildrenResponse{}, nil
}

func (f *fakeCompanionClient) State(context.Context, StateRequest) (StateResponse, error) {
	return StateResponse{}, nil
}
