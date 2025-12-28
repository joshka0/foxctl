package actor

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"

	agenttypes "github.com/jkatigb/agentctl/internal/agent/types"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

func TestDspyActorConfig(t *testing.T) {
	// Test that DspyActorConfig holds all required fields
	cfg := DspyActorConfig{
		ActorConfig: Config{
			ID:        "test-actor-1",
			Namespace: "test-ns",
			Role:      "coder",
		},
		AgentConfig: agenttypes.AgentConfig{
			Role:        agenttypes.RoleCoder,
			ActorID:     "actor-1",
			WorkspaceID: "ws-1",
		},
		LLMProvider:   "gemini",
		LLMModel:      "gemini-flash",
		WorkspaceRoot: "/tmp/test",
	}

	if cfg.ActorConfig.ID != "test-actor-1" {
		t.Errorf("ActorConfig.ID = %q, want %q", cfg.ActorConfig.ID, "test-actor-1")
	}
	if cfg.AgentConfig.Role != agenttypes.RoleCoder {
		t.Errorf("AgentConfig.Role = %q, want %q", cfg.AgentConfig.Role, agenttypes.RoleCoder)
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"gemini", "gemini-2.0-flash"},
		{"", "gemini-2.0-flash"},
		{"openai", "gpt-4.1-mini"},
		{"anthropic", "claude-haiku-4-5"},
		{"groq", "llama-3.1-70b-versatile"},
		{"openrouter", "anthropic/claude-haiku-4-5"},
		{"unknown", "gemini-2.0-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := defaultModelForProvider(tt.provider)
			if got != tt.want {
				t.Errorf("defaultModelForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestBuildAgentSignature(t *testing.T) {
	tests := []struct {
		role     agenttypes.AgentRole
		contains string
	}{
		{agenttypes.RoleCoder, "coding agent"},
		{agenttypes.RolePlanner, "planning agent"},
		{agenttypes.RoleReviewer, "code review agent"},
		{agenttypes.AgentRole("unknown"), "helpful agent"},
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			sig := buildAgentSignature(tt.role)
			if sig == nil {
				t.Fatal("signature is nil")
			}
			// The signature should have an instruction
			if sig.Instruction == "" {
				t.Error("instruction is empty")
			}
		})
	}
}

func TestExtractResult(t *testing.T) {
	tests := []struct {
		name      string
		resultMap map[string]any
		want      string
	}{
		{
			name:      "nil map",
			resultMap: nil,
			want:      "Task completed",
		},
		{
			name: "result field",
			resultMap: map[string]any{
				"result": "The answer is 42",
			},
			want: "The answer is 42",
		},
		{
			name: "answer field",
			resultMap: map[string]any{
				"answer": "Yes, that's correct",
			},
			want: "Yes, that's correct",
		},
		{
			name: "output field",
			resultMap: map[string]any{
				"output": "Output value",
			},
			want: "Output value",
		},
		{
			name: "thought field",
			resultMap: map[string]any{
				"thought": "I think therefore I am",
			},
			want: "I think therefore I am",
		},
		{
			name: "priority: result over answer",
			resultMap: map[string]any{
				"result": "Result value",
				"answer": "Answer value",
			},
			want: "Result value",
		},
		{
			name: "skip internal fields",
			resultMap: map[string]any{
				"action":               "search",
				"observation":          "found something",
				"conversation_context": "internal state",
				"custom_field":         "user data",
			},
			want: "map[custom_field:user data]",
		},
		{
			name: "empty result uses thought",
			resultMap: map[string]any{
				"result":  "",
				"thought": "My reasoning",
			},
			want: "My reasoning",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractResult(tt.resultMap)
			if got != tt.want {
				t.Errorf("extractResult() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDspyActorMessageParsing(t *testing.T) {
	// Test that message payloads are correctly parsed
	t.Run("AskData parsing", func(t *testing.T) {
		askData := agentdomain.AskData{
			AskID:    "ask-123",
			Kind:     "context",
			Question: "What is the meaning of life?",
			Context:  map[string]any{"topic": "philosophy"},
		}
		env := envelope.OK("agent.ask", askData)
		payload, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}

		var parsed struct {
			Data agentdomain.AskData `json:"data"`
		}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}

		if parsed.Data.AskID != "ask-123" {
			t.Errorf("AskID = %q, want %q", parsed.Data.AskID, "ask-123")
		}
		if parsed.Data.Question != "What is the meaning of life?" {
			t.Errorf("Question = %q, want %q", parsed.Data.Question, "What is the meaning of life?")
		}
	})

	t.Run("CmdData parsing", func(t *testing.T) {
		cmdData := agentdomain.CmdData{
			CmdID:  "cmd-456",
			Action: "run_turn",
			Args:   map[string]any{"prompt": "Do something"},
		}
		env := envelope.OK("agent.cmd", cmdData)
		payload, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}

		var parsed struct {
			Data agentdomain.CmdData `json:"data"`
		}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}

		if parsed.Data.CmdID != "cmd-456" {
			t.Errorf("CmdID = %q, want %q", parsed.Data.CmdID, "cmd-456")
		}
		if parsed.Data.Action != "run_turn" {
			t.Errorf("Action = %q, want %q", parsed.Data.Action, "run_turn")
		}
	})

	t.Run("EventData parsing", func(t *testing.T) {
		eventData := agentdomain.EventData{
			EventID:  "event-789",
			Kind:     "heartbeat",
			JobCount: 5,
		}
		env := envelope.OK("agent.event", eventData)
		payload, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}

		var parsed struct {
			Data agentdomain.EventData `json:"data"`
		}
		if err := json.Unmarshal(payload, &parsed); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}

		if parsed.Data.EventID != "event-789" {
			t.Errorf("EventID = %q, want %q", parsed.Data.EventID, "event-789")
		}
		if parsed.Data.Kind != "heartbeat" {
			t.Errorf("Kind = %q, want %q", parsed.Data.Kind, "heartbeat")
		}
	})
}

