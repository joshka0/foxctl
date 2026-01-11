package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMChatEngine_BuildMessages(t *testing.T) {
	engine := &LLMChatEngine{}

	input := EngineInput{
		SystemPrompt: "You are a helpful assistant.",
		Messages: []Message{
			{Role: RoleUser, Content: "Hello"},
			{Role: RoleAssistant, Content: "Hi there!"},
			{Role: RoleUser, Content: "How are you?"},
		},
	}

	messages := engine.buildMessages(input)

	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}

	// Check system message
	if messages[0].Role != "system" {
		t.Errorf("expected system role, got %s", messages[0].Role)
	}
	if messages[0].Content != "You are a helpful assistant." {
		t.Errorf("unexpected system content: %s", messages[0].Content)
	}

	// Check user/assistant messages
	if messages[1].Role != "user" || messages[1].Content != "Hello" {
		t.Errorf("unexpected message[1]: %+v", messages[1])
	}
	if messages[2].Role != "assistant" || messages[2].Content != "Hi there!" {
		t.Errorf("unexpected message[2]: %+v", messages[2])
	}
}

func TestLLMChatEngine_BuildTools(t *testing.T) {
	engine := &LLMChatEngine{}

	tools := []ToolDef{
		{
			Name:        "get_weather",
			Description: "Get the weather for a location",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
		},
	}

	oaiTools := engine.buildTools(tools)

	if len(oaiTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(oaiTools))
	}

	if oaiTools[0].Type != "function" {
		t.Errorf("expected function type, got %s", oaiTools[0].Type)
	}
	if oaiTools[0].Function.Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", oaiTools[0].Function.Name)
	}
}

func TestLLMChatEngine_Run_NoToolCalls(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := oaiResponse{
			ID: "test-123",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "Hello! How can I help you?"},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{
				PromptTokens:     10,
				CompletionTokens: 8,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       server.URL,
			Model:         "test-model",
			MaxIterations: 10,
		},
		client: http.DefaultClient,
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.AssistantText != "Hello! How can I help you?" {
		t.Errorf("unexpected response: %s", output.AssistantText)
	}
	if output.StopReason != StopReasonEndTurn {
		t.Errorf("expected end_turn, got %s", output.StopReason)
	}
	if output.Tokens.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", output.Tokens.InputTokens)
	}
}

func TestLLMChatEngine_Run_WithToolCall(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		if callCount == 1 {
			// First call: return tool call
			resp := oaiResponse{
				ID: "test-123",
				Choices: []struct {
					Message      oaiMessage `json:"message"`
					FinishReason string     `json:"finish_reason"`
				}{
					{
						Message: oaiMessage{
							Role: "assistant",
							ToolCalls: []oaiToolCall{
								{
									ID:   "call_123",
									Type: "function",
									Function: oaiFunction{
										Name:      "get_weather",
										Arguments: `{"location":"Seattle"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Second call: return final response
		resp := oaiResponse{
			ID: "test-456",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "The weather in Seattle is sunny."},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create mock tool executor
	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"weather":"sunny","temp":72}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       server.URL,
			Model:         "test-model",
			MaxIterations: 10,
		},
		client:     http.DefaultClient,
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "What's the weather in Seattle?"}},
		Tools: []ToolDef{
			{Name: "get_weather", Description: "Get weather"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.AssistantText != "The weather in Seattle is sunny." {
		t.Errorf("unexpected response: %s", output.AssistantText)
	}
	if len(output.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(output.ToolCalls))
	}
	if output.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected get_weather, got %s", output.ToolCalls[0].Name)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestLLMChatEngine_Run_MaxIterations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return tool call to trigger max iterations
		resp := oaiResponse{
			ID: "test-123",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message: oaiMessage{
						Role: "assistant",
						ToolCalls: []oaiToolCall{
							{
								ID:   "call_loop",
								Type: "function",
								Function: oaiFunction{
									Name:      "loop_tool",
									Arguments: `{}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"result":"continue"}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       server.URL,
			Model:         "test-model",
			MaxIterations: 3, // Low limit
		},
		client:     http.DefaultClient,
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Loop forever"}},
		Tools:    []ToolDef{{Name: "loop_tool", Description: "Loops"}},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.StopReason != StopReasonMaxIterations {
		t.Errorf("expected max_iterations, got %s", output.StopReason)
	}
}

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		wantKey  string
		wantProv string
	}{
		{
			name:     "no keys",
			envVars:  map[string]string{},
			wantKey:  "",
			wantProv: "",
		},
		{
			name:     "openrouter priority",
			envVars:  map[string]string{"OPENROUTER_API_KEY": "or-key", "GROQ_API_KEY": "groq-key"},
			wantKey:  "or-key",
			wantProv: "openrouter",
		},
		{
			name:     "groq fallback",
			envVars:  map[string]string{"GROQ_API_KEY": "groq-key"},
			wantKey:  "groq-key",
			wantProv: "groq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env
			for _, k := range []string{"OPENROUTER_API_KEY", "GROQ_API_KEY", "OPENAI_API_KEY"} {
				t.Setenv(k, "")
			}
			// Set test env
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			key, prov := detectProvider()
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
			}
			if prov != tt.wantProv {
				t.Errorf("provider = %q, want %q", prov, tt.wantProv)
			}
		})
	}
}

func TestBaseURLForProvider(t *testing.T) {
	tests := []struct {
		provider string
		want     string
	}{
		{"openrouter", "https://openrouter.ai/api/v1"},
		{"groq", "https://api.groq.com/openai/v1"},
		{"openai", "https://api.openai.com/v1"},
		{"", "https://api.openai.com/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := baseURLForProvider(tt.provider)
			if got != tt.want {
				t.Errorf("baseURLForProvider(%q) = %q, want %q", tt.provider, got, tt.want)
			}
		})
	}
}
