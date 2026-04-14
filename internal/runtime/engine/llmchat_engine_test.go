package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/runtime/hooks"
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       "http://mock",
			Model:         "test-model",
			MaxIterations: 10,
		},
		client: &http.Client{Transport: &handlerTransport{handler: handler}},
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

func TestLLMChatEngine_Run_NoAuthHeaderWhenAuthModeNone(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		resp := oaiResponse{
			ID: "test-noauth",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			Provider:      "openai_compat",
			BaseURL:       "http://mock",
			Model:         "demo-model",
			AuthMode:      "none",
			MaxIterations: 1,
		},
		client: &http.Client{Transport: &handlerTransport{handler: handler}},
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.AssistantText != "ok" {
		t.Fatalf("response = %q, want ok", output.AssistantText)
	}
}

func TestLLMChatEngine_Run_CustomHeaderAuth(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Demo-Key"); got != "Token demo-secret" {
			t.Fatalf("X-Demo-Key = %q, want %q", got, "Token demo-secret")
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "" {
			t.Fatalf("unexpected Authorization header: %q", got)
		}
		resp := oaiResponse{
			ID: "test-header-auth",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			Provider:      "openai_compat",
			APIKey:        "demo-secret",
			BaseURL:       "http://mock",
			Model:         "demo-model",
			AuthMode:      "header",
			AuthHeader:    "X-Demo-Key",
			AuthPrefix:    "Token ",
			MaxIterations: 1,
		},
		client: &http.Client{Transport: &handlerTransport{handler: handler}},
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.AssistantText != "ok" {
		t.Fatalf("response = %q, want ok", output.AssistantText)
	}
}

func TestLLMChatEngine_Run_EmptyFinalResponseForcesFinalize(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			resp := oaiResponse{
				ID: "test-empty-1",
				Choices: []struct {
					Message      oaiMessage `json:"message"`
					FinishReason string     `json:"finish_reason"`
				}{
					{
						Message:      oaiMessage{Role: "assistant", Content: ""},
						FinishReason: "stop",
					},
				},
				Usage: struct {
					PromptTokens     int `json:"prompt_tokens"`
					CompletionTokens int `json:"completion_tokens"`
				}{
					PromptTokens:     12,
					CompletionTokens: 15,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		resp := oaiResponse{
			ID: "test-empty-2",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "Fallback final answer."},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			}{
				PromptTokens:     6,
				CompletionTokens: 4,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       "http://mock",
			Model:         "test-model",
			MaxIterations: 10,
		},
		client: &http.Client{Transport: &handlerTransport{handler: handler}},
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.AssistantText != "Fallback final answer." {
		t.Fatalf("assistant_text=%q want %q", output.AssistantText, "Fallback final answer.")
	}
	if callCount != 2 {
		t.Fatalf("call_count=%d want 2", callCount)
	}
}

func TestLLMChatEngine_Run_WithToolCall(t *testing.T) {
	callCount := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	// Create mock tool executor
	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"weather":"sunny","temp":72}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       "http://mock",
			Model:         "test-model",
			MaxIterations: 10,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
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
	if len(output.Iterations) != 2 {
		t.Fatalf("iterations=%d want 2", len(output.Iterations))
	}
	if output.Iterations[0].ToolCalls != 1 || len(output.Iterations[0].ToolNames) != 1 || output.Iterations[0].ToolNames[0] != "get_weather" {
		t.Fatalf("iteration[0]=%+v", output.Iterations[0])
	}
	if output.Iterations[0].ToolResultTokenEstimate == 0 {
		t.Fatalf("iteration[0]=%+v expected tool result token estimate", output.Iterations[0])
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls, got %d", callCount)
	}
}

