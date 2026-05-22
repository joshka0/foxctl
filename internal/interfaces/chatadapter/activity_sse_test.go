package chatadapter

import (
	"strings"
	"testing"
)

func TestDecodeActivitySSEMessage_DecodesPrefixedActivity(t *testing.T) {
	raw := []byte(`data: {"type":"activity","data":{"operation":"agent.spawn","status":"ok","session_id":"sess-1"}}` + "\n\n")

	got, ok, err := DecodeActivitySSEMessage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected activity event")
	}
	if got.Operation != "agent.spawn" {
		t.Fatalf("expected operation agent.spawn, got %q", got.Operation)
	}
	if got.Status != "ok" {
		t.Fatalf("expected status ok, got %q", got.Status)
	}
	if got.SessionID != "sess-1" {
		t.Fatalf("expected session id sess-1, got %q", got.SessionID)
	}
}

func TestDecodeActivitySSEMessage_DecodesRawJSONActivity(t *testing.T) {
	raw := []byte(`{"type":"activity","data":{"operation":"agent.complete","status":"ok","agent_id":"agent-1"}}`)

	got, ok, err := DecodeActivitySSEMessage(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected activity event")
	}
	if got.Operation != "agent.complete" {
		t.Fatalf("expected operation agent.complete, got %q", got.Operation)
	}
	if got.AgentID != "agent-1" {
		t.Fatalf("expected agent id agent-1, got %q", got.AgentID)
	}
}

func TestDecodeActivitySSEMessage_IgnoresNonActivityEvent(t *testing.T) {
	_, ok, err := DecodeActivitySSEMessage([]byte(`data: {"type":"heartbeat","data":{"alive":true}}` + "\n\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected non-activity event to be ignored")
	}
}

func TestDecodeActivitySSEMessage_ReportsOuterEventError(t *testing.T) {
	_, ok, err := DecodeActivitySSEMessage([]byte(`data: {`))
	if ok {
		t.Fatal("expected malformed event not to decode")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if got := ActivitySSEDecodeStage(err); got != activitySSEDecodeStageEvent {
		t.Fatalf("expected stage %q, got %q", activitySSEDecodeStageEvent, got)
	}
}

func TestDecodeActivitySSEMessage_ReportsActivityPayloadError(t *testing.T) {
	raw := []byte(`data: {"type":"activity","data":{"operation":123}}` + "\n\n")

	_, ok, err := DecodeActivitySSEMessage(raw)
	if ok {
		t.Fatal("expected malformed activity not to decode")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if got := ActivitySSEDecodeStage(err); got != activitySSEDecodeStageActivity {
		t.Fatalf("expected stage %q, got %q", activitySSEDecodeStageActivity, got)
	}
	if !strings.Contains(err.Error(), activitySSEDecodeStageActivity) {
		t.Fatalf("expected error to include stage, got %q", err.Error())
	}
}
