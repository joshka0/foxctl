package jido

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/joshka0/foxctl/internal/v2/core/ask"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

func TestRuntimeAdapter_SendMapsAskToSignal(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		signalResp: SignalResponse{
			Status:    "sent",
			MessageID: "msg-123",
		},
	}

	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{
		Client: client,
	})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	msg := ask.Message{
		AskID:          "ask-1",
		RequestID:      "req-1",
		Kind:           "context",
		Question:       "What did you find?",
		ConversationID: "conv-1",
		FromNS:         "caller:one",
		ToNS:           "agent:one",
		TTLMS:          120000,
	}

	mid, err := adapter.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if mid != "msg-123" {
		t.Fatalf("message_id=%q want msg-123", mid)
	}

	if client.signalReq.AgentID != "agent:one" {
		t.Fatalf("agent_id=%q want agent:one", client.signalReq.AgentID)
	}
	if client.signalReq.Mode != SignalModeCall {
		t.Fatalf("mode=%q want %q", client.signalReq.Mode, SignalModeCall)
	}
	if client.signalReq.Signal.Type != DefaultAskSignal {
		t.Fatalf("signal.type=%q want %q", client.signalReq.Signal.Type, DefaultAskSignal)
	}
	if client.signalReq.Signal.Source != DefaultSignalSource {
		t.Fatalf("signal.source=%q want %q", client.signalReq.Signal.Source, DefaultSignalSource)
	}

	var data map[string]any
	if err := json.Unmarshal(client.signalReq.Signal.Data, &data); err != nil {
		t.Fatalf("unmarshal signal data: %v", err)
	}
	if got := data["question"]; got != "What did you find?" {
		t.Fatalf("signal.data.question=%v want %q", got, "What did you find?")
	}
}

