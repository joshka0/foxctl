package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/actor"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestNewRegistry(t *testing.T) {
	reg := NewRegistry()
	if reg == nil {
		t.Fatal("registry is nil")
	}

	// Check default handlers are registered
	if reg.Get("coder") == nil {
		t.Error("coder handler not registered")
	}
	if reg.Get("planner") == nil {
		t.Error("planner handler not registered")
	}
	if reg.Get("reviewer") == nil {
		t.Error("reviewer handler not registered")
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry()

	// Test existing handlers
	if h := reg.Get("coder"); h == nil || h.Role() != "coder" {
		t.Error("coder handler not found or wrong role")
	}
	if h := reg.Get("planner"); h == nil || h.Role() != "planner" {
		t.Error("planner handler not found or wrong role")
	}
	if h := reg.Get("reviewer"); h == nil || h.Role() != "reviewer" {
		t.Error("reviewer handler not found or wrong role")
	}

	// Test non-existent handler
	if h := reg.Get("unknown"); h != nil {
		t.Error("expected nil for unknown handler")
	}
}

func TestParseAskData(t *testing.T) {
	askData := agentdomain.AskData{
		AskID:    "ask-123",
		Kind:     "context",
		Question: "What is the meaning of life?",
	}
	env := envelope.OK("agent.ask", askData)
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	parsed, err := parseAskData(body)
	if err != nil {
		t.Fatalf("parseAskData() error = %v", err)
	}
	if parsed.AskID != "ask-123" {
		t.Errorf("AskID = %q, want ask-123", parsed.AskID)
	}
	if parsed.Kind != "context" {
		t.Errorf("Kind = %q, want context", parsed.Kind)
	}
}

func TestParseCmdData(t *testing.T) {
	cmdData := agentdomain.CmdData{
		CmdID:  "cmd-456",
		Action: "run_turn",
		Args:   map[string]any{"prompt": "Do something"},
	}
	env := envelope.OK("agent.cmd", cmdData)
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	parsed, err := parseCmdData(body)
	if err != nil {
		t.Fatalf("parseCmdData() error = %v", err)
	}
	if parsed.CmdID != "cmd-456" {
		t.Errorf("CmdID = %q, want cmd-456", parsed.CmdID)
	}
	if parsed.Action != "run_turn" {
		t.Errorf("Action = %q, want run_turn", parsed.Action)
	}
}

func TestParseEventData(t *testing.T) {
	eventData := agentdomain.EventData{
		EventID:  "event-789",
		Kind:     "heartbeat",
		JobCount: 5,
	}
	env := envelope.OK("agent.event", eventData)
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	parsed, err := parseEventData(body)
	if err != nil {
		t.Fatalf("parseEventData() error = %v", err)
	}
	if parsed.EventID != "event-789" {
		t.Errorf("EventID = %q, want event-789", parsed.EventID)
	}
	if parsed.Kind != "heartbeat" {
		t.Errorf("Kind = %q, want heartbeat", parsed.Kind)
	}
}

func TestBuildReplyMessage(t *testing.T) {
	answer := map[string]any{
		"response": "42",
		"role":     "coder",
	}

	msg, err := buildReplyMessage("ask-123", answer)
	if err != nil {
		t.Fatalf("buildReplyMessage() error = %v", err)
	}
	if msg == nil {
		t.Fatal("message is nil")
	}
	if msg.Subject != "agent.reply" {
		t.Errorf("Subject = %q, want agent.reply", msg.Subject)
	}

	// Verify the reply payload
	var env struct {
		Data agentdomain.ReplyData `json:"data"`
	}
	if err := json.Unmarshal(msg.Body, &env); err != nil {
		t.Fatalf("unmarshal reply: %v", err)
	}
	if env.Data.AskID != "ask-123" {
		t.Errorf("AskID = %q, want ask-123", env.Data.AskID)
	}
}

func TestExtractAgentResult(t *testing.T) {
	tests := []struct {
		name   string
		result map[string]any
		want   string
	}{
		{
			name:   "result field",
			result: map[string]any{"result": "The answer is 42"},
			want:   "The answer is 42",
		},
		{
			name:   "answer field",
			result: map[string]any{"answer": "Yes"},
			want:   "Yes",
		},
		{
			name:   "output field",
			result: map[string]any{"output": "Output value"},
			want:   "Output value",
		},
		{
			name:   "response field",
			result: map[string]any{"response": "Response value"},
			want:   "Response value",
		},
		{
			name:   "thought field",
			result: map[string]any{"thought": "Thinking..."},
			want:   "Thinking...",
		},
		{
			name:   "priority order - result first",
			result: map[string]any{"result": "Result", "answer": "Answer"},
			want:   "Result",
		},
		{
			name:   "empty result",
			result: map[string]any{},
			want:   "Task completed",
		},
		{
			name:   "nil result",
			result: nil,
			want:   "Task completed",
		},
		{
			name:   "unknown fields as JSON",
			result: map[string]any{"custom": "value"},
			want:   `{"custom":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAgentResult(tt.result)
			if got != tt.want {
				t.Errorf("extractAgentResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCoderHandler_Role(t *testing.T) {
	h := NewCoderHandler()
	if h.Role() != "coder" {
		t.Errorf("Role() = %q, want coder", h.Role())
	}
}

func TestPlannerHandler_Role(t *testing.T) {
	h := NewPlannerHandler()
	if h.Role() != "planner" {
		t.Errorf("Role() = %q, want planner", h.Role())
	}
}

func TestReviewerHandler_Role(t *testing.T) {
	h := NewReviewerHandler()
	if h.Role() != "reviewer" {
		t.Errorf("Role() = %q, want reviewer", h.Role())
	}
}

func TestCoderHandler_HandleEvent_Heartbeat(t *testing.T) {
	h := NewCoderHandler()

	eventData := agentdomain.EventData{
		EventID: "event-1",
		Kind:    "heartbeat",
	}
	env := envelope.OK("agent.event", eventData)
	body, _ := json.Marshal(env)

	msg := &actor.Message{
		ID:      "msg-1",
		Subject: "agent.event",
		Body:    body,
	}

	err := h.HandleEvent(context.Background(), msg, nil, nil)
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestPlannerHandler_HandleEvent_Heartbeat(t *testing.T) {
	h := NewPlannerHandler()

	eventData := agentdomain.EventData{
		EventID: "event-1",
		Kind:    "heartbeat",
	}
	env := envelope.OK("agent.event", eventData)
	body, _ := json.Marshal(env)

	msg := &actor.Message{
		ID:      "msg-1",
		Subject: "agent.event",
		Body:    body,
	}

	err := h.HandleEvent(context.Background(), msg, nil, nil)
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestReviewerHandler_HandleEvent_Heartbeat(t *testing.T) {
	h := NewReviewerHandler()

	eventData := agentdomain.EventData{
		EventID: "event-1",
		Kind:    "heartbeat",
	}
	env := envelope.OK("agent.event", eventData)
	body, _ := json.Marshal(env)

	msg := &actor.Message{
		ID:      "msg-1",
		Subject: "agent.event",
		Body:    body,
	}

	err := h.HandleEvent(context.Background(), msg, nil, nil)
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestHandler_Interface(t *testing.T) {
	// Verify all handlers implement Handler interface
	var _ Handler = (*CoderHandler)(nil)
	var _ Handler = (*PlannerHandler)(nil)
	var _ Handler = (*ReviewerHandler)(nil)
}