func TestDspyActorReplyMessage(t *testing.T) {
	// Test reply message construction
	replyData := agentdomain.ReplyData{
		AskID:  "ask-123",
		Answer: map[string]any{"response": "The answer is 42"},
	}
	replyEnv := envelope.OK("agent.reply", replyData)
	replyPayload, err := json.Marshal(replyEnv)
	if err != nil {
		t.Fatalf("marshal reply envelope: %v", err)
	}

	msg := &Message{
		ID:        "msg-001",
		Subject:   "agent.reply",
		Body:      replyPayload,
		CreatedAt: time.Now(),
	}

	if msg.Subject != "agent.reply" {
		t.Errorf("Subject = %q, want %q", msg.Subject, "agent.reply")
	}
	if msg.ID == "" {
		t.Error("ID is empty")
	}
}

func TestDspyActorOptions(t *testing.T) {
	// Test that options are applied correctly
	logger := zerolog.New(nil).With().Str("test", "true").Logger()

	actor := &DspyActor{}
	opt := WithDspyLogger(logger)
	opt(actor)

	// The logger should be set (we can't easily compare loggers, but we can verify no panic)
	if actor.logger.GetLevel() != logger.GetLevel() {
		t.Error("logger not set correctly")
	}
}

func TestDspyActorInterface(t *testing.T) {
	// Verify DspyActor implements Actor interface at compile time
	var _ Actor = (*DspyActor)(nil)
}

func TestDspyActorLifecycle(t *testing.T) {
	// Test actor state transitions
	baseActor := NewBaseActor(Config{
		ID:        "test-lifecycle",
		Namespace: "test-ns",
		Role:      "coder",
	})

	// Initial state should be stopped
	if baseActor.State() != StateStopped {
		t.Errorf("initial state = %v, want %v", baseActor.State(), StateStopped)
	}

	// Start should transition to idle
	ctx := context.Background()
	if err := baseActor.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if baseActor.State() != StateIdle {
		t.Errorf("after start state = %v, want %v", baseActor.State(), StateIdle)
	}

	// Stop should transition to stopped
	if err := baseActor.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if baseActor.State() != StateStopped {
		t.Errorf("after stop state = %v, want %v", baseActor.State(), StateStopped)
	}
}

func TestDspyActorHandlerRegistration(t *testing.T) {
	// Test that handlers are registered correctly
	baseActor := NewBaseActor(Config{
		ID:        "test-handlers",
		Namespace: "test-ns",
		Role:      "coder",
	})

	handlerCalled := false
	handler := func(ctx context.Context, msg *Message) (*Message, error) {
		handlerCalled = true
		return nil, nil
	}

	baseActor.RegisterHandler("test.subject", handler)

	// Send a message with that subject
	msg := &Message{
		ID:      "msg-001",
		Subject: "test.subject",
		Body:    []byte("{}"),
	}

	ctx := context.Background()
	if err := baseActor.OnMailReceived(ctx, msg); err != nil {
		t.Fatalf("OnMailReceived() error = %v", err)
	}

	if !handlerCalled {
		t.Error("handler was not called")
	}
}

func TestDspyActorNoHandlerError(t *testing.T) {
	// Test that missing handler returns error
	baseActor := NewBaseActor(Config{
		ID:        "test-no-handler",
		Namespace: "test-ns",
		Role:      "coder",
	})

	msg := &Message{
		ID:      "msg-001",
		Subject: "unknown.subject",
		Body:    []byte("{}"),
	}

	ctx := context.Background()
	err := baseActor.OnMailReceived(ctx, msg)
	if err == nil {
		t.Error("expected error for unknown subject")
	}
}