func TestRuntimeAdapter_SendFallsBackToAskID(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		signalResp: SignalResponse{
			Status: "sent",
		},
	}
	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{Client: client})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	mid, err := adapter.Send(context.Background(), ask.Message{
		AskID:    "ask-fallback",
		Kind:     "context",
		Question: "hello",
		FromNS:   "caller",
		ToNS:     "agent",
		TTLMS:    1000,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if mid != "ask-fallback" {
		t.Fatalf("message_id=%q want ask-fallback", mid)
	}
}

func TestRuntimeAdapter_SendValidation(t *testing.T) {
	t.Parallel()

	client := &fakeClient{}
	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{Client: client})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	_, err = adapter.Send(context.Background(), ask.Message{
		AskID:  "ask-1",
		Kind:   "context",
		FromNS: "caller",
		ToNS:   "agent",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeAdapter_SendInvokesSignalAckHook(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		signalResp: SignalResponse{
			Status:    "processed",
			MessageID: "msg-ack-1",
		},
	}
	var called atomic.Int32

	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{
		Client: client,
		OnSignalAck: func(_ context.Context, req SignalRequest, resp SignalResponse) error {
			called.Add(1)
			if req.AgentID != "agent:ack" {
				t.Fatalf("req.agent_id=%q want agent:ack", req.AgentID)
			}
			if resp.MessageID != "msg-ack-1" {
				t.Fatalf("resp.message_id=%q want msg-ack-1", resp.MessageID)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	_, err = adapter.Send(context.Background(), ask.Message{
		AskID:    "ask-ack",
		Kind:     "context",
		Question: "ack?",
		FromNS:   "caller:ack",
		ToNS:     "agent:ack",
		TTLMS:    1_000,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("ack hook calls=%d want 1", got)
	}
}

func TestRuntimeAdapter_SendInvokesPrepareSignalHook(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		signalResp: SignalResponse{
			Status:    "sent",
			MessageID: "msg-prep-1",
		},
	}

	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{
		Client: client,
		PrepareSignal: func(_ context.Context, msg ask.Message, req *SignalRequest) error {
			if msg.ToNS != "agent:prep" {
				t.Fatalf("msg.to_ns=%q want agent:prep", msg.ToNS)
			}
			if req.Signal.Metadata == nil {
				req.Signal.Metadata = map[string]any{}
			}
			req.Signal.Metadata["task_continuity"] = map[string]any{
				"task_id":  "T-1",
				"artifact": "sha256:test",
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	_, err = adapter.Send(context.Background(), ask.Message{
		AskID:    "ask-prep",
		Kind:     "context",
		Question: "refresh continuity",
		FromNS:   "caller:prep",
		ToNS:     "agent:prep",
		TTLMS:    1000,
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	meta, ok := client.signalReq.Signal.Metadata["task_continuity"].(map[string]any)
	if !ok {
		t.Fatalf("task_continuity=%T", client.signalReq.Signal.Metadata["task_continuity"])
	}
	if got := meta["task_id"]; got != "T-1" {
		t.Fatalf("task_id=%v want T-1", got)
	}
}

func TestRuntimeAdapter_SpawnChildForwardsRequest(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		spawnChildResp: SignalResponse{
			Status:    "spawned",
			MessageID: "spawn-1",
		},
	}

	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{Client: client})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	resp, err := adapter.SpawnChild(context.Background(), SignalRequest{
		AgentID: "parent-1",
		Signal: Signal{
			Type:   DefaultSpawnChildSignal,
			Source: "/tests",
			Data:   json.RawMessage(`{"tag":"worker-1","child_id":"agent:worker-1","profile":"worker"}`),
		},
	})
	if err != nil {
		t.Fatalf("SpawnChild() error = %v", err)
	}
	if resp.MessageID != "spawn-1" {
		t.Fatalf("message_id=%q want spawn-1", resp.MessageID)
	}
	if client.spawnChildReq.AgentID != "parent-1" {
		t.Fatalf("agent_id=%q want parent-1", client.spawnChildReq.AgentID)
	}
	if client.spawnChildReq.Signal.Type != DefaultSpawnChildSignal {
		t.Fatalf("signal.type=%q want %q", client.spawnChildReq.Signal.Type, DefaultSpawnChildSignal)
	}
}

func TestRuntimeAdapter_ConfigValidation(t *testing.T) {
	t.Parallel()

	_, err := NewRuntimeAdapter(RuntimeAdapterConfig{})
	if err == nil {
		t.Fatal("expected missing client error")
	}
}

func TestRuntimeAdapter_WorkerMapsJidoStateToWorkerRecord(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		stateResp: StateResponse{
			Status: "running",
			State: json.RawMessage(`{
				"foxctl": {
					"status": "completed",
					"run_id": "run-123",
					"session_id": "sess-123",
					"workspace_id": "ws-123",
					"role": "reviewer",
					"parent_agent_id": "agent:parent",
					"stop_reason": "done",
					"pid": "4321",
					"metadata": {"lane":"review"}
				}
			}`),
		},
	}
	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{Client: client})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	record, err := adapter.Worker(context.Background(), coreworker.LookupRequest{AgentID: "agent:child"})
	if err != nil {
		t.Fatalf("Worker() error = %v", err)
	}
	if client.stateReq.AgentID != "agent:child" {
		t.Fatalf("state request agent_id=%q want agent:child", client.stateReq.AgentID)
	}
	if record.WorkerID != "jido:agent:child" {
		t.Fatalf("worker_id=%q want jido:agent:child", record.WorkerID)
	}
	if record.Status != coreworker.StatusCompleted {
		t.Fatalf("status=%q want %q", record.Status, coreworker.StatusCompleted)
	}
	if record.RunID != "run-123" {
		t.Fatalf("run_id=%q want run-123", record.RunID)
	}
	if record.ParentAgentID != "agent:parent" {
		t.Fatalf("parent_agent_id=%q want agent:parent", record.ParentAgentID)
	}
	if record.PID != "4321" {
		t.Fatalf("pid=%q want 4321", record.PID)
	}
	if got := record.Metadata["lane"]; got != "review" {
		t.Fatalf("metadata.lane=%v want review", got)
	}
}

func TestRuntimeAdapter_ChildrenMapsChildRefsToWorkerRecords(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		getChildrenResp: GetChildrenResponse{
			AgentID: "agent:parent",
			Children: map[string]ChildRef{
				"b": {
					Tag:     "child-b",
					AgentID: "agent:b",
					PID:     "222",
					Metadata: map[string]any{
						"run_id":       "run-b",
						"workspace_id": "ws-b",
						"profile":      "worker",
					},
				},
				"a": {
					Tag:     "child-a",
					AgentID: "agent:a",
					PID:     "111",
					Metadata: map[string]any{
						"run_id":       "run-a",
						"workspace_id": "ws-a",
						"role":         "reviewer",
					},
				},
			},
		},
	}
	adapter, err := NewRuntimeAdapter(RuntimeAdapterConfig{Client: client})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter() error = %v", err)
	}

	records, err := adapter.Children(context.Background(), coreworker.ChildrenRequest{ParentAgentID: "agent:parent"})
	if err != nil {
		t.Fatalf("Children() error = %v", err)
	}
	if client.getChildrenReq.AgentID != "agent:parent" {
		t.Fatalf("children request agent_id=%q want agent:parent", client.getChildrenReq.AgentID)
	}
	if len(records) != 2 {
		t.Fatalf("records len=%d want 2", len(records))
	}
	if records[0].AgentID != "agent:a" || records[1].AgentID != "agent:b" {
		t.Fatalf("record order=%q,%q want agent:a,agent:b", records[0].AgentID, records[1].AgentID)
	}
	if records[0].ParentAgentID != "agent:parent" {
		t.Fatalf("parent_agent_id=%q want agent:parent", records[0].ParentAgentID)
	}
	if records[1].Role != "worker" {
		t.Fatalf("role=%q want worker", records[1].Role)
	}
	if records[0].Status != coreworker.StatusUnknown {
		t.Fatalf("status=%q want %q", records[0].Status, coreworker.StatusUnknown)
	}
}

type fakeClient struct {
	signalReq       SignalRequest
	signalResp      SignalResponse
	signalErr       error
	spawnChildReq   SignalRequest
	spawnChildResp  SignalResponse
	spawnChildErr   error
	awaitReq        AwaitRequest
	awaitResp       AwaitResponse
	awaitErr        error
	getChildrenReq  GetChildrenRequest
	getChildrenResp GetChildrenResponse
	getChildrenErr  error
	stateReq        StateRequest
	stateResp       StateResponse
	stateErr        error
}

func (f *fakeClient) Health(context.Context) (HealthResponse, error) {
	return HealthResponse{Status: "ok"}, nil
}

func (f *fakeClient) StartAgent(context.Context, StartAgentRequest) (StartAgentResponse, error) {
	return StartAgentResponse{Status: "started"}, nil
}

func (f *fakeClient) StopAgent(context.Context, StopAgentRequest) (StopAgentResponse, error) {
	return StopAgentResponse{Status: "stopped"}, nil
}

func (f *fakeClient) Signal(_ context.Context, req SignalRequest) (SignalResponse, error) {
	f.signalReq = req
	if f.signalErr != nil {
		return SignalResponse{}, f.signalErr
	}
	return f.signalResp, nil
}

func (f *fakeClient) SpawnChild(_ context.Context, req SignalRequest) (SignalResponse, error) {
	f.spawnChildReq = req
	if f.spawnChildErr != nil {
		return SignalResponse{}, f.spawnChildErr
	}
	return f.spawnChildResp, nil
}

func (f *fakeClient) Await(_ context.Context, req AwaitRequest) (AwaitResponse, error) {
	f.awaitReq = req
	if f.awaitErr != nil {
		return AwaitResponse{}, f.awaitErr
	}
	if f.awaitResp.Status == "" {
		return AwaitResponse{Status: "completed"}, nil
	}
	return f.awaitResp, nil
}

func (f *fakeClient) GetChildren(_ context.Context, req GetChildrenRequest) (GetChildrenResponse, error) {
	f.getChildrenReq = req
	if f.getChildrenErr != nil {
		return GetChildrenResponse{}, f.getChildrenErr
	}
	return f.getChildrenResp, nil
}

func (f *fakeClient) State(_ context.Context, req StateRequest) (StateResponse, error) {
	f.stateReq = req
	if f.stateErr != nil {
		return StateResponse{}, f.stateErr
	}
	return f.stateResp, nil
}
