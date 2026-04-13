package actor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	agenttypes "github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/runtime/agentprompt"
	agentdomain "github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
)

func TestAgentActorConfig(t *testing.T) {
	// Test that AgentActorConfig holds all required fields
	cfg := AgentActorConfig{
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
	// Ensure CI/user env doesn't override defaults and make this test deterministic.
	t.Setenv("CEREBRAS_MODEL", "")
	t.Setenv("OPENROUTER_MODEL", "")
	t.Setenv("GROQ_MODEL", "")
	t.Setenv("GEMINI_MODEL", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("LMSTUDIO_MODEL", "")

	tests := []struct {
		provider string
		want     string
	}{
		{"gemini", "gemini-2.5-flash"},
		{"", "gemini-2.5-flash"},
		{"openai", "gpt-4.1-mini"},
		{"anthropic", "claude-haiku-4-5"},
		{"groq", "llama-4-scout-17b-16e"},
		{"openrouter", "google/gemini-3.1-flash-lite-preview"},
		{"lmstudio", "zai-org/glm-4.7-flash"},
		{"unknown", "zai-org/glm-4.7-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := llmproviders.DefaultModelForProvider(tt.provider)
			if got != tt.want {
				t.Errorf("DefaultModelForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}

func TestAgentInstruction(t *testing.T) {
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
			instruction := agentprompt.Instruction(tt.role)
			if instruction == "" {
				t.Error("instruction is empty")
			}
			if !strings.Contains(instruction, tt.contains) {
				t.Errorf("instruction should contain %q", tt.contains)
			}
		})
	}
}

func TestAgentActorMessageParsing(t *testing.T) {
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

func TestAgentActorReplyMessage(t *testing.T) {
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

func TestAgentActorOptions(t *testing.T) {
	// Test that options are applied correctly
	logger := zerolog.New(nil).With().Str("test", "true").Logger()

	actor := &AgentActor{}
	opt := WithAgentLogger(logger)
	opt(actor)

	// The logger should be set (we can't easily compare loggers, but we can verify no panic)
	if actor.logger.GetLevel() != logger.GetLevel() {
		t.Error("logger not set correctly")
	}
}

func TestAgentActorInterface(t *testing.T) {
	// Verify AgentActor implements Actor interface at compile time
	var _ Actor = (*AgentActor)(nil)
}

func TestAgentActorLifecycle(t *testing.T) {
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

func TestAgentActorHandlerRegistration(t *testing.T) {
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

func TestAgentActorNoHandlerError(t *testing.T) {
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

// --- Console Handler Tests ---

func TestConsoleAskDataParsing(t *testing.T) {
	// Test that ConsoleAskData is correctly parsed
	askData := agentdomain.ConsoleAskData{
		AskID:     "ask-console-123",
		Prompt:    "What is the capital of France?",
		Context:   map[string]any{"topic": "geography"},
		ConsoleID: "console-456",
	}
	env := envelope.OK("console.ask", askData)
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var parsed struct {
		Data agentdomain.ConsoleAskData `json:"data"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if parsed.Data.AskID != "ask-console-123" {
		t.Errorf("AskID = %q, want %q", parsed.Data.AskID, "ask-console-123")
	}
	if parsed.Data.Prompt != "What is the capital of France?" {
		t.Errorf("Prompt = %q, want %q", parsed.Data.Prompt, "What is the capital of France?")
	}
	if parsed.Data.ConsoleID != "console-456" {
		t.Errorf("ConsoleID = %q, want %q", parsed.Data.ConsoleID, "console-456")
	}
	if parsed.Data.Context["topic"] != "geography" {
		t.Errorf("Context[topic] = %v, want %q", parsed.Data.Context["topic"], "geography")
	}
}

func TestConsoleCmdDataParsing(t *testing.T) {
	tests := []struct {
		name   string
		action string
		askID  string
	}{
		{"cancel", "cancel", "ask-to-cancel"},
		{"pause", "pause", "ask-to-pause"},
		{"resume", "resume", "ask-to-resume"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdData := agentdomain.ConsoleCmdData{
				CmdID:  "cmd-789",
				Action: tt.action,
				AskID:  tt.askID,
			}
			env := envelope.OK("console.cmd", cmdData)
			payload, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			var parsed struct {
				Data agentdomain.ConsoleCmdData `json:"data"`
			}
			if err := json.Unmarshal(payload, &parsed); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}

			if parsed.Data.CmdID != "cmd-789" {
				t.Errorf("CmdID = %q, want %q", parsed.Data.CmdID, "cmd-789")
			}
			if parsed.Data.Action != tt.action {
				t.Errorf("Action = %q, want %q", parsed.Data.Action, tt.action)
			}
			if parsed.Data.AskID != tt.askID {
				t.Errorf("AskID = %q, want %q", parsed.Data.AskID, tt.askID)
			}
		})
	}
}

func TestConsoleReplyDataParsing(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		response string
	}{
		{"ok", "ok", "The capital of France is Paris."},
		{"error", "error", "Error: Connection timeout"},
		{"cancelled", "cancelled", "Cancelled by user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replyData := agentdomain.ConsoleReplyData{
				AskID:    "ask-reply-123",
				Response: tt.response,
				Status:   tt.status,
				Metrics: map[string]any{
					"duration_ms": int64(150),
				},
			}
			env := envelope.OK("console.reply", replyData)
			payload, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			var parsed struct {
				Data agentdomain.ConsoleReplyData `json:"data"`
			}
			if err := json.Unmarshal(payload, &parsed); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}

			if parsed.Data.AskID != "ask-reply-123" {
				t.Errorf("AskID = %q, want %q", parsed.Data.AskID, "ask-reply-123")
			}
			if parsed.Data.Status != tt.status {
				t.Errorf("Status = %q, want %q", parsed.Data.Status, tt.status)
			}
			if parsed.Data.Response != tt.response {
				t.Errorf("Response = %q, want %q", parsed.Data.Response, tt.response)
			}
			if parsed.Data.Metrics["duration_ms"] == nil {
				t.Error("Metrics[duration_ms] is nil")
			}
		})
	}
}

func TestConsoleEventDataParsing(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		content   string
		iteration int
		toolName  string
	}{
		{"thought", "thought", "I need to search for information", 1, ""},
		{"tool_call", "tool_call", "Calling search API", 1, "web_search"},
		{"tool_result", "tool_result", "Found 10 results", 1, "web_search"},
		{"progress", "progress", "Starting execution...", 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventData := agentdomain.ConsoleEventData{
				AskID:     "ask-event-123",
				Kind:      tt.kind,
				Content:   tt.content,
				Seq:       1,
				Iteration: tt.iteration,
				ToolName:  tt.toolName,
			}
			env := envelope.OK("console.event", eventData)
			payload, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}

			var parsed struct {
				Data agentdomain.ConsoleEventData `json:"data"`
			}
			if err := json.Unmarshal(payload, &parsed); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}

			if parsed.Data.AskID != "ask-event-123" {
				t.Errorf("AskID = %q, want %q", parsed.Data.AskID, "ask-event-123")
			}
			if parsed.Data.Kind != tt.kind {
				t.Errorf("Kind = %q, want %q", parsed.Data.Kind, tt.kind)
			}
			if parsed.Data.Content != tt.content {
				t.Errorf("Content = %q, want %q", parsed.Data.Content, tt.content)
			}
			if parsed.Data.Iteration != tt.iteration {
				t.Errorf("Iteration = %d, want %d", parsed.Data.Iteration, tt.iteration)
			}
			if parsed.Data.ToolName != tt.toolName {
				t.Errorf("ToolName = %q, want %q", parsed.Data.ToolName, tt.toolName)
			}
		})
	}
}

func TestConsoleMessageRoundTrip(t *testing.T) {
	// Test complete round-trip: console.ask -> console.event -> console.reply
	t.Run("full conversation flow", func(t *testing.T) {
		// Step 1: Build ask message
		askData := agentdomain.ConsoleAskData{
			AskID:  "ask-flow-001",
			Prompt: "Write a hello world program",
		}
		askEnv := envelope.OK("console.ask", askData)
		askPayload, _ := json.Marshal(askEnv)

		askMsg := &Message{
			ID:        "msg-ask-001",
			Subject:   "console.ask",
			Body:      askPayload,
			Headers:   map[string]string{"correlation": "corr-001"},
			CreatedAt: time.Now(),
		}

		// Verify ask message structure
		if askMsg.Subject != "console.ask" {
			t.Errorf("Ask subject = %q, want %q", askMsg.Subject, "console.ask")
		}
		if askMsg.Headers["correlation"] != "corr-001" {
			t.Errorf("Correlation = %q, want %q", askMsg.Headers["correlation"], "corr-001")
		}

		// Step 2: Build event message (simulating streaming)
		eventData := agentdomain.ConsoleEventData{
			AskID:     "ask-flow-001",
			Kind:      "progress",
			Content:   "Generating code...",
			Seq:       1,
			Iteration: 1,
		}
		eventEnv := envelope.OK("console.event", eventData)
		eventPayload, _ := json.Marshal(eventEnv)

		eventMsg := &Message{
			ID:        "msg-event-001",
			Subject:   "console.event",
			Body:      eventPayload,
			Headers:   map[string]string{"correlation": "corr-001", "ask_id": "ask-flow-001"},
			CreatedAt: time.Now(),
		}

		// Verify event message structure
		if eventMsg.Subject != "console.event" {
			t.Errorf("Event subject = %q, want %q", eventMsg.Subject, "console.event")
		}
		if eventMsg.Headers["ask_id"] != "ask-flow-001" {
			t.Errorf("Event ask_id = %q, want %q", eventMsg.Headers["ask_id"], "ask-flow-001")
		}

		// Step 3: Build reply message
		replyData := agentdomain.ConsoleReplyData{
			AskID:    "ask-flow-001",
			Response: "print('Hello, World!')",
			Status:   "ok",
			Metrics:  map[string]any{"duration_ms": int64(250)},
		}
		replyEnv := envelope.OK("console.reply", replyData)
		replyPayload, _ := json.Marshal(replyEnv)

		replyMsg := &Message{
			ID:        "msg-reply-001",
			Subject:   "console.reply",
			Body:      replyPayload,
			Headers:   map[string]string{"correlation": "corr-001"},
			CreatedAt: time.Now(),
		}

		// Verify reply message structure
		if replyMsg.Subject != "console.reply" {
			t.Errorf("Reply subject = %q, want %q", replyMsg.Subject, "console.reply")
		}

		// Verify all messages have the same correlation ID
		if askMsg.Headers["correlation"] != replyMsg.Headers["correlation"] {
			t.Error("Ask and Reply have different correlation IDs")
		}
	})
}

func TestConsoleCancelFlow(t *testing.T) {
	// Test that cancel command message is properly structured
	cmdData := agentdomain.ConsoleCmdData{
		CmdID:  "cmd-cancel-001",
		Action: "cancel",
		AskID:  "ask-to-cancel",
	}
	cmdEnv := envelope.OK("console.cmd", cmdData)
	cmdPayload, err := json.Marshal(cmdEnv)
	if err != nil {
		t.Fatalf("marshal cmd: %v", err)
	}

	cmdMsg := &Message{
		ID:        "msg-cmd-001",
		Subject:   "console.cmd",
		Body:      cmdPayload,
		Headers:   map[string]string{"ask_id": "ask-to-cancel"},
		CreatedAt: time.Now(),
	}

	if cmdMsg.Subject != "console.cmd" {
		t.Errorf("Subject = %q, want %q", cmdMsg.Subject, "console.cmd")
	}
	if cmdMsg.Headers["ask_id"] != "ask-to-cancel" {
		t.Errorf("ask_id header = %q, want %q", cmdMsg.Headers["ask_id"], "ask-to-cancel")
	}

	// Parse back the payload to verify
	var parsed struct {
		Data agentdomain.ConsoleCmdData `json:"data"`
	}
	if err := json.Unmarshal(cmdMsg.Body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Data.Action != "cancel" {
		t.Errorf("Action = %q, want %q", parsed.Data.Action, "cancel")
	}
}
