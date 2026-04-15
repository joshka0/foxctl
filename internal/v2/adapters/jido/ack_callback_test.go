package jido

import (
	"encoding/json"
	"testing"
)

func TestSignalAckToCallback_CompletedFromFoxctlState(t *testing.T) {
	t.Parallel()

	req := SignalRequest{
		RequestID: "req-1",
		AgentID:   "agent:1",
		Signal: Signal{
			ID:            "sig-1",
			CorrelationID: "ask-1",
			CausationID:   "cause-1",
			Data:          json.RawMessage(`{"ask_id":"ask-1"}`),
		},
	}
	resp := SignalResponse{
		AgentID:   "agent:1",
		MessageID: "msg-1",
		Status:    "processed",
		Data: json.RawMessage(`{
			"state": {
				"foxctl": {
					"status": "completed",
					"last_result": {
						"tool": "fs/ls",
						"envelope": {"status":"ok"}
					}
				}
			}
		}`),
	}

	cb := SignalAckToCallback(req, resp)
	if cb.AskID != "ask-1" {
		t.Fatalf("ask_id=%q want ask-1", cb.AskID)
	}
	if cb.Status != "completed" {
		t.Fatalf("status=%q want completed", cb.Status)
	}
	if cb.MessageID != "msg-1" {
		t.Fatalf("message_id=%q want msg-1", cb.MessageID)
	}
	if cb.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestSignalAckToCallback_FailedWhenErrorPresent(t *testing.T) {
	t.Parallel()

	req := SignalRequest{
		RequestID: "req-2",
		AgentID:   "agent:2",
		Signal: Signal{
			ID:          "sig-2",
			CausationID: "cause-2",
			Data:        json.RawMessage(`{"ask_id":"ask-2"}`),
		},
	}
	resp := SignalResponse{
		AgentID:  "agent:2",
		SignalID: "sig-2",
		Status:   "processed",
		Data: json.RawMessage(`{
			"state": {
				"foxctl": {
					"status": "failed",
					"last_error": {"message":"tool failed"}
				}
			}
		}`),
	}

	cb := SignalAckToCallback(req, resp)
	if cb.AskID != "ask-2" {
		t.Fatalf("ask_id=%q want ask-2", cb.AskID)
	}
	if cb.Status != "failed" {
		t.Fatalf("status=%q want failed", cb.Status)
	}
	if cb.Error != "tool failed" {
		t.Fatalf("error=%q want tool failed", cb.Error)
	}
}

func TestSignalAckToCallback_IgnoresNilLikeErrors(t *testing.T) {
	t.Parallel()

	req := SignalRequest{
		RequestID: "req-3",
		AgentID:   "agent:3",
		Signal: Signal{
			ID:            "sig-3",
			CorrelationID: "ask-3",
			CausationID:   "cause-3",
			Data:          json.RawMessage(`{"ask_id":"ask-3"}`),
		},
	}
	resp := SignalResponse{
		AgentID:   "agent:3",
		MessageID: "msg-3",
		Status:    "processed",
		Data: json.RawMessage(`{
			"state": {
				"foxctl": {
					"status": "completed",
					"last_error": null,
					"last_result": {
						"tool": "fs/ls",
						"envelope": {"status":"ok"}
					}
				}
			}
		}`),
	}

	cb := SignalAckToCallback(req, resp)
	if cb.Status != "completed" {
		t.Fatalf("status=%q want completed", cb.Status)
	}
	if cb.Error != "" {
		t.Fatalf("error=%q want empty", cb.Error)
	}
}

func TestSignalAckToCallback_ExtractsNestedTransportError(t *testing.T) {
	t.Parallel()

	req := SignalRequest{
		RequestID: "req-4",
		AgentID:   "agent:4",
		Signal: Signal{
			ID:            "sig-4",
			CorrelationID: "ask-4",
			CausationID:   "cause-4",
			Data:          json.RawMessage(`{"ask_id":"ask-4"}`),
		},
	}
	resp := SignalResponse{
		AgentID:   "agent:4",
		MessageID: "msg-4",
		Status:    "processed",
		Data: json.RawMessage(`{
			"state": {
				"foxctl": {
					"status": "failed",
					"last_error": {
						"stage":"transport",
						"transport_error":{"reason":"dial unix /tmp/foxctl.sock: connect: no such file"},
						"cli_error":{"message":"fallback failed"}
					}
				}
			}
		}`),
	}

	cb := SignalAckToCallback(req, resp)
	if cb.Status != "failed" {
		t.Fatalf("status=%q want failed", cb.Status)
	}
	if cb.Error == "" {
		t.Fatal("expected non-empty error")
	}
	if cb.Error != "dial unix /tmp/foxctl.sock: connect: no such file" {
		t.Fatalf("error=%q unexpected", cb.Error)
	}
}

func TestSignalAckToCallback_ExtractsCompanionMetadata(t *testing.T) {
	t.Parallel()

	req := SignalRequest{
		RequestID: "req-5",
		AgentID:   "agent:5",
		Signal: Signal{
			ID:            "sig-5",
			CorrelationID: "ask-5",
			CausationID:   "cause-5",
			Data:          json.RawMessage(`{"ask_id":"ask-5"}`),
		},
	}
	resp := SignalResponse{
		AgentID:   "agent:5",
		MessageID: "msg-5",
		Status:    "processed",
		Data: json.RawMessage(`{
			"state": {
				"foxctl": {
					"status": "completed",
					"last_result": {
						"companion_context": {
							"refs": ["memory/a", "session/s1"],
							"meta": {"memory_count": 1}
						},
						"envelope": {"status":"ok"}
					}
				}
			}
		}`),
	}

	cb := SignalAckToCallback(req, resp)
	if cb.Status != "completed" {
		t.Fatalf("status=%q want completed", cb.Status)
	}
	if cb.Metadata["companion_ref_count"] != 2 {
		t.Fatalf("companion_ref_count=%v want 2", cb.Metadata["companion_ref_count"])
	}
	if _, ok := cb.Metadata["companion_meta"]; !ok {
		t.Fatalf("expected companion_meta in metadata, got %+v", cb.Metadata)
	}
}