func TestExtractFinalAnswerJSONPayload(t *testing.T) {
	tests := []struct {
		name string
		args string
		want string
	}{
		{
			name: "payload field",
			args: `{"payload":{"owner":"Mina","codename":"amber-river-19"}}`,
			want: `{"codename":"amber-river-19","owner":"Mina"}`,
		},
		{
			name: "json alias",
			args: `{"json":{"status":"ok"}}`,
			want: `{"status":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractFinalAnswerJSONPayload(json.RawMessage(tt.args))
			if err != nil {
				t.Fatalf("extractFinalAnswerJSONPayload() error = %v", err)
			}

			var gotJSON any
			var wantJSON any
			if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
				t.Fatalf("unmarshal got: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.want), &wantJSON); err != nil {
				t.Fatalf("unmarshal want: %v", err)
			}
			gotBytes, _ := json.Marshal(gotJSON)
			wantBytes, _ := json.Marshal(wantJSON)
			if string(gotBytes) != string(wantBytes) {
				t.Fatalf("extractFinalAnswerJSONPayload() = %s, want %s", gotBytes, wantBytes)
			}
		})
	}
}

func TestLLMChatEngine_Run_WithFinalAnswerJSONTool(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := oaiResponse{
			ID: "test-final-json",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message: oaiMessage{
						Role: "assistant",
						ToolCalls: []oaiToolCall{
							{
								ID:   "call_final_json",
								Type: "function",
								Function: oaiFunction{
									Name:      FinalAnswerJSONToolName,
									Arguments: `{"payload":{"owner":"Mina","codename":"amber-river-19"}}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       "http://mock",
			Model:         "test-model",
			MaxIterations: 10,
		},
		client: &http.Client{Transport: &handlerTransport{handler: handler}},
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Return exact JSON."}},
		Tools:    []ToolDef{FinalAnswerJSONToolDef()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.StopReason != StopReasonEndTurn {
		t.Fatalf("stop reason = %q, want %q", output.StopReason, StopReasonEndTurn)
	}
	if output.AssistantText == "" {
		t.Fatal("assistant text should be populated")
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(output.AssistantText), &got); err != nil {
		t.Fatalf("assistant text should be valid json: %v", err)
	}
	if got["owner"] != "Mina" || got["codename"] != "amber-river-19" {
		t.Fatalf("assistant payload = %+v", got)
	}
	if len(output.ToolCalls) != 1 || output.ToolCalls[0].Name != FinalAnswerJSONToolName {
		t.Fatalf("tool calls = %+v", output.ToolCalls)
	}
	if len(output.ToolResults) != 1 || output.ToolResults[0].IsError {
		t.Fatalf("tool results = %+v", output.ToolResults)
	}
}

// --- buildResearchSummary Tests ---

func TestBuildResearchSummary_Mixed(t *testing.T) {
	calls := []ToolCall{
		{Name: "fs_read_file", Arguments: json.RawMessage(`{"path":"/src/engine/runtime.go"}`)},
		{Name: "context_search", Arguments: json.RawMessage(`{"query":"hook dispatch"}`)},
		{Name: "code_symbols", Arguments: json.RawMessage(`{"path":"/src/engine/types.go"}`)},
		{Name: "repo_index_dag_grep", Arguments: json.RawMessage(`{"query":"Spawn"}`)},
		{Name: "context_grep", Arguments: json.RawMessage(`{"query":"runSession"}`)},
	}

	result := buildResearchSummary(calls)

	if !strings.Contains(result, "runtime.go") {
		t.Errorf("expected runtime.go in files, got: %s", result)
	}
	if !strings.Contains(result, "types.go (symbols)") {
		t.Errorf("expected types.go (symbols) in files, got: %s", result)
	}
	if !strings.Contains(result, `context_search("hook dispatch")`) {
		t.Errorf("expected context_search in searches, got: %s", result)
	}
	if !strings.Contains(result, `dag_grep("Spawn")`) {
		t.Errorf("expected dag_grep in searches, got: %s", result)
	}
	if !strings.Contains(result, `context_grep("runSession")`) {
		t.Errorf("expected context_grep in searches, got: %s", result)
	}
}

func TestBuildResearchSummary_Dedup(t *testing.T) {
	calls := []ToolCall{
		{Name: "fs_read_file", Arguments: json.RawMessage(`{"path":"/src/a.go"}`)},
		{Name: "fs_read_file", Arguments: json.RawMessage(`{"path":"/src/a.go"}`)},
		{Name: "fs_read_file", Arguments: json.RawMessage(`{"path":"/src/b.go"}`)},
	}

	result := buildResearchSummary(calls)

	// Count occurrences of "a.go"
	count := strings.Count(result, "a.go")
	if count != 1 {
		t.Errorf("expected a.go to appear once, appeared %d times in: %s", count, result)
	}
}

func TestBuildResearchSummary_Empty(t *testing.T) {
	result := buildResearchSummary(nil)
	if result != "Files read: (none)\nSearches: (none)" {
		t.Errorf("unexpected empty result: %q", result)
	}
}

func TestBuildResearchSummary_MalformedJSON(t *testing.T) {
	calls := []ToolCall{
		{Name: "fs_read_file", Arguments: json.RawMessage(`not json`)},
		{Name: "fs_read_file", Arguments: json.RawMessage(`{"path":"/valid.go"}`)},
	}

	result := buildResearchSummary(calls)
	if !strings.Contains(result, "valid.go") {
		t.Errorf("expected valid.go despite malformed entry, got: %s", result)
	}
}

// --- Synthesis Transition Tests ---

func TestLLMChatEngine_Run_SynthesisTransition(t *testing.T) {
	callCount := 0
	var lastRequestTools bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		// Parse request to check if tools are present
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		_, lastRequestTools = reqBody["tools"]

		// For iterations 1-3 (MaxIterations=5, SynthesisReserve=2, threshold at iter 3):
		// return tool calls to keep the loop going
		if callCount <= 3 {
			resp := oaiResponse{
				ID: "test",
				Choices: []struct {
					Message      oaiMessage `json:"message"`
					FinishReason string     `json:"finish_reason"`
				}{
					{
						Message: oaiMessage{
							Role: "assistant",
							ToolCalls: []oaiToolCall{
								{
									ID:   fmt.Sprintf("call_%d", callCount),
									Type: "function",
									Function: oaiFunction{
										Name:      "fs_read_file",
										Arguments: fmt.Sprintf(`{"path":"/file%d.go"}`, callCount),
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

		// After synthesis transition (tools stripped), model produces text
		resp := oaiResponse{
			ID: "test-final",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "Here is my synthesis report."},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"content":"file data"}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:           "test-key",
			BaseURL:          "http://mock",
			Model:            "test-model",
			MaxIterations:    5,
			SynthesisReserve: 2,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Research the codebase"}},
		Tools:    []ToolDef{{Name: "fs_read_file", Description: "Read files"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.AssistantText != "Here is my synthesis report." {
		t.Errorf("expected synthesis report, got: %s", output.AssistantText)
	}

	// The 4th LLM call should have had tools stripped
	if lastRequestTools {
		t.Error("expected tools to be stripped after synthesis transition")
	}
}

func TestLLMChatEngine_SynthesisReserve_EdgeCase(t *testing.T) {
	// When MaxIterations <= SynthesisReserve, synthesis should never trigger
	callCount := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Always return tool calls to exhaust iterations
		resp := oaiResponse{
			ID: "test",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message: oaiMessage{
						Role: "assistant",
						ToolCalls: []oaiToolCall{
							{
								ID:   fmt.Sprintf("call_%d", callCount),
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
	})

	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:           "test-key",
			BaseURL:          "http://mock",
			Model:            "test-model",
			MaxIterations:    2,
			SynthesisReserve: 5, // Greater than MaxIterations
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Loop"}},
		Tools:    []ToolDef{{Name: "loop_tool", Description: "Loops"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should hit max iterations, not synthesis
	if output.StopReason != StopReasonMaxIterations {
		t.Errorf("expected max_iterations, got %s", output.StopReason)
	}
	if output.Error != "" {
		t.Errorf("expected empty error for max_iterations stop, got %q", output.Error)
	}
}

type handlerTransport struct {
	handler http.Handler
}

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, req)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}

func TestLLMChatEngine_Run_MaxIterations(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})

	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"result":"continue"}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:        "test-key",
			BaseURL:       "http://mock",
			Model:         "test-model",
			MaxIterations: 3, // Low limit
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
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
	if output.Error != "" {
		t.Errorf("expected empty error for max_iterations stop, got %q", output.Error)
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
			name:     "lmstudio default when no keys",
			envVars:  map[string]string{},
			wantKey:  "lm-studio",
			wantProv: "lmstudio",
		},
		{
			name:     "lmstudio env priority over remote keys",
			envVars:  map[string]string{"LMSTUDIO_API_KEY": "local-key", "OPENROUTER_API_KEY": "or-key"},
			wantKey:  "local-key",
			wantProv: "lmstudio",
		},
		{
			name:     "remote fallback when lmstudio not set",
			envVars:  map[string]string{"GROQ_API_KEY": "groq-key"},
			wantKey:  "groq-key",
			wantProv: "groq",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env
			for _, k := range []string{"LMSTUDIO_API_KEY", "LMSTUDIO_BASE_URL", "LMSTUDIO_MODEL", "OPENROUTER_API_KEY", "GROQ_API_KEY", "OPENAI_API_KEY"} {
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
	t.Setenv("LMSTUDIO_BASE_URL", "")

	tests := []struct {
		provider string
		want     string
	}{
		{"lmstudio", "http://localhost:1234/v1"},
		{"openrouter", "https://openrouter.ai/api/v1"},
		{"groq", "https://api.groq.com/openai/v1"},
		{"openai", "https://api.openai.com/v1"},
		{"openai_compat", "https://api.openai.com/v1"},
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

func TestNewLLMChatEngine_AllowsExplicitNoAuthWithoutAPIKey(t *testing.T) {
	eng, err := NewLLMChatEngine(LLMChatConfig{
		Provider: "openai_compat",
		BaseURL:  "http://localhost:8080/v1",
		Model:    "demo-model",
		AuthMode: "none",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if eng.config.AuthMode != "none" {
		t.Fatalf("auth mode = %q, want none", eng.config.AuthMode)
	}
}

// --- LLMChatEngine Hook Tests ---

func TestLLMChatEngine_Run_WithHooks(t *testing.T) {
	callCount := 0
	var hookCalls []string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
										Name:      "get_data",
										Arguments: `{"id":"123"}`,
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
					Message:      oaiMessage{Role: "assistant", Content: "Here is the data."},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Create mock dispatcher that tracks calls
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			hookCalls = append(hookCalls, string(input.Event))
			return hooks.Result{Output: hooks.NewApprove("ok", nil)}, nil
		},
	}

	// Create mock tool executor
	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			return `{"data":"test"}`, nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:         "test-key",
			BaseURL:        "http://mock",
			Model:          "test-model",
			MaxIterations:  10,
			HookDispatcher: dispatcher,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}), // ToolRunner without dispatcher
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Get the data"}},
		Tools:    []ToolDef{{Name: "get_data", Description: "Get data"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output.AssistantText != "Here is the data." {
		t.Errorf("unexpected response: %s", output.AssistantText)
	}

	// Verify hooks were called in correct order
	if len(hookCalls) < 2 {
		t.Fatalf("expected at least 2 hook calls, got %d: %v", len(hookCalls), hookCalls)
	}
	if hookCalls[0] != "PreToolUse" {
		t.Errorf("expected first hook to be PreToolUse, got %s", hookCalls[0])
	}
	if hookCalls[1] != "PostToolUse" {
		t.Errorf("expected second hook to be PostToolUse, got %s", hookCalls[1])
	}
}

func TestLLMChatEngine_Run_HookBlocks(t *testing.T) {
	callCount := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
									ID:   "call_blocked",
									Type: "function",
									Function: oaiFunction{
										Name:      "dangerous_tool",
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
			return
		}

		// Second call: return final response (after seeing blocked result)
		resp := oaiResponse{
			ID: "test-456",
			Choices: []struct {
				Message      oaiMessage `json:"message"`
				FinishReason string     `json:"finish_reason"`
			}{
				{
					Message:      oaiMessage{Role: "assistant", Content: "Tool was blocked by policy."},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Create dispatcher that blocks the tool
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPreToolUse {
				return hooks.Result{Output: hooks.NewBlock("security policy")}, nil
			}
			return hooks.Result{Output: hooks.NewNone()}, nil
		},
	}

	// Executor that should NOT be called
	executorCalled := false
	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			executorCalled = true
			return "should not see this", nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:         "test-key",
			BaseURL:        "http://mock",
			Model:          "test-model",
			MaxIterations:  10,
			HookDispatcher: dispatcher,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	output, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Do something dangerous"}},
		Tools:    []ToolDef{{Name: "dangerous_tool", Description: "Dangerous"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify executor was NOT called due to hook block
	if executorCalled {
		t.Error("executor should not have been called when hook blocks")
	}

	// Verify the tool result shows blocked
	if len(output.ToolResults) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(output.ToolResults))
	}
	if !output.ToolResults[0].IsError {
		t.Error("expected tool result to be an error")
	}
	if output.ToolResults[0].Content != "Blocked by hook: security policy" {
		t.Errorf("expected blocked message, got: %s", output.ToolResults[0].Content)
	}
}

func TestLLMChatEngine_Run_HookUpdatesInput(t *testing.T) {
	callCount := 0

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
									ID:   "call_modified",
									Type: "function",
									Function: oaiFunction{
										Name:      "process_data",
										Arguments: `{"value":"original"}`,
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
					Message:      oaiMessage{Role: "assistant", Content: "Processed with modified args."},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Create dispatcher that modifies tool input
	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPreToolUse {
				out := hooks.NewApprove("modified", nil)
				out.UpdatedToolInput = json.RawMessage(`{"value":"modified_by_hook"}`)
				return hooks.Result{Output: out}, nil
			}
			return hooks.Result{Output: hooks.NewNone()}, nil
		},
	}

	// Executor that captures received args
	var receivedArgs json.RawMessage
	mockExecutor := &MockToolExecutor{
		ExecuteFn: func(ctx context.Context, name string, args json.RawMessage) (string, error) {
			receivedArgs = args
			return "ok", nil
		},
	}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:         "test-key",
			BaseURL:        "http://mock",
			Model:          "test-model",
			MaxIterations:  10,
			HookDispatcher: dispatcher,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	_, err := engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Process data"}},
		Tools:    []ToolDef{{Name: "process_data", Description: "Process"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify hook modified the args
	if string(receivedArgs) != `{"value":"modified_by_hook"}` {
		t.Errorf("expected modified args, got: %s", string(receivedArgs))
	}
}

func TestLLMChatEngine_SetHookContext(t *testing.T) {
	var receivedInput hooks.Input

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
								ID:       "call_ctx",
								Type:     "function",
								Function: oaiFunction{Name: "test_tool", Arguments: `{}`},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	dispatcher := &MockHookDispatcher{
		DispatchFn: func(ctx context.Context, input hooks.Input) (hooks.Result, error) {
			if input.Event == hooks.EventPreToolUse {
				receivedInput = input
			}
			// Block to end the test quickly
			return hooks.Result{Output: hooks.NewBlock("test")}, nil
		},
	}

	mockExecutor := &MockToolExecutor{}

	engine := &LLMChatEngine{
		config: LLMChatConfig{
			APIKey:         "test-key",
			BaseURL:        "http://mock",
			Model:          "test-model",
			MaxIterations:  10,
			HookDispatcher: dispatcher,
		},
		client:     &http.Client{Transport: &handlerTransport{handler: handler}},
		toolRunner: NewToolRunner(mockExecutor, nil, ToolRunnerConfig{}),
	}

	// Set hook context
	engine.SetHookContext(HookContext{
		SessionID:     "session-test-123",
		ActorID:       "actor-test-456",
		WorkspaceID:   "ws-test-789",
		WorkspaceRoot: "/test/workspace",
	})

	_, _ = engine.Run(context.Background(), EngineInput{
		Messages: []Message{{Role: RoleUser, Content: "Test"}},
		Tools:    []ToolDef{{Name: "test_tool"}},
	})

	// Verify context was passed to hooks
	if receivedInput.SessionID != "session-test-123" {
		t.Errorf("expected session-test-123, got %s", receivedInput.SessionID)
	}
	if receivedInput.ActorID != "actor-test-456" {
		t.Errorf("expected actor-test-456, got %s", receivedInput.ActorID)
	}
	if receivedInput.WorkspaceID != "ws-test-789" {
		t.Errorf("expected ws-test-789, got %s", receivedInput.WorkspaceID)
	}
	if receivedInput.WorkspaceRoot != "/test/workspace" {
		t.Errorf("expected /test/workspace, got %s", receivedInput.WorkspaceRoot)
	}
}
