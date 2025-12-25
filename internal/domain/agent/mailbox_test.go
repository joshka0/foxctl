package agent

import (
	"encoding/json"
	"testing"
)

func TestMessageType_Constants(t *testing.T) {
	tests := []struct {
		msgType MessageType
		want    string
	}{
		{MessageTypeAsk, "agent.ask"},
		{MessageTypeReply, "agent.reply"},
		{MessageTypeCmd, "agent.cmd"},
		{MessageTypeEvent, "agent.event"},
	}

	for _, tt := range tests {
		t.Run(string(tt.msgType), func(t *testing.T) {
			if string(tt.msgType) != tt.want {
				t.Errorf("MessageType = %q, want %q", tt.msgType, tt.want)
			}
		})
	}
}

func TestMessage_JSONSerialization(t *testing.T) {
	msg := Message{
		ID:        "msg-123",
		FromNS:    "ns:sender",
		ToNS:      "ns:receiver",
		Type:      MessageTypeAsk,
		TTLMS:     60000,
		Headers:   map[string]string{"X-Priority": "high", "X-Retry": "3"},
		Payload:   json.RawMessage(`{"ask_id":"ask-456","question":"What is the answer?"}`),
		VisibleAt: 1703520000,
		Attempt:   1,
		Timestamp: 1703519900,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got Message
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ID != msg.ID {
		t.Errorf("ID = %q, want %q", got.ID, msg.ID)
	}
	if got.FromNS != msg.FromNS {
		t.Errorf("FromNS = %q, want %q", got.FromNS, msg.FromNS)
	}
	if got.ToNS != msg.ToNS {
		t.Errorf("ToNS = %q, want %q", got.ToNS, msg.ToNS)
	}
	if got.Type != msg.Type {
		t.Errorf("Type = %v, want %v", got.Type, msg.Type)
	}
	if got.TTLMS != msg.TTLMS {
		t.Errorf("TTLMS = %d, want %d", got.TTLMS, msg.TTLMS)
	}
	if len(got.Headers) != len(msg.Headers) {
		t.Errorf("Headers length = %d, want %d", len(got.Headers), len(msg.Headers))
	}
	if got.Attempt != msg.Attempt {
		t.Errorf("Attempt = %d, want %d", got.Attempt, msg.Attempt)
	}
}

func TestAskData_JSONSerialization(t *testing.T) {
	ask := AskData{
		AskID:     "ask-123",
		Kind:      "context",
		Question:  "What is the project structure?",
		NeedsByMS: 30000,
		Context:   map[string]any{"workspace": "/home/user/project", "depth": 2},
	}

	data, err := json.Marshal(ask)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got AskData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.AskID != ask.AskID {
		t.Errorf("AskID = %q, want %q", got.AskID, ask.AskID)
	}
	if got.Kind != ask.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, ask.Kind)
	}
	if got.Question != ask.Question {
		t.Errorf("Question = %q, want %q", got.Question, ask.Question)
	}
	if got.NeedsByMS != ask.NeedsByMS {
		t.Errorf("NeedsByMS = %d, want %d", got.NeedsByMS, ask.NeedsByMS)
	}
}

func TestReplyData_JSONSerialization(t *testing.T) {
	reply := ReplyData{
		AskID:  "ask-123",
		Answer: map[string]any{"result": "success", "count": 42, "items": []string{"a", "b"}},
	}

	data, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ReplyData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.AskID != reply.AskID {
		t.Errorf("AskID = %q, want %q", got.AskID, reply.AskID)
	}
	if got.Answer == nil {
		t.Error("Answer should not be nil")
	}
}

func TestCmdData_JSONSerialization(t *testing.T) {
	cmd := CmdData{
		CmdID:  "cmd-456",
		Action: "execute",
		Skill:  "fs/read",
		Args:   map[string]any{"path": "/tmp/test.txt", "encoding": "utf-8"},
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got CmdData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.CmdID != cmd.CmdID {
		t.Errorf("CmdID = %q, want %q", got.CmdID, cmd.CmdID)
	}
	if got.Action != cmd.Action {
		t.Errorf("Action = %q, want %q", got.Action, cmd.Action)
	}
	if got.Skill != cmd.Skill {
		t.Errorf("Skill = %q, want %q", got.Skill, cmd.Skill)
	}
}

func TestEventData_JSONSerialization(t *testing.T) {
	event := EventData{
		EventID:   "evt-789",
		Kind:      "heartbeat",
		JobCount:  5,
		CacheHits: 120,
		Custom:    map[string]any{"version": "1.0.0", "uptime_seconds": 3600},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got EventData
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.EventID != event.EventID {
		t.Errorf("EventID = %q, want %q", got.EventID, event.EventID)
	}
	if got.Kind != event.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, event.Kind)
	}
	if got.JobCount != event.JobCount {
		t.Errorf("JobCount = %d, want %d", got.JobCount, event.JobCount)
	}
	if got.CacheHits != event.CacheHits {
		t.Errorf("CacheHits = %d, want %d", got.CacheHits, event.CacheHits)
	}
}

func TestAskData_AllKinds(t *testing.T) {
	kinds := []string{"context", "secret", "approval", "toolhint", "other"}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			ask := AskData{
				AskID:    "ask-test",
				Kind:     kind,
				Question: "Test question",
			}

			data, err := json.Marshal(ask)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got AskData
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Kind != kind {
				t.Errorf("Kind = %q, want %q", got.Kind, kind)
			}
		})
	}
}

func TestEventData_AllKinds(t *testing.T) {
	kinds := []string{"heartbeat", "liveness-failed", "job-complete", "error"}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			event := EventData{
				EventID: "evt-test",
				Kind:    kind,
			}

			data, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got EventData
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Kind != kind {
				t.Errorf("Kind = %q, want %q", got.Kind, kind)
			}
		})
	}
}
